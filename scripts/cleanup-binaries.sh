#!/bin/bash

# Script to clean up binary files from the repository
# This helps reduce repository size and prevent accidental binary commits

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧹 Tapio Binary Cleanup Script${NC}"
echo "================================"

# Find and list large files
echo -e "\n${YELLOW}Searching for large files (>10MB)...${NC}"
large_files=$(find . -type f -size +10M ! -path "*/\.git/*" ! -path "*/node_modules/*" ! -path "*/vendor/*" 2>/dev/null)

if [ -z "$large_files" ]; then
    echo -e "${GREEN}✅ No large files found${NC}"
else
    echo -e "${RED}Found large files:${NC}"
    echo "$large_files" | while read -r file; do
        if [ -f "$file" ]; then
            size=$(du -h "$file" | cut -f1)
            echo "  - $file ($size)"
        fi
    done
    
    echo -e "\n${YELLOW}Removing from git tracking...${NC}"
    echo "$large_files" | while read -r file; do
        if [ -f "$file" ]; then
            git rm --cached "$file" 2>/dev/null && echo "  Removed: $file" || rm -f "$file"
        fi
    done
fi

# Find binary files without extension
echo -e "\n${YELLOW}Searching for binary files...${NC}"
binaries=""

# Check common binary locations
for dir in cmd test pkg/collectors; do
    if [ -d "$dir" ]; then
        # Find files without extension (likely binaries)
        found=$(find "$dir" -type f ! -name "*.*" -exec file {} \; 2>/dev/null | grep -E "executable|binary" | cut -d: -f1)
        if [ -n "$found" ]; then
            binaries="$binaries$found"$'\n'
        fi
    fi
done

# Remove test binaries
test_binaries=$(find . -name "*.test" -type f 2>/dev/null)
if [ -n "$test_binaries" ]; then
    binaries="$binaries$test_binaries"
fi

if [ -z "$binaries" ]; then
    echo -e "${GREEN}✅ No binary files found${NC}"
else
    echo -e "${RED}Found binary files:${NC}"
    echo "$binaries" | while read -r file; do
        if [ -f "$file" ]; then
            size=$(du -h "$file" 2>/dev/null | cut -f1)
            echo "  - $file ($size)"
            rm -f "$file"
        fi
    done
    echo -e "${GREEN}✅ Binary files removed${NC}"
fi

# Clean build directories
echo -e "\n${YELLOW}Cleaning build directories...${NC}"
rm -rf bin/ build/ dist/ 2>/dev/null
echo -e "${GREEN}✅ Build directories cleaned${NC}"

# Install pre-push hook
echo -e "\n${YELLOW}Installing pre-push hook...${NC}"
if [ -f ".github/hooks/pre-push" ]; then
    cp .github/hooks/pre-push .git/hooks/pre-push
    chmod +x .git/hooks/pre-push
    echo -e "${GREEN}✅ Pre-push hook installed${NC}"
else
    echo -e "${YELLOW}⚠️  Pre-push hook not found${NC}"
fi

# Summary
echo -e "\n${GREEN}✅ Cleanup complete!${NC}"
echo -e "\n${BLUE}Next steps:${NC}"
echo "1. Review changes: git status"
echo "2. Commit .gitignore updates: git add .gitignore && git commit -m 'chore: update gitignore for binaries'"
echo "3. If you need to remove files from history:"
echo "   git filter-branch --force --index-filter 'git rm --cached --ignore-unmatch collectors' --prune-empty --tag-name-filter cat -- --all"
echo ""
echo -e "${YELLOW}Prevention tips:${NC}"
echo "- Always use 'make build' which puts binaries in bin/"
echo "- Run 'make clean' regularly"
echo "- The pre-push hook will prevent large files from being pushed"