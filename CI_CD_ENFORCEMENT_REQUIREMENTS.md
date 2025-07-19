# CI/CD Enforcement Requirements for Tapio Architecture

> **HANDOFF TO AGENT 2**: Complete enforcement specification for maintaining Tapio's 5-level architectural hierarchy

## 🚨 **CRITICAL: ZERO-TOLERANCE ENFORCEMENT RULES**

Per Claude.md mandate: **"NO CODE will be accepted that violates these constraints. NO INCOMPLETE IMPLEMENTATIONS will be accepted."**

---

## 🔒 **BLOCKING MECHANISMS (PR Auto-Fail)**

### **1. Dependency Hierarchy Enforcement (HIGHEST PRIORITY)**

**REQUIRED VALIDATION:**
```yaml
Level 0 (pkg/domain/):        # MUST have zero external dependencies
Level 1 (pkg/collectors/):    # MUST only import pkg/domain
Level 2 (pkg/intelligence/):  # MUST only import pkg/domain + Level 1  
Level 3 (pkg/integrations/):  # MUST only import pkg/domain + L1 + L2
Level 4 (pkg/interfaces/):    # CAN import all lower levels
```

**FORBIDDEN PATTERNS (AUTO-FAIL PR):**
- ❌ Any Level 1 component importing another Level 1 component
- ❌ Any component importing a higher-level component  
- ❌ Cross-imports between same-level components
- ❌ Circular dependencies anywhere in the graph

**IMPLEMENTATION:**
```bash
# Required validation commands
go list -deps pkg/domain                    # Must show ZERO external deps
go list -deps pkg/collectors/ebpf           # Must only show pkg/domain
go list -deps pkg/collectors/k8s            # Must only show pkg/domain  
go list -deps pkg/collectors/systemd        # Must only show pkg/domain
go list -deps pkg/collectors/journald       # Must only show pkg/domain

# Enhanced dependency graph analysis
./scripts/check-dependencies.sh --strict --fail-on-violation
```

### **2. Module Independence Validation (BLOCKING)**

**REQUIRED VALIDATION:**
```bash
# Each module MUST build independently
cd pkg/collectors/ebpf && go build ./...     # MUST succeed standalone
cd pkg/collectors/k8s && go build ./...      # MUST succeed standalone  
cd pkg/collectors/systemd && go build ./...  # MUST succeed standalone
cd pkg/collectors/journald && go build ./... # MUST succeed standalone

# Each module MUST test independently  
cd pkg/collectors/ebpf && go test ./...      # MUST pass standalone
cd pkg/intelligence/correlation && go test ./... # MUST pass standalone
```

**FORBIDDEN PATTERNS (AUTO-FAIL PR):**
- ❌ Build dependencies between areas at the same level
- ❌ Runtime dependencies that prevent standalone operation
- ❌ Tests that require other areas to be present

### **3. Implementation Completeness (ZERO TOLERANCE)**

**FORBIDDEN CODE PATTERNS (AUTO-FAIL PR):**
```go
// ❌ ABSOLUTELY FORBIDDEN - Auto-fail CI immediately
func (c *Collector) CollectEvents(ctx context.Context) ([]Event, error) {
    // TODO: implement this later
    return nil, fmt.Errorf("not implemented")
}

func (c *Collector) ProcessEvent(event Event) error {
    // We'll add the real logic later
    return nil
}

// Any variation of:
return nil, errors.New("not implemented")
return nil, errors.New("TODO")
// TODO: implement
// FIXME: implement later
// We'll fix this later
```

**DETECTION IMPLEMENTATION:**
```bash
# Static analysis for forbidden patterns
grep -r "not implemented" --include="*.go" .     # FAIL if found
grep -r "TODO.*implement" --include="*.go" .     # FAIL if found  
grep -r "We'll.*later" --include="*.go" .        # FAIL if found
grep -r "FIXME.*implement" --include="*.go" .    # FAIL if found

# AST analysis for empty function bodies with TODO comments
./scripts/detect-stubs.go --fail-on-stubs
```

### **4. Test Coverage Requirements (MANDATORY 80%)**

**REQUIRED VALIDATION:**
```bash
# Each module MUST achieve 80% coverage minimum
go test -cover ./pkg/domain/... | grep "coverage:.*80%" || exit 1
go test -cover ./pkg/collectors/... | grep "coverage:.*80%" || exit 1
go test -cover ./pkg/intelligence/... | grep "coverage:.*80%" || exit 1

# Coverage differential for PR changes
./scripts/coverage-check.sh --minimum=80 --fail-below-threshold
```

**BLOCKING RULES:**
- ❌ Any public function without tests
- ❌ Any validation logic without tests  
- ❌ Any external dependency without integration tests
- ❌ Any performance-critical code without benchmarks

### **5. Type Safety Enforcement (BLOCKING)**

**FORBIDDEN PATTERNS (AUTO-FAIL PR):**
```go
// ❌ Type unsafe patterns - Auto-fail
type Config map[string]interface{}           # Use strongly-typed structs
func Process(data interface{}) error         # No interface{} in public APIs
func Handle(payload any) error               # No any without explicit justification
```

**REQUIRED PATTERNS:**
```go
// ✅ Type safe patterns - Required
type Config struct {
    Host     string        `json:"host" validate:"required"`
    Port     int           `json:"port" validate:"min=1,max=65535"`
    Timeout  time.Duration `json:"timeout" validate:"min=1s"`
}

func (c Config) Validate() error {
    // Validation logic here
    return nil
}
```

**DETECTION IMPLEMENTATION:**
```bash
# Static analysis for type safety violations
./scripts/check-type-safety.sh --fail-on-violations
golangci-lint run --enable=depguard --fail-on=any
```

### **6. Error Handling Enforcement (BLOCKING)**

**REQUIRED PATTERNS:**
```go
// ✅ Every error MUST be handled explicitly
if err := someOperation(); err != nil {
    return fmt.Errorf("context: %w", err)    # MUST wrap with context
}

// ✅ Custom error types for domain-specific errors
type ValidationError struct {
    Field   string
    Value   interface{}
    Message string
}

func (e ValidationError) Error() string {
    return fmt.Sprintf("validation failed for field %s: %s", e.Field, e.Message)
}
```

**FORBIDDEN PATTERNS (AUTO-FAIL PR):**
```go
someOperation() // ❌ Silent error ignoring with _
panic("error") // ❌ Panic in library code
```

### **7. Context Handling Enforcement (BLOCKING)**

**REQUIRED PATTERNS:**
```go
// ✅ Every public function that does I/O MUST accept context.Context as first parameter
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()    # MUST respect context cancellation
    default:
    }
    
    return c.doCollection(ctx, criteria)    # MUST pass ctx throughout
}
```

**FORBIDDEN PATTERNS (AUTO-FAIL PR):**
```go
func CollectEvents(criteria Criteria) error         # ❌ Missing context parameter
func ProcessData() error {
    ctx := context.Background()                      # ❌ Using Background() in library code
}
```

---

## 🛠️ **IMPLEMENTATION STRATEGY**

### **Pre-commit Hooks (Prevention)**
```yaml
pre-commit-validation:
  - gofmt/goimports formatting check
  - Basic golangci-lint subset
  - Stub/TODO pattern detection  
  - Secret scanning
  - Import cycle detection
```

### **PR Validation Pipeline (Blocking)**
```yaml
pr-blocking-checks:
  dependency-hierarchy:
    - run: ./scripts/check-dependencies.sh --strict
    - fail-on: Any hierarchy violations
    
  module-independence:
    - run: ./scripts/test-modular-builds.sh --isolated
    - fail-on: Any cross-module build dependencies
    
  implementation-completeness:
    - run: ./scripts/detect-stubs.sh --zero-tolerance
    - fail-on: Any stub/TODO/not-implemented patterns
    
  test-coverage:
    - run: go test -cover ./... --minimum=80%
    - fail-on: Coverage below 80% for any module
    
  type-safety:
    - run: ./scripts/check-type-safety.sh --strict
    - fail-on: Any map[string]interface{} abuse or interface{} in public APIs
    
  code-quality:
    - run: golangci-lint run --config .golangci.yml
    - fail-on: Any linting violations
```

### **GitHub Actions Integration**
```yaml
# .github/workflows/architecture-enforcement.yml
name: Architecture Enforcement
on: [pull_request]

jobs:
  enforce-architecture:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: "BLOCKING: Dependency Hierarchy Check"
        run: ./scripts/check-dependencies.sh --strict --fail-fast
        
      - name: "BLOCKING: Module Independence Check"  
        run: ./scripts/test-modular-builds.sh --isolated --fail-fast
        
      - name: "BLOCKING: Implementation Completeness Check"
        run: ./scripts/detect-stubs.sh --zero-tolerance --fail-fast
        
      - name: "BLOCKING: Test Coverage Check"
        run: ./scripts/coverage-check.sh --minimum=80 --fail-fast
        
      - name: "BLOCKING: Type Safety Check"
        run: ./scripts/check-type-safety.sh --strict --fail-fast

  # Any failure in enforce-architecture BLOCKS the PR
  require-enforcement:
    needs: enforce-architecture
    runs-on: ubuntu-latest
    if: success()
    steps:
      - run: echo "All architectural constraints satisfied ✅"
```

---

## 📋 **VALIDATION CHECKLIST FOR AGENT 2**

### **Before ANY Code is Accepted, Verify:**

#### **Architecture Validation (BLOCKING):**
- [ ] Component is at correct dependency level
- [ ] No cross-imports between same-level components  
- [ ] Independent go.mod with minimal dependencies
- [ ] Correct directory structure

#### **Implementation Validation (BLOCKING):**
- [ ] No stub functions or placeholder code
- [ ] Every function is fully implemented and working
- [ ] All code paths tested and functional
- [ ] No "TODO: implement later" comments

#### **Independence Validation (BLOCKING):**
- [ ] Area builds without requiring other areas (`go build ./...`)
- [ ] Area tests run independently (`go test ./...`)
- [ ] Area has standalone executables for testing and debugging
- [ ] Area gracefully handles missing dependencies

#### **Quality Validation (BLOCKING):**
- [ ] No `map[string]interface{}` without justification
- [ ] All errors handled explicitly with context
- [ ] Context passed to all I/O operations
- [ ] All resources properly cleaned up
- [ ] Input validation implemented

#### **Testing Validation (BLOCKING):**
- [ ] `go test ./...` passes with 80%+ coverage
- [ ] Integration tests for external dependencies
- [ ] Benchmarks for performance-critical code
- [ ] No untested public APIs

---

## ⚡ **CRITICAL SUCCESS CRITERIA**

**The CI/CD enforcement MUST:**

1. **BLOCK any PR** that violates architectural constraints
2. **PREVENT any stub code** from entering the codebase  
3. **ENFORCE 80% test coverage** minimum on all modules
4. **VALIDATE module independence** with isolated builds
5. **DETECT type safety violations** automatically
6. **ENSURE error handling compliance** across all code

**Failure Consequences:**
- ❌ **Code MUST be rejected** if any constraint is violated
- ❌ **No exceptions** without explicit architectural committee approval  
- ❌ **Technical debt is NOT acceptable** for constraint violations

---

## 🎯 **DELIVERABLES FOR AGENT 2**

1. **Enhanced GitHub Actions workflows** with blocking enforcement
2. **Comprehensive validation scripts** for all architectural rules
3. **Pre-commit hooks** for early violation detection
4. **Coverage tracking** with automatic PR blocking
5. **Dependency analysis tools** with violation reporting
6. **Type safety validation** with static analysis
7. **Integration testing** for enforcement pipeline itself

**The goal:** Create an enforcement system so robust that architectural violations become **impossible** to merge, maintaining Tapio's clean 5-level hierarchy permanently.

---

> **Agent 2**: This document provides complete specifications for implementing zero-tolerance architectural enforcement. The system should **automatically block** any code that violates these constraints, ensuring our clean modular architecture remains intact.