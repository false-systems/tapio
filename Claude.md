# Claude Code Agent Configuration for Tapio

## 🌲 **Project Overview**

**Tapio** is an advanced system monitoring and intelligence platform that provides predictive analytics, correlation analysis, and automated incident response for complex distributed systems. Named after the Finnish forest god, Tapio watches over your digital infrastructure with the wisdom of nature.

### **Core Capabilities**

- **Intelligent Event Collection**: Multi-source data gathering from eBPF, Kubernetes, systemd, journald, and container runtimes
- **Advanced Correlation**: Sophisticated pattern recognition and causal analysis across system events
- **Predictive Analytics**: Machine learning-powered prediction of failures, resource exhaustion, and performance issues
- **Self-Healing Integration**: Automated responses and remediation through OpenTelemetry, Prometheus, and webhook integrations
- **Human-Friendly Interfaces**: CLI and server interfaces that explain complex correlations in understandable terms

### **What Makes Tapio Special**

1. **Deep System Understanding**: Goes beyond simple metrics to understand causal relationships between events
2. **Predictive Intelligence**: Anticipates problems before they become critical issues
3. **Contextual Correlation**: Understands how different system layers interact and influence each other
4. **Extensible Architecture**: Modular design allows adding new data sources and intelligence algorithms
5. **Zero-Dependency Domain**: Clean, type-safe foundation that's easy to understand and maintain

---

## 🏗️ **Architecture Overview**

Tapio follows a strict **5-level dependency hierarchy** designed for modularity, testability, and maintainability:

```
Level 0: pkg/domain/          # Zero dependencies - Core types and contracts
Level 1: pkg/collectors/      # Domain only - Data collection (eBPF, K8s, systemd, journald, runtime)
Level 2: pkg/intelligence/    # Domain + L1 - Correlation, prediction, anomaly detection, patterns
Level 3: pkg/integrations/    # Domain + L1 + L2 - OpenTelemetry, Prometheus, gRPC, webhooks
Level 4: pkg/interfaces/      # All above - CLI, server, output formatting, configuration
```

### **Component Isolation**

- **No cross-imports** between same-level components
- **Communication via APIs**, not Go imports
- **Independent modules** with minimal dependencies
- **Platform-specific builds** with stub implementations

### **Current Implementation Status**

- ✅ **Domain Layer**: Type-safe foundation with Event, Correlation, and Finding models
- 🚧 **Next Phase**: Level 1 collectors starting with eBPF
- 📋 **Planned**: Intelligence layer with correlation engine and prediction algorithms

---

## 🎯 **Development Philosophy**

### **Quality Over Speed**

- **No technical debt** - Build it right the first time
- **No stubs, no shortcuts** - Every function must be fully implemented
- **Type safety first** - Eliminate `map[string]interface{}` abuse
- **Test everything** - 80% minimum coverage, comprehensive integration tests
- **Document thoroughly** - Self-explanatory code with rich documentation

### **Predictable Architecture**

- **Consistent patterns** across all components
- **Clear interfaces** between layers
- **Explicit error handling** with rich context
- **Resource management** with proper cleanup

### **Extensibility by Design**

- **Interface-driven** development for easy mocking and testing
- **Plugin architecture** for new data sources and algorithms
- **Configuration-driven** behavior where appropriate
- **Platform abstraction** for cross-platform compatibility

---

# 🔒 **MANDATORY CONSTRAINTS FOR ALL CLAUDE CODE AGENTS**

### **CRITICAL: Every Claude Code agent MUST follow these rules without exception**

---

## 📋 **SECTION 1: ARCHITECTURE CONSTRAINTS**

### **🏗️ Rule A1: Dependency Hierarchy (MANDATORY)**

```
STRICT HIERARCHY - NO EXCEPTIONS:
Level 0: pkg/domain/          # Zero external dependencies
Level 1: pkg/collectors/      # ONLY depends on pkg/domain
Level 2: pkg/intelligence/    # ONLY depends on pkg/domain + Level 1
Level 3: pkg/integrations/    # ONLY depends on pkg/domain + Level 1 + Level 2  
Level 4: pkg/interfaces/      # Can depend on all above levels
```

**ENFORCEMENT:**

- ❌ **FORBIDDEN:** Any Level 1 component importing another Level 1 component
- ❌ **FORBIDDEN:** Any component importing a higher-level component
- ✅ **REQUIRED:** Each component MUST have its own go.mod with minimal dependencies
- ✅ **REQUIRED:** Run `go list -deps` to verify hierarchy compliance

### **🏗️ Rule A2: Component Isolation (MANDATORY)**

```
NO DIRECT GO IMPORTS BETWEEN SAME-LEVEL COMPONENTS
```

**Examples:**

- ❌ `pkg/collectors/ebpf` importing `pkg/collectors/k8s`
- ❌ `pkg/intelligence/correlation` importing `pkg/intelligence/prediction`
- ✅ Communication via HTTP/gRPC APIs
- ✅ Communication via message queues
- ✅ Communication via file-based interfaces

### **🏗️ Rule A3: Module Structure (MANDATORY)**

Every component MUST have:

```
pkg/[area]/[component]/
├── go.mod                    # Independent module
├── core/                     # Core interfaces and types
│   ├── interfaces.go         # Component contracts
│   ├── types.go             # Component-specific types
│   └── errors.go            # Component-specific errors
├── internal/                # Internal implementation
├── [platform]/             # Platform-specific code (linux/, windows/, etc.)
├── stub/                    # Stub implementations for unsupported platforms
└── README.md               # Component documentation
```

### **🏗️ Rule A4: Implementation Completeness (MANDATORY)**

```
NO STUBS, NO SHORTCUTS, NO PLACEHOLDER CODE
```

**ENFORCEMENT:**

- ❌ **FORBIDDEN:** Functions that return `nil, fmt.Errorf("not implemented")`
- ❌ **FORBIDDEN:** Empty function bodies with TODO comments
- ❌ **FORBIDDEN:** Placeholder implementations that don't work
- ❌ **FORBIDDEN:** "We'll fix this later" code
- ✅ **REQUIRED:** Every function must be fully implemented and working
- ✅ **REQUIRED:** Every feature must be complete before moving to next component
- ✅ **REQUIRED:** All code paths must be tested and functional

**Example of FORBIDDEN code:**

```go
// ❌ ABSOLUTELY FORBIDDEN
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    // TODO: implement this later
    return nil, fmt.Errorf("not implemented")
}

func (c *Collector) ProcessEvent(event Event) error {
    // We'll add the real logic later
    return nil
}
```

**Example of REQUIRED code:**

```go
// ✅ REQUIRED - Fully implemented, working code
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    if err := criteria.Validate(); err != nil {
        return nil, fmt.Errorf("invalid criteria: %w", err)
    }
    
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    // Real implementation with error handling, validation, and proper logic
    events, err := c.queryDataSource(ctx, criteria)
    if err != nil {
        return nil, fmt.Errorf("failed to query data source: %w", err)
    }
    
    return events, nil
}
```

### **🏗️ Rule A5: Independent Build and Run (MANDATORY)**

```
EVERY AREA MUST BUILD AND RUN INDEPENDENTLY
```

**ENFORCEMENT:**

- ✅ **REQUIRED:** Each area must build without requiring other areas
- ✅ **REQUIRED:** Each area must have its own executable/runnable components
- ✅ **REQUIRED:** Each area must have standalone tests that don't depend on other areas
- ✅ **REQUIRED:** Each area must have its own configuration and initialization
- ❌ **FORBIDDEN:** Build dependencies between areas at the same level
- ❌ **FORBIDDEN:** Runtime dependencies that prevent standalone operation

**Examples of REQUIRED independence:**

```bash
# Each area builds independently
cd pkg/collectors/ebpf && go build ./...     # Works standalone
cd pkg/collectors/k8s && go build ./...      # Works standalone
cd pkg/intelligence/correlation && go build ./... # Works standalone

# Each area tests independently  
cd pkg/collectors/ebpf && go test ./...      # No external dependencies
cd pkg/intelligence/correlation && go test ./... # No external dependencies

# Each area can run standalone for testing
cd pkg/collectors/ebpf && go run ./cmd/collector  # Runs independently
cd pkg/interfaces/cli && go run ./cmd/tapio check # Runs with available components
```

**REQUIRED Area Structure:**

```
pkg/[area]/[component]/
├── go.mod                    # Independent module
├── cmd/                      # Standalone executables for testing
│   ├── collector/           # Area-specific test runners
│   └── debug/               # Debug utilities
├── core/                     # Core interfaces and types
├── internal/                # Internal implementation  
├── testdata/                # Test data and fixtures
└── examples/                # Usage examples
```

**REQUIRED Independence Patterns:**

- **Graceful degradation** when other areas are unavailable
- **Configuration-driven** feature enablement
- **Interface-based** communication with optional implementations
- **Self-contained** test suites with mocked dependencies

```
NO STUBS, NO SHORTCUTS, NO PLACEHOLDER CODE
```

**ENFORCEMENT:**

- ❌ **FORBIDDEN:** Functions that return `nil, fmt.Errorf("not implemented")`
- ❌ **FORBIDDEN:** Empty function bodies with TODO comments
- ❌ **FORBIDDEN:** Placeholder implementations that don't work
- ❌ **FORBIDDEN:** "We'll fix this later" code
- ✅ **REQUIRED:** Every function must be fully implemented and working
- ✅ **REQUIRED:** Every feature must be complete before moving to next component
- ✅ **REQUIRED:** All code paths must be tested and functional

**Example of FORBIDDEN code:**

```go
// ❌ ABSOLUTELY FORBIDDEN
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    // TODO: implement this later
    return nil, fmt.Errorf("not implemented")
}

func (c *Collector) ProcessEvent(event Event) error {
    // We'll add the real logic later
    return nil
}
```

**Example of REQUIRED code:**

```go
// ✅ REQUIRED - Fully implemented, working code
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    if err := criteria.Validate(); err != nil {
        return nil, fmt.Errorf("invalid criteria: %w", err)
    }
    
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    // Real implementation with error handling, validation, and proper logic
    events, err := c.queryDataSource(ctx, criteria)
    if err != nil {
        return nil, fmt.Errorf("failed to query data source: %w", err)
    }
    
    return events, nil
}
```

Every component MUST have:

```
pkg/[area]/[component]/
├── go.mod                    # Independent module
├── core/                     # Core interfaces and types
│   ├── interfaces.go         # Component contracts
│   ├── types.go             # Component-specific types
│   └── errors.go            # Component-specific errors
├── internal/                # Internal implementation
├── [platform]/             # Platform-specific code (linux/, windows/, etc.)
├── stub/                    # Stub implementations for unsupported platforms
└── README.md               # Component documentation
```

---

## 🔍 **SECTION 2: CODE QUALITY CONSTRAINTS**

### **💎 Rule Q1: Type Safety (MANDATORY)**

```go
❌ FORBIDDEN: map[string]interface{} without strong justification
❌ FORBIDDEN: interface{} in public APIs
❌ FORBIDDEN: any without explicit comment explaining why
✅ REQUIRED: Strongly-typed structs for all data
✅ REQUIRED: Validation methods for all input types
✅ REQUIRED: Type assertions with error handling
```

**Example of REQUIRED pattern:**

```go
// ✅ GOOD - Type safe
type Config struct {
    Host     string        `json:"host" validate:"required"`
    Port     int           `json:"port" validate:"min=1,max=65535"`
    Timeout  time.Duration `json:"timeout" validate:"min=1s"`
}

func (c Config) Validate() error {
    // Validation logic here
    return nil
}

// ❌ BAD - Type unsafe
type Config map[string]interface{}
```

### **💎 Rule Q2: Error Handling (MANDATORY)**

```go
✅ REQUIRED: Every error must be handled explicitly
✅ REQUIRED: Wrapped errors with context using fmt.Errorf("context: %w", err)
✅ REQUIRED: Custom error types for domain-specific errors
❌ FORBIDDEN: Silent error ignoring with _
❌ FORBIDDEN: Panic in library code (except for programmer errors)
```

**REQUIRED Error Pattern:**

```go
// Custom error types
type ValidationError struct {
    Field   string
    Value   interface{}
    Message string
}

func (e ValidationError) Error() string {
    return fmt.Sprintf("validation failed for field %s: %s", e.Field, e.Message)
}

// Error wrapping
func processData(data []byte) error {
    if err := validate(data); err != nil {
        return fmt.Errorf("failed to process data: %w", err)
    }
    return nil
}
```

### **💎 Rule Q3: Context Handling (MANDATORY)**

```go
✅ REQUIRED: Every public function that does I/O MUST accept context.Context as first parameter
✅ REQUIRED: Context cancellation MUST be respected
✅ REQUIRED: Context timeout MUST be handled gracefully
❌ FORBIDDEN: Using context.Background() in library code
```

**REQUIRED Context Pattern:**

```go
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    // Use ctx throughout the function
    return c.doCollection(ctx, criteria)
}
```

### **💎 Rule Q4: Resource Management (MANDATORY)**

```go
✅ REQUIRED: Every resource MUST have explicit cleanup
✅ REQUIRED: Use defer for cleanup when possible
✅ REQUIRED: Implement io.Closer for stateful types
✅ REQUIRED: Context-aware resource cleanup
❌ FORBIDDEN: Resource leaks
```

**REQUIRED Resource Pattern:**

```go
type Collector struct {
    conn   net.Conn
    cancel context.CancelFunc
}

func (c *Collector) Close() error {
    c.cancel()
    if c.conn != nil {
        return c.conn.Close()
    }
    return nil
}

func NewCollector(ctx context.Context) (*Collector, error) {
    ctx, cancel := context.WithCancel(ctx)
    conn, err := dial(ctx)
    if err != nil {
        cancel()
        return nil, fmt.Errorf("failed to create collector: %w", err)
    }
    
    return &Collector{
        conn:   conn,
        cancel: cancel,
    }, nil
}
```

---

## 🧪 **SECTION 3: TESTING CONSTRAINTS**

### **🧪 Rule T1: Test Coverage (MANDATORY)**

```
✅ REQUIRED: Minimum 80% test coverage for all public functions
✅ REQUIRED: Unit tests for all validation logic
✅ REQUIRED: Integration tests for external dependencies
✅ REQUIRED: Benchmark tests for performance-critical code
❌ FORBIDDEN: Untested public APIs
```

### **🧪 Rule T2: Test Structure (MANDATORY)**

```go
✅ REQUIRED: Table-driven tests for multiple scenarios
✅ REQUIRED: Test helpers in testhelpers/ directory
✅ REQUIRED: Mock implementations for external dependencies
✅ REQUIRED: Cleanup in test teardown
```

**REQUIRED Test Pattern:**

```go
func TestCollector_CollectEvents(t *testing.T) {
    tests := []struct {
        name     string
        criteria Criteria
        want     []Event
        wantErr  bool
    }{
        {
            name:     "valid criteria",
            criteria: validCriteria(),
            want:     expectedEvents(),
            wantErr:  false,
        },
        {
            name:     "invalid criteria",
            criteria: invalidCriteria(),
            want:     nil,
            wantErr:  true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            c := setupCollector(t)
            defer c.Close()
            
            got, err := c.CollectEvents(context.Background(), tt.criteria)
            
            if (err != nil) != tt.wantErr {
                t.Errorf("CollectEvents() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("CollectEvents() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## ⚡ **SECTION 4: PERFORMANCE CONSTRAINTS**

### **⚡ Rule P1: Memory Management (MANDATORY)**

```go
✅ REQUIRED: Pre-allocate slices with known capacity
✅ REQUIRED: Reuse buffers where possible
✅ REQUIRED: Limit concurrent goroutines with semaphores
✅ REQUIRED: Pool expensive objects
❌ FORBIDDEN: Unbounded memory growth
❌ FORBIDDEN: Memory leaks
```

### **⚡ Rule P2: Goroutine Management (MANDATORY)**

```go
✅ REQUIRED: Bounded goroutine pools
✅ REQUIRED: Graceful shutdown with context cancellation
✅ REQUIRED: WaitGroup for goroutine coordination
❌ FORBIDDEN: Unbounded goroutine creation
❌ FORBIDDEN: Goroutine leaks
```

---

## 🔐 **SECTION 5: SECURITY CONSTRAINTS**

### **🔐 Rule S1: Input Validation (MANDATORY)**

```go
✅ REQUIRED: Validate ALL external inputs
✅ REQUIRED: Sanitize data before processing
✅ REQUIRED: Rate limiting for external requests
✅ REQUIRED: Timeout limits for all operations
❌ FORBIDDEN: Trusting external data
❌ FORBIDDEN: SQL injection vulnerabilities
❌ FORBIDDEN: Path traversal vulnerabilities
```

### **🔐 Rule S2: Secrets Management (MANDATORY)**

```go
✅ REQUIRED: Environment variables for secrets
✅ REQUIRED: Clear secrets from memory after use
✅ REQUIRED: No secrets in logs
✅ REQUIRED: Encrypted secrets at rest
❌ FORBIDDEN: Hardcoded secrets
❌ FORBIDDEN: Secrets in configuration files
❌ FORBIDDEN: Secrets in error messages
```

---

## 📚 **SECTION 6: DOCUMENTATION CONSTRAINTS**

### **📚 Rule D1: Code Documentation (MANDATORY)**

```go
✅ REQUIRED: Godoc comments for ALL public types and functions
✅ REQUIRED: Examples in documentation
✅ REQUIRED: Error documentation
✅ REQUIRED: Performance characteristics documentation
```

**REQUIRED Documentation Pattern:**

```go
// CollectEvents retrieves events matching the specified criteria.
// It returns a slice of events and an error if the operation fails.
//
// The context is used for cancellation and timeout control.
// If the context is cancelled, the operation will return immediately.
//
// Performance: This operation has O(n) time complexity where n is the
// number of events in the time window.
//
// Errors:
//   - ValidationError: if criteria is invalid
//   - ConnectionError: if unable to connect to data source
//   - TimeoutError: if operation exceeds context deadline
//
// Example:
//   criteria := Criteria{TimeWindow: TimeWindow{Start: start, End: end}}
//   events, err := collector.CollectEvents(ctx, criteria)
//   if err != nil {
//       return fmt.Errorf("failed to collect events: %w", err)
//   }
func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    // Implementation
}
```

---

## ✅ **VALIDATION CHECKLIST**

### **Before Submitting ANY Code, Verify:**

#### **Architecture Validation:**

- [ ] Component is at correct dependency level
- [ ] No cross-imports between same-level components  
- [ ] Independent go.mod with minimal dependencies
- [ ] Correct directory structure

#### **Implementation Validation:**

- [ ] No stub functions or placeholder code
- [ ] Every function is fully implemented and working
- [ ] All code paths tested and functional
- [ ] No "TODO: implement later" comments

#### **Independence Validation:**

- [ ] Area builds without requiring other areas (`cd pkg/[area]/[component] && go build ./...`)
- [ ] Area tests run independently (`go test ./...` works standalone)
- [ ] Area has standalone executables for testing and debugging
- [ ] Area gracefully handles missing dependencies from other areas
- [ ] Area configuration is self-contained

#### **Code Quality Validation:**

- [ ] No `map[string]interface{}` without justification
- [ ] All errors handled explicitly
- [ ] Context passed to all I/O operations
- [ ] All resources properly cleaned up
- [ ] Input validation implemented

#### **Testing Validation:**

- [ ] `go test ./...` passes
- [ ] Coverage ≥ 80% (`go test -cover ./...`)
- [ ] Benchmarks for performance-critical code
- [ ] Integration tests for external dependencies

#### **Performance Validation:**

- [ ] No unbounded memory growth
- [ ] Goroutines properly managed
- [ ] No resource leaks
- [ ] Appropriate use of pools and caching

#### **Security Validation:**

- [ ] All inputs validated
- [ ] No hardcoded secrets
- [ ] Rate limiting implemented
- [ ] Timeouts configured

#### **Documentation Validation:**

- [ ] All public APIs documented
- [ ] Examples provided
- [ ] Error conditions documented
- [ ] README.md updated

---

## 🛠️ **Quick Reference Commands**

### **Validation Commands:**

```bash
# Architecture validation
go list -deps pkg/[component]               # Check dependency hierarchy
go mod graph | grep pkg/                    # Visualize module dependencies

# Build verification  
go build ./...                              # Verify everything compiles
go mod tidy                                 # Clean up dependencies

# Testing
go test ./...                               # Run all tests
go test -cover ./...                        # Test with coverage
go test -race ./...                         # Race condition detection
go test -bench=. ./...                      # Run benchmarks

# Code quality
go vet ./...                                # Static analysis
gofmt -l .                                  # Format checking
golint ./...                                # Linting (if available)

# Security
go list -m all                              # Audit dependencies
```

### **Independence Commands:**

```bash
# Build and test individual areas
cd pkg/collectors/ebpf && go build ./... && go test ./...
cd pkg/collectors/k8s && go build ./... && go test ./...
cd pkg/intelligence/correlation && go build ./... && go test ./...

# Run standalone area executables
cd pkg/collectors/ebpf && go run ./cmd/collector --config=testdata/config.yaml
cd pkg/interfaces/cli && go run ./cmd/tapio check --collectors=ebpf

# Validate area independence
go list -deps pkg/[area]/[component]        # Check dependency hierarchy
go mod why pkg/[external-dependency]        # Understand why dependencies exist
```

### **Component Creation Template:**

```bash
# Create new component structure
mkdir -p pkg/[area]/[component]/{core,internal,[platform],stub}
cd pkg/[area]/[component]

# Initialize module
go mod init github.com/falseyair/tapio/pkg/[area]/[component]

# Add domain dependency
go mod edit -require github.com/falseyair/tapio/pkg/domain@latest
```

---

## 🚨 **ENFORCEMENT**

### **Automated Checks (REQUIRED):**

Every component MUST pass all validation checks before acceptance.

### **Manual Review (REQUIRED):**

- Architecture review for dependency compliance
- Code review for quality standards  
- Security review for security constraints
- Performance review for critical paths

### **Failure Consequences:**

- ❌ **Code MUST be rejected** if any constraint is violated
- ❌ **No exceptions** without explicit architectural committee approval
- ❌ **Technical debt is NOT acceptable** for constraint violations

---

## 🏁 **FINAL MANDATE**

**Every Claude Code agent MUST:**

1. **Read and understand** these constraints completely
2. **Apply ALL rules** without exception
3. **Implement everything fully** - no stubs, no shortcuts, no placeholders
4. **Validate compliance** before submitting code
5. **Document any deviations** with architectural justification

**NO CODE will be accepted that violates these constraints.**
**NO INCOMPLETE IMPLEMENTATIONS will be accepted.**

These rules ensure we build a maintainable, secure, performant, and scalable system that serves as the foundation for Tapio's sophisticated intelligence and correlation capabilities.
