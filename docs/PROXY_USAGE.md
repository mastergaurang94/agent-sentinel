# Proxy Usage

Run the Agent Sentinel proxy locally, exercise Gemini/OpenAI flows, and validate rate limiting plus loop detection.

## Prerequisites
- Docker and Docker Compose
- `.env` with:
  - `GEMINI_API_KEY` (and optionally `OPENAI_API_KEY`)
  - `TARGET_API` (`gemini` or `openai`)
  - For embedding sidecar build:  
    ```
    MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
    MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452
    ```

## Start the stack
```bash
docker compose up -d --build
```
Services:
- Proxy on `localhost:8080`
- `redis` for rate limiting
- `redis-embedding` + `embedding-sidecar` for loop detection (gRPC over UDS)

## Quick checks
- List Gemini models:
  ```bash
  curl -s "https://generativelanguage.googleapis.com/v1beta/models?key=$GEMINI_API_KEY" \
    | jq -r '.models[] | select(.displayName|test("flash";"i")) | .name'
  ```

- Basic request (Gemini flash):
  ```bash
  curl -i -X POST http://localhost:8080/v1beta/models/gemini-flash-latest:generateContent \
    -H "Content-Type: application/json" \
    -H "X-Tenant-ID: demo-tenant" \
    -d '{"contents":[{"parts":[{"text":"Say hello in 3 languages"}]}]}'
  ```
  Expect 200, rate-limit headers, and a small spend recorded for `spend:demo-tenant`.

1) Success + cost adjust  
```bash
curl -i -X POST http://localhost:8080/v1beta/models/gemini-flash-latest:generateContent \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: success-tenant" \
  -d '{"contents":[{"parts":[{"text":"List 3 colors"}]}]}'
```
Expect 200, rate-limit headers, and Redis spend > 0 for `spend:success-tenant`.

2) Refund on error (invalid model)  
```bash
curl -i -X POST http://localhost:8080/v1beta/models/invalid-model:generateContent \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: refund-tenant" \
  -d '{"contents":[{"parts":[{"text":"Trigger error"}]}]}'
```
Expect 404, rate-limit headers, and Redis spend stays 0 for `spend:refund-tenant`.

3) Rate-limit enforcement  
Set a tiny limit (e.g., `$0.001`) for a tenant:  
```bash
docker exec agent-sentinel-redis redis-cli set limit:limit-tenant 0.001
```
Send a large estimated-cost request:  
```bash
python - <<'PY' > /tmp/large.json
import json
long_text = "lorem ipsum " * 2000
payload = {"contents":[{"parts":[{"text": long_text}]}],"generationConfig":{"maxOutputTokens":200000}}
print(json.dumps(payload))
PY
curl -i -X POST http://localhost:8080/v1beta/models/gemini-flash-latest:generateContent \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: limit-tenant" \
  --data-binary @/tmp/large.json
```
Expect 429 with rate-limit headers; Redis `spend:limit-tenant` shows small spend; limit key enforced.

## Notes
- `X-Tenant-ID` is the default tenant header; override with `RATE_LIMIT_HEADER` if needed.
- Streaming responses are cost-adjusted incrementally.
- Loop detection calls the embedding sidecar via UDS (`LOOP_EMBEDDING_SIDECAR_UDS`), and the proxy fail-opens if the sidecar is down.
- OTLP tracing can be enabled with `OTEL_EXPORTER_OTLP_ENDPOINT`; spans avoid recording prompt content.

