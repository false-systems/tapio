# Tapio Architecture Migration Plan

## Executive Summary

This plan outlines a systematic approach to migrate Tapio from its current mixed architecture to a clean, enforced 5-level hierarchy as defined in Claude.md. The migration will be done in phases to minimize disruption while ensuring each step is fully functional.

## Current State Problems

1. **Mixed architecture** - Components at wrong levels
2. **Circular dependencies** - Uncontrolled imports between modules
3. **No enforcement** - Architecture violations only caught in review
4. **Incomplete implementations** - Stubs and TODOs throughout
5. **Unclear boundaries** - Components with ambiguous purposes

## Target Architecture

```
Level 0: pkg/domain/          # Zero dependencies - Core types
Level 1: pkg/collectors/      # Domain only - Data collection
Level 2: pkg/intelligence/    # Domain + L1 - Analysis & correlation
Level 3: pkg/integrations/    # Domain + L1 + L2 - External systems
Level 4: pkg/interfaces/      # All above - User interfaces
```

## Migration Phases

### Phase 1: Prepare Migration Infrastructure (Week 1)

#### 1.1 Create Migration Scripts
```bash
# Create scripts/migration/ directory
mkdir -p scripts/migration

# Script to verify dependencies
scripts/migration/check-dependencies.sh

# Script to create module structure
scripts/migration/create-module.sh

# Script to move packages
scripts/migration/move-package.sh
```

#### 1.2 Document Current State
```bash
# Generate dependency graph
go mod graph > docs/migration/current-dependencies.txt

# List all packages and their imports
scripts/migration/analyze-imports.sh > docs/migration/current-imports.txt

# Identify circular dependencies
scripts/migration/find-circular.sh > docs/migration/circular-deps.txt
```

#### 1.3 Set Up CI/CD Checks
```yaml
# .github/workflows/architecture-check.yml
name: Architecture Compliance
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Check dependency hierarchy
        run: scripts/migration/check-dependencies.sh
```

### Phase 2: Fix Domain Layer (Week 1-2)

#### 2.1 Verify Domain Independence
```bash
# Check domain has zero external dependencies
cd pkg/domain
go list -deps ./... | grep -v "github.com/falseyair/tapio/pkg/domain"
# Should return empty
```

#### 2.2 Complete Domain Implementation
- Remove ALL stubs/TODOs
- Ensure 80%+ test coverage
- Document all types and interfaces

#### 2.3 Tag Domain Version
```bash
git tag pkg/domain/v1.0.0
git push origin pkg/domain/v1.0.0
```

### Phase 3: Reorganize Level 1 - Collectors (Week 2-3)

#### 3.1 Move Existing Collectors
```bash
# K8s collector (if it's actually a collector)
scripts/migration/move-package.sh pkg/k8s pkg/collectors/k8s

# Create proper structure for each collector
for collector in ebpf k8s systemd journald runtime; do
    scripts/migration/create-module.sh pkg/collectors/$collector
done
```

#### 3.2 Fix Each Collector
For each collector:
1. Create independent go.mod
2. Import ONLY pkg/domain
3. Remove all stubs
4. Implement full functionality
5. Add comprehensive tests
6. Tag version

#### 3.3 Example: eBPF Collector
```bash
cd pkg/collectors/ebpf

# Create go.mod
go mod init github.com/falseyair/tapio/pkg/collectors/ebpf
go mod edit -require github.com/falseyair/tapio/pkg/domain@v1.0.0

# Verify dependencies
go list -deps ./... | grep "github.com/falseyair/tapio"
# Should only show domain imports

# Tag when complete
git tag pkg/collectors/ebpf/v1.0.0
```

### Phase 4: Consolidate Level 2 - Intelligence (Week 3-4)

#### 4.1 Merge Correlation Components
```bash
# Move correlation components to intelligence
scripts/migration/move-package.sh pkg/correlation pkg/intelligence/correlation
scripts/migration/move-package.sh pkg/events_correlation pkg/intelligence/correlation
scripts/migration/move-package.sh pkg/patternrecognition pkg/intelligence/patterns
```

#### 4.2 Create Intelligence Modules
```bash
# Each intelligence component gets its own module
cd pkg/intelligence/correlation
go mod init github.com/falseyair/tapio/pkg/intelligence/correlation
go mod edit -require github.com/falseyair/tapio/pkg/domain@v1.0.0
go mod edit -require github.com/falseyair/tapio/pkg/collectors/ebpf@v1.0.0
# Add other collector dependencies as needed
```

#### 4.3 Implement Intelligence Features
- Semantic correlation engine
- Pattern recognition
- Anomaly detection
- Prediction algorithms

### Phase 5: Create Level 3 - Integrations (Week 4-5)

#### 5.1 Move Existing Integrations
```bash
# Move OTEL
scripts/migration/move-package.sh pkg/otel pkg/integrations/otel

# Move monitoring if it's an integration
scripts/migration/move-package.sh pkg/monitoring pkg/integrations/prometheus
```

#### 5.2 Create New Integrations
```bash
# Create integration structure
for integration in otel prometheus grpc webhooks; do
    scripts/migration/create-module.sh pkg/integrations/$integration
done
```

#### 5.3 Implement Each Integration
- OpenTelemetry exporter
- Prometheus metrics
- gRPC server for external communication
- Webhook dispatcher

### Phase 6: Create Level 4 - Interfaces (Week 5-6)

#### 6.1 Move Existing Interfaces
```bash
# Move server
scripts/migration/move-package.sh pkg/server pkg/interfaces/server

# Move API
scripts/migration/move-package.sh pkg/api pkg/interfaces/server/http

# Move human output
scripts/migration/move-package.sh pkg/humanoutput pkg/interfaces/output
```

#### 6.2 Create CLI Interface
```bash
# Create CLI with cobra
cd pkg/interfaces/cli
go mod init github.com/falseyair/tapio/pkg/interfaces/cli
go get github.com/spf13/cobra

# Implement commands
# - tapio check
# - tapio collect
# - tapio analyze
# - tapio config
```

#### 6.3 Consolidate Configuration
```bash
# Create unified config management
scripts/migration/create-module.sh pkg/interfaces/config
```

### Phase 7: Clean Up Orphaned Packages (Week 6)

#### 7.1 Analyze Remaining Packages
```bash
# List packages not in new architecture
find pkg/ -name "go.mod" | grep -v -E "(domain|collectors|intelligence|integrations|interfaces)"
```

#### 7.2 Decision Matrix
| Package | Current Location | Decision | New Location |
|---------|-----------------|----------|--------------|
| pkg/capabilities | Root | Move | pkg/domain/capabilities |
| pkg/checker | Root | Move | pkg/interfaces/health |
| pkg/discovery | Root | Move | pkg/collectors/discovery |
| pkg/events | Root | Remove | Use pkg/domain |
| pkg/logging | Root | Move | pkg/interfaces/logging |
| pkg/utils | Root | Distribute | Various locations |
| pkg/universal | Root | Analyze | TBD |
| pkg/performance | Root | Move | pkg/intelligence/performance |
| pkg/resilience | Root | Move | pkg/integrations/resilience |
| pkg/security | Root | Distribute | Cross-cutting |

#### 7.3 Execute Cleanup
```bash
# For each orphaned package
# 1. Determine if needed
# 2. Move to correct location or remove
# 3. Update imports in dependent code
```

### Phase 8: Implementation Enforcement (Week 7)

#### 8.1 Update go.work
```go
// go.work
go 1.21

use (
    ./pkg/domain
    ./pkg/collectors/ebpf
    ./pkg/collectors/k8s
    ./pkg/collectors/systemd
    ./pkg/collectors/journald
    ./pkg/intelligence/correlation
    ./pkg/intelligence/patterns
    ./pkg/integrations/otel
    ./pkg/integrations/prometheus
    ./pkg/integrations/grpc
    ./pkg/integrations/webhooks
    ./pkg/interfaces/cli
    ./pkg/interfaces/server
    ./pkg/interfaces/output
    ./pkg/interfaces/config
)
```

#### 8.2 Create Makefile Targets
```makefile
# Makefile additions
.PHONY: check-arch
check-arch:
	@echo "Checking architecture compliance..."
	@scripts/migration/check-dependencies.sh

.PHONY: build-all
build-all:
	@echo "Building all modules..."
	@for module in $$(find pkg -name go.mod -type f); do \
		echo "Building $$(dirname $$module)..."; \
		cd $$(dirname $$module) && go build ./... || exit 1; \
		cd - > /dev/null; \
	done

.PHONY: test-all
test-all:
	@echo "Testing all modules..."
	@for module in $$(find pkg -name go.mod -type f); do \
		echo "Testing $$(dirname $$module)..."; \
		cd $$(dirname $$module) && go test ./... || exit 1; \
		cd - > /dev/null; \
	done
```

#### 8.3 CI/CD Pipeline
```yaml
# .github/workflows/build.yml
name: Build and Test
on: [push, pull_request]
jobs:
  architecture-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Check architecture
        run: make check-arch
  
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Build all modules
        run: make build-all
  
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Test all modules
        run: make test-all
```

## Success Criteria

### Per Component
- ✅ Independent go.mod file
- ✅ Correct dependency hierarchy
- ✅ No stubs or TODOs
- ✅ 80%+ test coverage
- ✅ Comprehensive documentation
- ✅ Tagged version

### Overall System
- ✅ No circular dependencies
- ✅ Clean build with GOWORK=off
- ✅ All tests pass
- ✅ Architecture check passes in CI
- ✅ Can build and deploy each component independently

## Risk Mitigation

1. **Gradual Migration**: Each phase is self-contained
2. **Continuous Testing**: Nothing breaks existing functionality
3. **Rollback Plan**: Git tags at each successful phase
4. **Parallel Development**: Use go.work during migration
5. **Clear Communication**: Document all changes

## Timeline Summary

- Week 1: Infrastructure and Domain
- Week 2-3: Collectors (Level 1)
- Week 3-4: Intelligence (Level 2)
- Week 4-5: Integrations (Level 3)
- Week 5-6: Interfaces (Level 4)
- Week 6: Cleanup
- Week 7: Enforcement and Documentation

Total: 7 weeks for complete migration

## Next Steps

1. **Review and approve** this plan
2. **Create migration scripts**
3. **Start with Phase 1**
4. **Daily progress updates**
5. **Weekly architecture review**