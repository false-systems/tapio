# Tapio Collector Refactoring Plan

## Executive Summary

This document outlines the plan to refactor Tapio's collector architecture from a complex, monolithic design to a clean, K8s-focused architecture with clear separation of concerns.

### Current Problems
- **eBPF collector is 5.8MB** with 8 separate BPF programs
- **Business logic mixed with collection** (enrichment, processing, formatting in collectors)
- **Multiple separate processes** with duplicated functionality
- **No clear separation** between data collection and processing

### Target State
- **Single unified collector binary** (<1MB)
- **Collectors only collect** - no business logic
- **K8s-first design** - everything optimized for Kubernetes context
- **Clean pipeline architecture** - all processing in separate service

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     Current Architecture                      │
├───────────────┬───────────────┬───────────────┬─────────────┤
│ eBPF (5.8MB)  │ K8s (184KB)   │ CNI (180KB)   │ Systemd     │
│ • 8 programs  │ • Processing  │ • Monitoring  │ • DBus      │
│ • Enrichment  │ • Formatting  │ • Analysis    │ • Logic     │
│ • Business    │ • Business    │ • Business    │ • Business  │
└───────────────┴───────────────┴───────────────┴─────────────┘

                              ↓

┌─────────────────────────────────────────────────────────────┐
│                     Target Architecture                       │
├─────────────────────────┬───────────────────────────────────┤
│   Unified Collector     │        K8s Pipeline              │
│   (Single DaemonSet)    │     (Scalable Deployment)        │
├─────────────────────────┼───────────────────────────────────┤
│ • eBPF (minimal)        │ • Decode raw events              │
│ • K8s API watcher       │ • Enrich with K8s context        │
│ • CNI log reader        │ • Convert to UnifiedEvent        │
│ • Systemd monitor       │ • Correlate and analyze          │
├─────────────────────────┼───────────────────────────────────┤
│ Just read & forward     │ All intelligence here            │
└─────────────────────────┴───────────────────────────────────┘
```

## Phase 1: Simplify Collectors (Week 1)

### 1.1 Strip Business Logic
Remove all processing, enrichment, and formatting from collectors:

```bash
# Files to delete
rm pkg/collectors/ebpf/processor.go
rm pkg/collectors/ebpf/enricher.go
rm pkg/collectors/ebpf/tapio_client.go
rm pkg/collectors/ebpf/raw_event_formatter.go
rm pkg/collectors/*/internal/processor.go
rm pkg/collectors/*/internal/enricher.go
```

### 1.2 Define Pluggable Collector Interface
```go
// pkg/collectors/interface.go
type Collector interface {
    Name() string
    Run(ctx context.Context, output chan<- []byte) error
    Stop() error
    Health() CollectorHealth
}

// CollectorRegistry for dynamic collector registration
type CollectorRegistry struct {
    collectors map[string]CollectorFactory
    mu         sync.RWMutex
}

type CollectorFactory func(config map[string]interface{}) (Collector, error)

// Register a new collector type
func (r *CollectorRegistry) Register(name string, factory CollectorFactory) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.collectors[name] = factory
}

// Create collector by name
func (r *CollectorRegistry) Create(name string, config map[string]interface{}) (Collector, error) {
    r.mu.RLock()
    factory, exists := r.collectors[name]
    r.mu.RUnlock()
    
    if !exists {
        return nil, fmt.Errorf("unknown collector: %s", name)
    }
    
    return factory(config)
}

// RawEvent with Enhanced OTEL support
type RawEvent struct {
    Source    string    // "ebpf", "k8s", "cni", "systemd", "etcd", etc.
    Timestamp int64     // Unix nano
    Data      []byte    // Raw event data
    
    // Enhanced OTEL context for correlation
    TraceID   string    // OTEL trace ID
    SpanID    string    // OTEL span ID
    
    // K8s context (always populated in K8s environment)
    NodeName  string    // K8s node name
    PodName   string    // If available at collection time
    Namespace string    // If available at collection time
}
```

### 1.3 Implement Minimal Collectors with OTEL

**eBPF Collector with CO-RE and OTEL** (~150 lines):
```go
// pkg/collectors/ebpf/collector.go
package ebpf

import (
    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/btf"
    "github.com/cilium/ebpf/link"
    "github.com/cilium/ebpf/ringbuf"
    "github.com/cilium/ebpf/rlimit"
)

type EBPFCollector struct {
    // CO-RE BPF objects
    bpfObjs     *bpfObjects
    ringBuffer  *ringbuf.Reader
    links       []link.Link
    
    // OTEL
    tracer      trace.Tracer
    eventsTotal metric.Int64Counter
}

// Embed compiled BPF program (CO-RE)
//go:embed bpf/unified.bpf.o
var bpfProgram []byte

type bpfObjects struct {
    Programs *bpfPrograms `ebpf:"programs"`
    Maps     *bpfMaps     `ebpf:"maps"`
}

type bpfPrograms struct {
    TraceExec      *ebpf.Program `ebpf:"trace_exec"`
    TraceTcpSendmsg *ebpf.Program `ebpf:"trace_tcp_sendmsg"`
}

type bpfMaps struct {
    Events *ebpf.Map `ebpf:"events"`
}

func NewEBPFCollector(config map[string]interface{}) (collectors.Collector, error) {
    // Remove memory limit for BPF
    if err := rlimit.RemoveMemlock(); err != nil {
        return nil, fmt.Errorf("remove memlock: %w", err)
    }
    
    // Load CO-RE BPF program
    spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(bpfProgram))
    if err != nil {
        return nil, fmt.Errorf("load collection spec: %w", err)
    }
    
    // CO-RE: Rewrite constants based on kernel
    if err := spec.RewriteConstants(map[string]interface{}{
        "RINGBUF_SIZE": config["ring_buffer_size"],
    }); err != nil {
        return nil, fmt.Errorf("rewrite constants: %w", err)
    }
    
    // Load BPF objects
    objs := &bpfObjects{}
    opts := &ebpf.CollectionOptions{
        Programs: ebpf.ProgramOptions{
            // CO-RE: Use BTF for relocation
            KernelTypes: btf.LoadKernelSpec(),
        },
    }
    
    if err := spec.LoadAndAssign(objs, opts); err != nil {
        return nil, fmt.Errorf("load BPF objects: %w", err)
    }
    
    // Create ring buffer reader
    rd, err := ringbuf.NewReader(objs.Maps.Events)
    if err != nil {
        return nil, fmt.Errorf("create ring buffer: %w", err)
    }
    
    // Initialize OTEL
    meter := otel.Meter("tapio.collector.ebpf")
    eventsTotal, _ := meter.Int64Counter("ebpf.events.total")
    
    c := &EBPFCollector{
        bpfObjs:     objs,
        ringBuffer:  rd,
        tracer:      otel.Tracer("ebpf-collector"),
        eventsTotal: eventsTotal,
    }
    
    // Attach programs to kernel
    if err := c.attachPrograms(); err != nil {
        return nil, fmt.Errorf("attach programs: %w", err)
    }
    
    return c, nil
}

func (c *EBPFCollector) attachPrograms() error {
    // Attach to BTF-enabled tracepoint (CO-RE)
    l, err := link.AttachTracing(link.TracingOptions{
        Program: c.bpfObjs.Programs.TraceExec,
    })
    if err != nil {
        return fmt.Errorf("attach trace_exec: %w", err)
    }
    c.links = append(c.links, l)
    
    // Attach to fentry (CO-RE)
    l, err = link.AttachTracing(link.TracingOptions{
        Program: c.bpfObjs.Programs.TraceTcpSendmsg,
    })
    if err != nil {
        return fmt.Errorf("attach trace_tcp_sendmsg: %w", err)
    }
    c.links = append(c.links, l)
    
    return nil
}

func (c *EBPFCollector) Run(ctx context.Context, output chan<- []byte) error {
    for {
        record, err := c.ringBuffer.Read()
        if err != nil {
            if errors.Is(err, ringbuf.ErrClosed) {
                return nil
            }
            continue
        }
        
        // Parse event (CO-RE ensures correct layout)
        var event bpfEvent
        if err := binary.Read(bytes.NewReader(record.RawSample), 
            binary.NativeEndian, &event); err != nil {
            continue
        }
        
        // Create raw event with OTEL context
        raw := RawEvent{
            Source:    "ebpf",
            Timestamp: int64(event.Timestamp),
            Data:      record.RawSample,
            TraceID:   trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
        }
        
        output <- raw.Serialize()
        c.eventsTotal.Add(ctx, 1)
    }
}

func (c *EBPFCollector) Stop() error {
    for _, l := range c.links {
        l.Close()
    }
    return c.ringBuffer.Close()
}
```

## Phase 2: Consolidate eBPF Programs (Week 1-2)

### 2.1 Delete Redundant BPF Programs
```bash
# Current: 8 separate programs
rm pkg/collectors/ebpf/bpf/http_tracer.c      # Move to userspace
rm pkg/collectors/ebpf/bpf/grpc_tracer.c      # Move to userspace
rm pkg/collectors/ebpf/bpf/dns_monitor.c      # Consolidate
rm pkg/collectors/ebpf/bpf/packet_analyzer.c  # Consolidate
rm pkg/collectors/ebpf/bpf/protocol_analyzer.c # Move to userspace
```

### 2.2 Create Single Unified BPF Program with CO-RE
```c
// pkg/collectors/ebpf/bpf/unified.c
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Event structure with explicit sizing
struct event {
    u64 timestamp;
    u32 pid;
    u32 tid;
    u8  type;  // NETWORK, SYSCALL, MEMORY
    u8  data[64]; // Minimal data capture
} __attribute__((packed));

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 8 * 1024 * 1024); // 8MB
} events SEC(".maps");

// Use CO-RE for kernel compatibility
SEC("tp_btf/sched_process_exec")
int trace_exec(u64 *ctx) {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task_btf();
    
    // CO-RE: safely read fields that may not exist in older kernels
    u32 pid = BPF_CORE_READ(task, tgid);
    
    // Check if in container using CO-RE
    u32 ns_pid = 0;
    if (bpf_core_field_exists(task->nsproxy)) {
        struct pid_namespace *pidns = BPF_CORE_READ(task, nsproxy, pid_ns_for_children);
        if (pidns) {
            ns_pid = BPF_CORE_READ(pidns, level);
        }
    }
    
    if (ns_pid == 0) return 0; // Not in container
    
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = pid;
    e->tid = BPF_CORE_READ(task, pid);
    e->type = SYSCALL;
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Network tracing with CO-RE
SEC("fentry/tcp_sendmsg")
int BPF_PROG(trace_tcp_sendmsg, struct sock *sk, struct msghdr *msg, size_t size) {
    // CO-RE: handle different kernel versions
    u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    if (family != AF_INET && family != AF_INET6) {
        return 0;
    }
    
    struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e) return 0;
    
    e->timestamp = bpf_ktime_get_ns();
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->type = NETWORK;
    
    // CO-RE: safely read network info
    e->data[0] = family;
    if (family == AF_INET) {
        BPF_CORE_READ_INTO(&e->data[1], sk, __sk_common.skc_daddr);
        BPF_CORE_READ_INTO(&e->data[5], sk, __sk_common.skc_dport);
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
```

### 2.3 Build with CO-RE Support
```makefile
# pkg/collectors/ebpf/bpf/Makefile
CLANG := clang
LLVM_STRIP := llvm-strip
BPFTOOL := bpftool
ARCH := $(shell uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')

# CO-RE compilation flags
BPF_CFLAGS := -g -O2 -target bpf -D__TARGET_ARCH_$(ARCH)
BPF_CFLAGS += -I/usr/include/$(shell uname -m)-linux-gnu
BPF_CFLAGS += -Wall -Wno-unused-value -Wno-pointer-sign
BPF_CFLAGS += -Wno-compare-distinct-pointer-types

# Generate vmlinux.h for CO-RE
vmlinux.h:
	$(BPFTOOL) btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h

# Build BPF program with CO-RE
unified.bpf.o: unified.c vmlinux.h
	$(CLANG) $(BPF_CFLAGS) -c unified.c -o unified.bpf.o
	$(LLVM_STRIP) -g unified.bpf.o

# Generate skeleton for Go
unified.skel.h: unified.bpf.o
	$(BPFTOOL) gen skeleton unified.bpf.o > unified.skel.h

clean:
	rm -f *.o *.skel.h vmlinux.h
```

## Phase 3: Unified Collector Binary with OTEL (Week 2)

### 3.1 Single Binary Architecture with Pluggable Collectors
```go
// cmd/tapio-collector/main.go
func main() {
    // Initialize OTEL
    ctx := context.Background()
    otelShutdown := initOTEL(ctx)
    defer otelShutdown(ctx)
    
    // Initialize collector registry
    registry := collectors.NewRegistry()
    registerBuiltinCollectors(registry)
    
    config := LoadConfig()
    output := make(chan []byte, 10000)
    
    // Create collectors from config
    activeCollectors := []collectors.Collector{}
    for _, collectorConfig := range config.Collectors {
        collector, err := registry.Create(
            collectorConfig.Type,
            collectorConfig.Config,
        )
        if err != nil {
            log.Printf("Failed to create %s collector: %v", collectorConfig.Type, err)
            continue
        }
        activeCollectors = append(activeCollectors, collector)
    }
    
    // Run all collectors
    for _, collector := range activeCollectors {
        go collector.Run(ctx, output)
    }
    
    // Stream to pipeline with trace context
    client := NewPipelineClient(config.PipelineEndpoint)
    for event := range output {
        client.SendWithContext(ctx, event)
    }
}

// Register built-in collectors
func registerBuiltinCollectors(registry *collectors.CollectorRegistry) {
    // Core collectors
    registry.Register("ebpf", collectors.NewEBPFCollector)
    registry.Register("k8s", collectors.NewK8sCollector)
    registry.Register("cni", collectors.NewCNICollector)
    registry.Register("systemd", collectors.NewSystemdCollector)
    
    // Easy to add new ones
    registry.Register("etcd", collectors.NewEtcdCollector)
    registry.Register("prometheus", collectors.NewPrometheusCollector)
    registry.Register("vault", collectors.NewVaultCollector)
}

// Configuration supports dynamic collectors
type Config struct {
    Collectors []CollectorConfig `yaml:"collectors"`
}

type CollectorConfig struct {
    Type   string                 `yaml:"type"`
    Config map[string]interface{} `yaml:"config"`
}
```

### 3.2 Example: Adding etcd Collector
```go
// pkg/collectors/etcd/collector.go
package etcd

import (
    "github.com/yairfalse/tapio/pkg/collectors"
    clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdCollector struct {
    client  *clientv3.Client
    tracer  trace.Tracer
    metrics struct {
        eventsTotal metric.Int64Counter
    }
}

// Factory function for registry
func NewEtcdCollector(config map[string]interface{}) (collectors.Collector, error) {
    endpoints := config["endpoints"].([]string)
    
    client, err := clientv3.New(clientv3.Config{
        Endpoints: endpoints,
    })
    if err != nil {
        return nil, err
    }
    
    return &EtcdCollector{
        client: client,
        tracer: otel.Tracer("etcd-collector"),
    }, nil
}

func (c *EtcdCollector) Name() string {
    return "etcd"
}

func (c *EtcdCollector) Run(ctx context.Context, output chan<- []byte) error {
    // Watch etcd events
    watchChan := c.client.Watch(ctx, "", clientv3.WithPrefix())
    
    for watchResp := range watchChan {
        for _, event := range watchResp.Events {
            // Create raw event
            raw := collectors.RawEvent{
                Source:    "etcd",
                Timestamp: time.Now().UnixNano(),
                Data:      event.Kv.Value,
                TraceID:   trace.SpanFromContext(ctx).SpanContext().TraceID().String(),
            }
            
            output <- raw.Serialize()
            c.metrics.eventsTotal.Add(ctx, 1)
        }
    }
    
    return nil
}
```

// Initialize OpenTelemetry
func initOTEL(ctx context.Context) func(context.Context) error {
    // Prometheus metrics exporter
    promExporter, _ := prometheus.New()
    meterProvider := metric.NewMeterProvider(
        metric.WithReader(promExporter),
    )
    otel.SetMeterProvider(meterProvider)
    
    // OTLP trace exporter (enhanced for correlation)
    traceExporter, _ := otlptrace.New(ctx,
        otlptrace.WithEndpoint(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
    )
    traceProvider := trace.NewTracerProvider(
        trace.WithBatcher(traceExporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.ServiceNameKey.String("tapio-collector"),
            semconv.ServiceVersionKey.String(version),
        )),
    )
    otel.SetTracerProvider(traceProvider)
    
    return func(ctx context.Context) error {
        return traceProvider.Shutdown(ctx)
    }
}
```

### 3.3 CO-RE Benefits in Production

With CO-RE (Compile Once, Run Everywhere), the eBPF collector:

1. **No Kernel Headers Required**
   - Ships with pre-compiled BPF bytecode
   - BTF (BPF Type Format) handles relocations
   - Works across kernel versions (4.18+)

2. **Single Binary Distribution**
   ```dockerfile
   # Dockerfile - No build tools needed!
   FROM alpine:3.18
   COPY tapio-collector /usr/local/bin/
   # BPF program embedded in binary
   CMD ["tapio-collector"]
   ```

3. **Automatic Field Adaptation**
   - `BPF_CORE_READ` handles struct changes
   - `bpf_core_field_exists` for optional fields
   - Graceful degradation on older kernels

4. **Deployment Simplicity**
   ```yaml
   # No kernel-specific images needed
   image: tapio/collector:latest  # Works on any CO-RE enabled kernel
   ```

### 3.4 Configuration Example
```yaml
# /etc/tapio/collector.yaml
collectors:
  # Core collectors
  - type: ebpf
    config:
      ring_buffer_size: 8388608  # 8MB
      
  - type: k8s
    config:
      resources:
        - pods
        - services
        - nodes
        
  - type: cni
    config:
      log_path: /var/log/cni/cni.log
      
  - type: systemd
    config:
      services:
        - kubelet
        - containerd
        
  # Additional collectors (just add to config!)
  - type: etcd
    config:
      endpoints:
        - https://etcd-0:2379
        - https://etcd-1:2379
        - https://etcd-2:2379
      watch_prefix: /registry
      
  - type: prometheus
    config:
      endpoint: http://prometheus:9090
      queries:
        - name: pod_cpu_usage
          query: 'rate(container_cpu_usage_seconds_total[5m])'
          interval: 30s

pipeline:
  endpoint: tapio-pipeline:9090
  batch_size: 100
  flush_interval: 5s
```

### 3.4 K8s Deployment with OTEL
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-collector
  namespace: tapio-system
spec:
  selector:
    matchLabels:
      app: tapio-collector
  template:
    metadata:
      labels:
        app: tapio-collector
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: tapio-collector
      hostPID: true
      hostNetwork: true
      containers:
      - name: collector
        image: tapio/unified-collector:latest
        env:
        - name: PIPELINE_ENDPOINT
          value: "tapio-pipeline:9090"
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        # OTEL Configuration
        - name: OTEL_SERVICE_NAME
          value: "tapio-collector"
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "http://otel-collector:4317"
        - name: OTEL_METRICS_EXPORTER
          value: "prometheus"
        - name: OTEL_TRACES_EXPORTER
          value: "otlp"
        - name: OTEL_RESOURCE_ATTRIBUTES
          value: "k8s.node.name=$(NODE_NAME),k8s.namespace.name=tapio-system"
        ports:
        - name: metrics
          containerPort: 9090
          protocol: TCP
        securityContext:
          privileged: true
        volumeMounts:
        - name: sys
          mountPath: /sys
        - name: cni-log
          mountPath: /var/log/cni
        resources:
          limits:
            memory: "256Mi"
            cpu: "500m"
          requests:
            memory: "128Mi"
            cpu: "100m"
      volumes:
      - name: sys
        hostPath:
          path: /sys
      - name: cni-log
        hostPath:
          path: /var/log/cni
```

### 3.3 OTEL Collector Configuration
```yaml
# For collecting metrics and traces from all collectors
apiVersion: v1
kind: ConfigMap
metadata:
  name: otel-collector-config
  namespace: tapio-system
data:
  otel-collector-config.yaml: |
    receivers:
      prometheus:
        config:
          scrape_configs:
            - job_name: 'tapio-collectors'
              kubernetes_sd_configs:
                - role: pod
              relabel_configs:
                - source_labels: [__meta_kubernetes_pod_label_app]
                  action: keep
                  regex: tapio-collector
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
    
    processors:
      batch:
        timeout: 10s
      k8sattributes:
        extract:
          metadata:
            - k8s.pod.name
            - k8s.pod.uid
            - k8s.namespace.name
            - k8s.node.name
            - k8s.deployment.name
    
    exporters:
      prometheus:
        endpoint: "0.0.0.0:8889"
      jaeger:
        endpoint: jaeger-collector:14250
        tls:
          insecure: true
    
    service:
      pipelines:
        metrics:
          receivers: [prometheus]
          processors: [batch, k8sattributes]
          exporters: [prometheus]
        traces:
          receivers: [otlp]
          processors: [batch, k8sattributes]
          exporters: [jaeger]
```

## Phase 4: K8s-Aware Pipeline (Week 2-3)

### 4.1 Pipeline Architecture - Bridge to Intelligence
The pipeline serves as the bridge between raw collectors and the intelligence package which expects UnifiedEvents.

```go
// pkg/pipeline/k8s_pipeline.go
type K8sPipeline struct {
    // K8s context cache
    k8sCache *K8sContextCache
    
    // Processing stages
    decoder   *EventDecoder
    enricher  *K8sEnricher
    converter *UnifiedEventConverter
    
    // Intelligence pipeline (expects UnifiedEvents)
    intelligencePipeline pipeline.IntelligencePipeline
    
    // Output
    output chan *domain.UnifiedEvent
}

func (p *K8sPipeline) Process(raw []byte) error {
    // 1. Decode raw event envelope
    envelope := &RawEventEnvelope{}
    if err := json.Unmarshal(raw, envelope); err != nil {
        return err
    }
    
    // 2. Extract trace context if present (Enhanced OTEL)
    ctx := context.Background()
    if envelope.TraceID != "" {
        ctx = trace.ContextWithSpanContext(ctx, trace.SpanContext{
            TraceID: trace.TraceID(envelope.TraceID),
            SpanID:  trace.SpanID(envelope.SpanID),
        })
    }
    
    // 3. Convert to UnifiedEvent based on source
    var unified *domain.UnifiedEvent
    switch envelope.Source {
    case "ebpf":
        unified = p.convertEBPFEvent(ctx, envelope)
    case "k8s":
        unified = p.convertK8sEvent(ctx, envelope)
    case "cni":
        unified = p.convertCNIEvent(ctx, envelope)
    case "systemd":
        unified = p.convertSystemdEvent(ctx, envelope)
    }
    
    // 4. Enrich with K8s context
    p.enricher.EnrichEvent(unified)
    
    // 5. Send to intelligence pipeline for correlation
    if err := p.intelligencePipeline.ProcessEvent(unified); err != nil {
        return fmt.Errorf("intelligence pipeline error: %w", err)
    }
    
    // 6. Also output for other consumers
    p.output <- unified
    
    return nil
}

// Example: Convert raw eBPF to UnifiedEvent
func (p *K8sPipeline) convertEBPFEvent(ctx context.Context, envelope *RawEventEnvelope) *domain.UnifiedEvent {
    // Decode eBPF-specific data
    var ebpfData struct {
        PID     uint32
        Syscall uint32
        CPU     uint32
    }
    binary.Read(bytes.NewReader(envelope.Data), binary.LittleEndian, &ebpfData)
    
    // Build UnifiedEvent
    event := &domain.UnifiedEvent{
        ID:        domain.GenerateEventID(),
        Timestamp: time.Unix(0, envelope.Timestamp),
        Type:      domain.EventTypeKernel,
        Source:    "ebpf",
        
        // Preserve trace context
        TraceContext: &domain.TraceContext{
            TraceID: envelope.TraceID,
            SpanID:  envelope.SpanID,
        },
        
        // Kernel data
        Kernel: &domain.KernelData{
            PID:     ebpfData.PID,
            CPUCore: int(ebpfData.CPU),
            Syscall: syscallToString(ebpfData.Syscall),
        },
        
        // Raw data for replay/debug
        RawData: envelope.Data,
    }
    
    return event
}
```

### 4.2 Integration with Correlation Engine

The pipeline ensures all events are properly formatted for the correlation engine:

```go
// pkg/pipeline/correlation_integration.go
type CorrelationIntegration struct {
    pipeline             *K8sPipeline
    correlationEngine    correlation.SemanticCorrelationEngine
    intelligencePipeline pipeline.IntelligencePipeline
}

func (ci *CorrelationIntegration) Start(ctx context.Context) error {
    // Initialize intelligence pipeline with correlation
    config := pipeline.DefaultConfig()
    config.CorrelationEnabled = true
    
    ci.intelligencePipeline = pipeline.NewBuilder().
        WithMode(pipeline.PipelineModeHighPerformance).
        WithCorrelationEngine(&ci.correlationEngine).
        WithMetricsExporter(promExporter).
        Build()
    
    // Start processing unified events
    go ci.processUnifiedEvents(ctx)
    
    return ci.intelligencePipeline.Start(ctx)
}

func (ci *CorrelationIntegration) processUnifiedEvents(ctx context.Context) {
    for unified := range ci.pipeline.output {
        // Events already have:
        // - OTEL trace context for correlation
        // - K8s context for grouping
        // - Semantic context for intelligence
        
        // The correlation engine can now:
        // 1. Group by trace ID
        // 2. Correlate by K8s workload
        // 3. Detect patterns across sources
        
        ci.correlationEngine.ProcessUnifiedEvent(unified)
    }
}
```

### 4.3 K8s Context Cache
```go
// pkg/pipeline/k8s_cache.go
type K8sContextCache struct {
    // Core objects
    pods       map[string]*v1.Pod
    services   map[string]*v1.Service
    nodes      map[string]*v1.Node
    
    // Reverse lookups for enrichment
    pidToPod       map[uint32]*PodInfo
    ipToPod        map[string]*PodInfo
    containerToPod map[string]*PodInfo
    
    // Service relationships
    podToServices  map[string][]string
    serviceToPods  map[string][]string
}
```

## Phase 5: Testing & Migration (Week 3-4)

### 5.1 Testing Strategy
- Unit tests for each minimal collector
- Integration tests with mock K8s API
- Performance benchmarks (target: <2% CPU overhead)
- Memory usage tests (target: <100MB per collector)

### 5.2 Migration Plan
1. Deploy new collector alongside old (with different metrics)
2. Compare event rates and quality
3. Gradually shift traffic to new collector
4. Remove old collectors after validation

## Observability Strategy

### Using OpenTelemetry (OTEL) and Enhanced OTEL
We leverage OTEL for all observability needs instead of building custom APIs:

1. **Metrics**: Prometheus exporter for collector health, event rates, and performance
2. **Traces**: OTLP exporter for distributed tracing and event correlation
3. **Enhanced OTEL**: Trace context propagation through all events for correlation

### Key OTEL Enhancements
- **Trace Context in Events**: Every raw event carries OTEL trace/span IDs
- **K8s Attributes**: Automatic injection of K8s metadata via k8sattributes processor
- **Correlation Support**: Events can be correlated across collectors via trace IDs
- **No Custom APIs**: Health/metrics/status all handled via standard OTEL

## Success Metrics

### Performance
- **Binary size**: 6MB → <1MB
- **Memory usage**: 200MB → <100MB  
- **CPU overhead**: <2% on nodes
- **Startup time**: <5 seconds

### Architecture
- **Lines of code**: 50% reduction
- **Test coverage**: >80%
- **Deployment complexity**: 4 configs → 1 config
- **Observability**: 100% OTEL instrumented

### Functionality
- **Event capture rate**: Same or better
- **K8s context accuracy**: 100%
- **Event latency**: <10ms p99
- **Trace correlation**: 100% of events traceable

## Implementation Timeline

| Week | Phase | Deliverables |
|------|-------|--------------|
| 1 | Simplify Collectors | Minimal collectors without business logic |
| 1-2 | Consolidate eBPF | Single BPF program, userspace protocol detection |
| 2 | Unified Binary | Single collector binary with all collectors |
| 2-3 | K8s Pipeline | Separate pipeline service with enrichment |
| 3-4 | Testing & Migration | Full testing suite and gradual rollout |

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Missing events during migration | High | Run old and new collectors in parallel |
| Performance regression | Medium | Extensive benchmarking before rollout |
| K8s API rate limits | Medium | Implement caching and rate limiting |
| BPF verifier issues | Low | Start with minimal BPF program |

## Next Steps

1. **Get approval** for this plan
2. **Create feature branch** `feat/collector-refactor`
3. **Start with Phase 1** - simplify collectors
4. **Weekly progress reviews**

---

## Appendix: OTEL Integration Examples

### Collector with OTEL Metrics
```go
// Example: K8s collector with rich metrics
func (c *K8sCollector) recordEventMetrics(ctx context.Context, event *v1.Event) {
    c.eventsTotal.Add(ctx, 1,
        attribute.String("reason", event.Reason),
        attribute.String("type", event.Type),
        attribute.String("namespace", event.Namespace),
        attribute.String("kind", event.InvolvedObject.Kind),
    )
    
    if event.Type == "Warning" {
        c.warningsTotal.Add(ctx, 1)
    }
}
```

### Enhanced OTEL for Correlation
```go
// Pipeline preserves and enhances trace context
func (p *Pipeline) Process(ctx context.Context, raw RawEvent) {
    // Extract trace context from raw event
    if raw.TraceID != "" {
        ctx = trace.ContextWithSpanContext(ctx, trace.SpanContext{
            TraceID: trace.TraceID(raw.TraceID),
            SpanID:  trace.SpanID(raw.SpanID),
        })
    }
    
    // Create child span for processing
    ctx, span := p.tracer.Start(ctx, "pipeline.process",
        trace.WithAttributes(
            attribute.String("event.source", raw.Source),
            attribute.String("k8s.node", raw.NodeName),
        ),
    )
    defer span.End()
    
    // Process maintains trace context
    unified := p.enrichAndConvert(ctx, raw)
    unified.TraceContext = &TraceContext{
        TraceID: span.SpanContext().TraceID().String(),
        SpanID:  span.SpanContext().SpanID().String(),
    }
}
```

---

### Critical Integration Points

1. **Collectors → Pipeline**: Raw bytes with OTEL trace context
2. **Pipeline → UnifiedEvent**: Conversion happens here, not in collectors
3. **UnifiedEvent → Intelligence**: All correlation/analytics expect UnifiedEvent
4. **OTEL Context**: Preserved throughout for correlation

### Data Flow Summary
```
[Raw Collectors] → [Raw Bytes + TraceID] → [Pipeline Converter] → [UnifiedEvent] → [Intelligence/Correlation]
     ↑                                           ↑                      ↑                    ↑
     |                                           |                      |                    |
  Simple                                    K8s Enrichment          Standard            Advanced
  No Logic                                  Happens Here             Format             Processing
  Pluggable!                                                                        
```

### Adding New Collectors

To add a new collector (e.g., etcd):

1. **Implement the Collector interface** (~100 lines)
   ```go
   type EtcdCollector struct {}
   func (c *EtcdCollector) Run(ctx context.Context, output chan<- []byte) error
   ```

2. **Register in main.go**
   ```go
   registry.Register("etcd", collectors.NewEtcdCollector)
   ```

3. **Add to config**
   ```yaml
   collectors:
     - type: etcd
       config:
         endpoints: ["etcd:2379"]
   ```

4. **Pipeline automatically handles it** - No changes needed!

That's it! The pluggable architecture means:
- No recompilation needed (if using plugin system)
- Pipeline knows how to convert any source
- Correlation works across all collectors
- OTEL metrics/traces automatic

---

### CO-RE Summary

The refactored collector uses BPF CO-RE throughout:
- **Compile Once**: BPF programs built during development
- **Run Everywhere**: Works on any Linux 4.18+ with BTF
- **No Dependencies**: No kernel headers, LLVM, or Clang on target
- **Embedded Programs**: BPF bytecode embedded in Go binary
- **Automatic Adaptation**: Handles kernel struct changes

This means:
```bash
# Old way (requires kernel headers, LLVM, etc.)
apt-get install linux-headers-$(uname -r) llvm clang
make bpf
./collector

# New way with CO-RE
./tapio-collector  # Just works!
```

---

**Document Status**: READY FOR REVIEW  
**Last Updated**: 2024-07-29  
**Author**: Tapio Team  
**Changes**: 
- Added OTEL observability strategy and integration examples
- Clarified integration with correlation engine and intelligence pipeline
- Added data flow diagram showing how raw events become UnifiedEvents
- Added pluggable collector architecture with registry pattern
- Added BPF CO-RE implementation for cross-kernel compatibility