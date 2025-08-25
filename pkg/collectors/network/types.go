package network

import (
	"context"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NetworkCollectorConfig configuration for network collector
type NetworkCollectorConfig struct {
	BufferSize         int           `json:"buffer_size"`
	FlushInterval      time.Duration `json:"flush_interval"`
	EnableIPv4         bool          `json:"enable_ipv4"`
	EnableTCP          bool          `json:"enable_tcp"`
	EnableUDP          bool          `json:"enable_udp"`
	EnableHTTP         bool          `json:"enable_http"`
	EnableHTTPS        bool          `json:"enable_https"`
	HTTPPorts          []int         `json:"http_ports"`
	HTTPSPorts         []int         `json:"https_ports"`
	MaxEventsPerSecond int           `json:"max_events_per_second"`
	SamplingRate       float64       `json:"sampling_rate"`
}

// EventProcessor interface for processing events
type EventProcessor interface {
	Process(ctx context.Context, event *domain.CollectorEvent) error
}

// Base Collector for embedding
type Collector struct {
	name           string
	logger         *zap.Logger
	events         chan *domain.CollectorEvent
	ctx            context.Context
	wg             *sync.WaitGroup
	mutex          sync.RWMutex
	eventProcessor EventProcessor
}

// NewCollector creates a new base network collector
func NewCollector(name string, config *NetworkCollectorConfig, logger *zap.Logger) (*Collector, error) {
	if config == nil {
		config = &NetworkCollectorConfig{
			BufferSize: 1000,
			EnableIPv4: true,
			EnableTCP:  true,
			EnableUDP:  true,
		}
	}

	return &Collector{
		name:   name,
		logger: logger,
		events: make(chan *domain.CollectorEvent, config.BufferSize),
		wg:     &sync.WaitGroup{},
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the collector
func (c *Collector) Start(ctx context.Context) error {
	c.ctx = ctx
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if c.events != nil {
		close(c.events)
	}
	return nil
}

// Events returns the events channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return true
}

// IntelligenceCollectorConfig configuration for intelligence-focused network collector
type IntelligenceCollectorConfig struct {
	*NetworkCollectorConfig // Embed base config

	// Intelligence-specific settings
	EnableIntelligenceMode   bool     `json:"enable_intelligence_mode"`
	SlowRequestThresholdMs   int      `json:"slow_request_threshold_ms"`
	ErrorStatusThreshold     int      `json:"error_status_threshold"`
	LatencyDeviationFactor   float64  `json:"latency_deviation_factor"`
	DependencyCacheTTLMs     int      `json:"dependency_cache_ttl_ms"`
	IntelligenceSamplingRate float64  `json:"intelligence_sampling_rate"`
	ErrorCascadeWindowMs     int      `json:"error_cascade_window_ms"`
	ServiceDiscoveryEnabled  bool     `json:"service_discovery_enabled"`
	SecurityAnalysisEnabled  bool     `json:"security_analysis_enabled"`
	HTTPIntelligenceEnabled  bool     `json:"http_intelligence_enabled"`
	GRPCIntelligenceEnabled  bool     `json:"grpc_intelligence_enabled"`
	DNSIntelligenceEnabled   bool     `json:"dns_intelligence_enabled"`
	SuspiciousUserAgents     []string `json:"suspicious_user_agents"`
	SuspiciousEndpoints      []string `json:"suspicious_endpoints"`
	KnownGoodServices        []string `json:"known_good_services"`
}

// IntelligenceCollectorStats statistics for intelligence collector
type IntelligenceCollectorStats struct {
	EventsProcessed     int64   `json:"events_processed"`
	DependenciesFound   int64   `json:"dependencies_found"`
	ErrorPatternsFound  int64   `json:"error_patterns_found"`
	LatencyAnomalies    int64   `json:"latency_anomalies"`
	DNSFailures         int64   `json:"dns_failures"`
	SecurityConcerns    int64   `json:"security_concerns"`
	FilteringEfficiency float64 `json:"filtering_efficiency"`
}

// IntelligenceEventType represents types of intelligence events
type IntelligenceEventType int

const (
	IntelEventServiceDependency IntelligenceEventType = iota + 1
	IntelEventErrorPattern
	IntelEventLatencyAnomaly
	IntelEventDNSFailure
	IntelEventSecurityConcern
)

// IntelligenceEvent represents a processed intelligence event
type IntelligenceEvent struct {
	EventID         string                `json:"event_id"`
	Timestamp       time.Time             `json:"timestamp"`
	Type            IntelligenceEventType `json:"type"`
	Protocol        string                `json:"protocol"`
	SourceIP        string                `json:"source_ip"`
	DestIP          string                `json:"dest_ip"`
	SourcePort      int32                 `json:"source_port"`
	DestPort        int32                 `json:"dest_port"`
	ProcessID       uint32                `json:"process_id"`
	CgroupID        uint64                `json:"cgroup_id"`
	PodUID          string                `json:"pod_uid"`
	SourceService   string                `json:"source_service"`
	DestService     string                `json:"dest_service"`
	AnalysisContext map[string]string     `json:"analysis_context"`

	// Specific event data
	ErrorPattern   *ErrorPattern   `json:"error_pattern,omitempty"`
	LatencyAnomaly *LatencyAnomaly `json:"latency_anomaly,omitempty"`
	DNSFailure     *DNSFailure     `json:"dns_failure,omitempty"`
}

// ServiceDependency represents a discovered service dependency
type ServiceDependency struct {
	FromService  string        `json:"from_service"`
	ToService    string        `json:"to_service"`
	Protocol     string        `json:"protocol"`
	Port         int32         `json:"port"`
	RequestCount int64         `json:"request_count"`
	LastSeen     time.Time     `json:"last_seen"`
	AvgLatency   time.Duration `json:"avg_latency"`
	ErrorRate    float64       `json:"error_rate"`
	IsHealthy    bool          `json:"is_healthy"`
}

// ErrorPattern represents detected error patterns
type ErrorPattern struct {
	Endpoint         string    `json:"endpoint"`
	Method           string    `json:"method"`
	StatusCode       int       `json:"status_code"`
	ErrorCount       int32     `json:"error_count"`
	WindowStart      time.Time `json:"window_start"`
	IsCascade        bool      `json:"is_cascade"`
	AffectedServices []string  `json:"affected_services"`
}

// LatencyAnomaly represents detected latency anomalies
type LatencyAnomaly struct {
	Endpoint        string        `json:"endpoint"`
	Latency         time.Duration `json:"latency"`
	BaselineLatency time.Duration `json:"baseline_latency"`
	DeviationFactor float64       `json:"deviation_factor"`
	RequestCount    int64         `json:"request_count"`
}

// DNSFailure represents DNS resolution failures
type DNSFailure struct {
	Domain        string `json:"domain"`
	ResponseCode  uint16 `json:"response_code"`
	ResponseText  string `json:"response_text"`
	RetryCount    int32  `json:"retry_count"`
	SourceService string `json:"source_service"`
}

// SecurityConcern represents detected security threats and suspicious activities
type SecurityConcern struct {
	SourceService  string    `json:"source_service"`
	DestService    string    `json:"dest_service,omitempty"`
	SourceIP       string    `json:"source_ip"`
	DestIP         string    `json:"dest_ip,omitempty"`
	ConcernType    string    `json:"concern_type"`
	Description    string    `json:"description"`
	Severity       string    `json:"severity"`
	Confidence     float64   `json:"confidence"`
	Evidence       []string  `json:"evidence,omitempty"`
	RiskLevel      string    `json:"risk_level"`
	Timestamp      time.Time `json:"timestamp"`
	Recommendation string    `json:"recommendation,omitempty"`
	Blocked        bool      `json:"blocked"`
	AttackerUA     string    `json:"attacker_ua,omitempty"`
	TargetEndpoint string    `json:"target_endpoint,omitempty"`
	RequestCount   int32     `json:"request_count"`
}

// SecurityConcernType constants for structured threat classification
const (
	SecurityConcernPortScan         = "port_scan"
	SecurityConcernSQLInjection     = "sql_injection"
	SecurityConcernBruteForce       = "brute_force"
	SecurityConcernSuspiciousUA     = "suspicious_user_agent"
	SecurityConcernPathTraversal    = "path_traversal"
	SecurityConcernXSSAttempt       = "xss_attempt"
	SecurityConcernUnauthorized     = "unauthorized_access"
	SecurityConcernRateLimitHit     = "rate_limit_exceeded"
	SecurityConcernMaliciousIP      = "malicious_ip"
	SecurityConcernDataExfiltration = "data_exfiltration"
)

// SecuritySeverity constants for threat severity levels
const (
	SecuritySeverityLow      = "low"
	SecuritySeverityMedium   = "medium"
	SecuritySeverityHigh     = "high"
	SecuritySeverityCritical = "critical"
)

// SecurityRiskLevel constants for risk assessment
const (
	SecurityRiskLow      = "low"
	SecurityRiskMedium   = "medium"
	SecurityRiskHigh     = "high"
	SecurityRiskCritical = "critical"
)

// DefaultIntelligenceConfig returns a default intelligence collector configuration
func DefaultIntelligenceConfig() *IntelligenceCollectorConfig {
	return &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize:         1000,
			FlushInterval:      time.Second,
			EnableIPv4:         true,
			EnableTCP:          true,
			EnableUDP:          true,
			EnableHTTP:         true,
			EnableHTTPS:        true,
			HTTPPorts:          []int{80, 8080, 3000},
			HTTPSPorts:         []int{443, 8443},
			MaxEventsPerSecond: 5000, // Lower than regular collector
			SamplingRate:       1.0,
		},
		EnableIntelligenceMode:   true,
		SlowRequestThresholdMs:   1000,
		ErrorStatusThreshold:     400,
		LatencyDeviationFactor:   3.0,
		DependencyCacheTTLMs:     300000, // 5 minutes
		IntelligenceSamplingRate: 1.0,
		ErrorCascadeWindowMs:     30000, // 30 seconds
		ServiceDiscoveryEnabled:  true,
		SecurityAnalysisEnabled:  true,
		HTTPIntelligenceEnabled:  true,
		GRPCIntelligenceEnabled:  true,
		DNSIntelligenceEnabled:   true,
		SuspiciousUserAgents:     []string{"masscan", "nmap", "sqlmap", "nikto"},
		SuspiciousEndpoints:      []string{"/.env", "/admin", "/wp-admin", "/.git"},
		KnownGoodServices:        []string{"kubernetes", "istio-proxy", "envoy"},
	}
}
