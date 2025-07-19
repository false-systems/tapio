# Tapio Architecture Enforcement Scripts

This directory contains critical enforcement scripts that validate Tapio's architecture according to Claude.md requirements with **ZERO TOLERANCE** for violations.

## 🚨 Critical Enforcement Scripts

All scripts are designed to **FAIL THE BUILD** when violations are detected. There are no warnings - only PASS/FAIL.

### 1. `check-architecture.go` - Dependency Hierarchy Enforcement

**Purpose:** Enforces the 5-level dependency hierarchy with zero tolerance.

**Rules Enforced:**
- ✅ Level 0 (domain): Zero dependencies  
- ✅ Level 1 (collectors): Domain only
- ✅ Level 2 (intelligence): Domain + Level 1
- ✅ Level 3 (integrations): Domain + Level 1 + Level 2  
- ✅ Level 4 (interfaces): All levels allowed
- ❌ **FORBIDDEN:** Same-level imports between components
- ❌ **FORBIDDEN:** Upward dependencies (higher level importing lower)

**Usage:**
```bash
make check-architecture
# OR
go run scripts/check-architecture.go
```

**Exit Codes:**
- `0`: All dependencies respect hierarchy
- `1`: Architecture violations found (BUILD FAILS)

### 2. `check-module-independence.go` - Module Independence Validation

**Purpose:** Ensures each area builds and runs independently (Claude.md Rule A5).

**Validation Checks:**
- ✅ Each module has independent go.mod
- ✅ Each module builds without external dependencies  
- ✅ Each module tests run standalone
- ✅ Standalone executables exist and build
- ✅ Graceful degradation when dependencies unavailable

**Usage:**
```bash
make check-independence
# OR
go run scripts/check-module-independence.go
```

**Performance Metrics:**
- Build time per module
- Test execution time
- Resource usage validation

### 3. `check-implementation-completeness.go` - NO STUBS Enforcement

**Purpose:** Enforces Claude.md Rule A4 with ZERO TOLERANCE for incomplete implementations.

**FORBIDDEN Patterns (Build-Blocking):**
- ❌ `return nil, fmt.Errorf("not implemented")`
- ❌ `// TODO: implement this later`
- ❌ `// We'll add the real logic later`
- ❌ Functions with only `return nil`
- ❌ `panic("not implemented")`
- ❌ Empty function bodies with placeholder comments

**Required Standards:**
- ✅ Every function fully implemented and working
- ✅ All code paths tested and functional
- ✅ Real error handling instead of placeholder errors
- ✅ Complete feature implementation before moving to next component

**Usage:**
```bash
make check-completeness
# OR
go run scripts/check-implementation-completeness.go
```

### 4. `check-coverage.go` - 80% Test Coverage Enforcement

**Purpose:** Enforces Claude.md Rule T1 - minimum 80% test coverage for all public functions.

**Coverage Requirements:**
- ✅ Minimum 80% line coverage for each module
- ✅ Unit tests for all public functions
- ✅ Integration tests for external dependencies
- ✅ Table-driven tests for multiple scenarios
- ✅ Benchmark tests for performance-critical code

**Analysis Features:**
- Function-level coverage analysis
- Untested public function detection
- Module-by-module coverage breakdown
- Overall project coverage statistics

**Usage:**
```bash
make check-coverage
# OR
go run scripts/check-coverage.go
```

### 5. `check-type-safety.go` - Strong Typing Enforcement

**Purpose:** Enforces Claude.md Rule Q1 - strong typing with zero tolerance for type abuse.

**FORBIDDEN Patterns (Build-Blocking):**
- ❌ `map[string]interface{}` without strong justification
- ❌ `interface{}` in public APIs
- ❌ `any` without explicit comment explaining why
- ❌ Function parameters with `interface{}` type
- ❌ Function returns with `interface{}` type
- ❌ Struct fields with `interface{}` type

**Required Standards:**
- ✅ Strongly-typed structs for all data
- ✅ Validation methods for all input types
- ✅ Type assertions with proper error handling
- ✅ Concrete types instead of `interface{}`

**Usage:**
```bash
make check-type-safety
# OR
go run scripts/check-type-safety.go
```

## 🔧 Running All Enforcement Checks

### Complete Enforcement Suite
```bash
make enforce-all
```
This runs ALL 5 enforcement scripts in sequence and fails if ANY violations are found.

### CI/CD Integration
```bash
make ci                    # Full CI with enforcement
make ci-quick             # Quick CI with enforcement (no Docker)
make ci-enforcement-only  # Only enforcement checks
```

## 🚦 Integration with CI/CD

### GitHub Actions Integration

The enforcement scripts are integrated into `.github/workflows/ci.yml`:

1. **First Step:** Architecture enforcement (blocks all other steps if failed)
2. **Parallel Steps:** Linting, testing, building (only if enforcement passes)
3. **Final Steps:** Security, release (only if all previous steps pass)

### Makefile Integration

All enforcement scripts are integrated into the Makefile:

- `make enforce-all` - Run all enforcement checks
- `make ci` - Full CI pipeline with enforcement
- `make ci-quick` - Quick CI with enforcement
- Individual checks available as separate targets

## 📊 Performance Characteristics

### Script Performance
- **Architecture Check:** ~1-3 seconds for typical codebase
- **Independence Check:** ~5-15 seconds (includes building each module)
- **Completeness Check:** ~2-5 seconds for AST analysis
- **Coverage Check:** ~10-30 seconds (includes running tests)
- **Type Safety Check:** ~2-5 seconds for AST analysis

### Total Enforcement Time
- **Complete Suite:** ~20-60 seconds depending on codebase size
- **Parallel Execution:** Not currently implemented (sequential for accuracy)

## 🛠️ Customization

### Threshold Configuration

Coverage threshold can be modified in `check-coverage.go`:
```go
const COVERAGE_THRESHOLD = 80.0 // Minimum 80% coverage required
```

### Pattern Customization

Forbidden patterns can be extended in `check-implementation-completeness.go`:
```go
forbiddenPatterns := []string{
    `fmt\.Errorf\("not implemented"\)`,
    // Add custom patterns here
}
```

### Architecture Levels

Architecture hierarchy is defined in `check-architecture.go`:
```go
var levelHierarchy = map[string]int{
    "pkg/domain":       0, // Zero dependencies
    "pkg/collectors":   1, // Domain only
    "pkg/intelligence": 2, // Domain + Level 1
    "pkg/integrations": 3, // Domain + Level 1 + Level 2
    "pkg/interfaces":   4, // All above levels
}
```

## 🚨 Troubleshooting

### Common Issues

1. **"Architecture script not found"**
   - Ensure you're running from the project root
   - Check that `scripts/` directory exists

2. **"Module independence check fails"**
   - Verify each module has its own `go.mod`
   - Check for missing dependencies in individual modules

3. **"Coverage check fails"**
   - Add test files for modules without tests
   - Increase test coverage to meet 80% threshold

4. **"Type safety violations"**
   - Replace `map[string]interface{}` with typed structs
   - Remove `interface{}` from public APIs

### Debug Mode

For detailed debugging, run scripts directly:
```bash
go run scripts/check-architecture.go -v
go run scripts/check-coverage.go --debug
```

## 📝 Exit Codes

All enforcement scripts follow consistent exit code patterns:

- `0`: All checks passed
- `1`: Violations found (build should fail)
- `2`: Script error or invalid usage

## 🎯 Agent 1 Requirements Fulfilled

These scripts implement all of Agent 1's CI/CD enforcement requirements:

✅ **Dependency hierarchy enforcement with zero tolerance**  
✅ **Module independence validation**  
✅ **Implementation completeness (no stubs/TODOs)**  
✅ **80% test coverage requirement**  
✅ **Type safety enforcement**  
✅ **Integration with CI/CD pipeline**  
✅ **Build-blocking on violations**  
✅ **Comprehensive reporting and guidance**

The enforcement suite ensures that Tapio maintains the highest architectural standards throughout development, preventing technical debt and ensuring production-ready code quality.