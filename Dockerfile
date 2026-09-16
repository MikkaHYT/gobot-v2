# Build stage
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go generate ./... && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o gobot .

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache \
    ffmpeg \
    python3 \
    py3-pip \
    ca-certificates \
    tzdata

RUN pip3 install --break-system-packages --no-cache-dir "yt-dlp==2026.08.19"

RUN adduser -D -u 10001 appuser

WORKDIR /app

COPY --from=builder /build/gobot .

RUN mkdir -p data external && \
    chown -R appuser:appuser /app

USER appuser:appuser

VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ps aux | grep -v grep | grep gobot || exit 1

ENTRYPOINT ["./gobot"]
