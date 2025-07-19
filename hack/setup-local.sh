#!/bin/bash
set -e

echo "🚀 Setting up Tapio local development with Skaffold..."

# Check requirements
echo "📋 Checking requirements..."
command -v skaffold >/dev/null 2>&1 || { echo "❌ Skaffold not found. Run: brew install skaffold"; exit 1; }
command -v minikube >/dev/null 2>&1 || { echo "❌ Minikube not found. Run: brew install minikube"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "❌ kubectl not found. Run: brew install kubectl"; exit 1; }

# Start minikube if not running
if ! minikube status >/dev/null 2>&1; then
    echo "🔧 Starting minikube..."
    minikube start --cpus=4 --memory=8192 --driver=docker
    minikube addons enable metrics-server
else
    echo "✅ Minikube already running"
fi

# Use minikube's Docker daemon
echo "🐳 Configuring Docker environment..."
eval $(minikube docker-env)

# Create namespace if it doesn't exist
echo "📦 Creating namespace..."
kubectl create namespace tapio-system --dry-run=client -o yaml | kubectl apply -f -

echo "✅ Setup complete!"
echo ""
echo "To start development:"
echo "  skaffold dev"
echo ""
echo "Services will be available at:"
echo "  - Tapio API: http://localhost:8080"
echo "  - Tapio gRPC: http://localhost:9090"
echo "  - Jaeger UI: http://localhost:16686"
echo ""
echo "To run the GUI:"
echo "  tapio gui --api-endpoint=http://localhost:8080"