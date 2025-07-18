#!/bin/bash

# Tapio Infrastructure Audit Script
# This script helps identify potential infrastructure cleanup opportunities

echo "=== Tapio Infrastructure Audit ==="
echo

# Check for duplicate Go module definitions
echo "1. Checking for Go modules..."
find . -name "go.mod" -type f | grep -v vendor | grep -v node_modules | sort
echo

# Check for similar Dockerfile patterns
echo "2. Analyzing Dockerfiles for similarities..."
echo "Dockerfile line counts:"
find . -name "Dockerfile*" -type f | grep -v vendor | while read f; do
    echo "$f: $(wc -l < "$f") lines"
done
echo

# Find potential duplicate YAML configurations
echo "3. Checking for similar YAML files..."
echo "Services:"
find . -name "*.yaml" -o -name "*.yml" | xargs grep -l "kind: Service" 2>/dev/null | wc -l
echo "Deployments:"
find . -name "*.yaml" -o -name "*.yml" | xargs grep -l "kind: Deployment" 2>/dev/null | wc -l
echo "ConfigMaps:"
find . -name "*.yaml" -o -name "*.yml" | xargs grep -l "kind: ConfigMap" 2>/dev/null | wc -l
echo

# Check for hardcoded values that should be configurable
echo "4. Checking for hardcoded values..."
echo "Hardcoded ports:"
grep -r ":[0-9]\{4,5\}" --include="*.go" . | grep -v test | grep -v vendor | grep -E "(8080|9090|3000|5000)" | wc -l
echo "Hardcoded IPs:"
grep -r -E "[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}" --include="*.go" . | grep -v test | grep -v vendor | grep -v "127.0.0.1" | grep -v "0.0.0.0" | wc -l
echo

# Find TODO/FIXME comments related to infrastructure
echo "5. Infrastructure-related TODOs..."
grep -r "TODO\|FIXME" --include="*.go" --include="*.yaml" --include="*.yml" . | grep -i "deploy\|config\|infra\|k8s\|docker" | wc -l
echo

# Check for unused Docker images in compose files
echo "6. Docker Compose analysis..."
find . -name "docker-compose*.yml" -o -name "docker-compose*.yaml" | wc -l
echo

# Look for potential secret leaks
echo "7. Security scan..."
echo "Files containing 'secret' or 'password':"
grep -r -i "secret\|password" --include="*.yaml" --include="*.yml" --include="*.env*" . | grep -v ".git" | grep -v "vendor" | wc -l
echo

# Check for large files that might need cleanup
echo "8. Large files check (>1MB)..."
find . -type f -size +1M | grep -v ".git" | grep -v "vendor" | grep -v "node_modules" | wc -l
echo

# Summary of health and metrics endpoints
echo "9. Health and metrics endpoints summary..."
echo "Health endpoints:"
grep -r "health" --include="*.go" . | grep -E "HandleFunc|Handle|Router" | grep -v test | grep -v vendor | wc -l
echo "Metrics endpoints:"
grep -r "metrics\|prometheus" --include="*.go" . | grep -E "HandleFunc|Handle|Router" | grep -v test | grep -v vendor | wc -l
echo

echo "=== Audit Complete ==="
echo
echo "Review the counts above. High numbers may indicate areas for cleanup."
echo "For detailed results, remove the '| wc -l' from individual commands."