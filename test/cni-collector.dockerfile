FROM golang:1.24-alpine AS builder

# Install build dependencies for eBPF
RUN apk add --no-cache \
    build-base \
    linux-headers \
    clang \
    llvm

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the CNI collector test program
RUN cd test && go build -o /build/cni-collector cni-test-main.go

# Runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Copy the collector binary
COPY --from=builder /build/cni-collector /usr/local/bin/

# Create non-root user (but we'll need root for eBPF)
RUN adduser -D -s /bin/sh collector

# For eBPF, we need to run as root, but in production we'd use capabilities
USER root

CMD ["/usr/local/bin/cni-collector"]