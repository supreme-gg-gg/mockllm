# Mock LLM Server

A simple mock LLM server for end-to-end testing. Provides request/response mocking for OpenAI and Anthropic APIs using their official SDK types.

## Features

- OpenAI Chat Completions API (streaming and non-streaming)
- OpenAI Responses API (streaming and non-streaming)
- OpenAI Embeddings API
- Anthropic Messages API (non-streaming)
- Exact and contains matching
- Optional header matching (e.g., tenant ID, API key, custom headers)
- Tool/function calls support
- JSON configuration files

## Architecture

- **Server**: HTTP server with Gorilla mux router
- **Providers**: Separate handlers for OpenAI and Anthropic
- **Matching**: Linear search through mocks with exact/contains matching
- **SDK Integration**: Uses official OpenAI and Anthropic SDK types directly

## Key Types

Current implementation uses these core types:

### Configuration

- `Config`: Root configuration containing arrays of OpenAI and Anthropic mocks
- `OpenAIMock`, `OpenAIResponseMock`, and `OpenAIEmbeddingMock` Maps OpenAI requests to responses using official SDK types
- `AnthropicMock`: Maps Anthropic requests to responses using official SDK types

### Matching

- `MatchType`: Enum for matching strategies (`exact`, `contains`)
- `HeaderMatch`: Defines a header matching rule (name, value, match type)
- `OpenAIRequestMatch` and `OpenAIResponseRequestMatch`: Defines how to match OpenAI requests (match type + message + optional headers)
- `AnthropicRequestMatch`: Defines how to match Anthropic requests (match type + message + optional headers)

## API Coverage

### OpenAI Chat Completions

- **Endpoint**: `POST /v1/chat/completions`
- **Auth**: `Authorization: Bearer <token>` (presence check only)
- **Request**: `openai.ChatCompletionNewParams`
- **Response**: `openai.ChatCompletion` (streaming: `openai.ChatCompletionChunk`)
- **Matching**: Exact or contains on last message

### OpenAI Responses API

- **Endpoint**: `POST /v1/responses`
- **Auth**: `Authorization: Bearer <token>` (presence check only)
- **Request**: `responses.ResponseNewParams`
- **Response**: `responses.Response`
- **Matching**: Exact or contains on input field
- **Features**: Supports text output and function call outputs

### OpenAI Embeddings API

- **Endpoint**: `POST /v1/embeddings`
- **Auth**: `Authorization: Bearer <token>` (presence check only)
- **Request**: `openai.EmbeddingNewParams`
- **Response**: `openai.CreateEmbeddingResponse`
- **Matching**: Exact or contains on input field

### Anthropic Messages API

- **Endpoint**: `POST /v1/messages`
- **Auth**: `x-api-key` (presence check only)
- **Headers**: `anthropic-version` required
- **Request**: `anthropic.MessageNewParams`
- **Response**: `anthropic.Message`
- **Matching**: Exact or contains on last message

## Configuration

### Config Go Structs

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
    OpenAIEmbeddings: []mockllm.OpenAIEmbeddingMock{
        {
            Name: "embedding-response",
            Match: mockllm.OpenAIEmbeddingRequestMatch{
                MatchType: mockllm.MatchTypeExact,
                Input: /* openai.EmbeddingNewParamsInputUnion */,
            },
            Response: /* openai.CreateEmbeddingResponse */,
        },
    },
    Anthropic: []mockllm.AnthropicMock{/* ... */},
}
```

### Config JSON Files

```json
{
  "openai": [
    {
      "name": "initial_request",
      "match": {
        "match_type": "exact",
        "message" : {
          "content": "List all nodes in the cluster",
          "role": "user"
        }
      },
      "response": {
        "id": "chatcmpl-1",
        "object": "chat.completion",
        "created": 1677652288,
        "model": "gpt-4.1-mini",
        "choices": [
          {
            "index": 0,
            "role": "assistant",
            "message": {
              "content": "",
              "tool_calls": [
                ...
              ]
            },
            "finish_reason": "tool_calls"
          }
        ]
      }
    },
    {
      "name": "k8s_get_resources_response",
      "match": {
        "match_type": "contains",
        "message" : {
          "content": "kagent-control-plane",
          "role": "tool",
          "tool_call_id": "call_1"
        }
      },
      "response": {
        "id": "call_1",
        "object": "chat.completion.tool_message",
        "created": 1677652288,
        "model": "gpt-4.1-mini",
        "choices": [
          ...
        ]
      }
    }
  ],
  "openai_response": [
    /* ... */
  ],
  "openai_embeddings": [
    /* ... */
  ],
  "anthropic": [
    /* ... */
  ]
}
```

### Generating Embeddings

When configuring mocks, you need to specify the embedding vectors for the input strings. You can do so using the OpenAI embeddings API (with your API key) or use a local option like `ollama` (e.g. `ollama run embeddinggemma "Hello world"`). MockLLM works with embeddings of any dimension, so you can use any embedding model you like.

### Header Matching

Mocks can optionally require specific HTTP headers to match. When `headers` is specified, all header rules must match (AND semantics) in addition to the body match. Header matching is optional — mocks without `headers` continue to work identically.

#### Go Header Structs

```go
mock := mockllm.OpenAIMock{
    Name: "tenant-a-response",
    Match: mockllm.OpenAIRequestMatch{
        MatchType: mockllm.MatchTypeContains,
        Message:   /* ... */,
        Headers: []mockllm.HeaderMatch{
            {Name: "X-Tenant-ID", Value: "tenant-a", MatchType: mockllm.MatchTypeExact},
        },
    },
    Response: /* ... */,
}
```

#### JSON Header Objects

```json
{
  "name": "tenant-a-response",
  "match": {
    "match_type": "contains",
    "message": { "role": "user", "content": "Hello" },
    "headers": [
      { "name": "X-Tenant-ID", "value": "tenant-a", "match_type": "exact" },
      { "name": "Authorization", "value": "Bearer", "match_type": "contains" }
    ]
  },
  "response": {}
}
```

- `name`: Header name (case-insensitive, per HTTP spec)
- `value`: Value to match against
- `match_type`: `"exact"` (default if omitted) or `"contains"`

### Matching Algorithm

Simple linear search through mocks:

1. Parse incoming request into appropriate SDK type
2. Iterate through provider-specific mocks in order
3. For each mock, check if the match criteria are met:
   - **Body**: Exact JSON comparison or string contains check on last message/input
   - **Headers** (optional): All specified header rules must match
4. Return the response from the first matching mock
5. Return 404 if no match found

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
- `headers.go` — Shared header matching logic
- `openai.go` — OpenAI handler (Chat Completions)
- `openai_response.go` — OpenAI handler (Responses API)
- `openai_embeddings.go` — OpenAI handler (Embeddings API)
- `anthropic.go` — Anthropic handler
- `server_test.go` — Integration tests
- `testdata/` — Test fixtures

## Dependencies

- `github.com/openai/openai-go/v3`
- `github.com/anthropics/anthropic-sdk-go`
- `github.com/gorilla/mux`

## Limitations

- Simple matching only (exact/contains on last message/input and optional headers)
- **Does not mock hosted tools (e.g. OpenAI file search, code execution) calls, reasoning, and MCP calls**
- No stateful conversation tracking
- No latency simulation
- No error injection
