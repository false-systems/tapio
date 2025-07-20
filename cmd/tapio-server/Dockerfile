# Tapio Server - Production Ready Container
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make ca-certificates

WORKDIR /build

# Copy go modules first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -X main.version=docker" \
    -o tapio-server ./cmd/tapio-server

# Final stage - minimal runtime
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 tapio && \
    adduser -u 1000 -G tapio -D tapio

# Create required directories
RUN mkdir -p /etc/tapio /var/lib/tapio /var/log/tapio && \
    chown -R tapio:tapio /etc/tapio /var/lib/tapio /var/log/tapio

# Copy binary
COPY --from=builder /build/tapio-server /usr/local/bin/tapio-server

# Switch to non-root user
USER tapio

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD tapio-server health || exit 1

# Expose ports
EXPOSE 8080 9090

ENTRYPOINT ["tapio-server"]
CMD ["serve"]