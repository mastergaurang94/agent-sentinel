# Loop Detection Next Steps (temporary)

Planned tasks to finish embedding sidecar integration and hardening:

1) Proxy middleware
   - Add loop-detection middleware calling the sidecar gRPC over UDS.
   - Fail-open on errors/timeouts (50 ms).
   - Use configured threshold/history; inject intervention hint when `loop_detected` is true.

2) Sidecar client wiring
   - Add client config/env (`LOOP_EMBEDDING_SIDECAR_UDS`, timeout).
   - Use generated proto stubs from the sidecar module.

3) Real embedding
   - Replace placeholder embedder with actual ONNX inference (tokenization + session run).
   - Keep warmup before serving; benchmark latency.

4) Observability
   - Add metrics/tracing around sidecar calls (latency, similarity score, warmup success, Redis VSS query duration).

5) CI/test hardening
   - Run sidecar integration tests with Redis Stack in CI; optionally cache model download.

6) Hardening
   - Add a gRPC health/readiness check for the sidecar.
   - Make embedding Redis port configurable if needed.

7) Docs
   - Add a short note in proxy docs about enabling loop detection (envs, UDS path, behavior on fail-open) once middleware is in place.

