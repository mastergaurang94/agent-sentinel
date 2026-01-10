# Metrics Notes (Proxy + Sidecar)

Quick reference for dashboarding and alert wiring.

## Proxy
- `ratelimit.requests` (counter): result=allowed|denied|fail_open, reason=over_limit|redis_error|ok, provider, model, tenant.id
- `ratelimit.redis.latency_ms` (histogram): op=check_limit|adjust_cost|refund_estimate, result=ok|error, backend, tenant.id
- `ratelimit.redis.errors` (counter): op, backend, tenant.id
- `ratelimit.estimate.latency_ms` (histogram): provider, model, tenant.id
- `ratelimit.cost.delta_usd` (histogram): provider, model, tenant.id
- `ratelimit.cost.refunds` (counter): reason=error_no_usage|stream_error|proxy_error, provider, model, tenant.id
- `proxy.ttft_ms` (histogram): provider, model, tenant.id
- `proxy.stream.duration_ms` (histogram): provider, model, tenant.id
- `proxy.provider_http.latency_ms` (histogram): provider, model, http.status_code, result=ok|error
- `proxy.provider_http.errors` (counter): provider, model, http.status_code, result=error
- `proxy.runtime.goroutines` (gauge)
- `proxy.async.queue_depth` (gauge)

## Embedding Sidecar
- `sidecar.embedder.latency_ms` (histogram): embedder.dim, embedder.output_name, result=ok|error
- `sidecar.embedder.errors` (counter)
- `sidecar.redis.latency_ms` (histogram): op=ensure_index|store_embedding|search_embeddings, result=ok|error, tenant.id
- `sidecar.redis.errors` (counter): op, tenant.id
- `sidecar.loop_check.requests` (counter): result=detected|not_detected|error, tenant.id

## Notes
- Tenant hashing middleware is still on backlog; tenant.id currently passes through as-is.
- OTLP export only; Prometheus export and dashboards remain to be added.

