FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/daily-github ./cmd/server


FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -S app \
	&& adduser -S app -G app \
	&& mkdir -p /app/data \
	&& chown -R app:app /app

COPY --from=builder /out/daily-github /app/daily-github
COPY --chown=app:app index.html /app/index.html

USER app

ENV PORT=18080 \
	MCP_PORT=18081 \
	MCP_PATH=/mcp \
	MCP_ENABLED=false \
	DATA_DIR=/app/data \
	GENERATE_ON_STARTUP=true \
	GENERATE_CRON="5 0 * * *" \
	GENERATE_TIMEZONE=UTC \
	SERVER_READ_TIMEOUT=15s \
	SERVER_WRITE_TIMEOUT=120s \
	SERVER_IDLE_TIMEOUT=120s \
	SERVER_SHUTDOWN_TIMEOUT=15s \
	MCP_READY_TIMEOUT=15s \
	MCP_READY_RETRY_COUNT=3 \
	MCP_READY_RETRY_DELAY=10s \
	MCP_SERVER_READ_TIMEOUT=15s \
	MCP_SERVER_WRITE_TIMEOUT=300s \
	MCP_SERVER_IDLE_TIMEOUT=120s

VOLUME ["/app/data"]

EXPOSE 18080 18081

ENTRYPOINT ["/app/daily-github"]