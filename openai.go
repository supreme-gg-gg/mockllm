package mockllm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// Provider handles OpenAI request/response mocking
type OpenAIProvider struct {
	mocks         []OpenAIMock
	responseMocks []OpenAIResponseMock
}

// NewOpenAIProvider creates a new OpenAI OpenAIProvider with the given mocks
// Both chat completion and responses mocks can be provided - they work independently
// and are routed to different endpoints (/v1/chat/completions vs /v1/responses)
func NewOpenAIProvider(mocks []OpenAIMock, responseMocks []OpenAIResponseMock) *OpenAIProvider {
	return &OpenAIProvider{
		mocks:         mocks,
		responseMocks: responseMocks,
	}
}

// Handle processes an OpenAI chat completion request
func (p *OpenAIProvider) Handle(w http.ResponseWriter, r *http.Request) {
	// Parse request body to check for stream
	// This is due to a limitation on the ChatCompletionNewParams type in the SDK
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check for stream: true in raw JSON
	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Parse the incoming request into SDK type
	var requestBody openai.ChatCompletionNewParams
	if err := json.NewDecoder(bytes.NewBuffer(bodyBytes)).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Find a matching mock
	mock := p.findMatchingMock(requestBody)
	if mock == nil {
		requestBodyBytes, err := json.MarshalIndent(requestBody, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode request body: %v", err),
				http.StatusInternalServerError)
			return
		}

		http.Error(w, fmt.Sprintf("No matching mock found. Request: %s",
			string(requestBodyBytes)), http.StatusNotFound)
		return
	}

	// Return the response
	isStream := false
	if streamVal, ok := rawMap["stream"].(bool); ok {
		isStream = streamVal
	}

	if isStream {
		p.handleStreamingResponse(w, mock.Response)
	} else {
		p.handleNonStreamingResponse(w, mock.Response)
	}
}

// findMatchingMock finds the first mock that matches the request
func (p *OpenAIProvider) findMatchingMock(request openai.ChatCompletionNewParams) *OpenAIMock {
	for _, mock := range p.mocks {
		if p.requestsMatch(mock.Match, request) {
			return &mock
		}
	}
	return nil
}

// requestsMatch checks if two requests are equivalent
func (p *OpenAIProvider) requestsMatch(expected OpenAIRequestMatch, actual openai.ChatCompletionNewParams) bool {
	// Simple deep equal comparison for now
	// In the future, we could add more sophisticated matching
	switch expected.MatchType {
	case MatchTypeExact:
		// get Last message from actual
		if len(actual.Messages) == 0 {
			return false
		}
		lastMessage := actual.Messages[len(actual.Messages)-1]
		// Check json is equal
		jsonExpected, err := json.Marshal(expected.Message)
		if err != nil {
			return false
		}
		jsonActual, err := json.Marshal(lastMessage)
		if err != nil {
			return false
		}
		return bytes.Equal(jsonExpected, jsonActual)
	case MatchTypeContains:
		// Check if the last message contains the expected message
		if len(actual.Messages) == 0 {
			return false
		}
		lastMessage := actual.Messages[len(actual.Messages)-1]
		if *lastMessage.GetRole() != *expected.Message.GetRole() {
			return false
		}
		strExpected, ok := expected.Message.GetContent().AsAny().(*string)
		if !ok {
			return false
		}
		strActual, ok := lastMessage.GetContent().AsAny().(*string)
		if !ok {
			return false
		}
		return strings.Contains(*strActual, *strExpected)
	default:
		return false
	}
}

// handleNonStreamingResponse sends a JSON response
func (p *OpenAIProvider) handleNonStreamingResponse(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

// handleStreamingResponse sends a streaming SSE response
func (p *OpenAIProvider) handleStreamingResponse(w http.ResponseWriter, response openai.ChatCompletion) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported by server", http.StatusInternalServerError)
		return
	}

	// Helper to send a chunk
	sendChunk := func(chunk openai.ChatCompletionChunk) {
		jsonBytes, err := json.Marshal(chunk)
		if err != nil {
			fmt.Printf("Failed to encode chunk: %v\n", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
		flusher.Flush()
	}

	// 1. Initial chunk with role
	chunk1 := openai.ChatCompletionChunk{
		ID:      response.ID,
		Object:  "chat.completion.chunk",
		Created: response.Created,
		Model:   response.Model,
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: openai.ChatCompletionChunkChoiceDelta{
					Role: string(response.Choices[0].Message.Role),
				},
			},
		},
	}
	sendChunk(chunk1)

	// 2. Content chunk
	chunk2 := openai.ChatCompletionChunk{
		ID:      response.ID,
		Object:  "chat.completion.chunk",
		Created: response.Created,
		Model:   response.Model,
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: openai.ChatCompletionChunkChoiceDelta{
					Content: response.Choices[0].Message.Content,
				},
			},
		},
	}
	sendChunk(chunk2)

	// 2.5 ToolCalls chunk
	if len(response.Choices[0].Message.ToolCalls) > 0 {
		var toolCallDeltas []openai.ChatCompletionChunkChoiceDeltaToolCall
		for i, tc := range response.Choices[0].Message.ToolCalls {
			toolCallDeltas = append(toolCallDeltas, openai.ChatCompletionChunkChoiceDeltaToolCall{
				Index: int64(i),
				ID:    tc.ID,
				Type:  tc.Type,
				Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}

		chunkTool := openai.ChatCompletionChunk{
			ID:      response.ID,
			Object:  "chat.completion.chunk",
			Created: response.Created,
			Model:   response.Model,
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openai.ChatCompletionChunkChoiceDelta{
						ToolCalls: toolCallDeltas,
					},
				},
			},
		}
		sendChunk(chunkTool)
	}

	// 3. Finish reason chunk
	chunk3 := openai.ChatCompletionChunk{
		ID:      response.ID,
		Object:  "chat.completion.chunk",
		Created: response.Created,
		Model:   response.Model,
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index:        0,
				Delta:        openai.ChatCompletionChunkChoiceDelta{},
				FinishReason: response.Choices[0].FinishReason,
			},
		},
	}
	sendChunk(chunk3)

	// 4. DONE
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// HandleResponses processes an OpenAI Responses API request
func (p *OpenAIProvider) HandleResponses(w http.ResponseWriter, r *http.Request) {
	// Parse request body to check for stream
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check for stream: true in raw JSON
	// ResponseNewParams type also does not contain streaming field like ChatCompletionNewParams
	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Parse the incoming request into SDK type
	var requestBody responses.ResponseNewParams
	if err := json.NewDecoder(bytes.NewBuffer(bodyBytes)).Decode(&requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Find a matching mock
	mock := p.findMatchingResponseMock(requestBody)
	if mock == nil {
		requestBodyBytes, err := json.MarshalIndent(requestBody, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode request body: %v", err),
				http.StatusInternalServerError)
			return
		}

		http.Error(w, fmt.Sprintf("No matching mock found. Request: %s",
			string(requestBodyBytes)), http.StatusNotFound)
		return
	}

	// Return the response
	isStream := false
	if streamVal, ok := rawMap["stream"].(bool); ok {
		isStream = streamVal
	}

	if isStream {
		p.handleResponsesStreamingResponse(w, mock.Response)
	} else {
		p.handleNonStreamingResponse(w, mock.Response)
	}
}

// findMatchingResponseMock finds the first mock that matches the Responses API request
func (p *OpenAIProvider) findMatchingResponseMock(request responses.ResponseNewParams) *OpenAIResponseMock {
	for _, mock := range p.responseMocks {
		if p.responseRequestsMatch(mock.Match, request) {
			return &mock
		}
	}
	return nil
}

// responseRequestsMatch checks if two Responses API requests are equivalent
func (p *OpenAIProvider) responseRequestsMatch(expected OpenAIResponseRequestMatch, actual responses.ResponseNewParams) bool {
	switch expected.MatchType {
	case MatchTypeExact:
		// Check if Input fields match exactly using JSON comparison
		jsonExpected, err := json.Marshal(expected.Input)
		if err != nil {
			return false
		}
		jsonActual, err := json.Marshal(actual.Input)
		if err != nil {
			return false
		}
		return bytes.Equal(jsonExpected, jsonActual)
	case MatchTypeContains:
		// Input is a union type, either OfString or OfInputItemList will be set
		// Check if expected is OfString
		if expected.Input.OfString.Valid() {
			// Expected is a string, so actual must also be a string
			if !actual.Input.OfString.Valid() {
				return false
			}
			return strings.Contains(actual.Input.OfString.Value, expected.Input.OfString.Value)
		}

		// Check if expected is OfInputItemList and non empty
		expectedBytes, err := json.Marshal(expected.Input.OfInputItemList)
		if err == nil && len(expectedBytes) > 0 && string(expectedBytes) != "null" && string(expectedBytes) != "{}" {
			actualBytes, err := json.Marshal(actual.Input.OfInputItemList)
			if err != nil || len(actualBytes) == 0 || string(actualBytes) == "null" || string(actualBytes) == "{}" {
				return false
			}
			// For contains matching on item lists, check if the actual JSON contains the expected JSON
			// Ideally we would use a deep equal comparison, but ResponseInputParam is another union type so it will be complex
			return strings.Contains(string(actualBytes), string(expectedBytes))
		}

		// Neither field is set in expected, cannot match
		return false
	default:
		return false
	}
}

// handleResponsesStreamingResponse sends a streaming SSE response for Responses API
func (p *OpenAIProvider) handleResponsesStreamingResponse(w http.ResponseWriter, response responses.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported by server", http.StatusInternalServerError)
		return
	}

	// Helper to send a chunk
	sendChunk := func(chunk map[string]interface{}) {
		jsonBytes, err := json.Marshal(chunk)
		if err != nil {
			fmt.Printf("Failed to encode chunk: %v\n", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
		flusher.Flush()
	}

	// Send response.created event
	chunk := map[string]interface{}{
		"type": "response.created",
	}
	sendChunk(chunk)

	for _, outputItem := range response.Output {
		switch outputItem.Type {
		case "message":
			message := outputItem.AsMessage()

			for _, contentItem := range message.Content {
				if contentItem.Type == "output_text" && contentItem.Text != "" {
					text := contentItem.Text
					// Send delta chunks of 10 characters at a time
					chunkSize := 10
					for i := 0; i < len(text); i += chunkSize {
						end := i + chunkSize
						if end > len(text) {
							end = len(text)
						}
						delta := text[i:end]

						chunk := map[string]interface{}{
							"type":  "response.output_text.delta",
							"delta": delta,
						}
						sendChunk(chunk)
					}

					// Send done event with complete text
					chunk := map[string]interface{}{
						"type": "response.output_text.done",
						"json": map[string]interface{}{
							"text": text,
						},
						"text": text,
					}
					sendChunk(chunk)
				}
			}

		case "function_call":
			funcCall := outputItem.AsFunctionCall()
			// Send function call arguments delta
			if funcCall.Arguments != "" {
				chunk := map[string]interface{}{
					"type":      "response.function_call_arguments.delta",
					"name":      funcCall.Name,
					"arguments": funcCall.Arguments,
				}
				sendChunk(chunk)
			}

			// Send function call arguments done
			chunk := map[string]interface{}{
				"type":      "response.function_call_arguments.done",
				"name":      funcCall.Name,
				"arguments": funcCall.Arguments,
				"call_id":   funcCall.CallID,
			}
			sendChunk(chunk)

		case "function_call_output":
			callID := outputItem.CallID
			output := ""

			// The Output field is a union type, try to extract string value
			if outputItem.Output.OfString != "" {
				output = outputItem.Output.OfString
			} else {
				// If it's not a string, marshal it to JSON
				// These are meant for complex OpenAI built-in tools, unlikely to be used in mocks
				if outputBytes, err := json.Marshal(outputItem.Output); err == nil {
					output = string(outputBytes)
				}
			}

			if output != "" {
				// Send function call output delta
				chunk := map[string]interface{}{
					"type":    "response.function_call_output.delta",
					"call_id": callID,
					"output":  output,
				}
				sendChunk(chunk)

				// Send function call output done
				chunk = map[string]interface{}{
					"type":    "response.function_call_output.done",
					"call_id": callID,
					"output":  output,
				}
				sendChunk(chunk)
			}
		}
	}

	// Send response.completed event
	chunk = map[string]interface{}{
		"type":     "response.completed",
		"response": response,
	}
	sendChunk(chunk)

	// Send DONE
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
