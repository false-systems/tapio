# Orphaned Packages Analysis

## Packages to Move

### To pkg/interfaces/server/
- **pkg/api/** - REST API handlers → `pkg/interfaces/server/http/`
- **pkg/server/** - Server implementation → `pkg/interfaces/server/`

### To pkg/integrations/
- **pkg/otel/** - OpenTelemetry → `pkg/integrations/otel/` (merge with existing)
- **pkg/monitoring/** - Monitoring integration → `pkg/integrations/prometheus/` or new monitoring

### To pkg/intelligence/
- **pkg/patternrecognition/** - Pattern recognition → `pkg/intelligence/patterns/`

### To pkg/collectors/ or domain/
- **pkg/k8s/** - If collector → `pkg/collectors/k8s/` (already exists!)
- **pkg/events/** - Event types → merge with `pkg/domain/`

### Utility/Cross-cutting (need decisions)
- **pkg/capabilities/** - What is this? System capabilities?
- **pkg/checker/** - Health checking? → maybe `pkg/interfaces/health/`
- **pkg/discovery/** - Service discovery? → `pkg/integrations/discovery/`?
- **pkg/health/** - Health checks → merge with `pkg/interfaces/health/`
- **pkg/logging/** - Logging utilities → keep as shared utility?
- **pkg/performance/** - Performance monitoring → `pkg/intelligence/performance/`?
- **pkg/resilience/** - Resilience patterns → `pkg/integrations/resilience/`?
- **pkg/security/** - Security utilities → keep as shared utility?
- **pkg/universal/** - Universal utilities? → investigate and distribute
- **pkg/utils/** - General utilities → distribute to relevant packages

## Migration Priority

1. **HIGH**: pkg/api → pkg/interfaces/server/http
2. **HIGH**: pkg/server → pkg/interfaces/server
3. **HIGH**: pkg/otel → pkg/integrations/otel
4. **MEDIUM**: pkg/patternrecognition → pkg/intelligence/patterns
5. **MEDIUM**: pkg/monitoring → pkg/integrations/monitoring
6. **LOW**: Utility packages (need investigation first)

## Next Steps

1. Move API and Server (they belong together)
2. Move OTEL (merge with skeleton we created)
3. Investigate utility packages to understand their purpose
4. Distribute utilities to appropriate layers