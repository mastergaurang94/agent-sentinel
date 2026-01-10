package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.UDSPath == "" || cfg.RedisURL == "" || cfg.EmbeddingModelPath == "" {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
	if cfg.EmbeddingTTL != time.Hour {
		t.Fatalf("expected default ttl 1h, got %v", cfg.EmbeddingTTL)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("UDS_PATH", "/tmp/x.sock")
	t.Setenv("REDIS_URL", "redis://r:1")
	t.Setenv("EMBEDDING_REDIS_URL", "redis://er:2")
	t.Setenv("LOOP_SIMILARITY_THRESHOLD", "0.5")
	t.Setenv("LOOP_HISTORY_SIZE", "9")
	t.Setenv("LOOP_EMBEDDING_TTL", "10")
	t.Setenv("LOOP_EMBEDDING_MODEL_PATH", "m.onnx")
	t.Setenv("LOOP_EMBEDDING_VOCAB_PATH", "vocab")
	t.Setenv("LOOP_EMBEDDING_DIM", "123")
	t.Setenv("LOOP_EMBEDDING_OUTPUT_NAME", "out")
	t.Setenv("LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS", "250")

	cfg := Load()

	if cfg.UDSPath != "/tmp/x.sock" ||
		cfg.RedisURL != "redis://r:1" ||
		cfg.EmbeddingRedisURL != "redis://er:2" ||
		cfg.SimilarityThreshold != 0.5 ||
		cfg.HistorySize != 9 ||
		cfg.EmbeddingTTL != 10*time.Second ||
		cfg.EmbeddingModelPath != "m.onnx" ||
		cfg.EmbeddingVocabPath != "vocab" ||
		cfg.EmbeddingDim != 123 ||
		cfg.EmbeddingOutputName != "out" ||
		cfg.GRPCTimeout != 250*time.Millisecond {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestGetEnvFallback(t *testing.T) {
	if val := getEnv("NOT_SET", "fallback"); val != "fallback" {
		t.Fatalf("getEnv fallback failed, got %s", val)
	}
	t.Setenv("INT_KEY", "not-int")
	if v := getEnvInt("INT_KEY", 5); v != 5 {
		t.Fatalf("expected int fallback, got %d", v)
	}
	t.Setenv("FLOAT_KEY", "not-float")
	if v := getEnvFloat("FLOAT_KEY", 1.5); v != 1.5 {
		t.Fatalf("expected float fallback, got %v", v)
	}
}
