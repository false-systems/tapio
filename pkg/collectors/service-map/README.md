# Service Map Collector

A real-time service discovery and dependency mapping collector for Kubernetes environments. This collector provides a live map of your services and their interactions without the complexity of a service mesh.

## Overview

The service-map collector automatically discovers services in your Kubernetes cluster and tracks their real-time connections using eBPF (on Linux). It answers critical questions like:
- What services are running in my cluster?
- Who is calling who?
- What databases does my API connect to?
- Are there any unexpected service dependencies?

## Features

### 🔍 Service Discovery
- **Automatic Kubernetes discovery** - Watches Services, Endpoints, and Pods
- **Multi-namespace support** - Configure which namespaces to monitor
- **Service type detection** - Automatically identifies databases, caches, queues, and APIs
- **Version tracking** - Tracks service versions from labels

### 🔗 Connection Tracking
- **eBPF-powered** - Zero-overhead kernel-level connection monitoring (Linux)
- **TCP & UDP support** - Tracks both protocols
- **Bidirectional mapping** - Knows both dependencies and dependents
- **Real-time updates** - Connection changes detected immediately

### 🎯 Smart Detection
- **Port-based detection** - Common ports mapped to service types (MySQL=3306, Redis=6379, etc.)
- **Image-based detection** - Identifies service types from container images
- **Label-based detection** - Uses Kubernetes labels and annotations
- **Health monitoring** - Tracks service health from endpoint readiness

## Architecture

```
┌─────────────────────────────────────┐
│         Service Map Collector        │
├─────────────────────────────────────┤
│  Kubernetes Discovery │ eBPF Tracker │
├─────────────────────────────────────┤
│         Base Collector               │
│  (Stats, Health, Metrics, Events)    │
└─────────────────────────────────────┘
```

## Configuration

```yaml
collectors:
  - name: service-map
    enabled: true
    
    # Kubernetes discovery
    enable_k8s_discovery: true
    namespaces: []  # Empty = all namespaces
    exclude_namespaces:
      - kube-system
      - kube-public
      - kube-node-lease
    
    # eBPF connection tracking
    enable_ebpf: true
    max_connections: 100000
    connection_ttl: 5m
    
    # Service detection
    auto_detect_type: true
    
    # Filtering
    ignore_system_namespaces: true
    include_external_services: false
    min_connection_count: 1
```

## Service Types

The collector automatically detects and categorizes services:

| Type | Detection | Examples |
|------|-----------|----------|
| **Database** | Ports: 3306, 5432, 27017 | MySQL, PostgreSQL, MongoDB |
| **Cache** | Ports: 6379, 11211 | Redis, Memcached |
| **Queue** | Ports: 5672, 9092, 4222 | RabbitMQ, Kafka, NATS |
| **API** | Ports: 8080, 3000, 8000 | REST APIs, GraphQL |
| **Proxy** | Ports: 80, 443 | Nginx, Envoy, HAProxy |

## Smart Event Emission

The collector uses an intelligent event-driven emission strategy instead of naive periodic updates:

### Immediate Emission Triggers
- **Service added/removed** - New service discovered or existing service deleted
- **New dependency detected** - First time service A calls service B  
- **Service health critical** - Service goes down
- **Version change** - Deployment detected
- **Significant topology change** - Major shift in dependencies

### Debounced Changes (5 seconds)
- Connection count updates
- Request rate changes
- Minor health fluctuations
- Latency variations

### Emission Control
```yaml
# Smart emission configuration
emit_on_change: true           # Emit immediately on significant changes
change_debounce: 5s            # Batch rapid changes
full_snapshot_interval: 5m     # Periodic consistency snapshot
skip_unchanged: true           # Don't emit if nothing changed
min_emit_interval: 5s          # Rate limiting
```

## Events

The collector emits structured events efficiently based on actual changes:

### Service Map Event
```json
{
  "event_type": "service_map",
  "timestamp": "2024-01-10T10:00:30Z",
  "data": {
    "services": {
      "production/payment-service": {
        "type": "api",
        "version": "v2.1.0",
        "health": "healthy",
        "dependencies": ["postgres", "redis", "kafka"],
        "dependents": ["frontend", "order-service"]
      }
    },
    "connections": {
      "payment-service->postgres": 150,
      "payment-service->redis": 500
    }
  }
}
```

### Connection Event (Debug Level)
```json
{
  "event_type": "network_connection",
  "timestamp": "2024-01-10T10:00:30Z",
  "data": {
    "source_ip": "10.0.1.5",
    "dest_ip": "10.0.2.10",
    "source_port": 45678,
    "dest_port": 5432,
    "protocol": "TCP",
    "process": "payment-api",
    "pid": 1234
  }
}
```

## Platform Support

| Platform | Service Discovery | Connection Tracking |
|----------|------------------|---------------------|
| **Linux** | ✅ Full | ✅ eBPF-powered |
| **macOS** | ✅ Full | ⚠️ K8s only (no eBPF) |
| **Windows** | ❌ Not supported | ❌ Not supported |

## Performance

- **Lightweight**: ~10MB memory overhead
- **Efficient**: Uses eBPF ring buffers for zero-copy event streaming
- **Scalable**: Handles 100,000+ concurrent connections
- **Real-time**: Sub-second detection of new connections

## How It Works

1. **Service Discovery Phase**
   - Watches Kubernetes API for Services and Endpoints
   - Maps service IPs to service names
   - Detects service types from ports/images/labels

2. **Connection Tracking Phase** (Linux only)
   - eBPF programs attached to kernel TCP/UDP functions
   - Tracks connect/accept/close events
   - Records bytes transferred per connection

3. **Correlation Phase**
   - Maps IP addresses to service names
   - Builds dependency graph
   - Calculates call rates and patterns

4. **Event Emission**
   - Emits service map every 30 seconds
   - Sends individual connection events (debug mode)
   - Updates health status continuously

## Comparison with Service Meshes

| Feature | Service Map Collector | Istio/Linkerd |
|---------|----------------------|---------------|
| **Observability** | ✅ Read-only | ✅ Full |
| **Traffic Control** | ❌ None | ✅ Routing, retry, etc. |
| **Security** | ❌ None | ✅ mTLS, policies |
| **Overhead** | ~10MB | ~500MB per pod |
| **Complexity** | Low | High |
| **Setup Time** | < 1 minute | Hours/days |

**Choose service-map when you want visibility without control.**

## Troubleshooting

### No services discovered
- Check if collector has RBAC permissions to list Services/Endpoints
- Verify namespace configuration
- Check collector logs for Kubernetes API errors

### No connections tracked (Linux)
- Verify eBPF is enabled: `enable_ebpf: true`
- Check kernel version (4.14+ required)
- Look for eBPF loading errors in logs
- Ensure collector runs with sufficient privileges

### High memory usage
- Reduce `max_connections` limit
- Decrease `connection_ttl` to clean up faster
- Enable `min_connection_count` filtering

## Development

### Running Tests
```bash
go test ./pkg/collectors/service-map/...
```

### Generating eBPF Programs
```bash
cd pkg/collectors/service-map/bpf
go generate
```

### Building for Linux (with eBPF)
```bash
GOOS=linux GOARCH=amd64 go build ./...
```

### Local Development (macOS)
The collector works on macOS for development but without eBPF connection tracking. It will still discover services via Kubernetes API.

## Integration

The service-map collector integrates seamlessly with:
- **Prometheus** - Exports metrics via OpenTelemetry
- **Grafana** - Visualize service dependencies
- **Neo4j** - Store service graph for analysis
- **Tapio Intelligence** - Correlation with other events

## Future Enhancements

- [ ] HTTP path extraction for API endpoint mapping
- [ ] gRPC method detection
- [ ] Database query pattern analysis
- [ ] Distributed tracing correlation
- [ ] Service mesh detection and integration
- [ ] GraphQL schema discovery

## License

Part of the Tapio observability platform.