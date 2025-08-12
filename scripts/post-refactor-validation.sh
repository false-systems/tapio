#!/bin/bash

# Post-Refactor Validation Script
# Comprehensive validation before switching back to production mode

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 POST-REFACTOR VALIDATION PIPELINE${NC}"
echo -e "${BLUE}=====================================${NC}"
echo ""

# Configuration
COVERAGE_THRESHOLD=80
TIMEOUT_DURATION=60

# Track validation results
FAILED_CHECKS=0
FAILED_PACKAGES=""

# Helper function to check step result
check_result() {
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ $1 PASSED${NC}"
    else
        echo -e "${RED}❌ $1 FAILED${NC}"
        FAILED_CHECKS=$((FAILED_CHECKS + 1))
        if [ -n "$2" ]; then
            FAILED_PACKAGES="$FAILED_PACKAGES\n  - $2"
        fi
    fi
    echo ""
}

echo -e "${YELLOW}Phase 1: Code Quality Checks${NC}"
echo "=================================="

# 1. Format check
echo -n "Checking code formatting... "
UNFORMATTED=$(gofmt -l . | grep -v vendor | wc -l)
if [ "$UNFORMATTED" -eq "0" ]; then
    echo -e "${GREEN}✅ PASSED${NC}"
else
    echo -e "${RED}❌ FAILED - $UNFORMATTED files need formatting${NC}"
    gofmt -l . | grep -v vendor | head -10
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

# 2. Imports check
echo -n "Checking import organization... "
if command -v goimports > /dev/null; then
    UNORGANIZED=$(goimports -l . | grep -v vendor | wc -l)
    if [ "$UNORGANIZED" -eq "0" ]; then
        echo -e "${GREEN}✅ PASSED${NC}"
    else
        echo -e "${RED}❌ FAILED - $UNORGANIZED files need import organization${NC}"
        goimports -l . | grep -v vendor | head -10
        FAILED_CHECKS=$((FAILED_CHECKS + 1))
    fi
else
    echo -e "${YELLOW}⚠️  goimports not found - skipping${NC}"
fi
echo ""

# 3. TODO/FIXME check
echo -n "Checking for TODOs/FIXMEs... "
TODO_COUNT=$(grep -r "TODO\|FIXME\|XXX\|HACK" --include="*.go" . 2>/dev/null | wc -l)
if [ "$TODO_COUNT" -eq "0" ]; then
    echo -e "${GREEN}✅ PASSED${NC}"
else
    echo -e "${RED}❌ FAILED - Found $TODO_COUNT TODO/FIXME items${NC}"
    echo "First 5 items:"
    grep -r "TODO\|FIXME\|XXX\|HACK" --include="*.go" . 2>/dev/null | head -5
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

# 4. Ignored errors check
echo -n "Checking for ignored errors... "
IGNORED_ERRORS=$(grep -r "_ = " --include="*.go" . 2>/dev/null | grep -v "test.go" | wc -l)
if [ "$IGNORED_ERRORS" -eq "0" ]; then
    echo -e "${GREEN}✅ PASSED${NC}"
else
    echo -e "${RED}❌ FAILED - Found $IGNORED_ERRORS ignored errors${NC}"
    echo "First 5 items:"
    grep -r "_ = " --include="*.go" . 2>/dev/null | grep -v "test.go" | head -5
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

echo -e "${YELLOW}Phase 2: Build Verification${NC}"
echo "============================"

# 5. Build check
echo "Building all packages..."
if go build ./... 2>/dev/null; then
    echo -e "${GREEN}✅ BUILD PASSED${NC}"
else
    echo -e "${RED}❌ BUILD FAILED${NC}"
    echo "Build errors:"
    go build ./...
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

# 6. Vet check
echo "Running go vet..."
if go vet ./... 2>/dev/null; then
    echo -e "${GREEN}✅ VET PASSED${NC}"
else
    echo -e "${RED}❌ VET FAILED${NC}"
    echo "Vet issues:"
    go vet ./...
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

echo -e "${YELLOW}Phase 3: Architecture Validation${NC}"
echo "=================================="

# 7. Architecture check
echo "Verifying 5-level architecture..."
if ./scripts/verify-architecture.sh 2>/dev/null; then
    echo -e "${GREEN}✅ ARCHITECTURE PASSED${NC}"
else
    echo -e "${RED}❌ ARCHITECTURE FAILED${NC}"
    ./scripts/verify-architecture.sh
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

echo -e "${YELLOW}Phase 4: Test Verification${NC}"
echo "==========================="

# 8. Test execution
echo "Running all tests with race detector..."
if go test -race -timeout ${TIMEOUT_DURATION}s ./... 2>/dev/null; then
    echo -e "${GREEN}✅ ALL TESTS PASSED${NC}"
else
    echo -e "${RED}❌ TESTS FAILED${NC}"
    echo "Test failures:"
    go test -race -timeout ${TIMEOUT_DURATION}s ./... | grep -E "(FAIL|panic)"
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
fi
echo ""

# 9. Coverage check
echo -e "${YELLOW}Phase 5: Coverage Validation${NC}"
echo "============================="

echo "Checking test coverage (minimum ${COVERAGE_THRESHOLD}%)..."
COVERAGE_FAILED=0

go test -cover ./... 2>/dev/null | while read line; do
    if echo "$line" | grep -q "coverage:"; then
        COVERAGE=$(echo "$line" | sed 's/.*coverage: \([0-9.]*\)%.*/\1/')
        PACKAGE=$(echo "$line" | awk '{print $2}')
        
        # Use bc for floating point comparison if available
        if command -v bc > /dev/null; then
            if (( $(echo "$COVERAGE < $COVERAGE_THRESHOLD" | bc -l) )); then
                echo -e "${RED}❌ $PACKAGE: ${COVERAGE}% (minimum ${COVERAGE_THRESHOLD}%)${NC}"
                echo "$PACKAGE" >> /tmp/failed_coverage.txt
            else
                echo -e "${GREEN}✅ $PACKAGE: ${COVERAGE}%${NC}"
            fi
        else
            # Fallback for systems without bc
            COVERAGE_INT=$(echo "$COVERAGE" | cut -d'.' -f1)
            if [ "$COVERAGE_INT" -lt "$COVERAGE_THRESHOLD" ]; then
                echo -e "${RED}❌ $PACKAGE: ${COVERAGE}% (minimum ${COVERAGE_THRESHOLD}%)${NC}"
                echo "$PACKAGE" >> /tmp/failed_coverage.txt
            else
                echo -e "${GREEN}✅ $PACKAGE: ${COVERAGE}%${NC}"
            fi
        fi
    fi
done

# Check if any packages failed coverage
if [ -f /tmp/failed_coverage.txt ]; then
    FAILED_COVERAGE=$(wc -l < /tmp/failed_coverage.txt)
    echo -e "${RED}❌ COVERAGE FAILED - $FAILED_COVERAGE packages below threshold${NC}"
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
    rm -f /tmp/failed_coverage.txt
else
    echo -e "${GREEN}✅ COVERAGE PASSED - All packages meet threshold${NC}"
fi
echo ""

# 10. Security checks (optional)
echo -e "${YELLOW}Phase 6: Security Validation${NC}"
echo "============================="

if command -v gosec > /dev/null; then
    echo "Running security scan..."
    if gosec -quiet ./... 2>/dev/null; then
        echo -e "${GREEN}✅ SECURITY SCAN PASSED${NC}"
    else
        echo -e "${YELLOW}⚠️  SECURITY ISSUES FOUND${NC}"
        echo "Review security report (not failing validation)"
    fi
else
    echo -e "${YELLOW}⚠️  gosec not installed - skipping security scan${NC}"
fi
echo ""

# Final result
echo -e "${BLUE}==========================================${NC}"
echo -e "${BLUE}           VALIDATION SUMMARY${NC}"
echo -e "${BLUE}==========================================${NC}"

if [ "$FAILED_CHECKS" -eq "0" ]; then
    echo -e "${GREEN}🎉 ALL VALIDATIONS PASSED!${NC}"
    echo -e "${GREEN}✅ Ready to switch to production mode${NC}"
    echo ""
    echo -e "${YELLOW}Next steps:${NC}"
    echo "1. Run: make production-mode"
    echo "2. Test with: make ci-local"
    echo "3. Create PR to main branch"
    exit 0
else
    echo -e "${RED}❌ $FAILED_CHECKS VALIDATION(S) FAILED${NC}"
    echo -e "${RED}❌ NOT READY for production mode${NC}"
    echo ""
    echo -e "${YELLOW}Required actions:${NC}"
    echo "1. Fix the failed validations listed above"
    echo "2. Re-run this script until all checks pass"
    echo "3. Stay in refactor mode until issues are resolved"
    
    if [ -n "$FAILED_PACKAGES" ]; then
        echo ""
        echo -e "${YELLOW}Failed packages:${NC}"
        echo -e "$FAILED_PACKAGES"
    fi
    
    echo ""
    echo -e "${BLUE}💡 To fix common issues:${NC}"
    echo "  - Format code: make fmt"
    echo "  - Remove TODOs: Review and complete or remove TODO comments"
    echo "  - Fix ignored errors: Add proper error handling"
    echo "  - Fix architecture: Follow 5-level dependency rules"
    echo "  - Improve coverage: Add missing tests"
    
    exit 1
fi