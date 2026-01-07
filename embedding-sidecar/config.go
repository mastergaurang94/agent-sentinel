package main

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
	GRPCTimeout         time.Duration
}

func loadConfig() Config {
	cfg := Config{
		UDSPath:             getEnv("UDS_PATH", "/tmp/embedding-sidecar.sock"),
		RedisURL:            getEnv("REDIS_URL", "redis://localhost:6379"),
		SimilarityThreshold: getEnvFloat("LOOP_SIMILARITY_THRESHOLD", 0.95),
		HistorySize:         getEnvInt("LOOP_HISTORY_SIZE", 5),
		EmbeddingTTL:        time.Duration(getEnvInt("LOOP_EMBEDDING_TTL", 3600)) * time.Second,
		EmbeddingModelPath:  getEnv("LOOP_EMBEDDING_MODEL_PATH", "models/all-MiniLM-L6-v2.onnx"),
		GRPCTimeout:         time.Duration(getEnvInt("LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS", 50)) * time.Millisecond,
	}
	return cfg
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
