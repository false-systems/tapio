# kubectl-Era Package Deletion Plan

## Packages to Delete (kubectl enhancement legacy)

### kubectl-Specific Packages:
1. **pkg/capabilities/** (5,270 lines) - kubectl command helpers
2. **pkg/discovery/** (4,875 lines) - K8s service discovery for kubectl
3. **pkg/checker/** (878 lines) - kubectl health checking
4. **pkg/integrations/k8s/** (3,443 lines) - kubectl K8s client (just moved)

### Likely Outdated Utility Packages:
5. **pkg/utils/** (358 lines) - small utilities
6. **pkg/universal/** (5,383 lines) - universal converters/formatters

## Packages to Keep and Migrate:
- **pkg/events/** → merge with domain (contains domain events)
- **pkg/health/** → move to interfaces (general health checking)
- **pkg/logging/** → move to interfaces (general logging)
- **pkg/monitoring/** → move to integrations (monitoring integration)
- **pkg/performance/** → move to intelligence (performance analysis)
- **pkg/resilience/** → move to integrations (resilience patterns)
- **pkg/security/** → move to integrations (security integrations)

## Benefits of Deletion:
- **~20,000 lines** of kubectl-era code removed
- **Cleaner architecture** focused on observability
- **Faster builds** and less maintenance
- **Clear separation** from kubectl legacy

## Before Deletion - Dependency Check:
Check if any current code imports these packages before deletion.