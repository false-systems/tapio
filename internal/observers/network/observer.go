//go:build linux
// +build linux

package network

import (
	"fmt"
	"net"

	"github.com/cilium/ebpf"
	"github.com/yairfalse/tapio/internal/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
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
	Output           base.OutputConfig
	EventChannelSize int // Ring buffer → processor channel size (default: 1000)

	// K8s context service (optional - nil in OSS mode)
	// When provided, enables pod context enrichment with pre-computed OTEL attributes
	K8sContextService K8sContextGetter
}

// NetworkObserver tracks TCP/UDP/DNS network events using eBPF
type NetworkObserver struct {
	*base.BaseObserver
	config  Config
	ebpfMgr *base.EBPFManager // eBPF lifecycle manager

	// eBPF map references (nil when eBPF not loaded)
	connStatsMap *ebpf.Map // conn_stats LRU map from eBPF

	// Network-specific OTEL metrics
	connectionResets  metric.Int64Counter // connection_resets_total
	synTimeouts       metric.Int64Counter // syn_timeouts_total
	connectionRefused metric.Int64Counter // connection_refused_total

	// Packet loss metrics
	retransmitsTotal metric.Int64Counter // retransmits_total
	retransmitRate   metric.Float64Gauge // retransmit_rate_ratio (0.0-1.0)
	congestionEvents metric.Int64Counter // congestion_events_total

	// RTT spike metrics (Stage 3)
	rttSpikesTotal metric.Int64Counter // rtt_spikes_total
	rttCurrentMs   metric.Float64Gauge // rtt_current_ms

	rttDegradationPct metric.Float64Gauge // rtt_degradation_percent

	// eBPF health metrics
	ringbufferUtilization metric.Float64Gauge // ringbuffer_utilization_percent
	ebpfMapSize           metric.Int64Gauge   // ebpf_map_size_entries
}

// NewNetworkObserver creates a new network observer
func NewNetworkObserver(name string, config Config) (*NetworkObserver, error) {
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	// Create network-specific OTEL metrics
	meter := otel.Meter("tapio.observer.network")

	connectionResets, err := meter.Int64Counter(
		"connection_resets_total",
		metric.WithDescription("Total number of TCP connection resets (RST) received"),
		metric.WithUnit("{resets}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection_resets counter: %w", err)
	}

	synTimeouts, err := meter.Int64Counter(
		"syn_timeouts_total",
		metric.WithDescription("Total number of TCP SYN timeouts (no response)"),
		metric.WithUnit("{timeouts}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create syn_timeouts counter: %w", err)
	}

	connectionRefused, err := meter.Int64Counter(
		"connection_refused_total",
		metric.WithDescription("Total number of TCP connections refused (RST on SYN)"),
		metric.WithUnit("{refused}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection_refused counter: %w", err)
	}

	retransmitsTotal, err := meter.Int64Counter(
		"retransmits_total",
		metric.WithDescription("Total number of TCP packet retransmissions detected"),
		metric.WithUnit("{retransmits}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create retransmits_total counter: %w", err)
	}

	retransmitRate, err := meter.Float64Gauge(
		"retransmit_rate_ratio",
		metric.WithDescription("TCP retransmission rate as ratio of total packets (0.0-1.0)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create retransmit_rate gauge: %w", err)
	}

	congestionEvents, err := meter.Int64Counter(
		"congestion_events_total",
		metric.WithDescription("High retransmit rate events (>5%)"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create congestion_events counter: %w", err)
	}

	// RTT spike metrics (Stage 3)
	rttSpikesTotal, err := meter.Int64Counter(
		"rtt_spikes_total",
		metric.WithDescription("Total number of RTT spike events detected"),
		metric.WithUnit("{spikes}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rtt_spikes counter: %w", err)
	}

	rttCurrentMs, err := meter.Float64Gauge(
		"rtt_current_ms",
		metric.WithDescription("Current RTT in milliseconds when spike detected"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rtt_current gauge: %w", err)
	}

	rttDegradationPct, err := meter.Float64Gauge(
		"rtt_degradation_ratio",
		metric.WithDescription("RTT degradation ratio from baseline (0.0-1.0, where 1.0 = 100% degradation)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rtt_degradation gauge: %w", err)
	}

	// eBPF health metrics
	ringbufferUtilization, err := meter.Float64Gauge(
		"ringbuffer_utilization_percent",
		metric.WithDescription("eBPF ring buffer utilization percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ringbuffer_utilization gauge: %w", err)
	}

	ebpfMapSize, err := meter.Int64Gauge(
		"ebpf_map_size_entries",
		metric.WithDescription("Number of entries in eBPF maps (baseline_rtt)"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_map_size gauge: %w", err)
	}

	return &NetworkObserver{
		BaseObserver:          baseObs,
		config:                config,
		connectionResets:      connectionResets,
		synTimeouts:           synTimeouts,
		connectionRefused:     connectionRefused,
		retransmitsTotal:      retransmitsTotal,
		retransmitRate:        retransmitRate,
		congestionEvents:      congestionEvents,
		rttSpikesTotal:        rttSpikesTotal,
		rttCurrentMs:          rttCurrentMs,
		rttDegradationPct:     rttDegradationPct,
		ringbufferUtilization: ringbufferUtilization,
		ebpfMapSize:           ebpfMapSize,
	}, nil
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
