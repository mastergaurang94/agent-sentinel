# Agent Sentinel

A high-performance reverse proxy for LLM agents that forwards requests to Google's Gemini API or OpenAI.

## Features

- Reverse proxy for Gemini API and OpenAI API
- Automatic API key injection
- Request logging (model and prompt)
- Supports multiple request formats

## Setup

1. Set your API key as an environment variable or in a `.env` file:
   ```bash
   # For Gemini
   export GEMINI_API_KEY=your-api-key-here
   
   # For OpenAI
   export OPENAI_API_KEY=your-api-key-here
   ```

2. (Optional) Explicitly set the target API:
   ```bash
   export TARGET_API=gemini  # or "openai"
   ```

3. Run the proxy:
   ```bash
   go run main.go
   ```

The proxy listens on port 8080 and automatically detects which API to use based on available keys.

## Usage

Point your API client to `http://localhost:8080` instead of the original API endpoint. The proxy automatically adds your API key to all requests.

## Development

```bash
# Build
go build -o agent-sentinel

# Run
./agent-sentinel
```

