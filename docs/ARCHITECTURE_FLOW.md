# Tapio Architecture Flow

## Overview

Tapio is THE K8s intelligence platform that answers "WHY?" not "WHAT?" by collecting, correlating, and analyzing events across your Kubernetes infrastructure.

## Data Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           COLLECTORS                                      │
├─────────────────────────────────────────────────────────────────────────┤
│  KubeAPI  │  eBPF  │  Systemd/Journal  │  etcd  │  CNI                 │
└────┬──────┴───┬────┴──────────┬────────┴───┬────┴──┬──────────────────┘
     │          │               │            │       │
     └──────────┴───────────────┴────────────┴───────┴──────┐
                                                             │
                                                             ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         EVENT PIPELINE                                   │
│  • Collects RawEvents from all collectors                              │
│  • Enriches with K8s metadata                                          │
│  • Publishes to NATS JetStream                                         │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    NATS JetStream (raw.*)                               │
│  • Persists raw events with subjects like:                             │
│    - raw.kubeapi.pod.create                                           │
│    - raw.ebpf.network.connect                                         │
│    - raw.systemd.journal.error                                        │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      TRANSFORMER SERVICE                                 │
│  • Consumes from raw.*                                                 │
│  • Converts RawEvent → UnifiedEvent                                    │
│  • Adds semantic context                                               │
│  • Publishes to unified.*                                              │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                   NATS JetStream (unified.*)                           │
│  • Persists unified events with subjects like:                         │
│    - unified.pod.default.nginx-7d8f                                   │
│    - unified.service.prod.api-gateway                                 │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    CORRELATION SERVICE                                   │
│  • Consumes from unified.*                                             │
│  • Performs multi-dimensional correlation:                             │
│    - K8s hierarchy (Pod → ReplicaSet → Deployment)                    │
│    - Temporal correlation (events within time windows)                │
│    - Sequence detection (patterns of events)                          │
│  • Outputs correlation results                                         │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         NEO4J GRAPH                                      │
│  • Stores relationships and correlations                               │
│  • Enables complex queries                                             │
│  • Powers "WHY?" answers                                               │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          API SERVICE                                     │
│  • GraphQL/REST endpoints                                              │
│  • Answers questions like:                                             │
│    - "Why did my pod crash?"                                          │
│    - "What caused the network timeout?"                               │
│    - "Which config change triggered the failure?"                     │
└─────────────────────────────────────────────────────────────────────────┘
```

## Running the Services

### 1. Start NATS JetStream
```bash
docker run -d --name nats \
  -p 4222:4222 \
  -p 6222:6222 \
  -p 8222:8222 \
  nats:latest \
  -js
```

### 2. Start Collectors
```bash
# All collectors with default settings
./tapio-collectors --nats=nats://localhost:4222

# Or specific collectors
./tapio-collectors \
  --nats=nats://localhost:4222 \
  --enable-kubeapi=true \
  --enable-ebpf=true \
  --enable-systemd=true \
  --enable-etcd=true \
  --enable-cni=true
```

### 3. Start Transformer Service
```bash
./tapio-transformer
```

### 4. Start Correlation Service
```bash
./tapio-correlation
```

### 5. Start API Service
```bash
./tapio-api
```

## Event Examples

### Raw Event (from collector)
```json
{
  "timestamp": "2024-01-10T10:30:00Z",
  "type": "pod_created",
  "data": "...",
  "metadata": {
    "collector": "kubeapi",
    "k8s_namespace": "default",
    "k8s_name": "nginx-7d8f",
    "k8s_kind": "Pod",
    "k8s_uid": "abc-123",
    "k8s_labels": "app=nginx,version=1.0",
    "k8s_owner_refs": "ReplicaSet/nginx-7d8f"
  },
  "trace_id": "trace-123",
  "span_id": "span-456"
}
```

### Unified Event (after transformation)
```json
{
  "id": "evt-789",
  "timestamp": "2024-01-10T10:30:00Z",
  "type": "lifecycle",
  "source": "kubeapi",
  "severity": "info",
  "trace_context": {
    "trace_id": "trace-123",
    "span_id": "span-456"
  },
  "k8s_context": {
    "name": "nginx-7d8f",
    "namespace": "default",
    "kind": "Pod",
    "uid": "abc-123",
    "labels": {"app": "nginx", "version": "1.0"},
    "owner_references": [{
      "kind": "ReplicaSet",
      "name": "nginx-7d8f"
    }]
  },
  "entity": {
    "type": "pod",
    "name": "nginx-7d8f",
    "namespace": "default"
  }
}
```

## K8s Metadata Enhancement

All collectors enhance events with standardized K8s metadata:
- `k8s_namespace`: Resource namespace
- `k8s_name`: Resource name
- `k8s_kind`: Resource kind (Pod, Service, etc.)
- `k8s_uid`: Unique identifier
- `k8s_labels`: Resource labels (serialized)
- `k8s_owner_refs`: Owner references (serialized)

## Service Dependencies

1. **NATS JetStream**: Message broker and event store
2. **Kubernetes API**: For K8s metadata enrichment
3. **Neo4j**: Graph database for correlations (optional)
4. **etcd**: For etcd collector (optional)