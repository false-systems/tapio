package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// SimpleCorrelationSystem - The new zero-config correlation engine
// Replaces the old "AI-powered" semantic engine with something that actually works
type SimpleCorrelationSystem struct {
	logger *zap.Logger

	// Core correlation engines
	k8sCorrelator      *K8sNativeCorrelator           // 100% accurate K8s relationships
	temporalCorrelator *TemporalCorrelator            // Time-based patterns
	sequenceDetector   *SequenceDetector              // Sequential patterns
	confidenceScorer   *ConfidenceScorer              // Multi-dimensional scoring
	explanationEngine  *ExplanationEngine             // Human explanations
	persistenceService *CorrelationPersistenceService // Persistence layer

	// Event processing
	eventChan   chan *domain.UnifiedEvent
	insightChan chan domain.Insight

	// Configuration
	config SimpleSystemConfig

	// State
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	mu      sync.RWMutex
	wg      sync.WaitGroup

	// Statistics
	stats ProcessingStatistics
}

// SimpleSystemConfig configures the simple correlation system
type SimpleSystemConfig struct {
	// Processing
	EventBufferSize int
	MaxConcurrency  int

	// Correlation settings
	EnableK8sNative bool
	EnableTemporal  bool
	EnableSequence  bool

	// Performance
	ProcessingTimeout time.Duration
	CleanupInterval   time.Duration
}

// DefaultSimpleSystemConfig returns production-ready defaults
func DefaultSimpleSystemConfig() SimpleSystemConfig {
	return SimpleSystemConfig{
		EventBufferSize:   1000,
		MaxConcurrency:    4,
		EnableK8sNative:   true, // Always on - free correlations!
		EnableTemporal:    true, // Usually valuable
		EnableSequence:    true, // Pattern recognition
		ProcessingTimeout: 5 * time.Second,
		CleanupInterval:   1 * time.Minute,
	}
}

// ProcessingStatistics tracks system performance
type ProcessingStatistics struct {
	EventsProcessed        int64
	K8sCorrelationsFound   int64
	TemporalCorrelations   int64
	SequenceCorrelations   int64
	TotalInsightsGenerated int64
	AvgProcessingTime      time.Duration
	mu                     sync.RWMutex
}

// NewSimpleCorrelationSystem creates the new correlation system
func NewSimpleCorrelationSystem(logger *zap.Logger, config SimpleSystemConfig) *SimpleCorrelationSystem {
	ctx, cancel := context.WithCancel(context.Background())

	return &SimpleCorrelationSystem{
		logger: logger,

		// Initialize all correlation engines
		k8sCorrelator:      NewK8sNativeCorrelator(logger),
		temporalCorrelator: NewTemporalCorrelator(logger, DefaultTemporalConfig()),
		sequenceDetector:   NewSequenceDetector(logger, DefaultSequenceConfig()),
		confidenceScorer:   NewConfidenceScorer(logger, DefaultScorerConfig()),
		explanationEngine:  NewExplanationEngine(),
		persistenceService: NewCorrelationPersistenceService(NewInMemoryCorrelationStore(logger), logger),

		// Event channels
		eventChan:   make(chan *domain.UnifiedEvent, config.EventBufferSize),
		insightChan: make(chan domain.Insight, config.EventBufferSize), // Match event buffer size

		config: config,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins correlation processing
func (s *SimpleCorrelationSystem) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("simple correlation system already running")
	}

	// Start event processing workers
	for i := 0; i < s.config.MaxConcurrency; i++ {
		s.wg.Add(1)
		go s.processEvents()
	}

	// Start cleanup routine
	s.wg.Add(1)
	go s.cleanupRoutine()

	s.running = true
	s.logger.Info("Simple correlation system started",
		zap.Bool("k8s_native", s.config.EnableK8sNative),
		zap.Bool("temporal", s.config.EnableTemporal),
		zap.Bool("sequence", s.config.EnableSequence),
		zap.Int("concurrency", s.config.MaxConcurrency),
	)

	return nil
}

// ProcessEvent processes a single event through all correlation engines
func (s *SimpleCorrelationSystem) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	if !s.running {
		return fmt.Errorf("correlation system not running")
	}

	// Use a timeout to prevent indefinite blocking
	timer := time.NewTimer(s.config.ProcessingTimeout)
	defer timer.Stop()

	select {
	case s.eventChan <- event:
		return nil
	case <-timer.C:
		return fmt.Errorf("timeout sending event to processing queue")
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return fmt.Errorf("correlation system stopped")
	}
}

// processEvents is the main event processing loop
func (s *SimpleCorrelationSystem) processEvents() {
	defer s.wg.Done()

	for {
		select {
		case event := <-s.eventChan:
			s.processEventSync(event)

		case <-s.ctx.Done():
			return
		}
	}
}

// processEventSync processes a single event synchronously
func (s *SimpleCorrelationSystem) processEventSync(event *domain.UnifiedEvent) {
	startTime := time.Now()
	defer func() {
		s.updateProcessingStats(time.Since(startTime))
	}()

	correlations := make([]interface{}, 0)

	// 1. K8s Native Correlations (instant, 100% confidence)
	if s.config.EnableK8sNative {
		k8sCorrelations := s.k8sCorrelator.FindCorrelations(event)
		for _, corr := range k8sCorrelations {
			correlations = append(correlations, corr)
			s.createInsightFromK8sCorrelation(event, corr)
		}
		s.updateK8sStats(len(k8sCorrelations))
	}

	// 2. Temporal Correlations (learned patterns)
	if s.config.EnableTemporal {
		temporalCorrelations := s.temporalCorrelator.Process(event)
		for _, corr := range temporalCorrelations {
			correlations = append(correlations, corr)
			s.createInsightFromTemporalCorrelation(event, corr)
		}
		s.updateTemporalStats(len(temporalCorrelations))
	}

	// 3. Sequence Correlations (sequential patterns)
	if s.config.EnableSequence {
		sequenceCorrelations := s.sequenceDetector.Process(event)
		for _, corr := range sequenceCorrelations {
			correlations = append(correlations, corr)
			s.createInsightFromSequenceCorrelation(event, corr)
		}
		s.updateSequenceStats(len(sequenceCorrelations))
	}
}

// createInsightFromK8sCorrelation creates insights from K8s correlations
func (s *SimpleCorrelationSystem) createInsightFromK8sCorrelation(event *domain.UnifiedEvent, corr K8sCorrelation) {
	explanation := s.explanationEngine.ExplainK8sCorrelation(corr)

	insight := domain.Insight{
		ID:          fmt.Sprintf("k8s-corr-%s-%d", corr.Type, time.Now().UnixNano()),
		Type:        "k8s_correlation",
		Title:       explanation.Summary,
		Description: explanation.Details,
		Severity:    s.mapConfidenceToSeverity(corr.Confidence),
		Source:      "k8s_native_correlator",
		Timestamp:   time.Now(),
		Metadata: map[string]interface{}{
			"correlation_type": corr.Type,
			"confidence":       corr.Confidence,
			"source_resource":  fmt.Sprintf("%s/%s", corr.Source.Kind, corr.Source.Name),
			"target_resource":  fmt.Sprintf("%s/%s", corr.Target.Kind, corr.Target.Name),
			"explanation":      explanation,
			"actionable":       explanation.Actionable,
		},
	}

	s.sendInsight(insight)

	// Persist the K8s correlation for historical analysis
	if s.persistenceService != nil {
		go func() {
			ctx := context.Background()
			err := s.persistenceService.PersistCorrelation(ctx, event, corr, explanation, corr.Confidence)
			if err != nil {
				s.logger.Warn("Failed to persist K8s correlation", zap.Error(err))
			}
		}()
	}
}

// createInsightFromTemporalCorrelation creates insights from temporal correlations
func (s *SimpleCorrelationSystem) createInsightFromTemporalCorrelation(event *domain.UnifiedEvent, corr TemporalCorrelation) {
	explanation := s.explanationEngine.ExplainTemporalCorrelation(corr)

	insight := domain.Insight{
		ID:          fmt.Sprintf("temporal-corr-%d", time.Now().UnixNano()),
		Type:        "temporal_correlation",
		Title:       explanation.Summary,
		Description: explanation.Details,
		Severity:    s.mapConfidenceToSeverity(corr.Confidence),
		Source:      "temporal_correlator",
		Timestamp:   time.Now(),
		Metadata: map[string]interface{}{
			"pattern":      corr.Pattern,
			"confidence":   corr.Confidence,
			"occurrences":  corr.Occurrences,
			"time_delta":   corr.TimeDelta.String(),
			"source_event": corr.SourceEvent.EventType,
			"target_event": corr.TargetEvent.EventType,
			"explanation":  explanation,
			"actionable":   explanation.Actionable,
		},
	}

	s.sendInsight(insight)

	// Persist the temporal correlation for historical analysis
	if s.persistenceService != nil {
		go func() {
			ctx := context.Background()
			err := s.persistenceService.PersistCorrelation(ctx, event, corr, explanation, corr.Confidence)
			if err != nil {
				s.logger.Warn("Failed to persist temporal correlation", zap.Error(err))
			}
		}()
	}
}

// createInsightFromSequenceCorrelation creates insights from sequence correlations
func (s *SimpleCorrelationSystem) createInsightFromSequenceCorrelation(event *domain.UnifiedEvent, corr SequenceCorrelation) {
	explanation := s.explanationEngine.ExplainSequenceCorrelation(corr)

	insight := domain.Insight{
		ID:          fmt.Sprintf("sequence-corr-%d", time.Now().UnixNano()),
		Type:        "sequence_correlation",
		Title:       explanation.Summary,
		Description: explanation.Details,
		Severity:    s.mapConfidenceToSeverity(corr.Confidence),
		Source:      "sequence_detector",
		Timestamp:   time.Now(),
		Metadata: map[string]interface{}{
			"pattern":      corr.Pattern.Pattern,
			"confidence":   corr.Confidence,
			"occurrences":  corr.Pattern.Occurrences,
			"duration":     corr.Duration.String(),
			"events_count": len(corr.Events),
			"explanation":  explanation,
			"actionable":   explanation.Actionable,
		},
	}

	s.sendInsight(insight)

	// Persist the sequence correlation for historical analysis
	if s.persistenceService != nil {
		go func() {
			ctx := context.Background()
			err := s.persistenceService.PersistCorrelation(ctx, event, corr, explanation, corr.Confidence)
			if err != nil {
				s.logger.Warn("Failed to persist sequence correlation", zap.Error(err))
			}
		}()
	}
}

// sendInsight sends an insight to the channel
func (s *SimpleCorrelationSystem) sendInsight(insight domain.Insight) {
	select {
	case s.insightChan <- insight:
		s.updateInsightStats()
	case <-s.ctx.Done():
		return
	default:
		// Channel full, log and drop
		s.logger.Debug("Insight channel full, dropping insight",
			zap.String("insight_id", insight.ID),
			zap.String("insight_type", insight.Type))
	}
}

// mapConfidenceToSeverity maps correlation confidence to insight severity
func (s *SimpleCorrelationSystem) mapConfidenceToSeverity(confidence float64) domain.SeverityLevel {
	if confidence >= 0.9 {
		return domain.SeverityHigh
	} else if confidence >= 0.7 {
		return domain.SeverityMedium
	} else if confidence >= 0.5 {
		return domain.SeverityLow
	}
	return domain.SeverityInfo
}

// cleanupRoutine performs periodic cleanup
func (s *SimpleCorrelationSystem) cleanupRoutine() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Cleanup old sequences
			s.sequenceDetector.cleanupSequences()

			// Cleanup old temporal data
			s.temporalCorrelator.eventWindow.Clean()

		case <-s.ctx.Done():
			return
		}
	}
}

// Statistics update methods

func (s *SimpleCorrelationSystem) updateProcessingStats(duration time.Duration) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()

	s.stats.EventsProcessed++

	// Update rolling average processing time
	if s.stats.AvgProcessingTime == 0 {
		s.stats.AvgProcessingTime = duration
	} else {
		s.stats.AvgProcessingTime = (s.stats.AvgProcessingTime + duration) / 2
	}
}

func (s *SimpleCorrelationSystem) updateK8sStats(count int) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	s.stats.K8sCorrelationsFound += int64(count)
}

func (s *SimpleCorrelationSystem) updateTemporalStats(count int) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	s.stats.TemporalCorrelations += int64(count)
}

func (s *SimpleCorrelationSystem) updateSequenceStats(count int) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	s.stats.SequenceCorrelations += int64(count)
}

func (s *SimpleCorrelationSystem) updateInsightStats() {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	s.stats.TotalInsightsGenerated++
}

// Public interface methods

// Insights returns the channel of generated insights
func (s *SimpleCorrelationSystem) Insights() <-chan domain.Insight {
	return s.insightChan
}

// GetStats returns processing statistics
func (s *SimpleCorrelationSystem) GetStats() map[string]interface{} {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return map[string]interface{}{
		"running":                  s.running,
		"events_processed":         s.stats.EventsProcessed,
		"k8s_correlations_found":   s.stats.K8sCorrelationsFound,
		"temporal_correlations":    s.stats.TemporalCorrelations,
		"sequence_correlations":    s.stats.SequenceCorrelations,
		"total_insights_generated": s.stats.TotalInsightsGenerated,
		"avg_processing_time_ms":   s.stats.AvgProcessingTime.Milliseconds(),
		"event_buffer_size":        len(s.eventChan),
		"insight_queue_size":       len(s.insightChan),
	}
}

// Stop gracefully shuts down the correlation system
func (s *SimpleCorrelationSystem) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.logger.Info("Stopping simple correlation system...")

	// Cancel context to stop all goroutines
	s.cancel()

	// Wait for all workers to finish
	s.wg.Wait()

	// Close channels
	close(s.eventChan)
	close(s.insightChan)

	s.running = false
	s.logger.Info("Simple correlation system stopped")

	return nil
}

// UpdateConfiguration allows runtime configuration updates
func (s *SimpleCorrelationSystem) UpdateConfiguration(config SimpleSystemConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = config
	s.logger.Info("Simple correlation system configuration updated")

	return nil
}
