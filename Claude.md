Here's the combined, comprehensive version of both CLAUDE.md sections:

# 📋 TAPIO PRODUCTION STANDARDS - ZERO TOLERANCE ENFORCEMENT

## 🚨 CRITICAL: ENFORCEMENT STATUS

### ⚠️ CURRENT VIOLATIONS: 82 map[string]interface{} - GOING TO ZERO
**We deleted 124 violations! Down from 206 → 82. Adding even ONE new violation = INSTANT REJECTION**

### AUTOMATED REJECTION SYSTEM ACTIVE
```bash
# Pre-commit hooks WILL BLOCK violations:
./scripts/verify-no-interface-abuse.sh
make verify-interface
make verify-todos
make verify
```

## 🏗️ MANDATORY DEVELOPMENT WORKFLOW

### Phase 1: Design First (NO CODE WITHOUT DESIGN)
```markdown
# Feature: [Name] - REQUIRED BEFORE ANY CODE
## Problem Statement
- Current situation:
- Why it's a problem:
- Impact if not solved:

## Proposed Solution
- Core approach:
- Why this way:
- Alternatives considered:

## Implementation Plan
```ascii
┌─────────┐      ┌─────────┐      ┌─────────┐
│  Input  │ ───> │ Process │ ───> │ Output  │
└─────────┘      └─────────┘      └─────────┘
```

## Interfaces Required
```go
type Handler interface {
    Validate(input) error
    Process(input) (output, error)
    Cleanup() error
}
```

## Error Scenarios
1. Timeout → Retry with backoff
2. Invalid input → Return validation error
3. Resource unavailable → Circuit breaker

## Success Metrics
- Latency < 100ms p99
- Error rate < 0.1%
- Test coverage > 80%
```

### Phase 2: Test-Driven Development (STRICT TDD)
```go
// STEP 1: Write failing test FIRST
func TestReconciler_HandleOrphans(t *testing.T) {
    tests := []struct {
        name     string
        resource Resource
        want     Decision
        wantErr  bool
    }{
        {
            name:     "untagged_resource",
            resource: Resource{ID: "i-123"},
            want:     Decision{Action: ActionNotify},
        },
        {
            name:     "blessed_resource",
            resource: Resource{ID: "i-456", Tags: map[string]string{"sami:blessed": "true"}},
            want:     Decision{Action: ActionProtect},
        },
        {
            name:     "invalid_resource",
            resource: Resource{},
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            r := NewReconciler()
            got, err := r.HandleOrphan(tt.resource)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}

// STEP 2: Write MINIMAL code to pass
// STEP 3: Refactor with tests green
// STEP 4: Add edge cases
```

### Phase 3: Incremental Development (MAX 30 LINES PER COMMIT)
```bash
# Create feature branch
git checkout -b feat/orphan-detection

# Iteration 1: Interface only
vim orphan.go  # 10 lines max
make verify-quick  # Must pass
git add orphan.go
git commit -m "feat: define OrphanHandler interface"

# Iteration 2: Struct definition
vim orphan.go  # Add struct, 20 lines max
make verify-quick
git commit -m "feat: add Reconciler struct"

# Iteration 3: Core logic
vim orphan.go  # Add validation, 30 lines max
make verify-quick
git commit -m "feat: implement orphan detection"

# Iteration 4: Error handling
vim orphan.go  # Add error cases, 30 lines max
make verify-quick
git commit -m "feat: add comprehensive error handling"

# NEVER accumulate changes - commit every 30 lines!
```

### Phase 4: Verification Gates (EVERY COMMIT)
```bash
# Automatic pre-commit hook (.git/hooks/pre-commit)
#!/bin/bash
set -e

echo "🔍 TAPIO VERIFICATION GATES"

# Gate 1: TODOs/FIXMEs - ZERO TOLERANCE
if grep -r "TODO\|FIXME\|XXX\|HACK" --include="*.go" .; then
    echo "❌ TODOs found - complete implementation"
    exit 1
fi

# Gate 2: Formatting
if [[ $(gofmt -l . | wc -l) -ne 0 ]]; then
    echo "❌ Unformatted code"
    gofmt -l .
    exit 1
fi

# Gate 3: Build
if ! go build ./...; then
    echo "❌ Build failed"
    exit 1
fi

# Gate 4: Tests
if ! go test ./... -short -race; then
    echo "❌ Tests failed"
    exit 1
fi

# Gate 5: map[string]interface{} check
if grep "map\[string\]interface{}" *.go; then
    echo "❌ map[string]interface{} detected"
    exit 1
fi

echo "✅ All gates passed"
```

## ⛔ BANNED PATTERNS - AUTOMATIC REJECTION

### map[string]interface{} IS BANNED
```go
// ❌ NEVER - INSTANT REJECTION
func Process(data map[string]interface{}) error
config := map[string]interface{}{"timeout": 30}
var data map[string]interface{}
json.Unmarshal(raw, &data)

// ✅ ALWAYS - TYPED STRUCTS ONLY
type Config struct {
    Timeout   time.Duration `json:"timeout"`
    BatchSize int          `json:"batch_size"`
}
func Process(config Config) error
```

### NO TODOs, STUBS, OR INCOMPLETE CODE
```go
// ❌ INSTANT REJECTION
func Process() error {
    // TODO: implement
    return nil
}

// ❌ INSTANT REJECTION
func Handle() {
    // FIXME: add error handling
}

// ✅ COMPLETE IMPLEMENTATION ONLY
func Process() error {
    if err := validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    return execute()
}
```

## 🏛️ ARCHITECTURE RULES (IMMUTABLE)

### 5-Level Dependency Hierarchy
```
Level 0: pkg/domain/       # ZERO dependencies
Level 1: pkg/collectors/   # Domain ONLY
Level 2: pkg/intelligence/ # Domain + L1
Level 3: pkg/integrations/ # Domain + L1 + L2
Level 4: pkg/interfaces/   # All above
```

**VIOLATION = IMMEDIATE REJECTION**

## 🔭 OPENTELEMETRY STANDARDS (MANDATORY)

### Direct OTEL Only - NO WRAPPERS
```go
// ❌ BANNED - Custom wrappers
import "github.com/yairfalse/tapio/pkg/integrations/telemetry"

// ✅ REQUIRED - Direct OpenTelemetry
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

type Component struct {
    tracer          trace.Tracer
    eventsProcessed metric.Int64Counter
    errorsTotal     metric.Int64Counter
    processingTime  metric.Float64Histogram
}
```

## 💀 PLATFORM REALITY: LINUX-ONLY

### Development Setup
```bash
# Production: Linux with eBPF
sudo go run ./cmd/collectors

# Mac Development: Mock mode
export TAPIO_MOCK_MODE=true
go run ./cmd/collectors

# Real Testing: Colima VM
colima start --mount $HOME/tapio:w
colima ssh
cd /tapio && sudo go run ./cmd/collectors
```

## 📋 VERIFICATION COMMANDS (BEFORE EVERY COMMIT)

```bash
# Quick check during development
make verify-quick      # 5-30 seconds

# Full verification before PR
make verify-full       # Complete checks

# Must show:
✅ No TODOs/FIXMEs
✅ Code formatted
✅ Builds successfully
✅ Tests pass with race detector
✅ Coverage >= 80%
✅ No architecture violations
✅ No map[string]interface{}
```

## 🎯 DEFINITION OF DONE

- [ ] Design doc reviewed and approved
- [ ] Tests written BEFORE code
- [ ] All tests passing with -race
- [ ] Coverage >= 80% per package
- [ ] No TODOs, FIXMEs, or stubs
- [ ] All errors handled with context
- [ ] Resources properly cleaned up
- [ ] Commits <= 30 lines each
- [ ] PR <= 200 lines total
- [ ] Verification output included in PR

## 🚫 INSTANT REJECTION CRITERIA

1. ANY map[string]interface{} usage
2. ANY TODO/FIXME/stub functions
3. Missing tests or <80% coverage
4. Ignored errors (`_ = func()`)
5. Architecture violations
6. Commits > 30 lines
7. PRs > 200 lines
8. No design doc
9. interface{} in public APIs
10. Missing verification output

## 🔥 ENFORCEMENT

**NO EXCUSES. NO SHORTCUTS. NO COMPROMISES.**

- Pre-commit hooks WILL block violations
- CI WILL reject non-compliant code
- PRs WILL be closed without review
- Tasks WILL be reassigned for violations

**DELIVER EXCELLENCE OR GET REASSIGNED.**
