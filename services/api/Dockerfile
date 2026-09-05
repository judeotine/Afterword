FROM golang:1.25 AS builder

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOOS=linux

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN SCHEMA_VERSION="$(ls migrations/*.up.sql | sed -n 's#.*/0*\([0-9][0-9]*\)_.*#\1#p' | sort -n | tail -1)" && \
    go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/judeotine/afterword/services/api/internal/version.Version=${VERSION} \
      -X github.com/judeotine/afterword/services/api/internal/version.Commit=${COMMIT} \
      -X github.com/judeotine/afterword/services/api/internal/version.BuildDate=${BUILD_DATE} \
      -X github.com/judeotine/afterword/services/api/internal/httpx.ExpectedSchemaVersion=${SCHEMA_VERSION}" \
    -o /api ./cmd/api

RUN go build -trimpath -ldflags="-s -w" -o /migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /api /api
COPY --from=builder /migrate /migrate
COPY --from=builder /src/migrations /migrations

USER nonroot:nonroot
EXPOSE 8080
CMD ["/api"]
