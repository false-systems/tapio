# Base Debian image for Tapio services requiring eBPF or system tools
FROM golang:1.21-bullseye AS base-debian

# Install common build dependencies
RUN apt-get update && apt-get install -y \
    git \
    ca-certificates \
    tzdata \
    make \
    gcc \
    libc6-dev \
    libelf-dev \
    llvm \
    clang \
    && rm -rf /var/lib/apt/lists/*

# Set up workspace
WORKDIR /build

# Copy go mod files for better caching
ONBUILD COPY go.mod go.sum ./
ONBUILD RUN go mod download

# Install common Go tools
RUN go install github.com/cosmtrek/air@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest

# Create non-root user for runtime
RUN useradd -m -u 10001 -s /bin/bash tapio

# Set timezone
ENV TZ=UTC