package dns

import (
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// DNSEventType represents DNS event types
type DNSEventType string

const (
	DNSEventTypeQuery    DNSEventType = "query"
	DNSEventTypeResponse DNSEventType = "response"
	DNSEventTypeTimeout  DNSEventType = "timeout"
	DNSEventTypeError    DNSEventType = "error"
)

// DNSProtocol represents DNS transport protocols
type DNSProtocol string

const (
	DNSProtocolUDP DNSProtocol = "udp"
	DNSProtocolTCP DNSProtocol = "tcp"
)

// DNSQueryType represents DNS query types
type DNSQueryType string

const (
	DNSQueryTypeA     DNSQueryType = "A"
	DNSQueryTypeAAAA  DNSQueryType = "AAAA"
	DNSQueryTypeCNAME DNSQueryType = "CNAME"
	DNSQueryTypeMX    DNSQueryType = "MX"
	DNSQueryTypeNS    DNSQueryType = "NS"
	DNSQueryTypePTR   DNSQueryType = "PTR"
	DNSQueryTypeSOA   DNSQueryType = "SOA"
	DNSQueryTypeTXT   DNSQueryType = "TXT"
	DNSQueryTypeSRV   DNSQueryType = "SRV"
	DNSQueryTypeOther DNSQueryType = "OTHER"
)

// DNSResponseCode represents DNS response codes
type DNSResponseCode uint16

const (
	DNSResponseNoError   DNSResponseCode = 0 // NOERROR
	DNSResponseFormatErr DNSResponseCode = 1 // FORMERR
	DNSResponseServerErr DNSResponseCode = 2 // SERVFAIL
	DNSResponseNameErr   DNSResponseCode = 3 // NXDOMAIN
	DNSResponseNotImpl   DNSResponseCode = 4 // NOTIMP
	DNSResponseRefused   DNSResponseCode = 5 // REFUSED
)

// DNSFailureType represents types of DNS failures
type DNSFailureType string

const (
	DNSFailureTimeout  DNSFailureType = "timeout"
	DNSFailureNXDomain DNSFailureType = "nxdomain"
	DNSFailureServFail DNSFailureType = "servfail"
	DNSFailureRefused  DNSFailureType = "refused"
	DNSFailureNetErr   DNSFailureType = "network_error"
)

// DNSEvent represents a DNS query or response event - no map[string]interface{}
type DNSEvent struct {
	Timestamp    time.Time       `json:"timestamp"`
	EventType    DNSEventType    `json:"event_type"`
	QueryID      uint32          `json:"query_id"`
	QueryName    string          `json:"query_name"`
	QueryType    DNSQueryType    `json:"query_type"`
	Protocol     DNSProtocol     `json:"protocol"`
	ClientIP     string          `json:"client_ip"`
	ClientPort   uint16          `json:"client_port"`
	ServerIP     string          `json:"server_ip,omitempty"`
	ServerPort   uint16          `json:"server_port,omitempty"`
	ResolvedIP   string          `json:"resolved_ip,omitempty"`
	ResponseCode DNSResponseCode `json:"response_code,omitempty"`
	Success      bool            `json:"success"`
	LatencyMs    uint32          `json:"latency_ms,omitempty"`
	Namespace    string          `json:"namespace,omitempty"`
	ServiceName  string          `json:"service_name,omitempty"`
	PID          uint32          `json:"pid,omitempty"`
	TID          uint32          `json:"tid,omitempty"`
	ContainerID  string          `json:"container_id,omitempty"`
	CgroupID     uint64          `json:"cgroup_id,omitempty"`
}

// QueryFailure represents a DNS resolution failure
type QueryFailure struct {
	QueryName    string               `json:"query_name"`
	QueryType    DNSQueryType         `json:"query_type"`
	FailureType  DNSFailureType       `json:"failure_type"`
	ResponseCode DNSResponseCode      `json:"response_code,omitempty"`
	Timestamp    time.Time            `json:"timestamp"`
	ClientIP     string               `json:"client_ip"`
	ServerIP     string               `json:"server_ip,omitempty"`
	Protocol     DNSProtocol          `json:"protocol"`
	Namespace    string               `json:"namespace,omitempty"`
	ServiceName  string               `json:"service_name,omitempty"`
	Impact       []string             `json:"impact"` // affected services/pods
	Severity     domain.EventSeverity `json:"severity"`
	PID          uint32               `json:"pid,omitempty"`
	ContainerID  string               `json:"container_id,omitempty"`
}

// DNSMetrics holds DNS performance metrics
type DNSMetrics struct {
	QueryCount      int64         `json:"query_count"`
	ResponseCount   int64         `json:"response_count"`
	TimeoutCount    int64         `json:"timeout_count"`
	FailureCount    int64         `json:"failure_count"`
	AvgResponseTime time.Duration `json:"avg_response_time"`
	SlowQueries     int64         `json:"slow_queries"` // queries > 100ms
	NXDomainCount   int64         `json:"nxdomain_count"`
	ServFailCount   int64         `json:"servfail_count"`
	TCPFallbacks    int64         `json:"tcp_fallbacks"` // UDP->TCP fallbacks
	IPv6Queries     int64         `json:"ipv6_queries"`
	IPv4Queries     int64         `json:"ipv4_queries"`
}

// ServiceDNSHealth tracks DNS health per service
type ServiceDNSHealth struct {
	ServiceName     string        `json:"service_name"`
	Namespace       string        `json:"namespace"`
	QueryCount      int64         `json:"query_count"`
	FailureCount    int64         `json:"failure_count"`
	FailureRate     float64       `json:"failure_rate"` // 0.0-1.0
	AvgResponseTime time.Duration `json:"avg_response_time"`
	P95ResponseTime time.Duration `json:"p95_response_time"`
	LastQuery       time.Time     `json:"last_query"`
	LastFailure     *time.Time    `json:"last_failure,omitempty"`
	IsHealthy       bool          `json:"is_healthy"`
}

// DNSQuery represents a DNS query for tracking
type DNSQuery struct {
	ID          uint16       `json:"id"`
	Name        string       `json:"name"`
	Type        DNSQueryType `json:"type"`
	Protocol    DNSProtocol  `json:"protocol"`
	StartTime   time.Time    `json:"start_time"`
	ClientIP    string       `json:"client_ip"`
	ClientPort  uint16       `json:"client_port"`
	PID         uint32       `json:"pid"`
	Namespace   string       `json:"namespace,omitempty"`
	ServiceName string       `json:"service_name,omitempty"`
}

// DNSResponse represents a DNS response for tracking
type DNSResponse struct {
	QueryID      uint16          `json:"query_id"`
	ResponseCode DNSResponseCode `json:"response_code"`
	AnswerCount  uint16          `json:"answer_count"`
	ResolvedIPs  []string        `json:"resolved_ips"`
	TTL          uint32          `json:"ttl,omitempty"`
	ResponseTime time.Duration   `json:"response_time"`
	ServerIP     string          `json:"server_ip"`
	ServerPort   uint16          `json:"server_port"`
}

// String methods for type safety
func (t DNSEventType) String() string {
	return string(t)
}

func (p DNSProtocol) String() string {
	return string(p)
}

func (q DNSQueryType) String() string {
	return string(q)
}

func (f DNSFailureType) String() string {
	return string(f)
}

// IsError returns true if the response code indicates an error
func (r DNSResponseCode) IsError() bool {
	return r != DNSResponseNoError
}

// String returns human-readable response code
func (r DNSResponseCode) String() string {
	switch r {
	case DNSResponseNoError:
		return "NOERROR"
	case DNSResponseFormatErr:
		return "FORMERR"
	case DNSResponseServerErr:
		return "SERVFAIL"
	case DNSResponseNameErr:
		return "NXDOMAIN"
	case DNSResponseNotImpl:
		return "NOTIMP"
	case DNSResponseRefused:
		return "REFUSED"
	default:
		return "UNKNOWN"
	}
}

// DNSStats tracks DNS collector statistics
type DNSStats struct {
	EventsProcessed   int64     `json:"events_processed"`
	EventsDropped     int64     `json:"events_dropped"`
	ErrorCount        int64     `json:"error_count"`
	BufferUtilization float64   `json:"buffer_utilization"`
	EBPFAttached      bool      `json:"ebpf_attached"`
	LastEventTime     time.Time `json:"last_event_time"`
}

// Circuit breaker types for fault tolerance

// CircuitBreakerState represents the state of fault tolerance circuit breaker
type CircuitBreakerState int

const (
	CircuitClosed   CircuitBreakerState = iota // Normal operation
	CircuitOpen                                // Fault detected, stop processing
	CircuitHalfOpen                            // Testing if fault recovered
)

func (c CircuitBreakerState) String() string {
	switch c {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds configuration for fault tolerance
type CircuitBreakerConfig struct {
	Enabled               bool          `json:"enabled"`
	FailureThreshold      int           `json:"failure_threshold"`       // Number of failures before opening
	RecoveryTimeout       time.Duration `json:"recovery_timeout"`        // Time to wait before testing recovery
	SuccessThreshold      int           `json:"success_threshold"`       // Successes needed to close circuit
	MaxErrorRate          float64       `json:"max_error_rate"`          // Maximum error rate before opening
	TimeWindow            time.Duration `json:"time_window"`             // Time window for error rate calculation
	MaxConcurrentRequests int           `json:"max_concurrent_requests"` // Rate limiting
}

// DefaultCircuitBreakerConfig returns sensible defaults
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Enabled:               true,
		FailureThreshold:      10,               // Open after 10 failures
		RecoveryTimeout:       30 * time.Second, // Test recovery after 30s
		SuccessThreshold:      3,                // Close after 3 successes
		MaxErrorRate:          0.5,              // Open if >50% error rate
		TimeWindow:            1 * time.Minute,  // Calculate error rate per minute
		MaxConcurrentRequests: 1000,             // Limit concurrent processing
	}
}

// CircuitBreaker implements fault tolerance for DNS monitoring
type CircuitBreaker struct {
	mu                 sync.RWMutex
	config             CircuitBreakerConfig
	state              CircuitBreakerState
	failureCount       int
	successCount       int
	lastFailureTime    time.Time
	lastSuccessTime    time.Time
	recentErrors       []time.Time // Rolling window of recent errors
	concurrentRequests int64       // Current concurrent processing
	totalRequests      int64
	totalFailures      int64
	totalSuccesses     int64
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	if len(s) == 0 {
		return true
	}

	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
