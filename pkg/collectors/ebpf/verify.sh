#!/bin/bash

echo "=== eBPF Collector Module Verification ==="
echo

echo "1. Checking module structure..."
if [ -f go.mod ] && [ -d core ] && [ -d internal ] && [ -d linux ] && [ -d stub ]; then
    echo "✓ Module structure is correct"
else
    echo "✗ Module structure is incorrect"
    exit 1
fi

echo
echo "2. Checking dependencies..."
if ! grep -q "github.com/yairfalse/tapio/pkg/domain" go.mod; then
    echo "✗ Missing domain dependency"
    exit 1
fi

if ! grep -q "github.com/cilium/ebpf" go.mod; then
    echo "✗ Missing cilium/ebpf dependency"
    exit 1
fi

echo "✓ Dependencies are correct"

echo
echo "3. Building module..."
if go build ./...; then
    echo "✓ Module builds successfully"
else
    echo "✗ Module build failed"
    exit 1
fi

echo
echo "4. Running tests..."
if go test ./...; then
    echo "✓ Tests pass"
else
    echo "✗ Tests failed"
    exit 1
fi

echo
echo "5. Checking for architecture violations..."

# Check for forbidden imports
FORBIDDEN_IMPORTS=(
    "pkg/collectors/unified"
    "pkg/ebpf"
    "pkg/logging"
    "pkg/correlation"
    "pkg/intelligence"
    "pkg/integrations"
    "pkg/interfaces"
)

for import in "${FORBIDDEN_IMPORTS[@]}"; do
    if grep -r "$import" --include="*.go" . | grep -v "^./verify.sh"; then
        echo "✗ Found forbidden import: $import"
        exit 1
    fi
done

echo "✓ No architecture violations found"

echo
echo "6. Verifying standalone operation..."
cd examples/basic 2>/dev/null || mkdir -p examples/basic
if [ -f main.go ]; then
    if go build -o /tmp/ebpf-example main.go; then
        echo "✓ Example builds successfully"
        rm -f /tmp/ebpf-example
    else
        echo "✗ Example build failed"
    fi
else
    echo "- No example found (optional)"
fi

echo
echo "=== Verification Complete ==="
echo "The eBPF collector module complies with all architectural requirements!"