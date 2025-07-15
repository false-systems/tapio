#!/bin/bash

# Script to clean up conflicting go.mod files and consolidate to single module

echo "Cleaning up conflicting go.mod files..."

# List of go.mod files to remove
FILES_TO_REMOVE=(
    "cmd/tapio-cli/go.mod"
    "cmd/tapio-cli/go.sum"
    "cmd/tapio-engine/go.mod"
    "cmd/tapio-engine/go.sum"
    "cmd/tapio-gui/go.mod"
    "cmd/tapio-gui/go.sum"
    "cmd/plugins/tapio-otel/go.mod"
    "cmd/plugins/tapio-otel/go.sum"
    "cmd/plugins/tapio-prometheus/go.mod"
    "cmd/plugins/tapio-prometheus/go.sum"
    "gui/tapio-gui/go.mod"
    "gui/tapio-gui/go.sum"
)

# Remove each file
for file in "${FILES_TO_REMOVE[@]}"; do
    if [ -f "$file" ]; then
        echo "Removing $file"
        rm -f "$file"
    else
        echo "File not found (already removed): $file"
    fi
done

echo "Cleanup complete!"