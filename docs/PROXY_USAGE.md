# Proxy Usage & Testing

## Prerequisites
- Docker & Docker Compose
- `.env` with:
  - `GEMINI_API_KEY` (and optionally `OPENAI_API_KEY`)
  - `TARGET_API` (`gemini` or `openai`)
  - For embedding sidecar build:  
    ```
    MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
    MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452
    ```

## Bring up the stack
```bash
docker compose up -d --build
```
Services:
- `agent-sentinel` (proxy) on `localhost:8080`
- `redis` for rate limiting
- `redis-embedding` and `embedding-sidecar` for loop detection

## List available Gemini models
```bash
curl -s "https://generativelanguage.googleapis.com/v1beta/models?key=$GEMINI_API_KEY" \
  | jq -r '.models[] | select(.displayName|test("flash";"i")) | .name'
```

## Sample requests (Gemini flash)
```bash
curl -i -X POST http://localhost:8080/v1beta/models/gemini-flash-latest:generateContent \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: demo-tenant" \
  -d '{"contents":[{"parts":[{"text":"Say hello in 3 languages"}]}]}'
```

To exercise an error/refund path, use an invalid model name; rate-limit headers will still be returned and cost will refund on 4xx/5xx with no usage.

## Testing checklist (proxy)

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
- Rate limiting: tenant ID header defaults to `X-Tenant-ID` (configurable via `RATE_LIMIT_HEADER`).
- Streaming: SSE responses are cost-adjusted incrementally via the streaming reader.
- Loop detection: the proxy will call the embedding sidecar over UDS (configure `LOOP_EMBEDDING_SIDECAR_UDS` if needed). The sidecar is built with the ONNX model baked into the image.

