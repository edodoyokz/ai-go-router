# Multi-stage build for 9router-go
# Stage 1: Build
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o 9router ./cmd/router

# Stage 2: Run
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/9router .

# Copy config example
COPY config/config.example.yaml ./config/config.yaml

# Create data directory for SQLite
RUN mkdir -p /app/data

# Expose port
EXPOSE 20128

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:20128/healthz || exit 1

# Run the server
CMD ["./9router", "serve", "--config", "./config/config.yaml"]
