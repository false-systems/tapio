#!/bin/bash
set -e

echo "🔍 Checking for map[string]interface{} violations..."

# Find all violations (excluding vendor and docs that show what NOT to do)
VIOLATIONS=$(grep -r "map\[string\]interface{}" \
    --include="*.go" \
    --exclude-dir=vendor \
    --exclude-dir=.git \
    . 2>/dev/null | \
    grep -v "//" | \
    wc -l)

if [ "$VIOLATIONS" -gt "0" ]; then
    echo "❌ FAILED: Found $VIOLATIONS instances of map[string]interface{}"
    echo ""
    echo "Violations by file:"
    grep -r "map\[string\]interface{}" \
        --include="*.go" \
        --exclude-dir=vendor \
        --exclude-dir=.git \
        . 2>/dev/null | \
        grep -v "//" | \
        cut -d: -f1 | sort | uniq -c | sort -rn | head -20
    
    echo ""
    echo "❌ Fix these violations before committing!"
    echo "Replace with strongly-typed structs or specific types."
    exit 1
fi

echo "✅ PASSED: No map[string]interface{} violations found"