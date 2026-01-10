package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	UDSPath             string
	RedisURL            string
	SimilarityThreshold float64
	HistorySize         int
	EmbeddingTTL        time.Duration
	EmbeddingModelPath  string
	EmbeddingVocabPath  string
	EmbeddingDim        int
	EmbeddingOutputName string
	GRPCTimeout         time.Duration
	EmbeddingRedisURL   string
}

func Load() Config {
	return Config{
		UDSPath:             getEnv("UDS_PATH", "/tmp/embedding-sidecar.sock"),
		RedisURL:            getEnv("REDIS_URL", "redis://localhost:6379"),
		EmbeddingRedisURL:   getEnv("EMBEDDING_REDIS_URL", getEnv("REDIS_URL", "redis://localhost:6379")),
		SimilarityThreshold: getEnvFloat("LOOP_SIMILARITY_THRESHOLD", 0.95),
		HistorySize:         getEnvInt("LOOP_HISTORY_SIZE", 5),
		EmbeddingTTL:        time.Duration(getEnvInt("LOOP_EMBEDDING_TTL", 3600)) * time.Second,
		EmbeddingModelPath:  getEnv("LOOP_EMBEDDING_MODEL_PATH", "models/all-MiniLM-L6-v2.onnx"),
		EmbeddingVocabPath:  getEnv("LOOP_EMBEDDING_VOCAB_PATH", "models/vocab.txt"),
		EmbeddingDim:        getEnvInt("LOOP_EMBEDDING_DIM", 384),
		EmbeddingOutputName: getEnv("LOOP_EMBEDDING_OUTPUT_NAME", "sentence_embedding"),
		GRPCTimeout:         time.Duration(getEnvInt("LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS", 50)) * time.Millisecond,
	}
}

func getEnv(key, defaultVal string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return defaultVal
}
