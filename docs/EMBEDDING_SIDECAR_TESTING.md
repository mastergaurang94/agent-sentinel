# Embedding Sidecar Testing

Run the embedding sidecar integration tests against Redis Stack VSS. Tests auto-skip if Redis Stack is unavailable.

## Prerequisites
- Docker (for Redis Stack)
- Go 1.24+
- ONNX model at `embedding-sidecar/models/all-MiniLM-L6-v2.onnx`  
  - Use `./embedding-sidecar/scripts/download_model.sh` with `MODEL_URL` and `MODEL_SHA256` when not using the Docker image. Recommended:
    - `MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx`
    - `MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452`

## Bring up Redis Stack (test only)
```bash
docker run --rm -p 6380:6379 redis/redis-stack:latest
```
Redis Stack is available at `redis://localhost:6380`.

## Run integration tests
```bash
cd embedding-sidecar
REDIS_URL_INTEGRATION=redis://localhost:6380 go test ./...
```
Notes:
- If `REDIS_URL_INTEGRATION` is unset, tests default to `redis://localhost:6380`.
- Tests skip if Redis Stack is unreachable or the VSS index cannot be created.

## Telemetry and health (optional)
- `OTEL_EXPORTER_OTLP_ENDPOINT` enables OTLP gRPC tracing (embedder compute + Redis operations; prompt text is not recorded).
- gRPC health is exposed, e.g.:
  - `grpcurl -unix /sockets/embedding-sidecar.sock -plaintext grpc.health.v1.Health/Check`


