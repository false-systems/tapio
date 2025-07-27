# NATS Messaging Architecture for Tapio

## Overview

This document outlines the NATS-based messaging architecture for Tapio, designed for Kubernetes deployment with the new correlation engine architecture.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              Kubernetes Cluster                                 │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐             │
│  │   K8s Collector │    │ systemd Collect │    │  eBPF Collector │             │
│  │    DaemonSet    │    │   DaemonSet     │    │   DaemonSet     │             │
│  │                 │    │                 │    │                 │             │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │             │
│  │ │   Events    │─┼────┼─┤   Events    │─┼────┼─┤   Events    │ │             │
│  │ │  Publisher  │ │    │ │  Publisher  │ │    │ │  Publisher  │ │             │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │             │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘             │
│           │                       │                       │                    │
│           └───────────────────────┼───────────────────────┘                    │
│                                   │                                            │
│  ┌─────────────────────────────────┼─────────────────────────────────────────┐ │
│  │                    NATS JetStream Cluster                                  │ │
│  │                                 │                                         │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │ │
│  │  │    NATS     │  │    NATS     │  │    NATS     │  │ JetStream   │      │ │
│  │  │   Server    │  │   Server    │  │   Server    │  │   Storage   │      │ │
│  │  │ (StatefulS) │  │ (StatefulS) │  │ (StatefulS) │  │    (PVC)    │      │ │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘      │ │
│  │                                 │                                         │ │
│  └─────────────────────────────────┼─────────────────────────────────────────┘ │
│                                   │                                            │
│           ┌───────────────────────┼───────────────────────┐                    │
│           │                       │                       │                    │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐             │
│  │ NEW Correlation │    │   Pipeline      │    │  SIEM Export    │             │
│  │     Engine      │    │   Processor     │    │   Consumer      │             │
│  │   (New Design)  │    │  (Deployment)   │    │  (Deployment)   │             │
│  │                 │    │                 │    │                 │             │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │             │
│  │ │   Events    │ │    │ │   Events    │ │    │ │   Events    │ │             │
│  │ │ Subscriber  │ │    │ │ Subscriber  │ │    │ │ Subscriber  │ │             │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │             │
│  │       │         │    │       │         │    │       │         │             │
│  │ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │             │
│  │ │Correlations │ │    │ │  Processed  │ │    │ │   Alerts    │ │             │
│  │ │ Publisher   │ │    │ │ Publisher   │ │    │ │ Publisher   │ │             │
│  │ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │             │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘             │
│                                                                                 │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                        NATS Subject Structure                           │   │
│  │                                                                         │   │
│  │  tapio.events.k8s.{namespace}.{resource}.{action}                      │   │
│  │  tapio.events.systemd.{service}.{state}                                │   │
│  │  tapio.events.ebpf.{type}.{severity}                                   │   │
│  │  tapio.events.cni.{interface}.{direction}                              │   │
│  │  tapio.correlations.{confidence}.{type}                                │   │
│  │  tapio.alerts.{severity}.{component}                                   │   │
│  │  tapio.processed.{type}.{destination}                                  │   │
│  │                                                                         │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Component Details

### NATS JetStream Cluster

**Deployment**: StatefulSet with 3 replicas
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nats-jetstream
spec:
  replicas: 3
  serviceName: nats-jetstream
  template:
    spec:
      containers:
      - name: nats
        image: nats:2.10-alpine
        args:
          - "--cluster_name=tapio-nats"
          - "--jetstream"
          - "--store_dir=/data/jetstream"
          - "--max_file_store=10GB"
          - "--max_mem_store=1GB"
```

**Storage**: Persistent volumes for JetStream durability
**Networking**: ClusterIP service for internal communication

### Collectors (DaemonSets)

**Publisher Pattern**:
```go
// In each collector
type EventPublisher struct {
    nc   *nats.Conn
    js   nats.JetStreamContext
}

func (p *EventPublisher) PublishEvent(event *domain.UnifiedEvent) error {
    subject := p.buildSubject(event)
    data, _ := json.Marshal(event)
    
    _, err := p.js.Publish(subject, data)
    return err
}

func (p *EventPublisher) buildSubject(event *domain.UnifiedEvent) string {
    switch event.Source {
    case "k8s-collector":
        return fmt.Sprintf("tapio.events.k8s.%s.%s.%s", 
            event.Context.Namespace, event.Type, event.Action)
    case "systemd-collector":
        return fmt.Sprintf("tapio.events.systemd.%s.%s", 
            event.Data["service"], event.Type)
    case "ebpf-collector":
        return fmt.Sprintf("tapio.events.ebpf.%s.%s", 
            event.Type, event.Severity)
    }
}
```

### Correlation Engine (New Design)

**Subscriber Pattern**:
```go
type CorrelationEngine struct {
    nc         *nats.Conn
    js         nats.JetStreamContext
    processors map[string]*CorrelationProcessor
}

func (ce *CorrelationEngine) Initialize() error {
    // Subscribe to all event streams
    ce.js.Subscribe("tapio.events.>", ce.processEvent)
    
    // Set up correlation result publishing
    return nil
}

func (ce *CorrelationEngine) processEvent(msg *nats.Msg) {
    event := &domain.UnifiedEvent{}
    json.Unmarshal(msg.Data, event)
    
    // Run through new correlation algorithm
    correlations := ce.correlate(event)
    
    // Publish correlation results
    for _, corr := range correlations {
        subject := fmt.Sprintf("tapio.correlations.%s.%s", 
            corr.Confidence, corr.Type)
        ce.js.Publish(subject, corr.ToJSON())
    }
    
    msg.Ack()
}
```

## Subject Design Strategy

### Event Streams
```
tapio.events.k8s.default.pods.created
tapio.events.k8s.production.services.unhealthy
tapio.events.systemd.nginx.failed
tapio.events.ebpf.network.high_latency
tapio.events.cni.eth0.packet_loss
```

### Correlation Streams
```
tapio.correlations.high.cascading_failure
tapio.correlations.medium.performance_degradation
tapio.correlations.low.potential_issue
```

### Alert Streams
```
tapio.alerts.critical.security_incident
tapio.alerts.warning.resource_exhaustion
tapio.alerts.info.deployment_completed
```

## Kubernetes Deployment Strategy

### 1. NATS Operator Deployment
```bash
kubectl apply -f https://github.com/nats-io/nats-operator/releases/latest/download/00-prereqs.yaml
kubectl apply -f https://github.com/nats-io/nats-operator/releases/latest/download/10-deployment.yaml
```

### 2. NATS Cluster Configuration
```yaml
apiVersion: nats.io/v1alpha2
kind: NatsCluster
metadata:
  name: tapio-nats
spec:
  size: 3
  version: "2.10.0"
  
  jetstream:
    enabled: true
    fileStorage:
      size: 10Gi
      storageClass: "fast-ssd"
    memStorage:
      size: 1Gi
      
  pod:
    resources:
      requests:
        cpu: "100m"
        memory: "256Mi"
      limits:
        cpu: "500m"
        memory: "1Gi"
        
  auth:
    enabled: true
    timeout: "5s"
```

### 3. Collector DaemonSet Updates
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: tapio-k8s-collector
spec:
  template:
    spec:
      containers:
      - name: k8s-collector
        image: tapio/k8s-collector:latest
        env:
        - name: NATS_URL
          value: "nats://tapio-nats:4222"
        - name: NATS_SUBJECT_PREFIX
          value: "tapio.events.k8s"
        volumeMounts:
        - name: nats-creds
          mountPath: /etc/nats-creds
          readOnly: true
      volumes:
      - name: nats-creds
        secret:
          secretName: nats-user-creds
```

## Migration Strategy

### Phase 1: Core Messaging (Week 1-2)
1. Deploy NATS cluster in K8s
2. Update collectors to publish to NATS instead of direct pipeline
3. Update pipeline to consume from NATS
4. Basic subject structure implementation

### Phase 2: Correlation Integration (Week 3-4)
1. Integrate new correlation engine with NATS
2. Implement correlation result publishing
3. Add consumer for correlation results
4. Testing and validation

### Phase 3: Advanced Features (Week 5-6)
1. SIEM export consumer
2. Monitoring and alerting streams
3. Event replay capabilities
4. Performance optimization

### Phase 4: Production Hardening (Week 7-8)
1. Security hardening (TLS, auth)
2. Monitoring and observability
3. Backup and recovery procedures
4. Load testing and tuning

## Performance Considerations

### Throughput Expectations
- **eBPF Collector**: 100K-1M events/sec
- **K8s Collector**: 1K-10K events/sec  
- **systemd Collector**: 100-1K events/sec
- **Total**: ~1M events/sec peak

### NATS Configuration
```yaml
jetstream:
  max_memory: 1GB
  max_file: 10GB
  max_age: 7d  # Keep events for 7 days
  replicas: 3  # High availability
  
stream_limits:
  max_msgs: 10000000
  max_bytes: 10GB
  max_msg_size: 1MB
```

### Resource Requirements
```yaml
nats-server:
  requests:
    cpu: 200m
    memory: 512Mi
  limits:
    cpu: 1000m
    memory: 2Gi
    
storage:
  class: fast-ssd
  size: 50Gi  # Per NATS server
```

## Monitoring & Observability

### NATS Metrics
- Message throughput (msgs/sec)
- JetStream storage usage
- Consumer lag
- Connection health

### Prometheus Integration
```yaml
apiVersion: v1
kind: Service
metadata:
  name: nats-monitoring
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "7777"
    prometheus.io/path: "/metrics"
spec:
  ports:
  - name: monitoring
    port: 7777
    targetPort: 7777
```

### Grafana Dashboards
- NATS cluster health
- Event flow visualization
- Correlation engine performance
- Consumer lag monitoring

## Security

### Authentication
```yaml
auth:
  enabled: true
  users:
    - user: "tapio-collectors"
      permissions:
        publish: ["tapio.events.>"]
    - user: "tapio-correlation"
      permissions:
        subscribe: ["tapio.events.>"]
        publish: ["tapio.correlations.>"]
    - user: "tapio-consumers"
      permissions:
        subscribe: ["tapio.correlations.>", "tapio.alerts.>"]
```

### TLS Encryption
```yaml
tls:
  enabled: true
  cert: /etc/nats-certs/server-cert.pem
  key: /etc/nats-certs/server-key.pem
  ca_cert: /etc/nats-certs/ca-cert.pem
  verify: true
```

## Advantages of This Architecture

### For Tapio
1. **Decoupling**: Collectors independent from correlation engine changes
2. **Scalability**: Easy horizontal scaling of consumers
3. **Reliability**: Message persistence and replay capabilities
4. **Flexibility**: Multiple consumers (correlation, SIEM, debugging)
5. **K8s Native**: Built for cloud-native deployment

### For Development
1. **Easier Testing**: Event replay for correlation testing
2. **Better Debugging**: Message inspection and tracing
3. **Parallel Development**: Teams can work independently
4. **A/B Testing**: Multiple correlation engine versions

### Operational
1. **High Availability**: NATS cluster with automatic failover
2. **Monitoring**: Rich metrics and observability
3. **Security**: Strong authentication and encryption
4. **Backup**: Event persistence for disaster recovery

## Next Steps

1. **Prototype**: Create basic NATS integration with one collector
2. **Benchmark**: Test performance with expected event volumes  
3. **Design**: Finalize subject structure and consumer patterns
4. **Implement**: Roll out to all collectors
5. **Optimize**: Tune for production performance

This architecture positions Tapio for massive scale while maintaining the flexibility needed for your correlation engine evolution!