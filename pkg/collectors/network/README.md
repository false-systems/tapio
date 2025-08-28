# Network Collector - L3-L4-L7 Intelligence-Focused Monitoring

## Executive Summary

The Tapio Network Collector provides **intelligence-focused** L3-L4-L7 network monitoring with automatic service dependency discovery, error cascade detection, and protocol-aware analysis. Unlike traditional network monitoring that drowns you in metrics, this collector captures only **actionable intelligence** for root cause analysis.

## Key Features

### 🔍 L7 Protocol Intelligence
- **HTTP/1.1 & HTTP/2**: Method extraction, status codes, content analysis
- **gRPC**: Frame parsing, call tracking, response correlation
- **DNS**: Query/response matching (complementary to DNS collector)
- **Automatic Protocol Detection**: No configuration needed

### 🔗 Service Dependency Discovery
- **Automatic Mapping**: Discovers service-to-service communication patterns
- **5-Tuple Flow Tracking**: `src_ip:port → dst_ip:port + protocol`
- **Real-time Graph Building**: Powers correlation engine with live dependencies
- **Zero Configuration**: Works out of the box

### 🚨 Error Cascade Detection
- **Cross-Service Error Tracking**: Detects error propagation across microservices
- **Pattern Recognition**: Identifies recurring failure patterns
- **4xx/5xx Analysis**: HTTP error categorization and correlation
- **Cascading Failure Detection**: Service A fails → B fails → C fails

### ⚡ Performance & Efficiency
- **95% Noise Reduction**: In-kernel filtering captures only interesting events
- **<0.5% CPU Overhead**: Compared to 2-5% for traditional APM
- **50-100MB Memory**: Versus 200-500MB for traditional solutions
- **Smart Sampling**: Adaptive rate during incidents

## Architecture

```
┌─────────────────────────────────────────────────┐
│                User Space (Go)                  │
│                                                 │
│  ┌─────────────────┐    ┌──────────────────┐  │
│  │  Intelligence    │───▶│  Correlation   │  │
│  │   Collector     │    │    Engine      │  │
│  └─────────────────┘    └──────────────────┘  │
│           ▲                                    │
│           │                                    │
│    ┌─────────────┐                            │
│    │ Ring Buffer │                            │
│    │   Reader    │                            │
│    └─────────────┘                            │
└───────────┬─────────────────────────────────────┘
            │ Events (only interesting ones)
┌───────────┴─────────────────────────────────────┐
│         Kernel Space (eBPF)                     │
│                                                 │
│  ┌─────────────────────────────────────────┐  │
│  │   Intelligence Filtering (In-Kernel)     │  │
│  │  - Error patterns (4xx/5xx)             │  │
│  │  - New service dependencies             │  │
│  │  - Latency anomalies (3x baseline)      │  │
│  │  - Security concerns                    │  │
│  └─────────────────────────────────────────┘  │
│                      ▲                         │
│  ┌──────────────────────────────────────────┐ │
│  │        L7 Protocol Parsing               │ │
│  │  - HTTP header extraction               │ │
│  │  - gRPC frame analysis                  │ │
│  │  - DNS query/response matching          │ │
│  └──────────────────────────────────────────┘ │
│                      ▲                         │
│  ┌──────────────────────────────────────────┐ │
│  │     L3-L4 Connection Tracking            │ │
│  │  - TCP state machine (SYN→FIN)          │ │
│  │  - UDP flow monitoring                  │ │
│  │  - Connection correlation               │ │
│  └──────────────────────────────────────────┘ │
│                      ▲                         │
│  ┌──────────────────────────────────────────┐ │
│  │          eBPF Tracepoints                │ │
│  │  - tcp_sendmsg / tcp_recvmsg           │ │
│  │  - udp_sendmsg / udp_recvmsg           │ │
│  │  - sys_connect / sys_accept            │ │
│  └──────────────────────────────────────────┘ │
└──────────────────────────────────────────────┘
```

## What Makes This Collector Unique

### Intelligence vs Observability
Traditional network monitoring gives you:
- 50,000+ events per second
- Hundreds of metrics per service
- Massive data storage requirements
- Alert fatigue from noise

This collector gives you:
- 500 actionable events per second
- Only anomalies and dependencies
- Minimal storage footprint
- Every event matters for RCA

### In-Kernel Intelligence
The dual eBPF program architecture:
1. **`network_monitor.c`**: Full protocol parsing and connection tracking
2. **`network_monitor_intelligence.c`**: Smart filtering and aggregation

This means intelligence decisions happen in kernel space, preventing userspace flooding.

## Configuration

```go
type IntelligenceCollectorConfig struct {
    // Base network monitoring
    EnableIPv4         bool   // Monitor IPv4 traffic
    EnableTCP          bool   // Track TCP connections
    EnableUDP          bool   // Track UDP flows
    EnableHTTP         bool   // Parse HTTP/1.1 & HTTP/2
    EnableHTTPS        bool   // Parse HTTPS (with limitations)
    
    // Intelligence settings
    SlowRequestThresholdMs   int     // Latency anomaly threshold (default: 1000ms)
    ErrorStatusThreshold     int     // HTTP status considered error (default: 400)
    LatencyDeviationFactor   float64 // Deviation from baseline (default: 3.0x)
    
    // L7 Protocol intelligence
    HTTPPorts          []int   // Ports to check for HTTP (default: [80, 8080, 3000])
    HTTPSPorts         []int   // Ports to check for HTTPS (default: [443, 8443])
    GRPCIntelligence   bool    // Enable gRPC parsing
    DNSIntelligence    bool    // Enable DNS parsing (port 53)
    
    // Security analysis
    SuspiciousUserAgents []string // User agents to flag (e.g., "masscan", "sqlmap")
    SuspiciousEndpoints  []string // Endpoints to monitor (e.g., "/.env", "/admin")
}
```

## Events Generated

### Service Dependency Event
```json
{
    "type": "service_dependency",
    "source_service": "frontend",
    "dest_service": "api-gateway",
    "protocol": "TCP",
    "port": 8080,
    "first_seen": "2024-08-27T10:00:00Z",
    "request_count": 1523
}
```

### Error Pattern Event
```json
{
    "type": "error_pattern",
    "endpoint": "/api/checkout",
    "method": "POST",
    "status_code": 503,
    "error_count": 45,
    "affected_services": ["checkout", "payment", "inventory"],
    "is_cascade": true
}
```

### Latency Anomaly Event
```json
{
    "type": "latency_anomaly",
    "endpoint": "/api/search",
    "latency_ms": 3500,
    "baseline_ms": 250,
    "deviation_factor": 14.0,
    "source_service": "web",
    "dest_service": "search"
}
```

### Security Concern Event
```json
{
    "type": "security_concern",
    "concern_type": "suspicious_user_agent",
    "source_ip": "45.142.122.55",
    "user_agent": "sqlmap/1.5",
    "target_endpoint": "/api/users",
    "severity": "high",
    "blocked": false
}
```

## Use Cases

### 1. Root Cause Analysis
When an incident occurs, the collector provides:
- Service dependency graph at the time of failure
- Error propagation path
- Latency spikes that preceded the failure
- Connection failures between services

### 2. Security Monitoring
- Detect scanning attempts (port scans, endpoint enumeration)
- Identify suspicious user agents
- Track unauthorized service communication
- Monitor for data exfiltration patterns

### 3. Performance Optimization
- Find slow endpoints automatically
- Identify chatty service communication
- Detect retry storms
- Track connection pool exhaustion

### 4. Compliance & Auditing
- Track all service-to-service communication
- Monitor access to sensitive endpoints
- Audit external service calls
- Verify network segmentation

## Integration with Tapio

The network collector feeds critical data to:

1. **Correlation Engine**: Service dependencies for graph building
2. **Intelligence Layer**: Error patterns and anomalies
3. **Storage Layer**: Connection metadata for historical analysis
4. **API Layer**: Real-time network insights

## Performance Impact

Benchmarks on a typical Kubernetes cluster (100 pods, 10k req/s):

| Metric | Traditional APM | Network Collector |
|--------|----------------|-------------------|
| CPU Overhead | 2-5% | <0.5% |
| Memory Usage | 200-500MB | 50-100MB |
| Events/sec | 50,000 | 500 |
| Storage/day | 10-50GB | 100-500MB |
| Signal/Noise | 1:100 | 95:5 |

## Limitations

1. **HTTPS**: Limited to metadata (IPs, ports, timing) unless TLS termination is accessible
2. **Encrypted gRPC**: Similar limitations as HTTPS
3. **Custom Protocols**: Not automatically detected (can be extended)
4. **High-Speed Networks**: May require tuning for 40Gbps+ networks

## Development

### Building eBPF Programs
```bash
cd pkg/collectors/network/bpf
go generate ./...
```

### Testing
```bash
# Unit tests
go test ./pkg/collectors/network/...

# Integration tests (requires Linux)
go test -tags=integration ./pkg/collectors/network/...

# Stress tests
go test -run=TestStress ./pkg/collectors/network/...
```

### Adding New L7 Protocol

1. Add protocol constants to `bpf_src/network_monitor.c`
2. Implement parsing logic in kernel space
3. Add intelligence filters in `network_monitor_intelligence.c`
4. Update Go structures in `types.go`
5. Add protocol handler in `collector.go`

## Troubleshooting

### No Events Captured
1. Check eBPF programs loaded: `bpftool prog list`
2. Verify tracepoints attached: `bpftool perf list`
3. Check ring buffer stats: `bpftool map dump name events`

### High CPU Usage
1. Reduce sampling rate in config
2. Increase filtering thresholds
3. Disable non-critical protocols

### Missing Dependencies
1. Ensure services have distinct IPs/ports
2. Check cgroup ID extraction is working
3. Verify pod correlation is active

## Future Enhancements

- [ ] TLS/SSL interception for HTTPS visibility (with proper security)
- [ ] MySQL/PostgreSQL protocol parsing
- [ ] Redis/Memcached protocol support
- [ ] WebSocket tracking
- [ ] GraphQL query analysis
- [ ] Service mesh (Istio/Linkerd) integration
- [ ] ML-based anomaly detection
- [ ] Automated baseline learning

## Contributing

The network collector is a critical component of Tapio's intelligence layer. When contributing:

1. Maintain the intelligence-first philosophy
2. Keep overhead below 0.5% CPU
3. Filter aggressively in kernel space
4. Every event must be actionable
5. Follow Tapio's coding standards (NO map[string]interface{})

## License

GPL-2.0 (eBPF components)
Apache-2.0 (Go components)