# Mock LLM Server

A simple mock LLM server for end-to-end testing. Provides request/response mocking for OpenAI and Anthropic APIs using their official SDK types.

## Features

- OpenAI Chat Completions API (streaming and non-streaming)
- OpenAI Responses API (streaming and non-streaming, including function outputs)
- Anthropic Messages API (non-streaming)
- Exact and contains matching
- Tool/function calls support
- JSON configuration files

## Architecture

- **Server**: HTTP server with Gorilla mux router
- **Providers**: Separate handlers for OpenAI and Anthropic
- **Matching**: Linear search through mocks with exact/contains matching
- **SDK Integration**: Uses official OpenAI and Anthropic SDK types directly

## API Coverage

### OpenAI Chat Completions

- **Endpoint**: `POST /v1/chat/completions`
- **Request**: `openai.ChatCompletionNewParams`
- **Response**: `openai.ChatCompletion` (streaming: `openai.ChatCompletionChunk`)
- **Matching**: Exact or contains on last message

### OpenAI Responses API

- **Endpoint**: `POST /v1/responses`
- **Request**: `responses.ResponseNewParams`
- **Response**: `responses.Response`
- **Matching**: Exact or contains on input field
- **Features**: Supports text output and function call outputs

### Anthropic Messages API

- **Endpoint**: `POST /v1/messages`
- **Request**: `anthropic.MessageNewParams`
- **Response**: `anthropic.Message`
- **Matching**: Exact or contains on last message

## Configuration

### Go Structs

```go
config := mockllm.Config{
    OpenAI: []mockllm.OpenAIMock{
        {
            Name: "simple-response",
            Match: mockllm.OpenAIRequestMatch{
                MatchType: mockllm.MatchTypeExact,
                Message: /* openai.ChatCompletionMessageParamUnion */,
            },
            Response: /* openai.ChatCompletion */,
        },
    },
    OpenAIResponse: []mockllm.OpenAIResponseMock{
        {
            Name: "haiku-response",
            Match: mockllm.OpenAIResponseRequestMatch{
                MatchType: mockllm.MatchTypeContains,
                Input: /* responses.ResponseNewParamsInputUnion */,
            },
            Response: /* responses.Response */,
        },
    },
    Anthropic: []mockllm.AnthropicMock{/* ... */},
}
```

### JSON Files

```json
{
  "openai": [
    {
      "name": "simple-response",
      "match": {
        "match_type": "exact",
        "message": {
          "role": "user",
          "content": "Hello"
        }
      },
      "response": {
        "id": "chatcmpl-123",
        "object": "chat.completion",
        "model": "gpt-4o-mini",
        "choices": [
          /* ... */
        ]
      }
    }
  ],
  "openai_response": [
    /* ... */
  ],
  "anthropic": [
    /* ... */
  ]
}
```

## Matching

- **Exact**: JSON comparison of the last message/input
- **Contains**: String contains check on message content/input
- First matching mock wins
- Returns 404 if no match found

## Response Types

- **Non-streaming**: JSON responses using SDK types
- **Streaming**: Server-Sent Events (SSE) for Chat Completions and Responses API
- Uses official SDK response types directly

## Usage

```go
config := mockllm.Config{/* mocks */}
server := mockllm.NewServer(config)
baseURL, err := server.Start(context.Background())
defer server.Stop(context.Background())

// Use baseURL for API calls in tests
client := openai.NewClient(
    option.WithBaseURL(baseURL+"/v1/"),
    option.WithAPIKey("test-key"),
)
```

## Project Structure

- `server.go` — HTTP server, routing, lifecycle
- `types.go` — Configuration types
- `openai.go` — OpenAI handler (Chat Completions + Responses)
- `anthropic.go` — Anthropic handler
- `server_test.go` — Integration tests
- `testdata/` — Test fixtures

## Dependencies

- `github.com/openai/openai-go/v3`
- `github.com/anthropics/anthropic-sdk-go`
- `github.com/gorilla/mux`

## Limitations

- Simple matching only (exact/contains on last message/input)
- **Does not mock hosted tools (e.g. OpenAI file search, code execution) calls, reasoning, and MCP calls**
- No stateful conversation tracking
- No latency simulation
- No error injection
