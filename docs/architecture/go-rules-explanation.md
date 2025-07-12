# Go Code Rules - What It Means for Tapio

## Overview

Using Go code for correlation rules means writing actual Go functions instead of configuration files. Your rules become part of your codebase with all the benefits of a compiled language.

## What This Looks Like in Practice

### 1. Rules as Code Structure

```
tapio/
├── pkg/
│   └── correlation/
│       ├── engine.go          # The correlation engine
│       ├── rules/             # Your correlation rules live here
│       │   ├── memory.go      # Memory-related rules
│       │   ├── network.go     # Network-related rules
│       │   ├── cpu.go         # CPU-related rules
│       │   └── registry.go    # Rule registration
│       └── types.go           # Common types
```

### 2. Writing a Rule

Instead of YAML configuration, you write Go functions:

```go
// pkg/correlation/rules/memory.go
package rules

import (
    "fmt"
    "time"
    "github.com/yairfalse/tapio/pkg/correlation"
)

// MemoryPressureCascade detects when memory pressure causes cascading failures
func MemoryPressureCascade() *correlation.Rule {
    return &correlation.Rule{
        ID:          "memory-pressure-cascade",
        Name:        "Memory Pressure Cascade Detection",
        Description: "Detects when OOM kills lead to pod restarts and service failures",
        Category:    correlation.CategoryResource,
        
        // This function runs for every event window
        Evaluate: func(ctx *correlation.Context) *correlation.Result {
            // Get relevant events from the correlation context
            oomKills := ctx.GetEvents(correlation.Filter{
                Source: "ebpf",
                Type:   "oom_kill",
            })
            
            podRestarts := ctx.GetEvents(correlation.Filter{
                Source: "kubernetes", 
                Type:   "pod.restart",
                Since:  ctx.Window.Start,
            })
            
            // Your correlation logic in plain Go
            if len(oomKills) == 0 || len(podRestarts) < 2 {
                return nil // No correlation found
            }
            
            // Check temporal relationship
            firstOOM := oomKills[0]
            firstRestart := podRestarts[0]
            
            timeDiff := firstRestart.Timestamp.Sub(firstOOM.Timestamp)
            if timeDiff > 30*time.Second || timeDiff < 0 {
                return nil // Events too far apart
            }
            
            // Check if events are related (same node/pod)
            if !ctx.EntitiesRelated(firstOOM.Entity, firstRestart.Entity) {
                return nil
            }
            
            // Calculate confidence score
            confidence := 0.5
            if len(oomKills) > 1 {
                confidence += 0.2
            }
            if ctx.SameNode(firstOOM, firstRestart) {
                confidence += 0.2
            }
            
            // Build the result
            return &correlation.Result{
                RuleID:     "memory-pressure-cascade",
                Confidence: confidence,
                Severity:   correlation.SeverityHigh,
                
                Title: fmt.Sprintf("Memory pressure cascade on %s", 
                    firstOOM.Entity.Node),
                    
                Description: fmt.Sprintf(
                    "OOM kill triggered %d pod restarts within 30 seconds. "+
                    "The node appears to be under memory pressure.",
                    len(podRestarts),
                ),
                
                Evidence: correlation.Evidence{
                    Events: append(oomKills, podRestarts...),
                    Metrics: map[string]float64{
                        "oom_count":     float64(len(oomKills)),
                        "restart_count": float64(len(podRestarts)),
                        "time_span":     timeDiff.Seconds(),
                    },
                },
                
                Recommendations: []string{
                    "Increase memory limits for affected pods",
                    "Consider adding more nodes to the cluster",
                    "Review memory usage patterns of applications",
                },
                
                Actions: []correlation.Action{
                    {
                        Type:        "alert",
                        Target:      "ops-team",
                        Priority:    "high",
                    },
                    {
                        Type:        "annotate",
                        Target:      firstOOM.Entity.String(),
                        Annotation:  "memory-pressure-detected",
                    },
                },
            }
        },
    }
}
```

### 3. Registering Rules

Rules are registered when your application starts:

```go
// pkg/correlation/rules/registry.go
package rules

import "github.com/yairfalse/tapio/pkg/correlation"

// RegisterAll registers all correlation rules with the engine
func RegisterAll(engine *correlation.Engine) error {
    rules := []*correlation.Rule{
        MemoryPressureCascade(),
        CPUThrottleDetection(),
        NetworkDropCorrelation(),
        DiskPressurePattern(),
        ServiceCascadeFailure(),
    }
    
    for _, rule := range rules {
        if err := engine.RegisterRule(rule); err != nil {
            return fmt.Errorf("failed to register rule %s: %w", rule.ID, err)
        }
    }
    
    return nil
}
```

### 4. Testing Rules

Since rules are Go code, you can unit test them:

```go
// pkg/correlation/rules/memory_test.go
package rules

import (
    "testing"
    "time"
    "github.com/stretchr/testify/assert"
)

func TestMemoryPressureCascade(t *testing.T) {
    tests := []struct {
        name       string
        events     []correlation.Event
        wantResult bool
        wantConf   float64
    }{
        {
            name: "detects OOM followed by restarts",
            events: []correlation.Event{
                {
                    Type:      "oom_kill",
                    Source:    "ebpf",
                    Timestamp: time.Now(),
                    Entity:    correlation.Entity{Node: "node1", Pod: "app-123"},
                },
                {
                    Type:      "pod.restart", 
                    Source:    "kubernetes",
                    Timestamp: time.Now().Add(10 * time.Second),
                    Entity:    correlation.Entity{Node: "node1", Pod: "app-123"},
                },
                {
                    Type:      "pod.restart",
                    Source:    "kubernetes", 
                    Timestamp: time.Now().Add(15 * time.Second),
                    Entity:    correlation.Entity{Node: "node1", Pod: "app-456"},
                },
            },
            wantResult: true,
            wantConf:   0.7,
        },
        {
            name: "ignores unrelated events",
            events: []correlation.Event{
                {
                    Type:      "oom_kill",
                    Source:    "ebpf",
                    Timestamp: time.Now(),
                    Entity:    correlation.Entity{Node: "node1"},
                },
                {
                    Type:      "pod.restart",
                    Source:    "kubernetes",
                    Timestamp: time.Now().Add(2 * time.Minute), // Too far apart
                    Entity:    correlation.Entity{Node: "node1"},
                },
            },
            wantResult: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := correlation.NewTestContext(tt.events)
            rule := MemoryPressureCascade()
            
            result := rule.Evaluate(ctx)
            
            if tt.wantResult {
                assert.NotNil(t, result)
                assert.Equal(t, tt.wantConf, result.Confidence)
            } else {
                assert.Nil(t, result)
            }
        })
    }
}
```

### 5. Benefits of Go Code Rules

#### **Compile-Time Safety**
```go
// This won't compile - caught immediately
podRestarts := ctx.GetEvents(correlation.Filter{
    Sorce: "kubernetes", // Typo! Compiler error
})

// With YAML, this typo would only fail at runtime
```

#### **IDE Support**
- Auto-completion for all methods and fields
- Jump to definition
- Refactoring support
- Inline documentation

#### **Complex Logic Made Simple**
```go
// Try writing this in YAML!
func NetworkAnomalyDetection() *correlation.Rule {
    return &correlation.Rule{
        Evaluate: func(ctx *correlation.Context) *correlation.Result {
            // Statistical analysis
            packets := ctx.GetMetricSeries("network.packets", ctx.Window)
            mean, stddev := packets.Statistics()
            
            // Anomaly detection
            recent := packets.Last(5 * time.Minute)
            for _, point := range recent {
                if math.Abs(point.Value-mean) > 3*stddev {
                    // Anomaly detected
                    // Check for correlated events
                    drops := ctx.GetEvents(correlation.Filter{
                        Type:   "packet.drop",
                        Since:  point.Timestamp.Add(-30 * time.Second),
                        Until:  point.Timestamp.Add(30 * time.Second),
                    })
                    
                    if len(drops) > 10 {
                        return &correlation.Result{
                            Title: "Network anomaly with packet drops",
                            // ... rest of result
                        }
                    }
                }
            }
            return nil
        },
    }
}
```

#### **Reusable Components**
```go
// Common patterns can be extracted into functions
func checkMemoryPressure(ctx *correlation.Context, threshold float64) bool {
    memUsage := ctx.GetMetric("memory.usage_percent")
    return memUsage > threshold
}

func checkCPUThrottle(ctx *correlation.Context) bool {
    throttleRatio := ctx.GetMetric("cpu.throttle_ratio")
    return throttleRatio > 0.2
}

// Use in multiple rules
func ResourceExhaustionRule() *correlation.Rule {
    return &correlation.Rule{
        Evaluate: func(ctx *correlation.Context) *correlation.Result {
            memPressure := checkMemoryPressure(ctx, 0.9)
            cpuThrottle := checkCPUThrottle(ctx)
            
            if memPressure && cpuThrottle {
                return &correlation.Result{
                    Title: "Multiple resource exhaustion detected",
                }
            }
            return nil
        },
    }
}
```

### 6. How Rules Are Executed

```go
// The correlation engine runs continuously
type Engine struct {
    rules  map[string]*Rule
    events *EventStream
}

func (e *Engine) Run(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    
    for {
        select {
        case <-ticker.C:
            // Create correlation context with recent events
            corrCtx := &Context{
                Window: TimeWindow{
                    Start: time.Now().Add(-5 * time.Minute),
                    End:   time.Now(),
                },
                Events: e.events.GetWindow(window),
            }
            
            // Run all rules
            for _, rule := range e.rules {
                if result := rule.Evaluate(corrCtx); result != nil {
                    e.handleResult(result)
                }
            }
            
        case <-ctx.Done():
            return
        }
    }
}
```

## Summary

Using Go code for rules means:

1. **Rules are functions** in your codebase, not config files
2. **Full programming power** - loops, conditions, math, statistics
3. **Compile-time validation** - errors caught before deployment
4. **Testable** - unit tests for each rule
5. **IDE support** - autocomplete, refactoring, documentation
6. **Performance** - compiled code, no parsing overhead
7. **Version controlled** - rules change with your code

It's like the difference between:
- **YAML/Config**: Writing instructions for someone else to follow
- **Go Code**: Writing the actual logic yourself

This approach is used by many production systems because it provides the perfect balance of power, safety, and maintainability.