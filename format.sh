#!/bin/bash

# Format all Go files
echo "Formatting Go files..."
find . -name "*.go" -not -path "./vendor/*" -not -path "./pkg/mod/*" -exec gofmt -s -w {} \;
echo "Formatting complete!"