# syntax=docker/dockerfile:1
FROM rust:1-bookworm AS transcribe-builder

RUN apt-get update && apt-get install -y --no-install-recommends \
        cmake \
        clang \
        libclang-dev \
        pkg-config \
        libssl-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY . .
RUN cargo build --release -p afterword-core --bin afterword-transcribe

FROM golang:1.25-bookworm AS go-builder

ENV CGO_ENABLED=0 \
    GOOS=linux

WORKDIR /src/services/api
COPY services/api/go.mod services/api/go.sum ./
RUN go mod download
COPY services/api/ ./
RUN go build -trimpath -ldflags="-s -w" -o /transcribe-worker ./cmd/transcribe-worker

FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        ffmpeg \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --system --create-home --uid 10001 afterword

COPY --from=go-builder /transcribe-worker /usr/local/bin/transcribe-worker
COPY --from=transcribe-builder /src/target/release/afterword-transcribe /usr/local/bin/afterword-transcribe
RUN chmod +x /usr/local/bin/transcribe-worker /usr/local/bin/afterword-transcribe

ENV TRANSCRIBE_BIN=/usr/local/bin/afterword-transcribe \
    FFMPEG_BIN=/usr/bin/ffmpeg \
    MODELS_DIR=/models \
    WORK_DIR=/work

RUN mkdir -p /models /work && chown -R afterword:afterword /models /work

USER afterword
ENTRYPOINT ["/usr/local/bin/transcribe-worker"]
