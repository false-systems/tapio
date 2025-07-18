# Base Alpine image for Tapio Go services
FROM golang:1.21-alpine AS base-alpine

# Install common build dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    make \
    gcc \
    musl-dev

# Set up workspace
WORKDIR /build

# Copy go mod files for better caching
ONBUILD COPY go.mod go.sum ./
ONBUILD RUN go mod download

# Install common Go tools
RUN go install github.com/cosmtrek/air@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest

# Create non-root user for runtime
RUN adduser -D -u 10001 tapio

# Set timezone
ENV TZ=UTC