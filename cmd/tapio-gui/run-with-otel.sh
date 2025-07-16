#!/bin/bash
# Run Tapio GUI with OTEL backend

set -e

echo "🚀 Starting Tapio GUI with OTEL..."
echo

# Check if Jaeger is running
if ! curl -s http://localhost:16686 > /dev/null; then
    echo "📦 Starting Jaeger..."
    docker run -d \
        --name tapio-jaeger \
        -p 16686:16686 \
        -p 4317:4317 \
        -p 4318:4318 \
        jaegertracing/all-in-one:latest
    
    echo "⏳ Waiting for Jaeger to be ready..."
    sleep 5
    
    echo "✅ Jaeger started"
    echo "   UI: http://localhost:16686"
    echo
else
    echo "✅ Jaeger is already running"
fi

# Check if binary exists
if [ ! -f "./build/bin/tapio-gui" ]; then
    echo "❌ Binary not found. Building..."
    ./build.sh
fi

# Run the GUI
echo "🎨 Starting Tapio GUI..."
echo
./build/bin/tapio-gui