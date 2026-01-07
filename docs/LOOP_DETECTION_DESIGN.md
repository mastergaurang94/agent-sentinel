# Semantic Loop Detection Design - Agent Sentinel

## Overview

Detect when agents enter semantic loops (repeating the same intent with different wording) by comparing prompt embeddings using cosine similarity. When a loop is detected (>0.95 similarity to any of last 5 prompts), inject a system message hint to break the loop.

## Architecture

**Service Split**: The loop detection functionality is split into two separate services:

1. **Proxy Service** (Agent Sentinel): Handles request flow, rate limiting, and state management
2. **Embedding Sidecar Service**: Handles embedding generation, similarity computation, and embedding state

```
Request Flow:
┌─────────────┐
│   Client    │
└──────┬──────┘
       │
       ▼
┌─────────────────────┐
│ Tracing Middleware  │
└──────────┬──────────┘
       │
       ▼
┌──────────────────────────┐
│ Rate Limiting Middleware │ (Proxy Redis)
└──────────┬───────────────┘
       │
       ▼
┌──────────────────────────┐
│ Loop Detection Middleware│ ← NEW: Calls sidecar service
└──────────┬───────────────┘
       │    │
       │    └─── gRPC/UDS ──► ┌─────────────────────┐
       │                      │ Embedding Sidecar    │
       │                      │ Service              │
       │                      │ - Generate embedding │
       │                      │ - Check similarity   │
       │                      │ - Store embeddings   │
       │                      │ (Embedding Redis)    │
       │                      └──────────────────────┘
       │
       ▼
┌─────────────────────┐
│ Logging Middleware  │
└──────────┬──────────┘
       │
       ▼
┌─────────────────────┐
│   Proxy Handler     │
└─────────────────────┘
```

**Placement**: Loop detection middleware runs after rate limiting to ensure financial constraints are enforced first and avoid slow embedding generations, then semantic analysis is performed on allowed requests.

**Communication**: Proxy calls embedding sidecar service via gRPC over Unix Domain Sockets (UDS). The embedding sidecar manages its own dedicated Redis instance for embedding storage, separate from the proxy's rate limiting Redis.

## Components

### Proxy Service Components

#### 1. Loop Detection Middleware (`loopdetect_middleware.go`)

**Placement**: After rate limiting, before logging.

**Logic Flow**:
1. Extract prompt text from request (reuse `extractFullRequestText` from `parser.go`)
2. Skip if no prompt text found
3. Extract tenant ID from request header
4. Call embedding sidecar service via gRPC to check for loop (see Embedding Sidecar API below)
5. If loop detected (similarity > threshold):
   - Log loop detection with tenant ID and similarity score
   - Modify request to inject system message (see Intervention below)
6. Continue to next middleware

**Fail-Open Strategy**: If embedding sidecar service is unavailable or returns an error, allow request through (log warning). This ensures 100% uptime even if loop detection is unavailable.

**Embedding Sidecar gRPC Call**:
- **Service**: `EmbeddingService`
- **Method**: `CheckLoop`
- **Request**:
  ```protobuf
  message CheckLoopRequest {
    string tenant_id = 1;
    string prompt = 2;
  }
  ```
- **Response**:
  ```protobuf
  message CheckLoopResponse {
    bool loop_detected = 1;
    double max_similarity = 2;
    string similar_prompt = 3;
  }
  ```
- **Transport**: Unix Domain Socket (UDS)
- **Timeout**: 50ms (fail-open if exceeded)

### Embedding Sidecar Service Components

#### 1. Embedding Generation (`embedder.go`)

**Model**: all-MiniLM-L6-v2 (384-dimensional embeddings)

**Implementation**: ONNX Runtime Go bindings with pre-converted model. The model file (~80MB) is bundled in the repository to avoid download failures and ensure consistent behavior across environments. ONNX Runtime provides sub-10ms latency for embedding generation, though it increases the binary size.

**Interface Design**: Implement an `Embedding` interface with a `Compute(text string) ([]float32, error)` method. This allows easy swapping of embedding models in the future. The initial implementation will use ONNX with all-MiniLM-L6-v2, but the interface ensures the code remains flexible for future model changes.

#### 2. Vector Storage (`store.go`)

**Redis Stack Vector Similarity Search (VSS)** (per tenant, in dedicated Embedding Redis):

**Index**: HNSW index on `loop:embeddings` with COSINE distance metric.

**Storage Structure**:
```
HSET loop:{tenant_id}:{timestamp}
  - tenant_id: "{tenant_id}"
  - prompt: "{original_prompt_text}"
  - vec: [BINARY_BLOB] (embedding vector)
```

**Operations**:
- `StoreEmbedding(tenantID, prompt string, embedding []float32) error` - Stores embedding as HSET with vector field
- `SearchSimilarEmbeddings(tenantID string, queryEmbedding []float32, limit int, threshold float64) ([]EmbeddingRecord, error)` - Uses Redis VSS KNN search
- Maintain last 5 embeddings per tenant (cleanup older entries when adding 6th)

**Cleanup**: Uses `EXPIRE` (TTL) of 1 hour on each hash key for automatic expiration.

**Benefits**:
- Cosine similarity computed directly in Redis (faster, reduces CPU usage)
- Lower storage costs (binary blob format, efficient indexing)
- Native vector search capabilities (HNSW index for fast approximate nearest neighbor search)

**Tenant Isolation**: Each tenant has separate embedding history to prevent cross-tenant interference.

**Redis Instance**: Embedding sidecar uses its own dedicated Redis Stack instance (with VSS module), separate from the proxy's rate limiting Redis.

#### 3. Vector Similarity Search

**Redis VSS**: Uses Redis Stack's native vector similarity search with HNSW index.

**Distance Metric**: COSINE distance (returns 0.0 to 2.0, where 0.0 = identical, 2.0 = opposite)

**Similarity Conversion**: Convert COSINE distance to similarity: `similarity = 1 - (distance / 2)`
- Distance 0.0 → Similarity 1.0 (identical)
- Distance 0.1 → Similarity 0.95 (threshold for loop detection)
- Distance 2.0 → Similarity 0.0 (opposite)

**Threshold**: Similarity >0.95 (equivalent to COSINE distance <0.1) indicates semantic loop.

**Query**: Use `FT.SEARCH` with KNN query to find most similar embeddings for a tenant.

#### 4. Loop Detection gRPC Handler (`handler.go`)

**gRPC Service**: `EmbeddingService`

**Logic Flow**:
1. Parse gRPC request (tenant_id, prompt)
2. Generate embedding for prompt
3. Query Redis VSS for similar embeddings (KNN search, limit 5, filter by tenant_id)
4. Convert COSINE distance to similarity score
5. Find max similarity
6. Store new embedding in Embedding Redis (async, don't block response)
7. Return gRPC response with loop detection result

**Response Time Target**: <30ms total (including embedding generation, Redis VSS query, similarity conversion)

**Startup Warmup**: Before opening the UDS port, the sidecar performs a dummy embedding request to warm up the ONNX model and ensure it's ready to handle requests. This prevents the first real request from experiencing cold start latency.

### 5. Request Modification (Intervention)

When a loop is detected, inject a system message to guide the agent to break the loop.

**For OpenAI** (Chat Completion format):
- If `messages` array exists, prepend system message:
  ```json
  {
    "role": "system",
    "content": "You seem to be repeating similar requests. Please take a different approach or consider that the previous attempts may have already addressed your question."
  }
  ```

**For Gemini** (Contents format):
- Add to `systemInstruction.parts`:
  ```json
  {
    "systemInstruction": {
      "parts": [{
        "text": "You seem to be repeating similar requests. Please take a different approach or consider that the previous attempts may have already addressed your question."
      }]
    }
  }
  ```

**For OpenAI Responses API** (`input` field):
- Wrap input with system context (may require format detection)

**Note**: The intervention modifies the request body, so we need to ensure the modified body is properly formatted and doesn't break the API contract.

### 6. Configuration

**Proxy Service Environment Variables**:
- `LOOP_DETECTION_ENABLED` (default: `true`) - Enable/disable loop detection
- `LOOP_EMBEDDING_SIDECAR_UDS` (default: `/tmp/embedding-sidecar.sock`) - Unix Domain Socket path for embedding sidecar
- `LOOP_EMBEDDING_SIDECAR_TIMEOUT` (default: `50ms`) - gRPC timeout for embedding sidecar calls
- `LOOP_SIMILARITY_THRESHOLD` (default: `0.95`) - Cosine similarity threshold (0.0-1.0)
- `LOOP_INTERVENTION_MESSAGE` (optional) - Custom intervention message text

**Embedding Sidecar Service Environment Variables**:
- `UDS_PATH` (default: `/tmp/embedding-sidecar.sock`) - Unix Domain Socket path for gRPC server
- `REDIS_URL` - Embedding Redis connection URL (dedicated instance)
- `LOOP_SIMILARITY_THRESHOLD` (default: `0.95`) - Cosine similarity threshold (0.0-1.0)
- `LOOP_HISTORY_SIZE` (default: `5`) - Number of recent prompts to compare against
- `LOOP_EMBEDDING_TTL` (default: `3600` seconds) - TTL for stored embeddings
- `LOOP_EMBEDDING_MODEL_PATH` (optional) - Path to ONNX model file

## Implementation Details

### Service Structure

**Proxy Service** (Agent Sentinel):
```
agent-sentinel/
├── loopdetect_middleware.go  # HTTP middleware that calls embedding sidecar via gRPC
└── ... (existing proxy code)
```

**Embedding Sidecar Service** (separate repository/service):
```
embedding-sidecar/
├── main.go           # gRPC server entry point (with warmup before UDS bind)
├── handler.go        # gRPC CheckLoop handler
├── embedder.go       # Embedding interface and ONNX implementation
├── store.go          # Redis VSS storage operations (Embedding Redis)
├── detector.go       # Main detection logic
└── proto/            # gRPC protobuf definitions
    └── embedding.proto
```

**Note**: `embedder.go` will define the `Embedding` interface and provide the ONNX-based implementation. Future embedding models can implement the same interface.

### Dependencies

**Proxy Service**:
- gRPC client libraries for embedding sidecar communication
- No embedding or ONNX dependencies

**Embedding Sidecar Service**:
- `google.golang.org/grpc` - gRPC framework
- `github.com/yalue/onnxruntime_go` - ONNX Runtime bindings
- `github.com/redis/go-redis/v9` - Redis client for embedding storage (with Redis Stack VSS support)
- Pre-converted ONNX model file (all-MiniLM-L6-v2, ~80MB, bundled in repository)
- **Redis Stack**: Requires Redis Stack (not standard Redis) for Vector Similarity Search module

### Performance Considerations

**Proxy Service**:
- **Embedding sidecar gRPC call**: Target <30ms total (UDS provides low latency)
- **Timeout**: 50ms with fail-open behavior
- **Request modification**: Minimize body parsing overhead by reusing existing parser functions

**Embedding Sidecar Service**:
- **Embedding generation**: Target <10ms (ONNX should achieve this)
- **Redis VSS operations**: Use FT.SEARCH with KNN for fast vector similarity search (HNSW index)
- **Similarity computation**: Handled by Redis VSS (COSINE distance), convert to similarity score
- **Async storage**: Store new embedding asynchronously to not block response
- **Startup warmup**: Perform dummy embedding request before opening UDS port to avoid cold start
- **Total response time**: Target <30ms (embedding + Redis VSS query + similarity conversion)

### Error Handling

**Proxy Service**:
- **Embedding sidecar unavailable**: Fail-open, log warning, allow request through
- **Embedding sidecar timeout**: Fail-open, log warning, allow request through
- **Embedding sidecar error response**: Fail-open, log error, allow request through
- **Invalid prompt format**: Skip detection, allow request
- **Request modification failure**: Log error, allow original request through

**Embedding Sidecar Service**:
- **Model load failure**: Return 500 error, log error, do not start gRPC server
- **Startup warmup failure**: Return 500 error, log error, do not start gRPC server
- **Redis unavailable**: Return 500 error, log error (proxy will fail-open)
- **Redis VSS index not found**: Return 500 error, log error (proxy will fail-open)
- **Embedding generation failure**: Return 500 error, log error (proxy will fail-open)
- **Invalid request format**: Return 400 error

## Testing Strategy

**Proxy Service**:
- Unit tests for embedding sidecar gRPC client and error handling
- Integration test: Mock embedding sidecar service, verify loop detection middleware
- Fail-open test: Verify requests proceed when embedding sidecar unavailable/times out
- Request modification test: Verify system message injection for both OpenAI and Gemini formats

**Embedding Sidecar Service**:
- Unit tests for Redis VSS operations (store, search)
- Unit tests for distance-to-similarity conversion
- Unit tests for embedding generation
- Integration test: Simulate 5 similar prompts, verify loop detection
- Performance test: Measure embedding generation latency (<10ms target)
- Performance test: Measure Redis VSS query latency
- Performance test: Measure total response time (<30ms target)
- Startup test: Verify warmup completes before UDS port opens

**End-to-End**:
- Full integration test with both services running
- Verify fail-open behavior when embedding sidecar is down

## Deployment

**Service Separation**: Proxy and Embedding Sidecar are deployed as separate services with independent scaling and resource allocation.

**Redis Instances**:
- **Proxy Redis**: Used for rate limiting (existing, standard Redis)
- **Embedding Redis**: Dedicated instance for embedding storage (Redis Stack with VSS module)

**Docker Compose Structure**:
```yaml
services:
  redis:              # Proxy Redis (rate limiting, standard Redis)
  redis-embedding:    # Embedding Redis (Redis Stack with VSS)
  agent-sentinel:     # Proxy service
  embedding-sidecar: # Embedding sidecar service
```

**Communication**: Proxy calls embedding sidecar via gRPC over Unix Domain Sockets (UDS). Services share the same container/host filesystem for UDS communication. UDS path configured via environment variable.

**Scaling**: Embedding sidecar can be scaled independently based on embedding workload. Multiple embedding sidecar instances can share the same Embedding Redis instance.

## Planned Features (Implementation Order)

**Phase 1: Embedding Sidecar Service**
1. Core embedding sidecar service (gRPC server, embedding generation, Redis VSS storage)
2. Redis Stack setup for embedding sidecar (dedicated instance with VSS module)
3. HNSW index creation and vector field storage
4. Startup warmup (dummy embedding request before UDS bind)
5. gRPC protobuf definitions

**Phase 2: Proxy Integration**
4. Loop detection middleware in proxy (embedding sidecar gRPC client)
5. Request modification (intervention system messages)
6. Configuration and fail-open behavior

**Phase 3: Observability**
7. Metrics/telemetry for loop detection events (both services)

## Future Enhancements

- Escalation strategy for repeated loop detections
- Support for additional embedding models (via the Embedding interface)
- Embedding sidecar service discovery/load balancing for high availability

