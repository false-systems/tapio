#!/bin/bash

# Script to remove experimental build tags from Go files

echo "Removing experimental build tags from Go files..."

# Find all Go files with experimental tags
files=$(find . -name "*.go" -type f | xargs grep -l "^//go:build experimental" | grep -v vendor)

for file in $files; do
    echo "Processing: $file"
    
    # Remove the build tags (both formats)
    # This handles both //go:build experimental and // +build experimental
    sed -i '' '/^\/\/go:build experimental$/d' "$file"
    sed -i '' '/^\/\/ +build experimental$/d' "$file"
    
    # Remove any blank lines at the start of the file after removing tags
    sed -i '' '/./,$!d' "$file"
done

echo "Done! Removed experimental tags from $(echo "$files" | wc -l | tr -d ' ') files"