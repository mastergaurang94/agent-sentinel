package ratelimit

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestCheckLimitFailOpenWhenNilClient(t *testing.T) {
	rl := &RateLimiter{defaultLimit: 123}
	res, err := rl.CheckLimitAndIncrement(context.Background(), "t1", 1.5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Allowed || res.Limit != 123 || res.Remaining != 123 {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestCheckLimitAllowsOnScriptError(t *testing.T) {
	defer func() { runScript = defaultRunScript }()
	runScript = func(ctx context.Context, script *redis.Script, client redis.UniversalClient, keys []string, args ...any) (any, error) {
		return nil, errors.New("script fail")
	}
	rl := &RateLimiter{client: &RedisClient{}, defaultLimit: 50}
	res, err := rl.CheckLimitAndIncrement(context.Background(), "t1", 2)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Allowed || res.Limit != 50 {
		t.Fatalf("expected fail-open allow with default limit, got %+v", res)
	}
}

func TestCheckLimitParsesResult(t *testing.T) {
	defer func() { runScript = defaultRunScript }()
	runScript = func(ctx context.Context, script *redis.Script, client redis.UniversalClient, keys []string, args ...any) (any, error) {
		return []any{int64(1), "1.5", "10", "8.5"}, nil
	}
	rl := &RateLimiter{client: &RedisClient{}, defaultLimit: 10}
	res, err := rl.CheckLimitAndIncrement(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Allowed || res.CurrentSpend != 1.5 || res.Limit != 10 || res.Remaining != 8.5 {
		t.Fatalf("unexpected parsed result %+v", res)
	}
}

func TestAdjustCostFailOpenOnError(t *testing.T) {
	defer func() { runScriptErr = defaultRunScriptErr }()
	runScriptErr = func(ctx context.Context, script *redis.Script, client redis.UniversalClient, keys []string, args ...any) error {
		return errors.New("script fail")
	}
	rl := &RateLimiter{client: &RedisClient{}, defaultLimit: 10}
	if err := rl.AdjustCost(context.Background(), "t1", 1, 2); err != nil {
		t.Fatalf("expected nil on error, got %v", err)
	}
}

func TestRefundEstimateFailOpenOnError(t *testing.T) {
	defer func() { runScriptErr = defaultRunScriptErr }()
	runScriptErr = func(ctx context.Context, script *redis.Script, client redis.UniversalClient, keys []string, args ...any) error {
		return errors.New("script fail")
	}
	rl := &RateLimiter{client: &RedisClient{}, defaultLimit: 10}
	if err := rl.RefundEstimate(context.Background(), "t1", 1); err != nil {
		t.Fatalf("expected nil on error, got %v", err)
	}
}
