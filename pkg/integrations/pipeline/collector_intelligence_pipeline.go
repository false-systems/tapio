package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/analytics"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// CollectorIntelligencePipeline connects collectors to the intelligence pipeline
type CollectorIntelligencePipeline struct {
	manager            *collectors.Manager
	correlationAdapter *analytics.AnalyticsCorrelationAdapter
	correlationSystem  *correlation.SimpleCorrelationSystem
	logger             *zap.Logger
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	processedEvents    uint64
	correlationErrors  uint64
	enrichmentEnabled  bool
	batchSize          int
	batchTimeout       time.Duration
}

// Config holds configuration for the pipeline
type Config struct {
	EnrichmentEnabled bool
	BatchSize         int
	BatchTimeout      time.Duration
}

// DefaultConfig returns default pipeline configuration
func DefaultConfig() Config {
	return Config{
		EnrichmentEnabled: true,
		BatchSize:         100,
		BatchTimeout:      5 * time.Second,
	}
}

// NewCollectorIntelligencePipeline creates a new pipeline instance
func NewCollectorIntelligencePipeline(
	manager *collectors.Manager,
	logger *zap.Logger,
	config Config,
) (*CollectorIntelligencePipeline, error) {
	if manager == nil {
		return nil, fmt.Errorf("collector manager is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create correlation system
	correlationConfig := correlation.DefaultSimpleSystemConfig()
	correlationSystem := correlation.NewSimpleCorrelationSystem(logger, correlationConfig)

	// Create analytics adapter
	correlationAdapter := analytics.NewAnalyticsCorrelationAdapter(correlationSystem, logger)

	ctx, cancel := context.WithCancel(context.Background())

	return &CollectorIntelligencePipeline{
		manager:            manager,
		correlationAdapter: correlationAdapter,
		correlationSystem:  correlationSystem,
		logger:             logger,
		ctx:                ctx,
		cancel:             cancel,
		enrichmentEnabled:  config.EnrichmentEnabled,
		batchSize:          config.BatchSize,
		batchTimeout:       config.BatchTimeout,
	}, nil
}

// Start begins processing events from collectors through the intelligence pipeline
func (p *CollectorIntelligencePipeline) Start() error {
	// Start the correlation system
	if err := p.correlationAdapter.Start(); err != nil {
		return fmt.Errorf("failed to start correlation adapter: %w", err)
	}

	// Start event processing
	p.wg.Add(1)
	go p.processEvents()

	// Start batch processor if batch processing is enabled
	if p.batchSize > 1 {
		p.wg.Add(1)
		go p.processBatches()
	}

	// Start monitoring
	p.wg.Add(1)
	go p.monitorPipeline()

	p.logger.Info("Collector intelligence pipeline started")
	return nil
}

// Stop gracefully shuts down the pipeline
func (p *CollectorIntelligencePipeline) Stop() error {
	p.logger.Info("Stopping collector intelligence pipeline...")

	// Cancel context to stop all goroutines
	p.cancel()

	// Wait for all goroutines to finish
	p.wg.Wait()

	// Stop the correlation adapter
	if err := p.correlationAdapter.Stop(); err != nil {
		p.logger.Error("Failed to stop correlation adapter", zap.Error(err))
	}

	p.logger.Info("Collector intelligence pipeline stopped")
	return nil
}

// processEvents reads events from collector manager and sends them to intelligence
func (p *CollectorIntelligencePipeline) processEvents() {
	defer p.wg.Done()

	events := p.manager.Events()
	eventBatch := make([]*domain.UnifiedEvent, 0, p.batchSize)
	batchTimer := time.NewTimer(p.batchTimeout)
	defer batchTimer.Stop()

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed, process remaining batch
				if len(eventBatch) > 0 {
					p.processBatchEvents(eventBatch)
				}
				return
			}

			// Enrich event if enabled
			if p.enrichmentEnabled {
				p.enrichEvent(&event)
			}

			if p.batchSize > 1 {
				// Add to batch
				eventBatch = append(eventBatch, &event)

				// Process batch if full
				if len(eventBatch) >= p.batchSize {
					p.processBatchEvents(eventBatch)
					eventBatch = make([]*domain.UnifiedEvent, 0, p.batchSize)
					batchTimer.Reset(p.batchTimeout)
				}
			} else {
				// Process immediately
				p.processEvent(&event)
			}

		case <-batchTimer.C:
			// Process partial batch on timeout
			if len(eventBatch) > 0 {
				p.processBatchEvents(eventBatch)
				eventBatch = make([]*domain.UnifiedEvent, 0, p.batchSize)
			}
			batchTimer.Reset(p.batchTimeout)

		case <-p.ctx.Done():
			// Process remaining batch before exit
			if len(eventBatch) > 0 {
				p.processBatchEvents(eventBatch)
			}
			return
		}
	}
}

// processEvent sends a single event through the correlation pipeline
func (p *CollectorIntelligencePipeline) processEvent(event *domain.UnifiedEvent) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	if err := p.correlationAdapter.ProcessEvent(ctx, event); err != nil {
		p.correlationErrors++
		p.logger.Error("Failed to process event in correlation",
			zap.String("event_id", event.ID),
			zap.String("event_type", string(event.Type)),
			zap.Error(err))
	} else {
		p.processedEvents++
		if p.processedEvents%1000 == 0 {
			p.logger.Debug("Pipeline progress",
				zap.Uint64("processed_events", p.processedEvents))
		}
	}
}

// processBatchEvents processes a batch of events
func (p *CollectorIntelligencePipeline) processBatchEvents(events []*domain.UnifiedEvent) {
	for _, event := range events {
		p.processEvent(event)
	}
}

// processBatches handles batch timeout processing
func (p *CollectorIntelligencePipeline) processBatches() {
	defer p.wg.Done()
	// Batch processing is handled in processEvents
	<-p.ctx.Done()
}

// enrichEvent adds additional context to events before correlation
func (p *CollectorIntelligencePipeline) enrichEvent(event *domain.UnifiedEvent) {
	// Add pipeline metadata
	if event.Attributes == nil {
		event.Attributes = make(map[string]interface{})
	}
	event.Attributes["pipeline_timestamp"] = time.Now().Unix()
	event.Attributes["pipeline_version"] = "1.0.0"

	// Enrich based on event type
	switch event.Type {
	case domain.EventTypeKubernetes:
		p.enrichKubernetesEvent(event)
	case domain.EventTypeLog:
		p.enrichLogEvent(event)
	case domain.EventTypeNetwork:
		p.enrichNetworkEvent(event)
	case domain.EventTypeSystem:
		p.enrichSystemEvent(event)
	}

	// Add semantic context if not present
	if event.Semantic == nil && event.Category != "" {
		event.Semantic = &domain.SemanticContext{
			Intent:     p.inferIntent(event),
			Category:   event.Category,
			Tags:       p.generateSemanticTags(event),
			Confidence: 0.7,
		}
	}
}

// enrichKubernetesEvent adds Kubernetes-specific enrichment
func (p *CollectorIntelligencePipeline) enrichKubernetesEvent(event *domain.UnifiedEvent) {
	if event.Kubernetes == nil {
		return
	}

	// Add namespace to entity if not present
	if event.Entity == nil && event.K8sContext != nil {
		event.Entity = &domain.EntityContext{
			Type:      "pod",
			Name:      event.K8sContext.Name,
			Namespace: event.K8sContext.Namespace,
		}
	}

	// Infer impact based on event type
	if event.Impact == nil && event.Kubernetes.EventType != "" {
		event.Impact = p.inferKubernetesImpact(event.Kubernetes)
	}
}

// enrichLogEvent adds log-specific enrichment
func (p *CollectorIntelligencePipeline) enrichLogEvent(event *domain.UnifiedEvent) {
	if event.Application == nil {
		return
	}

	// Add application entity
	if event.Entity == nil && event.Application != nil {
		event.Entity = &domain.EntityContext{
			Type: "service",
			Name: "application",
		}
	}

	// Set severity based on log level
	if event.Severity == "" && event.Application.Level != "" {
		event.Severity = p.mapLogLevelToSeverity(event.Application.Level)
	}
}

// enrichNetworkEvent adds network-specific enrichment
func (p *CollectorIntelligencePipeline) enrichNetworkEvent(event *domain.UnifiedEvent) {
	if event.Network == nil {
		return
	}

	// Add connection entity
	if event.Entity == nil {
		event.Entity = &domain.EntityContext{
			Type: "connection",
			Name: fmt.Sprintf("%s:%d->%s:%d",
				event.Network.SourceIP,
				event.Network.SourcePort,
				event.Network.DestIP,
				event.Network.DestPort),
		}
	}
}

// enrichSystemEvent adds system-specific enrichment
func (p *CollectorIntelligencePipeline) enrichSystemEvent(event *domain.UnifiedEvent) {
	// System events are typically from eBPF/kernel
	if event.Kernel == nil {
		return
	}

	// Add process entity based on kernel data
	if event.Entity == nil && event.Kernel.Comm != "" {
		event.Entity = &domain.EntityContext{
			Type: "process",
			Name: event.Kernel.Comm,
			Attributes: map[string]string{
				"pid": fmt.Sprintf("%d", event.Kernel.PID),
			},
		}
	}
}

// inferIntent infers semantic intent from event
func (p *CollectorIntelligencePipeline) inferIntent(event *domain.UnifiedEvent) string {
	switch event.Type {
	case domain.EventTypeKubernetes:
		if event.Kubernetes != nil {
			// Use Kubernetes reason to infer intent
			switch event.Kubernetes.Reason {
			case "Created":
				return "deployment"
			case "Killing", "Deleted":
				return "termination"
			case "OOMKilling":
				return "resource_exhaustion"
			case "Failed", "BackOff":
				return "failure_condition"
			default:
				return "infrastructure_change"
			}
		}
	case domain.EventTypeLog:
		if event.Severity == "error" || event.Severity == "critical" {
			return "error_condition"
		}
		return "application_activity"
	case domain.EventTypeNetwork:
		return "network_communication"
	case domain.EventTypeSystem:
		return "system_operation"
	}
	return "unknown"
}

// generateSemanticTags generates semantic tags for an event
func (p *CollectorIntelligencePipeline) generateSemanticTags(event *domain.UnifiedEvent) []string {
	tags := make([]string, 0)

	// Add type tag
	tags = append(tags, fmt.Sprintf("type:%s", event.Type))

	// Add source tag
	if event.Source != "" {
		tags = append(tags, fmt.Sprintf("source:%s", event.Source))
	}

	// Add severity tag
	if event.Severity != "" {
		tags = append(tags, fmt.Sprintf("severity:%s", event.Severity))
	}

	// Add entity type tag
	if event.Entity != nil {
		tags = append(tags, fmt.Sprintf("entity:%s", event.Entity.Type))
	}

	return tags
}

// inferKubernetesImpact infers impact from Kubernetes event
func (p *CollectorIntelligencePipeline) inferKubernetesImpact(k8s *domain.KubernetesData) *domain.ImpactContext {
	impact := &domain.ImpactContext{
		Severity:         "info",
		AffectedServices: []string{},
	}

	switch k8s.EventType {
	case "pod_oom_killed":
		impact.Severity = "high"
		impact.InfrastructureImpact = 0.8
		impact.SystemCritical = true
	case "pod_crash_loop":
		impact.Severity = "critical"
		impact.InfrastructureImpact = 0.9
		impact.SystemCritical = true
	case "deployment_failed":
		impact.Severity = "high"
		impact.InfrastructureImpact = 0.7
		impact.CascadeRisk = true
	case "node_not_ready":
		impact.Severity = "critical"
		impact.InfrastructureImpact = 0.95
		impact.SystemCritical = true
	}

	// Add affected service
	if k8s.Object != "" {
		impact.AffectedServices = append(impact.AffectedServices, k8s.Object)
	}

	return impact
}

// mapLogLevelToSeverity maps log levels to event severity
func (p *CollectorIntelligencePipeline) mapLogLevelToSeverity(level string) domain.EventSeverity {
	switch level {
	case "error", "fatal", "panic":
		return domain.EventSeverity("error")
	case "warn", "warning":
		return domain.EventSeverity("warning")
	case "info":
		return domain.EventSeverity("info")
	case "debug", "trace":
		return domain.EventSeverity("debug")
	default:
		return domain.EventSeverity("info")
	}
}

// monitorPipeline monitors pipeline health and performance
func (p *CollectorIntelligencePipeline) monitorPipeline() {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ticker.C:
			uptime := time.Since(startTime)
			rate := float64(p.processedEvents) / uptime.Seconds()

			// Get correlation statistics
			correlationStats := p.correlationAdapter.GetStats()

			// Get latest findings
			latestFinding := p.correlationAdapter.GetLatestFindings()
			semanticGroups := p.correlationAdapter.GetSemanticGroups()

			p.logger.Info("Pipeline statistics",
				zap.Uint64("processed_events", p.processedEvents),
				zap.Uint64("correlation_errors", p.correlationErrors),
				zap.Float64("events_per_second", rate),
				zap.Duration("uptime", uptime),
				zap.Any("correlation_stats", correlationStats),
				zap.Int("semantic_groups", len(semanticGroups)),
				zap.Bool("has_findings", latestFinding != nil))

			// Log finding details if available
			if latestFinding != nil {
				p.logger.Info("Latest finding",
					zap.String("finding_id", latestFinding.ID),
					zap.String("pattern_type", latestFinding.PatternType),
					zap.Float64("confidence", latestFinding.Confidence),
					zap.String("description", latestFinding.Description))
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// GetStatistics returns pipeline statistics
func (p *CollectorIntelligencePipeline) GetStatistics() map[string]interface{} {
	correlationStats := p.correlationAdapter.GetStats()

	return map[string]interface{}{
		"processed_events":   p.processedEvents,
		"correlation_errors": p.correlationErrors,
		"enrichment_enabled": p.enrichmentEnabled,
		"batch_size":         p.batchSize,
		"correlation_stats":  correlationStats,
	}
}

// GetLatestFindings returns the latest correlation findings
func (p *CollectorIntelligencePipeline) GetLatestFindings() interface{} {
	return p.correlationAdapter.GetLatestFindings()
}

// GetSemanticGroups returns current semantic groups
func (p *CollectorIntelligencePipeline) GetSemanticGroups() interface{} {
	return p.correlationAdapter.GetSemanticGroups()
}
