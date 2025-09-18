package network

import (
	"fmt"
	"net"
	"time"
)

// Network event types - must match C constants
const (
	EventTypeConnection      uint8 = 1
	EventTypeConnectionClose uint8 = 2
	EventTypeHTTPRequest     uint8 = 3
	EventTypeHTTPResponse    uint8 = 4
	EventTypeGRPCCall        uint8 = 5
	EventTypeGRPCResponse    uint8 = 6
	EventTypeDNSQuery        uint8 = 7
	EventTypeDNSResponse     uint8 = 8
)

// Connection states - must match C constants
const (
	ConnStateConnecting  uint8 = 0
	ConnStateEstablished uint8 = 1
	ConnStateClosing     uint8 = 2
	ConnStateClosed      uint8 = 3
	ConnStateListening   uint8 = 4
)

// L7 protocols - must match C constants
const (
	L7ProtocolUnknown uint8 = 0
	L7ProtocolHTTP    uint8 = 1
	L7ProtocolGRPC    uint8 = 2
	L7ProtocolDNS     uint8 = 3
)

// HTTP methods - must match C constants
const (
	HTTPMethodGet     uint8 = 1
	HTTPMethodPost    uint8 = 2
	HTTPMethodPut     uint8 = 3
	HTTPMethodDelete  uint8 = 4
	HTTPMethodPatch   uint8 = 5
	HTTPMethodHead    uint8 = 6
	HTTPMethodOptions uint8 = 7
)

// Protocol constants
const (
	ProtocolTCP  uint8 = 6
	ProtocolUDP  uint8 = 17
	ProtocolICMP uint8 = 1
)

// IP version constants
const (
	IPVersion4 uint8 = 4
	IPVersion6 uint8 = 6
)

// Direction constants
const (
	DirectionInbound  uint8 = 0
	DirectionOutbound uint8 = 1
)

// Size constants - must match C defines
const (
	MaxL7DataSize  = 255
	MaxCommSize    = 16
	MaxPodUIDSize  = 40
	MaxFlowKeySize = 24
	MaxServiceName = 64
	MaxMethodName  = 32
)

// BPFNetworkEvent represents the network event structure from eBPF
// Must exactly match the C struct network_event in network_monitor.c
type BPFNetworkEvent struct {
	// Header (8-byte aligned)
	Timestamp uint64 // timestamp
	PID       uint32 // pid
	TID       uint32 // tid

	// Event info
	EventType uint8 // event_type
	Protocol  uint8 // protocol
	IPVersion uint8 // ip_version
	Direction uint8 // direction

	// Network addresses (IPv6-compatible)
	SrcAddr [16]uint8 // src_addr
	DstAddr [16]uint8 // dst_addr

	// Ports
	SrcPort uint16 // src_port
	DstPort uint16 // dst_port

	// Process info
	UID      uint32    // uid
	GID      uint32    // gid
	CgroupID uint64    // cgroup_id
	Comm     [16]uint8 // comm

	// Connection state and metrics
	ConnState   uint8  // conn_state
	_pad1       uint8  // _pad1
	_pad2       uint16 // _pad2
	BytesSent   uint64 // bytes_sent
	BytesRecv   uint64 // bytes_recv
	PacketsSent uint32 // packets_sent
	PacketsRecv uint32 // packets_recv

	// L7 protocol information
	L7Protocol uint8      // l7_protocol
	L7DataLen  uint8      // l7_data_len
	_pad3      uint16     // _pad3
	L7Data     [255]uint8 // l7_data

	// Performance metrics
	LatencyNs  uint64 // latency_ns
	DurationNs uint64 // duration_ns

	// Container context
	PodUID [40]uint8 // pod_uid

	// Network interface
	IfIndex uint32 // if_index
	_pad4   uint32 // _pad4
}

// BPFFlowKey represents the flow key structure for connection tracking
// Must exactly match the C struct flow_key in network_monitor.c
type BPFFlowKey struct {
	SrcIP     [4]uint32 // src_ip (IPv6-compatible, IPv4 uses first element)
	DstIP     [4]uint32 // dst_ip (IPv6-compatible, IPv4 uses first element)
	SrcPort   uint16    // src_port
	DstPort   uint16    // dst_port
	Protocol  uint8     // protocol
	IPVersion uint8     // ip_version
	_pad      [2]uint8  // _pad
}

// BPFConnInfo represents connection tracking information
// Must exactly match the C struct conn_info in network_monitor.c
type BPFConnInfo struct {
	StartTime   uint64   // start_time
	BytesSent   uint64   // bytes_sent
	BytesRecv   uint64   // bytes_recv
	PacketsSent uint32   // packets_sent
	PacketsRecv uint32   // packets_recv
	PID         uint32   // pid
	State       uint8    // state
	L7Protocol  uint8    // l7_protocol
	_pad        [2]uint8 // _pad
}

// BPFHTTPState represents HTTP parsing state
// Must exactly match the C struct http_state in network_monitor.c
type BPFHTTPState struct {
	Method        uint8    // method
	StatusCode    uint16   // status_code
	Version       uint8    // version (HTTP/1.1=11, HTTP/2.0=20)
	ContentLength uint32   // content_length
	IsRequest     uint8    // is_request
	IsResponse    uint8    // is_response
	_pad          [2]uint8 // _pad
}

// BPFGRPCState represents gRPC call state
// Must exactly match the C struct grpc_state in network_monitor.c
type BPFGRPCState struct {
	StreamID    uint32    // stream_id
	MessageType uint8     // message_type (0=request, 1=response)
	Compression uint8     // compression
	StatusCode  uint8     // status_code
	_pad        uint8     // _pad
	Service     [64]uint8 // service
	Method      [32]uint8 // method
}

// NetworkEvent represents a parsed and enriched network event
type NetworkEvent struct {
	// Event metadata
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`

	// Process information
	PID     uint32 `json:"pid"`
	TID     uint32 `json:"tid"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
	Command string `json:"command"`

	// Network information
	Protocol    string `json:"protocol"`
	IPVersion   uint8  `json:"ip_version"`
	SrcIP       net.IP `json:"src_ip"`
	DstIP       net.IP `json:"dst_ip"`
	SrcPort     uint16 `json:"src_port"`
	DstPort     uint16 `json:"dst_port"`
	Direction   string `json:"direction"`
	PayloadSize int64  `json:"payload_size"`

	// Connection metrics
	ConnState   string        `json:"conn_state"`
	BytesSent   uint64        `json:"bytes_sent"`
	BytesRecv   uint64        `json:"bytes_recv"`
	PacketsSent uint32        `json:"packets_sent"`
	PacketsRecv uint32        `json:"packets_recv"`
	Latency     time.Duration `json:"latency"`
	Duration    time.Duration `json:"duration"`
	InterfaceID uint32        `json:"interface_id"`

	// L7 protocol data
	L7Protocol string    `json:"l7_protocol"`
	HTTPData   *HTTPData `json:"http_data,omitempty"`
	GRPCData   *GRPCData `json:"grpc_data,omitempty"`
	DNSData    *DNSData  `json:"dns_data,omitempty"`

	// Container context
	CgroupID   uint64              `json:"cgroup_id"`
	PodUID     string              `json:"pod_uid,omitempty"`
	Kubernetes *KubernetesMetadata `json:"kubernetes,omitempty"`

	// Security context
	SecurityFlags []string `json:"security_flags,omitempty"`
}

// HTTPData represents parsed HTTP request/response data
type HTTPData struct {
	Method        string            `json:"method,omitempty"`
	URL           string            `json:"url,omitempty"`
	Path          string            `json:"path,omitempty"`
	Query         string            `json:"query,omitempty"`
	StatusCode    uint16            `json:"status_code,omitempty"`
	StatusText    string            `json:"status_text,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	UserAgent     string            `json:"user_agent,omitempty"`
	ContentType   string            `json:"content_type,omitempty"`
	ContentLength int64             `json:"content_length,omitempty"`
	Host          string            `json:"host,omitempty"`
	Version       string            `json:"version,omitempty"`
	ResponseTime  time.Duration     `json:"response_time,omitempty"`
	IsRequest     bool              `json:"is_request"`
	IsResponse    bool              `json:"is_response"`
	Payload       []byte            `json:"payload,omitempty"`
}

// GRPCData represents parsed gRPC call data
type GRPCData struct {
	Service     string        `json:"service"`
	Method      string        `json:"method"`
	StreamID    uint32        `json:"stream_id"`
	MessageType string        `json:"message_type"` // "request" or "response"
	StatusCode  uint8         `json:"status_code"`
	Compression string        `json:"compression,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	Payload     []byte        `json:"payload,omitempty"`
}

// DNSData represents parsed DNS query/response data
type DNSData struct {
	TransactionID      uint16             `json:"transaction_id"`
	QueryName          string             `json:"query_name"`
	QueryType          string             `json:"query_type"`
	QueryClass         string             `json:"query_class"`
	ResponseCode       string             `json:"response_code,omitempty"`
	Answers            []NetworkDNSAnswer `json:"answers,omitempty"`
	Authoritative      bool               `json:"authoritative"`
	Truncated          bool               `json:"truncated"`
	RecursionAvailable bool               `json:"recursion_available"`
	Duration           time.Duration      `json:"duration,omitempty"`
	ServerIP           net.IP             `json:"server_ip,omitempty"`
}

// NetworkDNSAnswer represents a DNS response record for network events
type NetworkDNSAnswer struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Class string `json:"class"`
	TTL   uint32 `json:"ttl"`
	Data  string `json:"data"`
}

// ConnectionInfo tracks connection state and metrics
type ConnectionInfo struct {
	FlowKey       FlowKey       `json:"flow_key"`
	State         string        `json:"state"`
	StartTime     time.Time     `json:"start_time"`
	LastActivity  time.Time     `json:"last_activity"`
	BytesSent     uint64        `json:"bytes_sent"`
	BytesRecv     uint64        `json:"bytes_recv"`
	PacketsSent   uint32        `json:"packets_sent"`
	PacketsRecv   uint32        `json:"packets_recv"`
	L7Protocol    string        `json:"l7_protocol"`
	Duration      time.Duration `json:"duration"`
	Latency       time.Duration `json:"latency"`
	ProcessInfo   *ProcessInfo  `json:"process_info,omitempty"`
	SecurityFlags []string      `json:"security_flags,omitempty"`
}

// FlowKey represents a network flow identifier
type FlowKey struct {
	SrcIP     net.IP `json:"src_ip"`
	DstIP     net.IP `json:"dst_ip"`
	SrcPort   uint16 `json:"src_port"`
	DstPort   uint16 `json:"dst_port"`
	Protocol  string `json:"protocol"`
	IPVersion uint8  `json:"ip_version"`
}

// ProcessInfo contains process context for network events
type ProcessInfo struct {
	PID     uint32 `json:"pid"`
	TID     uint32 `json:"tid"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
	Command string `json:"command"`
}

// KubernetesMetadata contains Kubernetes context information
type KubernetesMetadata struct {
	PodName       string            `json:"pod_name,omitempty"`
	PodNamespace  string            `json:"pod_namespace,omitempty"`
	PodUID        string            `json:"pod_uid,omitempty"`
	ContainerID   string            `json:"container_id,omitempty"`
	ContainerName string            `json:"container_name,omitempty"`
	NodeName      string            `json:"node_name,omitempty"`
	ServiceName   string            `json:"service_name,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	WorkloadKind  string            `json:"workload_kind,omitempty"` // Deployment, StatefulSet, etc.
	WorkloadName  string            `json:"workload_name,omitempty"`
}

// TCPMetrics contains TCP-specific performance metrics
type TCPMetrics struct {
	RTT              time.Duration `json:"rtt"`
	RTO              time.Duration `json:"rto"`
	Retransmissions  uint32        `json:"retransmissions"`
	FastRetrans      uint32        `json:"fast_retrans"`
	TimeoutRetrans   uint32        `json:"timeout_retrans"`
	CongestionWindow uint32        `json:"congestion_window"`
	SSThreshold      uint32        `json:"ss_threshold"`
	SndWnd           uint32        `json:"snd_wnd"`
	RcvWnd           uint32        `json:"rcv_wnd"`
	ThroughputBps    uint64        `json:"throughput_bps"`
}

// SecurityContext contains security-related information
type SecurityContext struct {
	IsSuspicious      bool     `json:"is_suspicious"`
	ThreatLevel       string   `json:"threat_level"` // "low", "medium", "high", "critical"
	AnomalyScore      float64  `json:"anomaly_score"`
	ViolationTypes    []string `json:"violation_types,omitempty"`
	EncryptionEnabled bool     `json:"encryption_enabled"`
	CertificateValid  bool     `json:"certificate_valid,omitempty"`
}

// NetworkStats tracks network observer statistics
type NetworkStats struct {
	EventsGenerated      uint64            `json:"events_generated"`
	EventsDropped        uint64            `json:"events_dropped"`
	ConnectionsTracked   uint64            `json:"connections_tracked"`
	HTTPRequestsParsed   uint64            `json:"http_requests_parsed"`
	HTTPResponsesParsed  uint64            `json:"http_responses_parsed"`
	DNSQueriesParsed     uint64            `json:"dns_queries_parsed"`
	DNSResponsesParsed   uint64            `json:"dns_responses_parsed"`
	GRPCCallsParsed      uint64            `json:"grpc_calls_parsed"`
	BytesProcessed       uint64            `json:"bytes_processed"`
	PacketsProcessed     uint64            `json:"packets_processed"`
	L7ParseErrors        uint64            `json:"l7_parse_errors"`
	K8sEnrichmentRate    float64           `json:"k8s_enrichment_rate"`
	ProtocolDistribution map[string]uint64 `json:"protocol_distribution"`
	SecurityAnomalies    uint64            `json:"security_anomalies"`
	AverageLatency       time.Duration     `json:"average_latency"`
	AverageThroughput    uint64            `json:"average_throughput_bps"`
}

// NetworkReport represents a comprehensive network analysis report
type NetworkReport struct {
	Timestamp          time.Time                  `json:"timestamp"`
	ReportID           string                     `json:"report_id"`
	TimeRange          TimeRange                  `json:"time_range"`
	TopConnections     []ConnectionInfo           `json:"top_connections"`
	ProtocolSummary    map[string]*ProtocolStats  `json:"protocol_summary"`
	SecurityFindings   []SecurityFinding          `json:"security_findings"`
	PerformanceMetrics *NetworkPerformanceMetrics `json:"performance_metrics"`
	Kubernetes         []KubernetesNetworkSummary `json:"kubernetes,omitempty"`
	Recommendations    []string                   `json:"recommendations,omitempty"`
}

// TimeRange represents a time period
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ProtocolStats contains statistics for a specific protocol
type ProtocolStats struct {
	Protocol         string        `json:"protocol"`
	Connections      uint64        `json:"connections"`
	BytesTransferred uint64        `json:"bytes_transferred"`
	PacketsProcessed uint64        `json:"packets_processed"`
	AverageLatency   time.Duration `json:"average_latency"`
	ErrorRate        float64       `json:"error_rate"`
	TopEndpoints     []Endpoint    `json:"top_endpoints"`
}

// Endpoint represents a network endpoint
type Endpoint struct {
	IP          net.IP `json:"ip"`
	Port        uint16 `json:"port"`
	Connections uint64 `json:"connections"`
	Bytes       uint64 `json:"bytes"`
}

// SecurityFinding represents a security-related finding
type SecurityFinding struct {
	ID           string              `json:"id"`
	Timestamp    time.Time           `json:"timestamp"`
	Severity     string              `json:"severity"`
	Type         string              `json:"type"`
	Description  string              `json:"description"`
	Source       net.IP              `json:"source_ip"`
	Destination  net.IP              `json:"destination_ip"`
	Process      string              `json:"process"`
	Kubernetes   *KubernetesMetadata `json:"kubernetes,omitempty"`
	Remediation  string              `json:"remediation,omitempty"`
	AnomalyScore float64             `json:"anomaly_score"`
}

// NetworkPerformanceMetrics contains network performance data
type NetworkPerformanceMetrics struct {
	TotalThroughput   uint64        `json:"total_throughput_bps"`
	AverageLatency    time.Duration `json:"average_latency"`
	P99Latency        time.Duration `json:"p99_latency"`
	PacketLossRate    float64       `json:"packet_loss_rate"`
	Retransmissions   uint64        `json:"retransmissions"`
	ConnectionErrors  uint64        `json:"connection_errors"`
	TCPResets         uint64        `json:"tcp_resets"`
	DNSResolutionTime time.Duration `json:"dns_resolution_time"`
	HTTPErrorRate     float64       `json:"http_error_rate"`
	GRPCErrorRate     float64       `json:"grpc_error_rate"`
}

// KubernetesNetworkSummary contains network summary for Kubernetes workloads
type KubernetesNetworkSummary struct {
	Namespace        string            `json:"namespace"`
	WorkloadKind     string            `json:"workload_kind"`
	WorkloadName     string            `json:"workload_name"`
	ServiceName      string            `json:"service_name,omitempty"`
	Connections      uint64            `json:"connections"`
	BytesTransferred uint64            `json:"bytes_transferred"`
	ErrorRate        float64           `json:"error_rate"`
	AverageLatency   time.Duration     `json:"average_latency"`
	TopEndpoints     []Endpoint        `json:"top_endpoints"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// Helper functions for type conversions and validation

// String returns the string representation of event type
func (e NetworkEvent) EventTypeName() string {
	switch e.EventType {
	case "1":
		return "connection"
	case "2":
		return "connection_close"
	case "3":
		return "http_request"
	case "4":
		return "http_response"
	case "5":
		return "grpc_call"
	case "6":
		return "grpc_response"
	case "7":
		return "dns_query"
	case "8":
		return "dns_response"
	default:
		return fmt.Sprintf("unknown_%s", e.EventType)
	}
}

// String returns the string representation of connection state
func (c ConnectionInfo) StateName() string {
	switch c.State {
	case "0":
		return "connecting"
	case "1":
		return "established"
	case "2":
		return "closing"
	case "3":
		return "closed"
	case "4":
		return "listening"
	default:
		return fmt.Sprintf("unknown_%s", c.State)
	}
}

// GetFlowKey returns a flow key for the network event
func (e NetworkEvent) GetFlowKey() FlowKey {
	return FlowKey{
		SrcIP:     e.SrcIP,
		DstIP:     e.DstIP,
		SrcPort:   e.SrcPort,
		DstPort:   e.DstPort,
		Protocol:  e.Protocol,
		IPVersion: e.IPVersion,
	}
}

// IsInbound returns true if the connection is inbound
func (e NetworkEvent) IsInbound() bool {
	return e.Direction == "inbound"
}

// IsOutbound returns true if the connection is outbound
func (e NetworkEvent) IsOutbound() bool {
	return e.Direction == "outbound"
}

// HasL7Data returns true if the event contains L7 protocol data
func (e NetworkEvent) HasL7Data() bool {
	return e.HTTPData != nil || e.GRPCData != nil || e.DNSData != nil
}

// IsSecure returns true if the connection uses encryption
func (e NetworkEvent) IsSecure() bool {
	if e.HTTPData != nil {
		return e.DstPort == 443 || e.SrcPort == 443
	}
	return false
}

// GetL7ProtocolName returns human-readable L7 protocol name
func GetL7ProtocolName(protocol uint8) string {
	switch protocol {
	case L7ProtocolHTTP:
		return "HTTP"
	case L7ProtocolGRPC:
		return "gRPC"
	case L7ProtocolDNS:
		return "DNS"
	default:
		return "Unknown"
	}
}

// GetProtocolName returns human-readable protocol name
func GetProtocolName(protocol uint8) string {
	switch protocol {
	case ProtocolTCP:
		return "TCP"
	case ProtocolUDP:
		return "UDP"
	case ProtocolICMP:
		return "ICMP"
	default:
		return fmt.Sprintf("Protocol_%d", protocol)
	}
}

// GetEventTypeName returns human-readable event type name
func GetEventTypeName(eventType uint8) string {
	switch eventType {
	case EventTypeConnection:
		return "connection"
	case EventTypeConnectionClose:
		return "connection_close"
	case EventTypeHTTPRequest:
		return "http_request"
	case EventTypeHTTPResponse:
		return "http_response"
	case EventTypeGRPCCall:
		return "grpc_call"
	case EventTypeGRPCResponse:
		return "grpc_response"
	case EventTypeDNSQuery:
		return "dns_query"
	case EventTypeDNSResponse:
		return "dns_response"
	default:
		return fmt.Sprintf("event_%d", eventType)
	}
}

// GetConnStateName returns human-readable connection state name
func GetConnStateName(state uint8) string {
	switch state {
	case ConnStateConnecting:
		return "connecting"
	case ConnStateEstablished:
		return "established"
	case ConnStateClosing:
		return "closing"
	case ConnStateClosed:
		return "closed"
	case ConnStateListening:
		return "listening"
	default:
		return fmt.Sprintf("state_%d", state)
	}
}

// GetHTTPMethodName returns human-readable HTTP method name
func GetHTTPMethodName(method uint8) string {
	switch method {
	case HTTPMethodGet:
		return "GET"
	case HTTPMethodPost:
		return "POST"
	case HTTPMethodPut:
		return "PUT"
	case HTTPMethodDelete:
		return "DELETE"
	case HTTPMethodPatch:
		return "PATCH"
	case HTTPMethodHead:
		return "HEAD"
	case HTTPMethodOptions:
		return "OPTIONS"
	default:
		return fmt.Sprintf("method_%d", method)
	}
}

// GetDirectionName returns human-readable direction name
func GetDirectionName(direction uint8) string {
	switch direction {
	case DirectionInbound:
		return "inbound"
	case DirectionOutbound:
		return "outbound"
	default:
		return "unknown"
	}
}

// ValidateNetworkEvent validates a network event for completeness and consistency
func ValidateNetworkEvent(event *NetworkEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	if event.PID == 0 {
		return fmt.Errorf("invalid PID: %d", event.PID)
	}

	if event.SrcIP == nil || event.DstIP == nil {
		return fmt.Errorf("invalid IP addresses: src=%v, dst=%v", event.SrcIP, event.DstIP)
	}

	if event.Protocol == "" {
		return fmt.Errorf("protocol is empty")
	}

	if event.EventType == "" {
		return fmt.Errorf("event type is empty")
	}

	if event.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is zero")
	}

	return nil
}
