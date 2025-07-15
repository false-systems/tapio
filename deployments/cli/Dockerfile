# Multi-stage build for tapio-cli
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
COPY cmd/tapio-cli/go.mod cmd/tapio-cli/go.sum ./cmd/tapio-cli/

# Download dependencies
RUN go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY cmd/tapio-cli/ ./cmd/tapio-cli/

# Build the CLI binary
WORKDIR /app/cmd/tapio-cli
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tapio-cli .

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
COPY --from=builder /app/cmd/tapio-cli/tapio-cli /usr/local/bin/tapio-cli

# Copy configuration
COPY deployments/cli/config.yaml /etc/tapio/cli.yaml

# Set permissions
RUN chown -R tapio:tapio /home/tapio && \
    chmod +x /usr/local/bin/tapio-cli

# Switch to non-root user
USER tapio

# Set environment variables
ENV TAPIO_ENGINE_ENDPOINT=tapio-engine:9090
ENV TAPIO_CLI_CONFIG=/etc/tapio/cli.yaml

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD tapio-cli version || exit 1

# Default command
ENTRYPOINT ["tapio-cli"]
CMD ["--help"]

# Metadata
LABEL org.opencontainers.image.title="Tapio CLI"
LABEL org.opencontainers.image.description="Kubernetes Intelligence Platform CLI"
LABEL org.opencontainers.image.source="https://github.com/yairfalse/tapio"
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.vendor="Tapio"