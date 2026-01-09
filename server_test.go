package mockllm_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/kagent-dev/mockllm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/*.json
var testdataFS embed.FS

// loadTestData loads a JSON file from testdata and unmarshals it into the target
func loadTestData(t *testing.T, filename string, target interface{}) {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + filename)
	require.NoError(t, err)
	err = json.Unmarshal(data, target)
	require.NoError(t, err)
}

// newOpenAIMock creates an OpenAIMock with the given parameters, handling marshal/unmarshal
func newOpenAIMock(t *testing.T, name string, matchType mockllm.MatchType, request openai.ChatCompletionNewParams, response openai.ChatCompletion) mockllm.OpenAIMock {
	t.Helper()
	mock := mockllm.OpenAIMock{
		Name:     name,
		Response: response,
		Match: mockllm.OpenAIRequestMatch{
			MatchType: matchType,
			Message:   request.Messages[len(request.Messages)-1],
		},
	}

	// Marshal and unmarshal the request to get it in the right format
	reqBytes, err := json.Marshal(request)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Match)
	require.NoError(t, err)

	return mock
}

// newOpenAIResponseMock creates an OpenAIResponseMock with the given parameters
func newOpenAIResponseMock(t *testing.T, name string, matchType mockllm.MatchType, request responses.ResponseNewParams, response responses.Response) mockllm.OpenAIResponseMock {
	t.Helper()
	mock := mockllm.OpenAIResponseMock{
		Name:     name,
		Response: response,
		Match: mockllm.OpenAIResponseRequestMatch{
			MatchType: matchType,
			Input:     request.Input,
		},
	}

	// Marshal and unmarshal to get proper structure
	reqBytes, err := json.Marshal(request)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Match)
	require.NoError(t, err)

	return mock
}

// newAnthropicMock creates an AnthropicMock with the given parameters
func newAnthropicMock(t *testing.T, name string, matchType mockllm.MatchType, request anthropic.MessageNewParams, response anthropic.Message) mockllm.AnthropicMock {
	t.Helper()
	mock := mockllm.AnthropicMock{
		Name:     name,
		Response: response,
		Match: mockllm.AnthropicRequestMatch{
			MatchType: matchType,
			Message:   request.Messages[len(request.Messages)-1],
		},
	}

	// Marshal and unmarshal the request to get it in the right format
	reqBytes, err := json.Marshal(request)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Match)
	require.NoError(t, err)

	return mock
}

// startTestServer starts a test server and returns the base URL and cleanup function
func startTestServer(t *testing.T, config mockllm.Config) (string, func()) {
	t.Helper()
	server := mockllm.NewServer(config)
	baseURL, err := server.Start(t.Context())
	require.NoError(t, err)
	return baseURL, func() {
		server.Stop(context.Background())
	}
}

func TestSimpleAnthropicMock(t *testing.T) {
	anthropicRequest := anthropic.MessageNewParams{
		Model:     "claude-3-5-sonnet-20240620",
		MaxTokens: 1000,
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{
						OfText: &anthropic.TextBlockParam{
							Text: "Hello",
						},
					},
				},
			},
		},
	}

	var anthropicResponse anthropic.Message
	loadTestData(t, "anthropic_mock.json", &anthropicResponse)

	mock := newAnthropicMock(t, "test-response", mockllm.MatchTypeContains, anthropicRequest, anthropicResponse)
	config := mockllm.Config{
		Anthropic: []mockllm.AnthropicMock{mock},
	}

	baseURL, cleanup := startTestServer(t, config)
	defer cleanup()

	reqBytes, err := json.Marshal(anthropicRequest)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", baseURL+"/v1/messages", bytes.NewReader(reqBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test-key")
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	// Check response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	require.NoError(t, err)

	assert.Equal(t, "msg_123", responseBody["id"])
	assert.Equal(t, "message", responseBody["type"])
}

func TestHealthCheck(t *testing.T) {
	config := mockllm.Config{
		OpenAI:         []mockllm.OpenAIMock{},
		OpenAIResponse: []mockllm.OpenAIResponseMock{},
		Anthropic:      []mockllm.AnthropicMock{},
	}
	baseURL, cleanup := startTestServer(t, config)
	defer cleanup()

	resp, err := http.Get(baseURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	require.NoError(t, err)

	assert.Equal(t, "healthy", responseBody["status"])
	assert.Equal(t, "mock-llm", responseBody["service"])
	assert.NotNil(t, responseBody["openai"])
	assert.NotNil(t, responseBody["openai_response"])
	assert.NotNil(t, responseBody["anthropic"])
}

func TestOpenAICompletionMock(t *testing.T) {
	// Setup simple response for non-streaming
	simpleRequest := openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Role: "user",
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String("Hello"),
					},
				},
			},
		},
	}

	var simpleResponse openai.ChatCompletion
	loadTestData(t, "openai_mock.json", &simpleResponse)
	simpleMock := newOpenAIMock(t, "test-response", mockllm.MatchTypeExact, simpleRequest, simpleResponse)

	// Setup tool call response for streaming
	toolCallRequest := openai.ChatCompletionNewParams{
		Model: "gpt-4.1-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Role: "user",
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String("What is 2+2?"),
					},
				},
			},
		},
	}

	var toolCallResponse openai.ChatCompletion
	loadTestData(t, "openai_function_mock.json", &toolCallResponse)
	toolCallMock := newOpenAIMock(t, "calculate_request", mockllm.MatchTypeContains, toolCallRequest, toolCallResponse)

	config := mockllm.Config{
		OpenAI: []mockllm.OpenAIMock{simpleMock, toolCallMock},
	}

	baseURL, cleanup := startTestServer(t, config)
	defer cleanup()

	client := openai.NewClient(
		option.WithBaseURL(baseURL+"/v1/"),
		option.WithAPIKey("test-key"),
	)

	// Test non-streaming response
	t.Run("non-streaming", func(t *testing.T) {
		resp, err := client.Chat.Completions.New(t.Context(), simpleRequest)
		require.NoError(t, err)

		assert.Equal(t, "chatcmpl-123", resp.ID)
		assert.Equal(t, "chat.completion", string(resp.Object))
		assert.Equal(t, "Hello! How can I help you today?", resp.Choices[0].Message.Content)
	})

	// Test streaming response (tool calls)
	t.Run("streaming", func(t *testing.T) {
		stream := client.Chat.Completions.NewStreaming(t.Context(), toolCallRequest)

		var receivedToolCalls []openai.ChatCompletionChunkChoiceDeltaToolCall

		for stream.Next() {
			evt := stream.Current()
			if len(evt.Choices) > 0 {
				if len(evt.Choices[0].Delta.ToolCalls) > 0 {
					receivedToolCalls = append(receivedToolCalls, evt.Choices[0].Delta.ToolCalls...)
				}
			}
		}

		if err := stream.Err(); err != nil {
			t.Fatalf("Stream error: %v", err)
		}

		require.Len(t, receivedToolCalls, 1)
		assert.Equal(t, "calculate", receivedToolCalls[0].Function.Name)
		assert.Equal(t, `{"expression": "2+2"}`, receivedToolCalls[0].Function.Arguments)
	})
}

func TestOpenAIResponseMock(t *testing.T) {
	// Setup haiku response for non-streaming
	haikuRequest := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Write me a haiku about Kagent"),
		},
	}

	var haikuResponse responses.Response
	loadTestData(t, "openai_response_mock.json", &haikuResponse)
	haikuMock := newOpenAIResponseMock(t, "haiku-response", mockllm.MatchTypeContains, haikuRequest, haikuResponse)

	// Setup function output response for streaming
	funcRequest := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Calculate 2+2"),
		},
	}

	var funcResponse responses.Response
	loadTestData(t, "openai_response_function_mock.json", &funcResponse)
	funcMock := newOpenAIResponseMock(t, "function-response", mockllm.MatchTypeContains, funcRequest, funcResponse)

	// Setup both mocks on the same server and use request matching to determine which response to return
	config := mockllm.Config{
		OpenAIResponse: []mockllm.OpenAIResponseMock{haikuMock, funcMock},
	}

	baseURL, cleanup := startTestServer(t, config)
	defer cleanup()

	client := openai.NewClient(
		option.WithBaseURL(baseURL+"/v1/"),
		option.WithAPIKey("test-key"),
	)

	// Test non-streaming response (haiku)
	t.Run("non-streaming", func(t *testing.T) {
		resp, err := client.Responses.New(t.Context(), haikuRequest)
		require.NoError(t, err)

		assert.Equal(t, "resp_123", resp.ID)
		outputText := resp.OutputText()
		assert.Contains(t, outputText, "Kagent finds its mind")
	})

	// Test streaming response (function output)
	t.Run("streaming", func(t *testing.T) {
		stream := client.Responses.NewStreaming(t.Context(), funcRequest)

		var receivedEvents []string
		var functionCallFound, functionOutputFound bool

		for stream.Next() {
			data := stream.Current()
			if data.Type != "" {
				receivedEvents = append(receivedEvents, data.Type)
				if data.Type == "response.function_call_arguments.done" {
					functionCallFound = true
				}
				if data.Type == "response.function_call_output.done" {
					functionOutputFound = true
				}
			}
		}

		if err := stream.Err(); err != nil {
			t.Fatalf("Stream error: %v", err)
		}

		assert.Greater(t, len(receivedEvents), 0)
		assert.True(t, functionCallFound, "Should receive function_call_arguments.done event")
		assert.True(t, functionOutputFound, "Should receive function_call_output.done event")
	})
}

func TestOpenAICompletionAndResponseMocks(t *testing.T) {
	// Chat Completions mock
	openaiRequest := openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Role: "user",
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String("Hello"),
					},
				},
			},
		},
	}

	var openaiResponse openai.ChatCompletion
	loadTestData(t, "openai_mock.json", &openaiResponse)
	chatMock := newOpenAIMock(t, "chat-test", mockllm.MatchTypeExact, openaiRequest, openaiResponse)

	// Responses API mock
	responseRequest := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Write me a haiku about Kagent"),
		},
	}

	var response responses.Response
	loadTestData(t, "openai_response_mock.json", &response)
	responseMock := newOpenAIResponseMock(t, "response-test", mockllm.MatchTypeContains, responseRequest, response)

	config := mockllm.Config{
		OpenAI:         []mockllm.OpenAIMock{chatMock},
		OpenAIResponse: []mockllm.OpenAIResponseMock{responseMock},
	}

	baseURL, cleanup := startTestServer(t, config)
	defer cleanup()

	client := openai.NewClient(
		option.WithBaseURL(baseURL+"/v1/"),
		option.WithAPIKey("test-key"),
	)

	chatResp, err := client.Chat.Completions.New(t.Context(), openaiRequest)
	require.NoError(t, err)
	assert.Equal(t, "chatcmpl-123", chatResp.ID)
	assert.Contains(t, chatResp.Choices[0].Message.Content, "Hello")

	// Test that string version mock matches message list version request
	// This simulates how some frameworks send message lists instead of strings
	// Sends {"input": [{"role": "user", "content": "..."}]}
	responseRequestTest := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4,
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(
					"Write me a haiku about Kagent",
					responses.EasyInputMessageRoleUser,
				),
			},
		},
	}

	responseResp, err := client.Responses.New(t.Context(), responseRequestTest)
	require.NoError(t, err)
	assert.Equal(t, "resp_123", responseResp.ID)
	assert.Contains(t, responseResp.OutputText(), "Kagent finds its mind")
}
