package mockllm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3/responses"
)

// OpenAIResponseProvider handles OpenAI Responses API request/response mocking
type OpenAIResponseProvider struct {
	mocks []OpenAIResponseMock
}

// NewOpenAIResponseProvider creates a new OpenAI Responses API provider with the given mocks
func NewOpenAIResponseProvider(mocks []OpenAIResponseMock) *OpenAIResponseProvider {
	return &OpenAIResponseProvider{
		mocks: mocks,
	}
}

// Handle processes an OpenAI Responses API request
func (p *OpenAIResponseProvider) Handle(w http.ResponseWriter, r *http.Request) {
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

	// Patch input items to add "type" field if missing
	// This works around upstream SDK bug: https://github.com/openai/openai-go/issues/465
	patchResponseInputType(rawMap)

	// Re-marshal the patched JSON
	patchedBytes, err := json.Marshal(rawMap)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal patched request: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse the patched request into SDK type
	var requestBody responses.ResponseNewParams
	if err := json.Unmarshal(patchedBytes, &requestBody); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Find a matching mock
	mock := p.findMatchingResponseMock(requestBody, r.Header)
	if mock == nil {
		http.Error(w, fmt.Sprintf("No matching mock found. Request: %s",
			string(bodyBytes)), http.StatusNotFound)
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

// patchResponseInputType adds "type": "message" to input items that are missing it
// This is necessary since without this type field, the SDK will not be able to unmarshal the input into the ResponseNewParams type
func patchResponseInputType(rawMap map[string]interface{}) {
	input, ok := rawMap["input"]
	if !ok {
		return
	}

	inputList, ok := input.([]interface{})
	if !ok {
		return
	}

	for _, item := range inputList {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		// Add type field if missing and item looks like a message (has role and content)
		if _, hasType := itemMap["type"]; !hasType {
			if _, hasRole := itemMap["role"]; hasRole {
				if _, hasContent := itemMap["content"]; hasContent {
					itemMap["type"] = "message"
				}
			}
		}
	}
}

// findMatchingResponseMock finds the first mock that matches the Responses API request
func (p *OpenAIResponseProvider) findMatchingResponseMock(request responses.ResponseNewParams, headers http.Header) *OpenAIResponseMock {
	for _, mock := range p.mocks {
		if p.responseRequestsMatch(mock.Match, request) && headersMatch(mock.Match.Headers, headers) {
			return &mock
		}
	}
	return nil
}

// responseRequestsMatch checks if two Responses API requests are equivalent
func (p *OpenAIResponseProvider) responseRequestsMatch(expected OpenAIResponseRequestMatch, actual responses.ResponseNewParams) bool {
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
			// Expected is a string, check if actual contains it
			if actual.Input.OfString.Valid() {
				// Both are strings, do simple contains check
				return strings.Contains(actual.Input.OfString.Value, expected.Input.OfString.Value)
			}

			// Some agent frameworks like OpenAI Agents SDK will format the user input as a list of messages to be compatible with Chat Completion API
			// Therefore we also need to match a string against the last message in the input (similar to chat completion matching)
			if len(actual.Input.OfInputItemList) > 0 {
				lastItem := actual.Input.OfInputItemList[len(actual.Input.OfInputItemList)-1]
				actualBytes, err := json.Marshal(lastItem)
				if err == nil {
					return strings.Contains(string(actualBytes), expected.Input.OfString.Value)
				}
			}

			return false
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

// handleNonStreamingResponse sends a JSON response
func (p *OpenAIResponseProvider) handleNonStreamingResponse(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

// handleResponsesStreamingResponse sends a streaming SSE response for Responses API
func (p *OpenAIResponseProvider) handleResponsesStreamingResponse(w http.ResponseWriter, response responses.Response) {
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
