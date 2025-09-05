package link

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// LinkFailure represents a network link failure at any OSI layer
type LinkFailure struct {
	Type      string // syn_timeout, arp_timeout, rst, icmp_unreachable, etc
	Layer     int    // OSI layer (2, 3, 4)
	Timestamp time.Time

	// Network details
	SrcIP     string
	DstIP     string
	SrcPort   int32
	DstPort   int32
	Interface string

	// Failure specifics
	ErrorCode  int
	ErrorMsg   string
	RetryCount int
	Duration   time.Duration
}

// SYNAttempt tracks a pending TCP SYN
type SYNAttempt struct {
	Timestamp time.Time
	SrcIP     string
	DstIP     string
	SrcPort   int32
	DstPort   int32
	SeqNum    uint32
	Retries   int
}

// ARPRequest tracks a pending ARP request
type ARPRequest struct {
	Timestamp time.Time
	SrcIP     string
	SrcMAC    string
	TargetIP  string
	Interface string
	Retries   int
}

// LinkState tracks the health of a network link
type LinkState struct {
	Endpoint     string // IP:Port or IP for L3
	LastSeen     time.Time
	FailureCount int
	SuccessCount int
	PacketLoss   float64
	AvgLatency   time.Duration
	State        string // healthy, degraded, failed
	LastFailure  *LinkFailure
}

// LinkDiagnosis is the root cause analysis result
type LinkDiagnosis struct {
	Pattern    string  // Which failure pattern matched
	Confidence float32 // 0-1 confidence score
	Severity   domain.EventSeverity
	Layer      int // OSI layer where issue detected
	Timestamp  time.Time

	// The diagnosis
	Summary    string   // One-line summary
	Details    string   // Full explanation
	Evidence   []string // Supporting evidence
	Impact     string   // What's affected
	Resolution string   // How to fix

	// Related failures
	PrimaryFailure  *LinkFailure
	RelatedFailures []*LinkFailure

	// Context
	NetworkPolicy string // If policy blocked
	PodInfo       *PodContext
	ServiceInfo   *ServiceContext
}

// PodContext provides pod-related context for failures
type PodContext struct {
	Name        string
	Namespace   string
	IP          string
	Node        string
	Restarted   bool
	RestartTime time.Time
}

// ServiceContext provides service-related context
type ServiceContext struct {
	Name      string
	Namespace string
	ClusterIP string
	Endpoints []string
	Healthy   bool
}

// FailurePattern defines a known link failure pattern
type FailurePattern struct {
	Name        string
	Description string
	Layer       int // OSI layer
	Detector    func(*LinkFailure, *CorrelationContext) *LinkDiagnosis
}

// CorrelationContext holds recent events for correlation
type CorrelationContext struct {
	RecentSYNs      map[uint64]*SYNAttempt     // Pending SYNs
	RecentARPs      map[uint32]*ARPRequest     // Pending ARPs
	RecentFailures  []*LinkFailure             // Last N failures
	LinkStates      map[string]*LinkState      // Link health tracking
	NetworkPolicies map[string]*PolicyInfo     // Active policies
	PodStates       map[string]*PodContext     // Pod info
	ServiceStates   map[string]*ServiceContext // Service info
}

// PolicyInfo represents network policy information
type PolicyInfo struct {
	Name        string
	Namespace   string
	PodSelector map[string]string
	Ingress     []PolicyRule
	Egress      []PolicyRule
}

// PolicyRule represents a network policy rule
type PolicyRule struct {
	Ports     []int32
	Protocols []string
	From      []string // Source selectors
	To        []string // Dest selectors
}

// LinkEvent represents a raw network event from eBPF
type LinkEvent struct {
	TimestampNs uint64
	EventType   uint32 // 1=SYN, 2=SYN-ACK, 3=RST, 4=ARP_REQ, 5=ARP_REPLY, etc
	Layer       uint8  // 2, 3, or 4

	// Network info
	SrcIP    [16]byte
	DstIP    [16]byte
	SrcPort  uint16
	DstPort  uint16
	Protocol uint8

	// Event specifics
	Flags     uint32
	ErrorCode uint32
	Latency   uint64 // in nanoseconds

	// Process context
	PID      uint32
	CgroupID uint64
	Comm     [16]byte
}

// MetricsSnapshot for health checking
type MetricsSnapshot struct {
	TotalFailures    int64
	SYNTimeouts      int64
	ARPFailures      int64
	PolicyBlocks     int64
	Retransmissions  int64
	ConnectionResets int64
	AvgDetectionTime time.Duration
}
