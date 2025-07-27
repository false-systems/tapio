# Architecture Issue: Analytics Adapter

## Problem
The file `analytics_adapter.go` in `pkg/intelligence/correlation` (Level 2) imports from `pkg/intelligence/interfaces` (Level 4), which violates the 5-level hierarchy rule.

## Current State
- The adapter is used to adapt the `SimpleCorrelationSystem` to work with the analytics engine
- It needs to return types defined in the `interfaces` package (`interfaces.Finding`, `interfaces.SemanticGroup`)
- This creates a circular dependency issue in the architecture

## Possible Solutions

### Option 1: Move the Adapter
Move `analytics_adapter.go` to a higher level package:
- `pkg/intelligence/interfaces/adapters/` - Since it's adapting to interfaces types
- `pkg/integrations/analytics/` - If it's specifically for analytics integration

### Option 2: Return Local Types
- Have the adapter return local `Finding` and `SemanticGroupSummary` types
- Let the caller (which should be at a higher level) do the conversion

### Option 3: Remove the Adapter
- If the analytics integration is not actively used, consider removing it
- Or refactor the analytics engine to work directly with correlation types

## Recommendation
Option 1 is likely the best approach - move the adapter to `pkg/integrations/analytics/` since:
1. It's an integration concern, not core correlation logic
2. Integration layer (Level 3) can import from both correlation (Level 2) and interfaces (Level 4)
3. Keeps the correlation package focused on its core responsibility

## Impact
- No functional changes needed
- Just moving the file to the appropriate layer
- Update import paths in files that use this adapter