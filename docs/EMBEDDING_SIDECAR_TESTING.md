# Embedding Sidecar Testing

Integration tests rely on Redis Stack (VSS) and the UDS-based gRPC sidecar. The tests are designed to skip automatically if Redis Stack is not reachable.

## Prerequisites

- Docker (for Redis Stack)
- Go 1.24+
- ONNX model present at `embedding-sidecar/models/all-MiniLM-L6-v2.onnx` (run `./embedding-sidecar/scripts/download_model.sh` with `MODEL_URL` and `MODEL_SHA256` set if you are not using the Docker image). Recommended:
  - `MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx`
  - `MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452`

## Bring up Redis Stack (test only)

```bash
docker run --rm -p 6380:6379 redis/redis-stack:latest
```

This exposes Redis Stack on `redis://localhost:6380`.

## Run integration tests

```bash
cd embedding-sidecar
REDIS_URL_INTEGRATION=redis://localhost:6380 go test ./...
```

Notes:
- If `REDIS_URL_INTEGRATION` is unset, tests default to `redis://localhost:6380`.
- Tests will skip if Redis Stack is unreachable or the VSS index cannot be created.

## Telemetry and health (optional)
- Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable tracing (OTLP gRPC).
- Spans cover embedder compute and Redis search/store; prompt content is not recorded.
- Health: gRPC health service is registered; for example:
  - `grpcurl -unix /sockets/embedding-sidecar.sock -plaintext grpc.health.v1.Health/Check`

## What’s covered

- Redis VSS store/search KNN flow (`store_integration_test.go`)
- gRPC UDS end-to-end CheckLoop (`grpc_integration_test.go`)
- Warmup behavior before serving (`warmup_test.go`)


