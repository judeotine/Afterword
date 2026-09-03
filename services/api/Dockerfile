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

RUN go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/judeotine/afterword/services/api/internal/version.Version=${VERSION} \
      -X github.com/judeotine/afterword/services/api/internal/version.Commit=${COMMIT} \
      -X github.com/judeotine/afterword/services/api/internal/version.BuildDate=${BUILD_DATE}" \
    -o /api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /api /api
COPY --from=builder /src/migrations /migrations

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
