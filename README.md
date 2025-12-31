# Agent Sentinel

A high-performance reverse proxy for LLM agents that forwards requests to Google's Gemini API.

## Features

- Reverse proxy that forwards all requests to `https://generativelanguage.googleapis.com`
- Extracts Gemini API key from environment variable
- Logs model and prompt information from request bodies
- Clean, idiomatic Go implementation

## Setup

1. Set your Gemini API key as an environment variable:
   ```bash
   export GEMINI_API_KEY=your-api-key-here
   ```

2. Run the proxy:
   ```bash
   go run main.go
   ```

The proxy will listen on port 8080 and forward all requests to Gemini's API.

## Usage

Point your Gemini API client to `http://localhost:8080` instead of `https://generativelanguage.googleapis.com`. The proxy will automatically add your API key to all requests.

## Development

```bash
# Build
go build -o agent-sentinel

# Run
./agent-sentinel
```

