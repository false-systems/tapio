//go:build linux
// +build linux

package network

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// IntelligenceCollector extends the base network collector with intelligence-focused L7 monitoring
type IntelligenceCollector struct {
	*Collector // Embed base collector

	// Intelligence-specific fields
	intelConfig         *IntelligenceCollectorConfig
	intelStats          *IntelligenceCollectorStats
	intelligenceEvents  chan *IntelligenceEvent
	serviceDependencies map[string]*ServiceDependency
	latencyBaselines    map[string]*LatencyBaseline
	errorCascadeTracker map[string]*ErrorCascade

	// OpenTelemetry for intelligence metrics
	intelTracer          trace.Tracer
	serviceDepsCounter   metric.Int64Counter
	errorPatternsCounter metric.Int64Counter
	anomaliesCounter     metric.Int64Counter
	dnsFailuresCounter   metric.Int64Counter
	filteringRatio       metric.Float64Gauge
}

// LatencyBaseline tracks latency patterns for anomaly detection
type LatencyBaseline struct {
	Endpoint     string
	AvgLatency   time.Duration
	RequestCount int64
	LastUpdate   time.Time
	StandardDev  time.Duration
}

// ErrorCascade tracks error patterns across services
type ErrorCascade struct {
	WindowStart time.Time
	ErrorCount  int32
	Services    map[string]int32
	StatusCodes map[int32]int32
}

// NewIntelligenceCollector creates a new intelligence-focused network collector
func NewIntelligenceCollector(name string, config *IntelligenceCollectorConfig, logger *zap.Logger) (*IntelligenceCollector, error) {
	if config == nil {
		config = DefaultIntelligenceConfig()
	}

	// Create base network collector
	baseCollector, err := NewCollector(name, config.NetworkCollectorConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("creating base network collector: %w", err)
	}

	// Initialize intelligence-specific metrics
	tracer := otel.Tracer(fmt.Sprintf("%s-intelligence", name))
	meter := otel.Meter(fmt.Sprintf("%s-intelligence", name))

	serviceDepsCounter, err := meter.Int64Counter(
		fmt.Sprintf("%s_service_dependencies_discovered_total", name),
		metric.WithDescription("Total service dependencies discovered"),
	)
	if err != nil {
		logger.Warn("Failed to create service dependencies counter", zap.Error(err))
	}

	errorPatternsCounter, err := meter.Int64Counter(
		fmt.Sprintf("%s_error_patterns_detected_total", name),
		metric.WithDescription("Total error patterns detected"),
	)
	if err != nil {
		logger.Warn("Failed to create error patterns counter", zap.Error(err))
	}

	anomaliesCounter, err := meter.Int64Counter(
		fmt.Sprintf("%s_latency_anomalies_detected_total", name),
		metric.WithDescription("Total latency anomalies detected"),
	)
	if err != nil {
		logger.Warn("Failed to create anomalies counter", zap.Error(err))
	}

	dnsFailuresCounter, err := meter.Int64Counter(
		fmt.Sprintf("%s_dns_failures_detected_total", name),
		metric.WithDescription("Total DNS failures detected"),
	)
	if err != nil {
		logger.Warn("Failed to create DNS failures counter", zap.Error(err))
	}

	filteringRatio, err := meter.Float64Gauge(
		fmt.Sprintf("%s_intelligence_filtering_ratio", name),
		metric.WithDescription("Ratio of events filtered for intelligence (higher = more selective)"),
	)
	if err != nil {
		logger.Warn("Failed to create filtering ratio gauge", zap.Error(err))
	}

	ic := &IntelligenceCollector{
		Collector:            baseCollector,
		intelConfig:          config,
		intelStats:           &IntelligenceCollectorStats{},
		intelligenceEvents:   make(chan *IntelligenceEvent, config.BufferSize),
		serviceDependencies:  make(map[string]*ServiceDependency),
		latencyBaselines:     make(map[string]*LatencyBaseline),
		errorCascadeTracker:  make(map[string]*ErrorCascade),
		intelTracer:          tracer,
		serviceDepsCounter:   serviceDepsCounter,
		errorPatternsCounter: errorPatternsCounter,
		anomaliesCounter:     anomaliesCounter,
		dnsFailuresCounter:   dnsFailuresCounter,
		filteringRatio:       filteringRatio,
	}

	return ic, nil
}

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

// Start starts the intelligence-focused network collector
func (ic *IntelligenceCollector) Start(ctx context.Context) error {
	ctx, span := ic.intelTracer.Start(ctx, "intelligence-collector.start")
	defer span.End()

	ic.logger.Info("Starting intelligence-focused network collector",
		zap.String("collector", ic.name),
		zap.Bool("intelligence_mode", ic.intelConfig.EnableIntelligenceMode),
	)

	// Start intelligence event processing goroutine
	ic.wg.Add(1)
	go ic.processIntelligenceEvents()

	// Start the base collector (which handles eBPF setup)
	if err := ic.Collector.Start(ctx); err != nil {
		return fmt.Errorf("starting base collector: %w", err)
	}

	return nil
}

// Stop stops the intelligence collector
func (ic *IntelligenceCollector) Stop() error {
	ic.logger.Info("Stopping intelligence-focused network collector")

	// Stop base collector first
	if err := ic.Collector.Stop(); err != nil {
		ic.logger.Error("Error stopping base collector", zap.Error(err))
	}

	// Close intelligence events channel
	close(ic.intelligenceEvents)

	return nil
}

// processIntelligenceEvents processes intelligence events and performs analysis
func (ic *IntelligenceCollector) processIntelligenceEvents() {
	defer ic.wg.Done()

	ticker := time.NewTicker(time.Duration(ic.intelConfig.FlushInterval))
	defer ticker.Stop()

	for {
		select {
		case <-ic.ctx.Done():
			return
		case event := <-ic.intelligenceEvents:
			if event == nil {
				return // Channel closed
			}
			ic.analyzeIntelligenceEvent(event)
		case <-ticker.C:
			ic.performPeriodicAnalysis()
		}
	}
}

// analyzeIntelligenceEvent analyzes a single intelligence event
func (ic *IntelligenceCollector) analyzeIntelligenceEvent(event *IntelligenceEvent) {
	ctx, span := ic.intelTracer.Start(ic.ctx, "intelligence.analyze_event")
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", fmt.Sprintf("%d", event.Type)),
		attribute.String("event.severity", fmt.Sprintf("%d", event.Severity)),
		attribute.String("source.service", event.SourceService),
		attribute.String("dest.service", event.DestService),
	)

	ic.intelStats.TotalEventsProcessed++

	switch event.Type {
	case IntelEventServiceDependency:
		ic.handleServiceDependency(ctx, event)
	case IntelEventErrorPattern:
		ic.handleErrorPattern(ctx, event)
	case IntelEventLatencyAnomaly:
		ic.handleLatencyAnomaly(ctx, event)
	case IntelEventDNSFailure:
		ic.handleDNSFailure(ctx, event)
	case IntelEventSecurityConcern:
		ic.handleSecurityConcern(ctx, event)
	default:
		ic.logger.Warn("Unknown intelligence event type",
			zap.Uint32("type", uint32(event.Type)))
	}

	// Update filtering efficiency metrics
	if ic.intelStats.TotalEventsProcessed > 0 {
		efficiency := float64(ic.intelStats.IntelligentEventsEmitted) / float64(ic.intelStats.TotalEventsProcessed)
		if ic.filteringRatio != nil {
			ic.filteringRatio.Record(ctx, 1.0-efficiency) // Higher is more selective
		}
		ic.intelStats.FilteringEfficiency = (1.0 - efficiency) * 100
	}

	// Send to event processor if configured
	if ic.eventProcessor != nil {
		domainEvent := ic.convertIntelligenceEventToDomain(event)
		if domainEvent != nil {
			if err := ic.eventProcessor.Process(ctx, domainEvent); err != nil {
				ic.logger.Error("Failed to process intelligence event",
					zap.Error(err),
					zap.String("event_id", event.EventID),
				)
			}
		}
	}
}

// handleServiceDependency processes service dependency discoveries
func (ic *IntelligenceCollector) handleServiceDependency(ctx context.Context, event *IntelligenceEvent) {
	if event.ServiceDependency == nil {
		return
	}

	dep := event.ServiceDependency
	key := fmt.Sprintf("%s->%s:%d", dep.SourceService, dep.DestService, dep.DestPort)

	// Track or update service dependency
	if existing, exists := ic.serviceDependencies[key]; exists {
		existing.LastSeen = dep.LastSeen
		existing.RequestCount += dep.RequestCount
		existing.ErrorCount += dep.ErrorCount
	} else {
		ic.serviceDependencies[key] = dep
		ic.intelStats.NewServicesDiscovered++

		if ic.serviceDepsCounter != nil {
			ic.serviceDepsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("source_service", dep.SourceService),
				attribute.String("dest_service", dep.DestService),
				attribute.Int("dest_port", int(dep.DestPort)),
			))
		}
	}

	ic.intelStats.ServiceDependencies++
	ic.intelStats.IntelligentEventsEmitted++

	ic.logger.Debug("Service dependency discovered",
		zap.String("source", dep.SourceService),
		zap.String("destination", dep.DestService),
		zap.Int32("port", dep.DestPort),
		zap.Bool("new", dep.IsNewService),
	)
}

// handleErrorPattern processes HTTP error patterns
func (ic *IntelligenceCollector) handleErrorPattern(ctx context.Context, event *IntelligenceEvent) {
	if event.ErrorPattern == nil {
		return
	}

	error := event.ErrorPattern

	// Check for error cascades
	windowKey := fmt.Sprintf("cascade-%d", error.Timestamp.Truncate(time.Duration(ic.intelConfig.ErrorCascadeWindowMs)*time.Millisecond).Unix())

	cascade, exists := ic.errorCascadeTracker[windowKey]
	if !exists {
		cascade = &ErrorCascade{
			WindowStart: error.Timestamp.Truncate(time.Duration(ic.intelConfig.ErrorCascadeWindowMs) * time.Millisecond),
			Services:    make(map[string]int32),
			StatusCodes: make(map[int32]int32),
		}
		ic.errorCascadeTracker[windowKey] = cascade
	}

	cascade.ErrorCount++
	cascade.Services[error.SourceService]++
	cascade.StatusCodes[error.StatusCode]++

	// Detect cascade if multiple services are affected
	if len(cascade.Services) > 2 {
		error.IsCascade = true
		ic.intelStats.ErrorCascadesDetected++
	}

	if ic.errorPatternsCounter != nil {
		ic.errorPatternsCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("source_service", error.SourceService),
			attribute.String("dest_service", error.DestService),
			attribute.Int("status_code", int(error.StatusCode)),
			attribute.Bool("is_cascade", error.IsCascade),
		))
	}

	ic.intelStats.ErrorPatterns++
	ic.intelStats.IntelligentEventsEmitted++

	severity := "warning"
	if error.StatusCode >= 500 {
		severity = "critical"
	}

	ic.logger.Info("Error pattern detected",
		zap.String("source", error.SourceService),
		zap.String("destination", error.DestService),
		zap.String("endpoint", error.Endpoint),
		zap.Int32("status_code", error.StatusCode),
		zap.String("severity", severity),
		zap.Bool("cascade", error.IsCascade),
	)
}

// handleLatencyAnomaly processes latency anomalies
func (ic *IntelligenceCollector) handleLatencyAnomaly(ctx context.Context, event *IntelligenceEvent) {
	if event.LatencyAnomaly == nil {
		return
	}

	anomaly := event.LatencyAnomaly
	baselineKey := fmt.Sprintf("%s:%s", anomaly.DestService, anomaly.Endpoint)

	// Update latency baseline
	baseline, exists := ic.latencyBaselines[baselineKey]
	if !exists {
		baseline = &LatencyBaseline{
			Endpoint:     anomaly.Endpoint,
			AvgLatency:   anomaly.BaselineLatency,
			RequestCount: 1,
			LastUpdate:   anomaly.Timestamp,
		}
		ic.latencyBaselines[baselineKey] = baseline
		ic.intelStats.LatencyBaselinesTracked++
	}

	// Update baseline with exponential moving average
	alpha := 0.1 // Smoothing factor
	baseline.AvgLatency = time.Duration(float64(baseline.AvgLatency)*alpha + float64(anomaly.Latency)*(1-alpha))
	baseline.RequestCount++
	baseline.LastUpdate = anomaly.Timestamp

	if ic.anomaliesCounter != nil {
		ic.anomaliesCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("source_service", anomaly.SourceService),
			attribute.String("dest_service", anomaly.DestService),
			attribute.String("endpoint", anomaly.Endpoint),
			attribute.Float64("deviation_factor", anomaly.DeviationFactor),
		))
	}

	ic.intelStats.LatencyAnomalies++
	ic.intelStats.IntelligentEventsEmitted++

	ic.logger.Info("Latency anomaly detected",
		zap.String("source", anomaly.SourceService),
		zap.String("destination", anomaly.DestService),
		zap.String("endpoint", anomaly.Endpoint),
		zap.Duration("latency", anomaly.Latency),
		zap.Duration("baseline", anomaly.BaselineLatency),
		zap.Float64("deviation", anomaly.DeviationFactor),
	)
}

// handleDNSFailure processes DNS failures
func (ic *IntelligenceCollector) handleDNSFailure(ctx context.Context, event *IntelligenceEvent) {
	if event.DNSFailure == nil {
		return
	}

	failure := event.DNSFailure

	if ic.dnsFailuresCounter != nil {
		ic.dnsFailuresCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("source_service", failure.SourceService),
			attribute.String("domain", failure.Domain),
			attribute.Int("response_code", int(failure.ResponseCode)),
		))
	}

	ic.intelStats.DNSFailures++
	ic.intelStats.IntelligentEventsEmitted++

	ic.logger.Warn("DNS failure detected",
		zap.String("source", failure.SourceService),
		zap.String("domain", failure.Domain),
		zap.Int32("response_code", failure.ResponseCode),
		zap.String("response_text", failure.ResponseText),
	)
}

// handleSecurityConcern processes security-related events
func (ic *IntelligenceCollector) handleSecurityConcern(ctx context.Context, event *IntelligenceEvent) {
	if event.SecurityConcern == nil {
		return
	}

	concern := event.SecurityConcern
	ic.intelStats.SecurityConcerns++
	ic.intelStats.IntelligentEventsEmitted++

	ic.logger.Warn("Security concern detected",
		zap.String("source", concern.SourceService),
		zap.String("destination", concern.DestService),
		zap.String("type", concern.ConcernType),
		zap.String("description", concern.Description),
		zap.String("severity", concern.Severity),
	)
}

// performPeriodicAnalysis performs periodic analysis and cleanup
func (ic *IntelligenceCollector) performPeriodicAnalysis() {
	now := time.Now()

	// Clean up old error cascade windows
	for key, cascade := range ic.errorCascadeTracker {
		if now.Sub(cascade.WindowStart) > time.Duration(ic.intelConfig.ErrorCascadeWindowMs)*time.Millisecond*2 {
			delete(ic.errorCascadeTracker, key)
		}
	}

	// Update intelligence events per second
	if ic.intelStats.TotalEventsProcessed > 0 {
		// This is a simplified calculation - in production would use a proper windowed rate
		ic.intelStats.IntelligenceEventsPerSec = float64(ic.intelStats.IntelligentEventsEmitted) / 60.0
	}
}

// convertIntelligenceEventToDomain converts an intelligence event to a domain event
func (ic *IntelligenceCollector) convertIntelligenceEventToDomain(intelEvent *IntelligenceEvent) *domain.CollectorEvent {
	var eventType domain.CollectorEventType
	var severity domain.EventSeverity

	// Map intelligence event type to domain event type
	switch intelEvent.Type {
	case IntelEventServiceDependency:
		eventType = domain.EventTypeKernelNetwork
		severity = domain.SeverityInfo
	case IntelEventErrorPattern:
		eventType = domain.EventTypeHTTP
		severity = domain.SeverityWarning
		if intelEvent.ErrorPattern != nil && intelEvent.ErrorPattern.StatusCode >= 500 {
			severity = domain.SeverityCritical
		}
	case IntelEventLatencyAnomaly:
		eventType = domain.EventTypeTCP
		severity = domain.SeverityWarning
	case IntelEventDNSFailure:
		eventType = domain.EventTypeDNS
		severity = domain.SeverityWarning
	case IntelEventSecurityConcern:
		eventType = domain.EventTypeKernelSyscall
		severity = domain.SeverityCritical
	default:
		eventType = domain.EventTypeKernelNetwork
		severity = domain.SeverityInfo
	}

	// Create intelligence-focused network data
	networkData := &domain.NetworkData{
		Protocol:  intelEvent.Protocol,
		Direction: "outbound", // Intelligence events are typically outbound
		SourceIP:  intelEvent.SourceIP,
		DestIP:    intelEvent.DestIP,
	}

	if intelEvent.SourcePort > 0 {
		networkData.SourcePort = intelEvent.SourcePort
	}
	if intelEvent.DestPort > 0 {
		networkData.DestPort = intelEvent.DestPort
	}

	event := &domain.CollectorEvent{
		EventID:   intelEvent.EventID,
		Timestamp: intelEvent.Timestamp,
		Type:      eventType,
		Source:    ic.name,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Network: networkData,
		},
		Metadata: domain.EventMetadata{
			PID:      intelEvent.ProcessID,
			CgroupID: intelEvent.CgroupID,
			PodUID:   intelEvent.PodUID,
		},
	}

	// Add intelligence-specific context
	if intelEvent.AnalysisContext == nil {
		intelEvent.AnalysisContext = make(map[string]string)
	}
	intelEvent.AnalysisContext["intelligence_type"] = fmt.Sprintf("%d", intelEvent.Type)
	intelEvent.AnalysisContext["source_service"] = intelEvent.SourceService
	intelEvent.AnalysisContext["dest_service"] = intelEvent.DestService

	// Add intelligence-specific data based on event type
	if intelEvent.ErrorPattern != nil {
		if event.EventData.HTTP == nil {
			event.EventData.HTTP = &domain.HTTPData{}
		}
		event.EventData.HTTP.StatusCode = intelEvent.ErrorPattern.StatusCode
		event.EventData.HTTP.URL = intelEvent.ErrorPattern.Endpoint
		event.EventData.HTTP.Method = ic.getMethodString(intelEvent.ErrorPattern.Method)
		intelEvent.AnalysisContext["is_error_cascade"] = fmt.Sprintf("%v", intelEvent.ErrorPattern.IsCascade)
	}

	if intelEvent.LatencyAnomaly != nil {
		networkData.Latency = intelEvent.LatencyAnomaly.Latency
		intelEvent.AnalysisContext["baseline_latency_ms"] = fmt.Sprintf("%d", intelEvent.LatencyAnomaly.BaselineLatency.Milliseconds())
		intelEvent.AnalysisContext["deviation_factor"] = fmt.Sprintf("%.2f", intelEvent.LatencyAnomaly.DeviationFactor)
	}

	if intelEvent.DNSFailure != nil {
		if event.EventData.DNS == nil {
			event.EventData.DNS = &domain.DNSData{}
		}
		event.EventData.DNS.QueryName = intelEvent.DNSFailure.Domain
		event.EventData.DNS.ResponseCode = int(intelEvent.DNSFailure.ResponseCode)
		intelEvent.AnalysisContext["dns_response_text"] = intelEvent.DNSFailure.ResponseText
	}

	return event
}

// getMethodString converts method number to string
func (ic *IntelligenceCollector) getMethodString(method string) string {
	// This would map method codes to strings - simplified for now
	return method
}

// GetIntelligenceStats returns intelligence collector statistics
func (ic *IntelligenceCollector) GetIntelligenceStats() *IntelligenceCollectorStats {
	ic.mutex.RLock()
	defer ic.mutex.RUnlock()

	// Make a copy to avoid race conditions
	statsCopy := *ic.intelStats
	return &statsCopy
}

// GetServiceDependencies returns discovered service dependencies
func (ic *IntelligenceCollector) GetServiceDependencies() map[string]*ServiceDependency {
	ic.mutex.RLock()
	defer ic.mutex.RUnlock()

	// Make a copy to avoid race conditions
	deps := make(map[string]*ServiceDependency)
	for k, v := range ic.serviceDependencies {
		depCopy := *v
		deps[k] = &depCopy
	}
	return deps
}

// extractString safely extracts a null-terminated string from byte array
func extractString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}
