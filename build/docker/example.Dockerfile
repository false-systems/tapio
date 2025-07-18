# Example: Building a Tapio service using base images

# Build stage
FROM tapio/base-alpine:latest AS builder

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tapio-service ./cmd/service

# Runtime stage
FROM tapio/runtime-alpine:latest

# Copy the binary from builder
COPY --from=builder /build/tapio-service /usr/local/bin/tapio-service

# Copy config files
COPY --from=builder /build/config /etc/tapio/

# Expose ports
EXPOSE 8080 8081

# Run the service
ENTRYPOINT ["/usr/local/bin/tapio-service"]