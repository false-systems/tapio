package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/interfaces/server/adapters/correlation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// NewEventServerWithRealStore creates an EventServer with production-ready storage backend
func NewEventServerWithRealStore(logger *zap.Logger, tracer trace.Tracer) *EventServer {
	// Create real event store using our simple implementation
	eventStore := NewSimpleEventStore(
		50000,                    // Max events in memory
		7*24*time.Hour,          // 7 days retention
		logger.Named("event-store"),
	)

	// Create the event server with real storage
	server := NewEventServer(
		logger,
		tracer,
		eventStore,
	)

	logger.Info("EventService initialized with real storage backend",
		zap.String("store_type", "in-memory"),
		zap.Int("max_events", 50000),
		zap.Duration("retention", 7*24*time.Hour),
		zap.Int("max_events_per_batch", server.config.MaxEventsPerBatch),
		zap.Int("max_events_per_second", server.config.MaxEventsPerSecond),
	)

	return server
}

// HealthCheck verifies the event service is operational
func (s *EventServer) HealthCheck() error {
	// Check event store
	if s.eventStore == nil {
		return fmt.Errorf("event store not initialized")
	}

	// Check processor
	if s.processor == nil {
		return fmt.Errorf("event processor not initialized")
	}

	return nil
}

// GetServiceStats returns statistics about the event service
func (s *EventServer) GetServiceStats() map[string]interface{} {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	s.mu.RLock()
	subscriptionCount := len(s.subscriptions)
	s.mu.RUnlock()

	// Get event store stats if available
	var storeStats interface{}
	if store, ok := s.eventStore.(*correlation.InMemoryEventStore); ok {
		storeStats = store.GetStats()
	}

	return map[string]interface{}{
		"service": map[string]interface{}{
			"total_events":        s.stats.TotalEvents,
			"events_per_second":   s.stats.EventsPerSecond,
			"active_streams":      s.stats.ActiveStreams,
			"active_subscriptions": s.stats.ActiveSubscriptions,
			"failed_events":       s.stats.FailedEvents,
			"avg_processing_time": s.stats.ProcessingTime.String(),
		},
		"subscriptions": map[string]interface{}{
			"count": subscriptionCount,
			"max":   s.config.MaxSubscriptions,
		},
		"config": map[string]interface{}{
			"max_events_per_batch":      s.config.MaxEventsPerBatch,
			"max_events_per_second":     s.config.MaxEventsPerSecond,
			"real_time_streaming":       s.config.EnableRealTimeStreaming,
			"statistics_enabled":        s.config.EnableStatistics,
			"retention_period":          s.config.RetentionPeriod.String(),
		},
		"store": storeStats,
	}
}

// Integration points for event collection

// IngestEvents accepts events from various collectors
func (s *EventServer) IngestEvents(events []domain.Event) error {
	ctx := context.Background()

	// Process events
	processedEvents, err := s.processor.ProcessEvents(ctx, events)
	if err != nil {
		s.logger.Error("Failed to process ingested events", zap.Error(err))
		s.stats.mu.Lock()
		s.stats.FailedEvents += int64(len(events))
		s.stats.mu.Unlock()
		return err
	}

	// Store events
	if err := s.eventStore.Store(ctx, processedEvents); err != nil {
		s.logger.Error("Failed to store ingested events", zap.Error(err))
		s.stats.mu.Lock()
		s.stats.FailedEvents += int64(len(events))
		s.stats.mu.Unlock()
		return err
	}

	// Update statistics
	s.stats.mu.Lock()
	s.stats.TotalEvents += int64(len(events))
	s.stats.mu.Unlock()

	// Notify subscribers
	if s.config.EnableRealTimeStreaming {
		s.notifySubscribers(processedEvents)
	}

	s.logger.Debug("Ingested events successfully",
		zap.Int("count", len(events)),
		zap.Int64("total_events", s.stats.TotalEvents),
	)

	return nil
}

// IngestCollectorEvents accepts events from the collector service
func (s *EventServer) IngestCollectorEvents(collectorID string, events []domain.Event) error {
	// Add collector metadata to events
	for i := range events {
		if events[i].Context.Labels == nil {
			events[i].Context.Labels = make(map[string]string)
		}
		events[i].Context.Labels["collector_id"] = collectorID
		events[i].Context.Labels["ingestion_path"] = "collector_service"
	}

	return s.IngestEvents(events)
}

// IngesteBPFEvents accepts events from eBPF collectors (AGENT 2 integration)
func (s *EventServer) IngesteBPFEvents(events []domain.Event) error {
	// Add eBPF source labeling
	for i := range events {
		if events[i].Context.Labels == nil {
			events[i].Context.Labels = make(map[string]string)
		}
		events[i].Context.Labels["source"] = "ebpf"
		events[i].Context.Labels["ingestion_path"] = "ebpf_collector"
		events[i].Source = domain.SourceEBPF
	}

	return s.IngestEvents(events)
}

// IngestK8sEvents accepts events from Kubernetes collectors
func (s *EventServer) IngestK8sEvents(events []domain.Event) error {
	// Add Kubernetes source labeling
	for i := range events {
		if events[i].Context.Labels == nil {
			events[i].Context.Labels = make(map[string]string)
		}
		events[i].Context.Labels["source"] = "kubernetes"
		events[i].Context.Labels["ingestion_path"] = "k8s_collector"
		events[i].Source = domain.SourceK8s
		events[i].Type = domain.EventTypeKubernetes
	}

	return s.IngestEvents(events)
}

// Configuration for event service integration
type EventIngestionConfig struct {
	EnableCollectorIngestion bool
	EnableeBPFIngestion      bool
	EnableK8sIngestion       bool
	EnableOTELIngestion      bool

	// Filtering - which events to accept
	EventFilter    func(domain.Event) bool
	SeverityFilter []domain.EventSeverity
	TypeFilter     []domain.EventType

	// Rate limiting
	MaxEventsPerSecond int
	MaxEventsPerBatch  int

	// Enrichment
	EnableContextEnrichment bool
	EnableAIEnrichment      bool
}

// ConfigureEventIngestion sets up event ingestion from various sources
func (s *EventServer) ConfigureEventIngestion(config EventIngestionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update service configuration based on ingestion config
	if config.MaxEventsPerSecond > 0 {
		s.config.MaxEventsPerSecond = config.MaxEventsPerSecond
	}
	if config.MaxEventsPerBatch > 0 {
		s.config.MaxEventsPerBatch = config.MaxEventsPerBatch
	}

	s.logger.Info("Event ingestion configured",
		zap.Bool("collector_ingestion", config.EnableCollectorIngestion),
		zap.Bool("ebpf_ingestion", config.EnableeBPFIngestion),
		zap.Bool("k8s_ingestion", config.EnableK8sIngestion),
		zap.Bool("otel_ingestion", config.EnableOTELIngestion),
		zap.Int("max_events_per_sec", config.MaxEventsPerSecond),
		zap.Int("max_events_per_batch", config.MaxEventsPerBatch),
	)
}

// GetActiveSubscriptions returns information about active event subscriptions
func (s *EventServer) GetActiveSubscriptions() map[string]*EventSubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	subscriptions := make(map[string]*EventSubscription)
	for id, sub := range s.subscriptions {
		subCopy := *sub
		subscriptions[id] = &subCopy
	}

	return subscriptions
}

// CloseSubscription manually closes a subscription
func (s *EventServer) CloseSubscription(subscriptionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscription, exists := s.subscriptions[subscriptionID]
	if !exists {
		return fmt.Errorf("subscription %s not found", subscriptionID)
	}

	// Close the update channel
	if updateChan, exists := s.subscribers[subscriptionID]; exists {
		close(updateChan)
		delete(s.subscribers, subscriptionID)
	}

	// Remove subscription
	delete(s.subscriptions, subscriptionID)

	s.logger.Info("Subscription closed manually",
		zap.String("subscription_id", subscriptionID),
		zap.Int64("events_sent", subscription.EventsSent),
		zap.Duration("duration", time.Since(subscription.StartTime)),
	)

	return nil
}

// CleanupInactiveSubscriptions removes inactive subscriptions
func (s *EventServer) CleanupInactiveSubscriptions(inactivityThreshold time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var cleaned []string

	for id, sub := range s.subscriptions {
		if now.Sub(sub.LastActivity) > inactivityThreshold {
			// Close the update channel
			if updateChan, exists := s.subscribers[id]; exists {
				close(updateChan)
				delete(s.subscribers, id)
			}

			// Remove subscription
			delete(s.subscriptions, id)
			cleaned = append(cleaned, id)
		}
	}

	if len(cleaned) > 0 {
		s.logger.Info("Cleaned up inactive subscriptions",
			zap.Strings("subscription_ids", cleaned),
			zap.Duration("inactivity_threshold", inactivityThreshold),
		)
	}
}