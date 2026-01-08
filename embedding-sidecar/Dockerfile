FROM golang:1.24-alpine AS build

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache build-base git curl tar

COPY go.mod go.sum ./ 
RUN go mod download

COPY . .

# Install ONNX Runtime (CPU) libs
ENV ONNXRUNTIME_VERSION=1.18.0
RUN curl -L -o /tmp/onnxruntime.tgz https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz \
    && tar -xzf /tmp/onnxruntime.tgz -C /tmp \
    && cp -r /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/* /usr/local/lib/ \
    && cp -r /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/include/* /usr/local/include/ \
    && rm -rf /tmp/onnxruntime.tgz /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}

ENV LD_LIBRARY_PATH=/usr/local/lib

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o embedding-sidecar ./...

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates curl

# ONNX Runtime libs
ENV ONNXRUNTIME_VERSION=1.18.0
RUN curl -L -o /tmp/onnxruntime.tgz https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz \
    && tar -xzf /tmp/onnxruntime.tgz -C /tmp \
    && cp -r /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/* /usr/local/lib/ \
    && rm -rf /tmp/onnxruntime.tgz /tmp/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}

ARG MODEL_URL
ARG MODEL_SHA256
ARG MODEL_FILENAME=all-MiniLM-L6-v2.onnx
RUN set -euo pipefail \
    && [ -n "${MODEL_URL}" ] || (echo "MODEL_URL build arg is required" && exit 1) \
    && [ -n "${MODEL_SHA256}" ] || (echo "MODEL_SHA256 build arg is required" && exit 1) \
    && mkdir -p /app/models \
    && curl -L "${MODEL_URL}" -o /tmp/model.onnx \
    && echo "${MODEL_SHA256}  /tmp/model.onnx" | sha256sum -c - \
    && mv /tmp/model.onnx "/app/models/${MODEL_FILENAME}"

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
ENV LOOP_EMBEDDING_SIDECAR_TIMEOUT_MS=50

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/embedding-sidecar"]

