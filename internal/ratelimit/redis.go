package ratelimit

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

// RedisClient wraps the Redis client with connection type detection
type RedisClient struct {
	client      redis.UniversalClient
	backendType string
}

// NewRedisClient creates a Redis client based on REDIS_URL environment variable
// Supports single instance, cluster, and sentinel configurations
// Returns nil if REDIS_URL is not set or connection fails (fail-open)
func NewRedisClient() *RedisClient {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		slog.Debug("REDIS_URL not set, rate limiting disabled")
		return nil
	}

	client, backend := parseRedisURL(redisURL)
	if client == nil {
		slog.Warn("Failed to create Redis client, rate limiting disabled",
			"redis_url", maskRedisURL(redisURL),
		)
		return nil
	}

	// Test connection
	if err := client.Ping(context.Background()).Err(); err != nil {
		slog.Warn("Redis connection test failed, rate limiting disabled",
			"error", err,
			"redis_url", maskRedisURL(redisURL),
		)
		return nil
	}

	slog.Info("Redis client connected successfully",
		"redis_url", maskRedisURL(redisURL),
	)

	return &RedisClient{client: client, backendType: backend}
}

// parseRedisURL parses the Redis URL and returns appropriate client and backend type.
func parseRedisURL(redisURL string) (redis.UniversalClient, string) {
	parsedURL, err := url.Parse(redisURL)
	if err != nil {
		slog.Error("Invalid Redis URL format",
			"error", err,
			"redis_url", maskRedisURL(redisURL),
		)
		return nil, ""
	}

	switch parsedURL.Scheme {
	case "redis", "rediss":
		// Single instance
		opt, err := redis.ParseURL(redisURL)
		if err != nil {
			slog.Error("Failed to parse Redis URL",
				"error", err,
				"redis_url", maskRedisURL(redisURL),
			)
			return nil, ""
		}
		return redis.NewClient(opt), "single"

	case "redis-cluster", "rediss-cluster":
		// Cluster mode - URL format: redis-cluster://node1:6379,node2:6379,node3:6379
		addrs := strings.Split(parsedURL.Host, ",")
		if len(addrs) == 0 {
			slog.Error("No cluster nodes specified in Redis URL")
			return nil, ""
		}

		// Parse password from URL if present
		password, _ := parsedURL.User.Password()

		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Password: password,
		}), "cluster"

	case "sentinel":
		// Sentinel mode - URL format: sentinel://localhost:26379?master=mymaster&password=xxx
		masterName := parsedURL.Query().Get("master")
		if masterName == "" {
			slog.Error("Sentinel master name not specified (use ?master=name)")
			return nil, ""
		}

		password, _ := parsedURL.User.Password()
		sentinelPassword := parsedURL.Query().Get("sentinel_password")

		return redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       masterName,
			SentinelAddrs:    []string{parsedURL.Host},
			Password:         password,
			SentinelPassword: sentinelPassword,
		}), "sentinel"

	default:
		slog.Error("Unsupported Redis URL scheme",
			"scheme", parsedURL.Scheme,
			"supported", []string{"redis", "rediss", "redis-cluster", "rediss-cluster", "sentinel"},
		)
		return nil, ""
	}
}

// maskRedisURL masks sensitive information in Redis URL for logging
func maskRedisURL(redisURL string) string {
	parsed, err := url.Parse(redisURL)
	if err != nil {
		return "***"
	}

	// Mask password if present
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.User(parsed.User.Username())
			return parsed.String() + ":***"
		}
	}

	return redisURL
}

// Client returns the underlying Redis client
func (r *RedisClient) Client() redis.UniversalClient {
	return r.client
}

// Backend returns the redis backend type (single, cluster, sentinel).
func (r *RedisClient) Backend() string {
	return r.backendType
}

// Close closes the Redis connection
func (r *RedisClient) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// IsAvailable returns true if Redis client is available and connected
func (r *RedisClient) IsAvailable() bool {
	if r.client == nil {
		return false
	}

	// Quick ping to check connection
	err := r.client.Ping(context.Background()).Err()
	return err == nil
}
