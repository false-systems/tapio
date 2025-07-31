# CNI Network Policy Monitoring Enhancement

## Overview

The CNI collector has been enhanced with eBPF-based network policy monitoring to provide deep visibility into Kubernetes network policy enforcement across different CNI plugins.

## Key Features

### 1. **Ring Buffer Performance**
- Uses eBPF ring buffer (not perf events) for efficient event streaming
- 256KB ring buffer for high-throughput policy decisions
- Zero-copy event delivery from kernel to userspace

### 2. **CO-RE (Compile Once, Run Everywhere)**
- Uses BTF and CO-RE for kernel version independence
- Works across different kernel versions without recompilation
- Leverages `bpf_core_read` for safe kernel structure access

### 3. **CNI-Specific Monitoring**

#### Calico
- Monitors TC (Traffic Control) hooks for policy enforcement
- Tracks iptables/netfilter decisions
- Captures Felix policy updates

#### Cilium
- XDP hook monitoring for high-performance policies
- Identity-based policy tracking
- eBPF map visibility for Cilium's policy cache

#### Flannel
- iptables rule monitoring
- Basic allow/drop tracking
- Integration with kube-proxy rules

### 4. **Policy Event Tracking**
- Packet allow/drop decisions with full context
- Source/destination IP and ports
- Pod name and namespace correlation
- Policy rule that matched (or didn't match)
- Direction (ingress/egress)

## Architecture

```
┌─────────────────────┐
│   K8s Network       │
│     Policies        │
└──────────┬──────────┘
           │
┌──────────▼──────────┐
│   CNI Plugin        │
│ (Calico/Cilium/etc) │
└──────────┬──────────┘
           │
┌──────────▼──────────┐     ┌─────────────┐
│   eBPF Programs     │────▶│ Ring Buffer │
│ - TC hooks          │     │   256KB     │
│ - XDP hooks         │     └──────┬──────┘
│ - Kprobes           │            │
└─────────────────────┘            │
                                   ▼
                          ┌─────────────────┐
                          │ CNI Collector   │
                          │ (User Space)    │
                          └─────────────────┘
```

## Usage

### Basic Collection
```go
// Collector auto-detects CNI and starts basic monitoring
collector, _ := cni.NewCollector(config)
collector.Start(ctx)
```

### Enhanced Network Policy Monitoring
```go
// On Linux, enhance with network policy tracking
if runtime.GOOS == "linux" {
    err := collector.EnhanceWithNetworkPolicy()
    if err == nil {
        // Now tracking policy decisions
    }
}
```

## Event Format

Policy events include:
```json
{
  "timestamp": "2024-01-30T10:00:00Z",
  "source_ip": "10.0.1.5",
  "dest_ip": "10.0.2.10",
  "source_port": 45678,
  "dest_port": 80,
  "protocol": "TCP",
  "action": "allow",
  "direction": "ingress",
  "pod_name": "frontend-abc123",
  "namespace": "production",
  "policy_name": "allow-frontend-to-backend",
  "cni_plugin": "calico"
}
```

## Metrics

The enhanced collector provides metrics:
- `PacketsAllowed`: Total packets allowed by policies
- `PacketsDropped`: Total packets dropped by policies
- `PolicyMatches`: Packets that matched a policy rule
- `PolicyMisses`: Packets with no matching policy

## Implementation Details

### eBPF Programs

1. **TC Ingress/Egress** - For CNIs using Traffic Control (Calico)
2. **XDP** - For high-performance CNIs (Cilium)
3. **Kprobes** - For netfilter/iptables monitoring (generic)

### Maps

1. **policy_events** - Ring buffer for events
2. **active_policies** - Policy rules cache
3. **pod_metadata_map** - IP to pod/namespace mapping

### Safety

- All kernel reads use `bpf_probe_read_kernel`
- Bounded loops and stack usage
- Proper bounds checking for packet parsing

## Future Enhancements

1. **Service Mesh Integration** - Track Envoy/Istio policies
2. **DNS Policy** - Monitor DNS-based policies
3. **Rate Limiting** - Detect and report rate limit violations
4. **ML Integration** - Anomaly detection on traffic patterns