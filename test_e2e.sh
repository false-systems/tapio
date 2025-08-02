#!/bin/bash

echo "=== Testing Tapio E2E Flow ==="

# Check if NATS is running
if ! nc -z localhost 4222 2>/dev/null; then
    echo "❌ NATS not running on localhost:4222"
    echo "Please start NATS: docker run -p 4222:4222 nats:latest -js"
    exit 1
fi
echo "✅ NATS is running"

# Check if minikube is running
if ! minikube status | grep -q "Running"; then
    echo "❌ Minikube not running"
    echo "Please start minikube: minikube start"
    exit 1
fi
echo "✅ Minikube is running"

# Start correlation service in background
echo "Starting correlation service..."
go run cmd/correlation-service/main.go &
CORR_PID=$!
sleep 3

# Start tapio with kubeapi collector
echo "Starting tapio with kubeapi collector..."
./tapio-fixed -nats nats://localhost:4222 &
TAPIO_PID=$!
sleep 3

# Create a test pod to generate events
echo "Creating test pod to generate events..."
kubectl create deployment test-nginx --image=nginx:latest

# Wait and check
sleep 5

# Check if events are flowing
echo "Checking NATS for events..."
nats sub 'traces.>.' --count=5 --timeout=10s

# Cleanup
echo "Cleaning up..."
kubectl delete deployment test-nginx
kill $TAPIO_PID $CORR_PID

echo "Test complete!"