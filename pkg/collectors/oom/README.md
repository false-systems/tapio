# OOM Killer Collector - The ULTIMATE Root Cause Detective 🔍

The OOM (Out-of-Memory) Killer Collector is Tapio's **most valuable collector** for Kubernetes root cause analysis. Every OOM event is a **smoking gun** - a direct cause of container deaths and service outages. This collector doesn't just detect OOM kills; it **predicts them before they happen** and provides the complete causality chain.

## Why This Matters 💰

- **Every OOM kill costs money**: Downtime, lost transactions, customer frustration
- **Every prediction saves time**: Fix memory leaks before they kill your services  
- **Every root cause found prevents repeats**: Stop playing whack-a-mole with container deaths

## What You Get 🎯

### Critical OOM Events (The Smoking Guns)
- **OOM Kill Detection**: Exact moment when containers die
- **Memory Forensics**: Usage, limits, peak consumption at time of death
- **Process Causality**: Which process triggered the OOM, who killed whom
- **Kubernetes Context**: Pod, namespace, container, node correlation

### Predictive Analytics (The Early Warning System)
- **Memory Pressure Detection**: Warning when containers approach limits
- **Allocation Rate Tracking**: Memory growth velocity (MB/s)
- **Time-to-OOM Predictions**: "Your payment-service will OOM in 5 minutes"
- **Confidence Scoring**: How certain we are about predictions

### Business Intelligence (The Impact Analysis)
- **Container-to-Pod Mapping**: Which workload is affected
- **Service Impact Assessment**: Critical vs non-critical failures
- **Resource Optimization Hints**: Right-sizing recommendations
- **Pattern Recognition**: Recurring memory leak detection

## Architecture 🏗️

### eBPF Program (`oom_monitor.c`)
- **Zero-overhead monitoring**: Hooks into kernel OOM killer
- **Rich event capture**: 40+ fields per OOM event
- **Memory pressure detection**: Predictive early warnings
- **Ring buffer reliability**: Never lose a critical OOM event

### Go Collector (`collector_linux.go`)
- **Type-safe processing**: No `map[string]interface{}` abuse
- **OpenTelemetry instrumentation**: Full observability
- **Memory prediction engine**: AI-powered early warnings
- **Kubernetes context enrichment**: Full workload correlation

### Event Flow
```
Kernel OOM → eBPF Hook → Ring Buffer → Go Processor → CollectorEvent → ObservationEvent → Correlation Engine
```

## Configuration ⚙️

### Basic Configuration
```go
config := &oom.Config{
    OOMConfig: &oom.OOMConfig{
        EnablePrediction:         true,
        PredictionThresholdPct:   95,  // Alert at 95% memory usage
        HighPressureThresholdPct: 80,  // Warn at 80% memory usage
        RingBufferSize:          1048576, // 1MB buffer
        CollectCmdline:          true,
        EnableK8sCorrelation:    true,
    },
}
```

### Advanced Configuration
```go
config := &oom.Config{
    OOMConfig: &oom.OOMConfig{
        // Prediction settings
        EnablePrediction:         true,
        PredictionThresholdPct:   90,   // More aggressive prediction
        HighPressureThresholdPct: 75,   // Earlier warnings
        
        // Performance settings  
        RingBufferSize:     2097152, // 2MB for high-load environments
        EventBatchSize:     200,     // Larger batches
        MaxEventsPerSecond: 2000,    // Higher throughput
        
        // Data collection
        CollectCmdline:       true,  // Full command lines
        CollectEnvironment:   true,  // Environment variables (expensive)
        CollectMemoryDetails: true,  // Detailed memory stats
        
        // Filtering
        ExcludeSystemProcesses: true,           // Focus on application OOMs
        IncludeNamespaces:     []string{"production", "staging"},
        ExcludeNamespaces:     []string{"kube-system"},
        
        // Correlation
        EnableK8sCorrelation: true,
        K8sContextTimeout:   time.Second * 10, // Longer timeout for accuracy
    },
}
```

## Deployment 🚀

### Requirements
- **Linux kernel**: 4.18+ (for eBPF tracepoint support)
- **Privileged access**: Required for eBPF program loading
- **Memory**: 50MB base + ring buffer size
- **CPU**: < 1% under normal load

### Kubernetes Deployment
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-oom-collector
  namespace: tapio-system
spec:
  selector:
    matchLabels:
      app: tapio-oom-collector
  template:
    metadata:
      labels:
        app: tapio-oom-collector
    spec:
      hostPID: true
      serviceAccountName: tapio-oom-collector
      containers:
      - name: oom-collector
        image: tapio/oom-collector:latest
        securityContext:
          privileged: true  # Required for eBPF
        resources:
          requests:
            memory: "64Mi"
            cpu: "50m"
          limits:
            memory: "256Mi" 
            cpu: "200m"
        volumeMounts:
        - name: sys
          mountPath: /sys
          readOnly: true
        - name: debugfs
          mountPath: /sys/kernel/debug
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
      volumes:
      - name: sys
        hostPath:
          path: /sys
      - name: debugfs
        hostPath:
          path: /sys/kernel/debug
      tolerations:
      - operator: Exists  # Run on all nodes
```

### Docker Run
```bash
docker run -d \
  --name tapio-oom-collector \
  --privileged \
  --pid=host \
  -v /sys:/sys:ro \
  -v /sys/kernel/debug:/sys/kernel/debug \
  tapio/oom-collector:latest
```

## Usage Examples 📚

### Basic Usage
```go
package main

import (
    "context"
    "log"
    
    "github.com/yairfalse/tapio/pkg/collectors/oom"
    "go.uber.org/zap"
)

func main() {
    logger, _ := zap.NewDevelopment()
    config := oom.NewConfig()
    
    collector, err := oom.CreateCollector(config, logger)
    if err != nil {
        log.Fatal(err)
    }
    
    ctx := context.Background()
    if err := collector.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Process events
    for event := range collector.Events() {
        if oom.IsCriticalOOMEvent(event) {
            log.Printf("CRITICAL OOM: %s killed in %s/%s", 
                event.Metadata.Command,
                event.Metadata.PodNamespace, 
                event.Metadata.PodName)
        }
        
        if oom.IsPredictiveOOMEvent(event) {
            log.Printf("PREDICTION: %s/%s approaching memory limit",
                event.Metadata.PodNamespace,
                event.Metadata.PodName)
        }
    }
}
```

### Event Analysis
```go
// Extract OOM-specific context
context := oom.ExtractOOMContext(event)
eventType := context["event_type"]
pressureLevel := context["pressure_level"]

// Get memory statistics
if containerData, ok := event.EventData.Container; ok {
    log.Printf("Container %s was killed", containerData.ContainerID)
}

if processData, ok := event.EventData.Process; ok {
    log.Printf("Process %s (PID %d) was the victim", 
        processData.Command, processData.PID)
}
```

### Monitoring Integration
```go
// Prometheus metrics
var (
    oomKillsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tapio_oom_kills_total",
            Help: "Total OOM kills detected",
        },
        []string{"namespace", "pod", "container"},
    )
    
    memoryPredictions = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tapio_oom_predictions_total", 
            Help: "Total OOM predictions made",
        },
        []string{"namespace", "pod", "prediction_type"},
    )
)

// Process events
for event := range collector.Events() {
    if oom.IsCriticalOOMEvent(event) {
        oomKillsTotal.WithLabelValues(
            event.Metadata.PodNamespace,
            event.Metadata.PodName,
            event.Metadata.ContainerName,
        ).Inc()
    }
    
    if oom.IsPredictiveOOMEvent(event) {
        context := oom.ExtractOOMContext(event)
        memoryPredictions.WithLabelValues(
            event.Metadata.PodNamespace,
            event.Metadata.PodName, 
            context["event_type"],
        ).Inc()
    }
}
```

## Event Types 📊

### Critical Events (Immediate Action Required)
- **`oom_kill_victim`**: Process was killed by OOM killer
- **`oom_kill_triggered`**: OOM killer was activated  
- **`memory_pressure_critical`**: Memory usage > 95%, OOM imminent

### Predictive Events (Early Warning)
- **`memory_pressure_high`**: Memory usage > 80%, monitor closely
- **`container_memory_limit`**: Container approaching memory limit
- **`cgroup_oom_notification`**: Cgroup memory controller notification

## Metrics & Observability 📈

### Collector Metrics
- **`oom_events_total`**: Total OOM events processed
- **`predictions_total`**: Total OOM predictions made  
- **`errors_total`**: Collection errors by type
- **`processing_duration_ms`**: Event processing latency
- **`memory_pressure_ratio`**: Current memory pressure by container

### Event Attributes
Every OOM event includes:
- **Memory Statistics**: Usage, limit, peak consumption
- **Process Context**: PID, command, parent process
- **Kubernetes Context**: Pod, namespace, container, node
- **Performance Data**: Pages scanned, reclaim efficiency
- **Prediction Data**: Time to OOM, confidence score

## Troubleshooting 🔧

### Common Issues

#### eBPF Program Load Failure
```
Error: failed to load eBPF program: operation not permitted
```
**Solution**: Run with `--privileged` or `CAP_SYS_ADMIN` capability

#### Missing Tracepoints  
```
Error: failed to attach tracepoint oom:oom_kill_process
```
**Solution**: Kernel too old (< 4.18) or tracepoint not available

#### High Memory Usage
```
Warning: OOM collector using 500MB memory
```
**Solution**: Reduce `RingBufferSize` or increase batch processing

#### No Events Detected
```
Info: OOM collector healthy but no events received
```
**Solution**: This is normal! No OOM = healthy system

### Debugging

#### Enable Debug Logging
```go
config.DebugMode = true
```

#### Check eBPF Program Status
```bash
# List loaded programs
bpftool prog list | grep oom

# Check map contents
bpftool map dump id <map_id>
```

#### Verify Kernel Support
```bash
# Check kernel version
uname -r

# Check tracepoint availability
ls /sys/kernel/debug/tracing/events/oom/
ls /sys/kernel/debug/tracing/events/kmem/
```

### Performance Tuning

#### High-Load Environments
```go
config := &oom.OOMConfig{
    RingBufferSize:     4194304, // 4MB buffer
    EventBatchSize:     500,     // Larger batches
    MaxEventsPerSecond: 5000,    // Higher throughput
}
```

#### Memory-Constrained Environments
```go
config := &oom.OOMConfig{
    RingBufferSize:      262144,  // 256KB buffer
    CollectEnvironment:  false,   // Disable expensive collection
    CollectMemoryDetails: false, // Minimal data collection
}
```

## Best Practices 🏆

### Configuration
- **Start conservative**: Use default thresholds initially
- **Monitor performance**: Watch collector resource usage
- **Tune for environment**: Adjust based on OOM frequency

### Alerting
- **Critical OOMs**: Immediate alert (PagerDuty/Slack)
- **High predictions**: Warning alert (>90% confidence)
- **Pattern detection**: Daily summary of recurring OOMs

### Response Procedures
1. **Immediate**: Check if service is still healthy
2. **Short-term**: Scale up or restart affected pods
3. **Long-term**: Analyze memory usage patterns and fix leaks

### Memory Optimization Workflow
1. **Collect baseline**: Run collector for 24-48 hours
2. **Identify patterns**: Look for recurring OOM victims
3. **Analyze predictions**: Focus on high-confidence alerts
4. **Right-size resources**: Adjust memory limits based on actual usage
5. **Fix leaks**: Address growing memory consumption patterns

## Integration with Tapio Intelligence 🧠

The OOM collector feeds into Tapio's correlation engine for advanced analytics:

### Correlation Patterns
- **Service Degradation**: OOM → Pod Restart → Service Latency
- **Cascade Failures**: Memory Pressure → Multiple OOMs → Node Pressure
- **Resource Contention**: High Allocation Rate → Memory Competition → OOM

### Intelligence Features
- **Anomaly Detection**: Unusual memory growth patterns
- **Predictive Scaling**: Automatic resource adjustments
- **Root Cause Analysis**: Complete causality chains
- **Cost Optimization**: Right-sizing recommendations

## Security Considerations 🔒

### Permissions Required
- **CAP_SYS_ADMIN**: For eBPF program loading
- **Host PID**: For process information access
- **Host filesystem**: For cgroup path resolution

### Data Privacy
- **Process commands**: May contain sensitive arguments
- **Environment variables**: Disabled by default (can contain secrets)
- **Memory contents**: Never collected, only metadata

### Network Security
- **No network access**: Collector operates entirely locally
- **Local-only data**: Events sent to local Tapio agents only

## Support & Feedback 💬

This collector represents the pinnacle of OOM detection and prediction. It's designed to be the **definitive solution** for container memory issues in Kubernetes.

**Feedback channels:**
- GitHub Issues: Bug reports and feature requests
- Slack: Real-time support and discussions  
- Email: Enterprise support and consulting

**Contributing:**
- eBPF expertise welcome for kernel-level improvements
- ML/AI contributions for better prediction algorithms
- Integration testing across different Kubernetes distributions

---

**Remember**: Every OOM event is preventable. This collector gives you the tools to predict, prevent, and properly respond to memory exhaustion before it impacts your users.

*Stop playing defense with container deaths. Start playing offense with predictive intelligence.* 🚀