package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisIndexName = "loop:embeddings_idx"
	redisKeyPrefix = "loop:"
)

type VectorStore struct {
	client redis.UniversalClient
	ttl    time.Duration
	keep   int
}

type EmbeddingRecord struct {
	Prompt     string
	Similarity float64
	Distance   float64
	Key        string
}

func NewVectorStore(redisURL string, ttl time.Duration, keep int) (*VectorStore, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	return &VectorStore{client: client, ttl: ttl, keep: keep}, nil
}

func (s *VectorStore) EnsureIndex(ctx context.Context) error {
	_, err := s.client.Do(ctx, "FT.INFO", redisIndexName).Result()
	if err == nil {
		return nil
	}

	args := []any{
		"FT.CREATE", redisIndexName,
		"ON", "HASH",
		"PREFIX", 1, redisKeyPrefix,
		"SCHEMA",
		"tenant_id", "TAG",
		"prompt", "TEXT",
		"vec", "VECTOR", "HNSW", 12,
		"TYPE", "FLOAT32",
		"DIM", embeddingDim,
		"DISTANCE_METRIC", "COSINE",
	}
	return s.client.Do(ctx, args...).Err()
}

func (s *VectorStore) StoreEmbedding(ctx context.Context, tenantID, prompt string, embedding []float32) error {
	if len(embedding) != embeddingDim {
		return fmt.Errorf("embedding dimension mismatch: got %d want %d", len(embedding), embeddingDim)
	}

	key := fmt.Sprintf("%s%s:%d", redisKeyPrefix, tenantID, time.Now().UnixNano())
	vecBlob := float32SliceToBytes(embedding)

	fields := []any{
		"tenant_id", tenantID,
		"prompt", prompt,
		"vec", vecBlob,
	}

	if err := s.client.HSet(ctx, key, fields...).Err(); err != nil {
		return err
	}
	if err := s.client.Expire(ctx, key, s.ttl).Err(); err != nil {
		return err
	}

	// Optional pruning to keep recent embeddings small per tenant.
	if s.keep > 0 {
		go s.pruneOldEmbeddings(context.Background(), tenantID, s.keep)
	}
	return nil
}

func (s *VectorStore) pruneOldEmbeddings(ctx context.Context, tenantID string, keep int) {
	iter := s.client.Scan(ctx, 0, fmt.Sprintf("%s%s:*", redisKeyPrefix, tenantID), 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		slog.Warn("prune scan failed", "tenant", tenantID, "error", err)
		return
	}
	if len(keys) <= keep {
		return
	}
	sort.Strings(keys)
	toDelete := keys[:len(keys)-keep]
	if err := s.client.Del(ctx, toDelete...).Err(); err != nil {
		slog.Warn("prune delete failed", "tenant", tenantID, "error", err, "count", len(toDelete))
	}
}

func (s *VectorStore) SearchSimilarEmbeddings(ctx context.Context, tenantID string, queryEmbedding []float32, limit int) ([]EmbeddingRecord, error) {
	if len(queryEmbedding) != embeddingDim {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d want %d", len(queryEmbedding), embeddingDim)
	}

	vecBlob := float32SliceToBytes(queryEmbedding)

	// Using Redis VSS KNN query with tenant filter.
	query := fmt.Sprintf("@tenant_id:{%s}=>[KNN %d @vec $vec AS score]", tenantID, limit)

	args := []any{
		"FT.SEARCH", redisIndexName,
		query,
		"PARAMS", 2, "vec", vecBlob,
		"SORTBY", "score",
		"RETURN", 2, "prompt", "score",
		"DIALECT", 2,
	}

	raw, err := s.client.Do(ctx, args...).Result()
	if err != nil {
		// If index missing, surface error so startup can create.
		return nil, err
	}

	arr, ok := raw.([]any)
	if !ok || len(arr) < 1 {
		return nil, nil
	}

	var records []EmbeddingRecord
	for i := 1; i < len(arr); i += 2 {
		key, _ := arr[i].(string)
		data, _ := arr[i+1].([]any)
		var prompt string
		var distance float64
		for j := 0; j < len(data); j += 2 {
			field, _ := data[j].(string)
			switch strings.ToLower(field) {
			case "prompt":
				prompt, _ = data[j+1].(string)
			case "score":
				switch v := data[j+1].(type) {
				case string:
					distance, _ = strconvParseFloatSafe(v)
				case float64:
					distance = v
				case float32:
					distance = float64(v)
				}
			}
		}
		similarity := distanceToSimilarity(distance)
		records = append(records, EmbeddingRecord{
			Prompt:     prompt,
			Similarity: similarity,
			Distance:   distance,
			Key:        key,
		})
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}

func distanceToSimilarity(distance float64) float64 {
	// COSINE distance: 0.0 (identical) to 2.0 (opposite). Convert to similarity 0..1.
	if distance <= 0 {
		return 1
	}
	if distance >= 2 {
		return 0
	}
	return 1 - (distance / 2)
}

func float32SliceToBytes(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func strconvParseFloatSafe(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
