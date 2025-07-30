# CNI Collector

Minimal Container Network Interface (CNI) event collector for Tapio.

## Features

- **Auto-detection**: Automatically detects CNI plugin from K8s DaemonSets
- **Multi-CNI support**: Optimized collection for Calico, Cilium, and Flannel
- **Raw data only**: Emits raw events without processing (following Tapio philosophy)
- **Simple interface**: Just 5 methods to implement

## Supported CNI Plugins

1. **Calico** - Detected from calico-node DaemonSet
   - Monitors: `/var/log/calico/`, Felix logs
   - Watches: CNI config, Calico state directory

2. **Cilium** - Detected from cilium-agent DaemonSet  
   - Monitors: Cilium logs, CNI logs
   - Watches: CNI config, Cilium runtime directory

3. **Flannel** - Detected from kube-flannel DaemonSet
   - Monitors: Flanneld logs
   - Watches: CNI config, Flannel state directory

4. **Generic** - Fallback for unknown CNIs
   - Monitors: Standard CNI paths
   - Watches: Common CNI directories

## Usage

```go
config := collectors.DefaultCollectorConfig()
collector, err := cni.NewCollector(config)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
if err := collector.Start(ctx); err != nil {
    log.Fatal(err)
}
defer collector.Stop()

// Process raw events
for event := range collector.Events() {
    // Raw CNI data in event.Data
    // CNI type in event.Metadata["cni_plugin"]
    pipeline.Process(event)
}
```

## Architecture

The collector follows Tapio's minimal collector pattern:
- No business logic
- No data transformation
- Just raw event emission
- All intelligence in the pipeline