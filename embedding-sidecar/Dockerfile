FROM golang:1.24-bookworm AS build

WORKDIR /app

# Install build dependencies
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    build-essential git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Install ONNX Runtime (CPU) libs
ENV ONNXRUNTIME_VERSION=1.23.0
RUN curl -L -o /tmp/onnxruntime.tgz https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz \
    && tar -xzf /tmp/onnxruntime.tgz -C /tmp \
    && mkdir -p /usr/local/lib /usr/local/include \
    && cp -r /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/* /usr/local/lib/ \
    && cp -r /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/include/* /usr/local/include/ \
    && ln -sf /usr/local/lib/libonnxruntime.so.${ONNXRUNTIME_VERSION} /usr/local/lib/libonnxruntime.so \
    && rm -rf /tmp/onnxruntime.tgz /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}

ENV LD_LIBRARY_PATH=/usr/local/lib

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o embedding-sidecar .

FROM debian:bookworm-slim

SHELL ["/bin/bash", "-c"]

WORKDIR /app

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates curl libstdc++6 libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# ONNX Runtime libs
ENV ONNXRUNTIME_VERSION=1.23.0
RUN curl -L -o /tmp/onnxruntime.tgz https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz \
    && tar -xzf /tmp/onnxruntime.tgz -C /tmp \
    && mkdir -p /usr/local/lib \
    && cp -r /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/* /usr/local/lib/ \
    && ln -sf /usr/local/lib/libonnxruntime.so.${ONNXRUNTIME_VERSION} /usr/local/lib/libonnxruntime.so \
    && rm -rf /tmp/onnxruntime.tgz /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}

ARG MODEL_URL
ARG MODEL_SHA256
ARG MODEL_FILENAME=all-MiniLM-L6-v2.onnx
ARG VOCAB_URL=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/vocab.txt
RUN set -euo pipefail \
    && [ -n "${MODEL_URL}" ] || (echo "MODEL_URL build arg is required" && exit 1) \
    && [ -n "${MODEL_SHA256}" ] || (echo "MODEL_SHA256 build arg is required" && exit 1) \
    && mkdir -p /app/models \
    && curl -L "${MODEL_URL}" -o /tmp/model.onnx \
    && echo "${MODEL_SHA256}  /tmp/model.onnx" | sha256sum -c - \
    && mv /tmp/model.onnx "/app/models/${MODEL_FILENAME}" \
    && curl -L "${VOCAB_URL}" -o /app/models/vocab.txt

ENV LD_LIBRARY_PATH=/usr/local/lib

# Create sockets dir for UDS
RUN mkdir -p /sockets

COPY --from=build /app/embedding-sidecar /usr/local/bin/embedding-sidecar

ENV UDS_PATH=/sockets/embedding-sidecar.sock
ENV REDIS_URL=redis://redis-embedding:6379
ENV LOOP_SIMILARITY_THRESHOLD=0.95
ENV LOOP_HISTORY_SIZE=5
ENV LOOP_EMBEDDING_TTL=3600
ENV LOOP_EMBEDDING_MODEL_PATH=/app/models/all-MiniLM-L6-v2.onnx
ENV LOOP_EMBEDDING_VOCAB_PATH=/app/models/vocab.txt
ENV LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS=50

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/embedding-sidecar"]

