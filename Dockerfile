# Multi-stage build for NusaNexus Router
# Stage 1: Build
FROM golang:1.24-alpine AS builder

# Install build dependencies (build-base needed for CGO/SQLite)
RUN apk add --no-cache git ca-certificates tzdata build-base

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary with version info
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo \
    -ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" \
    -o router ./cmd/router

# Stage 2: Run
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/router .

# Copy config example
COPY config/config.example.yaml ./config/config.yaml

# Create data directory for SQLite
RUN mkdir -p /app/data

# Expose port
EXPOSE 1988

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:1988/healthz || exit 1

# Run the server
CMD ["./router", "serve", "--config", "./config/config.yaml"]
