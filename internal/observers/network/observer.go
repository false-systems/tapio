//go:build linux
// +build linux

package network

import (
	"fmt"
	"net"

	"github.com/cilium/ebpf"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
)

// PodInfo matches internal/services/k8scontext/types.go:PodInfo
// Redefined here to avoid circular import (observers should not import services)
type PodInfo struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	PodIP          string            `json:"pod_ip"`
	HostIP         string            `json:"host_ip"`
	Labels         map[string]string `json:"labels"`
	OTELAttributes map[string]string `json:"otel_attributes,omitempty"`
}

// K8sContextGetter provides K8s pod metadata lookup by IP
// Implemented by internal/services/k8scontext.Service
type K8sContextGetter interface {
	GetPodByIP(ip string) (*PodInfo, error)
}

// Config holds network observer configuration
type Config struct {
	EventChannelSize int // Ring buffer → processor channel size (default: 1000)

	// K8s context service (optional - nil in OSS mode)
	// When provided, enables pod context enrichment with pre-computed OTEL attributes
	K8sContextService K8sContextGetter
}

// NetworkObserver tracks TCP/UDP/DNS network events using eBPF
type NetworkObserver struct {
	*base.BaseObserver
	name    string     // Explicit name for lean pattern
	deps    *base.Deps // Injected dependencies
	config  Config
	ebpfMgr *base.EBPFManager // eBPF lifecycle manager

	// eBPF map references (nil when eBPF not loaded)
	connStatsMap *ebpf.Map // conn_stats LRU map from eBPF

	// Network-specific Prometheus metrics (native, zero-allocation)
	connectionResets  *prometheus.Counter // connection_resets_total
	synTimeouts       *prometheus.Counter // syn_timeouts_total
	connectionRefused *prometheus.Counter // connection_refused_total

	// Packet loss metrics
	retransmitsTotal *prometheus.Counter // retransmits_total
	retransmitRate   *prometheus.Gauge   // retransmit_rate_ratio (0.0-1.0)
	congestionEvents *prometheus.Counter // congestion_events_total

	// RTT spike metrics (Stage 3)
	rttSpikesTotal *prometheus.Counter // rtt_spikes_total
	rttCurrentMs   *prometheus.Gauge   // rtt_current_ms

	rttDegradationPct *prometheus.Gauge // rtt_degradation_percent

	// eBPF health metrics
	ringbufferUtilization *prometheus.Gauge // ringbuffer_utilization_percent
	ebpfMapSize           *prometheus.Gauge // ebpf_map_size_entries
}

// NewNetworkObserver creates a new network observer
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	// Create deps for lean pattern compatibility
	emitter, err := intelligence.New(intelligence.Config{Tier: intelligence.TierDebug})
	if err != nil {
		return nil, fmt.Errorf("failed to create emitter: %w", err)
	}
	deps := base.NewDeps(base.GlobalRegistry, emitter)

	obs := &NetworkObserver{
		BaseObserver: baseObs,
		name:         name,
		deps:         deps,
		config:       config,
	}

	// Create network-specific Prometheus metrics using fluent API
	err = base.NewPromMetricBuilder(base.GlobalRegistry, name).
		Counter(&obs.connectionResets, "connection_resets_total", "TCP connection resets (RST) received").
		Counter(&obs.synTimeouts, "syn_timeouts_total", "TCP SYN timeouts (no response)").
		Counter(&obs.connectionRefused, "connection_refused_total", "TCP connections refused (RST on SYN)").
		Counter(&obs.retransmitsTotal, "retransmits_total", "TCP packet retransmissions detected").
		Gauge(&obs.retransmitRate, "retransmit_rate_ratio", "TCP retransmission rate ratio (0.0-1.0)").
		Counter(&obs.congestionEvents, "congestion_events_total", "High retransmit rate events (>5%)").
		Counter(&obs.rttSpikesTotal, "rtt_spikes_total", "RTT spike events detected").
		Gauge(&obs.rttCurrentMs, "rtt_current_ms", "Current RTT in milliseconds when spike detected").
		Gauge(&obs.rttDegradationPct, "rtt_degradation_ratio", "RTT degradation ratio from baseline (0.0-1.0)").
		Gauge(&obs.ringbufferUtilization, "ringbuffer_utilization_percent", "eBPF ring buffer utilization percentage").
		Gauge(&obs.ebpfMapSize, "ebpf_map_size_entries", "Number of entries in eBPF maps (baseline_rtt)").
		Build()

	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	return obs, nil
}

// New creates a network observer with dependency injection (lean pattern).
// This replaces NewNetworkObserver for the new architecture.
func New(config Config, deps *base.Deps) *NetworkObserver {
	obs := &NetworkObserver{
		name:   "network",
		deps:   deps,
		config: config,
	}

	// Create observer-specific Prometheus metrics
	builder := base.NewPromMetricBuilder(base.GlobalRegistry, "network")
	builder.Counter(&obs.connectionResets, "connection_resets_total", "TCP connection resets (RST) received")
	builder.Counter(&obs.synTimeouts, "syn_timeouts_total", "TCP SYN timeouts (no response)")
	builder.Counter(&obs.connectionRefused, "connection_refused_total", "TCP connections refused (RST on SYN)")
	builder.Counter(&obs.retransmitsTotal, "retransmits_total", "TCP packet retransmissions detected")
	builder.Gauge(&obs.retransmitRate, "retransmit_rate_ratio", "TCP retransmission rate ratio (0.0-1.0)")
	builder.Counter(&obs.congestionEvents, "congestion_events_total", "High retransmit rate events (>5%)")
	builder.Counter(&obs.rttSpikesTotal, "rtt_spikes_total", "RTT spike events detected")
	builder.Gauge(&obs.rttCurrentMs, "rtt_current_ms", "Current RTT in milliseconds when spike detected")
	builder.Gauge(&obs.rttDegradationPct, "rtt_degradation_ratio", "RTT degradation ratio from baseline (0.0-1.0)")
	builder.Gauge(&obs.ringbufferUtilization, "ringbuffer_utilization_percent", "eBPF ring buffer utilization percentage")
	builder.Gauge(&obs.ebpfMapSize, "ebpf_map_size_entries", "Number of entries in eBPF maps (baseline_rtt)")
	//nolint:errcheck // metrics registration errors are non-fatal for observer operation
	builder.Build()

	return obs
}

// stateToEventType maps TCP state transitions to domain event types
// Takes connection key to check if RST was received (to distinguish refused vs timeout)
// For tests: pass empty string for connKey and nil for observer
func stateToEventType(oldState, newState uint8, connKey string, observer *NetworkObserver) string {
	switch newState {
	case TCP_ESTABLISHED:
		return "connection_established"
	case TCP_LISTEN:
		if oldState == TCP_CLOSE {
			return "listen_started"
		}
	case TCP_CLOSE:
		if oldState == TCP_LISTEN {
			return "listen_stopped"
		}
		// SYN_SENT → CLOSE: Check if RST was received
		if oldState == TCP_SYN_SENT {
			// Check eBPF LRU map for RST flag (only if observer and map available)
			if observer != nil && observer.connStatsMap != nil && connKey != "" {
				// Parse connKey to create eBPF key
				var srcIP, dstIP string
				var srcPort, dstPort uint16
				n, err := fmt.Sscanf(connKey, "%15[^:]:%d:%15[^:]:%d", &srcIP, &srcPort, &dstIP, &dstPort)
				if err != nil || n != 4 {
					// Failed to parse connKey, treat as timeout (default behavior)
					return "connection_syn_timeout"
				}

				// Convert to eBPF key format
				key := ebpfConnKey{
					SrcAddr: ipv4StringToUint32(srcIP),
					DstAddr: ipv4StringToUint32(dstIP),
					SrcPort: srcPort,
					DstPort: dstPort,
				}

				var stats ebpfRetransmitStats
				if err := observer.connStatsMap.Lookup(&key, &stats); err == nil {
					if stats.RSTReceived != 0 {
						return "connection_refused" // RST received = connection refused
					}
				}
			}
			return "connection_syn_timeout" // No RST or no observer = timeout (default)
		}
		return "connection_closed"
	}
	return "tcp_state_change"
}

// convertIPv4 converts uint32 IPv4 (little-endian) to string
func convertIPv4(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}

// ipv4StringToUint32 converts IP string to uint32 (little-endian)
func ipv4StringToUint32(ipStr string) uint32 {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0
	}
	ip = ip.To4() // Convert to IPv4
	if ip == nil {
		return 0
	}
	// Convert to little-endian uint32 (match eBPF format)
	return uint32(ip[0]) | uint32(ip[1])<<8 | uint32(ip[2])<<16 | uint32(ip[3])<<24
}

// convertIPv6 converts [16]byte IPv6 to string
func convertIPv6(ip [16]byte) string {
	return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
		uint16(ip[0])<<8|uint16(ip[1]),
		uint16(ip[2])<<8|uint16(ip[3]),
		uint16(ip[4])<<8|uint16(ip[5]),
		uint16(ip[6])<<8|uint16(ip[7]),
		uint16(ip[8])<<8|uint16(ip[9]),
		uint16(ip[10])<<8|uint16(ip[11]),
		uint16(ip[12])<<8|uint16(ip[13]),
		uint16(ip[14])<<8|uint16(ip[15]))
}

// extractComm extracts null-terminated process name
func extractComm(comm [16]byte) string {
	for i, b := range comm {
		if b == 0 {
			return string(comm[:i])
		}
	}
	return string(comm[:])
}

// ebpfConnKey matches the eBPF conn_key struct (IPv4 only)
type ebpfConnKey struct {
	SrcAddr uint32
	DstAddr uint32
	SrcPort uint16
	DstPort uint16
}

// ebpfRetransmitStats matches the eBPF retransmit_stats struct
type ebpfRetransmitStats struct {
	TotalPackets     uint64
	Retransmits      uint64
	LastRetransmitNs uint64
	RSTReceived      uint8
	Padding          [7]uint8
}
