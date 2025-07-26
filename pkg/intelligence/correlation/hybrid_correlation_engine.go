package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// HybridCorrelationEngine combines semantic AI with simple structural correlations
type HybridCorrelationEngine struct {
	logger *zap.Logger

	// Existing semantic engine (for AI-powered analysis)
	semanticEngine *SemanticCorrelationEngine

	// New simple correlation engines (for structural/zero-config)
	k8sCorrelator      *K8sNativeCorrelator
	temporalCorrelator *TemporalCorrelator
	sequenceDetector   *SequenceDetector
	confidenceScorer   *ConfidenceScorer
	explanationEngine  *ExplanationEngine

	// Configuration
	config HybridConfig

	// State
	running bool
	mu      sync.RWMutex
}

// HybridConfig configures the hybrid engine
type HybridConfig struct {
	// Enable/disable different correlation types
	EnableK8sNative bool
	EnableTemporal  bool
	EnableSequence  bool
	EnableSemantic  bool

	// Merge strategy
	MergeStrategy    string // "priority", "confidence", "all"
	ConfidenceWeight float64

	// Performance tuning
	MaxConcurrentProcessing int
}

// DefaultHybridConfig returns sensible defaults
func DefaultHybridConfig() HybridConfig {
	return HybridConfig{
		EnableK8sNative:         true, // Always on - free correlations!
		EnableTemporal:          true, // Usually valuable
		EnableSequence:          true, // Pattern recognition
		EnableSemantic:          true, // AI insights
		MergeStrategy:           "confidence",
		ConfidenceWeight:        0.7,
		MaxConcurrentProcessing: 4,
	}
}

// NewHybridCorrelationEngine creates a hybrid engine
func NewHybridCorrelationEngine(logger *zap.Logger, config HybridConfig) *HybridCorrelationEngine {
	return &HybridCorrelationEngine{
		logger: logger,

		// Keep existing semantic engine
		semanticEngine: NewSemanticCorrelationEngine(),

		// Add new simple correlators
		k8sCorrelator:      NewK8sNativeCorrelator(logger),
		temporalCorrelator: NewTemporalCorrelator(logger, DefaultTemporalConfig()),
		sequenceDetector:   NewSequenceDetector(logger, DefaultSequenceConfig()),
		confidenceScorer:   NewConfidenceScorer(logger, DefaultScorerConfig()),
		explanationEngine:  NewExplanationEngine(),

		config: config,
	}
}

// ProcessEvent processes an event through all enabled correlation engines
func (h *HybridCorrelationEngine) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) (*HybridCorrelationResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if !h.running {
		return nil, fmt.Errorf("hybrid correlation engine not running")
	}

	startTime := time.Now()
	result := &HybridCorrelationResult{
		Event:        event,
		Correlations: make([]EnrichedCorrelation, 0),
	}

	// Channel for collecting results from different engines
	resultChan := make(chan CorrelationResult, h.config.MaxConcurrentProcessing)
	var wg sync.WaitGroup

	// 1. K8s Native Correlations (instant, 100% confidence)
	if h.config.EnableK8sNative {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k8sCorrelations := h.k8sCorrelator.FindCorrelations(event)
			for _, corr := range k8sCorrelations {
				resultChan <- CorrelationResult{
					Type:        "k8s-native",
					Correlation: corr,
					Confidence:  corr.Confidence,
					Source:      "structural",
					Explanation: h.explanationEngine.ExplainK8sCorrelation(corr),
				}
			}
		}()
	}

	// 2. Temporal Correlations (time-based patterns)
	if h.config.EnableTemporal {
		wg.Add(1)
		go func() {
			defer wg.Done()
			temporalCorrelations := h.temporalCorrelator.Process(event)
			for _, corr := range temporalCorrelations {
				resultChan <- CorrelationResult{
					Type:        "temporal",
					Correlation: corr,
					Confidence:  corr.Confidence,
					Source:      "pattern",
					Explanation: h.explanationEngine.ExplainTemporalCorrelation(corr),
				}
			}
		}()
	}

	// 3. Sequence Correlations (sequential patterns)
	if h.config.EnableSequence {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sequenceCorrelations := h.sequenceDetector.Process(event)
			for _, corr := range sequenceCorrelations {
				resultChan <- CorrelationResult{
					Type:        "sequence",
					Correlation: corr,
					Confidence:  corr.Confidence,
					Source:      "sequence",
					Explanation: h.explanationEngine.ExplainSequenceCorrelation(corr),
				}
			}
		}()
	}

	// 4. Semantic Correlations (AI-powered)
	if h.config.EnableSemantic {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Process through existing semantic engine
			h.semanticEngine.ProcessEvent(ctx, event)
			// Get semantic insights and convert to correlations
			// (This would need adaptation of the semantic engine)
		}()
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect all correlation results
	correlationResults := make([]CorrelationResult, 0)
	for corrResult := range resultChan {
		correlationResults = append(correlationResults, corrResult)
	}

	// Merge and rank correlations
	result.Correlations = h.mergeCorrelations(correlationResults)
	result.ProcessingTime = time.Since(startTime)

	return result, nil
}

// HybridCorrelationResult contains all correlation results
type HybridCorrelationResult struct {
	Event          *domain.UnifiedEvent
	Correlations   []EnrichedCorrelation
	ProcessingTime time.Duration
	Stats          ProcessingStats
}

// EnrichedCorrelation combines correlation with metadata
type EnrichedCorrelation struct {
	Type        string // k8s-native, temporal, sequence, semantic
	Source      string // structural, pattern, sequence, ai
	Confidence  float64
	Correlation interface{} // Actual correlation object
	Explanation CorrelationExplanation
	Rank        int // After merging/ranking
}

// CorrelationResult is intermediate result from each engine
type CorrelationResult struct {
	Type        string
	Correlation interface{}
	Confidence  float64
	Source      string
	Explanation CorrelationExplanation
}

// ProcessingStats contains processing statistics
type ProcessingStats struct {
	K8sCorrelations      int
	TemporalCorrelations int
	SequenceCorrelations int
	SemanticCorrelations int
	TotalProcessingTime  time.Duration
}

// mergeCorrelations combines results from different engines
func (h *HybridCorrelationEngine) mergeCorrelations(results []CorrelationResult) []EnrichedCorrelation {
	merged := make([]EnrichedCorrelation, 0, len(results))

	for i, result := range results {
		enriched := EnrichedCorrelation{
			Type:        result.Type,
			Source:      result.Source,
			Confidence:  result.Confidence,
			Correlation: result.Correlation,
			Explanation: result.Explanation,
			Rank:        i + 1, // Will be re-ranked
		}
		merged = append(merged, enriched)
	}

	// Apply merge strategy
	switch h.config.MergeStrategy {
	case "confidence":
		h.sortByConfidence(merged)
	case "priority":
		h.sortByPriority(merged)
	case "all":
		// Keep original order
	}

	// Re-rank after sorting
	for i := range merged {
		merged[i].Rank = i + 1
	}

	return merged
}

// sortByConfidence sorts correlations by confidence score
func (h *HybridCorrelationEngine) sortByConfidence(correlations []EnrichedCorrelation) {
	// Sort by confidence descending
	for i := 0; i < len(correlations)-1; i++ {
		for j := i + 1; j < len(correlations); j++ {
			if correlations[i].Confidence < correlations[j].Confidence {
				correlations[i], correlations[j] = correlations[j], correlations[i]
			}
		}
	}
}

// sortByPriority sorts by predefined priority (K8s > Temporal > Sequence > Semantic)
func (h *HybridCorrelationEngine) sortByPriority(correlations []EnrichedCorrelation) {
	priority := map[string]int{
		"k8s-native": 1,
		"temporal":   2,
		"sequence":   3,
		"semantic":   4,
	}

	for i := 0; i < len(correlations)-1; i++ {
		for j := i + 1; j < len(correlations); j++ {
			if priority[correlations[i].Type] > priority[correlations[j].Type] {
				correlations[i], correlations[j] = correlations[j], correlations[i]
			}
		}
	}
}

// Start initializes all correlation engines
func (h *HybridCorrelationEngine) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return fmt.Errorf("hybrid correlation engine already running")
	}

	// Start semantic engine
	if h.config.EnableSemantic {
		if err := h.semanticEngine.Start(); err != nil {
			return fmt.Errorf("failed to start semantic engine: %w", err)
		}
	}

	h.running = true
	h.logger.Info("Hybrid correlation engine started",
		zap.Bool("k8s_native", h.config.EnableK8sNative),
		zap.Bool("temporal", h.config.EnableTemporal),
		zap.Bool("sequence", h.config.EnableSequence),
		zap.Bool("semantic", h.config.EnableSemantic),
	)

	return nil
}

// Stop gracefully shuts down all engines
func (h *HybridCorrelationEngine) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return nil
	}

	// Stop semantic engine
	if h.config.EnableSemantic && h.semanticEngine != nil {
		if err := h.semanticEngine.Stop(); err != nil {
			h.logger.Error("Failed to stop semantic engine", zap.Error(err))
		}
	}

	h.running = false
	h.logger.Info("Hybrid correlation engine stopped")

	return nil
}

// GetStats returns processing statistics
func (h *HybridCorrelationEngine) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"running": h.running,
		"config":  h.config,
	}

	// Add semantic engine stats if available
	if h.config.EnableSemantic && h.semanticEngine != nil {
		stats["semantic_stats"] = h.semanticEngine.GetStats()
	}

	return stats
}

// UpdateConfiguration allows runtime configuration updates
func (h *HybridCorrelationEngine) UpdateConfiguration(config HybridConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.config = config
	h.logger.Info("Hybrid correlation engine configuration updated")

	return nil
}
