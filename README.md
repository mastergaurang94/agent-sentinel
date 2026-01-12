# Agent Sentinel

Agent Sentinel is a high-performance reverse proxy designed to govern LLM agents. It provides financial governance by enforcing multi-tenant spend limits and detecting semantic loops. 

More specifically, it sits in front of Gemini or OpenAI, enforcing spend limits, tracking cost, handling streaming-aware accounting, and detecting agentic loops via a dedicated embedding sidecar. OpenTelemetry tracing/metrics are wired in for both proxy and sidecar.

## Key Architectural Decisions
- Decoupled Intelligence: Unlike monolithic proxies, Agent Sentinel offloads heavy vector inference to a gRPC sidecar communicating over Unix Domain Sockets (UDS). This prevents ONNX runtime overhead from competing with the proxy's networking stack.

- Semantic Guardrails: Traditional rate limiters fail when agents repeat logic with different words. We use Semantic Similarity (KNN) search via Redis VSS to detect behavioral loops and inject system hints to break the cycle.

- Streaming-Aware Accounting: Built for the modern LLM stack, handling TTFT (Time to First Token) metrics and automated refunds/adjustments for partial stream failures.

- Resiliency (Fail-Open): Implements a 350ms P99 latency budget for guardrails. If the sidecar or vector store times out, the system degrades gracefully to preserve agent availability while maintaining financial rails.

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
