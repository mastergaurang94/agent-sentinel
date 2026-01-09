# all-MiniLM-L6-v2 ONNX model

The ONNX model is intentionally not stored in git. The binary (`all-MiniLM-L6-v2.onnx`) is git-ignored and docker-ignored.

How to obtain:

- Docker builds: pass `MODEL_URL` and `MODEL_SHA256` build args so the Dockerfile downloads and verifies the model and bakes it into the image. The recommended values are:
  - `MODEL_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx`
  - `MODEL_SHA256=6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452`
- Local (non-Docker): run `./embedding-sidecar/scripts/download_model.sh` from the repo root (requires `MODEL_URL` and `MODEL_SHA256` env vars); the script downloads to `embedding-sidecar/models/all-MiniLM-L6-v2.onnx` by default and verifies the checksum.

After downloading, the embedding sidecar reads the model from `LOOP_EMBEDDING_MODEL_PATH` (default `/app/models/all-MiniLM-L6-v2.onnx`).

