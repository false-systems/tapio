# Collector Implementation Blueprint

This document provides the standard blueprint for implementing minimal collectors in Tapio.

## Core Principles

1. **Raw Data Only** - Collectors emit raw bytes, no processing
2. **No Business Logic** - All intelligence lives in the pipeline
3. **Simple Interface** - Just 5 methods to implement
4. **K8s-Focused** - Designed for Kubernetes observability

## Directory Structure

```
pkg/collectors/<name>/
├── collector.go          # Main implementation
├── collector_test.go     # Unit tests
├── types.go             # Collector-specific types (optional)
└── README.md            # Documentation
```

## Implementation Template

### collector.go

```go
package <name>

import (
    "context"
    "sync"
    "time"
    
    "github.com/yairfalse/tapio/pkg/collectors"
)

// Collector implements minimal <name> collection
type Collector struct {
    config  collectors.CollectorConfig
    events  chan collectors.RawEvent
    
    // Add source-specific fields
    // e.g., client, watcher, reader
    
    ctx     context.Context
    cancel  context.CancelFunc
    wg      sync.WaitGroup
    
    mu      sync.RWMutex
    healthy bool
}

// NewCollector creates a new <name> collector
func NewCollector(config collectors.CollectorConfig) (*Collector, error) {
    return &Collector{
        config:  config,
        events:  make(chan collectors.RawEvent, config.BufferSize),
        healthy: true,
    }, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
    return "<name>"
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.ctx != nil {
        return errors.New("collector already started")
    }
    
    c.ctx, c.cancel = context.WithCancel(ctx)
    
    // Initialize source connection
    // Start collection goroutine
    c.wg.Add(1)
    go c.collect()
    
    return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.cancel != nil {
        c.cancel()
    }
    
    c.wg.Wait()
    close(c.events)
    
    return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
    return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.healthy
}

// collect is the main collection loop
func (c *Collector) collect() {
    defer c.wg.Done()
    
    for {
        select {
        case <-c.ctx.Done():
            return
            
        default:
            // Read from source
            data := c.readFromSource()
            
            // Create raw event
            event := collectors.RawEvent{
                Timestamp: time.Now(),
                Type:      "<name>",
                Data:      data,  // Raw bytes
                Metadata:  c.createMetadata(),
            }
            
            // Send event
            select {
            case c.events <- event:
                // Sent
            case <-c.ctx.Done():
                return
            default:
                // Buffer full, drop
            }
        }
    }
}
```

## Specific Implementations

### 1. Systemd Collector

**Purpose**: Collect logs from K8s-related systemd services

**What to collect**:
- kubelet logs
- containerd/docker logs  
- crio logs (if used)

**Implementation approach**:
```go
// Use systemd journal API
import "github.com/coreos/go-systemd/v22/sdjournal"

// Watch specific units
units := []string{"kubelet.service", "containerd.service"}

// Emit raw journal entries as JSON
data, _ := json.Marshal(entry.Fields)
```

**Metadata**:
- unit: Service name
- hostname: Node name
- priority: Log level

### 2. CNI Collector

**Purpose**: Collect Container Network Interface events

**What to collect**:
- CNI ADD/DEL operations
- Network namespace events
- CNI plugin logs

**Implementation approach**:
```go
// Option 1: Watch CNI log files
logPath := "/var/log/cni/"
watcher := fsnotify.NewWatcher()

// Option 2: Watch network namespaces
netnsPath := "/var/run/netns/"

// Emit raw log lines or namespace events
data := []byte(logLine)
```

**Metadata**:
- operation: ADD/DEL
- plugin: CNI plugin name
- pod: Pod name (if extractable)

### 3. K8s Collector (Already shown)

**Purpose**: Watch Kubernetes API events

**What to collect**:
- Core resources: pods, services, nodes, events
- Apps resources: deployments, replicasets
- Optional: configmaps, secrets (metadata only)

**Implementation approach**:
```go
// Use dynamic client for flexibility
client := dynamic.NewForConfig(config)

// Use shared informer factory
factory := dynamicinformer.NewDynamicSharedInformerFactory(client, time.Minute)

// Emit raw K8s objects as JSON
data, _ := json.Marshal(obj.Object)
```

**Metadata**:
- type: ADDED/MODIFIED/DELETED
- resource: pods/services/etc
- namespace: Object namespace
- name: Object name

### 4. etcd Collector

**Purpose**: Watch etcd for K8s state changes at the storage layer

**What to collect**:
- Key changes under /registry/
- Lease events
- Compaction events
- Watch events

**Implementation approach**:
```go
// Use etcd client v3
import "go.etcd.io/etcd/client/v3"

// Watch K8s registry prefix
watchChan := client.Watch(ctx, "/registry/", clientv3.WithPrefix())

// Emit raw etcd events
data, _ := json.Marshal(map[string]interface{}{
    "type": watchResp.Events[0].Type.String(),
    "key": string(watchResp.Events[0].Kv.Key),
    "value": string(watchResp.Events[0].Kv.Value),
    "version": watchResp.Events[0].Kv.Version,
})
```

**Metadata**:
- operation: PUT/DELETE
- key: Full key path
- resource_type: Extracted from key (e.g., /registry/pods/...)
- revision: etcd revision number

**Why etcd?**:
- See K8s changes before they hit API
- Detect split-brain scenarios
- Track actual storage-level changes
- Debug K8s control plane issues

## Testing Template

```go
func TestCollector(t *testing.T) {
    config := collectors.DefaultCollectorConfig()
    collector, err := NewCollector(config)
    require.NoError(t, err)
    
    ctx := context.Background()
    err = collector.Start(ctx)
    require.NoError(t, err)
    defer collector.Stop()
    
    // Trigger some events
    
    // Verify events received
    select {
    case event := <-collector.Events():
        assert.Equal(t, "<name>", event.Type)
        assert.NotEmpty(t, event.Data)
    case <-time.After(time.Second):
        t.Fatal("timeout waiting for event")
    }
}
```

## Integration Example

```go
// main.go or manager
registry := collectors.NewRegistry()

// Register all collectors
registry.Register("kernel", kernel.NewUnifiedCollector(config))
registry.Register("k8s", k8s.NewCollector(config))
registry.Register("systemd", systemd.NewCollector(config))
registry.Register("cni", cni.NewCollector(config))
registry.Register("etcd", etcd.NewCollector(config))

// Start all
registry.Start(ctx)

// Single event stream
for event := range registry.Events() {
    pipeline.Process(event)
}
```

## Guidelines

1. **Keep it simple** - If you're adding complex logic, stop
2. **Raw data only** - Don't parse, filter, or enrich
3. **Fail gracefully** - Don't crash on errors
4. **Buffer wisely** - Drop events if buffer full
5. **Test basics** - Start, stop, event emission