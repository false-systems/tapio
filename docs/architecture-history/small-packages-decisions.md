# Small Packages Migration Decisions

Based on package analysis, here are the migration decisions:

## Large Packages (>3K lines) - Need Migration

### 1. pkg/events/ (11,078 lines) → MERGE with pkg/domain/
**Reason**: Contains "correlation_ready.go" - likely domain event types
**Action**: Merge into domain layer, clean up duplicates

### 2. pkg/resilience/ (8,721 lines) → pkg/integrations/resilience/
**Reason**: Resilience patterns (circuit breakers, etc.) - integration patterns
**Action**: Move to integrations as new module

### 3. pkg/universal/ (5,383 lines) → DISTRIBUTE
**Reason**: Contains converters/formatters - distribute to appropriate layers
**Action**: Move converters to domain, formatters to interfaces/output

### 4. pkg/capabilities/ (5,270 lines) → pkg/collectors/capabilities/ 
**Reason**: System capability detection - used by collectors
**Action**: Move to collectors layer

### 5. pkg/discovery/ (4,875 lines) → pkg/integrations/discovery/
**Reason**: Service discovery - external system integration
**Action**: Move to integrations as new module

### 6. pkg/k8s/ (3,443 lines) → CHECK if duplicate of pkg/collectors/k8s/
**Reason**: Might be duplicate collector
**Action**: Compare with existing, merge if duplicate

## Medium Packages (1K-3K lines) - Integrate

### 7. pkg/security/ (2,527 lines) → pkg/integrations/security/
**Reason**: Security auditing and auth - cross-cutting integration
**Action**: Move to integrations

### 8. pkg/performance/ (2,767 lines) → pkg/intelligence/performance/
**Reason**: Performance monitoring and analysis
**Action**: Move to intelligence layer

### 9. pkg/monitoring/ (1,523 lines) → pkg/integrations/monitoring/
**Reason**: Monitoring integration (likely Prometheus-related)
**Action**: Move to integrations

## Small Packages (<1K lines) - Simple Moves

### 10. pkg/checker/ (878 lines) → MERGE with pkg/health/
**Reason**: Both are health checking utilities
**Action**: Merge both into pkg/interfaces/health/

### 11. pkg/health/ (675 lines) → pkg/interfaces/health/
**Reason**: Health checking for interfaces
**Action**: Move to interfaces layer

### 12. pkg/logging/ (847 lines) → pkg/interfaces/logging/
**Reason**: Logging utilities for interfaces
**Action**: Move to interfaces layer

### 13. pkg/utils/ (358 lines) → DISTRIBUTE
**Reason**: Too small to be standalone
**Action**: Distribute functions to relevant packages

## Migration Priority

**HIGH PRIORITY (Large impacts):**
1. Merge pkg/events/ → pkg/domain/
2. Check pkg/k8s/ vs pkg/collectors/k8s/ for duplicates
3. Move pkg/universal/ converters and formatters

**MEDIUM PRIORITY:**
4. Move pkg/resilience/ → pkg/integrations/resilience/
5. Move pkg/capabilities/ → pkg/collectors/capabilities/
6. Move pkg/discovery/ → pkg/integrations/discovery/

**LOW PRIORITY (Simple moves):**
7. Merge pkg/health/ + pkg/checker/ → pkg/interfaces/health/
8. Move remaining packages to appropriate layers

## Expected Results

After migration:
- ~47K lines properly positioned
- Zero orphaned packages
- Clean architectural boundaries
- All code in correct layers