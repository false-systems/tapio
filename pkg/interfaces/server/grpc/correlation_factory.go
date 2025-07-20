package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	corrDomain "github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// NewCorrelationServerWithRealStore creates a CorrelationService with production-ready storage backend
func NewCorrelationServerWithRealStore(logger *zap.Logger, tracer trace.Tracer) *CorrelationServer {
	// Create real correlation manager
	correlationManager := &corrDomain.Manager{
		CollectionManager: &corrDomain.CollectionManager{},
	}

	// Create real event store using our simple implementation
	eventStore := NewSimpleEventStore(
		50000,          // Max events in memory
		7*24*time.Hour, // 7 days retention
		logger.Named("correlation-event-store"),
	)

	// Create configuration for correlation service
	config := CorrelationServiceConfig{
		MaxEventsPerAnalysis:       10000,
		MaxSubscriptions:           1000,
		SubscriptionBufferSize:     10000,
		AnalysisWorkers:            8,
		MinConfidenceThreshold:     0.7,
		EnableRealTimeAnalysis:     true,
		EnableRootCauseAnalysis:    true,
		EnableImpactAssessment:     true,
		EnablePredictions:          true,
		AnalysisTimeout:            30 * time.Second,
		CorrelationRetentionPeriod: 30 * 24 * time.Hour, // 30 days
	}

	// Create the correlation server with real storage
	server := NewCorrelationServer(
		logger,
		tracer,
		eventStore,
	)

	// Use the custom correlation manager
	server.correlationMgr = correlationManager

	logger.Info("CorrelationService initialized with real storage backend",
		zap.String("store_type", "in-memory"),
		zap.Int("max_events_per_analysis", config.MaxEventsPerAnalysis),
		zap.Int("analysis_workers", config.AnalysisWorkers),
		zap.Float64("min_confidence", config.MinConfidenceThreshold),
		zap.Bool("real_time_analysis", config.EnableRealTimeAnalysis),
		zap.Bool("root_cause_analysis", config.EnableRootCauseAnalysis),
		zap.Bool("impact_assessment", config.EnableImpactAssessment),
		zap.Bool("predictions", config.EnablePredictions),
		zap.Duration("analysis_timeout", config.AnalysisTimeout),
		zap.Duration("retention_period", config.CorrelationRetentionPeriod),
	)

	return server
}

// HealthCheck verifies the correlation service is operational
func (s *CorrelationServer) HealthCheck() error {
	// Check correlation manager
	if s.correlationMgr == nil {
		return fmt.Errorf("correlation manager not initialized")
	}

	// Check event store
	if s.eventStore == nil {
		return fmt.Errorf("event store not initialized")
	}

	// Check if subscriptions are working
	s.mu.RLock()
	subscriptionCount := len(s.subscriptions)
	s.mu.RUnlock()

	s.logger.Debug("Correlation service health check passed",
		zap.Int("active_subscriptions", subscriptionCount),
		zap.Bool("real_time_analysis", s.config.EnableRealTimeAnalysis),
	)

	return nil
}

// GetServiceStats returns statistics about the correlation service
func (s *CorrelationServer) GetServiceStats() map[string]interface{} {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	s.mu.RLock()
	subscriptionCount := len(s.subscriptions)
	s.mu.RUnlock()

	// Get event store stats if available
	var storeStats interface{}
	if store, ok := s.eventStore.(*SimpleEventStore); ok {
		storeStats = store.GetStats()
	}

	return map[string]interface{}{
		"service": map[string]interface{}{
			"total_correlations":   s.stats.TotalCorrelations,
			"total_analyses":       s.stats.TotalAnalyses,
			"active_subscriptions": s.stats.ActiveSubscriptions,
			"failed_analyses":      s.stats.FailedAnalyses,
			"avg_analysis_time":    s.stats.AvgAnalysisTime.String(),
			"predictions_made":     s.stats.PredictionsMade,
		},
		"subscriptions": map[string]interface{}{
			"count": subscriptionCount,
			"max":   s.config.MaxSubscriptions,
		},
		"config": map[string]interface{}{
			"max_events_per_analysis":  s.config.MaxEventsPerAnalysis,
			"analysis_workers":         s.config.AnalysisWorkers,
			"min_confidence_threshold": s.config.MinConfidenceThreshold,
			"real_time_analysis":       s.config.EnableRealTimeAnalysis,
			"root_cause_analysis":      s.config.EnableRootCauseAnalysis,
			"impact_assessment":        s.config.EnableImpactAssessment,
			"predictions_enabled":      s.config.EnablePredictions,
			"analysis_timeout":         s.config.AnalysisTimeout.String(),
			"retention_period":         s.config.CorrelationRetentionPeriod.String(),
		},
		"store": storeStats,
	}
}

// Integration points for correlation analysis

// AnalyzeEventsFromCollector accepts events from collectors for real-time analysis
func (s *CorrelationServer) AnalyzeEventsFromCollector(collectorID string, events []domain.Event) (*corrDomain.AnalysisResult, error) {
	ctx := context.Background()

	// Add collector metadata to events
	for i := range events {
		if events[i].Context.Labels == nil {
			events[i].Context.Labels = make(map[string]string)
		}
		events[i].Context.Labels["collector_id"] = collectorID
		events[i].Context.Labels["analysis_source"] = "collector"
	}

	// Store events first
	if err := s.eventStore.Store(ctx, events); err != nil {
		s.logger.Error("Failed to store events for analysis", zap.Error(err))
		return nil, err
	}

	// Perform correlation analysis
	analysisOptions := &corrDomain.AnalysisOptions{
		EnableRootCause:        true,
		EnablePredictions:      s.config.EnablePredictions,
		EnableImpactAssessment: s.config.EnableImpactAssessment,
		MinConfidence:          s.config.MinConfidenceThreshold,
	}

	// Convert domain.Event to []*domain.Event
	eventPtrs := make([]*domain.Event, len(events))
	for i := range events {
		eventPtrs[i] = &events[i]
	}

	result := s.correlationMgr.AnalyzeEvents(ctx, eventPtrs, analysisOptions)
	if result == nil {
		s.logger.Error("Failed to analyze events from collector",
			zap.String("collector_id", collectorID),
		)
		s.stats.mu.Lock()
		s.stats.FailedAnalyses++
		s.stats.mu.Unlock()
		return nil, fmt.Errorf("analysis returned nil result")
	}

	// Update statistics
	s.stats.mu.Lock()
	s.stats.TotalAnalyses++
	s.stats.TotalCorrelations += int64(len(result.Correlations))
	s.stats.mu.Unlock()

	// Notify subscribers of new correlations
	if s.config.EnableRealTimeAnalysis {
		s.notifyCorrelationSubscribers(result)
	}

	s.logger.Info("Analyzed events from collector",
		zap.String("collector_id", collectorID),
		zap.Int("events", len(events)),
		zap.Int("correlations", len(result.Correlations)),
		zap.Int("semantic_groups", len(result.SemanticGroups)),
	)

	return result, nil
}

// AnalyzeeBPFEvents accepts events from eBPF collectors for real-time analysis (AGENT 2 integration)
func (s *CorrelationServer) AnalyzeeBPFEvents(events []domain.Event) (*corrDomain.AnalysisResult, error) {
	// Add eBPF source labeling
	for i := range events {
		if events[i].Context.Labels == nil {
			events[i].Context.Labels = make(map[string]string)
		}
		events[i].Context.Labels["source"] = "ebpf"
		events[i].Context.Labels["analysis_source"] = "ebpf_collector"
		events[i].Source = domain.SourceEBPF
	}

	return s.AnalyzeEventsFromCollector("ebpf", events)
}

// AnalyzeK8sEvents accepts events from Kubernetes collectors for real-time analysis
func (s *CorrelationServer) AnalyzeK8sEvents(events []domain.Event) (*corrDomain.AnalysisResult, error) {
	// Add Kubernetes source labeling
	for i := range events {
		if events[i].Context.Labels == nil {
			events[i].Context.Labels = make(map[string]string)
		}
		events[i].Context.Labels["source"] = "kubernetes"
		events[i].Context.Labels["analysis_source"] = "k8s_collector"
		events[i].Source = domain.SourceK8s
		events[i].Type = domain.EventTypeKubernetes
	}

	return s.AnalyzeEventsFromCollector("kubernetes", events)
}

// Configuration for correlation service integration
type CorrelationIngestionConfig struct {
	EnableCollectorAnalysis bool
	EnableeBPFAnalysis      bool
	EnableK8sAnalysis       bool
	EnableOTELAnalysis      bool

	// Analysis settings
	ConfidenceThreshold   float64
	MaxEventsPerAnalysis  int
	AnalysisTimeout       time.Duration
	EnableRealTimeUpdates bool

	// Filtering - which events to analyze
	EventFilter    func(domain.Event) bool
	SeverityFilter []domain.EventSeverity
	TypeFilter     []domain.EventType

	// Feature flags
	EnableRootCause    bool
	EnablePredictions  bool
	EnableImpactAssess bool
}

// ConfigureCorrelationIngestion sets up correlation analysis from various sources
func (s *CorrelationServer) ConfigureCorrelationIngestion(config CorrelationIngestionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update service configuration based on ingestion config
	if config.ConfidenceThreshold > 0 {
		s.config.MinConfidenceThreshold = config.ConfidenceThreshold
	}
	if config.MaxEventsPerAnalysis > 0 {
		s.config.MaxEventsPerAnalysis = config.MaxEventsPerAnalysis
	}
	if config.AnalysisTimeout > 0 {
		s.config.AnalysisTimeout = config.AnalysisTimeout
	}
	if config.EnableRealTimeUpdates {
		s.config.EnableRealTimeAnalysis = config.EnableRealTimeUpdates
	}

	// Update feature flags
	s.config.EnableRootCauseAnalysis = config.EnableRootCause
	s.config.EnablePredictions = config.EnablePredictions
	s.config.EnableImpactAssessment = config.EnableImpactAssess

	s.logger.Info("Correlation ingestion configured",
		zap.Bool("collector_analysis", config.EnableCollectorAnalysis),
		zap.Bool("ebpf_analysis", config.EnableeBPFAnalysis),
		zap.Bool("k8s_analysis", config.EnableK8sAnalysis),
		zap.Bool("otel_analysis", config.EnableOTELAnalysis),
		zap.Float64("confidence_threshold", config.ConfidenceThreshold),
		zap.Int("max_events_per_analysis", config.MaxEventsPerAnalysis),
		zap.Duration("analysis_timeout", config.AnalysisTimeout),
		zap.Bool("real_time_updates", config.EnableRealTimeUpdates),
		zap.Bool("root_cause_enabled", config.EnableRootCause),
		zap.Bool("predictions_enabled", config.EnablePredictions),
		zap.Bool("impact_assessment_enabled", config.EnableImpactAssess),
	)
}

// GetActiveSubscriptions returns information about active correlation subscriptions
func (s *CorrelationServer) GetActiveSubscriptions() map[string]*CorrelationSubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	subscriptions := make(map[string]*CorrelationSubscription)
	for id, sub := range s.subscriptions {
		subCopy := *sub
		subscriptions[id] = &subCopy
	}

	return subscriptions
}

// CloseSubscription manually closes a correlation subscription
func (s *CorrelationServer) CloseSubscription(subscriptionID string) error {
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

	s.logger.Info("Correlation subscription closed manually",
		zap.String("subscription_id", subscriptionID),
		zap.Int64("updates_sent", subscription.UpdatesSent),
		zap.Duration("duration", time.Since(subscription.StartTime)),
	)

	return nil
}

// CleanupInactiveSubscriptions removes inactive correlation subscriptions
func (s *CorrelationServer) CleanupInactiveSubscriptions(inactivityThreshold time.Duration) {
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
		s.logger.Info("Cleaned up inactive correlation subscriptions",
			zap.Strings("subscription_ids", cleaned),
			zap.Duration("inactivity_threshold", inactivityThreshold),
		)
	}
}

// StartPeriodicAnalysis starts background correlation analysis
func (s *CorrelationServer) StartPeriodicAnalysis(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.logger.Info("Started periodic correlation analysis",
			zap.Duration("interval", interval),
		)

		for {
			select {
			case <-ticker.C:
				s.performPeriodicAnalysis()
			}
		}
	}()
}

// performPeriodicAnalysis runs periodic correlation analysis on stored events
func (s *CorrelationServer) performPeriodicAnalysis() {
	ctx := context.Background()
	start := time.Now()

	// Get recent events for analysis
	events, err := s.eventStore.GetLatest(ctx, s.config.MaxEventsPerAnalysis)
	if err != nil {
		s.logger.Error("Failed to get events for periodic analysis", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	// Perform analysis
	// Convert domain.Event to []*domain.Event
	eventPtrs := make([]*domain.Event, len(events))
	for i := range events {
		eventPtrs[i] = &events[i]
	}

	analysisOptions := &corrDomain.AnalysisOptions{
		EnableRootCause:        s.config.EnableRootCauseAnalysis,
		EnablePredictions:      s.config.EnablePredictions,
		EnableImpactAssessment: s.config.EnableImpactAssessment,
		MinConfidence:          s.config.MinConfidenceThreshold,
	}

	result := s.correlationMgr.AnalyzeEvents(ctx, eventPtrs, analysisOptions)
	if result == nil {
		s.logger.Error("Failed to perform periodic analysis")
		s.stats.mu.Lock()
		s.stats.FailedAnalyses++
		s.stats.mu.Unlock()
		return
	}

	// Update statistics
	s.stats.mu.Lock()
	s.stats.TotalAnalyses++
	s.stats.TotalCorrelations += int64(len(result.Correlations))
	analysisTime := time.Since(start)
	if s.stats.AvgAnalysisTime == 0 {
		s.stats.AvgAnalysisTime = analysisTime
	} else {
		s.stats.AvgAnalysisTime = (s.stats.AvgAnalysisTime + analysisTime) / 2
	}
	s.stats.mu.Unlock()

	// Notify subscribers of new correlations
	if s.config.EnableRealTimeAnalysis && len(result.Correlations) > 0 {
		s.notifyCorrelationSubscribers(result)
	}

	s.logger.Debug("Completed periodic correlation analysis",
		zap.Int("events_analyzed", len(events)),
		zap.Int("correlations_found", len(result.Correlations)),
		zap.Int("semantic_groups", len(result.SemanticGroups)),
		zap.Duration("analysis_time", analysisTime),
	)
}
