# Build stage
FROM golang:1.25.5-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /wordbot ./cmd/wordbot

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates bash
WORKDIR /app

COPY --from=builder /wordbot /app/wordbot
COPY scripts/backup-wordlearn-db.sh /app/backup-wordlearn-db.sh
RUN chmod +x /app/backup-wordlearn-db.sh

# Default DB path inside container; override with WORDLEARN_DB_PATH
ENV WORDLEARN_DB_PATH=/data/wordlearn.db
ENV WORDLEARN_BACKUP_DIR=/backups

VOLUME ["/data"]

ENTRYPOINT ["/bin/sh", "-c", "/app/backup-wordlearn-db.sh; exec /app/wordbot"]
