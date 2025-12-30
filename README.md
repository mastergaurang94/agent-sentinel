# Agent Sentinel

A high-performance reverse proxy for LLM agents that forwards requests to OpenAI's API.

## Features

- Reverse proxy that forwards all requests to `https://api.openai.com`
- Extracts OpenAI API key from environment variable
- Logs model and prompt information from request bodies
- Clean, idiomatic Go implementation

## Setup

1. Set your OpenAI API key as an environment variable:
   ```bash
   export OPENAI_API_KEY=your-api-key-here
   ```

2. Run the proxy:
   ```bash
   go run main.go
   ```

The proxy will listen on port 8080 and forward all requests to OpenAI's API.

## Usage

Point your OpenAI API client to `http://localhost:8080` instead of `https://api.openai.com`. The proxy will automatically add your API key to all requests.

## Development

```bash
# Build
go build -o agent-sentinel

# Run
./agent-sentinel
```

