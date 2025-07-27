# NATS Implementation Guide for Tapio

## Quick Implementation Steps

### 1. Deploy NATS in K8s (5 minutes)

```bash
# Install NATS Operator
kubectl apply -f https://raw.githubusercontent.com/nats-io/nats-operator/main/deploy/00-prereqs.yaml
kubectl apply -f https://raw.githubusercontent.com/nats-io/nats-operator/main/deploy/10-deployment.yaml

# Deploy NATS Cluster
cat <<EOF | kubectl apply -f -
apiVersion: nats.io/v1alpha2
kind: NatsCluster
metadata:
  name: tapio-nats
  namespace: tapio
spec:
  size: 3
  version: "2.10.0"
  
  jetstream:
    enabled: true
    fileStorage:
      size: 10Gi
      storageClass: "standard"
    memStorage:
      size: 512Mi
      
  pod:
    resources:
      requests:
        cpu: "100m"
        memory: "256Mi"
      limits:
        cpu: "500m"
        memory: "1Gi"
EOF
```

### 2. Update Collector Code (Minimal Changes)

```go
// pkg/collectors/common/nats_publisher.go
package common

import (
    "encoding/json"
    "fmt"
    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/domain"
)

type NATSPublisher struct {
    nc *nats.Conn
    js nats.JetStreamContext
}

func NewNATSPublisher(url string) (*NATSPublisher, error) {
    nc, err := nats.Connect(url)
    if err != nil {
        return nil, err
    }
    
    js, err := nc.JetStream()
    if err != nil {
        return nil, err
    }
    
    return &NATSPublisher{nc: nc, js: js}, nil
}

func (np *NATSPublisher) PublishEvent(event *domain.UnifiedEvent) error {
    subject := np.buildSubject(event)
    data, err := json.Marshal(event)
    if err != nil {
        return err
    }
    
    _, err = np.js.Publish(subject, data)
    return err
}

func (np *NATSPublisher) buildSubject(event *domain.UnifiedEvent) string {
    switch event.Source {
    case "k8s-collector":
        namespace := "default"
        if ns, ok := event.Context["namespace"]; ok {
            namespace = ns.(string)
        }
        return fmt.Sprintf("tapio.events.k8s.%s.%s", namespace, event.Type)
        
    case "systemd-collector":
        service := "unknown"
        if svc, ok := event.Data["service"]; ok {
            service = svc.(string)
        }
        return fmt.Sprintf("tapio.events.systemd.%s.%s", service, event.Type)
        
    case "ebpf-collector":
        return fmt.Sprintf("tapio.events.ebpf.%s", event.Type)
        
    default:
        return fmt.Sprintf("tapio.events.%s.%s", event.Source, event.Type)
    }
}

func (np *NATSPublisher) Close() {
    if np.nc != nil {
        np.nc.Close()
    }
}
```

### 3. Update Existing Collectors (One-line change!)

```go
// In each collector (k8s, systemd, ebpf)
// Replace the pipeline client with NATS publisher

// OLD:
// client := grpc.NewPipelineClient(conn)
// client.ProcessEvent(event)

// NEW:
publisher, _ := common.NewNATSPublisher("nats://tapio-nats:4222")
publisher.PublishEvent(event)
```

### 4. Update Pipeline to Consume from NATS

```go
// pkg/intelligence/pipeline/nats_consumer.go
package pipeline

import (
    "encoding/json"
    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/domain"
)

type NATSConsumer struct {
    nc       *nats.Conn
    js       nats.JetStreamContext
    pipeline IntelligencePipeline
}

func NewNATSConsumer(url string, pipeline IntelligencePipeline) (*NATSConsumer, error) {
    nc, err := nats.Connect(url)
    if err != nil {
        return nil, err
    }
    
    js, err := nc.JetStream()
    if err != nil {
        return nil, err
    }
    
    consumer := &NATSConsumer{
        nc:       nc,
        js:       js,
        pipeline: pipeline,
    }
    
    // Subscribe to all Tapio events
    js.Subscribe("tapio.events.>", consumer.handleEvent)
    
    return consumer, nil
}

func (nc *NATSConsumer) handleEvent(msg *nats.Msg) {
    event := &domain.UnifiedEvent{}
    if err := json.Unmarshal(msg.Data, event); err != nil {
        msg.Nak() // Negative acknowledgment
        return
    }
    
    // Process through existing pipeline
    if err := nc.pipeline.ProcessEvent(event); err != nil {
        msg.Nak()
        return
    }
    
    msg.Ack() // Acknowledge successful processing
}
```

### 5. Docker Compose for Local Development

```yaml
# docker-compose.nats.yml
version: '3.8'
services:
  nats:
    image: nats:2.10-alpine
    ports:
      - "4222:4222"  # Client connections
      - "8222:8222"  # HTTP monitoring
      - "6222:6222"  # Cluster routes
    command:
      - "--jetstream"
      - "--store_dir=/data/jetstream"
      - "--http_port=8222"
    volumes:
      - nats_data:/data
    environment:
      - NATS_CLIENT_ADVERTISE=nats:4222

  # Your existing Tapio services can now connect to nats:4222
  
volumes:
  nats_data:
```

### 6. Kubernetes Deployment Updates

```yaml
# Update collector DaemonSets
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
        - name: LOG_LEVEL
          value: "info"
---
# Update pipeline deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tapio-pipeline
spec:
  template:
    spec:
      containers:
      - name: pipeline
        image: tapio/pipeline:latest
        env:
        - name: NATS_URL
          value: "nats://tapio-nats:4222"
        - name: CONSUME_SUBJECTS
          value: "tapio.events.>"
```

## Testing the Implementation

### 1. Local Testing Script

```bash
#!/bin/bash
# test_nats_integration.sh

echo "🚀 Starting NATS for Tapio..."
docker-compose -f docker-compose.nats.yml up -d

echo "⏳ Waiting for NATS to be ready..."
sleep 5

echo "📊 NATS Status:"
curl -s http://localhost:8222/varz | jq '.connections'

echo "🧪 Testing event publishing..."
# Use NATS CLI to test
docker run --rm --network host natsio/nats-box:latest \
  nats pub tapio.events.test '{"id":"test-123","type":"test","source":"manual","timestamp":"2024-01-01T00:00:00Z"}'

echo "👂 Listening for events..."
docker run --rm --network host natsio/nats-box:latest \
  nats sub "tapio.events.>" --count=5

echo "✅ NATS integration test complete!"
```

### 2. Performance Testing

```go
// cmd/nats-perf-test/main.go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "time"
    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/domain"
)

func main() {
    nc, _ := nats.Connect("nats://localhost:4222")
    js, _ := nc.JetStream()
    
    // Test high-volume publishing (simulate eBPF collector)
    start := time.Now()
    eventCount := 100000
    
    for i := 0; i < eventCount; i++ {
        event := &domain.UnifiedEvent{
            ID:        fmt.Sprintf("perf-test-%d", i),
            Type:      "network_packet",
            Source:    "ebpf-collector",
            Timestamp: time.Now(),
            Data: map[string]interface{}{
                "src_ip": "192.168.1.100",
                "dst_ip": "192.168.1.200",
                "bytes":  1024,
            },
        }
        
        data, _ := json.Marshal(event)
        js.Publish("tapio.events.ebpf.network", data)
    }
    
    duration := time.Since(start)
    rate := float64(eventCount) / duration.Seconds()
    
    fmt.Printf("📈 Published %d events in %v\n", eventCount, duration)
    fmt.Printf("🚀 Rate: %.0f events/second\n", rate)
}
```

## Migration Checklist

### Pre-Migration
- [ ] Deploy NATS cluster in K8s
- [ ] Test NATS connectivity from pods
- [ ] Create NATS subjects and streams
- [ ] Update collector images with NATS support

### Migration (Rolling)
- [ ] Update K8s collector DaemonSet (rolling update)
- [ ] Update systemd collector DaemonSet
- [ ] Update eBPF collector DaemonSet  
- [ ] Update pipeline deployment
- [ ] Verify event flow through NATS

### Post-Migration
- [ ] Monitor NATS performance and health
- [ ] Set up NATS monitoring in Grafana
- [ ] Test event replay capabilities
- [ ] Document operational procedures

### Rollback Plan
- [ ] Keep old direct-connection code available
- [ ] Switch back via environment variable
- [ ] Monitor for event loss during rollback

## NATS Monitoring & Ops

### Health Checks
```bash
# Check NATS cluster health
kubectl get natsclusters -n tapio

# Check JetStream status
kubectl exec -it tapio-nats-0 -n tapio -- nats server info

# Monitor event throughput
kubectl exec -it tapio-nats-0 -n tapio -- nats stream info
```

### Troubleshooting Common Issues

1. **Connection Refused**
   ```bash
   # Check service DNS
   kubectl exec -it <collector-pod> -- nslookup tapio-nats
   
   # Test connectivity
   kubectl exec -it <collector-pod> -- nc -zv tapio-nats 4222
   ```

2. **High Memory Usage**
   ```bash
   # Check JetStream memory
   kubectl exec -it tapio-nats-0 -- nats server info
   
   # Adjust limits in NatsCluster config
   kubectl edit natscluster tapio-nats
   ```

3. **Message Loss**
   ```bash
   # Check JetStream config
   kubectl exec -it tapio-nats-0 -- nats stream ls
   
   # Verify consumer lag
   kubectl exec -it tapio-nats-0 -- nats consumer info
   ```

## Benefits Summary

### Immediate Benefits
✅ **Decoupling**: Collectors independent from pipeline changes  
✅ **Reliability**: Message persistence and replay  
✅ **Scalability**: Easy horizontal scaling  
✅ **Multiple Consumers**: SIEM, debugging, monitoring  

### Future Benefits  
🚀 **Correlation Engine Evolution**: Easy to swap/upgrade correlation without collector changes  
🚀 **Event Replay**: Test correlation accuracy with historical events  
🚀 **Multi-Region**: Stream events across regions/clusters  
🚀 **A/B Testing**: Run multiple correlation engines simultaneously  

This architecture makes Tapio bulletproof for your correlation engine overhaul while keeping deployment simple!