# Minimal runtime image for Tapio services
FROM alpine:3.19

# Install runtime dependencies only
RUN apk add --no-cache \
    ca-certificates \
    tzdata

# Create non-root user
RUN adduser -D -u 10001 tapio

# Set up common directories
RUN mkdir -p /etc/tapio /var/log/tapio /var/lib/tapio && \
    chown -R tapio:tapio /etc/tapio /var/log/tapio /var/lib/tapio

# Switch to non-root user
USER tapio

# Set timezone
ENV TZ=UTC

# Health check endpoint port
EXPOSE 8081