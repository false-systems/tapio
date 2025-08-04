# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o k8sgrapher ./pkg/integrations/k8sgrapher/cmd/main.go

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -g '' appuser

# Copy binary from builder
COPY --from=builder /app/k8sgrapher /usr/local/bin/k8sgrapher

# Switch to non-root user
USER appuser

# Expose metrics port
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["k8sgrapher"]