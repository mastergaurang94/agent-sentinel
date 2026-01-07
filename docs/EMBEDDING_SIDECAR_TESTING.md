# Embedding Sidecar Testing

Integration tests rely on Redis Stack (VSS) and the UDS-based gRPC sidecar. The tests are designed to skip automatically if Redis Stack is not reachable.

## Prerequisites

- Docker (for Redis Stack)
- Go 1.24+

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

## What’s covered

- Redis VSS store/search KNN flow (`store_integration_test.go`)
- gRPC UDS end-to-end CheckLoop (`grpc_integration_test.go`)
- Warmup behavior before serving (`warmup_test.go`)


