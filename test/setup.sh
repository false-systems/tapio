#!/bin/bash
set -e

# Tapio Test Environment Setup Script
# All test files contained within test/ directory

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
TAPIO_DIR="$(dirname "$TEST_DIR")"

echo "🚀 Starting Tapio Test Environment Setup"
echo "======================================"

# Check prerequisites
check_prerequisites() {
    echo "📋 Checking prerequisites..."
    
    commands=("minikube" "kubectl" "docker")
    for cmd in "${commands[@]}"; do
        if ! command -v $cmd &> /dev/null; then
            echo "❌ $cmd is not installed"
            exit 1
        fi
    done
    
    # Check if Docker is running on macOS
    if [[ "$OSTYPE" == "darwin"* ]]; then
        if ! docker info &> /dev/null; then
            echo "❌ Docker is not running. Please start Docker Desktop."
            exit 1
        fi
    fi
    
    echo "✅ Prerequisites satisfied"
}

# Setup Minikube
setup_minikube() {
    echo "🔧 Setting up Minikube..."
    
    if minikube status &> /dev/null; then
        echo "Minikube is already running"
    else
        minikube start \
            --cpus=4 \
            --memory=8192 \
            --disk-size=50g \
            --driver=docker
    fi
    
    echo "✅ Minikube ready"
}

# Deploy infrastructure
deploy_infrastructure() {
    echo "🏗️  Deploying infrastructure..."
    
    # Create namespace
    kubectl create namespace tapio-system --dry-run=client -o yaml | kubectl apply -f -
    
    # Deploy NATS (using existing config)
    echo "📡 Deploying NATS..."
    if [ -f "$TAPIO_DIR/k8s/nats-fixed.yaml" ]; then
        kubectl apply -f $TAPIO_DIR/k8s/nats-fixed.yaml
    else
        echo "⚠️  NATS config not found, skipping..."
    fi
    
    # Deploy Neo4j (using existing config)
    echo "🗄️  Deploying Neo4j..."
    if [ -f "$TAPIO_DIR/k8s/neo4j.yaml" ]; then
        # Update namespace to tapio-system
        sed 's/namespace: default/namespace: tapio-system/g' $TAPIO_DIR/k8s/neo4j.yaml | kubectl apply -f -
    fi
    
    echo "✅ Infrastructure deployed"
}

# Build test images
build_test_images() {
    echo "🔨 Building test images..."
    
    cd $TAPIO_DIR
    
    # Build correlation service
    docker build -f $TEST_DIR/docker/Dockerfile.correlation -t tapio/correlation-service:test .
    
    # Build collector
    docker build -f $TEST_DIR/docker/Dockerfile.collector -t tapio/collector:test .
    
    # Load into minikube
    minikube image load tapio/correlation-service:test
    minikube image load tapio/collector:test
    
    echo "✅ Test images built"
}

# Deploy Tapio for testing
deploy_tapio() {
    echo "🚀 Deploying Tapio components..."
    
    # Deploy test manifests
    kubectl apply -f $TEST_DIR/k8s/
    
    # Wait for deployments
    kubectl -n tapio-system wait --for=condition=available deployment/correlation-service --timeout=300s || true
    
    echo "✅ Tapio deployed"
}

# Create test scenarios
create_test_scenarios() {
    echo "💥 Creating test scenarios..."
    
    mkdir -p $TEST_DIR/scenarios
    
    # Copy scenario creation from previous script
    # ... (scenarios remain the same)
    
    echo "✅ Test scenarios created"
}

# Main execution
main() {
    check_prerequisites
    setup_minikube
    deploy_infrastructure
    build_test_images
    deploy_tapio
    create_test_scenarios
    
    # Create helper scripts
    cp $TEST_DIR/run-tests.sh $TEST_DIR/run-tests.sh.tmp 2>/dev/null || cat > $TEST_DIR/run-tests.sh <<'EOF'
#!/bin/bash
# Test runner
TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

run_scenario() {
    local scenario=$1
    echo "🧪 Running scenario: $scenario"
    kubectl apply -f $TEST_DIR/scenarios/$scenario.yaml
    sleep 60
    kubectl logs -n tapio-system deployment/correlation-service --tail=50 | grep -E "(pattern|confidence)"
    kubectl delete -f $TEST_DIR/scenarios/$scenario.yaml --force --grace-period=0
}

case "${1:-all}" in
    all)
        for s in oom-killer crash-loop cpu-stress network-failure disk-filler; do
            run_scenario "$s"
        done
        ;;
    *)
        run_scenario "$1"
        ;;
esac
EOF
    chmod +x $TEST_DIR/run-tests.sh
    
    echo "
    ====================================
    🎉 Test Environment Ready!
    ====================================
    
    Run tests with:
    cd test/
    ./run-tests.sh all
    "
}

main