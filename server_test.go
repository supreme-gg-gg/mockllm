package mockllm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/kagent-dev/mockllm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// For now, we'll use a simple approach where we create mocks with JSON-compatible structures
// that can be marshaled to/from the SDK types. This allows us to test the basic functionality
// while using the SDK types in the type definitions.

func TestSimpleOpenAIMock(t *testing.T) {
	// Create a simple config - we'll use JSON marshaling to convert to SDK types
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

	openaiResponse := openai.ChatCompletion{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4o-mini",
		Choices: []openai.ChatCompletionChoice{
			{
				Index: 0,
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you today?",
				},
				FinishReason: "stop",
			},
		},
	}

	// Convert to JSON and back to get SDK-compatible structure
	var mock mockllm.OpenAIMock
	mock.Name = "test-response"
	mock.Response = openaiResponse

	mock.Match = mockllm.OpenAIRequestMatch{
		MatchType: mockllm.MatchTypeExact,
		Message:   openaiRequest.Messages[len(openaiRequest.Messages)-1],
	}

	// Marshal and unmarshal the request to get it in the right format
	reqBytes, err := json.Marshal(openaiRequest)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Match)
	require.NoError(t, err)

	config := mockllm.Config{
		OpenAI: []mockllm.OpenAIMock{mock},
	}

	// Start server
	server := mockllm.NewServer(config)
	baseURL, err := server.Start(t.Context())
	require.NoError(t, err)
	defer server.Stop(context.Background())

	// Use Client
	client := openai.NewClient(
		option.WithBaseURL(baseURL+"/v1/"),
		option.WithAPIKey("test-key"),
	)

	resp, err := client.Chat.Completions.New(t.Context(), openaiRequest)
	require.NoError(t, err)

	assert.Equal(t, "chatcmpl-123", resp.ID)
	// Cast to string to avoid undefined constant issues
	assert.Equal(t, "chat.completion", string(resp.Object))
	assert.Equal(t, "Hello! How can I help you today?", resp.Choices[0].Message.Content)
}

func TestSimpleAnthropicMock(t *testing.T) {
	// Create a simple config - we'll use JSON marshaling to convert to SDK types
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

	anthropicResponse := anthropic.Message{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []anthropic.ContentBlockUnion{
			{
				Type: "text",
				Text: "Hello! How can I assist you today?",
			},
		},
		Model:      "claude-3-5-sonnet-20240620",
		StopReason: "end_turn",
	}

	// Convert to JSON and back to get SDK-compatible structure
	var mock mockllm.AnthropicMock
	mock.Name = "test-response"
	mock.Response = anthropicResponse
	mock.Match = mockllm.AnthropicRequestMatch{
		MatchType: mockllm.MatchTypeContains,
		Message:   anthropicRequest.Messages[len(anthropicRequest.Messages)-1],
	}
	// Marshal and unmarshal the request to get it in the right format
	reqBytes, err := json.Marshal(anthropicRequest)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Match)
	require.NoError(t, err)

	config := mockllm.Config{
		Anthropic: []mockllm.AnthropicMock{mock},
	}

	// Start server
	server := mockllm.NewServer(config)
	baseURL, err := server.Start(t.Context())
	require.NoError(t, err)
	defer server.Stop(context.Background()) //nolint:errcheck

	// Make request
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
	config := mockllm.Config{}
	server := mockllm.NewServer(config)
	baseURL, err := server.Start(t.Context())
	require.NoError(t, err)
	defer server.Stop(context.Background()) //nolint:errcheck

	resp, err := http.Get(baseURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var responseBody map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&responseBody)
	require.NoError(t, err)

	assert.Equal(t, "healthy", responseBody["status"])
	assert.Equal(t, "mock-llm", responseBody["service"])
}

func TestStreamingToolCallsOpenAIMock(t *testing.T) {
	// Request
	openaiRequest := openai.ChatCompletionNewParams{
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

	// Response with Tool Calls
	openaiResponse := openai.ChatCompletion{
		ID:      "chatcmpl-calc",
		Object:  "chat.completion",
		Created: 1677652288,
		Model:   "gpt-4.1-mini",
		Choices: []openai.ChatCompletionChoice{
			{
				Index: 0,
				Message: openai.ChatCompletionMessage{
					Role: "assistant",
					ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
						{
							ID:   "call_abc123",
							Type: "function",
							Function: openai.ChatCompletionMessageFunctionToolCallFunction{
								Name:      "calculate",
								Arguments: `{"expression": "2+2"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	// Create mock
	var mock mockllm.OpenAIMock
	mock.Name = "calculate_request"

	// Direct assignment as types.go uses structural typing
	mock.Response = openaiResponse

	mock.Match = mockllm.OpenAIRequestMatch{
		MatchType: mockllm.MatchTypeContains,
		Message:   openaiRequest.Messages[len(openaiRequest.Messages)-1],
	}

	// Marshaling setup for Match struct to be populated correctly in the config
	reqBytes, err := json.Marshal(openaiRequest)
	require.NoError(t, err)
	err = json.Unmarshal(reqBytes, &mock.Match)
	require.NoError(t, err)

	config := mockllm.Config{
		OpenAI: []mockllm.OpenAIMock{mock}, // Match Unmarshall logic issues if any?
		// Note: OpenAI field in Config is []OpenAIMock.
	}

	server := mockllm.NewServer(config)
	baseURL, err := server.Start(t.Context())
	require.NoError(t, err)
	defer server.Stop(context.Background())

	// Use Client
	client := openai.NewClient(
		option.WithBaseURL(baseURL+"/v1/"),
		option.WithAPIKey("test-key"),
	)

	stream := client.Chat.Completions.NewStreaming(t.Context(), openaiRequest)

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

	// Expect to receive the tool call
	require.Len(t, receivedToolCalls, 1)
	assert.Equal(t, "calculate", receivedToolCalls[0].Function.Name)
	assert.Equal(t, `{"expression": "2+2"}`, receivedToolCalls[0].Function.Arguments)
}
