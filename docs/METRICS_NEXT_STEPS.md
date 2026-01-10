# Metrics Next Steps (scratch)

- Add tenant hashing middleware to avoid high-cardinality labels with raw IDs.
- Decide on export for dashboards (Prometheus vs OTLP only); wire a small starter dashboard:
  - Rate limit outcomes (allowed/denied/fail_open), Redis latency/errors.
  - Provider HTTP latency/error and TTFT/stream duration.
  - Loop detection counts and sidecar Redis latency.
  - Runtime gauges (goroutines, async queue depth) for saturation signals.
- Consider adding provider-specific status buckets (e.g., 2xx/4xx/5xx) if needed for alerts.
- Add unit/integration tests to assert metric hooks aren’t nil when OTEL is enabled.

