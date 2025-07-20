# Tapio Development Guidelines

## 🎯 Mission

Build enterprise-grade observability platform with modular architecture and semantic correlation.

## 🏗️ Architecture Rules (CRITICAL)

### 5-Level Hierarchy (MANDATORY)

```
Level 0: pkg/domain/          # Zero dependencies
Level 1: pkg/collectors/      # Domain only
Level 2: pkg/intelligence/    # Domain + L1
Level 3: pkg/integrations/    # Domain + L1 + L2
Level 4: pkg/interfaces/      # All above
```

**RULE:** Components can only import from lower levels. NO exceptions.

### Module Structure (MANDATORY)

- We use one go.mod file.
- Must build standalone: `cd pkg/X && go build ./...`
- Must test standalone: `cd pkg/X && go test ./...`

## ⚡ Agent Instructions (BRUTAL)

### Build Requirements

1. **MUST FORMAT:** `make fmt` before any commit
2. **MUST COMPILE:** `go build ./...` must pass
3. **MUST TEST:** `go test ./...` must pass
4. **NO STUBS:** No "TODO", "not implemented", empty functions
5. **SHOW PROOF:** Paste build/test output or FAIL
6.

### Quality Standards

- **80% test coverage minimum**
- **No `map[string]interface{}`** in public APIs
- **No `interface{}`** abuse
- **Proper error handling** with context
- **NO Stubs, no shortcuts**
- **YOU work on a dedicated branch**

### Verification (MANDATORY)

```bash
# You MUST show this output:
make fmt                 # Format code first
gofmt -l . | grep -v vendor | wc -l    # MUST return 0
go build ./...
go test ./...
go mod verify
```

## 🔧 Current Priorities
- Getting the whole thing to work


### Success Metrics

- All collectors building independently ✅
- Semantic correlation working ✅
- CI/CD enforcement active ✅
- Revenue features ready

### Core Mission

- **Root Cause Analysis:** Every correlation must identify WHY, not just WHAT happened
- **Best Practices:** No shortcuts, no stubs, no endless loops of "fixing/thinking/asking" - deliver working solutions

## 🚫 Failure Conditions

### Instant Task Reassignment If

- Code not formatted (gofmt failures)
- Build errors
- Test failures
- Architectural violations
- Missing verification output
- Stub functions or TODOs

### No Excuses For

- "Forgot to format" - Always run `make fmt`
- "Complex existing code" - Use what exists
- "Need to refactor first" - Follow requirements
- "Just one small TODO" - Zero tolerance
- "Can't find interfaces" - Ask for help

## 📋 Task Template

Every task must include:

```markdown
## Verification Results

### Code Formatting:
```bash
$ make fmt
[PASTE OUTPUT - should show "Code formatted successfully" or similar]

$ gofmt -l . | grep -v vendor | wc -l
0
[MUST be 0 - if not 0, code is not properly formatted]
```

### Build Test

```bash
$ go build ./...
[PASTE OUTPUT]
```

### Unit Tests

```bash
$ go test ./...
[PASTE OUTPUT]
```

### Files Created

- file1.go (X lines)
- file2.go (Y lines)
Total: Z lines

## Architecture Compliance

✅ Code properly formatted
✅ Follows 5-level hierarchy
✅ Independent go.mod
✅ No architectural violations
✅ Proper imports only

```

## 🎯 Bottom Line

**Format code. Build working code. Prove it works. Follow architecture. No shortcuts.**

Deliver or get reassigned.
