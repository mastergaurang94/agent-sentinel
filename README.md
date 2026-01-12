# Agent Sentinel

Agent Sentinel is a high-performance reverse proxy for LLM agents. It sits in front of Gemini or OpenAI, enforcing spend limits, tracking cost, handling streaming-aware accounting, and detecting agentic loops via a dedicated embedding sidecar. OpenTelemetry tracing/metrics are wired in for both proxy and sidecar.

## What’s included
- Proxy: rate limiting (Redis), cost tracking/refunds, TTFT/stream duration metrics, goroutine/runtime gauges, provider HTTP tracing/metrics.
- Loop detection: gRPC over UDS to an embedding sidecar (ONNX MiniLM), Redis VSS store with HNSW, mean-pooled embeddings, configurable thresholds.
- Telemetry: OTLP tracing/metrics ready for the provided collector; default dashboard notes in `docs/METRICS_NOTES.md`.
- Docker: compose stack for proxy, Redis, Redis Stack (VSS), embedding sidecar, and OTel collector.

## Quick start
1) Environment (`.env`):
```
GEMINI_API_KEY=...
OPENAI_API_KEY=...
TARGET_API=gemini   # or "openai"
MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452
```

2) Bring up the stack:
```
docker compose up -d --build
```

3) Call the proxy (example):
```
curl -X POST http://localhost:8080/v1beta/models/gemini-pro-flash:generateContent \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: demo-tenant" \
  -d '{ "contents": [{ "parts": [{ "text": "Say hello in 3 languages" }] }] }'
```

## Testing
- Unit and integration tests:
```
go test ./...
```
- Proxy integration (stubbed sidecar/provider, requires Redis):
```
REDIS_URL_INTEGRATION=redis://localhost:6379 go test ./internal/integration -count=1
```
- Full-stack integration (real sidecar over UDS, stub provider):
```
RUN_FULLSTACK_SIDECAR=1 \
LOOP_EMBEDDING_SIDECAR_UDS=/Users/gaurangpatel/Documents/dev/agent-sentinel/.sockets/embedding-sidecar.sock \
REDIS_URL_INTEGRATION=redis://localhost:6379 \
go test ./internal/integration -count=1
```

## More docs
- Proxy usage, curl flows, and testing: `docs/PROXY_USAGE.md`
- Metrics reference: `docs/METRICS_NOTES.md`
- Embedding sidecar details: `embedding-sidecar/models/README.md`
