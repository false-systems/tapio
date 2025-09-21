package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer implements service discovery and dependency mapping
type Observer struct {
	*base.BaseObserver        // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle

	// Core fields
	config *Config
	logger *zap.Logger
	name   string

	// Core components
	connectionsTracker *ConnectionTracker // Raw connection tracking
	k8sMapper          *K8sEnricher       // K8s context enrichment

	// State tracking
	mu    sync.RWMutex
	stats ServiceStats

	// OpenTelemetry instrumentation
	tracer             trace.Tracer
	connectionsTracked metric.Int64Counter
	servicesDiscovered metric.Int64Counter
	eventsProcessed    metric.Int64Counter
	errorsTotal        metric.Int64Counter
}

// ServiceStats tracks observer statistics
type ServiceStats struct {
	ActiveConnections  uint64
	ServicesDiscovered uint64
	ServiceFlows       uint64
	LastEventTime      time.Time
	K8sMappingEnabled  bool
}

// NewObserver creates a new services observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize OpenTelemetry
	meter := otel.Meter("tapio.observers.services")
	tracer := otel.Tracer("tapio.observers.services")

	// Create metrics
	connectionsTracked, _ := meter.Int64Counter(
		fmt.Sprintf("%s_connections_tracked_total", name),
		metric.WithDescription("Total TCP connections tracked"),
	)
	servicesDiscovered, _ := meter.Int64Counter(
		fmt.Sprintf("%s_services_discovered_total", name),
		metric.WithDescription("Total services discovered"),
	)
	eventsProcessed, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)
	errorsTotal, _ := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in observer"),
	)

	o := &Observer{
		BaseObserver:        base.NewBaseObserver(name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger.Named(name)),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger.Named(name)),
		config:              config,
		logger:              logger.Named(name),
		name:                name,
		stats: ServiceStats{
			K8sMappingEnabled: config.EnableK8sMapping,
		},
		tracer:             tracer,
		connectionsTracked: connectionsTracked,
		servicesDiscovered: servicesDiscovered,
		eventsProcessed:    eventsProcessed,
		errorsTotal:        errorsTotal,
	}

	// Initialize connection tracking (always required)
	o.connectionsTracker = NewConnectionTracker(config, logger)

	// Initialize K8s enrichment if K8s mapping enabled
	if config.EnableK8sMapping {
		var err error
		o.k8sMapper, err = NewK8sEnricher(config, logger, o.connectionsTracker)
		if err != nil {
			return nil, fmt.Errorf("failed to create K8s enrichment K8s mapper: %w", err)
		}
	}

	o.logger.Info("Services observer created",
		zap.String("name", name),
		zap.Bool("k8s_mapping", config.EnableK8sMapping))

	return o, nil
}

// Start begins service discovery
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting services observer")

	// Start connection tracking - Connection tracking
	if err := o.connectionsTracker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start connection tracking: %w", err)
	}

	// Start K8s enrichment - K8s mapping
	if o.k8sMapper != nil {
		if err := o.k8sMapper.Start(ctx); err != nil {
			return fmt.Errorf("failed to start K8s enrichment: %w", err)
		}
	}

	// Start event processor
	o.LifecycleManager.Start("event-processor", func() {
		o.processEvents(ctx)
	})

	// Start stats collector
	o.LifecycleManager.Start("stats-collector", func() {
		o.collectStats(ctx)
	})

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Services observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping services observer")

	o.BaseObserver.SetHealthy(false)

	// Stop components in reverse order

	if o.k8sMapper != nil {
		if err := o.k8sMapper.Stop(); err != nil {
			o.logger.Error("Error stopping K8s enrichment", zap.Error(err))
		}
	}

	if err := o.connectionsTracker.Stop(); err != nil {
		o.logger.Error("Error stopping connection tracking", zap.Error(err))
	}

	// Stop lifecycle
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Error("Error stopping lifecycle", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.logger.Info("Services observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// GetStats returns current statistics
func (o *Observer) GetStats() ServiceStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.stats
}

// GetServiceMap returns the current service dependency map
func (o *Observer) GetServiceMap() map[string]*ServiceFlow {
	if o.k8sMapper == nil {
		return make(map[string]*ServiceFlow)
	}
	return o.k8sMapper.GetServiceFlows()
}

// processEvents processes events from different levels
func (o *Observer) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Process based on enabled features
			if o.k8sMapper != nil {
				o.processEnrichedEvents(ctx)
			} else {
				o.processConnectionEvents(ctx)
			}
		}
	}
}

// processConnectionEvents processes raw connection events
func (o *Observer) processConnectionEvents(ctx context.Context) {
	select {
	case event := <-o.connectionsTracker.Events():
		o.sendConnectionEvent(ctx, event)
	default:
		time.Sleep(10 * time.Millisecond)
	}
}

// processEnrichedEvents processes K8s-enriched events
func (o *Observer) processEnrichedEvents(ctx context.Context) {
	select {
	case event := <-o.k8sMapper.Events():
		o.sendEnrichedEvent(ctx, event)
	default:
		time.Sleep(10 * time.Millisecond)
	}
}

// sendConnectionEvent sends a raw connection event
func (o *Observer) sendConnectionEvent(ctx context.Context, event *ConnectionEvent) {
	if event == nil {
		return
	}

	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("conn-%d-%d", event.PID, event.Timestamp),
		Timestamp: event.GetTimestamp(),
		Type:      domain.EventTypeNetworkConnection,
		Source:    o.name,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: event.EventType.String(),
				Protocol:  "TCP",
				SrcIP:     event.GetSrcIPString(),
				DstIP:     event.GetDstIPString(),
				SrcPort:   int32(event.SrcPort),
				DstPort:   int32(event.DstPort),
				Direction: "outbound",
			},
			Process: &domain.ProcessData{
				PID:     int32(event.PID),
				Command: event.GetComm(),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":  o.name,
				"version":   "1.0.0",
				"level":     "1",
				"conn_type": event.EventType.String(),
			},
		},
	}

	if o.EventChannelManager.SendEvent(domainEvent) {
		o.BaseObserver.RecordEvent()
		o.connectionsTracked.Add(ctx, 1)
	} else {
		o.BaseObserver.RecordDrop()
	}
}

// sendEnrichedEvent sends a K8s-enriched connection event
func (o *Observer) sendEnrichedEvent(ctx context.Context, event *EnrichedConnection) {
	if event == nil || event.ServiceFlow == nil {
		return
	}

	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("flow-%s-%d", event.ServiceFlow.SourceService, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeServiceMap,
		Source:    o.name,
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: "service_flow",
				Protocol:  "TCP",
				SrcPort:   int32(event.ServiceFlow.Port),
				DstPort:   int32(event.ServiceFlow.Port),
				Direction: flowTypeString(event.ServiceFlow.FlowType),
			},
			KubernetesResource: &domain.K8sResourceData{
				Namespace: event.ServiceFlow.SourceNamespace,
				Name:      event.ServiceFlow.SourceService,
				Kind:      "Service",
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":  o.name,
				"version":   "1.0.0",
				"level":     "2",
				"flow_type": flowTypeString(event.ServiceFlow.FlowType),
				"src_ns":    event.ServiceFlow.SourceNamespace,
				"dst_ns":    event.ServiceFlow.DestinationNS,
				"src_svc":   event.ServiceFlow.SourceService,
				"dst_svc":   event.ServiceFlow.DestinationService,
			},
		},
	}

	if o.EventChannelManager.SendEvent(domainEvent) {
		o.BaseObserver.RecordEvent()
		o.servicesDiscovered.Add(ctx, 1)
	} else {
		o.BaseObserver.RecordDrop()
	}
}

// collectStats periodically collects statistics from all levels
func (o *Observer) collectStats(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.updateStats()
		}
	}
}

// updateStats updates statistics from all levels
func (o *Observer) updateStats() {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Get connection tracking stats
	connectionsStats := o.connectionsTracker.GetStats()
	o.stats.ActiveConnections = connectionsStats.ActiveConnections

	// Get K8s enrichment stats
	if o.k8sMapper != nil {
		serviceFlows := o.k8sMapper.GetServiceFlows()
		o.stats.ServicesDiscovered = uint64(len(serviceFlows))
		o.stats.ServiceFlows = uint64(len(serviceFlows))
	}

	o.stats.LastEventTime = time.Now()
}

// Helper function to convert flow type to string
func flowTypeString(ft FlowType) string {
	switch ft {
	case FlowIntraNamespace:
		return "intra-namespace"
	case FlowInterNamespace:
		return "inter-namespace"
	case FlowExternal:
		return "external"
	case FlowIngress:
		return "ingress"
	case FlowEgress:
		return "egress"
	default:
		return "unknown"
	}
}
