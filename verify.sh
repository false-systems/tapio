#!/bin/bash
set -e

echo "🔍 TAPIO STRICT VERIFICATION"
echo "============================"

# 1. Check for TODOs and stubs
echo -n "Checking for TODOs/FIXMEs... "
if grep -r "TODO\|FIXME\|XXX\|HACK" --include="*.go" . 2>/dev/null; then
    echo "❌ FAILED - Found TODO/FIXME/stub code"
    exit 1
fi
echo "✅ PASSED"

# 2. Check for ignored errors
echo -n "Checking for ignored errors... "
if grep -r "_ = " --include="*.go" . 2>/dev/null | grep -v "test.go"; then
    echo "❌ FAILED - Found ignored errors"
    exit 1
fi
echo "✅ PASSED"

# 3. Check for interface{} in public APIs
echo -n "Checking for interface{} abuse... "
if grep -r "interface{}" --include="*.go" . | grep -v "json" | grep -v "test.go" | grep "func.*interface{}"; then
    echo "❌ FAILED - Found interface{} in public APIs"
    exit 1
fi
echo "✅ PASSED"

# 4. Check for panic() calls
echo -n "Checking for panic() calls... "
if grep -r "panic(" --include="*.go" . | grep -v "init()" | grep -v "test.go"; then
    echo "❌ FAILED - Found panic() outside init()"
    exit 1
fi
echo "✅ PASSED"

# 5. Format check
echo -n "Checking code formatting... "
UNFORMATTED=$(gofmt -l . | grep -v vendor | wc -l)
if [ "$UNFORMATTED" -ne "0" ]; then
    echo "❌ FAILED - Code not formatted"
    gofmt -l . | grep -v vendor
    exit 1
fi
echo "✅ PASSED"

# 6. Build check
echo -n "Building project... "
if ! go build ./... 2>/dev/null; then
    echo "❌ FAILED - Build errors"
    go build ./...
    exit 1
fi
echo "✅ PASSED"

# 7. Test with race detector
echo -n "Running tests with race detector... "
if ! go test ./... -race -timeout 30s 2>/dev/null; then
    echo "❌ FAILED - Tests failed"
    go test ./... -race
    exit 1
fi
echo "✅ PASSED"

# 8. Coverage check
echo "Checking test coverage..."
go test ./... -cover | while read line; do
    if echo "$line" | grep -q "coverage:"; then
        COVERAGE=$(echo "$line" | sed 's/.*coverage: \([0-9.]*\)%.*/\1/')
        PACKAGE=$(echo "$line" | cut -d' ' -f2)
        if (( $(echo "$COVERAGE < 80" | bc -l) )); then
            echo "❌ FAILED - Package $PACKAGE has only $COVERAGE% coverage (minimum 80%)"
            exit 1
        fi
        echo "✅ $PACKAGE: $COVERAGE%"
    fi
done

# 9. Vet check
echo -n "Running go vet... "
if ! go vet ./... 2>/dev/null; then
    echo "❌ FAILED - Vet issues found"
    go vet ./...
    exit 1
fi
echo "✅ PASSED"

# 10. Architecture check
echo -n "Checking architecture rules... "
python3 -c "
import subprocess
import sys

hierarchy = {
    'pkg/domain': 0,
    'pkg/collectors': 1,
    'pkg/intelligence': 2,
    'pkg/integrations': 3,
    'pkg/interfaces': 4
}

result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}}: {{.Imports}}', './...'], 
                       capture_output=True, text=True)

violations = []
for line in result.stdout.split('\n'):
    if not line.strip():
        continue
    parts = line.split(': ')
    if len(parts) != 2:
        continue
    
    pkg = parts[0]
    imports = parts[1].strip('[]').split()
    
    pkg_level = -1
    for key, level in hierarchy.items():
        if key in pkg:
            pkg_level = level
            break
    
    if pkg_level == -1:
        continue
        
    for imp in imports:
        for key, level in hierarchy.items():
            if key in imp and level > pkg_level:
                violations.append(f'{pkg} (L{pkg_level}) imports {imp} (L{level})')

if violations:
    print('❌ FAILED - Architecture violations found:')
    for v in violations:
        print(f'  - {v}')
    sys.exit(1)
else:
    print('✅ PASSED')
"
if [ $? -ne 0 ]; then
    exit 1
fi

echo ""
echo "✅ ALL CHECKS PASSED - Code is production ready!"