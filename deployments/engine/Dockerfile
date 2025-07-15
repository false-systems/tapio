# Multi-stage build for tapio-engine
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
COPY cmd/tapio-engine/go.mod cmd/tapio-engine/go.sum ./cmd/tapio-engine/

# Download dependencies
RUN go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY cmd/tapio-engine/ ./cmd/tapio-engine/

# Build the engine binary
WORKDIR /app/cmd/tapio-engine
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tapio-engine .

# Final stage - minimal image
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 tapio && \
    adduser -u 1000 -G tapio -s /bin/sh -D tapio

# Set working directory
WORKDIR /home/tapio

# Copy binary from builder
COPY --from=builder /app/cmd/tapio-engine/tapio-engine /usr/local/bin/tapio-engine

# Copy configuration
COPY deployments/engine/config.yaml /etc/tapio/engine.yaml

# Create data directories
RUN mkdir -p /var/lib/tapio/data /var/log/tapio && \
    chown -R tapio:tapio /var/lib/tapio /var/log/tapio /home/tapio && \
    chmod +x /usr/local/bin/tapio-engine

# Switch to non-root user
USER tapio

# Set environment variables
ENV TAPIO_ENGINE_CONFIG=/etc/tapio/engine.yaml
ENV TAPIO_ENGINE_DATA_DIR=/var/lib/tapio/data
ENV TAPIO_ENGINE_LOG_DIR=/var/log/tapio

# Expose ports
EXPOSE 9090 8080 9091

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["tapio-engine"]
CMD ["--config", "/etc/tapio/engine.yaml"]

# Metadata
LABEL org.opencontainers.image.title="Tapio Engine"
LABEL org.opencontainers.image.description="Kubernetes Intelligence Platform Correlation Engine"
LABEL org.opencontainers.image.source="https://github.com/yairfalse/tapio"
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.vendor="Tapio"