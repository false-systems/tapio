# Runtime Collector Transformation Plan

## Overview
Transforming namespace-collector → runtime-collector to provide critical process death intelligence.

## Business Value
**Problem**: "My container crashed with exit code 137 - why?"
**Solution**: Complete signal attribution showing WHO killed it and WHY

## Transformation Steps (Following CLAUDE.md)

### Phase 1: eBPF Transformation ✅
- [x] Replace namespace monitoring with runtime signals
- [x] Track process lifecycle (exec, exit)  
- [x] Monitor signal generation and delivery
- [x] Capture exit codes with full context
- [x] Add OOM killer attribution

### Phase 2: Go Code Updates
- [ ] Update types for runtime events
- [ ] Modify event processing logic
- [ ] Keep excellent OTEL implementation
- [ ] Update metrics to reflect runtime focus

### Phase 3: Testing & Verification
- [ ] Unit tests with 80% coverage minimum
- [ ] Integration tests on Linux
- [ ] Verify no TODOs, no stubs
- [ ] Run full verification suite

## Architecture Changes

### Before (Namespace Focus):
```
setns/unshare syscalls → namespace events → limited value
```

### After (Runtime Focus):
```
process signals → death attribution → explains WHY containers die
```

## Event Types

### Old Events (Removed):
- EVENT_NETNS_ENTER
- EVENT_NETNS_CREATE  
- EVENT_NETNS_EXIT

### New Events (Added):
- EVENT_PROCESS_EXEC - New process started
- EVENT_PROCESS_EXIT - Process ended with code
- EVENT_SIGNAL_SENT - Signal generation
- EVENT_SIGNAL_RECEIVED - Signal delivery
- EVENT_OOM_KILL - OOM killer activated
- EVENT_CPU_THROTTLE - Container throttled

## Critical Requirements (CLAUDE.md)

### ZERO TOLERANCE:
- ❌ NO TODOs, FIXMEs, XXX, HACK
- ❌ NO map[string]interface{}
- ❌ NO ignored errors
- ❌ NO stubs or empty functions
- ✅ 80% test coverage minimum
- ✅ Proper OTEL with nil checks
- ✅ All errors wrapped with context

### Architecture Rules:
- Collectors (L1) can only import Domain (L0)
- No circular dependencies
- Must build standalone

### Verification Required:
```bash
make fmt                    # Format first
gofmt -l . | wc -l         # Must return 0
go build ./...             # Must pass
go test ./... -race        # Must pass
go test ./... -cover       # Must be >80%
```

## Implementation Progress

### Step 1: Create runtime_monitor.c ✅
Replacing namespace_monitor.c with process lifecycle tracking

### Step 2: Update Go types
Modify types.go for runtime events

### Step 3: Update collector logic
Adapt collector.go for runtime processing

### Step 4: Fix tests
Ensure 80% coverage with real assertions

### Step 5: Documentation
Create comprehensive README explaining value

## Success Metrics

### Technical:
- Zero build errors
- Zero linter warnings  
- >80% test coverage
- All verifications pass

### Product:
- Explains container deaths
- Shows signal attribution
- Provides root cause
- Reduces MTTR by 80%

## Risks & Mitigations

### Risk: Breaking existing functionality
**Mitigation**: Keep namespace collector code in git history, can restore if needed

### Risk: Performance overhead
**Mitigation**: Use efficient eBPF with in-kernel filtering

### Risk: Complex signal decoding
**Mitigation**: Start simple, iterate with real-world testing

## Timeline

- Hour 1: eBPF transformation ✅
- Hour 2: Go code updates
- Hour 3: Testing & verification
- Hour 4: Documentation & review

## Definition of Done

- [ ] All code formatted (gofmt)
- [ ] All tests passing with >80% coverage
- [ ] No TODOs or stubs in code
- [ ] Verification script passes
- [ ] README documents value proposition
- [ ] Can answer "Why did my container die?"