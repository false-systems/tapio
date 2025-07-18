# Tapio Architecture Analysis - Current State vs Requirements

## Required Architecture (from Claude.md)

```
Level 0: pkg/domain/          # Zero dependencies - Core types and contracts
Level 1: pkg/collectors/      # Domain only - Data collection
Level 2: pkg/intelligence/    # Domain + L1 - Correlation, prediction
Level 3: pkg/integrations/    # Domain + L1 + L2 - OpenTelemetry, Prometheus, gRPC
Level 4: pkg/interfaces/      # All above - CLI, server, output formatting
```

## Current State Analysis

### ✅ Correctly Placed Components

1. **Level 0 - Domain** ✅
   - `pkg/domain/` - Exists as required

2. **Level 1 - Collectors** ✅
   - `pkg/collectors/` - Exists as required

3. **Level 2 - Intelligence** ✅
   - `pkg/intelligence/` - Exists as required

### ❌ Missing Required Layers

1. **Level 3 - Integrations** ❌
   - `pkg/integrations/` - MISSING
   - Should contain: OpenTelemetry, Prometheus, gRPC, webhooks

2. **Level 4 - Interfaces** ❌
   - `pkg/interfaces/` - MISSING
   - Should contain: CLI, server, output formatting, configuration

### ⚠️ Components in Wrong Location

These components exist but may be in the wrong architectural level:

1. **pkg/api/** - Should this be in `pkg/interfaces/`?
2. **pkg/server/** - Should be in `pkg/interfaces/server/`
3. **pkg/otel/** - Should be in `pkg/integrations/otel/`
4. **pkg/humanoutput/** - Should be in `pkg/interfaces/output/`
5. **pkg/k8s/** - Is this a collector? Should be in `pkg/collectors/k8s/`?
6. **pkg/correlation/** - Should this be part of `pkg/intelligence/correlation/`?

### 🔍 Components Needing Review

These components need architectural placement decisions:

1. **pkg/capabilities/** - What level? Domain extension?
2. **pkg/checker/** - What is this? Health checks?
3. **pkg/discovery/** - Service discovery? What level?
4. **pkg/events/** - Duplicate of domain events?
5. **pkg/events_correlation/** - Part of intelligence?
6. **pkg/logging/** - Utility or integration?
7. **pkg/monitoring/** - Integration layer?
8. **pkg/patternrecognition/** - Part of intelligence?
9. **pkg/performance/** - What is this?
10. **pkg/resilience/** - What is this?
11. **pkg/security/** - Cross-cutting concern?
12. **pkg/universal/** - What is this?
13. **pkg/utils/** - Utility functions

## Proposed Actions

### 1. Create Missing Layers

#### pkg/integrations/ (Level 3)
```
pkg/integrations/
├── otel/           # Move from pkg/otel/
├── prometheus/     # New - Prometheus integration
├── grpc/          # New - gRPC integration
├── webhooks/      # New - Webhook integration
└── README.md      # Integration guidelines
```

#### pkg/interfaces/ (Level 4)
```
pkg/interfaces/
├── cli/           # Command-line interface
├── server/        # Move from pkg/server/
├── output/        # Move from pkg/humanoutput/
├── config/        # Configuration management
└── README.md      # Interface guidelines
```

### 2. Components to Move

- `pkg/otel/` → `pkg/integrations/otel/`
- `pkg/server/` → `pkg/interfaces/server/`
- `pkg/humanoutput/` → `pkg/interfaces/output/`
- `pkg/api/` → `pkg/interfaces/api/` (if it's REST API)
- `pkg/k8s/` → `pkg/collectors/k8s/` (if it's collecting K8s events)
- `pkg/correlation/` → `pkg/intelligence/correlation/` (merge with intelligence)

### 3. Components Needing Clarification

Before moving forward, we need to understand:

1. What is `pkg/capabilities/`? 
2. What is `pkg/checker/`?
3. What is `pkg/discovery/`?
4. Why do we have both `pkg/events/` and `pkg/domain/`?
5. What is `pkg/universal/`?
6. Should `pkg/utils/` be allowed? (It's not in the architecture)

## Dependency Violations to Check

We must verify:
- No Level 1 components import each other
- No components import higher levels
- Each component has its own go.mod

## Next Steps

1. **Get approval** on this analysis
2. **Clarify** the purpose of ambiguous components
3. **Create** the missing layers with proper structure
4. **Move** components to correct locations
5. **Verify** dependency hierarchy compliance
6. **Update** go.mod files for isolation