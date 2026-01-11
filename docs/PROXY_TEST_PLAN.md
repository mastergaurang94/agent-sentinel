## Proxy Unit Test Plan (internal/ + ratelimit/)

- **Handlers**
  - Proxy handler (LLM): cost adjust/refund paths; HasError detection; streaming error triggers refund.

- **Rate limit package**
  - Allow/deny decisions with Redis responses (mock client): spending against quotas; fail-open when Redis unavailable; cost estimate latency measured; refunds increment.
  - Pricing/token math: token counting accuracy, cost calculation (prompt+completion), model-specific pricing tables.

- **Config**
  - Env parsing defaults/overrides for proxy settings (redis URL, spend limit, headers).

- **Test strategy**
  - Use fakes/mocks for Redis and sidecar client to avoid network.
  - Table-driven tests for pricing/token usage and middleware decisions.
  - Prefer unit over integration; leave live provider/Redis checks to manual/integration flows.

