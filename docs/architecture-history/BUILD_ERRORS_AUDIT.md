# Correlation Package Build Errors Audit

## Executive Summary

The correlation package has systematic build errors due to:
1. **Multiple conflicting type definitions** across different files
2. **Missing method implementations** on struct types
3. **Interface vs struct mismatches** in pattern engines

## Detailed Findings

### 1. PatternResult - Multiple Definitions (CRITICAL)

Found **4 different definitions** of `PatternResult`:

#### Definition 1: `/pkg/correlation/pattern_engines.go:106`
```go
type PatternResult struct {
    PatternID   string
    PatternType string
    Confidence  float64
    Description string
    Timestamp   time.Time
    Evidence    map[string]interface{}
}
```

#### Definition 2: `/pkg/correlation/core/interfaces.go:95`
```go
type PatternResult struct {
    PatternID        string                 `json:"pattern_id"`
    PatternName      string                 `json:"pattern_name"`
    Type             string                 `json:"type"`
    Confidence       float64                `json:"confidence"`
    Detected         time.Time              `json:"detected"`
    AffectedEntities []Entity               `json:"affected_entities"`
    Severity         Severity               `json:"severity"`
    Description      string                 `json:"description"`
    Evidence         []Evidence             `json:"evidence"`
    Predictions      []Prediction           `json:"predictions"`
    Metadata         map[string]interface{} `json:"metadata"`
}
```

#### Definition 3: `/pkg/correlation/foundation/data_types.go:57`
```go
type PatternResult struct {
    PatternID        string                 `json:"pattern_id"`
    PatternName      string                 `json:"pattern_name"`
    PatternType      string                 `json:"pattern_type"`
    Version          string                 `json:"version"`
    Detected         bool                   `json:"detected"`
    Confidence       float64                `json:"confidence"`
    DetectionTime    time.Time              `json:"detection_time"`
    AffectedEntities []Entity               `json:"affected_entities"`
    Severity         Severity               `json:"severity"`
    Description      string                 `json:"description"`
    Evidence         []Evidence             `json:"evidence"`
    Predictions      []Prediction           `json:"predictions"`
    RootCause        *RootCause             `json:"root_cause,omitempty"`
    Remediation      []RemediationAction    `json:"remediation,omitempty"`
    Metadata         map[string]interface{} `json:"metadata,omitempty"`
}
```

#### Definition 4: `/pkg/correlation/types/patterns.go:8`
```go
type PatternResult struct {
    PatternID   string
    PatternName string
    Detected    bool
    Confidence  float64
    Severity    Severity
    Category    Category
    // ... many more fields including Timeline, CausalChain, etc.
}
```

### 2. Missing Method Implementations

The following methods are referenced but not implemented:

#### TemporalPatternEngine
- **Missing**: `FindSequences(ctx context.Context, events []*OpinionatedEvent) ([]*TemporalSequence, error)`
- **Found**: `DetectSequences` exists in `pattern_engines_helpers.go:255` but returns different type
- **Used in**: `pattern_matcher.go:159`

#### CausalityPatternEngine  
- **Missing**: `DetectCausality(ctx context.Context, event *OpinionatedEvent) ([]*CausalityPattern, error)`
- **Found**: `FindCausalChains` exists but has different signature
- **Used in**: `pattern_matcher.go:168`

#### PatternCache
- **Missing**: `Set(key string, result *PatternResult) error`
- **Found**: No implementation exists
- **Used in**: `pattern_matcher.go:276`

#### EmbeddingIndex
- **Missing**: `AddEmbedding(eventID string, embedding []float32) error`
- **Found**: No implementation exists
- **Used in**: `pattern_matcher.go:289`

### 3. Struct Type Definitions

All pattern engines are defined as **structs**, not interfaces:

```go
// In pattern_engines.go
type TemporalPatternEngine struct {
    sequences map[string]*TemporalSequence
}

type CausalityPatternEngine struct {
    causalChains map[string]*CausalChain
}

type PatternCache struct {
    cache map[string]*PatternResult
    mu    sync.RWMutex
}

type EmbeddingIndex struct {
    embeddings map[string][]float64
    mu         sync.RWMutex
}
```

### 4. Root Causes

1. **Package Organization Issue**: Multiple packages (core, foundation, types, root) defining the same types
2. **No Clear Ownership**: No single source of truth for type definitions
3. **Method Naming Mismatch**: Helper methods exist with different names than what's being called
4. **Missing Implementations**: Several critical methods are simply not implemented

### 5. Impact Analysis

- **Build Failures**: Code won't compile due to missing methods
- **Type Conflicts**: Import conflicts when using PatternResult from different packages
- **Integration Issues**: pattern_matcher.go expects methods that don't exist
- **Maintenance Debt**: Multiple definitions make updates error-prone

## Recommendations

1. **Consolidate Type Definitions**
   - Choose ONE package for each type definition
   - Remove duplicate definitions
   - Use type aliases for backward compatibility

2. **Implement Missing Methods**
   - Add `Set` method to PatternCache
   - Add `AddEmbedding` method to EmbeddingIndex
   - Rename or add wrapper methods for temporal/causality engines

3. **Consider Interface Extraction**
   - Define interfaces for pattern engines
   - Keep concrete implementations separate
   - Use dependency injection for flexibility

4. **Package Structure Reform**
   - core/ - Core interfaces only
   - foundation/ - Basic implementations
   - types/ - Shared type definitions (single source)
   - Root package - High-level orchestration

5. **Immediate Actions**
   - Fix method signatures to match usage
   - Remove duplicate PatternResult definitions
   - Implement missing cache and embedding methods