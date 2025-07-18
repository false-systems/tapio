# Tapio Architecture Migration Plan

## 🎯 Goal: Implement REAL Architecture from Claude.md

**Timeline**: 3-4 weeks  
**Approach**: Systematic, phase-by-phase migration  
**Principle**: Do it RIGHT, not fast  

---

## 📋 Phase 1: Foundation (Week 1)

### Day 1-2: Document Reality
- [ ] Map ACTUAL dependencies (use `go list -deps`)
- [ ] Document which correlation engine is REALLY used
- [ ] List all duplicate implementations
- [ ] Create dependency graph of current state

### Day 3-4: Create Missing Layers
```
pkg/
├── integrations/        # NEW - Level 3
│   ├── go.mod
│   ├── opentelemetry/
│   ├── prometheus/
│   └── webhooks/
└── interfaces/          # NEW - Level 4
    ├── go.mod
    ├── cli/
    ├── server/
    └── gui/
```

### Day 5: Migration Planning
- [ ] Decide which correlation implementation to keep
- [ ] Plan component moves
- [ ] Create validation scripts

---

## 🔧 Phase 2: Core Cleanup (Week 2)

### Day 6-7: Correlation Consolidation
**Decision**: Keep `pkg/intelligence/correlation/` (most modular)
- [ ] Migrate useful patterns from other implementations
- [ ] Create migration scripts
- [ ] Test thoroughly

### Day 8-9: Delete Duplicates
- [ ] Delete `pkg/correlation/` (the monster)
- [ ] Delete `pkg/events_correlation/`
- [ ] Delete duplicate correlation in collectors
- [ ] Remove all .bak, .old files

### Day 10: Move Components
- [ ] Move `pkg/collector/` → `pkg/collectors/integration/`
- [ ] Move CLI from `internal/cli/` → `pkg/interfaces/cli/`
- [ ] Move server from `pkg/server/` → `pkg/interfaces/server/`

---

## 🏗️ Phase 3: Dependency Fix (Week 3)

### Day 11-12: Fix Imports
- [ ] Update all import paths
- [ ] Remove circular dependencies
- [ ] Add proper interfaces at each level

### Day 13-14: Module Independence
- [ ] Each package gets proper go.mod
- [ ] Minimal dependencies only
- [ ] Can build independently

### Day 15: Integration Layer
- [ ] OpenTelemetry in `pkg/integrations/opentelemetry/`
- [ ] Prometheus in `pkg/integrations/prometheus/`
- [ ] Clean interfaces between layers

---

## ✅ Phase 4: Validation (Week 4)

### Day 16-17: Testing
- [ ] Each module builds independently
- [ ] No circular dependencies
- [ ] All tests pass

### Day 18-19: Documentation
- [ ] Update architecture docs
- [ ] Create ADRs for decisions
- [ ] Update Claude.md if needed

### Day 20: Enforcement
- [ ] Add CI checks for architecture
- [ ] Dependency validation scripts
- [ ] Pre-commit hooks

---

## 🎯 Success Criteria

### Architecture Compliance
```bash
# No violations when running:
go list -deps ./... | grep -E "collectors.*intelligence|intelligence.*integrations"
# Should return EMPTY
```

### Clean Structure
```
pkg/
├── domain/              # Level 0 ✓
├── collectors/          # Level 1 ✓
├── intelligence/        # Level 2 ✓
│   └── correlation/     # ONE implementation ✓
├── integrations/        # Level 3 ✓
└── interfaces/          # Level 4 ✓
```

### Build Independence
```bash
cd pkg/collectors/ebpf && go build ./...  # Works ✓
cd pkg/intelligence/correlation && go build ./...  # Works ✓
cd pkg/interfaces/cli && go build ./...  # Works ✓
```

---

## ⚠️ Risks & Mitigations

### Risk 1: Breaking Production
**Mitigation**: Feature flag for correlation engine switch

### Risk 2: Hidden Dependencies
**Mitigation**: Extensive testing at each phase

### Risk 3: Scope Creep
**Mitigation**: Strict phase boundaries, no new features

---

## 🏁 Final State

A clean, modular architecture that:
- ✅ Follows Claude.md rules EXACTLY
- ✅ Has ONE way to do each thing
- ✅ Build and tests pass
- ✅ New developers can understand
- ✅ Ready for long-term growth

No more lies, no more shortcuts. Just clean, honest architecture! 💪