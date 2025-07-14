# Tapio Correlation Engine Integration Demo

This document demonstrates how the `tapio check`, `tapio why`, and `tapio fix` commands work with the new correlation engine and collectors.

## Architecture Overview

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   eBPF/K8s/     │     │   Correlation    │     │   CLI Commands  │
│   Collectors    │────▶│     Server       │◀────│  check/why/fix  │
└─────────────────┘     └──────────────────┘     └─────────────────┘
         │                       │                         │
         │                       │                         │
    Streaming              Store Insights            Query Insights
     Events                & Predictions              & Get Fixes
```

## Data Flow Example: OOM Prediction

### 1. **Event Collection**
```go
// eBPF collector detects memory allocation patterns
event := &MemoryEvent{
    PID:        12345,
    Container:  "api-service",
    Allocation: 50 * 1024 * 1024, // 50MB
    Timestamp:  time.Now(),
}
```

### 2. **Correlation Analysis**
```go
// Correlation engine tracks memory growth
state := &ResourceState{
    MemoryUsage:    800 * 1024 * 1024,  // 800MB current
    MemoryLimit:    1024 * 1024 * 1024, // 1GB limit
    MemoryGrowthRate: 2.4 * 1024 * 1024, // 2.4MB/sec
}

// Calculate time to OOM
timeToOOM := (state.MemoryLimit - state.MemoryUsage) / state.MemoryGrowthRate
// = (1GB - 800MB) / 2.4MB/s = ~87 seconds = ~1.5 minutes

// Generate insight with prediction
insight := &Insight{
    Title: "OOM Kill Predicted in 1.5 minutes",
    Severity: "critical",
    Prediction: &Prediction{
        Type: "oom",
        TimeToEvent: 90 * time.Second,
        Probability: 0.92,
        Confidence: 0.85,
    },
    ActionableItems: []ActionableItem{{
        Description: "Increase memory limit to prevent OOM",
        Command: "kubectl patch deployment api-service ...",
    }},
}
```

### 3. **CLI Command Output**

#### `tapio check api-service`
```bash
$ tapio check api-service

HEALTH STATUS: api-service
  Status: UNHEALTHY
  Memory: 800MB/1GB (78.1%)
  CPU: 45%
  Restarts: 3

🔮 PREDICTIONS:
   → Pod will OOM in 1.5 minutes

💡 INSIGHTS:
   [critical] OOM Kill Predicted in 1.5 minutes
     Memory usage growing at 2.4MB/s, will exceed limit soon
     Suggested fixes:
       [1] Increase memory limit to prevent OOM
           $ kubectl patch deployment api-service -p '{"spec":{"template":{"spec":{"containers":[{"name":"api","resources":{"limits":{"memory":"2Gi"}}}]}}}}'

✅ Using advanced correlation analysis
```

#### `tapio why api-service`
```bash
$ tapio why api-service

ANALYSIS: Memory leak detected in api-service

ROOT CAUSE:
  → Memory leak in /api/users endpoint
    Evidence: 
    - Memory grows 2.4MB/s during user API calls
    - No corresponding memory releases detected
    - Garbage collection not freeing allocated memory

CORRELATION CHAIN:
  1. HTTP requests to /api/users (detected by eBPF)
  2. Memory allocations without deallocation
  3. Growing heap usage (800MB and climbing)
  4. OOM kill predicted in 1.5 minutes

PATTERN MATCH: memory_leak_api_endpoint (confidence: 92%)

RECOMMENDED ACTION:
  Immediate: Increase memory limit to buy time
  Long-term: Fix memory leak in user API handler
```

#### `tapio fix api-service`
```bash
$ tapio fix api-service

🔍 Analyzing issues for api-service...

Found 2 fixable issue(s):

[1] Increase memory limit to prevent imminent OOM
    Impact: Prevents pod restart and service disruption
    Risk: low
    Command: kubectl patch deployment api-service -p '{"spec":{"template":{"spec":{"containers":[{"name":"api","resources":{"limits":{"memory":"2Gi"}}}]}}}}'

[2] Add memory leak detection instrumentation
    Impact: Helps identify root cause of memory leak
    Risk: low  
    Command: kubectl set env deployment/api-service GODEBUG=gctrace=1

Apply these fixes? [y/N]: y

[1/2] Applying: Increase memory limit to prevent imminent OOM
   ✅ Applied successfully

[2/2] Applying: Add memory leak detection instrumentation  
   ✅ Applied successfully

📊 Summary: 2/2 fixes applied successfully
```

## Key Integration Points

### 1. **Collectors → Correlation Server**
- eBPF events stream via gRPC
- Kubernetes events via informers
- SystemD logs via journal API
- All unified in OpinionatedEvent format

### 2. **Correlation Server Processing**
- Pattern detection (6 ML-based patterns)
- Semantic correlation (event relationships)
- Temporal analysis (time-based patterns)
- Causality chains (root cause analysis)
- Predictive analytics (time to failure)

### 3. **Correlation Server → CLI**
- Query API for insights/predictions
- Fallback to local analysis if unavailable
- Real-time or cached results
- Actionable items with kubectl commands

### 4. **Enhanced User Experience**
- **Predictive**: "Pod will OOM in 7 minutes"
- **Explanatory**: "Memory leak in /api/users endpoint"
- **Actionable**: "Applied memory limit increase"
- **Intelligent**: Learns from outcomes

## Configuration

### Enable Correlation Server
```yaml
# tapio-config.yaml
correlation:
  enabled: true
  server: localhost:9090
  
collectors:
  ebpf:
    enabled: true
    memory_tracking: true
  kubernetes:
    enabled: true
  systemd:
    enabled: true
```

### Start Services
```bash
# Start correlation server
tapio-server --config tapio-config.yaml

# Collectors start automatically with server
# CLI commands auto-detect server availability
```

## Benefits

1. **Predictive Insights**: Know about problems before they happen
2. **Root Cause Analysis**: Understand why issues occur
3. **Automated Remediation**: Fix problems with one command
4. **Intelligent Correlation**: Connect dots across multiple data sources
5. **Zero Configuration**: Works out of the box with smart defaults

The integration makes Kubernetes debugging accessible to everyone, from junior developers to experts, by providing clear, actionable insights based on advanced correlation analysis.