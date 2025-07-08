#!/bin/bash
# Setup Kind cluster for E2E testing

set -e

CLUSTER_NAME="tapio-e2e"
KUBECONFIG="${HOME}/.kube/tapio-e2e-config"

echo "Setting up Kind cluster for Tapio E2E tests..."

# Check if kind is installed
if ! command -v kind &> /dev/null; then
    echo "Error: kind is not installed"
    echo "Install with: brew install kind (macOS) or check https://kind.sigs.k8s.io/"
    exit 1
fi

# Delete existing cluster if it exists
if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    echo "Deleting existing cluster..."
    kind delete cluster --name="${CLUSTER_NAME}"
fi

# Create kind cluster configuration
cat <<EOF > /tmp/kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
    - |
      kind: InitConfiguration
      nodeRegistration:
        kubeletExtraArgs:
          node-labels: "tapio-e2e=true"
  - role: worker
    kubeadmConfigPatches:
    - |
      kind: JoinConfiguration
      nodeRegistration:
        kubeletExtraArgs:
          node-labels: "tapio-e2e=true"
  - role: worker
    kubeadmConfigPatches:
    - |
      kind: JoinConfiguration
      nodeRegistration:
        kubeletExtraArgs:
          node-labels: "tapio-e2e=true"
EOF

# Create cluster
echo "Creating Kind cluster..."
kind create cluster \
    --name="${CLUSTER_NAME}" \
    --config=/tmp/kind-config.yaml \
    --kubeconfig="${KUBECONFIG}"

# Export kubeconfig
export KUBECONFIG="${KUBECONFIG}"

# Wait for cluster to be ready
echo "Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=300s

# Install metrics-server for resource metrics
echo "Installing metrics-server..."
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Patch metrics-server for Kind
kubectl patch deployment metrics-server -n kube-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--kubelet-insecure-tls"
  }
]'

# Wait for metrics-server to be ready
kubectl wait --for=condition=Ready pods -n kube-system -l k8s-app=metrics-server --timeout=300s

echo ""
echo "Kind cluster '${CLUSTER_NAME}' is ready!"
echo ""
echo "To use this cluster:"
echo "  export KUBECONFIG=${KUBECONFIG}"
echo ""
echo "To run E2E tests:"
echo "  go test -v -tags=e2e ./test/e2e/..."
echo ""
echo "To delete the cluster:"
echo "  kind delete cluster --name=${CLUSTER_NAME}"