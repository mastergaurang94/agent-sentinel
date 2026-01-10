# Loop Detection Next Steps (temporary)

Remaining items:

1) CI/test hardening
   - Run sidecar integration tests with Redis Stack in CI; optionally cache model/vocab download.

2) Hardening
   - Proxy-side span/metrics for the sidecar call (fail-open tagging, loop_detected, similarity).
   - Make embedding Redis port configurable if needed.

