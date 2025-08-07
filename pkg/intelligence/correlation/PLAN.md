# Correlation API Implementation Plan

## Current Assessment

### What's Working
- Correlation engine with 5 correlators
- Neo4j storage for correlations
- Query capabilities (WhyDidPodFail, WhatImpactsService, etc.)
- Basic API server with direct Neo4j queries

### What's Missing
- API handlers that use the intelligence service
- Connection between correlation results and API responses
- Caching for expensive queries
- Real-time correlation streaming
- Proper metrics and monitoring

## Phase 1: API Integration (Current Task)

### Step 1: Create API Handler Interface
Create `/pkg/interfaces/api/correlation/handlers.go`:
- CorrelationHandler interface
- Implementation that uses intelligence.Service
- Proper error handling and logging

### Step 2: Define API Types
Create `/pkg/interfaces/api/correlation/types.go`:
- Request/Response types for correlation queries
- Error types for API responses
- Pagination support

### Step 3: Implement Handlers
- WhyDidThisFailHandler - root cause analysis
- WhatDoesThisImpactHandler - impact analysis  
- GetCorrelationsHandler - list correlations
- GetCascadeFailuresHandler - cascade patterns
- HealthHandler - correlation system health

### Step 4: Update API Server
- Remove direct Neo4j queries
- Use new correlation handlers
- Add dependency injection for intelligence service

### Step 5: Add Tests
- Unit tests for handlers
- Integration tests with mock Neo4j
- API contract tests

## Questions Before Starting

1. **Should we keep the existing API contract?**
   - Current: `/api/v1/why?pod=X&namespace=Y`
   - Alternative: RESTful `/api/v1/correlations/root-cause/pod/X`

2. **How should we handle correlation types?**
   - Current: Hardcoded queries for pods/services
   - Proposed: Generic correlation query with resource type

3. **What about pagination?**
   - Add limit/offset parameters?
   - Use cursor-based pagination?

4. **Error handling strategy?**
   - Return correlation confidence even on partial failures?
   - Timeout handling for slow queries?

5. **Caching strategy?**
   - Cache at handler level or service level?
   - TTL based on correlation type?

## Next Immediate Steps

1. Create the handler interface and types
2. Implement the first handler (WhyDidThisFail)
3. Test with existing API server
4. Get feedback before implementing remaining handlers

Should I proceed with this plan?