#!/bin/bash

echo "=== Tapio Test Coverage Report ==="
echo "Generated on: $(date)"
echo ""

# Function to run coverage for a package group
run_coverage() {
    local pkg_group=$1
    local name=$2
    echo "=== $name ==="
    
    # Run tests with coverage
    output=$(go test -coverprofile=/tmp/coverage_${pkg_group}.out ./pkg/${pkg_group}/... 2>&1)
    
    # Extract coverage info
    if echo "$output" | grep -q "coverage:"; then
        echo "$output" | grep -E "(^ok|^FAIL|coverage:)" | sort | uniq
    else
        echo "No tests found or build failed"
    fi
    echo ""
}

# Test for test types
echo "=== Test Type Analysis ==="
echo ""

echo "Unit Tests (_test.go files):"
find ./pkg -name "*_test.go" -type f | wc -l

echo ""
echo "Integration Tests:"
find ./pkg -name "*_integration_test.go" -o -name "*_int_test.go" | wc -l

echo ""
echo "E2E Tests:"
find ./pkg -name "*_e2e_test.go" -o -name "*e2e*" -type d | wc -l

echo ""
echo "System Tests:"
find ./pkg -name "*_system_test.go" -o -name "*system*" -type d | wc -l

echo ""
echo "=== Package Coverage Analysis ==="
echo ""

# Run coverage for each major package group
run_coverage "collectors" "Collectors Package"
run_coverage "domain" "Domain Package"
run_coverage "integrations" "Integrations Package"
run_coverage "intelligence" "Intelligence Package"
run_coverage "interfaces" "Interfaces Package"
run_coverage "performance" "Performance Package"
run_coverage "persistence" "Persistence Package"

# Find packages without tests
echo "=== Packages Without Tests ==="
for dir in $(find ./pkg -type d -name "internal" -o -name "cmd" -o -name "examples" -prune -o -type d -print | sort); do
    if [ -d "$dir" ]; then
        test_count=$(find "$dir" -maxdepth 1 -name "*_test.go" 2>/dev/null | wc -l)
        if [ $test_count -eq 0 ] && [ $(find "$dir" -maxdepth 1 -name "*.go" -not -name "*_test.go" 2>/dev/null | wc -l) -gt 0 ]; then
            echo "$dir"
        fi
    fi
done

echo ""
echo "=== Test File Distribution ==="
echo ""
for pkg in collectors domain integrations intelligence interfaces performance persistence; do
    count=$(find ./pkg/$pkg -name "*_test.go" 2>/dev/null | wc -l)
    echo "$pkg: $count test files"
done