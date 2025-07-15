# Multi-stage build for Tapio
# This Dockerfile supports building with and without eBPF

# Stage 1: eBPF builder (Linux only)
FROM --platform=linux/amd64 golang:1.21-bullseye AS ebpf-builder

# Install dependencies for eBPF compilation
RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    linux-headers-amd64 \
    make \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy eBPF source files
COPY ebpf/ ebpf/
COPY go.mod go.sum ./

# Install Go dependencies for bpf2go
RUN go mod download

# Generate eBPF bindings
WORKDIR /build/ebpf
RUN make generate || echo "eBPF generation failed, continuing with stubs"

# Stage 2: Go builder
FROM golang:1.21-alpine AS go-builder

# Install build dependencies
RUN apk add --no-cache git make

WORKDIR /build

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy generated eBPF bindings from previous stage
COPY --from=ebpf-builder /build/pkg/ebpf/*_bpf*.go pkg/ebpf/ 2>/dev/null || true
COPY --from=ebpf-builder /build/pkg/ebpf/*_bpf*.o pkg/ebpf/ 2>/dev/null || true

# Determine build tags based on target platform
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG BUILD_TAGS=""

# Set build tags for Linux
RUN if [ "$TARGETOS" = "linux" ]; then \
        BUILD_TAGS="ebpf"; \
    fi

# Build the binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags "${BUILD_TAGS}" \
    -ldflags="-w -s -X github.com/yairfalse/tapio/internal/cli.version=docker" \
    -o tapio ./cmd/tapio

# Stage 3: Final image
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -g 1000 tapio && \
    adduser -u 1000 -G tapio -D tapio

# Copy binary from builder
COPY --from=go-builder /build/tapio /usr/local/bin/tapio

# Switch to non-root user (eBPF will require privileges at runtime)
USER tapio

ENTRYPOINT ["tapio"]
CMD ["--help"]