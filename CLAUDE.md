# 📋 TAPIO PRODUCTION STANDARDS - ZERO TOLERANCE ENFORCEMENT

## 🚨 CRITICAL: ENFORCEMENT STATUS

### ⚠️ CURRENT VIOLATIONS: 82 map[string]interface{} - GOING TO ZERO
**We deleted 124 violations! Down from 206 → 82. Adding even ONE new violation = INSTANT REJECTION**

### AUTOMATED REJECTION SYSTEM ACTIVE
Your code WILL BE AUTOMATICALLY REJECTED if it contains:
- `map[string]interface{}` - **BANNED - USE TYPED STRUCTS ONLY**
- `interface{}` in public APIs
- `TODO`, `FIXME`, `XXX`, `HACK` comments
- Ignored errors (`_ = someFunc()`)
- Missing tests or <80% coverage
- Stub functions or incomplete implementations

```bash
# Pre-commit hooks WILL BLOCK violations:
./scripts/verify-no-interface-abuse.sh
make verify-interface
make verify-todos
make verify
```

## 🏗️ MANDATORY DEVELOPMENT WORKFLOW

### 1. Design Session First
Before writing ANY code:
```markdown
## Design Session Checklist
- [ ] What problem are we solving?
- [ ] What's the simplest solution?
- [ ] Can we break it into smaller functions?
- [ ] What interfaces do we need?
- [ ] What can go wrong?
- [ ] Draw the flow (ASCII or diagram)

## Example Design:
Problem: Detect orphaned AWS resources
Solution: Tag-based lifecycle management

Flow:
┌─────────┐      ┌─────────┐      ┌─────────┐
│  Scan   │ ───> │ Analyze │ ───> │ Decide  │
└─────────┘      └─────────┘      └─────────┘

Interface:
type OrphanHandler interface {
    Scan(context.Context) ([]Resource, error)
    Analyze(Resource) Decision
    Execute(Decision) error
}

Failure modes:
- AWS API timeout → Exponential backoff
- No tags → Mark for review
- Rate limit → Circuit breaker
```

### 2. Write Tests Before Code
```go
// FIRST: Write the test
func TestReconciler_HandleOrphans(t *testing.T) {
    // Define expected behavior
    orphan := Resource{ID: "i-123", Tags: map[string]string{}}
    reconciler := NewReconciler()
    decision := reconciler.HandleOrphan(orphan)
    assert.Equal(t, "notify", decision.Action)
}
// THEN: Write minimal code to pass
// NO STUBS - complete implementation only
```
observers should have 
  1. observer_unit_test.go - Unit tests for individual methods and components
  2. observer_e2e_test.go - End-to-end workflow tests simulating complete scenarios
  3. observer_integration_test.go - Integration tests with real network components
  4. observer_system_test.go - Linux-specific system tests for eBPF functionality
  5. observer_performance_test.go - Performance benchmarks and load tests
  6. observer_negative_test.go - Negative tests for error handling and edge cases

### 3. Code in Small Chunks
```bash
# Work on dedicated branches
git checkout -b feat/orphan-detection

# Small iterations with verification
# 1. Write function (max 30 lines) → fmt → vet → lint → commit
# 2. Add validation → test → fmt → vet → lint → commit
# 3. Add error handling → test → fmt → vet → lint → commit

# MANDATORY before EVERY commit:
go fmt ./...
go vet ./...
golangci-lint run

# Push and PR when feature is complete
git push origin feat/orphan-detection
```

**NO STUBS. NO TODOs. COMPLETE CODE ONLY.**

## ⛔ BANNED PATTERNS - AUTOMATIC REJECTION

### map[string]interface{} IS BANNED
```go
// ❌ NEVER - INSTANT REJECTION
func Process(data map[string]interface{}) error
config := map[string]interface{}{"timeout": 30}

// ✅ ALWAYS - TYPED STRUCTS ONLY
type ProcessConfig struct {
    Timeout   time.Duration `json:"timeout"`
    BatchSize int          `json:"batch_size"`
}
func Process(config ProcessConfig) error
```

### NO TODOs OR STUBS - ZERO TOLERANCE
```go
// ❌ INSTANT REJECTION
func Process() error {
    // TODO: implement
    return nil
}

// ❌ INSTANT REJECTION
func Handle() error {
    panic("not implemented")
}

// ✅ COMPLETE IMPLEMENTATION ONLY
func Process() error {
    if err := validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    if err := execute(); err != nil {
        return fmt.Errorf("execution failed: %w", err)
    }
    return cleanup()
}
```

## 🏛️ ARCHITECTURE RULES (IMMUTABLE)

### 5-Level Dependency Hierarchy
```
Level 0: pkg/domain/       # ZERO dependencies
Level 1: pkg/collectors/   # Domain ONLY
         pkg/observers/    # Domain ONLY
Level 2: pkg/intelligence/ # Domain + L1
Level 3: pkg/integrations/ # Domain + L1 + L2
Level 4: pkg/interfaces/   # All above
```

**VIOLATION = IMMEDIATE TASK REASSIGNMENT**

## 💀 PLATFORM REALITY: LINUX-ONLY WITH MOCK MODE

### Development Setup
```bash
# Production: Linux with eBPF (all observers working)
sudo go run ./cmd/observers

# Mac Development: Use mock mode for local iteration
export TAPIO_MOCK_MODE=true
go run ./cmd/observers

# Real Testing: Colima VM for eBPF
colima start --mount $HOME/tapio:w
colima ssh
cd /tapio && sudo go run ./cmd/observers
```

### Observer Architecture (NO STUBS!)
```go
//go:build linux
// +build linux

package dns

func NewObserver(name string, cfg Config) (*Observer, error) {
    // Check for mock mode
    mockMode := os.Getenv("TAPIO_MOCK_MODE") == "true"
    if mockMode {
        logger.Info("Running in MOCK MODE")
    }
    // COMPLETE IMPLEMENTATION - NO STUBS
}
```

## 🔭 OPENTELEMETRY STANDARDS (MANDATORY)

### Direct OTEL Only - NO WRAPPERS
```go
// ❌ BANNED - Custom telemetry wrappers
import "github.com/yairfalse/tapio/pkg/integrations/telemetry"

// ✅ REQUIRED - Direct OpenTelemetry
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

type Observer struct {
    // Required OTEL fields
    tracer          trace.Tracer
    eventsProcessed metric.Int64Counter
    errorsTotal     metric.Int64Counter
    processingTime  metric.Float64Histogram
}

// Metric naming standards
eventsCounter := "observer_events_processed_total"      // _total suffix
durationHist := "observer_processing_duration_ms"       // unit in name
activeGauge := "observer_active_connections"           // current state
```

## 🧪 TESTING REQUIREMENTS

### Minimum 80% Coverage - NO EXCEPTIONS
```go
// Every public function needs tests
func TestObserverLifecycle(t *testing.T) {
    observer, err := NewObserver("test")
    require.NoError(t, err)
    require.NotNil(t, observer)

    ctx := context.Background()
    err = observer.Start(ctx)
    require.NoError(t, err)

    assert.True(t, observer.IsHealthy())

    err = observer.Stop()
    require.NoError(t, err)
}

// Test error paths
func TestObserverErrors(t *testing.T) {
    observer := &Observer{}
    err := observer.ProcessEvent(nil)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "nil event")
}
```

## 📋 VERIFICATION COMMANDS (BEFORE EVERY COMMIT)

```bash
# Quick verification during development
make verify-quick      # 5-30 seconds

# Full verification before PR
make verify-full       # Complete validation

# Manual verification
gofmt -l . | grep -v vendor | wc -l    # Must return 0
go build ./...                          # Must pass
go test ./... -race                     # Must pass
go test ./... -cover                    # Must show >80%
golangci-lint run                       # No warnings
```

## 🎯 DEFINITION OF DONE

A task is ONLY complete when:
- [ ] Design doc written and reviewed
- [ ] Tests written BEFORE implementation
- [ ] All tests passing with -race detector
- [ ] Coverage >= 80% per package
- [ ] NO TODOs, FIXMEs, or stub functions
- [ ] NO map[string]interface{} anywhere
- [ ] All errors handled with context
- [ ] Resources properly cleaned up
- [ ] Each commit <= 30 lines
- [ ] PR <= 200 lines total
- [ ] make verify-full passes
- [ ] Branch builds in CI

## 🚫 INSTANT REJECTION CRITERIA

Your PR/commit will be INSTANTLY REJECTED for:

1. **ANY map[string]interface{} usage** (except JSON unmarshaling)
2. **ANY TODO/FIXME/stub functions**
3. **Missing tests or <80% coverage**
4. **Ignored errors** (`_ = func()`)
5. **Architecture violations** (importing from higher level)
6. **Commits > 30 lines**
7. **PRs > 200 lines**
8. **No design doc**
9. **interface{} in public APIs**
10. **Missing verification output**
11. **Hardcoded values** (paths, IPs, credentials)
12. **No error context** (bare errors without wrapping)

## 🔥 ERROR HANDLING STANDARDS

```go
// ❌ BAD - No context
return fmt.Errorf("failed")

// ❌ BAD - Ignored error
_ = collector.Start()

// ✅ GOOD - Contextual error with wrapping
if err := collector.Start(ctx); err != nil {
    return fmt.Errorf("failed to start collector %s: %w", name, err)
}

// ✅ GOOD - Proper error handling chain
func Process(ctx context.Context) error {
    if err := validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }

    result, err := execute(ctx)
    if err != nil {
        return fmt.Errorf("execution failed for ID %s: %w", result.ID, err)
    }

    return nil
}
```

## 🔒 RESOURCE MANAGEMENT

```go
// ❌ BAD - Resource leak
func Process() error {
    conn := getConnection()
    return doWork(conn)  // Connection never closed!
}

// ✅ GOOD - Proper cleanup with defer
func Process() error {
    conn, err := getConnection()
    if err != nil {
        return fmt.Errorf("failed to get connection: %w", err)
    }
    defer conn.Close()

    return doWork(conn)
}

// ✅ GOOD - Context-aware cleanup
func Process(ctx context.Context) error {
    conn, err := getConnection(ctx)
    if err != nil {
        return fmt.Errorf("connection failed: %w", err)
    }
    defer func() {
        if err := conn.Close(); err != nil {
            log.Printf("failed to close connection: %v", err)
        }
    }()

    return doWork(ctx, conn)
}
```

## 🚀 PERFORMANCE STANDARDS

```go
// Memory pooling for hot paths
var eventPool = sync.Pool{
    New: func() interface{} {
        return &Event{Data: make([]byte, 0, 1024)}
    },
}

// Buffered channels for producers
events := make(chan Event, 1000)  // Never unbuffered

// Preallocate slices when size is known
results := make([]Result, 0, expectedSize)
```

## 📝 GIT WORKFLOW ENFORCEMENT

### Branch Naming
```bash
feat/feature-name     # New feature
fix/bug-description   # Bug fix
perf/optimization     # Performance improvement
docs/what-changed     # Documentation only
test/what-testing     # Test additions
refactor/what-changed # Code refactoring
```

### Commit Message Format
```bash
type(scope): description

- Detailed point 1
- Detailed point 2

Closes #123
```

### PR Rules
- **Max 200 lines** (split larger changes)
- **Must pass CI** (all checks green)
- **Must include verification output**
- **Design doc linked**
- **Tests included**

## 🛡️ SECURITY STANDARDS

```go
// NEVER hardcode secrets
password := os.Getenv("DB_PASSWORD")  // Good
password := "admin123"                 // INSTANT REJECTION

// NEVER trust user input
func Process(userInput string) error {
    sanitized := sanitize(userInput)
    if err := validate(sanitized); err != nil {
        return fmt.Errorf("invalid input: %w", err)
    }
    return execute(sanitized)
}

// ALWAYS use context for cancellation
func LongRunning(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            // Do work
        }
    }
}
```

## 🎖️ QUALITY METRICS

Every component MUST maintain:
- **Test Coverage**: >= 80%
- **Cyclomatic Complexity**: < 10 per function
- **Function Length**: < 50 lines
- **File Length**: < 500 lines
- **Package Dependencies**: Follow 5-level hierarchy
- **Error Rate**: < 0.1% in production
- **Memory Leaks**: ZERO tolerance
- **Data Races**: ZERO tolerance

## 🏆 FINAL ENFORCEMENT

**NO EXCUSES. NO SHORTCUTS. NO STUBS. NO COMPROMISES.**

Every line of code represents Tapio's quality. Incomplete code, TODOs, or stubs are NEVER acceptable. Write complete, tested, production-ready code or don't write anything at all.

**Remember:**
- **NO STUBS** - Complete implementations only
- **NO TODOs** - Finish it or don't start
- **Test first** - TDD is mandatory
- **Small commits** - 30 lines maximum
- **Format always** - `go fmt` before every commit
- **No map[string]interface{}** - Typed structs only
- **80% coverage minimum** - Test everything

**DELIVER EXCELLENCE OR GET REASSIGNED.**

## 📦 PKG/ REFACTORING DESIGN SESSION (Architecture Compliance)

### Problem
Current `pkg/` structure violates the 5-Level Dependency Hierarchy by exposing implementation details as public APIs.

### Solution
Restructure to follow mandatory architecture levels:

```
Level 0: pkg/domain/       # ZERO dependencies ✅ KEEP
Level 1: internal/observers/    # Domain ONLY ❌ MOVE FROM pkg/
Level 2: internal/intelligence/ # Domain + L1 ❌ MOVE FROM pkg/
Level 3: internal/integrations/ # Domain + L1 + L2 ❌ MOVE FROM pkg/
Level 4: pkg/interfaces/   # All above ✅ KEEP
```

### Refactoring Flow
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Analyze   │───▶│  Restructure │───▶│   Verify    │
│ Current pkg │    │  Following   │    │ Standards   │
│ Structure   │    │ Architecture │    │ Compliance  │
└─────────────┘    └─────────────┘    └─────────────┘
```

### Implementation Strategy
1. **Move Level 3 first** (`pkg/integrations/` → `internal/integrations/`)
2. **Move Level 2** (`pkg/intelligence/` → `internal/intelligence/`)
3. **Move Level 1** (`pkg/observers/` → `internal/observers/`)
4. **Keep Level 0 & 4** (`pkg/domain/`, `pkg/interfaces/`)

### Failure Prevention
- **Breaking imports** → Move in reverse dependency order (Level 3 → 1)
- **Architecture violations** → Pre-verify each package's dependency level
- **Test failures** → Run `make verify-full` after each move

### Success Criteria
- [ ] Only public APIs remain in `pkg/` (domain, interfaces, config)
- [ ] All implementation details moved to `internal/`
- [ ] Architecture hierarchy properly enforced
- [ ] Zero import breaks
- [ ] All tests passing
- [ ] `make verify-full` passes