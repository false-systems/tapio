#!/bin/bash

# Tapio Architecture Verification Script
# Ensures strict 5-level dependency hierarchy

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Verifying 5-level architecture hierarchy...${NC}"

# Define the hierarchy levels
declare -A HIERARCHY=(
    ["pkg/domain"]=0
    ["pkg/collectors"]=1
    ["pkg/intelligence"]=2
    ["pkg/integrations"]=3
    ["pkg/interfaces"]=4
)

VIOLATIONS=0
VIOLATION_LIST=""

# Check each package's imports
while IFS= read -r line; do
    if [[ -z "$line" ]]; then
        continue
    fi
    
    # Parse package and imports
    PKG=$(echo "$line" | cut -d':' -f1)
    IMPORTS=$(echo "$line" | cut -d':' -f2- | tr -d '[]' | tr ' ' '\n')
    
    # Determine package level
    PKG_LEVEL=-1
    for KEY in "${!HIERARCHY[@]}"; do
        if [[ "$PKG" == *"$KEY"* ]]; then
            PKG_LEVEL=${HIERARCHY[$KEY]}
            break
        fi
    done
    
    # Skip if not in our hierarchy
    if [[ $PKG_LEVEL -eq -1 ]]; then
        continue
    fi
    
    # Check each import
    while IFS= read -r IMPORT; do
        if [[ -z "$IMPORT" ]]; then
            continue
        fi
        
        # Check if import is from our project
        if [[ "$IMPORT" != *"github.com/yairfalse/tapio/pkg"* ]]; then
            continue
        fi
        
        # Determine import level
        IMPORT_LEVEL=-1
        for KEY in "${!HIERARCHY[@]}"; do
            if [[ "$IMPORT" == *"$KEY"* ]]; then
                IMPORT_LEVEL=${HIERARCHY[$KEY]}
                break
            fi
        done
        
        # Check for violation
        if [[ $IMPORT_LEVEL -gt $PKG_LEVEL ]]; then
            VIOLATIONS=$((VIOLATIONS + 1))
            VIOLATION_MSG="$PKG (Level $PKG_LEVEL) imports $IMPORT (Level $IMPORT_LEVEL)"
            VIOLATION_LIST="$VIOLATION_LIST\n  - $VIOLATION_MSG"
            echo -e "${RED}VIOLATION: $VIOLATION_MSG${NC}"
        fi
    done <<< "$IMPORTS"
done < <(go list -f '{{.ImportPath}}: {{.Imports}}' ./... 2>/dev/null | grep "github.com/yairfalse/tapio")

if [[ $VIOLATIONS -gt 0 ]]; then
    echo -e "${RED}❌ Architecture verification FAILED${NC}"
    echo -e "${RED}Found $VIOLATIONS architecture violations:${NC}"
    echo -e "$VIOLATION_LIST"
    exit 1
else
    echo -e "${GREEN}✅ Architecture verification PASSED${NC}"
    echo -e "${GREEN}All packages follow the 5-level hierarchy${NC}"
fi