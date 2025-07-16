#!/bin/bash
# Quick test script to verify OTEL connection

set -e

echo "🚀 Starting Tapio OTEL Connection Test"
echo

# Check if docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

# Start Jaeger if not already running
if ! docker ps | grep -q tapio-test-jaeger; then
    echo "📦 Starting Jaeger..."
    docker-compose up -d
    echo "⏳ Waiting for Jaeger to be ready..."
    sleep 5
else
    echo "✅ Jaeger is already running"
fi

# Check Jaeger health
echo "🔍 Checking Jaeger health..."
if curl -s http://localhost:16686 > /dev/null; then
    echo "✅ Jaeger UI is accessible"
else
    echo "❌ Cannot reach Jaeger UI at http://localhost:16686"
    exit 1
fi

# Run the test
echo
echo "🧪 Running OTEL connection test..."
go run test_otel_connection.go

echo
echo "📊 View your traces at: http://localhost:16686"
echo "   Service: tapio-otel-test"
echo
echo "🎯 Next steps:"
echo "   1. Open Jaeger UI"
echo "   2. Select 'tapio-otel-test' service"
echo "   3. Click 'Find Traces'"
echo "   4. Explore the rich Tapio intelligence data!"

# Optionally open browser
if command -v open > /dev/null; then
    echo
    read -p "Open Jaeger UI in browser? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        open http://localhost:16686
    fi
fi