# Build stage
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/db-backup-scheduler .

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

RUN adduser -D -u 1000 appuser

WORKDIR /app

COPY --from=builder /app/db-backup-scheduler .

RUN chown -R appuser:appuser /app && mkdir -p /data && chown appuser:appuser /data

USER appuser

ENV PORT=3400
ENV DATA_DIR=/data

EXPOSE 3400

VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/ || exit 1

ENTRYPOINT ["./db-backup-scheduler"]
