package dns

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config for DNS collector focused on operational monitoring
type Config struct {
	Name                  string               `json:"name"`
	BufferSize            int                  `json:"buffer_size"`
	EnableEBPF            bool                 `json:"enable_ebpf"`
	XDPInterfaces         []string             `json:"xdp_interfaces,omitempty"` // Specific interfaces for XDP
	CircuitBreakerConfig  CircuitBreakerConfig `json:"circuit_breaker_config"`
	ContainerIDExtraction bool                 `json:"container_id_extraction"` // Enable container ID parsing
	ParseAnswers          bool                 `json:"parse_answers"`           // Parse DNS answers for resolved IPs
	Labels                map[string]string    `json:"labels,omitempty"`        // Labels to add to all events
}

// DefaultConfig returns sensible defaults for operational monitoring
func DefaultConfig() Config {
	return Config{
		Name:                  "dns",
		BufferSize:            10000,
		EnableEBPF:            true,
		XDPInterfaces:         nil, // Auto-detect if nil
		CircuitBreakerConfig:  DefaultCircuitBreakerConfig(),
		ContainerIDExtraction: true,
		ParseAnswers:          true,
	}
}

// RawDNSEvent represents the DNS event from eBPF - must match C struct exactly
type RawDNSEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint8
	Protocol  uint8
	SrcPort   uint16
	DstPort   uint16
	QueryName [128]byte
	Data      [512]byte
}

// Collector implements DNS monitoring via eBPF with operational focus
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

	// eBPF components (platform-specific)
	ebpfState *eBPFState // Linux-specific eBPF state

	// Error handling
	consecutiveErrors int
	errorLogInterval  time.Time

	// Event processing
	events chan *domain.CollectorEvent

	// Fault tolerance
	circuitBreaker *CircuitBreaker

	// Container tracking
	containerCache map[uint64]string // cgroup_id -> container_id
	cacheMutex     sync.RWMutex      // Separate mutex for cache

	// OpenTelemetry metrics - focused on operational metrics
	tracer             trace.Tracer
	eventsProcessed    metric.Int64Counter
	errorsTotal        metric.Int64Counter
	processingTime     metric.Float64Histogram
	bufferUsage        metric.Int64Gauge
	droppedEvents      metric.Int64Counter
	dnsLatency         metric.Float64Histogram
	dnsFailures        metric.Int64Counter
	circuitBreakerHits metric.Int64Counter
}

// NewCollector creates a new DNS collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	// Initialize logger if not provided
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL components for operational metrics
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Essential operational metrics
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

	// DNS-specific operational metrics
	dnsLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_dns_latency_ms", name),
		metric.WithDescription(fmt.Sprintf("DNS query latency in milliseconds for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create DNS latency histogram", zap.Error(err))
	}

	dnsFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_dns_failures_total", name),
		metric.WithDescription(fmt.Sprintf("Total DNS failures for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create DNS failures counter", zap.Error(err))
	}

	circuitBreakerHits, err := meter.Int64Counter(
		fmt.Sprintf("%s_circuit_breaker_hits_total", name),
		metric.WithDescription(fmt.Sprintf("Total circuit breaker hits for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create circuit breaker hits counter", zap.Error(err))
	}

	// Initialize circuit breaker for fault tolerance
	circuitBreaker := NewCircuitBreaker(cfg.CircuitBreakerConfig, logger)

	collector := &Collector{
		name:               name,
		logger:             logger,
		config:             cfg,
		stats:              &DNSStats{},
		events:             make(chan *domain.CollectorEvent, cfg.BufferSize),
		circuitBreaker:     circuitBreaker,
		containerCache:     make(map[uint64]string),
		tracer:             tracer,
		eventsProcessed:    eventsProcessed,
		errorsTotal:        errorsTotal,
		processingTime:     processingTime,
		bufferUsage:        bufferUsage,
		droppedEvents:      droppedEvents,
		dnsLatency:         dnsLatency,
		dnsFailures:        dnsFailures,
		circuitBreakerHits: circuitBreakerHits,
	}

	return collector, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "dns.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	if !c.config.EnableEBPF {
		c.healthy = true
		c.logger.Info("eBPF disabled, collector running without event generation")
		return nil
	}

	// Start eBPF monitoring using platform-specific implementation
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
	c.stopEBPF()

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
			message = "DNS collector actively monitoring"
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
		},
	}
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

// Platform-agnostic stubs for testing

// extractContainerID extracts container ID from cgroup ID with caching
func (c *Collector) extractContainerID(cgroupID uint64) string {
	if cgroupID == 0 {
		return ""
	}

	// Check cache first
	c.cacheMutex.RLock()
	if containerID, exists := c.containerCache[cgroupID]; exists {
		c.cacheMutex.RUnlock()
		return containerID
	}
	c.cacheMutex.RUnlock()

	// Extract from cgroup path
	cgroupPath := c.getCgroupPath(cgroupID)
	containerID := c.parseContainerIDFromPath(cgroupPath)

	// Cache the result
	c.cacheMutex.Lock()
	if c.containerCache == nil {
		c.containerCache = make(map[uint64]string)
	}
	c.containerCache[cgroupID] = containerID
	// Prevent cache from growing too large
	if len(c.containerCache) > 10000 {
		c.cleanupContainerCache()
	}
	c.cacheMutex.Unlock()

	return containerID
}

// getCgroupPath gets cgroup path for a given cgroup ID
func (c *Collector) getCgroupPath(cgroupID uint64) string {
	if cgroupID == 0 {
		return ""
	}

	// Try to read cgroup path from procfs
	// This would be implemented differently on different platforms
	// For now, construct a likely path based on cgroup ID
	return fmt.Sprintf("/sys/fs/cgroup/unified/%d", cgroupID)
}

// parseContainerIDFromPath parses container ID from cgroup path
func (c *Collector) parseContainerIDFromPath(path string) string {
	if path == "" {
		return ""
	}

	// Try different container runtime patterns
	containerID := c.parseDockerContainerID(path)
	if containerID != "" {
		return containerID
	}

	containerID = c.parseContainerdContainerID(path)
	if containerID != "" {
		return containerID
	}

	containerID = c.parseCRIOContainerID(path)
	if containerID != "" {
		return containerID
	}

	return ""
}

// parseDockerContainerID extracts Docker container ID from cgroup path
func (c *Collector) parseDockerContainerID(path string) string {
	// Docker pattern: /docker/CONTAINERID or /system.slice/docker-CONTAINERID.scope
	patterns := []string{
		"/docker/",
		"docker-",
		"/docker.service/",
	}

	for _, pattern := range patterns {
		if idx := strings.Index(path, pattern); idx != -1 {
			start := idx + len(pattern)
			if start >= len(path) {
				continue
			}

			// Extract container ID (typically 64 hex characters)
			end := start
			for end < len(path) && (isHexChar(path[end]) || path[end] == '-') {
				end++
			}

			if containerID := path[start:end]; len(containerID) >= 12 {
				// Remove any trailing suffixes like .scope
				if idx := strings.Index(containerID, "."); idx != -1 {
					containerID = containerID[:idx]
				}
				// Docker container IDs are usually 64 characters, but accept 12+
				if len(containerID) >= 12 && isValidContainerID(containerID) {
					return containerID
				}
			}
		}
	}
	return ""
}

// parseContainerdContainerID extracts containerd container ID from cgroup path
func (c *Collector) parseContainerdContainerID(path string) string {
	// containerd pattern: /system.slice/containerd-CONTAINERID.scope
	patterns := []string{
		"containerd-",
		"/containerd/",
		"containerd.service/",
	}

	for _, pattern := range patterns {
		if idx := strings.Index(path, pattern); idx != -1 {
			start := idx + len(pattern)
			if start >= len(path) {
				continue
			}

			// Extract container ID
			end := start
			for end < len(path) && isHexChar(path[end]) {
				end++
			}

			if containerID := path[start:end]; len(containerID) >= 12 && isValidContainerID(containerID) {
				return containerID
			}
		}
	}
	return ""
}

// parseCRIOContainerID extracts CRI-O container ID from cgroup path
func (c *Collector) parseCRIOContainerID(path string) string {
	// CRI-O pattern: /machine.slice/libpod-CONTAINERID.scope or /crio-CONTAINERID.scope
	patterns := []string{
		"libpod-",
		"crio-",
		"/crio/",
	}

	for _, pattern := range patterns {
		if idx := strings.Index(path, pattern); idx != -1 {
			start := idx + len(pattern)
			if start >= len(path) {
				continue
			}

			// Extract container ID
			end := start
			for end < len(path) && (isHexChar(path[end]) || path[end] == '-') {
				end++
			}

			if containerID := path[start:end]; len(containerID) >= 12 {
				// Remove any trailing suffixes
				if idx := strings.Index(containerID, "."); idx != -1 {
					containerID = containerID[:idx]
				}
				if len(containerID) >= 12 && isValidContainerID(containerID) {
					return containerID
				}
			}
		}
	}
	return ""
}

// isHexChar checks if a character is a valid hexadecimal character
func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isValidContainerID validates that a string looks like a valid container ID
func isValidContainerID(id string) bool {
	if len(id) < 12 || len(id) > 64 {
		return false
	}

	// Container IDs are typically hexadecimal
	for _, c := range id {
		if !isHexChar(byte(c)) && c != '-' {
			return false
		}
	}

	return true
}

// cleanupContainerCache removes old entries to prevent memory growth
func (c *Collector) cleanupContainerCache() {
	// Remove half of the cache entries
	// In production, you'd want a more sophisticated LRU eviction
	count := 0
	target := len(c.containerCache) / 2
	for cgroupID := range c.containerCache {
		if count >= target {
			break
		}
		delete(c.containerCache, cgroupID)
		count++
	}
}

// extractPodUID extracts pod UID from cgroup path with enhanced parsing
func (c *Collector) extractPodUID(cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}

	// Kubernetes cgroup patterns:
	// - /kubepods/burstable/pod12345678-1234-1234-1234-123456789012
	// - /kubepods/besteffort/pod12345678_1234_1234_1234_123456789012
	// - /kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod12345678_1234_1234_1234_123456789012.slice
	parts := strings.Split(cgroupPath, "/")

	for _, part := range parts {
		// Look for pod UID patterns
		if podUID := c.extractPodUIDFromPart(part); podUID != "" {
			return podUID
		}
	}

	return ""
}

// extractPodUIDFromPart extracts pod UID from a single path component
func (c *Collector) extractPodUIDFromPart(part string) string {
	// Pattern 1: pod12345678-1234-1234-1234-123456789012
	if strings.HasPrefix(part, "pod") && len(part) > 39 {
		podUID := part[3:] // Remove "pod" prefix
		if c.isValidKubernetesUID(podUID) {
			return podUID
		}
	}

	// Pattern 2: kubepods-burstable-pod12345678_1234_1234_1234_123456789012.slice
	if strings.Contains(part, "pod") {
		if idx := strings.Index(part, "pod"); idx != -1 {
			start := idx + 3 // Skip "pod"
			end := len(part)

			// Find end of UID (before .slice or other suffix)
			for _, suffix := range []string{".slice", ".scope"} {
				if suffixIdx := strings.Index(part[start:], suffix); suffixIdx != -1 {
					end = start + suffixIdx
					break
				}
			}

			if end > start {
				podUID := part[start:end]
				// Convert underscores to hyphens for standard Kubernetes format
				podUID = strings.ReplaceAll(podUID, "_", "-")
				if c.isValidKubernetesUID(podUID) {
					return podUID
				}
			}
		}
	}

	return ""
}

// isValidKubernetesUID validates that a string looks like a valid Kubernetes UID
func (c *Collector) isValidKubernetesUID(uid string) bool {
	if len(uid) != 36 {
		return false
	}

	// Kubernetes UIDs follow UUID format: 8-4-4-4-12 hex digits separated by hyphens
	// Example: 12345678-1234-1234-1234-123456789012
	parts := strings.Split(uid, "-")
	if len(parts) != 5 {
		return false
	}

	// Check lengths: 8, 4, 4, 4, 12
	expectedLengths := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLengths[i] {
			return false
		}
		// Check that each part is hexadecimal
		for _, c := range part {
			if !isHexChar(byte(c)) {
				return false
			}
		}
	}

	return true
}

// calculateEventPriority calculates event priority (stub)
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

// handleReadError handles ring buffer read errors with rate limiting
func (c *Collector) handleReadError(ctx context.Context, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.consecutiveErrors++

	// Rate limit error logging - log every 10 errors or every minute
	if c.consecutiveErrors%10 == 0 || time.Since(c.errorLogInterval) > time.Minute {
		c.logger.Error("Failed to read from ring buffer",
			zap.Error(err),
			zap.Int("consecutive_errors", c.consecutiveErrors),
		)
		c.errorLogInterval = time.Now()
	}

	if c.errorsTotal != nil {
		c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "ring_buffer_read"),
		))
	}
}

// resetErrorCounter resets the consecutive error counter
func (c *Collector) resetErrorCounter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveErrors = 0
}

// handleDroppedEvent handles dropped events due to buffer full
func (c *Collector) handleDroppedEvent(ctx context.Context, queryName string) {
	c.logger.Warn("Event dropped due to full buffer",
		zap.String("query_name", queryName),
		zap.Int("buffer_len", len(c.events)),
		zap.Int("buffer_cap", cap(c.events)),
	)

	c.updateStats(0, 1, 0)
}

// GetEventChannel returns the event channel for reading events
func (c *Collector) GetEventChannel() <-chan *domain.CollectorEvent {
	return c.events
}
