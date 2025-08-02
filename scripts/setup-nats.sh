#!/bin/bash

# Setup NATS for Tapio development

echo "Starting NATS server with JetStream..."
docker-compose -f docker-compose.nats.yml up -d nats

echo "Waiting for NATS to be ready..."
sleep 5

echo "Creating JetStream streams..."
docker exec tapio-nats-box nats stream add TRACES \
  --subjects "traces.>" \
  --storage file \
  --retention limits \
  --max-msgs=-1 \
  --max-bytes=-1 \
  --max-age=24h \
  --max-msg-size=-1 \
  --duplicates=30m \
  --no-ack \
  --replicas=1 \
  --defaults

echo "Creating consumer for correlation..."
docker exec tapio-nats-box nats consumer add TRACES CORRELATION \
  --filter "traces.>" \
  --ack explicit \
  --pull \
  --deliver all \
  --max-deliver=-1 \
  --sample 100 \
  --defaults

echo "NATS setup complete!"
echo "Monitor at: http://localhost:8222/jsz"