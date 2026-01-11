package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"agent-sentinel/internal/telemetry"

	"github.com/redis/go-redis/v9"
)

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

// RateLimiter handles rate limiting using Redis with minute buckets
type RateLimiter struct {
	client       *RedisClient
	pricing      ProviderPricing
	defaultLimit float64
}

var (
	defaultRunScript = func(ctx context.Context, script *redis.Script, client redis.UniversalClient, keys []string, args ...any) (any, error) {
		return script.Run(ctx, client, keys, args...).Result()
	}
	defaultRunScriptErr = func(ctx context.Context, script *redis.Script, client redis.UniversalClient, keys []string, args ...any) error {
		return script.Run(ctx, client, keys, args...).Err()
	}

	runScript    = defaultRunScript
	runScriptErr = defaultRunScriptErr
)

// NewRateLimiter creates a new rate limiter
// Returns nil if Redis is not available (fail-open)
func NewRateLimiter(redisClient *RedisClient) *RateLimiter {
	if redisClient == nil || !redisClient.IsAvailable() {
		return nil
	}

	// Get default limit from environment
	defaultLimit := 100.00 // $100/hour default
	if limitStr := os.Getenv("DEFAULT_SPEND_LIMIT"); limitStr != "" {
		if limit, err := strconv.ParseFloat(limitStr, 64); err == nil {
			defaultLimit = limit
		}
	}

	return &RateLimiter{
		client:       redisClient,
		pricing:      GetPricing(),
		defaultLimit: defaultLimit,
	}
}

// CheckLimitResult contains the result of a limit check
type CheckLimitResult struct {
	Allowed      bool
	CurrentSpend float64
	Limit        float64
	Remaining    float64
}

// checkLimitAndIncrementLUA is the LUA script for atomic check and increment
const checkLimitAndIncrementLUA = `
local spendKey = KEYS[1]
local limitKey = KEYS[2]
local estimatedCost = tonumber(ARGV[1])
local defaultLimit = tonumber(ARGV[2])

-- Get current time from Redis (prevents server time skew)
local redisTime = redis.call('TIME')
local now = tonumber(redisTime[1])
local minuteBucket = math.floor(now / 60) * 60
local oneHourAgo = minuteBucket - 3600

-- Get tenant limit (from Redis or use default)
local limit = defaultLimit
local limitStr = redis.call('GET', limitKey)
if limitStr then
  limit = tonumber(limitStr)
end

-- Get current spend (sum all minute buckets from last hour)
local allBuckets = redis.call('HGETALL', spendKey)
local currentSpend = 0

for i = 1, #allBuckets, 2 do
  local bucketTime = tonumber(allBuckets[i])
  if bucketTime and bucketTime >= oneHourAgo then
    currentSpend = currentSpend + tonumber(allBuckets[i + 1])
  end
end

-- Check if adding estimated cost would exceed limit
local newSpend = currentSpend + estimatedCost
local allowed = newSpend <= limit
local remaining = math.max(0, limit - currentSpend)

if allowed then
  redis.call('HINCRBYFLOAT', spendKey, tostring(minuteBucket), estimatedCost)
  redis.call('EXPIRE', spendKey, 7200)
end

-- Cleanup old buckets (older than 1 hour)
for i = 1, #allBuckets, 2 do
  local bucketTime = tonumber(allBuckets[i])
  if bucketTime and bucketTime < oneHourAgo then
    redis.call('HDEL', spendKey, allBuckets[i])
  end
end

return {allowed and 1 or 0, tostring(currentSpend), tostring(limit), tostring(remaining)}
`

// adjustCostLUA is the LUA script for atomic cost adjustment
// Handles both cost adjustment (actual - estimate) and refunds (when actual is 0)
const adjustCostLUA = `
local spendKey = KEYS[1]
local estimate = tonumber(ARGV[1]) or 0
local actual = tonumber(ARGV[2]) or 0

-- Get current time from Redis (prevents server time skew)
local redisTime = redis.call('TIME')
local now = tonumber(redisTime[1])
local minuteBucket = math.floor(now / 60) * 60

-- If actual is 0, it becomes (0 - Estimate), which is a refund
local adjustment = actual - estimate

if adjustment ~= 0 then
  redis.call('HINCRBYFLOAT', spendKey, tostring(minuteBucket), adjustment)
  redis.call('EXPIRE', spendKey, 7200)
end

return 1
`

// CheckLimitAndIncrement atomically checks if the request is allowed and increments the bucket
// Returns the result with current spend, limit, and remaining budget
func (r *RateLimiter) CheckLimitAndIncrement(ctx context.Context, tenantID string, estimatedCost float64) (*CheckLimitResult, error) {
	if r == nil || r.client == nil {
		// Fail-open: if rate limiter not available, allow request
		return &CheckLimitResult{
			Allowed:      true,
			CurrentSpend: 0,
			Limit:        r.defaultLimit,
			Remaining:    r.defaultLimit,
		}, nil
	}

	spendKey := fmt.Sprintf("spend:%s", tenantID)
	limitKey := fmt.Sprintf("limit:%s", tenantID)

	client := r.client.Client()
	script := redis.NewScript(checkLimitAndIncrementLUA)
	start := time.Now()
	result, err := runScript(ctx, script, client, []string{spendKey, limitKey},
		estimatedCost, r.defaultLimit)

	if err != nil {
		telemetry.ObserveRedisLatency(ctx, "check_limit", r.client.Backend(), "error", time.Since(start), tenantID)
		telemetry.IncRedisError(ctx, "check_limit", r.client.Backend(), tenantID)
		slog.Warn("Redis error in CheckLimitAndIncrement, failing open",
			"error", err,
			"tenant_id", tenantID,
		)
		// Fail-open: allow request on error
		return &CheckLimitResult{
			Allowed:      true,
			CurrentSpend: 0,
			Limit:        r.defaultLimit,
			Remaining:    r.defaultLimit,
		}, nil
	}

	telemetry.ObserveRedisLatency(ctx, "check_limit", r.client.Backend(), "ok", time.Since(start), tenantID)

	// Parse result from LUA script
	results := result.([]any)
	allowed := results[0].(int64) == 1
	currentSpend := toFloat64(results[1])
	limit := toFloat64(results[2])
	remaining := toFloat64(results[3])

	return &CheckLimitResult{
		Allowed:      allowed,
		CurrentSpend: currentSpend,
		Limit:        limit,
		Remaining:    remaining,
	}, nil
}

// AdjustCost atomically adjusts the cost: subtracts estimate and adds actual
func (r *RateLimiter) AdjustCost(ctx context.Context, tenantID string, estimate, actual float64) error {
	if r == nil || r.client == nil {
		// Fail-open: silently ignore if rate limiter not available
		return nil
	}

	spendKey := fmt.Sprintf("spend:%s", tenantID)

	client := r.client.Client()
	script := redis.NewScript(adjustCostLUA)
	start := time.Now()

	err := runScriptErr(ctx, script, client, []string{spendKey},
		estimate, actual)

	if err != nil {
		telemetry.ObserveRedisLatency(ctx, "adjust_cost", r.client.Backend(), "error", time.Since(start), tenantID)
		telemetry.IncRedisError(ctx, "adjust_cost", r.client.Backend(), tenantID)
		slog.Warn("Redis error in AdjustCost",
			"error", err,
			"tenant_id", tenantID,
		)
		// Fail-open: log but don't fail
		return nil
	}

	telemetry.ObserveRedisLatency(ctx, "adjust_cost", r.client.Backend(), "ok", time.Since(start), tenantID)
	return nil
}

// RefundEstimate atomically refunds the estimate (subtracts it from bucket)
func (r *RateLimiter) RefundEstimate(ctx context.Context, tenantID string, estimate float64) error {
	if r == nil || r.client == nil {
		// Fail-open: silently ignore if rate limiter not available
		return nil
	}

	spendKey := fmt.Sprintf("spend:%s", tenantID)

	client := r.client.Client()
	script := redis.NewScript(adjustCostLUA)

	// Pass actual=0 to trigger refund logic (0 - estimate = -estimate)
	start := time.Now()
	err := runScriptErr(ctx, script, client, []string{spendKey},
		estimate, 0.0)

	if err != nil {
		telemetry.ObserveRedisLatency(ctx, "refund_estimate", r.client.Backend(), "error", time.Since(start), tenantID)
		telemetry.IncRedisError(ctx, "refund_estimate", r.client.Backend(), tenantID)
		slog.Warn("Redis error in RefundEstimate",
			"error", err,
			"tenant_id", tenantID,
		)
		// Fail-open: log but don't fail
		return nil
	}

	telemetry.ObserveRedisLatency(ctx, "refund_estimate", r.client.Backend(), "ok", time.Since(start), tenantID)
	return nil
}

// GetSpend returns the current spend for a tenant in the last hour
func (r *RateLimiter) GetSpend(ctx context.Context, tenantID string) (float64, error) {
	if r == nil || r.client == nil {
		return 0, nil
	}

	spendKey := fmt.Sprintf("spend:%s", tenantID)
	client := r.client.Client()

	redisTime, err := client.Time(ctx).Result()
	if err != nil {
		return 0, err
	}
	now := redisTime.Unix()
	oneHourAgo := (now/60)*60 - 3600

	allBuckets, err := client.HGetAll(ctx, spendKey).Result()
	if err != nil {
		return 0, err
	}

	var totalSpend float64
	for bucketTimeStr, costStr := range allBuckets {
		bucketTime, err := strconv.ParseInt(bucketTimeStr, 10, 64)
		if err != nil {
			continue
		}

		if bucketTime >= oneHourAgo {
			cost, err := strconv.ParseFloat(costStr, 64)
			if err == nil {
				totalSpend += cost
			}
		}
	}

	return totalSpend, nil
}

// GetLimit returns the limit for a tenant (from Redis or default)
func (r *RateLimiter) GetLimit(ctx context.Context, tenantID string) (float64, error) {
	if r == nil || r.client == nil {
		return r.defaultLimit, nil
	}

	limitKey := fmt.Sprintf("limit:%s", tenantID)
	client := r.client.Client()

	limitStr, err := client.Get(ctx, limitKey).Result()
	if err == redis.Nil {
		// No custom limit set, use default
		return r.defaultLimit, nil
	}
	if err != nil {
		return r.defaultLimit, err
	}

	limit, err := strconv.ParseFloat(limitStr, 64)
	if err != nil {
		return r.defaultLimit, err
	}

	return limit, nil
}

// GetPricing returns the pricing for a specific provider and model
func (r *RateLimiter) GetPricing(provider, model string) (Pricing, bool) {
	if r == nil {
		return Pricing{}, false
	}

	providerPricing, ok := r.pricing[provider]
	if !ok {
		return Pricing{}, false
	}

	pricing, ok := providerPricing[model]
	return pricing, ok
}
