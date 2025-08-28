package dns

import (
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
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

// BPFDNSEvent represents eBPF DNS event (stub for testing)
type BPFDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64
	EventType uint8
	Protocol  uint8
	IPVersion uint8
	SrcIP     [16]uint8
	DstIP     [16]uint8
	SrcPort   uint16
	DstPort   uint16
	QueryID   uint16
	QueryType uint16
	Rcode     uint8
	LatencyNs uint64
	QueryName [256]uint8
}

// Smart filtering and learning types

// FilteringMode defines different filtering strategies
type FilteringMode int

const (
	FilteringModePassthrough FilteringMode = iota // Capture everything (testing)
	FilteringModeBaseline                         // Learning mode - capture all to build baseline
	FilteringModeIntelligent                      // Production mode - filter based on learned patterns
	FilteringModeEmergency                        // Emergency mode - minimal capture only
)

func (f FilteringMode) String() string {
	switch f {
	case FilteringModePassthrough:
		return "passthrough"
	case FilteringModeBaseline:
		return "baseline"
	case FilteringModeIntelligent:
		return "intelligent"
	case FilteringModeEmergency:
		return "emergency"
	default:
		return "unknown"
	}
}

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

// EventImportance defines importance levels for DNS events
type EventImportance int

const (
	ImportanceLow EventImportance = iota
	ImportanceNormal
	ImportanceHigh
	ImportanceCritical
)

func (i EventImportance) String() string {
	switch i {
	case ImportanceLow:
		return "low"
	case ImportanceNormal:
		return "normal"
	case ImportanceHigh:
		return "high"
	case ImportanceCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// DNSPatternType represents types of DNS patterns for learning
type DNSPatternType int

const (
	PatternTypeNormal DNSPatternType = iota
	PatternTypeAnomaly
	PatternTypeSuspicious
	PatternTypeNoise
)

func (p DNSPatternType) String() string {
	switch p {
	case PatternTypeNormal:
		return "normal"
	case PatternTypeAnomaly:
		return "anomaly"
	case PatternTypeSuspicious:
		return "suspicious"
	case PatternTypeNoise:
		return "noise"
	default:
		return "unknown"
	}
}

// DNSLearningConfig holds configuration for the learning engine
type DNSLearningConfig struct {
	Enabled                bool          `json:"enabled"`
	BaselinePeriod         time.Duration `json:"baseline_period"`          // How long to learn baseline
	AnomalyThreshold       float64       `json:"anomaly_threshold"`        // Z-score threshold for anomalies
	SuspiciousDomainTTL    time.Duration `json:"suspicious_domain_ttl"`    // How long to remember suspicious domains
	MaxDomainsToTrack      int           `json:"max_domains_to_track"`     // Limit memory usage
	MaxServicesTracked     int           `json:"max_services_tracked"`     // Limit service tracking
	PatternUpdateInterval  time.Duration `json:"pattern_update_interval"`  // How often to update patterns
	AnomalyDetectionWindow time.Duration `json:"anomaly_detection_window"` // Rolling window for anomaly detection
}

// TimeSeriesEntry represents a single timestamp entry for frequency tracking
type TimeSeriesEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"`
}

// SlidingWindow implements a circular buffer for time-series data
type SlidingWindow struct {
	entries   []TimeSeriesEntry
	size      int
	head      int
	full      bool
	windowDur time.Duration
}

// NewSlidingWindow creates a new sliding window with specified capacity and duration
func NewSlidingWindow(size int, windowDur time.Duration) *SlidingWindow {
	return &SlidingWindow{
		entries:   make([]TimeSeriesEntry, size),
		size:      size,
		windowDur: windowDur,
	}
}

// AddEntry adds a new timestamp to the sliding window
func (sw *SlidingWindow) AddEntry(timestamp time.Time, count int) {
	sw.entries[sw.head] = TimeSeriesEntry{
		Timestamp: timestamp,
		Count:     count,
	}
	sw.head = (sw.head + 1) % sw.size
	if sw.head == 0 {
		sw.full = true
	}
}

// CountInWindow returns the total count of entries within the time window
func (sw *SlidingWindow) CountInWindow(now time.Time) int {
	cutoff := now.Add(-sw.windowDur)
	total := 0

	if sw.full {
		// Check all entries
		for i := 0; i < sw.size; i++ {
			if sw.entries[i].Timestamp.After(cutoff) {
				total += sw.entries[i].Count
			}
		}
	} else {
		// Only check up to head
		for i := 0; i < sw.head; i++ {
			if sw.entries[i].Timestamp.After(cutoff) {
				total += sw.entries[i].Count
			}
		}
	}

	return total
}

// DefaultLearningConfig returns sensible defaults for learning
func DefaultLearningConfig() DNSLearningConfig {
	return DNSLearningConfig{
		Enabled:                true,
		BaselinePeriod:         24 * time.Hour,  // Learn for 24 hours
		AnomalyThreshold:       3.0,             // 3 standard deviations
		SuspiciousDomainTTL:    1 * time.Hour,   // Remember suspicious domains for 1 hour
		MaxDomainsToTrack:      10000,           // Track up to 10k domains
		MaxServicesTracked:     1000,            // Track up to 1k services
		PatternUpdateInterval:  5 * time.Minute, // Update patterns every 5 minutes
		AnomalyDetectionWindow: 1 * time.Hour,   // 1 hour rolling window
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

// SmartFilterConfig holds configuration for intelligent filtering
type SmartFilterConfig struct {
	Mode                   FilteringMode `json:"mode"`
	SamplingRate           float64       `json:"sampling_rate"`            // Base sampling rate (0.0-1.0)
	AdaptiveSampling       bool          `json:"adaptive_sampling"`        // Enable adaptive sampling
	NoiseFilterEnabled     bool          `json:"noise_filter_enabled"`     // Filter known noise patterns
	HealthCheckFilter      bool          `json:"health_check_filter"`      // Filter health check queries
	DuplicateTimeWindow    time.Duration `json:"duplicate_time_window"`    // Window for duplicate detection
	MaxEventsPerSecond     int           `json:"max_events_per_second"`    // Rate limiting
	PriorityQueueEnabled   bool          `json:"priority_queue_enabled"`   // Enable priority-based processing
	BufferOverflowStrategy string        `json:"buffer_overflow_strategy"` // "drop_oldest", "drop_lowest_priority"
}

// DefaultSmartFilterConfig returns sensible defaults
func DefaultSmartFilterConfig() SmartFilterConfig {
	return SmartFilterConfig{
		Mode:                   FilteringModeBaseline, // Start in learning mode
		SamplingRate:           0.1,                   // 10% sampling by default
		AdaptiveSampling:       true,
		NoiseFilterEnabled:     true,
		HealthCheckFilter:      true,
		DuplicateTimeWindow:    1 * time.Second,
		MaxEventsPerSecond:     1000,
		PriorityQueueEnabled:   true,
		BufferOverflowStrategy: "drop_lowest_priority",
	}
}

// DNSBaseline represents learned normal behavior for a domain/service
type DNSBaseline struct {
	mu                 sync.RWMutex              `json:"-"` // Protect concurrent access
	DomainName         string                    `json:"domain_name"`
	Namespace          string                    `json:"namespace,omitempty"`
	ServiceName        string                    `json:"service_name,omitempty"`
	QueryTypes         map[DNSQueryType]int64    `json:"query_types"`          // Frequency of each query type
	AvgResponseTime    time.Duration             `json:"avg_response_time"`    // Average response time
	StdDevResponseTime time.Duration             `json:"stddev_response_time"` // Standard deviation
	QueryFrequency     float64                   `json:"query_frequency"`      // Queries per hour
	TypicalServers     map[string]int64          `json:"typical_servers"`      // DNS servers typically used
	ResponseCodes      map[DNSResponseCode]int64 `json:"response_codes"`       // Frequency of response codes
	FirstSeen          time.Time                 `json:"first_seen"`
	LastSeen           time.Time                 `json:"last_seen"`
	SampleCount        int64                     `json:"sample_count"`
	UpdatedAt          time.Time                 `json:"updated_at"`

	// Welford's algorithm state for accurate variance calculation
	varianceSum float64 `json:"-"` // M2 in Welford's algorithm
}

// DNSAnomaly represents detected anomalous behavior
type DNSAnomaly struct {
	ID                string               `json:"id"`
	Timestamp         time.Time            `json:"timestamp"`
	AnomalyType       string               `json:"anomaly_type"` // "frequency", "latency", "new_domain", etc.
	Severity          domain.EventSeverity `json:"severity"`
	DomainName        string               `json:"domain_name"`
	Namespace         string               `json:"namespace,omitempty"`
	ServiceName       string               `json:"service_name,omitempty"`
	Description       string               `json:"description"`
	Metrics           DNSAnomalyMetrics    `json:"metrics"`
	BaselineDeviation float64              `json:"baseline_deviation"` // Z-score
	ResolvedIPs       []string             `json:"resolved_ips,omitempty"`
	PID               uint32               `json:"pid,omitempty"`
	ContainerID       string               `json:"container_id,omitempty"`
}

// DNSAnomalyMetrics holds metrics for anomaly analysis
type DNSAnomalyMetrics struct {
	ActualValue    float64 `json:"actual_value"`
	ExpectedValue  float64 `json:"expected_value"`
	DeviationScore float64 `json:"deviation_score"`
	Confidence     float64 `json:"confidence"`
}

// SuspiciousDomain represents a domain flagged as potentially malicious
type SuspiciousDomain struct {
	DomainName       string               `json:"domain_name"`
	Reason           string               `json:"reason"` // "dga", "new_domain", "high_entropy", etc.
	FirstSeen        time.Time            `json:"first_seen"`
	LastSeen         time.Time            `json:"last_seen"`
	ConfidenceScore  float64              `json:"confidence_score"` // 0.0-1.0
	Severity         domain.EventSeverity `json:"severity"`
	QueryCount       int64                `json:"query_count"`
	AffectedServices []string             `json:"affected_services"`
	ResolvedIPs      []string             `json:"resolved_ips,omitempty"`
	TTL              time.Time            `json:"ttl"` // When to forget this domain
}

// DNSLearningEngine manages pattern learning and anomaly detection
type DNSLearningEngine struct {
	mu                sync.RWMutex
	config            DNSLearningConfig
	baselines         map[string]*DNSBaseline // domain -> baseline
	serviceBaselines  map[string]*DNSBaseline // namespace/service -> baseline
	suspiciousDomains map[string]*SuspiciousDomain
	recentAnomalies   []*DNSAnomaly
	mode              FilteringMode
	startTime         time.Time
	lastPatternUpdate time.Time
	anomalyCount      int64
	domainsSeen       int64
	learningActive    bool
	logger            *zap.Logger

	// Frequency tracking with sliding windows
	queryWindows map[string]*SlidingWindow // domain -> sliding window for frequency analysis
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

// SmartFilter implements intelligent event filtering and sampling
type SmartFilter struct {
	mu              sync.RWMutex
	config          SmartFilterConfig
	learningEngine  *DNSLearningEngine
	circuitBreaker  *CircuitBreaker
	recentEvents    map[string]time.Time // For duplicate detection
	priorityQueue   []*FilteredEvent     // Priority queue for events
	rateLimit       int64                // Current events per second
	lastRateReset   time.Time
	eventBuffer     []*domain.CollectorEvent // Overflow buffer
	bufferSize      int
	maxBufferSize   int
	droppedEvents   int64
	processedEvents int64
}

// FilteredEvent wraps a domain event with filtering metadata
type FilteredEvent struct {
	Event      *domain.CollectorEvent `json:"event"`
	Importance EventImportance        `json:"importance"`
	Score      float64                `json:"score"`     // Calculated importance score
	Reason     string                 `json:"reason"`    // Why this event was prioritized
	Timestamp  time.Time              `json:"timestamp"` // When filtered
}

// DNSCache represents effective DNS cache metrics
type DNSCache struct {
	DomainName    string    `json:"domain_name"`
	TTL           uint32    `json:"ttl"`
	ResolvedIPs   []string  `json:"resolved_ips"`
	CacheHits     int64     `json:"cache_hits"`
	CacheMisses   int64     `json:"cache_misses"`
	HitRate       float64   `json:"hit_rate"`
	Effectiveness float64   `json:"effectiveness"` // 0.0-1.0 cache effectiveness score
	LastAccessed  time.Time `json:"last_accessed"`
	ExpiresAt     time.Time `json:"expires_at"`
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
