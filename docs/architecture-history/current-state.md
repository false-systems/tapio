# Tapio Current State - Migration Baseline

Date: $(date)

## Current Module Structure

### Existing go.mod Files

```
Root Level:
- ./go.mod (main module)
- ./go.work (workspace)

Commands:
- ./cmd/plugins/tapio-prometheus/go.mod
- ./cmd/tapio-cli/go.mod
- ./cmd/tapio-engine/go.mod
- ./cmd/tapio-gui/go.mod

Domain Layer (Level 0):
✅ ./pkg/domain/go.mod

Collectors Layer (Level 1):
✅ ./pkg/collectors/go.mod
✅ ./pkg/collectors/ebpf/go.mod
✅ ./pkg/collectors/k8s/go.mod
✅ ./pkg/collectors/systemd/go.mod
✅ ./pkg/collectors/journald/go.mod
⚠️  ./pkg/collectors/correlation/go.mod (wrong level?)

Intelligence Layer (Level 2):
✅ ./pkg/intelligence/correlation/go.mod

Other:
❓ ./pkg/correlation/go.mod (being cleaned by Agent 2)
❓ ./pkg/humanoutput/go.mod (should be Level 4)
❓ ./pkg/patternrecognition/go.mod (should be Level 2)
❓ ./minimal-tapio/go.mod
```

## Packages Without go.mod (Need Migration)

### Found in pkg/ directory:
- pkg/api/ - Should be pkg/interfaces/api/
- pkg/capabilities/ - Unclear purpose
- pkg/checker/ - Health checking?
- pkg/discovery/ - Service discovery?
- pkg/events/ - Duplicate of domain?
- pkg/events_correlation/ - Another correlation engine
- pkg/health/ - Health checking
- pkg/logging/ - Logging utilities
- pkg/monitoring/ - Monitoring integration
- pkg/otel/ - Should be pkg/integrations/otel/
- pkg/performance/ - Performance monitoring?
- pkg/resilience/ - Resilience patterns?
- pkg/security/ - Security utilities
- pkg/server/ - Should be pkg/interfaces/server/
- pkg/simple/ - What is this?
- pkg/universal/ - Universal utilities?
- pkg/utils/ - General utilities

## Correlation Chaos (Agent 2 Working On This)

Found 6 different correlation implementations:
1. pkg/correlation/ - 44,339 lines
2. pkg/intelligence/correlation/ - 18,297 lines
3. pkg/collectors/correlation/ - 256 lines
4. pkg/events_correlation/ - 6,013 lines
5. pkg/collectors/integration/ - 6,011 lines
6. pkg/server/adapters/correlation/ - 1,767 lines

Total: ~76,683 lines of correlation code!

## Missing Required Layers

### Level 3 - Integrations (NOT EXISTS)
Need to create:
- pkg/integrations/otel/
- pkg/integrations/prometheus/
- pkg/integrations/grpc/
- pkg/integrations/webhooks/

### Level 4 - Interfaces (NOT EXISTS)
Need to create:
- pkg/interfaces/cli/
- pkg/interfaces/server/
- pkg/interfaces/output/
- pkg/interfaces/config/

## Action Items for Phase 1

1. ✅ Migration scripts created
2. ⏳ Document current dependencies
3. ⏳ Set up CI/CD checks
4. ⏳ Create architectural decision records