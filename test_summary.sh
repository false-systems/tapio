#!/bin/bash

echo "Tapio Test Summary Report"
echo "========================="
echo ""

# Count packages
total_packages=$(find pkg -type d -name "internal" -prune -o -type d -print | grep -v internal | wc -l)
packages_with_tests=$(find pkg -name "*_test.go" | xargs dirname | sort -u | wc -l)
test_files=$(find pkg -name "*_test.go" | wc -l)

echo "Total packages: $total_packages"
echo "Packages with tests: $packages_with_tests"
echo "Total test files: $test_files"
echo ""

echo "Test file breakdown by major package:"
echo "------------------------------------"
for dir in pkg/*/; do
    if [ -d "$dir" ]; then
        count=$(find "$dir" -name "*_test.go" | wc -l)
        if [ $count -gt 0 ]; then
            printf "%-30s %3d test files\n" "$dir" "$count"
        fi
    fi
done

echo ""
echo "Packages without any tests:"
echo "---------------------------"
for dir in $(find pkg -type d -maxdepth 3 | sort); do
    if [ -d "$dir" ] && [ ! -f "$dir/doc.go" ]; then
        test_count=$(find "$dir" -maxdepth 1 -name "*_test.go" 2>/dev/null | wc -l)
        go_count=$(find "$dir" -maxdepth 1 -name "*.go" -not -name "*_test.go" 2>/dev/null | wc -l)
        if [ $go_count -gt 0 ] && [ $test_count -eq 0 ]; then
            echo "$dir"
        fi
    fi
done