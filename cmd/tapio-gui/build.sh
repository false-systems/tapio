#!/bin/bash
# Build Tapio GUI with OTEL integration

set -e

echo "🚀 Building Tapio GUI with OTEL..."
echo

# Check if wails is installed
if ! command -v wails &> /dev/null; then
    echo "❌ Wails CLI not found. Installing..."
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    echo "✅ Wails installed"
fi

# Install frontend dependencies
echo "📦 Installing frontend dependencies..."
cd frontend
npm install
cd ..

# Build the application
echo "🔨 Building Tapio GUI..."
wails build

echo
echo "✅ Build complete!"
echo
echo "📍 Binary location: ./build/bin/tapio-gui"
echo
echo "🎯 Next steps:"
echo "   1. Start Jaeger: docker run -p 16686:16686 -p 4317:4317 jaegertracing/all-in-one"
echo "   2. Run GUI: ./build/bin/tapio-gui"
echo "   3. Click 'Traces' tab to see OTEL visualization!"
echo