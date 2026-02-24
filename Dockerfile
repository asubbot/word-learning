# Build stage
FROM golang:1.25.5-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /wordbot ./cmd/wordbot

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app

COPY --from=builder /wordbot /app/wordbot

# Default DB path inside container; override with WORDLEARN_DB_PATH
ENV WORDLEARN_DB_PATH=/data/wordlearn.db

VOLUME ["/data"]

ENTRYPOINT ["/app/wordbot"]
