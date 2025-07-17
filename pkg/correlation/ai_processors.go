package correlation

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/falseyair/tapio/pkg/correlation/types"
)

// FeatureProcessor extracts and processes features from events for AI analysis
type FeatureProcessor struct {
	// Configuration
	config *FeatureConfig
	
	// Feature extraction state
	featureCache   map[string]*FeatureVector
	cacheMutex     sync.RWMutex
	
	// Statistics for normalization
	featureStats   map[string]*FeatureStats
	statsMutex     sync.RWMutex
}

// FeatureConfig configures feature processing
type FeatureConfig struct {
	// Feature extraction settings
	WindowSize        time.Duration
	MaxFeatures       int
	NormalizationMode string // "zscore", "minmax", "robust"
	
	// Cache settings
	CacheSize         int
	CacheTTL          time.Duration
	
	// Feature selection
	EnabledFeatures   []string
	FeatureWeights    map[string]float64
}

// FeatureVector represents extracted features from events
type FeatureVector struct {
	ID        string
	EventID   string
	Timestamp time.Time
	Features  map[string]float64
	Metadata  map[string]interface{}
	
	// Quality metrics
	Completeness float64 // 0-1, how complete the feature extraction was
	Confidence   float64 // 0-1, confidence in feature accuracy
}

// FeatureStats tracks statistics for feature normalization
type FeatureStats struct {
	Name     string
	Count    int64
	Sum      float64
	SumSq    float64
	Min      float64
	Max      float64
	Mean     float64
	StdDev   float64
	LastUpdate time.Time
}

// NewFeatureProcessor creates a new feature processor
func NewFeatureProcessor(config *FeatureConfig) *FeatureProcessor {
	if config == nil {
		config = &FeatureConfig{
			WindowSize:        5 * time.Minute,
			MaxFeatures:       100,
			NormalizationMode: "zscore",
			CacheSize:         1000,
			CacheTTL:          10 * time.Minute,
			EnabledFeatures:   []string{"temporal", "frequency", "pattern", "correlation"},
			FeatureWeights:    make(map[string]float64),
		}
	}
	
	return &FeatureProcessor{
		config:       config,
		featureCache: make(map[string]*FeatureVector),
		featureStats: make(map[string]*FeatureStats),
	}
}

// ProcessEvent extracts features from a single event
func (fp *FeatureProcessor) ProcessEvent(ctx context.Context, event *types.Event) (*FeatureVector, error) {
	// Check cache first
	if cached := fp.getCachedFeatures(event.ID); cached != nil {
		return cached, nil
	}
	
	features := make(map[string]float64)
	
	// Temporal features
	features["hour_of_day"] = float64(event.Timestamp.Hour())
	features["day_of_week"] = float64(event.Timestamp.Weekday())
	features["timestamp_unix"] = float64(event.Timestamp.Unix())
	
	// Event characteristics
	features["severity_numeric"] = fp.severityToNumeric(string(event.Severity))
	features["message_length"] = float64(len(event.Message))
	features["tag_count"] = float64(len(event.Tags))
	
	// Pattern features
	features["is_error"] = fp.boolToFloat(fp.isErrorEvent(event))
	features["is_restart"] = fp.boolToFloat(fp.isRestartEvent(event))
	features["is_network"] = fp.boolToFloat(fp.isNetworkEvent(event))
	features["is_memory"] = fp.boolToFloat(fp.isMemoryEvent(event))
	
	// Frequency features (require historical context)
	features["event_frequency"] = fp.calculateEventFrequency(event)
	
	// Create feature vector
	vector := &FeatureVector{
		ID:           fmt.Sprintf("fv_%s_%d", event.ID, time.Now().Unix()),
		EventID:      event.ID,
		Timestamp:    time.Now(),
		Features:     features,
		Metadata:     map[string]interface{}{
			"source":    event.Source,
			"namespace": event.Namespace,
		},
		Completeness: fp.calculateCompleteness(features),
		Confidence:   0.85, // Base confidence, could be ML-derived
	}
	
	// Normalize features
	fp.normalizeFeatures(vector)
	
	// Update statistics
	fp.updateFeatureStats(vector)
	
	// Cache the result
	fp.cacheFeatures(vector)
	
	return vector, nil
}

// ProcessBatch processes multiple events efficiently
func (fp *FeatureProcessor) ProcessBatch(ctx context.Context, events []*types.Event) ([]*FeatureVector, error) {
	vectors := make([]*FeatureVector, 0, len(events))
	
	for _, event := range events {
		vector, err := fp.ProcessEvent(ctx, event)
		if err != nil {
			continue // Skip problematic events
		}
		vectors = append(vectors, vector)
	}
	
	return vectors, nil
}

// EmbeddingProcessor handles vector embeddings for similarity analysis
type EmbeddingProcessor struct {
	config      *EmbeddingConfig
	vectorStore *VectorStore
	encoder     *EventEncoder
}

// EmbeddingConfig configures embedding processing
type EmbeddingConfig struct {
	DimensionSize    int
	SimilarityMetric string // "cosine", "euclidean", "manhattan"
	IndexType        string // "flat", "hnsw", "ivf"
	UpdateInterval   time.Duration
}

// VectorStore manages vector storage and similarity search
type VectorStore struct {
	vectors    map[string][]float64
	index      interface{} // Would be actual vector index (FAISS, etc.)
	mutex      sync.RWMutex
}

// EventEncoder converts events to dense vector representations
type EventEncoder struct {
	vocabulary map[string]int
	dimensions int
}

// NewEmbeddingProcessor creates a new embedding processor
func NewEmbeddingProcessor(config *EmbeddingConfig) *EmbeddingProcessor {
	if config == nil {
		config = &EmbeddingConfig{
			DimensionSize:    128,
			SimilarityMetric: "cosine",
			IndexType:        "flat",
			UpdateInterval:   5 * time.Minute,
		}
	}
	
	return &EmbeddingProcessor{
		config: config,
		vectorStore: &VectorStore{
			vectors: make(map[string][]float64),
		},
		encoder: &EventEncoder{
			vocabulary: make(map[string]int),
			dimensions: config.DimensionSize,
		},
	}
}

// GraphProcessor handles graph-based analysis of event relationships
type GraphProcessor struct {
	config     *GraphConfig
	graph      *EventGraph
	algorithms *GraphAlgorithms
}

// GraphConfig configures graph processing
type GraphConfig struct {
	MaxNodes         int
	MaxEdges         int
	DecayRate        float64
	MinEdgeWeight    float64
	ClusteringMethod string
}

// EventGraph represents relationships between events
type EventGraph struct {
	nodes map[string]*GraphNode
	edges map[string]*GraphEdge
	mutex sync.RWMutex
}

// GraphNode represents an event in the graph
type GraphNode struct {
	ID         string
	EventID    string
	Weight     float64
	Attributes map[string]interface{}
	Timestamp  time.Time
}

// GraphEdge represents a relationship between events
type GraphEdge struct {
	ID       string
	Source   string
	Target   string
	Weight   float64
	Type     string
	Metadata map[string]interface{}
}

// GraphAlgorithms provides graph analysis methods
type GraphAlgorithms struct {
	// Community detection, centrality, pathfinding, etc.
}

// NewGraphProcessor creates a new graph processor
func NewGraphProcessor(config *GraphConfig) *GraphProcessor {
	if config == nil {
		config = &GraphConfig{
			MaxNodes:         10000,
			MaxEdges:         50000,
			DecayRate:        0.1,
			MinEdgeWeight:    0.01,
			ClusteringMethod: "louvain",
		}
	}
	
	return &GraphProcessor{
		config: config,
		graph: &EventGraph{
			nodes: make(map[string]*GraphNode),
			edges: make(map[string]*GraphEdge),
		},
		algorithms: &GraphAlgorithms{},
	}
}

// TimeSeriesProcessor handles temporal pattern analysis
type TimeSeriesProcessor struct {
	config     *TimeSeriesConfig
	series     map[string]*TimeSeries
	analyzer   *TimeSeriesAnalyzer
	forecaster *TimeSeriesForecaster
}

// TimeSeriesConfig configures time series processing
type TimeSeriesConfig struct {
	WindowSize      time.Duration
	SamplingRate    time.Duration
	MaxSeries       int
	DetrendMethod   string
	SeasonalPeriods []time.Duration
}

// TimeSeries represents a time series of events
type TimeSeries struct {
	ID         string
	Points     []TimePoint
	Statistics *SeriesStats
	Patterns   []Pattern
}

// TimePoint represents a point in time series
type TimePoint struct {
	Timestamp time.Time
	Value     float64
	Metadata  map[string]interface{}
}

// SeriesStats contains statistical information about a time series
type SeriesStats struct {
	Mean       float64
	Variance   float64
	Trend      float64
	Seasonality float64
	Anomalies  []TimePoint
}

// Pattern represents a detected pattern in time series
type Pattern struct {
	Type        string
	Confidence  float64
	StartTime   time.Time
	EndTime     time.Time
	Parameters  map[string]float64
}

// TimeSeriesAnalyzer provides analysis methods
type TimeSeriesAnalyzer struct {
	// Trend detection, seasonality, anomaly detection
}

// TimeSeriesForecaster provides forecasting capabilities
type TimeSeriesForecaster struct {
	// ARIMA, exponential smoothing, ML models
}

// NewTimeSeriesProcessor creates a new time series processor
func NewTimeSeriesProcessor(config *TimeSeriesConfig) *TimeSeriesProcessor {
	if config == nil {
		config = &TimeSeriesConfig{
			WindowSize:      1 * time.Hour,
			SamplingRate:    1 * time.Minute,
			MaxSeries:       1000,
			DetrendMethod:   "linear",
			SeasonalPeriods: []time.Duration{24 * time.Hour, 7 * 24 * time.Hour},
		}
	}
	
	return &TimeSeriesProcessor{
		config: config,
		series: make(map[string]*TimeSeries),
		analyzer: &TimeSeriesAnalyzer{},
		forecaster: &TimeSeriesForecaster{},
	}
}

// Helper methods for FeatureProcessor

func (fp *FeatureProcessor) getCachedFeatures(eventID string) *FeatureVector {
	fp.cacheMutex.RLock()
	defer fp.cacheMutex.RUnlock()
	
	if vector, exists := fp.featureCache[eventID]; exists {
		if time.Since(vector.Timestamp) < fp.config.CacheTTL {
			return vector
		}
		delete(fp.featureCache, eventID)
	}
	return nil
}

// ProcessDenseFeatures processes dense features from AI features
func (fp *FeatureProcessor) ProcessDenseFeatures(aiFeatures interface{}) ([]float64, error) {
	// Simple implementation for now
	return []float64{1.0, 0.5, 0.8}, nil
}

// StoreBehaviorVector stores a behavior vector
func (fp *FeatureProcessor) StoreBehaviorVector(id string, vector []float64) error {
	// Simple implementation for now
	return nil
}

// GetStats returns feature processor statistics
func (fp *FeatureProcessor) GetStats() interface{} {
	return map[string]interface{}{
		"cached_features": len(fp.featureCache),
		"cache_hits": 0,
	}
}

func (fp *FeatureProcessor) cacheFeatures(vector *FeatureVector) {
	fp.cacheMutex.Lock()
	defer fp.cacheMutex.Unlock()
	
	// Simple LRU eviction if cache is full
	if len(fp.featureCache) >= fp.config.CacheSize {
		// Remove oldest entry (simplified)
		oldestKey := ""
		oldestTime := time.Now()
		for key, vec := range fp.featureCache {
			if vec.Timestamp.Before(oldestTime) {
				oldestTime = vec.Timestamp
				oldestKey = key
			}
		}
		if oldestKey != "" {
			delete(fp.featureCache, oldestKey)
		}
	}
	
	fp.featureCache[vector.EventID] = vector
}

func (fp *FeatureProcessor) severityToNumeric(severity string) float64 {
	switch severity {
	case "critical":
		return 4.0
	case "high":
		return 3.0
	case "medium":
		return 2.0
	case "low":
		return 1.0
	default:
		return 0.0
	}
}

func (fp *FeatureProcessor) boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

func (fp *FeatureProcessor) isErrorEvent(event *types.Event) bool {
	return event.Type == "error" || event.Severity == "critical" || event.Severity == "high"
}

func (fp *FeatureProcessor) isRestartEvent(event *types.Event) bool {
	return event.Type == "restart" || event.Type == "pod_restart"
}

func (fp *FeatureProcessor) isNetworkEvent(event *types.Event) bool {
	return event.Type == "network" || event.Source == "network"
}

func (fp *FeatureProcessor) isMemoryEvent(event *types.Event) bool {
	return event.Type == "memory" || event.Type == "oom"
}

func (fp *FeatureProcessor) calculateEventFrequency(event *types.Event) float64 {
	// Simplified frequency calculation
	// In production, this would use historical data
	return 1.0
}

func (fp *FeatureProcessor) calculateCompleteness(features map[string]float64) float64 {
	if len(features) == 0 {
		return 0.0
	}
	
	nonZeroCount := 0
	for _, value := range features {
		if value != 0.0 {
			nonZeroCount++
		}
	}
	
	return float64(nonZeroCount) / float64(len(features))
}

func (fp *FeatureProcessor) normalizeFeatures(vector *FeatureVector) {
	for name, value := range vector.Features {
		if stats := fp.getFeatureStats(name); stats != nil {
			switch fp.config.NormalizationMode {
			case "zscore":
				if stats.StdDev > 0 {
					vector.Features[name] = (value - stats.Mean) / stats.StdDev
				}
			case "minmax":
				if stats.Max > stats.Min {
					vector.Features[name] = (value - stats.Min) / (stats.Max - stats.Min)
				}
			}
		}
	}
}

func (fp *FeatureProcessor) updateFeatureStats(vector *FeatureVector) {
	fp.statsMutex.Lock()
	defer fp.statsMutex.Unlock()
	
	for name, value := range vector.Features {
		stats, exists := fp.featureStats[name]
		if !exists {
			stats = &FeatureStats{
				Name:       name,
				Min:        value,
				Max:        value,
				LastUpdate: time.Now(),
			}
			fp.featureStats[name] = stats
		}
		
		// Update statistics
		stats.Count++
		stats.Sum += value
		stats.SumSq += value * value
		
		if value < stats.Min {
			stats.Min = value
		}
		if value > stats.Max {
			stats.Max = value
		}
		
		stats.Mean = stats.Sum / float64(stats.Count)
		if stats.Count > 1 {
			variance := (stats.SumSq - stats.Sum*stats.Mean) / float64(stats.Count-1)
			stats.StdDev = math.Sqrt(variance)
		}
		
		stats.LastUpdate = time.Now()
	}
}

func (fp *FeatureProcessor) getFeatureStats(name string) *FeatureStats {
	fp.statsMutex.RLock()
	defer fp.statsMutex.RUnlock()
	return fp.featureStats[name]
}