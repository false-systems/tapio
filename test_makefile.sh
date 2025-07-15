#!/bin/bash
# Test script to verify Makefile fixes

echo "🧪 Testing Makefile build targets..."

# Clean previous builds
echo "Cleaning previous builds..."
make clean

# Test environment validation
echo -e "\n📋 Testing environment validation..."
make validate-env

# Test basic build
echo -e "\n🔨 Testing basic build..."
make build

# Check if binary was created
if [ -f bin/tapio ]; then
    echo "✅ Binary created successfully"
    
    # Test version output
    echo -e "\n📊 Testing version output..."
    ./bin/tapio version
else
    echo "❌ Binary not created"
    exit 1
fi

# Test formatting check
echo -e "\n🎨 Testing formatting check..."
make fmt

# Test CI pipeline
echo -e "\n🚀 Testing CI pipeline..."
make ci

echo -e "\n✅ All tests passed!"