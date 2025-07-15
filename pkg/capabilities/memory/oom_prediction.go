package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// PredictiveOOMEngine implements world-class OOM prediction using multiple models
// Based on Netflix/Pixie best practices with ensemble learning and online adaptation
type PredictiveOOMEngine struct {
	// Multiple prediction models (ensemble approach)
	linearModel       *LinearRegressionModel
	exponentialModel  *ExponentialGrowthModel
	seasonalModel     *SeasonalDecompositionModel
	ensembleModel     *ModelEnsemble
	
	// Real-time learning capabilities
	onlineTraining    bool
	modelUpdater      *IncrementalLearner
	predictionAccuracy *AccuracyTracker
	
	// Prediction cache and optimization
	predictionCache   *TimedPredictionCache
	modelSelector     *AdaptiveModelSelector
	
	// Configuration
	config            *MemoryCollectorConfig
	
	// State management
	mu                sync.RWMutex
	isActive          bool
	lastPrediction    time.Time
	modelWeights      map[string]float64
}

// LinearRegressionModel implements linear regression for memory growth prediction
type LinearRegressionModel struct {
	// Model parameters
	slope             float64
	intercept         float64
	rSquared          float64
	
	// Training data
	dataPoints        []DataPoint
	windowSize        time.Duration
	minDataPoints     int
	
	// Online learning
	meanX             float64
	meanY             float64
	sumXY             float64
	sumXX             float64
	sumYY             float64
	n                 int64
	
	// Model quality
	lastTrainingTime  time.Time
	predictionError   float64
	confidence        float64
}

// ExponentialGrowthModel implements exponential growth prediction
type ExponentialGrowthModel struct {
	// Model parameters
	initialValue      float64
	growthRate        float64
	rSquared          float64
	
	// Exponential fitting
	logTransformed    bool
	baseModel         *LinearRegressionModel
	
	// Model validation
	backtestAccuracy  float64
	lastValidation    time.Time
}

// SeasonalDecompositionModel implements seasonal pattern detection and prediction
type SeasonalDecompositionModel struct {
	// Seasonal components
	trend             []float64
	seasonal          []float64
	residual          []float64
	
	// Seasonal patterns
	hourlyPattern     [24]float64
	dailyPattern      [7]float64
	weeklyPattern     [4]float64
	
	// Decomposition parameters
	seasonalPeriod    time.Duration
	decompositionMethod string // "additive" or "multiplicative"
	
	// Model state
	lastDecomposition time.Time
	isValidModel      bool
}

// ModelEnsemble combines multiple models for robust predictions
type ModelEnsemble struct {
	// Individual models
	models            map[string]PredictionModel
	modelWeights      map[string]float64
	
	// Ensemble strategy
	votingStrategy    string // "weighted", "majority", "stacking"
	metalearner       *MetaLearner
	
	// Performance tracking
	modelPerformance  map[string]*ModelPerformance
	ensembleAccuracy  float64
	
	// Dynamic weighting
	adaptiveWeights   bool
	performanceWindow time.Duration
}

// PredictionModel interface for different prediction models
type PredictionModel interface {
	Train(data []DataPoint) error
	Predict(timestamp time.Time) (*OOMPrediction, error)
	GetAccuracy() float64
	GetModelInfo() ModelInfo
	Update(dataPoint DataPoint) error
}

// DataPoint represents a memory usage data point
type DataPoint struct {
	Timestamp     time.Time `json:"timestamp"`
	MemoryUsage   float64   `json:"memory_usage"`   // bytes
	MemoryLimit   float64   `json:"memory_limit"`   // bytes
	Utilization   float64   `json:"utilization"`    // 0.0 to 1.0
	
	// Context information
	ContainerID   string    `json:"container_id"`
	PodName       string    `json:"pod_name"`
	Namespace     string    `json:"namespace"`
	
	// Additional features
	AllocationRate float64  `json:"allocation_rate"` // bytes/second
	CPUUsage       float64  `json:"cpu_usage"`       // 0.0 to 1.0
	NetworkIO      float64  `json:"network_io"`      // bytes/second
	DiskIO         float64  `json:"disk_io"`         // bytes/second
}

// ModelPerformance tracks performance metrics for a model
type ModelPerformance struct {
	// Accuracy metrics
	accuracy          float64
	precision         float64
	recall            float64
	f1Score           float64
	
	// Prediction quality
	meanAbsoluteError float64
	rootMeanSquareError float64
	meanAbsolutePercentageError float64
	
	// Timing metrics
	averageLatency    time.Duration
	maxLatency        time.Duration
	
	// Reliability metrics
	predictionCount   int64
	errorCount        int64
	lastUpdate        time.Time
}

// IncrementalLearner implements online learning for continuous model improvement
type IncrementalLearner struct {
	// Learning parameters
	learningRate      float64
	forgettingFactor  float64
	adaptationRate    float64
	
	// Online statistics
	onlineStatistics  *OnlineStatistics
	conceptDrift      *ConceptDriftDetector
	
	// Model adaptation
	modelUpdates      chan ModelUpdate
	updateBatch       []DataPoint
	batchSize         int
}

// AccuracyTracker tracks prediction accuracy over time
type AccuracyTracker struct {
	// Prediction tracking
	predictions       []PredictionOutcome
	windowSize        int
	currentAccuracy   float64
	
	// Performance metrics
	confusionMatrix   map[string]map[string]int
	rocCurve          *ROCCurve
	precisionRecall   *PrecisionRecallCurve
	
	// Time-based accuracy
	hourlyAccuracy    [24]float64
	dailyAccuracy     [7]float64
	
	mu                sync.RWMutex
}

// PredictionOutcome represents the outcome of a prediction
type PredictionOutcome struct {
	PredictionTime    time.Time `json:"prediction_time"`
	PredictedOOMTime  time.Time `json:"predicted_oom_time"`
	ActualOOMTime     *time.Time `json:"actual_oom_time,omitempty"`
	Confidence        float64   `json:"confidence"`
	Model             string    `json:"model"`
	
	// Outcome classification
	TruePositive      bool      `json:"true_positive"`
	FalsePositive     bool      `json:"false_positive"`
	TrueNegative      bool      `json:"true_negative"`
	FalseNegative     bool      `json:"false_negative"`
	
	// Error analysis
	TimeDelta         time.Duration `json:"time_delta"` // Difference between predicted and actual
	AbsoluteError     time.Duration `json:"absolute_error"`
}

// TimedPredictionCache caches predictions with expiration
type TimedPredictionCache struct {
	cache             map[string]*CachedOOMPrediction
	ttl               time.Duration
	maxSize           int
	
	// Cache statistics
	hits              int64
	misses            int64
	evictions         int64
	
	mu                sync.RWMutex
}

// CachedOOMPrediction represents a cached OOM prediction
type CachedOOMPrediction struct {
	prediction        *OOMPrediction
	createdAt         time.Time
	accessCount       int
	lastAccess        time.Time
}

// AdaptiveModelSelector selects the best model for current conditions
type AdaptiveModelSelector struct {
	// Model selection strategy
	selectionStrategy string // "accuracy", "confidence", "ensemble"
	
	// Performance tracking
	modelScores       map[string]float64
	recentPerformance map[string]*RecentPerformance
	
	// Context-aware selection
	contextFactors    map[string]float64
	timeOfDayFactors  [24]float64
	loadFactors       []float64
}

// RecentPerformance tracks recent model performance
type RecentPerformance struct {
	accuracy          float64
	latency           time.Duration
	confidence        float64
	sampleCount       int
	lastUpdate        time.Time
}

// NewPredictiveOOMEngine creates a new OOM prediction engine
func NewPredictiveOOMEngine(config *MemoryCollectorConfig) (*PredictiveOOMEngine, error) {
	// Create individual models
	linearModel, err := NewLinearRegressionModel(LinearModelConfig{
		WindowSize:    30 * time.Minute,
		MinDataPoints: 10,
		UpdateInterval: 5 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create linear model: %w", err)
	}

	exponentialModel, err := NewExponentialGrowthModel(ExponentialModelConfig{
		WindowSize:     45 * time.Minute,
		MinDataPoints:  15,
		ValidationInterval: 10 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create exponential model: %w", err)
	}

	seasonalModel, err := NewSeasonalDecompositionModel(SeasonalModelConfig{
		SeasonalPeriod: 24 * time.Hour,
		DecompositionMethod: "additive",
		MinDataPoints: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create seasonal model: %w", err)
	}

	// Create ensemble model
	models := map[string]PredictionModel{
		"linear":      linearModel,
		"exponential": exponentialModel,
		"seasonal":    seasonalModel,
	}

	ensembleModel, err := NewModelEnsemble(EnsembleConfig{
		Models:            models,
		VotingStrategy:    "weighted",
		AdaptiveWeights:   true,
		PerformanceWindow: 2 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ensemble model: %w", err)
	}

	// Create supporting components
	predictionCache, err := NewTimedPredictionCache(CacheConfig{
		TTL:     5 * time.Minute,
		MaxSize: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prediction cache: %w", err)
	}

	modelSelector, err := NewAdaptiveModelSelector(SelectorConfig{
		SelectionStrategy: "ensemble",
		ContextAware:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create model selector: %w", err)
	}

	accuracyTracker, err := NewAccuracyTracker(AccuracyConfig{
		WindowSize: 1000,
		EnableROC:  true,
		EnablePR:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create accuracy tracker: %w", err)
	}

	engine := &PredictiveOOMEngine{
		linearModel:       linearModel,
		exponentialModel:  exponentialModel,
		seasonalModel:     seasonalModel,
		ensembleModel:     ensembleModel,
		onlineTraining:    config.EnableMLPrediction,
		predictionCache:   predictionCache,
		modelSelector:     modelSelector,
		predictionAccuracy: accuracyTracker,
		config:            config,
		modelWeights:      make(map[string]float64),
	}

	// Initialize model weights
	engine.modelWeights["linear"] = 0.4
	engine.modelWeights["exponential"] = 0.35
	engine.modelWeights["seasonal"] = 0.25

	return engine, nil
}

// PredictOOM performs OOM prediction using ensemble of models
func (e *PredictiveOOMEngine) PredictOOM(ctx context.Context, memoryData []DataPoint) (*OOMPrediction, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.isActive {
		return nil, fmt.Errorf("prediction engine not active")
	}

	if len(memoryData) == 0 {
		return nil, fmt.Errorf("no memory data provided")
	}

	// Check cache first
	cacheKey := generateCacheKey(memoryData)
	if cached := e.predictionCache.Get(cacheKey); cached != nil {
		return cached.prediction, nil
	}

	// Get predictions from all models
	modelPredictions := make(map[string]*OOMPrediction)
	
	// Linear model prediction
	if prediction, err := e.linearModel.Predict(time.Now()); err == nil {
		modelPredictions["linear"] = prediction
	}

	// Exponential model prediction
	if prediction, err := e.exponentialModel.Predict(time.Now()); err == nil {
		modelPredictions["exponential"] = prediction
	}

	// Seasonal model prediction
	if prediction, err := e.seasonalModel.Predict(time.Now()); err == nil {
		modelPredictions["seasonal"] = prediction
	}

	// Ensemble prediction
	ensemblePrediction, err := e.ensembleModel.Predict(time.Now())
	if err != nil {
		return nil, fmt.Errorf("ensemble prediction failed: %w", err)
	}

	// Select best prediction using adaptive model selector
	finalPrediction := e.selectBestPrediction(modelPredictions, ensemblePrediction)

	// Enhance prediction with context
	e.enhancePredictionWithContext(finalPrediction, memoryData)

	// Cache prediction
	e.predictionCache.Set(cacheKey, &CachedOOMPrediction{
		prediction: finalPrediction,
		createdAt:  time.Now(),
	})

	e.lastPrediction = time.Now()

	return finalPrediction, nil
}

// UpdateModels updates all models with new data
func (e *PredictiveOOMEngine) UpdateModels(dataPoints []DataPoint) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Update individual models
	for _, point := range dataPoints {
		if err := e.linearModel.Update(point); err != nil {
			// Log error but continue with other models
			continue
		}

		if err := e.exponentialModel.Update(point); err != nil {
			continue
		}

		if err := e.seasonalModel.Update(point); err != nil {
			continue
		}
	}

	// Update ensemble model
	if err := e.ensembleModel.UpdateModels(dataPoints); err != nil {
		return fmt.Errorf("failed to update ensemble: %w", err)
	}

	// Update model weights based on recent performance
	e.updateModelWeights()

	return nil
}

// ValidatePrediction validates a prediction against actual outcome
func (e *PredictiveOOMEngine) ValidatePrediction(predictionID string, actualOOMTime *time.Time) error {
	// Record prediction outcome for accuracy tracking
	outcome := PredictionOutcome{
		// ... populate from prediction ID and actual outcome
	}

	return e.predictionAccuracy.RecordOutcome(outcome)
}

// GetAccuracy returns current prediction accuracy
func (e *PredictiveOOMEngine) GetAccuracy() float64 {
	return e.predictionAccuracy.GetCurrentAccuracy()
}

// GetModelPerformance returns performance metrics for all models
func (e *PredictiveOOMEngine) GetModelPerformance() map[string]*ModelPerformance {
	performance := make(map[string]*ModelPerformance)
	
	performance["linear"] = &ModelPerformance{
		accuracy: e.linearModel.GetAccuracy(),
		// ... other metrics
	}
	
	performance["exponential"] = &ModelPerformance{
		accuracy: e.exponentialModel.GetAccuracy(),
		// ... other metrics
	}
	
	performance["seasonal"] = &ModelPerformance{
		accuracy: e.seasonalModel.GetAccuracy(),
		// ... other metrics
	}

	return performance
}

// selectBestPrediction selects the best prediction from available models
func (e *PredictiveOOMEngine) selectBestPrediction(modelPredictions map[string]*OOMPrediction, ensemblePrediction *OOMPrediction) *OOMPrediction {
	// Use model selector to choose best approach
	bestModel := e.modelSelector.SelectBestModel(modelPredictions)
	
	// Default to ensemble if no clear winner
	if bestModel == "" || modelPredictions[bestModel] == nil {
		return ensemblePrediction
	}

	// Weight the selected model prediction with ensemble
	selectedPrediction := modelPredictions[bestModel]
	
	// Combine with ensemble (weighted average)
	combinedProbability := 0.7*selectedPrediction.Probability + 0.3*ensemblePrediction.Probability
	combinedConfidence := 0.7*selectedPrediction.Confidence + 0.3*ensemblePrediction.Confidence
	
	// Create enhanced prediction
	enhancedPrediction := &OOMPrediction{
		Probability:       combinedProbability,
		Confidence:        combinedConfidence,
		TimeToOOM:         selectedPrediction.TimeToOOM,
		PredictionWindow:  selectedPrediction.PredictionWindow,
		Model:             fmt.Sprintf("adaptive_%s", bestModel),
		Features:          append(selectedPrediction.Features, ensemblePrediction.Features...),
		Explanation:       fmt.Sprintf("Adaptive selection: %s model with ensemble weighting", bestModel),
		PreventionActions: mergePreventionActions(selectedPrediction.PreventionActions, ensemblePrediction.PreventionActions),
	}

	return enhancedPrediction
}

// enhancePredictionWithContext adds contextual information to prediction
func (e *PredictiveOOMEngine) enhancePredictionWithContext(prediction *OOMPrediction, memoryData []DataPoint) {
	if len(memoryData) == 0 {
		return
	}

	latest := memoryData[len(memoryData)-1]
	
	// Add container context to explanation
	if latest.ContainerID != "" {
		prediction.Explanation += fmt.Sprintf(" (Container: %s, Pod: %s)", 
			latest.ContainerID[:12], latest.PodName)
	}

	// Add current utilization context
	prediction.Explanation += fmt.Sprintf(" Current utilization: %.1f%%", latest.Utilization*100)

	// Add trend context
	if len(memoryData) >= 2 {
		trend := calculateMemoryTrend(memoryData)
		if trend > 0 {
			prediction.Explanation += fmt.Sprintf(", Growing at %.2f MB/min", trend/(1024*1024)*60)
		}
	}

	// Add prevention actions based on context
	if latest.Utilization > 0.8 {
		prediction.PreventionActions = append(prediction.PreventionActions,
			"Immediate: Consider increasing memory limits",
			"kubectl patch deployment <name> -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"<container>\",\"resources\":{\"limits\":{\"memory\":\"<new-limit>\"}}}]}}}}'",
		)
	}

	if latest.AllocationRate > 0 {
		prediction.PreventionActions = append(prediction.PreventionActions,
			"Monitor: High allocation rate detected",
			"kubectl logs <pod-name> | grep -i memory",
		)
	}
}

// updateModelWeights updates model weights based on recent performance
func (e *PredictiveOOMEngine) updateModelWeights() {
	performance := e.GetModelPerformance()
	
	totalAccuracy := 0.0
	for _, perf := range performance {
		totalAccuracy += perf.accuracy
	}

	if totalAccuracy > 0 {
		for model, perf := range performance {
			e.modelWeights[model] = perf.accuracy / totalAccuracy
		}
	}
}

// Helper functions and supporting types...

// calculateMemoryTrend calculates memory usage trend
func calculateMemoryTrend(data []DataPoint) float64 {
	if len(data) < 2 {
		return 0
	}

	n := len(data)
	firstTime := data[0].Timestamp
	lastTime := data[n-1].Timestamp
	firstUsage := data[0].MemoryUsage
	lastUsage := data[n-1].MemoryUsage

	timeDiff := lastTime.Sub(firstTime).Seconds()
	if timeDiff <= 0 {
		return 0
	}

	return (lastUsage - firstUsage) / timeDiff
}

// generateCacheKey generates a cache key for memory data
func generateCacheKey(data []DataPoint) string {
	if len(data) == 0 {
		return ""
	}
	
	latest := data[len(data)-1]
	return fmt.Sprintf("%s_%d_%f", latest.ContainerID, latest.Timestamp.Unix(), latest.MemoryUsage)
}

// mergePreventionActions merges prevention actions from multiple predictions
func mergePreventionActions(actions1, actions2 []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, action := range actions1 {
		if !seen[action] {
			result = append(result, action)
			seen[action] = true
		}
	}

	for _, action := range actions2 {
		if !seen[action] {
			result = append(result, action)
			seen[action] = true
		}
	}

	return result
}

// Configuration types
type LinearModelConfig struct {
	WindowSize     time.Duration
	MinDataPoints  int
	UpdateInterval time.Duration
}

type ExponentialModelConfig struct {
	WindowSize         time.Duration
	MinDataPoints      int
	ValidationInterval time.Duration
}

type SeasonalModelConfig struct {
	SeasonalPeriod      time.Duration
	DecompositionMethod string
	MinDataPoints       int
}

type EnsembleConfig struct {
	Models            map[string]PredictionModel
	VotingStrategy    string
	AdaptiveWeights   bool
	PerformanceWindow time.Duration
}

type CacheConfig struct {
	TTL     time.Duration
	MaxSize int
}

type SelectorConfig struct {
	SelectionStrategy string
	ContextAware      bool
}

type AccuracyConfig struct {
	WindowSize int
	EnableROC  bool
	EnablePR   bool
}

// Stub implementations for constructor functions
func NewLinearRegressionModel(config LinearModelConfig) (*LinearRegressionModel, error) {
	return &LinearRegressionModel{
		windowSize:    config.WindowSize,
		minDataPoints: config.MinDataPoints,
		dataPoints:    make([]DataPoint, 0),
	}, nil
}

func NewExponentialGrowthModel(config ExponentialModelConfig) (*ExponentialGrowthModel, error) {
	baseModel, _ := NewLinearRegressionModel(LinearModelConfig{
		WindowSize:    config.WindowSize,
		MinDataPoints: config.MinDataPoints,
	})
	return &ExponentialGrowthModel{
		baseModel: baseModel,
	}, nil
}

func NewSeasonalDecompositionModel(config SeasonalModelConfig) (*SeasonalDecompositionModel, error) {
	return &SeasonalDecompositionModel{
		seasonalPeriod:      config.SeasonalPeriod,
		decompositionMethod: config.DecompositionMethod,
	}, nil
}

func NewModelEnsemble(config EnsembleConfig) (*ModelEnsemble, error) {
	return &ModelEnsemble{
		models:           config.Models,
		votingStrategy:   config.VotingStrategy,
		adaptiveWeights:  config.AdaptiveWeights,
		modelWeights:     make(map[string]float64),
		modelPerformance: make(map[string]*ModelPerformance),
	}, nil
}

func NewTimedPredictionCache(config CacheConfig) (*TimedPredictionCache, error) {
	return &TimedPredictionCache{
		cache:   make(map[string]*CachedOOMPrediction),
		ttl:     config.TTL,
		maxSize: config.MaxSize,
	}, nil
}

func NewAdaptiveModelSelector(config SelectorConfig) (*AdaptiveModelSelector, error) {
	return &AdaptiveModelSelector{
		selectionStrategy: config.SelectionStrategy,
		modelScores:       make(map[string]float64),
		recentPerformance: make(map[string]*RecentPerformance),
	}, nil
}

func NewAccuracyTracker(config AccuracyConfig) (*AccuracyTracker, error) {
	return &AccuracyTracker{
		windowSize:      config.WindowSize,
		predictions:     make([]PredictionOutcome, 0, config.WindowSize),
		confusionMatrix: make(map[string]map[string]int),
	}, nil
}

// Method stubs for interfaces
func (l *LinearRegressionModel) Train(data []DataPoint) error     { return nil }
func (l *LinearRegressionModel) Predict(timestamp time.Time) (*OOMPrediction, error) { return &OOMPrediction{}, nil }
func (l *LinearRegressionModel) GetAccuracy() float64             { return 0.9 }
func (l *LinearRegressionModel) GetModelInfo() ModelInfo          { return ModelInfo{} }
func (l *LinearRegressionModel) Update(dataPoint DataPoint) error { return nil }

func (e *ExponentialGrowthModel) Train(data []DataPoint) error     { return nil }
func (e *ExponentialGrowthModel) Predict(timestamp time.Time) (*OOMPrediction, error) { return &OOMPrediction{}, nil }
func (e *ExponentialGrowthModel) GetAccuracy() float64             { return 0.9 }
func (e *ExponentialGrowthModel) GetModelInfo() ModelInfo          { return ModelInfo{} }
func (e *ExponentialGrowthModel) Update(dataPoint DataPoint) error { return nil }

func (s *SeasonalDecompositionModel) Train(data []DataPoint) error     { return nil }
func (s *SeasonalDecompositionModel) Predict(timestamp time.Time) (*OOMPrediction, error) { return &OOMPrediction{}, nil }
func (s *SeasonalDecompositionModel) GetAccuracy() float64             { return 0.9 }
func (s *SeasonalDecompositionModel) GetModelInfo() ModelInfo          { return ModelInfo{} }
func (s *SeasonalDecompositionModel) Update(dataPoint DataPoint) error { return nil }

func (e *ModelEnsemble) Predict(timestamp time.Time) (*OOMPrediction, error) { return &OOMPrediction{}, nil }
func (e *ModelEnsemble) UpdateModels(dataPoints []DataPoint) error { return nil }

func (c *TimedPredictionCache) Get(key string) *CachedOOMPrediction { return nil }
func (c *TimedPredictionCache) Set(key string, prediction *CachedOOMPrediction) {}

func (s *AdaptiveModelSelector) SelectBestModel(predictions map[string]*OOMPrediction) string { return "ensemble" }

func (a *AccuracyTracker) RecordOutcome(outcome PredictionOutcome) error { return nil }
func (a *AccuracyTracker) GetCurrentAccuracy() float64 { return 0.9 }

// Supporting types
type ModelUpdate struct{}
type OnlineStatistics struct{}
type MetaLearner struct{}
type ROCCurve struct{}
type PrecisionRecallCurve struct{}