#!/bin/bash

echo "Testing NATS in Kubernetes..."

# Create a test pod to interact with NATS
kubectl run nats-test -n tapio-system --image=natsio/nats-box:latest --rm -it --restart=Never -- \
  nats --server=nats://nats:4222 stream ls

echo ""
echo "Creating test stream..."
kubectl run nats-create-stream -n tapio-system --image=natsio/nats-box:latest --rm -it --restart=Never -- \
  nats --server=nats://nats:4222 stream add TRACES \
    --subjects="traces.>" \
    --storage=file \
    --retention=limits \
    --max-age=24h \
    --defaults

echo ""
echo "Publishing test event..."
kubectl run nats-pub -n tapio-system --image=natsio/nats-box:latest --rm -it --restart=Never -- \
  nats --server=nats://nats:4222 pub traces.test123 "{'event': 'test', 'trace_id': 'test123'}"

echo ""
echo "NATS monitoring available at: http://localhost:8222"
echo "JetStream info at: http://localhost:8222/jsz"