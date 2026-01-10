# Spec Review Notes (Temporary)

## Rate Limiting vs Design
- Env flag `RATE_LIMIT_ENABLED` not implemented; rate limiting auto-disables only when `REDIS_URL` is absent or the client fails to init. `DEFAULT_SPEND_LIMIT` is honored.
- Tests from the design doc (allow/deny, fail-open, cost adjust/refund, streaming) are not implemented.
- Behavior matches design for minute buckets, LUA atomic ops, 429 + headers, pre-estimate + post-adjust/refund, and fail-open on Redis errors.

## Loop Detection / Embedding Sidecar vs Design
- Gemini intervention format differs: current `InjectHint` prepends a `parts[].text` in the first content, not `systemInstruction.parts` as outlined. OpenAI hint injection matches the intent.
- Default UDS path differs (`/sockets/embedding-sidecar.sock` in proxy code vs `/tmp/embedding-sidecar.sock` in the design); configurable via envs.
- Metrics/telemetry implemented (spec called this “Phase 3”), but performance targets (<30ms total, <10ms embed) are not enforced/validated.
- Tests from the design doc (Redis VSS, similarity conversion, end-to-end loop detection, performance/warmup assertions) are not implemented.

## General
- Tenant ID hashing for metrics/labels remains on backlog (currently raw tenant IDs in labels).
- No dashboard/export wiring yet (OTLP only).

