#!/bin/bash

echo "Testing build for import cycles..."

# Test pkg/collector
echo "Building pkg/collector..."
go build ./pkg/collector/... 2>&1 | head -20

echo ""
echo "Building pkg/ebpf..."
go build ./pkg/ebpf/... 2>&1 | head -20

echo ""
echo "Building pkg/collectors..."
go build ./pkg/collectors/... 2>&1 | head -20

echo ""
echo "Building pkg/collectors/ebpf..."
go build ./pkg/collectors/ebpf/... 2>&1 | head -20

echo ""
echo "Testing full build..."
go build ./... 2>&1 | grep -E "import cycle|package" | head -20