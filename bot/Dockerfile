# syntax=docker/dockerfile:1
#
# Build context is the REPOSITORY ROOT, because stage 1 compiles the shared Rust
# transcription CLI from the same workspace the desktop app uses:
#
#   docker build -f bot/Dockerfile -t afterword-bot .
#
# ---------------------------------------------------------------------------
# Stage 1: build afterword-transcribe
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# Stage 2: compile the bot's TypeScript
# ---------------------------------------------------------------------------
FROM node:22-bookworm-slim AS bot-builder

RUN corepack enable
WORKDIR /app
COPY bot/package.json bot/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY bot/tsconfig.json ./
COPY bot/src ./src
RUN pnpm run build

# ---------------------------------------------------------------------------
# Stage 3: runtime (Playwright image, matching the pinned playwright version)
# ---------------------------------------------------------------------------
FROM mcr.microsoft.com/playwright:v1.62.1-jammy AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
        pulseaudio \
        pulseaudio-utils \
        xvfb \
        ffmpeg \
    && rm -rf /var/lib/apt/lists/*

RUN corepack enable
WORKDIR /app

COPY bot/package.json bot/pnpm-lock.yaml ./
RUN pnpm install --prod --frozen-lockfile

COPY --from=bot-builder /app/dist ./dist
COPY --from=transcribe-builder /src/target/release/afterword-transcribe /usr/local/bin/afterword-transcribe
COPY bot/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh /usr/local/bin/afterword-transcribe

ENV HEADLESS=true \
    PORT=8787 \
    RECORDINGS_DIR=/app/recordings \
    MODELS_DIR=/app/models \
    TRANSCRIBE_BIN=/usr/local/bin/afterword-transcribe \
    PULSE_SINK_NAME=afterword_sink \
    DISPLAY=:99

RUN mkdir -p /app/recordings /app/models

EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["node", "dist/index.js"]
