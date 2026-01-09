# Agent Sentinel

A high-performance reverse proxy for LLM agents that forwards requests to Google's Gemini API or OpenAI.

## Overview

Agent Sentinel is a reverse proxy for LLM agents with rate limiting, cost tracking, streaming-aware accounting, and loop-detection support (via the embedding sidecar).

## Quick start

1) Add API keys to `.env`:
```
GEMINI_API_KEY=...
OPENAI_API_KEY=...
TARGET_API=gemini   # or "openai"
```

2) For the embedding sidecar model (Docker build):
```
MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452
```

3) Run with Docker Compose:
```
docker compose up -d --build
```

4) Send traffic to the proxy:
```
POST http://localhost:8080/...
```

More detailed setup, curl examples, and testing notes are in `docs/PROXY_USAGE.md`. Embedding model specifics are in `embedding-sidecar/models/README.md`.

