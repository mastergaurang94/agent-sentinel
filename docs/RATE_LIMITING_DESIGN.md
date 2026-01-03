# Rate Limiting Design - Agent Sentinel

## Overview
Implement Redis-based rate limiting to prevent tenant overspending by tracking total spend per tenant in a sliding 1-hour window using minute buckets.

## Algorithm: Sliding Window with Minute Buckets

**Approach**: Group requests into 1-minute buckets (60 buckets per hour)
- Track total spend per minute bucket, sum last 60 buckets for hourly spend
- Memory efficient (max 60 entries per tenant)
- Accurate enough for financial constraints
- Fast lookups (O(60) operations)
- No need to audit every single request

**Redis Structure**: Hash with keys like `minute:{timestamp}` where timestamp is rounded to minute

**Atomic Operations**: All Redis operations use LUA scripts to prevent race conditions
- Check limit and increment bucket atomically
- Prevents multiple concurrent requests from reading the same spend total
- Ensures accurate cost tracking under high concurrency

## Tenant Identification

Custom Header `X-Tenant-ID`
- Header name configurable via `RATE_LIMIT_HEADER` env var (default: "X-Tenant-ID")
- Clients must send header with tenant identifier

## Cost Estimation Strategy

Hybrid Approach (Pre-request estimate + Post-request actual tracking)

**Pre-request (Estimate)**:
- Count input tokens from request body using tiktoken
- Use model pricing to estimate cost
- Atomically check limit and add estimated cost to minute bucket
- Block if estimated cost would exceed limit (return 429)
- If blocked, do not add estimate to bucket (no cost incurred)
- Prevents overspending

**Post-request (Actual)**:
- Parse response to get actual input/output tokens
- Calculate actual cost from response
- Handle different scenarios:
  - **Success**: Atomically adjust bucket: subtract estimate, add actual cost
  - **Provider error (4xx/5xx)**: Check if tokens were consumed
    - If tokens consumed: Use actual cost (subtract estimate, add actual)
    - If no tokens consumed: Subtract estimate only (no charge)
  - **Network/timeout errors**: Subtract estimate only (no charge)
- Net effect: Bucket contains actual cost only when tokens were consumed

## Token Counting

**Approach**: Use tiktoken (Go port of OpenAI's tiktoken library)
- Count input tokens from request body text
- Works for both OpenAI and Gemini (close enough for estimation purposes)
- Avoids expensive API calls to CountToken endpoints
- Estimation accuracy is sufficient for pre-request rate limiting
- Actual token counts from API responses used for post-request correction

## Cost Calculation

**Pricing Requirements**:
- **OpenAI**: Per 1K tokens (input/output different rates)
- **Gemini**: Per 1M tokens (input/output different rates)
- **Per-model pricing**: Different models have different rates
- **Frequent updates**: Pricing may change often

**Approach**: 
- Store pricing in code/config structure (easy to update)
- Structure: `map[provider][model]Pricing{InputPrice, OutputPrice}`
- Calculate: `(input_tokens * input_price) + (output_tokens * output_price)`
- Make pricing updates simple (code changes or config file)
- Support both providers and all their models

## Redis Key Structure (Minute Buckets)

```
spend:{tenant_id} -> Hash
  - Key: minute timestamp (unix seconds rounded to minute)
  - Value: total cost in that minute (float as string)
  - Example: { "1704067200": "5.23", "1704067260": "3.45", ... }
  - Cleanup: Remove buckets older than 1 hour (max 60 buckets)
  - TTL: 2 hours on the hash key

limit:{tenant_id} -> String
  - Value: hourly spend limit (float as string, e.g., "100.00")
  - TTL: None (persistent until updated)
```

**Operations** (all atomic via LUA scripts):
1. Get current minute bucket: `floor(now() / 60) * 60`
2. Check limit and increment bucket atomically:
   - LUA script: Read current spend, check limit, increment if allowed
   - Returns: allowed (bool), current spend, limit, remaining
3. Adjust cost atomically (post-request):
   - LUA script: Subtract estimate, add actual cost
4. Get hourly spend: Sum all buckets in hash (max 60)
5. Cleanup: Remove buckets older than 1 hour using `HDEL`

## Configuration

**Environment Variables**:
- `REDIS_URL` - Redis connection string (supports single, cluster, sentinel)
  - Single: `redis://localhost:6379`
  - Cluster: `redis://node1:6379,redis://node2:6379,redis://node3:6379`
  - Sentinel: `sentinel://localhost:26379?master=mymaster`
- `RATE_LIMIT_ENABLED` - Enable/disable rate limiting (default: false if REDIS_URL not set)
- `DEFAULT_SPEND_LIMIT` - Default limit per tenant (e.g., "100.00" = $100/hour)
- `RATE_LIMIT_HEADER` - Header name for tenant ID (default: "X-Tenant-ID")

**Per-tenant Limits**:
- Store in Redis: `limit:{tenant_id}` -> limit amount (float as string)
- Fallback to `DEFAULT_SPEND_LIMIT` if not set
- Can be updated via Redis without code changes

**Pricing Configuration**:
- Configurable per model, per provider
- Supports different input/output token pricing
- Can be updated as APIs change pricing
- Stored in code/config (not Redis) for easy updates

## Reliability & Failover

**Fail-Open Strategy**: If Redis is unreachable, rate limiting middleware must fail-open
- Log error and allow request through
- Ensures 100% uptime for users
- Rate limiting is a best-effort feature, not a hard requirement
- Prevents Redis outages from blocking all traffic

## Implementation Plan

1. **Add Redis client** (github.com/redis/go-redis/v9)
   - Support single instance, cluster, and sentinel via URL parsing
   - Handle connection errors gracefully (fail-open, log error)

2. **Add tiktoken library** (github.com/tiktoken-go/tokenizer)
   - For counting input tokens from request body
   - Works for both OpenAI and Gemini (estimation purposes)

3. **Create rate limiter package** with:
   - `RateLimiter` struct (Redis client, pricing config)
   - LUA scripts for atomic operations:
     - `checkLimitAndIncrement(tenantID, estimatedCost, limit)` -> (allowed bool, currentSpend, remaining float64, err error)
       - Only increments if allowed is true
       - If not allowed, returns without incrementing (no charge for 429)
     - `adjustCost(tenantID, estimate, actual)` -> error (subtracts estimate, adds actual)
     - `refundEstimate(tenantID, estimate)` -> error (subtracts estimate only, for errors with no cost)
   - `GetSpend(tenantID)` -> (spend float64, err error) (sums last 60 buckets)
   - `GetLimit(tenantID)` -> (limit float64, err error) (checks Redis, falls back to default)
   - `CleanupOldBuckets(tenantID)` -> error (removes buckets older than 1 hour)

4. **Add rate limiting middleware** before logging middleware:
   - Extract tenant ID from header
   - Count input tokens using tiktoken
   - Estimate cost from request
   - Atomically check limit and increment bucket (LUA script)
   - Add rate limit headers to response (X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset)
   - Return 429 if exceeded
   - Fail-open if Redis error (log and continue)

5. **Add cost tracking** from response:
   - Parse response to get actual input/output tokens
   - Determine if tokens were consumed (check response status and usage fields)
   - Calculate actual cost if tokens consumed
   - Handle scenarios:
     - Success: Atomically adjust bucket (subtract estimate, add actual)
     - Provider error with tokens: Atomically adjust bucket (subtract estimate, add actual)
     - Provider error without tokens: Atomically refund estimate (subtract estimate only)
     - Network/timeout error: Atomically refund estimate (subtract estimate only)

6. **Add pricing configuration**:
   - Per-model, per-provider pricing
   - Easy to update as APIs change
   - Support both OpenAI and Gemini pricing models

## Implementation Notes

- Use minute buckets for efficient memory usage
- Support all Redis deployment types (single, cluster, sentinel)
- Track both estimated and actual costs
- Support per-tenant limits (stored in Redis, fallback to default)
- Make pricing easy to update (configurable in code)

