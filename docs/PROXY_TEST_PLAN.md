## Proxy Unit Test Plan (internal/ + ratelimit/) - Remaining

- **Rate limit package**
  - Allow/deny decisions with Redis responses (mock client): spending against quotas; fail-open when Redis unavailable; cost estimate latency measured; refunds increment.
  - Pricing/token math: token counting accuracy, cost calculation (prompt+completion), model-specific pricing tables.

- **Test strategy**
  - Use fakes/mocks for Redis to avoid network.
  - Table-driven tests for pricing/token usage and middleware decisions.
  - Prefer unit over integration; leave live Redis checks to integration flows.

