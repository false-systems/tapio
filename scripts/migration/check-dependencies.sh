#!/bin/bash

# check-dependencies.sh - Verify Tapio architecture compliance
# This script ensures that each architectural level only imports from allowed levels

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🔍 Checking Tapio Architecture Compliance..."
echo "==========================================="

VIOLATIONS=0

# Function to check imports for a module
check_module_imports() {
    local module_path=$1
    local level=$2
    local allowed_imports=$3
    
    if [[ ! -f "$module_path/go.mod" ]]; then
        return 0
    fi
    
    echo -e "\n📦 Checking $module_path (Level $level)"
    
    cd "$module_path"
    
    # Get all imports from Go files
    local imports=$(go list -deps ./... 2>/dev/null | grep "github.com/falseyair/tapio" | grep -v "^$(go list -m)" || true)
    
    if [[ -z "$imports" ]]; then
        echo -e "${GREEN}✓ No internal imports${NC}"
        cd - > /dev/null
        return 0
    fi
    
    # Check each import
    while IFS= read -r import; do
        local valid=false
        
        # Check if import is in allowed list
        if [[ "$allowed_imports" == "none" ]]; then
            echo -e "${RED}✗ Forbidden import: $import${NC}"
            ((VIOLATIONS++))
        else
            for allowed in $allowed_imports; do
                if [[ "$import" == *"$allowed"* ]]; then
                    valid=true
                    break
                fi
            done
            
            if $valid; then
                echo -e "${GREEN}✓ Valid import: $import${NC}"
            else
                echo -e "${RED}✗ Forbidden import: $import${NC}"
                echo -e "  ${YELLOW}Allowed: $allowed_imports${NC}"
                ((VIOLATIONS++))
            fi
        fi
    done <<< "$imports"
    
    cd - > /dev/null
}

# Level 0: Domain (no dependencies)
echo -e "\n${YELLOW}Level 0: Domain Layer${NC}"
check_module_imports "pkg/domain" 0 "none"

# Level 1: Collectors (only domain)
echo -e "\n${YELLOW}Level 1: Collectors Layer${NC}"
for collector in pkg/collectors/*; do
    if [[ -d "$collector" ]]; then
        check_module_imports "$collector" 1 "pkg/domain"
    fi
done

# Level 2: Intelligence (domain + collectors)
echo -e "\n${YELLOW}Level 2: Intelligence Layer${NC}"
for intel in pkg/intelligence/*; do
    if [[ -d "$intel" ]]; then
        check_module_imports "$intel" 2 "pkg/domain pkg/collectors"
    fi
done

# Level 3: Integrations (domain + collectors + intelligence)
echo -e "\n${YELLOW}Level 3: Integrations Layer${NC}"
for integration in pkg/integrations/*; do
    if [[ -d "$integration" ]]; then
        check_module_imports "$integration" 3 "pkg/domain pkg/collectors pkg/intelligence"
    fi
done

# Level 4: Interfaces (all lower levels)
echo -e "\n${YELLOW}Level 4: Interfaces Layer${NC}"
for interface in pkg/interfaces/*; do
    if [[ -d "$interface" ]]; then
        check_module_imports "$interface" 4 "pkg/domain pkg/collectors pkg/intelligence pkg/integrations"
    fi
done

# Check for cross-imports at same level
echo -e "\n${YELLOW}Checking for cross-imports at same level...${NC}"

check_same_level_imports() {
    local level_path=$1
    local level_name=$2
    
    local modules=()
    for module in $level_path/*; do
        if [[ -d "$module" && -f "$module/go.mod" ]]; then
            modules+=("$(basename "$module")")
        fi
    done
    
    for module in $level_path/*; do
        if [[ -d "$module" && -f "$module/go.mod" ]]; then
            cd "$module"
            local module_name=$(basename "$module")
            
            for other_module in "${modules[@]}"; do
                if [[ "$other_module" != "$module_name" ]]; then
                    if go list -deps ./... 2>/dev/null | grep -q "$level_path/$other_module"; then
                        echo -e "${RED}✗ Cross-import detected: $module_name imports $other_module${NC}"
                        ((VIOLATIONS++))
                    fi
                fi
            done
            
            cd - > /dev/null
        fi
    done
}

# Check each level for cross-imports
for level_dir in pkg/collectors pkg/intelligence pkg/integrations pkg/interfaces; do
    if [[ -d "$level_dir" ]]; then
        check_same_level_imports "$level_dir" "$(basename "$level_dir")"
    fi
done

# Summary
echo -e "\n==========================================="
if [[ $VIOLATIONS -eq 0 ]]; then
    echo -e "${GREEN}✅ Architecture compliance check PASSED!${NC}"
    exit 0
else
    echo -e "${RED}❌ Architecture compliance check FAILED!${NC}"
    echo -e "${RED}   Found $VIOLATIONS violation(s)${NC}"
    exit 1
fi