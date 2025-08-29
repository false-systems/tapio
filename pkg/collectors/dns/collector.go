//go:build linux
// +build linux

package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/dns/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 dnsMonitor ./bpf_src/dns_monitor.c -- -I../bpf_common

// Config for DNS collector
type Config struct {
	Name       string
	BufferSize int
	EnableEBPF bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Name:       "dns",
		BufferSize: 10000,
		EnableEBPF: true,
	}
}

// BPFDNSEvent matches the C struct dns_event exactly for proper memory alignment
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
	Pad1      uint8

	// Source address (IPv4 or IPv6)
	SrcAddr [16]byte // Union of IPv4 (4 bytes) and IPv6 (16 bytes)

	// Destination address (IPv4 or IPv6)
	DstAddr [16]byte // Union of IPv4 (4 bytes) and IPv6 (16 bytes)

	SrcPort uint16
	DstPort uint16

	// DNS info
	DNSID    uint16
	DNSFlags uint16
	Opcode   uint8
	Rcode    uint8
	QType    uint16

	DataLen   uint32
	LatencyNs uint32

	QueryName [128]byte // MAX_DNS_NAME_LEN from C
	Data      [512]byte // MAX_DNS_DATA from C
}

// eBPF components
type ebpfState struct {
	objs   *bpf.DnsMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// Collector implements DNS monitoring via eBPF with mock mode support
type Collector struct {
	// Core
	name    string
	logger  *zap.Logger
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mu      sync.RWMutex

	// Statistics
	stats *DNSStats

	// eBPF components (nil in mock mode)
	ebpfState *ebpfState

	// Mock mode
	mockMode bool

	// Event processing
	events chan *domain.CollectorEvent

	// OpenTelemetry
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	bufferUsage     metric.Int64Gauge
	droppedEvents   metric.Int64Counter
}

// NewCollector creates a new DNS collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Check for mock mode
	mockMode := os.Getenv("TAPIO_MOCK_MODE") == "true"
	if mockMode {
		logger.Info("DNS collector running in MOCK MODE", zap.String("name", name))
	}

	// Initialize OTEL components
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total DNS events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("DNS processing duration in milliseconds for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped DNS events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	return &Collector{
		name:            name,
		logger:          logger,
		config:          cfg,
		mockMode:        mockMode,
		stats:           &DNSStats{},
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		bufferUsage:     bufferUsage,
		droppedEvents:   droppedEvents,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the DNS monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "dns.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	if !c.config.EnableEBPF {
		c.healthy = true
		return nil
	}

	// Check if we're in mock mode
	if c.mockMode {
		c.logger.Info("Starting DNS collector in mock mode")
		go c.generateMockEvents()
		c.healthy = true
		return nil
	}

	// Start real eBPF monitoring
	if err := c.startEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_start_failed"),
			))
		}
		span.RecordError(err)
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processing loop
	go c.readEBPFEvents()

	c.healthy = true
	c.logger.Info("DNS collector started",
		zap.String("name", c.name),
		zap.Bool("ebpf_enabled", c.config.EnableEBPF),
		zap.Bool("mock_mode", c.mockMode),
		zap.Int("buffer_size", c.config.BufferSize),
	)
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Stop eBPF if running
	if !c.mockMode && c.ebpfState != nil {
		c.stopEBPF()
	}

	// Close events channel
	if c.events != nil {
		close(c.events)
	}
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// Health returns domain-compatible health status
func (c *Collector) Health() *domain.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := domain.HealthUnhealthy
	message := "DNS collector not running"

	if c.healthy {
		bufferUsage := float64(len(c.events)) / float64(cap(c.events))
		if bufferUsage >= 0.9 {
			status = domain.HealthDegraded
			message = "DNS collector healthy but high buffer utilization"
		} else {
			status = domain.HealthHealthy
			if c.mockMode {
				message = "DNS collector running in mock mode"
			} else {
				message = "DNS collector actively monitoring"
			}
		}
	}

	return &domain.HealthStatus{
		Status:    status,
		Message:   message,
		Component: c.name,
		Timestamp: time.Now(),
	}
}

// Statistics returns domain-compatible statistics
func (c *Collector) Statistics() *domain.CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &domain.CollectorStats{
		EventsProcessed: c.stats.EventsProcessed,
		ErrorCount:      c.stats.ErrorCount,
		LastEventTime:   c.stats.LastEventTime,
		Uptime:          time.Since(c.stats.LastEventTime),
		CustomMetrics: map[string]string{
			"events_dropped":     fmt.Sprintf("%d", c.stats.EventsDropped),
			"buffer_utilization": fmt.Sprintf("%.2f", c.stats.BufferUtilization),
			"ebpf_attached":      fmt.Sprintf("%t", c.stats.EBPFAttached),
			"mock_mode":          fmt.Sprintf("%t", c.mockMode),
		},
	}
}

// generateMockEvents generates fake DNS events for development/testing
func (c *Collector) generateMockEvents() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	domains := []string{
		"api.kubernetes.io",
		"etcd.kube-system.svc.cluster.local",
		"prometheus.monitoring.svc.cluster.local",
		"grafana.monitoring.svc.cluster.local",
		"example.com",
		"google.com",
		"github.com",
		"invalid-domain-12345.local",
	}

	queryTypes := []DNSQueryType{
		DNSQueryTypeA,
		DNSQueryTypeAAAA,
		DNSQueryTypeCNAME,
		DNSQueryTypeMX,
		DNSQueryTypeSRV,
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Generate a random DNS event
			domain := domains[rand.Intn(len(domains))]
			queryType := queryTypes[rand.Intn(len(queryTypes))]
			success := rand.Float32() > 0.1 // 90% success rate

			event := &DNSEvent{
				Timestamp:    time.Now(),
				EventType:    DNSEventTypeQuery,
				QueryName:    domain,
				QueryType:    queryType,
				QueryID:      uint32(rand.Intn(65535)),
				ClientIP:     fmt.Sprintf("10.0.0.%d", rand.Intn(255)),
				ServerIP:     "8.8.8.8",
				ClientPort:   uint16(30000 + rand.Intn(10000)),
				ServerPort:   53,
				Protocol:     DNSProtocolUDP,
				Success:      success,
				ResponseCode: DNSResponseCodeNoError,
				LatencyMs:    uint32(rand.Intn(100)),
				PID:          uint32(1000 + rand.Intn(1000)),
				TID:          uint32(1000 + rand.Intn(1000)),
				ContainerID:  fmt.Sprintf("mock-container-%d", rand.Intn(10)),
				PodUID:       fmt.Sprintf("mock-pod-%d", rand.Intn(10)),
			}

			if !success {
				event.ResponseCode = DNSResponseCodeNXDomain
				event.EventType = DNSEventTypeError
			}

			// Convert to CollectorEvent
			collectorEvent := &domain.CollectorEvent{
				Type:      domain.EventTypeDNS,
				Timestamp: event.Timestamp,
				Source:    c.name,
				Priority:  domain.PriorityNormal,
				Data:      event,
				Metadata: domain.EventMetadata{
					Component: c.name,
					Host:      "mock-host",
					Attributes: map[string]string{
						"mock":  "true",
						"query": domain,
					},
				},
			}

			// Send event
			select {
			case c.events <- collectorEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(c.ctx, 1)
				}
				c.updateStats(1, 0, 0)
				c.logger.Debug("Generated mock DNS event",
					zap.String("query", domain),
					zap.String("type", string(queryType)),
				)
			case <-c.ctx.Done():
				return
			}
		}
	}
}

// startEBPF initializes eBPF monitoring
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "dns.ebpf.start")
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := &bpf.DnsMonitorObjects{}
	if err := bpf.LoadDnsMonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &ebpfState{objs: objs}

	// Dynamically detect network interfaces and attach XDP
	interfaces, err := c.getActiveNetworkInterfaces()
	if err != nil {
		c.logger.Warn("Failed to get network interfaces, trying defaults",
			zap.Error(err))
		interfaces = []string{"eth0", "ens33", "enp0s3", "wlan0", "docker0", "br0"}
	}

	var attachedLinks []link.Link
	var attachedInterfaces []string

	for _, iface := range interfaces {
		iface_obj, err := net.InterfaceByName(iface)
		if err != nil {
			c.logger.Debug("Failed to get interface object",
				zap.String("interface", iface),
				zap.Error(err))
			continue
		}

		xdpLink, err := link.AttachXDP(link.XDPOptions{
			Interface: iface_obj.Index,
			Program:   objs.XdpDnsMonitor,
			Flags:     0,
		})
		if err != nil {
			c.logger.Debug("Failed to attach XDP to interface",
				zap.String("interface", iface),
				zap.Error(err))
			continue
		}
		attachedLinks = append(attachedLinks, xdpLink)
		attachedInterfaces = append(attachedInterfaces, iface)
	}

	if len(attachedLinks) == 0 {
		objs.Close()
		return fmt.Errorf("failed to attach XDP DNS monitor to any interface")
	}

	c.ebpfState.reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		for _, l := range attachedLinks {
			l.Close()
		}
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	c.ebpfState.links = attachedLinks
	c.stats.EBPFAttached = true

	c.logger.Info("DNS eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(attachedLinks)),
		zap.Strings("interfaces", attachedInterfaces),
	)

	return nil
}

// stopEBPF cleans up eBPF resources
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	// Close reader
	if c.ebpfState.reader != nil {
		c.ebpfState.reader.Close()
	}

	// Close all links
	for _, link := range c.ebpfState.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if c.ebpfState.objs != nil {
		c.ebpfState.objs.Close()
	}

	c.logger.Info("DNS eBPF monitoring stopped", zap.String("collector", c.name))
}

// readEBPFEvents processes eBPF ring buffer events
func (c *Collector) readEBPFEvents() {
	if c.ebpfState == nil || c.ebpfState.reader == nil {
		return
	}

	ctx := c.ctx
	for {
		select {
		case <-ctx.Done():
			return
		default:
			record, err := c.ebpfState.reader.Read()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the eBPF event
			if len(record.RawSample) < int(unsafe.Sizeof(BPFDNSEvent{})) {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				continue
			}

			var bpfEvent BPFDNSEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &bpfEvent); err != nil {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.logger.Error("Failed to parse BPF DNS event", zap.Error(err))
				continue
			}

			// Extract and validate DNS query information
			queryName := c.extractQueryName(bpfEvent.QueryName[:])
			if queryName == "" {
				continue
			}

			// Convert BPF event to DNS event
			event := c.convertBPFEventToDNSEvent(&bpfEvent)
			event.QueryName = queryName

			// Log interesting DNS queries for debugging
			if c.logger.Core().Enabled(zap.DebugLevel) {
				c.logger.Debug("DNS query captured",
					zap.String("query", queryName),
					zap.Uint16("qtype", bpfEvent.QType),
					zap.String("protocol", c.getProtocolName(bpfEvent.Protocol)),
					zap.Uint32("pid", bpfEvent.PID),
				)
			}

			// Convert to CollectorEvent
			collectorEvent := &domain.CollectorEvent{
				Type:      domain.EventTypeDNS,
				Timestamp: event.Timestamp,
				Source:    c.name,
				Priority:  c.calculateEventPriority(&bpfEvent),
				Data:      event,
				Metadata: domain.EventMetadata{
					Component: c.name,
					Host:      c.getHostname(),
					Attributes: map[string]string{
						"query":        queryName,
						"query_type":   string(event.QueryType),
						"container_id": event.ContainerID,
						"pod_uid":      event.PodUID,
					},
				},
			}

			// Update buffer usage gauge
			if c.bufferUsage != nil {
				c.bufferUsage.Record(ctx, int64(len(c.events)))
			}

			// Send to event channel
			select {
			case c.events <- collectorEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(ctx, 1)
				}
				c.updateStats(1, 0, 0)
			case <-ctx.Done():
				return
			default:
				// Buffer full, drop event
				if c.droppedEvents != nil {
					c.droppedEvents.Add(ctx, 1)
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.updateStats(0, 1, 0)
			}
		}
	}
}

// getActiveNetworkInterfaces returns a list of active network interfaces
func (c *Collector) getActiveNetworkInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("getting network interfaces: %w", err)
	}

	var activeInterfaces []string
	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Check if interface has any addresses
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}

		activeInterfaces = append(activeInterfaces, iface.Name)
	}

	return activeInterfaces, nil
}

// convertBPFEventToDNSEvent converts BPF event structure to DNSEvent
func (c *Collector) convertBPFEventToDNSEvent(bpfEvent *BPFDNSEvent) *DNSEvent {
	event := &DNSEvent{
		Timestamp:  time.Unix(0, int64(bpfEvent.Timestamp)),
		QueryID:    uint32(bpfEvent.DNSID),
		PID:        bpfEvent.PID,
		TID:        bpfEvent.TID,
		CgroupID:   bpfEvent.CgroupID,
		ClientPort: bpfEvent.SrcPort,
		ServerPort: bpfEvent.DstPort,
		LatencyMs:  bpfEvent.LatencyNs / 1000000, // Convert ns to ms
	}

	// Parse event type
	switch bpfEvent.EventType {
	case 1: // DNS_EVENT_QUERY
		event.EventType = DNSEventTypeQuery
	case 2: // DNS_EVENT_RESPONSE
		event.EventType = DNSEventTypeResponse
		event.ResponseCode = DNSResponseCode(bpfEvent.Rcode)
		event.Success = bpfEvent.Rcode == 0
	case 3: // DNS_EVENT_TIMEOUT
		event.EventType = DNSEventTypeTimeout
	case 4: // DNS_EVENT_ERROR
		event.EventType = DNSEventTypeError
	}

	// Parse protocol
	if bpfEvent.Protocol == 17 { // IPPROTO_UDP
		event.Protocol = DNSProtocolUDP
	} else if bpfEvent.Protocol == 6 { // IPPROTO_TCP
		event.Protocol = DNSProtocolTCP
	}

	// Parse query type
	event.QueryType = c.parseQueryType(bpfEvent.QType)

	// Parse IP addresses based on version
	if bpfEvent.IPVersion == 4 {
		srcIP := net.IP(bpfEvent.SrcAddr[:4])
		dstIP := net.IP(bpfEvent.DstAddr[:4])
		event.ClientIP = srcIP.String()
		event.ServerIP = dstIP.String()
	} else if bpfEvent.IPVersion == 6 {
		srcIP := net.IP(bpfEvent.SrcAddr[:])
		dstIP := net.IP(bpfEvent.DstAddr[:])
		event.ClientIP = srcIP.String()
		event.ServerIP = dstIP.String()
	}

	// Extract container ID from cgroup ID
	if bpfEvent.CgroupID != 0 {
		event.ContainerID = c.extractContainerID(bpfEvent.CgroupID)
		cgroupPath := c.getCgroupPath(bpfEvent.CgroupID)
		event.PodUID = c.extractPodUID(cgroupPath)
	}

	return event
}

// parseQueryType converts numeric query type to enum
func (c *Collector) parseQueryType(qtype uint16) DNSQueryType {
	switch qtype {
	case 1:
		return DNSQueryTypeA
	case 28:
		return DNSQueryTypeAAAA
	case 5:
		return DNSQueryTypeCNAME
	case 15:
		return DNSQueryTypeMX
	case 2:
		return DNSQueryTypeNS
	case 12:
		return DNSQueryTypePTR
	case 6:
		return DNSQueryTypeSOA
	case 16:
		return DNSQueryTypeTXT
	case 33:
		return DNSQueryTypeSRV
	default:
		return DNSQueryTypeOther
	}
}

// calculateEventPriority calculates event priority
func (c *Collector) calculateEventPriority(bpfEvent *BPFDNSEvent) domain.EventPriority {
	if bpfEvent == nil {
		return domain.PriorityNormal
	}

	// Check for DNS failures
	if bpfEvent.Rcode != 0 {
		return domain.PriorityHigh
	}

	// Check for slow queries (>100ms)
	if bpfEvent.LatencyNs > 100*1000*1000 {
		return domain.PriorityHigh
	}

	return domain.PriorityNormal
}

// getHostname returns the current hostname
func (c *Collector) getHostname() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		return "unknown"
	}
	return hostname
}

// updateStats updates internal statistics
func (c *Collector) updateStats(eventsProcessed, eventsDropped, errorCount int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.EventsProcessed += eventsProcessed
	c.stats.EventsDropped += eventsDropped
	c.stats.ErrorCount += errorCount
	c.stats.BufferUtilization = float64(len(c.events)) / float64(cap(c.events))
	c.stats.LastEventTime = time.Now()
}

// GetDNSStats returns DNS-specific statistics
func (c *Collector) GetDNSStats() *DNSStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to avoid race conditions
	return &DNSStats{
		EventsProcessed:   c.stats.EventsProcessed,
		EventsDropped:     c.stats.EventsDropped,
		ErrorCount:        c.stats.ErrorCount,
		BufferUtilization: c.stats.BufferUtilization,
		EBPFAttached:      c.stats.EBPFAttached,
		LastEventTime:     c.stats.LastEventTime,
	}
}

// Helper methods for cgroup/container extraction

// extractContainerID extracts container ID from cgroup ID
func (c *Collector) extractContainerID(cgroupID uint64) string {
	if cgroupID == 0 {
		return ""
	}
	cgroupPath := c.getCgroupPath(cgroupID)
	return c.parseContainerIDFromPath(cgroupPath)
}

// getCgroupPath gets cgroup path for a given cgroup ID
func (c *Collector) getCgroupPath(cgroupID uint64) string {
	if cgroupID == 0 {
		return ""
	}
	// In real implementation, this would read from /proc/self/mountinfo
	// and resolve the actual cgroup path
	return fmt.Sprintf("/sys/fs/cgroup/unified/%d", cgroupID)
}

// parseContainerIDFromPath parses container ID from cgroup path
func (c *Collector) parseContainerIDFromPath(path string) string {
	// Look for Docker or containerd patterns in path
	// Example: /docker/abc123def456.../
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "docker" && i+1 < len(parts) {
			containerID := parts[i+1]
			if len(containerID) >= 12 {
				return containerID[:12] // Return first 12 chars
			}
		}
	}
	return ""
}

// extractPodUID extracts pod UID from cgroup path
func (c *Collector) extractPodUID(cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}

	// Look for pod UID pattern in path: /kubepods/pod12345678_1234_1234_1234_123456789012/
	parts := strings.Split(cgroupPath, "/")

	for _, part := range parts {
		if strings.HasPrefix(part, "pod") && len(part) > 3 {
			podUID := part[3:] // Remove "pod" prefix

			// Validate it looks like a UID
			if strings.Contains(podUID, "_") && len(podUID) >= 32 {
				// Convert underscores to hyphens for Kubernetes UID format
				return strings.ReplaceAll(podUID, "_", "-")
			}
		}
	}

	return ""
}
