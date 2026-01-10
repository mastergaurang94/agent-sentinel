#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
DEFAULT_DEST="${REPO_ROOT}/models/all-MiniLM-L6-v2.onnx"

MODEL_URL="${MODEL_URL:-}"
MODEL_SHA256="${MODEL_SHA256:-}"
VOCAB_URL="${VOCAB_URL:-https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/vocab.txt}"
DEST="${1:-${DEFAULT_DEST}}"

if [[ -z "${MODEL_URL}" ]]; then
  echo "MODEL_URL is required (export MODEL_URL=...)" >&2
  exit 1
fi

if [[ -z "${MODEL_SHA256}" ]]; then
  echo "MODEL_SHA256 is required (export MODEL_SHA256=...)" >&2
  exit 1
fi

mkdir -p "$(dirname "${DEST}")"

if [[ -f "${DEST}" ]]; then
  existing_sha="$(sha256sum "${DEST}" | awk '{print $1}')"
  if [[ "${existing_sha}" == "${MODEL_SHA256}" ]]; then
    echo "Model already present and checksum matches at ${DEST}"
    exit 0
  fi
  echo "Existing model checksum mismatch, re-downloading..."
fi

tmp_file="$(mktemp)"
trap 'rm -f "${tmp_file}"' EXIT

curl -L "${MODEL_URL}" -o "${tmp_file}"
echo "${MODEL_SHA256}  ${tmp_file}" | sha256sum -c -
mv "${tmp_file}" "${DEST}"
chmod 0644 "${DEST}"
echo "Model downloaded to ${DEST}"

VOCAB_DEST="$(dirname "${DEST}")/vocab.txt"
if [[ ! -f "${VOCAB_DEST}" ]]; then
  curl -L "${VOCAB_URL}" -o "${VOCAB_DEST}"
  chmod 0644 "${VOCAB_DEST}"
  echo "Vocab downloaded to ${VOCAB_DEST}"
fi

