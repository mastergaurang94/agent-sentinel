package store

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

	"embedding-sidecar/internal/embedder"
	"embedding-sidecar/internal/telemetry"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	redisIndexName = "loop:embeddings_idx"
	redisKeyPrefix = "loop:"
)

type VectorStore struct {
	client redis.UniversalClient
	ttl    time.Duration
	keep   int
	dim    int
}

type EmbeddingRecord struct {
	Prompt     string
	Similarity float64
	Distance   float64
	Key        string
}

func NewVectorStore(redisURL string, ttl time.Duration, keep int, dim int) (*VectorStore, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	if dim <= 0 {
		dim = embedder.DefaultEmbeddingDim
	}
	return &VectorStore{client: client, ttl: ttl, keep: keep, dim: dim}, nil
}

func (s *VectorStore) EnsureIndex(ctx context.Context) error {
	ctx, span := telemetry.StartSpan(ctx, "redis.ensure_index")
	defer span.End()
	start := time.Now()
	result := "ok"
	defer func() {
		telemetry.ObserveRedisLatency(ctx, "ensure_index", result, "", time.Since(start))
	}()

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
		"vec", "VECTOR", "HNSW", 6,
		"TYPE", "FLOAT32",
		"DIM", s.dim,
		"DISTANCE_METRIC", "COSINE",
	}
	if err := s.client.Do(ctx, args...).Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return err
	}
	return nil
}

func (s *VectorStore) StoreEmbedding(ctx context.Context, tenantID, prompt string, embedding []float32) error {
	ctx, span := telemetry.StartSpan(ctx, "redis.store_embedding",
		attribute.String("tenant.id", tenantID),
	)
	defer span.End()
	start := time.Now()
	result := "ok"
	defer func() {
		telemetry.ObserveRedisLatency(ctx, "store_embedding", result, tenantID, time.Since(start))
	}()

	if len(embedding) != s.dim {
		return fmt.Errorf("embedding dimension mismatch: got %d want %d", len(embedding), s.dim)
	}

	key := fmt.Sprintf("%s%s:%d", redisKeyPrefix, tenantID, time.Now().UnixNano())
	vecBlob := float32SliceToBytes(embedding)

	fields := []any{
		"tenant_id", tenantID,
		"prompt", prompt,
		"vec", vecBlob,
	}

	if err := s.client.HSet(ctx, key, fields...).Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return err
	}
	if err := s.client.Expire(ctx, key, s.ttl).Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
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
	ctx, span := telemetry.StartSpan(ctx, "redis.search_embeddings",
		attribute.String("tenant.id", tenantID),
		attribute.Int("search.limit", limit),
	)
	defer span.End()
	start := time.Now()
	result := "ok"
	defer func() {
		telemetry.ObserveRedisLatency(ctx, "search_embeddings", result, tenantID, time.Since(start))
	}()

	if len(queryEmbedding) != s.dim {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d want %d", len(queryEmbedding), s.dim)
	}

	vecBlob := float32SliceToBytes(queryEmbedding)

	// Using Redis VSS KNN query with tenant filter.
	tenantTag := escapeTagValue(tenantID)
	query := fmt.Sprintf("@tenant_id:{%s}=>[KNN %d @vec $vec AS score]", tenantTag, limit)

	args := []any{
		"FT.SEARCH", redisIndexName,
		query,
		"PARAMS", 2, "vec", vecBlob,
		"SORTBY", "score",
		"LIMIT", 0, limit,
		"RETURN", 2, "prompt", "score",
		"DIALECT", 2,
	}

	raw, err := s.client.Do(ctx, args...).Result()
	if err != nil {
		// If index missing, surface error so startup can create.
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		result = "error"
		return nil, err
	}

	// RESP3 (Redis 7.4+) returns a map; handle that first.
	if m, ok := raw.(map[any]any); ok {
		return parseSearchMapResult(m, limit), nil
	}

	arr, ok := raw.([]any)
	if !ok || len(arr) < 1 {
		return nil, nil
	}

	records := parseSearchArrayResult(arr, limit)
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

func escapeTagValue(v string) string {
	// RediSearch TAG requires escaping special characters (e.g., hyphen).
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "-", `\-`)
	return v
}

func parseSearchMapResult(m map[any]any, limit int) []EmbeddingRecord {
	resultsVal, ok := m["results"].([]any)
	if !ok {
		return nil
	}
	var records []EmbeddingRecord
	for _, r := range resultsVal {
		rm, ok := r.(map[any]any)
		if !ok {
			continue
		}
		key, _ := rm["id"].(string)
		extra, _ := rm["extra_attributes"].(map[any]any)
		prompt, _ := extra["prompt"].(string)
		distance := parseFloatFromAny(extra["score"])
		records = append(records, EmbeddingRecord{
			Prompt:     prompt,
			Similarity: distanceToSimilarity(distance),
			Distance:   distance,
			Key:        key,
		})
		if len(records) >= limit {
			break
		}
	}
	return records
}

func parseSearchArrayResult(arr []any, limit int) []EmbeddingRecord {
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
				distance = parseFloatFromAny(data[j+1])
			}
		}
		records = append(records, EmbeddingRecord{
			Prompt:     prompt,
			Similarity: distanceToSimilarity(distance),
			Distance:   distance,
			Key:        key,
		})
		if len(records) >= limit {
			break
		}
	}
	return records
}

func parseFloatFromAny(v any) float64 {
	switch val := v.(type) {
	case string:
		f, _ := strconvParseFloatSafe(val)
		return f
	case float64:
		return val
	case float32:
		return float64(val)
	}
	return 0
}
