#!/bin/bash
# CLAUDE.md Compliance Verification Script
# Used by both pre-commit hooks and CI/CD
#
# Usage:
#   ./verify-claude-md.sh [--strict] [files...]
#   --strict: Enable golangci-lint (slower, for CI)

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Parse flags
STRICT_MODE=0
FILES_ARG=""

for arg in "$@"; do
    case $arg in
        --strict)
            STRICT_MODE=1
            shift
            ;;
        *)
            FILES_ARG="$FILES_ARG $arg"
            ;;
    esac
done

# Default: check all Go files if no args provided
if [ -z "$FILES_ARG" ]; then
    GO_FILES=$(find . -name "*.go" -not -path "./vendor/*" -not -path "./.git/*")
else
    GO_FILES="$FILES_ARG"
fi

if [ -z "$GO_FILES" ]; then
    echo -e "${GREEN}✓ No Go files to check${NC}"
    exit 0
fi

VIOLATIONS=0

echo -e "${GREEN}🔍 CLAUDE.md Compliance Verification${NC}"
echo ""

# ============================================================================
# VIOLATION 1: map[string]interface{} (BANNED)
# ============================================================================
echo -e "${YELLOW}[1/6] Checking for map[string]interface{} violations...${NC}"

MAP_VIOLATIONS=$(echo "$GO_FILES" | xargs grep -n "map\[string\]interface{}" 2>/dev/null | grep -v "json.Unmarshal" | grep -v "// Exception:" || true)
if [ -n "$MAP_VIOLATIONS" ]; then
    echo -e "${RED}❌ VIOLATION: map[string]interface{} found:${NC}"
    echo "$MAP_VIOLATIONS"
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ No map[string]interface{} violations${NC}"
fi

# ============================================================================
# VIOLATION 2: TODO/FIXME/XXX/HACK (BANNED)
# ============================================================================
echo -e "${YELLOW}[2/6] Checking for TODO/FIXME/XXX/HACK comments...${NC}"

TODO_VIOLATIONS=$(echo "$GO_FILES" | xargs grep -n -E "//.*\b(TODO|FIXME|XXX|HACK)\b" 2>/dev/null || true)
if [ -n "$TODO_VIOLATIONS" ]; then
    echo -e "${RED}❌ VIOLATION: TODO/FIXME/XXX/HACK comments found:${NC}"
    echo "$TODO_VIOLATIONS"
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ No TODO/FIXME/XXX/HACK comments${NC}"
fi

# ============================================================================
# VIOLATION 3: Ignored errors (_ =)
# ============================================================================
echo -e "${YELLOW}[3/6] Checking for ignored errors...${NC}"

IGNORED_ERRORS=$(echo "$GO_FILES" | xargs grep -n "_ =" 2>/dev/null | grep -v "_test.go" | grep -v "// Ignore:" || true)
if [ -n "$IGNORED_ERRORS" ]; then
    echo -e "${RED}❌ VIOLATION: Ignored errors (_ =) found in non-test files:${NC}"
    echo "$IGNORED_ERRORS"
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ No ignored errors in production code${NC}"
fi

# ============================================================================
# VIOLATION 4: Stub implementations (panic)
# ============================================================================
echo -e "${YELLOW}[4/6] Checking for stub implementations...${NC}"

STUB_VIOLATIONS=$(echo "$GO_FILES" | xargs grep -n -E "panic\(\"not implemented\"|panic\(\"TODO" 2>/dev/null || true)
if [ -n "$STUB_VIOLATIONS" ]; then
    echo -e "${RED}❌ VIOLATION: Stub implementations found:${NC}"
    echo "$STUB_VIOLATIONS"
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ No stub implementations${NC}"
fi

# ============================================================================
# VIOLATION 5: interface{} in public APIs
# ============================================================================
echo -e "${YELLOW}[5/6] Checking for interface{} in public APIs...${NC}"

# Load exceptions if file exists
EXCEPTION_FILES=""
if [ -f ".claude-exceptions.yml" ]; then
    EXCEPTION_FILES=$(grep '^  - file:' .claude-exceptions.yml | sed 's/.*file: "\(.*\)"/\1/' | tr '\n' '|' | sed 's/|$//')
fi

INTERFACE_VIOLATIONS=$(echo "$GO_FILES" | xargs grep -n "^func.*interface{}" 2>/dev/null | grep -v "context.Context" | grep -v "any" || true)

# Filter out exceptions
if [ -n "$EXCEPTION_FILES" ]; then
    INTERFACE_VIOLATIONS=$(echo "$INTERFACE_VIOLATIONS" | grep -v -E "$EXCEPTION_FILES" || true)
fi

if [ -n "$INTERFACE_VIOLATIONS" ]; then
    echo -e "${RED}❌ VIOLATION: interface{} in public API:${NC}"
    echo "$INTERFACE_VIOLATIONS"
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ No interface{} in public APIs (exceptions documented in .claude-exceptions.yml)${NC}"
fi

# ============================================================================
# CODE QUALITY: Format check
# ============================================================================
echo -e "${YELLOW}[6/9] Checking go fmt...${NC}"

UNFORMATTED=$(echo "$GO_FILES" | xargs gofmt -l 2>/dev/null || true)
if [ -n "$UNFORMATTED" ]; then
    echo -e "${RED}❌ VIOLATION: Unformatted files:${NC}"
    echo "$UNFORMATTED"
    echo -e "${YELLOW}Run: go fmt ./...${NC}"
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ All files formatted${NC}"
fi

# ============================================================================
# CODE QUALITY: go vet
# ============================================================================
echo -e "${YELLOW}[7/9] Running go vet...${NC}"

# Get unique directories from files
DIRS=$(echo "$GO_FILES" | xargs -n1 dirname | sort -u)
VET_FAILED=0

for dir in $DIRS; do
    if [ -d "$dir" ] && ls "$dir"/*.go >/dev/null 2>&1; then
        if ! go vet "./$dir" 2>&1 | grep -q "no Go files"; then
            if ! go vet "./$dir" 2>/dev/null; then
                echo -e "${RED}❌ go vet failed for $dir${NC}"
                go vet "./$dir" 2>&1 | head -10
                VET_FAILED=1
            fi
        fi
    fi
done

if [ $VET_FAILED -eq 1 ]; then
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ go vet passed${NC}"
fi

# ============================================================================
# CODE QUALITY: golangci-lint (only in strict mode or CI)
# ============================================================================
echo -e "${YELLOW}[8/9] Running golangci-lint...${NC}"

if [ "$STRICT_MODE" -eq 1 ] || [ -n "$CI" ]; then
    if command -v golangci-lint &> /dev/null; then
        if echo "$GO_FILES" | xargs -n1 dirname | sort -u | xargs golangci-lint run --timeout=5m 2>/dev/null; then
            echo -e "${GREEN}✓ golangci-lint passed${NC}"
        else
            echo -e "${RED}❌ golangci-lint failed${NC}"
            VIOLATIONS=$((VIOLATIONS + 1))
        fi
    else
        echo -e "${RED}❌ golangci-lint not installed (required in strict mode)${NC}"
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
else
    echo -e "${YELLOW}⚠ Skipping golangci-lint (use --strict to enable)${NC}"
fi

# ============================================================================
# BUILD CHECK: Verify code compiles
# ============================================================================
echo -e "${YELLOW}[9/9] Verifying code compiles...${NC}"

DIRS=$(echo "$GO_FILES" | xargs -n1 dirname | sort -u)
BUILD_FAILED=0

for dir in $DIRS; do
    if [ -d "$dir" ] && ls "$dir"/*.go >/dev/null 2>&1; then
        if ! go build "./$dir" 2>/dev/null; then
            echo -e "${RED}❌ Build failed for $dir${NC}"
            go build "./$dir" 2>&1 | head -10
            BUILD_FAILED=1
        fi
    fi
done

if [ $BUILD_FAILED -eq 1 ]; then
    VIOLATIONS=$((VIOLATIONS + 1))
else
    echo -e "${GREEN}✓ Build check passed${NC}"
fi

# ============================================================================
# FINAL VERDICT
# ============================================================================
echo ""
if [ $VIOLATIONS -gt 0 ]; then
    echo -e "${RED}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${RED}❌ FAILED - $VIOLATIONS CLAUDE.md violations found${NC}"
    echo -e "${RED}═══════════════════════════════════════════════════════════${NC}"
    echo ""
    exit 1
fi

echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✅ ALL CLAUDE.MD COMPLIANCE CHECKS PASSED${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
echo ""
exit 0
