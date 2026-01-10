## Proxy Unit Test Plan (internal/ + ratelimit/)

- **Middleware / handlers**
  - Rate limit middleware: allow/deny/fail-open paths; headers set; redis error handling; default spend limit applied.
  - Loop detection middleware: no tenant header skips; successful call injects hint; sidecar error is fail-open; attributes recorded.
  - Proxy handler (LLM): cost adjust/refund paths; HasError detection; TTFT/stream duration metrics called; streaming error triggers refund.

- **Providers**
  - Request mappers: gemini/openai request shaping and hint mutation; ensure no leakage of provider-specific fields across providers.
  - Response parsing: token usage mapping; error detection; streamed chunk parsing (success/error).

- **Async / runtime**
  - Async queue depth: semaphore acquire/release behavior; QueueDepth reflects active goroutines.
  - Telemetry helpers: meter/tracer init no-ops vs. real; counters/histograms invoked without panic.

- **Rate limit package**
  - Allow/deny decisions with Redis responses (mock client): spending against quotas; fail-open when Redis unavailable; cost estimate latency measured; refunds increment.
  - Pricing/token math: token counting accuracy, cost calculation (prompt+completion), model-specific pricing tables.

- **Stream**
  - Stream splitting and accumulation: TTFT emitted on first chunk; stream duration on completion; error chunk produces refund.

- **Config**
  - Env parsing defaults/overrides for proxy settings (redis URL, spend limit, headers).

- **Test strategy**
  - Use fakes/mocks for Redis and sidecar client to avoid network.
  - Table-driven tests for pricing/token usage and middleware decisions.
  - Prefer unit over integration; leave live provider/Redis checks to manual/integration flows.

