# Rule Engine Specification

## Overview

The Tapio rule engine provides a declarative, extensible framework for defining correlation rules that operate on multi-source event streams. Rules are defined in YAML format and compiled to efficient execution plans.

## Rule Definition Formats

We support multiple formats for defining correlation rules. Choose the one that best fits your use case:

### Option 1: Go Code (Recommended for Complex Rules)

```go
package rules

import (
    "github.com/yairfalse/tapio/pkg/correlation"
    "time"
)

// MemoryPressureCascade detects memory pressure leading to service failures
var MemoryPressureCascade = &correlation.Rule{
    Name:        "memory-pressure-cascade",
    Description: "Detects memory pressure leading to service failures",
    Version:     "1.0.0",
    Tags:        []string{"memory", "cascade", "critical"},
    
    Sources: correlation.Sources{
        correlation.SourceEBPF{Required: true, Features: []string{"memory_tracking", "oom_detection"}},
        correlation.SourceK8s{Required: true, Features: []string{"pod_metrics", "events"}},
        correlation.SourceSystemd{Required: false},
    },
    
    Window: correlation.Window{
        Duration: 5 * time.Minute,
        Slide:    30 * time.Second,
    },
    
    MinConfidence: 0.7,
    
    // Define what events to collect
    Collect: []correlation.Collector{
        {
            Name: "oom_events",
            Filter: correlation.Filter{
                Source: "ebpf",
                Type:   "oom_kill",
            },
        },
        {
            Name: "pod_restarts",
            Filter: correlation.Filter{
                Source: "kubernetes",
                Type:   "pod.restart",
                Within: 1 * time.Minute,
            },
        },
        {
            Name: "service_failures",
            Filter: correlation.Filter{
                Source:        "systemd",
                Type:          "service.failed",
                EntityRelated: true,
            },
        },
    },
    
    // Correlation logic
    Evaluate: func(ctx *correlation.Context) *correlation.Result {
        oomEvents := ctx.Events("oom_events")
        podRestarts := ctx.Events("pod_restarts")
        serviceFailures := ctx.Events("service_failures")
        
        // Check conditions
        if len(oomEvents) == 0 || len(podRestarts) < 2 {
            return nil
        }
        
        if oomEvents[0].Timestamp.Sub(podRestarts[0].Timestamp) > 30*time.Second {
            return nil
        }
        
        // Calculate confidence
        confidence := 0.5
        if len(oomEvents) > 1 {
            confidence += 0.2
        }
        if len(serviceFailures) > 0 {
            confidence += 0.2
        }
        if ctx.SameNode(oomEvents[0], podRestarts[0]) {
            confidence += 0.1
        }
        
        // Determine severity
        severity := correlation.SeverityMedium
        if len(podRestarts) >= 5 {
            severity = correlation.SeverityCritical
        } else if len(podRestarts) >= 3 {
            severity = correlation.SeverityHigh
        }
        
        return &correlation.Result{
            Confidence: min(confidence, 1.0),
            Severity:   severity,
            Title:      "Memory pressure cascade detected",
            Description: fmt.Sprintf("Memory pressure on %s caused %d pod restarts following OOM events",
                ctx.Entity().Node, len(podRestarts)),
            Recommendations: []string{
                "Increase memory limits for affected pods",
                "Add more nodes to distribute memory load",
                "Review application memory usage patterns",
            },
        }
    },
}
```

### Option 2: CEL (Common Expression Language) for Simple Rules

```go
// Using CEL for declarative rules with compile-time validation
var CPUThrottleRule = correlation.NewCELRule(`
    name: "cpu-throttle-detection"
    description: "Detects CPU throttling affecting performance"
    
    when: |
        ebpf.cpu_throttle.exists() &&
        ebpf.cpu_throttle.throttle_ratio > 0.2 &&
        k8s.pod.cpu_usage > 0.8
        
    confidence: |
        0.3 + 
        (ebpf.cpu_throttle.throttle_ratio * 0.5) +
        (has(systemd.service.timeout) ? 0.2 : 0.0)
        
    severity: |
        ebpf.cpu_throttle.throttle_ratio > 0.5 ? "high" : "medium"
        
    output:
        title: "CPU throttling detected on " + k8s.pod.name
        impact: duration_since(ebpf.cpu_throttle.start_time)
`)
```

### Option 3: HCL (HashiCorp Configuration Language) for Operators

```hcl
rule "memory_pressure_cascade" {
  description = "Detects memory pressure leading to service failures"
  version     = "1.0.0"
  tags        = ["memory", "cascade", "critical"]
  
  source "ebpf" {
    required = true
    features = ["memory_tracking", "oom_detection"]
  }
  
  source "kubernetes" {
    required = true
    features = ["pod_metrics", "events"]
  }
  
  window {
    duration = "5m"
    slide    = "30s"
  }
  
  collect "oom_events" {
    source = "ebpf"
    type   = "oom_kill"
  }
  
  collect "pod_restarts" {
    source = "kubernetes"
    type   = "pod.restart"
    within = "1m"
  }
  
  condition {
    all = [
      "count(oom_events) >= 1",
      "count(pod_restarts) >= 2",
      "time_between(oom_events[0], pod_restarts[0]) < 30s"
    ]
  }
  
  analyze {
    confidence = <<-EOT
      base = 0.5
      if count(oom_events) > 1 {
        base += 0.2
      }
      if same_node(oom_events, pod_restarts) {
        base += 0.1
      }
      return min(base, 1.0)
    EOT
    
    severity = count(pod_restarts) >= 5 ? "critical" : "high"
  }
  
  output {
    title = "Memory pressure cascade detected"
    description = "Memory pressure on ${entity.node} caused ${count(pod_restarts)} pod restarts"
  }
}
```

### Option 4: JSON Schema with TypeScript Type Safety

```typescript
// TypeScript interface for compile-time type checking
interface CorrelationRule {
  name: string;
  description: string;
  version: string;
  sources: SourceRequirement[];
  window: TimeWindow;
  collect: EventCollector[];
  conditions: Condition[];
  analyze: AnalysisFunction;
  output: OutputTemplate;
}

// Example rule with full type safety
const memoryPressureRule: CorrelationRule = {
  name: "memory-pressure-cascade",
  description: "Detects memory pressure leading to service failures",
  version: "1.0.0",
  
  sources: [
    { type: "ebpf", required: true, features: ["memory_tracking", "oom_detection"] },
    { type: "kubernetes", required: true, features: ["pod_metrics", "events"] }
  ],
  
  window: {
    duration: "5m",
    slide: "30s"
  },
  
  collect: [
    {
      name: "oom_events",
      filter: {
        source: "ebpf",
        type: "oom_kill"
      }
    },
    {
      name: "pod_restarts", 
      filter: {
        source: "kubernetes",
        type: "pod.restart",
        within: "1m"
      }
    }
  ],
  
  conditions: [
    {
      all: [
        { expr: "count(oom_events) >= 1" },
        { expr: "count(pod_restarts) >= 2" },
        { expr: "time_between(oom_events[0], pod_restarts[0]) < 30s" }
      ]
    }
  ],
  
  analyze: {
    confidence: (ctx) => {
      let base = 0.5;
      if (ctx.count("oom_events") > 1) base += 0.2;
      if (ctx.count("service_failures") > 0) base += 0.2;
      if (ctx.sameNode("oom_events", "pod_restarts")) base += 0.1;
      return Math.min(base, 1.0);
    },
    
    severity: (ctx) => {
      const restarts = ctx.count("pod_restarts");
      return restarts >= 5 ? "critical" : restarts >= 3 ? "high" : "medium";
    }
  },
  
  output: {
    title: "Memory pressure cascade detected",
    description: (ctx) => `Memory pressure on ${ctx.entity.node} caused ${ctx.count("pod_restarts")} pod restarts`,
    recommendations: [
      "Increase memory limits for affected pods",
      "Add more nodes to distribute memory load"
    ]
  }
};
```

### Advanced Rule Example

```yaml
apiVersion: tapio.io/v1alpha1
kind: CorrelationRule
metadata:
  name: etcd-failure-cascade
  description: Detects etcd failures causing control plane instability
  version: 2.0.0
spec:
  sources:
    - type: kubernetes
      required: true
    - type: journald
      required: true
    - type: ebpf
      required: false
      
  # Multi-stage correlation
  stages:
    - name: detect_etcd_issues
      window: 2m
      collect:
        - name: etcd_errors
          filter:
            source: journald
            message.contains: ["etcd", "error", "timeout"]
            severity: ["error", "critical"]
            
        - name: api_errors
          filter:
            source: kubernetes
            type: event
            reason.in: ["FailedConnect", "Timeout"]
            involvedObject.component: "kube-apiserver"
            
      trigger:
        count(etcd_errors) >= 3
        
    - name: assess_impact
      window: 5m
      dependsOn: detect_etcd_issues
      collect:
        - name: pod_failures
          filter:
            source: kubernetes
            type: pod.failed
            namespace.in: ["kube-system", "default"]
            
        - name: node_issues
          filter:
            source: kubernetes
            type: node.condition
            condition: "Ready"
            status: "False"
            
      analyze:
        impactScore: |
          score = 0
          score += count(pod_failures) * 0.1
          score += count(node_issues) * 0.3
          if "kube-scheduler" in affected_components():
            score += 0.3
          return min(score, 1.0)
          
  # Complex conditions with temporal logic
  conditions:
    sequence:
      - stage: detect_etcd_issues
        passed: true
      - stage: assess_impact
        passed: true
        impactScore: ">= 0.5"
        
  # Dynamic output based on analysis
  output:
    title: "Control plane instability due to etcd issues"
    severity: |
      if stages.assess_impact.impactScore >= 0.8:
        return "critical"
      else:
        return "high"
    details:
      etcd_error_count: "{{ count .stages.detect_etcd_issues.etcd_errors }}"
      affected_nodes: "{{ unique .stages.assess_impact.node_issues.node_name }}"
      impact_duration: "{{ duration .stages.detect_etcd_issues.start .stages.assess_impact.end }}"
```

## Rule Language Specification

### Filter Expressions

```yaml
# Basic filters
filter:
  source: kubernetes
  type: pod.oom
  severity: critical
  namespace: production

# Advanced filters with operators
filter:
  # Comparison operators
  memory.usage: ">= 80%"
  restart.count: "> 3"
  timestamp: "< now() - 5m"
  
  # String matching
  message.contains: ["error", "fail"]
  message.matches: "connection.*refused"
  
  # List operations
  namespace.in: ["kube-system", "monitoring"]
  type.not_in: ["info", "debug"]
  
  # Logical operators
  or:
    - severity: critical
    - and:
      - severity: error
      - source: systemd
```

### Temporal Operators

```yaml
# Time-based correlations
conditions:
  # Event ordering
  - before(etcd_errors, api_failures)
  - after(oom_event, pod_restart, "30s")
  
  # Time windows
  - within(all_events, "5m")
  - time_between(event_a, event_b) < "1m"
  
  # Patterns
  - pattern:
      sequence:
        - oom_event
        - pod_restart
        - service_failure
      window: "2m"
```

### Aggregation Functions

```yaml
analyze:
  metrics:
    error_rate: count(errors) / duration_minutes()
    avg_restart_time: avg(restart_durations)
    p95_latency: percentile(latencies, 95)
    
  # Grouping
  by_namespace: |
    grouped = group_by(events, "namespace")
    for ns, events in grouped:
      if count(events) > threshold:
        mark_critical(ns)
```

### Entity Relationships

```yaml
# Define entity relationships
relationships:
  - pod.node: direct
  - container.pod: parent
  - service.pod: selector
  - systemd_unit.pod: metadata.unit

# Use in conditions
conditions:
  - same_node(oom_event.entity, pod_restart.entity)
  - related_entities(service_failure, pod_crash)
  - parent_entity(container_event) == pod_event.entity
```

## Rule Compilation

### Compilation Process

```go
type RuleCompiler struct {
    parser    *Parser
    validator *Validator
    optimizer *Optimizer
}

func (rc *RuleCompiler) Compile(yaml string) (*CompiledRule, error) {
    // Parse YAML to AST
    ast, err := rc.parser.Parse(yaml)
    if err != nil {
        return nil, fmt.Errorf("parse error: %w", err)
    }
    
    // Validate rule
    if err := rc.validator.Validate(ast); err != nil {
        return nil, fmt.Errorf("validation error: %w", err)
    }
    
    // Optimize execution plan
    plan := rc.optimizer.Optimize(ast)
    
    return &CompiledRule{
        AST:           ast,
        ExecutionPlan: plan,
        Metadata:      ast.Metadata,
    }, nil
}
```

### Execution Plan

```go
type ExecutionPlan struct {
    Stages       []Stage
    Dependencies DependencyGraph
    Optimizations []Optimization
}

type Stage struct {
    Name         string
    Collectors   []Collector
    Filters      []Filter
    Window       TimeWindow
    Trigger      Condition
    Parallel     bool
}

type Optimization struct {
    Type        OptimizationType
    Description string
    Impact      float64 // Estimated performance improvement
}

const (
    OptimizationFilterPushdown OptimizationType = iota
    OptimizationIndexUsage
    OptimizationParallelization
    OptimizationCaching
)
```

### Rule Execution

```go
type RuleExecutor struct {
    compiled *CompiledRule
    timeline *Timeline
    cache    *Cache
}

func (re *RuleExecutor) Execute(ctx context.Context) (*CorrelationResult, error) {
    result := &CorrelationResult{
        Rule:      re.compiled.Metadata.Name,
        StartTime: time.Now(),
    }
    
    // Execute stages in dependency order
    stageResults := make(map[string]*StageResult)
    
    for _, stage := range re.compiled.ExecutionPlan.Stages {
        // Check dependencies
        if !re.dependenciesMet(stage, stageResults) {
            continue
        }
        
        // Execute stage
        stageResult, err := re.executeStage(ctx, stage)
        if err != nil {
            return nil, fmt.Errorf("stage %s failed: %w", stage.Name, err)
        }
        
        stageResults[stage.Name] = stageResult
        
        // Check if correlation triggered
        if stageResult.Triggered {
            result.Triggered = true
        }
    }
    
    // Run analysis
    if result.Triggered {
        result.Analysis = re.runAnalysis(stageResults)
        result.Output = re.generateOutput(stageResults, result.Analysis)
    }
    
    result.EndTime = time.Now()
    return result, nil
}
```

## Built-in Functions

### Data Access Functions

```yaml
# Event access
functions:
  - first(events)           # First event in collection
  - last(events)            # Last event in collection
  - at(events, index)       # Event at specific index
  - between(events, t1, t2) # Events between timestamps

# Field access
  - field(event, "path.to.field")
  - fields(events, "field_name")
  - unique(field_values)
```

### Statistical Functions

```yaml
functions:
  # Basic statistics
  - count(events)
  - sum(values)
  - avg(values)
  - min(values)
  - max(values)
  - stddev(values)
  
  # Percentiles
  - percentile(values, n)
  - median(values)
  
  # Time series
  - rate(events, window)
  - increase(values, window)
  - delta(value1, value2)
```

### Pattern Matching Functions

```yaml
functions:
  # Sequence detection
  - sequence(events, pattern)
  - followed_by(event1, event2, timeout)
  
  # Pattern matching
  - matches_pattern(events, "A->B->C")
  - contains_pattern(timeline, pattern)
  
  # Anomaly detection
  - is_anomaly(value, history)
  - deviation_score(values)
```

## Rule Management API

### Rule CRUD Operations

```go
type RuleManager struct {
    store     RuleStore
    compiler  *RuleCompiler
    validator *RuleValidator
}

// Create a new rule
func (rm *RuleManager) CreateRule(rule RuleSpec) (*Rule, error) {
    // Validate rule spec
    if err := rm.validator.ValidateSpec(rule); err != nil {
        return nil, err
    }
    
    // Compile rule
    compiled, err := rm.compiler.Compile(rule)
    if err != nil {
        return nil, err
    }
    
    // Store rule
    stored := &Rule{
        ID:       uuid.New().String(),
        Spec:     rule,
        Compiled: compiled,
        Created:  time.Now(),
        Status:   RuleStatusActive,
    }
    
    return stored, rm.store.Create(stored)
}

// Update existing rule
func (rm *RuleManager) UpdateRule(id string, rule RuleSpec) (*Rule, error) {
    existing, err := rm.store.Get(id)
    if err != nil {
        return nil, err
    }
    
    // Validate changes
    if err := rm.validator.ValidateUpdate(existing.Spec, rule); err != nil {
        return nil, err
    }
    
    // Recompile
    compiled, err := rm.compiler.Compile(rule)
    if err != nil {
        return nil, err
    }
    
    // Update
    existing.Spec = rule
    existing.Compiled = compiled
    existing.Updated = time.Now()
    existing.Version++
    
    return existing, rm.store.Update(existing)
}
```

### Rule Testing Framework

```go
type RuleTester struct {
    executor *RuleExecutor
    mock     *MockTimeline
}

func (rt *RuleTester) TestRule(rule *Rule, scenario TestScenario) (*TestResult, error) {
    // Setup mock timeline with test events
    rt.mock.Clear()
    for _, event := range scenario.Events {
        rt.mock.AddEvent(event)
    }
    
    // Execute rule
    result, err := rt.executor.Execute(context.Background())
    if err != nil {
        return nil, err
    }
    
    // Verify expectations
    testResult := &TestResult{
        Rule:     rule.ID,
        Scenario: scenario.Name,
        Passed:   true,
    }
    
    for _, expectation := range scenario.Expectations {
        if !rt.verifyExpectation(result, expectation) {
            testResult.Passed = false
            testResult.Failures = append(testResult.Failures, 
                fmt.Sprintf("Failed: %s", expectation.Description))
        }
    }
    
    return testResult, nil
}
```

### Test Scenario Example

```yaml
apiVersion: tapio.io/v1alpha1
kind: TestScenario
metadata:
  name: memory-cascade-test
  rule: memory-pressure-cascade
spec:
  events:
    - timestamp: "2024-01-15T10:00:00Z"
      source: ebpf
      type: oom_kill
      entity:
        type: container
        name: app-container
        pod: app-pod-1
        
    - timestamp: "2024-01-15T10:00:15Z" 
      source: kubernetes
      type: pod.restart
      entity:
        type: pod
        name: app-pod-1
        
    - timestamp: "2024-01-15T10:00:30Z"
      source: kubernetes
      type: pod.restart
      entity:
        type: pod
        name: app-pod-2
        
  expectations:
    - correlation_triggered: true
    - severity: high
    - confidence: ">= 0.7"
    - events_correlated: 3
    - contains_recommendation: "Increase memory limits"
```

## Performance Considerations

### Rule Optimization

1. **Filter Pushdown**: Apply filters as early as possible
2. **Index Usage**: Leverage timeline indexes for fast lookups
3. **Parallel Execution**: Run independent stages concurrently
4. **Result Caching**: Cache intermediate results
5. **Incremental Processing**: Process only new events when possible

### Execution Metrics

```go
type RuleMetrics struct {
    ExecutionCount    int64
    TriggerCount      int64
    AvgExecutionTime  time.Duration
    AvgEventsScanned  int64
    AvgEventsMatched  int64
    LastExecution     time.Time
    LastTrigger       time.Time
}

func (r *Rule) RecordExecution(result *CorrelationResult) {
    r.Metrics.ExecutionCount++
    
    if result.Triggered {
        r.Metrics.TriggerCount++
        r.Metrics.LastTrigger = result.EndTime
    }
    
    r.Metrics.LastExecution = result.EndTime
    
    // Update averages
    duration := result.EndTime.Sub(result.StartTime)
    r.Metrics.AvgExecutionTime = updateAverage(
        r.Metrics.AvgExecutionTime,
        duration,
        r.Metrics.ExecutionCount,
    )
}
```

## Rule Library

### Standard Rules

The rule engine includes a library of standard rules for common scenarios:

1. **Infrastructure Rules**
   - CPU throttling detection
   - Memory pressure cascade
   - Disk space exhaustion
   - Network partition detection

2. **Application Rules**
   - Crash loop detection
   - Deployment failure analysis
   - Service degradation patterns
   - Database connection pool exhaustion

3. **Security Rules**
   - Privilege escalation detection
   - Suspicious network activity
   - Authentication failure patterns
   - Container escape attempts

4. **Performance Rules**
   - Latency spike detection
   - Throughput degradation
   - Resource saturation
   - Queue backup detection