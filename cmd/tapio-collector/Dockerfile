# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev linux-headers

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the collector
RUN go build -o tapio-collector ./cmd/tapio-collector/main.go

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -u 1000 tapio

# Copy binary from builder
COPY --from=builder /build/tapio-collector /usr/local/bin/tapio-collector

# Set ownership
RUN chown tapio:tapio /usr/local/bin/tapio-collector

# Switch to non-root user (may need to run as root for eBPF)
# USER tapio

# Expose metrics port (future enhancement)
EXPOSE 9091

# Default command
ENTRYPOINT ["/usr/local/bin/tapio-collector"]

# Default arguments
CMD ["--server", "tapio-server:9090", "--otel-endpoint", "otel-collector:4317"]