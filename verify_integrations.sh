#!/bin/bash
set -e

echo "🔍 TAPIO INTEGRATIONS STRICT VERIFICATION"
echo "========================================"

INTEGRATIONS_PATH="pkg/integrations"

# 1. Check for ignored errors
echo -n "Checking for ignored errors in integrations... "
IGNORED_ERRORS=$(grep -r "_, _ = \|_ = " --include="*.go" $INTEGRATIONS_PATH 2>/dev/null | grep -v "test.go" | grep -v "// Acknowledged" | wc -l)
if [ "$IGNORED_ERRORS" -gt "0" ]; then
    echo "❌ FAILED - Found $IGNORED_ERRORS ignored errors"
    grep -r "_, _ = \|_ = " --include="*.go" $INTEGRATIONS_PATH | grep -v "test.go" | grep -v "// Acknowledged"
    exit 1
fi
echo "✅ PASSED"

# 2. Check for test skipping
echo -n "Checking for test skipping... "
if grep -r "t\.Skip\|t\.Skipf" --include="*.go" $INTEGRATIONS_PATH 2>/dev/null; then
    echo "❌ FAILED - Found t.Skip() calls"
    exit 1
fi
echo "✅ PASSED"

# 3. Check for interface{} in public APIs
echo -n "Checking for interface{} abuse... "
INTERFACE_VIOLATIONS=$(grep -r "func.*interface{}" --include="*.go" $INTEGRATIONS_PATH | grep -v "test.go" | grep -v "any\|json\|internal" | wc -l)
if [ "$INTERFACE_VIOLATIONS" -gt "0" ]; then
    echo "❌ FAILED - Found $INTERFACE_VIOLATIONS interface{} in public APIs"
    grep -r "func.*interface{}" --include="*.go" $INTEGRATIONS_PATH | grep -v "test.go" | grep -v "any\|json\|internal"
    exit 1
fi
echo "✅ PASSED"

# 4. Check for massive functions (>50 lines)
echo "Checking for functions exceeding 50 lines..."
python3 -c "
import os
import re

def count_function_lines(filepath):
    violations = []
    with open(filepath, 'r') as f:
        lines = f.readlines()
    
    func_start = None
    func_name = None
    brace_count = 0
    in_function = False
    
    for i, line in enumerate(lines):
        # Check for function definition
        func_match = re.match(r'^func\s+(?:\([^)]*\)\s+)?(\w+)', line.strip())
        if func_match and not in_function:
            func_name = func_match.group(1)
            func_start = i
            brace_count = line.count('{') - line.count('}')
            in_function = True
            continue
        
        if in_function:
            brace_count += line.count('{') - line.count('}')
            if brace_count == 0:
                func_length = i - func_start + 1
                if func_length > 50:
                    violations.append((func_name, func_length, func_start + 1))
                in_function = False
                func_start = None
                func_name = None
    
    return violations

violations = []
for root, dirs, files in os.walk('$INTEGRATIONS_PATH'):
    for file in files:
        if file.endswith('.go'):
            filepath = os.path.join(root, file)
            file_violations = count_function_lines(filepath)
            for func_name, length, line_num in file_violations:
                violations.append((filepath, func_name, length, line_num))

if violations:
    print('❌ FAILED - Found functions exceeding 50 lines:')
    for filepath, func_name, length, line_num in violations:
        print(f'  - {filepath}:{line_num} {func_name}() = {length} lines')
    exit(1)
else:
    print('✅ PASSED')
"

# 5. Build check
echo -n "Building integrations package... "
if ! go build ./$INTEGRATIONS_PATH/... 2>/dev/null; then
    echo "❌ FAILED - Build errors"
    go build ./$INTEGRATIONS_PATH/...
    exit 1
fi
echo "✅ PASSED"

# 6. Test with race detector
echo -n "Running tests with race detector... "
if ! go test ./$INTEGRATIONS_PATH/... -race -timeout 30s 2>/dev/null; then
    echo "❌ FAILED - Tests failed"
    go test ./$INTEGRATIONS_PATH/... -race
    exit 1
fi
echo "✅ PASSED"

# 7. Coverage check (80% minimum)
echo "Checking test coverage..."
go test ./$INTEGRATIONS_PATH/... -cover | while read line; do
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

# 8. Architecture compliance check
echo -n "Checking architecture compliance... "
python3 -c "
import subprocess
import sys

result = subprocess.run(['go', 'list', '-f', '{{.ImportPath}}: {{.Imports}}', './$INTEGRATIONS_PATH/...'], 
                       capture_output=True, text=True)

violations = []
for line in result.stdout.split('\n'):
    if 'pkg/integrations' not in line:
        continue
    parts = line.split(': ')
    if len(parts) != 2:
        continue
    
    pkg = parts[0]
    imports = parts[1].strip('[]').split()
    
    # Integrations should only import domain, collectors, and intelligence
    forbidden_imports = []
    for imp in imports:
        if 'github.com/yairfalse/tapio/pkg/' in imp:
            if not any(allowed in imp for allowed in ['domain', 'collectors', 'intelligence']):
                forbidden_imports.append(imp)
    
    if forbidden_imports:
        violations.append((pkg, forbidden_imports))

if violations:
    print('❌ FAILED - Architecture violations:')
    for pkg, imports in violations:
        print(f'  - {pkg} imports: {imports}')
    sys.exit(1)
else:
    print('✅ PASSED')
"

echo ""
echo "✅ ALL INTEGRATIONS CHECKS PASSED - Ready for production!"