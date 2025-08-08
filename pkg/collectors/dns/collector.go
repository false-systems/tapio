package dns

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/dns/bpf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Config for DNS collector with comprehensive settings
type Config struct {
	// Basic settings
	Name         string
	BufferSize   int
	Interface    string
	EnableEBPF   bool
	EnableSocket bool

	// DNS specific
	DNSPort   uint16
	Protocols []string // ["udp", "tcp"]

	// Rate limiting
	RateLimitEnabled bool
	RateLimitRPS     float64
	RateLimitBurst   int

	// Cache settings
	CacheEnabled bool
	CacheSize    int
	CacheTTL     time.Duration

	// Performance
	WorkerCount        int
	BatchSize          int
	FlushInterval      time.Duration
	SlowQueryThreshold time.Duration

	// Logging
	Logger *zap.Logger
}

// DefaultConfig returns sensible defaults with production settings
func DefaultConfig() Config {
	return Config{
		Name:               "dns",
		BufferSize:         10000,
		Interface:          "eth0",
		EnableEBPF:         true,
		EnableSocket:       false, // socket filter needs special privileges
		DNSPort:            53,
		Protocols:          []string{"udp", "tcp"},
		RateLimitEnabled:   true,
		RateLimitRPS:       1000.0, // 1000 queries per second
		RateLimitBurst:     2000,
		CacheEnabled:       true,
		CacheSize:          10000,
		CacheTTL:           5 * time.Minute,
		WorkerCount:        4,
		BatchSize:          100,
		FlushInterval:      100 * time.Millisecond,
		SlowQueryThreshold: 100 * time.Millisecond,
	}
}

// IPv4Address represents IPv4 address for eBPF compatibility
type IPv4Address struct {
	Addr uint32
}

// IPv6Address represents IPv6 address for eBPF compatibility
type IPv6Address struct {
	Addr [4]uint32
}

// AddressUnion represents the union from eBPF struct
type AddressUnion struct {
	IPv4 IPv4Address
	IPv6 IPv6Address
}

// EnhancedDNSEvent represents the enhanced DNS event from eBPF - must match C struct exactly
type EnhancedDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64
	EventType uint8
	Protocol  uint8
	IPVersion uint8
	Pad1      uint8
	SrcAddr   AddressUnion
	DstAddr   AddressUnion
	SrcPort   uint16
	DstPort   uint16
	DNSID     uint16
	DNSFlags  uint16
	DNSOpcode uint8
	DNSRcode  uint8
	DNSQtype  uint16
	DataLen   uint32
	LatencyNs uint32
	QueryName [128]byte // Increased size
	Data      [512]byte // Increased size
}

// DNSCache holds cached DNS responses with TTL
type DNSCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	maxSize int
	ttl     time.Duration
}

type CacheEntry struct {
	Value     interface{}
	Expires   time.Time
	HitCount  int64
	CreatedAt time.Time
}

// DNSStats holds collector statistics
type DNSStats struct {
	QueriesTotal   int64
	ResponsesTotal int64
	TimeoutsTotal  int64
	ErrorsTotal    int64
	CacheHits      int64
	CacheMisses    int64
	ActiveQueries  int64
	PacketsDropped int64
	LastQueryTime  time.Time
	LatencySum     int64 // in nanoseconds
	LatencyCount   int64
	SlowQueries    int64 // queries > threshold
}

// Collector implements DNS monitoring via eBPF with comprehensive observability
type Collector struct {
	// Core
	name    string
	logger  *zap.Logger
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	stopped bool
	mu      sync.RWMutex

	// eBPF components
	objs   *bpf.DnsmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Event processing
	events   chan collectors.RawEvent
	workerWg sync.WaitGroup

	// Rate limiting
	rlimiter *rate.Limiter

	// Cache
	cache *DNSCache

	// Statistics
	stats DNSStats

	// Active queries tracking
	activeQueries sync.Map // queryID -> startTime

	// OpenTelemetry
	tracer             trace.Tracer
	meter              metric.Meter
	queriesTotal       metric.Int64Counter
	queryLatency       metric.Float64Histogram
	errorsTotal        metric.Int64Counter
	activeQueriesGauge metric.Int64UpDownCounter
	cacheHitsTotal     metric.Int64Counter
	cacheMissTotal     metric.Int64Counter
	slowQueriesTotal   metric.Int64Counter
	packetsDropped     metric.Int64Counter
}

// NewCollector creates a new DNS collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	return &Collector{
		name:   name,
		config: cfg,
		events: make(chan collectors.RawEvent, cfg.BufferSize),
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	if !c.config.EnableEBPF {
		c.healthy = true
		return nil
	}

	// Load eBPF program
	spec, err := bpf.LoadDnsmonitor()
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	c.objs = &bpf.DnsmonitorObjects{}
	if err := spec.LoadAndAssign(c.objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Populate DNS-related PIDs
	if err := c.populateDNSPIDs(); err != nil {
		return fmt.Errorf("failed to populate DNS PIDs: %w", err)
	}

	// Attach tracepoints for DNS monitoring using available programs
	// Monitor DNS queries
	queryLink, err := link.Tracepoint("syscalls", "sys_enter_sendto", c.objs.TraceDnsQuery, nil)
	if err != nil {
		return fmt.Errorf("failed to attach DNS query tracepoint: %w", err)
	}
	c.links = append(c.links, queryLink)

	// Monitor DNS responses
	responseLink, err := link.Tracepoint("syscalls", "sys_exit_recvfrom", c.objs.TraceDnsResponse, nil)
	if err != nil {
		return fmt.Errorf("failed to attach DNS response tracepoint: %w", err)
	}
	c.links = append(c.links, responseLink)

	// Open ring buffer - use available map name
	c.reader, err = ringbuf.NewReader(c.objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}

	// Start event processing
	go c.processEvents()

	c.healthy = true
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	// Prevent multiple stops
	if c.stopped {
		return nil
	}
	c.stopped = true

	if c.cancel != nil {
		c.cancel()
	}

	// Close links
	for _, l := range c.links {
		if l != nil {
			l.Close()
		}
	}

	// Close ring buffer
	if c.reader != nil {
		c.reader.Close()
	}

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
	}

	// Close events channel only once
	if c.events != nil {
		close(c.events)
	}
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// populateDNSPIDs finds and adds DNS-related PIDs to the map
func (c *Collector) populateDNSPIDs() error {
	// Find DNS-related processes
	dnsProcesses := []string{"systemd-resolved", "dnsmasq", "unbound", "bind9", "named", "coredns"}

	// Scan /proc for DNS processes
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}

	var value uint8 = 1
	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}

		pid, err := strconv.ParseUint(proc.Name(), 10, 32)
		if err != nil {
			continue
		}

		// Read comm to check if it's DNS-related
		commPath := fmt.Sprintf("/proc/%d/comm", pid)
		comm, err := os.ReadFile(commPath)
		if err != nil {
			continue
		}

		commStr := strings.TrimSpace(string(comm))
		for _, dnsProc := range dnsProcesses {
			if strings.Contains(commStr, dnsProc) {
				if err := c.objs.DnsQueries.Put(uint32(pid), value); err != nil {
					// Log but don't fail - just skip this PID
					continue
				}
				break
			}
		}
	}

	return nil
}

// processEvents processes events from the ring buffer with enhanced error handling
func (c *Collector) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		record, err := c.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			// Log error but continue processing - production requirement
			continue
		}

		// Parse enhanced event - safe binary unmarshaling
		expectedSize := int(unsafe.Sizeof(EnhancedDNSEvent{}))
		if len(record.RawSample) < expectedSize {
			continue
		}

		var event EnhancedDNSEvent
		// Safe binary unmarshaling with exact size check
		if len(record.RawSample) != expectedSize {
			continue
		}
		event = *(*EnhancedDNSEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Convert to RawEvent with enhanced metadata - NO BUSINESS LOGIC
		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(uint32(event.EventType)),
			Data:      record.RawSample, // Raw eBPF event data
			Metadata: map[string]string{
				"collector":  "dns",
				"pid":        fmt.Sprintf("%d", event.PID),
				"tid":        fmt.Sprintf("%d", event.TID),
				"uid":        fmt.Sprintf("%d", event.UID),
				"gid":        fmt.Sprintf("%d", event.GID),
				"cgroup_id":  fmt.Sprintf("%d", event.CgroupID),
				"protocol":   c.protocolToString(event.Protocol),
				"ip_version": fmt.Sprintf("%d", event.IPVersion),
				"dns_id":     fmt.Sprintf("%d", event.DNSID),
				"dns_opcode": fmt.Sprintf("%d", event.DNSOpcode),
				"dns_rcode":  fmt.Sprintf("%d", event.DNSRcode),
				"dns_flags":  fmt.Sprintf("0x%04x", event.DNSFlags),
				"dns_qtype":  fmt.Sprintf("%d", event.DNSQtype),
				"data_len":   fmt.Sprintf("%d", event.DataLen),
				"latency_ns": fmt.Sprintf("%d", event.LatencyNs),
			},
			// Generate new trace ID for each DNS event
			TraceID: collectors.GenerateTraceID(),
			SpanID:  collectors.GenerateSpanID(),
		}

		// Add network info based on IP version
		if event.IPVersion == 4 {
			if event.SrcAddr.IPv4.Addr != 0 {
				rawEvent.Metadata["src_ip"] = c.ipv4ToString(event.SrcAddr.IPv4.Addr)
			}
			if event.DstAddr.IPv4.Addr != 0 {
				rawEvent.Metadata["dst_ip"] = c.ipv4ToString(event.DstAddr.IPv4.Addr)
			}
		} else if event.IPVersion == 6 {
			srcIPv6 := c.ipv6ToString(event.SrcAddr.IPv6.Addr)
			if srcIPv6 != "::" {
				rawEvent.Metadata["src_ip"] = srcIPv6
			}
			dstIPv6 := c.ipv6ToString(event.DstAddr.IPv6.Addr)
			if dstIPv6 != "::" {
				rawEvent.Metadata["dst_ip"] = dstIPv6
			}
		}

		// Add port information
		if event.SrcPort != 0 {
			rawEvent.Metadata["src_port"] = fmt.Sprintf("%d", event.SrcPort)
		}
		if event.DstPort != 0 {
			rawEvent.Metadata["dst_port"] = fmt.Sprintf("%d", event.DstPort)
		}

		// Extract query name and add K8s metadata (minimal extraction, no business logic)
		queryName := c.nullTerminatedString(event.QueryName[:])
		if queryName != "" {
			rawEvent.Metadata["query_name"] = queryName

			// Extract namespace and service for K8s DNS patterns - raw extraction only
			namespace := c.extractNamespace(queryName)
			service := c.extractService(queryName)
			if namespace != "" {
				rawEvent.Metadata["namespace"] = namespace
			}
			if service != "" {
				rawEvent.Metadata["service"] = service
			}
		}

		// Add query type string if available
		if event.DNSQtype != 0 {
			rawEvent.Metadata["query_type"] = c.queryTypeToString(event.DNSQtype)
		}

		// Add response code string for responses
		if event.EventType == 2 { // DNS_EVENT_RESPONSE
			rawEvent.Metadata["response_code"] = c.responseCodeToString(event.DNSRcode)
		}

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full - production requirement
		}
	}
}

// eventTypeToString converts event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 1:
		return "dns_query"
	case 2:
		return "dns_response"
	case 3:
		return "dns_timeout"
	default:
		return "dns_unknown"
	}
}

// protocolToString converts protocol number to string
func (c *Collector) protocolToString(protocol uint8) string {
	switch protocol {
	case 17:
		return "udp"
	case 6:
		return "tcp"
	default:
		return fmt.Sprintf("proto_%d", protocol)
	}
}

// extractNamespace extracts K8s namespace from DNS query
func (c *Collector) extractNamespace(queryName string) string {
	// Check for K8s service pattern: service.namespace.svc.cluster.local
	parts := strings.Split(queryName, ".")
	if len(parts) >= 4 {
		// Look for .svc.cluster.local pattern
		for i := 0; i <= len(parts)-3; i++ {
			if i+2 < len(parts) && parts[i] == "svc" && parts[i+1] == "cluster" && parts[i+2] == "local" {
				// Found svc.cluster.local pattern, namespace is the part before "svc"
				if i > 0 {
					return parts[i-1] // namespace is before "svc"
				}
				break
			}
		}
	}
	return ""
}

// extractService extracts K8s service name from DNS query
func (c *Collector) extractService(queryName string) string {
	parts := strings.Split(queryName, ".")
	if len(parts) >= 3 && strings.HasSuffix(queryName, ".svc.cluster.local") {
		return parts[0]
	}
	return queryName
}

// ipv4ToString converts uint32 IP to string (network byte order)
func (c *Collector) ipv4ToString(ip uint32) string {
	// Convert from network byte order (big endian) to host representation
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip))
}

// ipv6ToString converts IPv6 address array to string
func (c *Collector) ipv6ToString(addr [4]uint32) string {
	// Check for zero address
	if addr[0] == 0 && addr[1] == 0 && addr[2] == 0 && addr[3] == 0 {
		return "::"
	}

	// Convert to 16-byte array
	var bytes [16]byte
	for i := 0; i < 4; i++ {
		val := addr[i]
		bytes[i*4] = byte(val)
		bytes[i*4+1] = byte(val >> 8)
		bytes[i*4+2] = byte(val >> 16)
		bytes[i*4+3] = byte(val >> 24)
	}

	// Format as IPv6 string
	return fmt.Sprintf("%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x",
		bytes[0], bytes[1], bytes[2], bytes[3],
		bytes[4], bytes[5], bytes[6], bytes[7],
		bytes[8], bytes[9], bytes[10], bytes[11],
		bytes[12], bytes[13], bytes[14], bytes[15])
}

// queryTypeToString converts DNS query type to string
func (c *Collector) queryTypeToString(qtype uint16) string {
	switch qtype {
	case 1:
		return "A"
	case 2:
		return "NS"
	case 5:
		return "CNAME"
	case 6:
		return "SOA"
	case 12:
		return "PTR"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 28:
		return "AAAA"
	case 33:
		return "SRV"
	default:
		return fmt.Sprintf("TYPE%d", qtype)
	}
}

// responseCodeToString converts DNS response code to string
func (c *Collector) responseCodeToString(rcode uint8) string {
	switch rcode {
	case 0:
		return "NOERROR"
	case 1:
		return "FORMERR"
	case 2:
		return "SERVFAIL"
	case 3:
		return "NXDOMAIN"
	case 4:
		return "NOTIMP"
	case 5:
		return "REFUSED"
	default:
		return fmt.Sprintf("RCODE%d", rcode)
	}
}

// Legacy function for backward compatibility - with correct byte order
func intToIP(ip uint32) string {
	// Convert from network byte order (big endian) to host representation
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip))
}

// nullTerminatedString converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
