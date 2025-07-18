#!/bin/bash

# analyze-imports.sh - Analyze all imports in the codebase
# Helps identify architectural violations and circular dependencies

set -euo pipefail

echo "🔍 Analyzing Tapio Import Structure"
echo "==================================="
echo ""

# Create temp directory for analysis
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Find all Go files and extract imports
echo "📊 Collecting import data..."

find . -name "*.go" -type f -not -path "./vendor/*" -not -path "./.git/*" | while read -r file; do
    # Extract package name
    package=$(grep "^package " "$file" | head -1 | awk '{print $2}')
    
    # Extract imports
    awk '/^import \(/{flag=1;next}/^\)/{flag=0}flag' "$file" | grep -v "^$" | tr -d '\t"' > "$TEMP_DIR/imports_multi.tmp" 2>/dev/null || true
    awk '/^import "/{print $2}' "$file" | tr -d '"' >> "$TEMP_DIR/imports_multi.tmp" 2>/dev/null || true
    
    # Get directory
    dir=$(dirname "$file")
    
    # Save imports for this file
    if [[ -s "$TEMP_DIR/imports_multi.tmp" ]]; then
        while IFS= read -r import; do
            echo "$dir|$package|$import" >> "$TEMP_DIR/all_imports.txt"
        done < "$TEMP_DIR/imports_multi.tmp"
    fi
    
    rm -f "$TEMP_DIR/imports_multi.tmp"
done

# Analyze internal imports
echo ""
echo "📦 Internal Import Analysis"
echo "---------------------------"

# Group by package
grep "github.com/falseyair/tapio" "$TEMP_DIR/all_imports.txt" 2>/dev/null | sort | uniq > "$TEMP_DIR/internal_imports.txt" || true

# Count imports by level
echo ""
echo "🏗️  Import Count by Architectural Level:"
echo ""

for level in domain collectors intelligence integrations interfaces; do
    count=$(grep -c "github.com/falseyair/tapio/pkg/$level" "$TEMP_DIR/internal_imports.txt" 2>/dev/null || echo "0")
    printf "%-15s: %d imports\n" "$level" "$count"
done

# Find potential circular dependencies
echo ""
echo "🔄 Checking for Potential Circular Dependencies"
echo "-----------------------------------------------"

# Build dependency graph
declare -A deps
while IFS='|' read -r dir package import; do
    if [[ "$import" == *"github.com/falseyair/tapio"* ]]; then
        # Normalize paths
        from_path=$(echo "$dir" | sed 's|^\./||')
        to_path=$(echo "$import" | sed 's|github.com/falseyair/tapio/||')
        
        # Skip self-imports
        if [[ "$from_path" != "$to_path"* ]]; then
            deps["$from_path"]="${deps["$from_path"]} $to_path"
        fi
    fi
done < "$TEMP_DIR/all_imports.txt"

# Simple cycle detection (depth 2)
CYCLES_FOUND=0
for from in "${!deps[@]}"; do
    for to in ${deps[$from]}; do
        if [[ -n "${deps[$to]}" ]]; then
            for back in ${deps[$to]}; do
                if [[ "$back" == "$from" ]]; then
                    echo "⚠️  Circular dependency: $from ←→ $to"
                    ((CYCLES_FOUND++))
                fi
            done
        fi
    done
done

if [[ $CYCLES_FOUND -eq 0 ]]; then
    echo "✅ No circular dependencies detected"
fi

# Find cross-level imports
echo ""
echo "🚫 Cross-Level Import Violations"
echo "--------------------------------"

VIOLATIONS_FOUND=0

# Check collectors importing from other collectors
echo ""
echo "Level 1 (Collectors) violations:"
grep "^pkg/collectors/" "$TEMP_DIR/internal_imports.txt" 2>/dev/null | grep "|github.com/falseyair/tapio/pkg/collectors/" | while IFS='|' read -r from pkg to; do
    from_collector=$(echo "$from" | cut -d'/' -f3)
    to_collector=$(echo "$to" | cut -d'/' -f5)
    if [[ "$from_collector" != "$to_collector" ]]; then
        echo "❌ $from imports $to"
        ((VIOLATIONS_FOUND++))
    fi
done || true

# Check intelligence importing from interfaces
echo ""
echo "Level 2 (Intelligence) violations:"
grep "^pkg/intelligence/" "$TEMP_DIR/internal_imports.txt" 2>/dev/null | grep -E "github.com/falseyair/tapio/pkg/(interfaces|integrations)" | while IFS='|' read -r from pkg to; do
    echo "❌ $from imports $to (higher level)"
    ((VIOLATIONS_FOUND++))
done || true

# Check integrations importing from interfaces
echo ""
echo "Level 3 (Integrations) violations:"
grep "^pkg/integrations/" "$TEMP_DIR/internal_imports.txt" 2>/dev/null | grep "github.com/falseyair/tapio/pkg/interfaces" | while IFS='|' read -r from pkg to; do
    echo "❌ $from imports $to (higher level)"
    ((VIOLATIONS_FOUND++))
done || true

if [[ $VIOLATIONS_FOUND -eq 0 ]]; then
    echo "✅ No cross-level violations detected"
fi

# Identify orphaned packages
echo ""
echo "🔍 Packages Outside Standard Architecture"
echo "-----------------------------------------"

find pkg -type d -name "*.go" -prune -o -type d -print | grep -v -E "(domain|collectors|intelligence|integrations|interfaces)" | grep -v "^pkg$" | while read -r dir; do
    if [[ -f "$dir/go.mod" ]] || ls "$dir"/*.go &>/dev/null; then
        echo "❓ $dir"
    fi
done

# Summary statistics
echo ""
echo "📊 Summary Statistics"
echo "--------------------"

total_files=$(find . -name "*.go" -type f -not -path "./vendor/*" | wc -l)
total_imports=$(wc -l < "$TEMP_DIR/all_imports.txt" 2>/dev/null || echo "0")
internal_imports=$(wc -l < "$TEMP_DIR/internal_imports.txt" 2>/dev/null || echo "0")

echo "Total Go files: $total_files"
echo "Total imports: $total_imports"
echo "Internal imports: $internal_imports"
echo "Circular dependencies found: $CYCLES_FOUND"

# Save detailed report
REPORT_FILE="docs/migration/import-analysis-$(date +%Y%m%d-%H%M%S).txt"
mkdir -p docs/migration
cp "$TEMP_DIR/internal_imports.txt" "$REPORT_FILE"

echo ""
echo "💾 Detailed report saved to: $REPORT_FILE"