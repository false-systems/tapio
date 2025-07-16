package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/types"
)

// ModelRegistry manages ML models for different correlation tasks
type ModelRegistry struct {
	models    map[string]Model
	mutex     sync.RWMutex
	config    *ModelRegistryConfig
	metrics   *ModelMetrics
}

// ModelRegistryConfig configures the model registry
type ModelRegistryConfig struct {
	MaxModels        int
	ModelTTL         time.Duration
	AutoReload       bool
	ReloadInterval   time.Duration
	MetricsEnabled   bool
}

// Model represents a machine learning model interface
type Model interface {
	// Core model operations
	Predict(ctx context.Context, input ModelInput) (ModelOutput, error)
	Train(ctx context.Context, data TrainingData) error
	Evaluate(ctx context.Context, testData TrainingData) (*ModelEvaluation, error)
	
	// Model metadata
	GetInfo() ModelInfo
	GetVersion() string
	GetMetrics() ModelMetrics
	
	// Lifecycle
	Load(ctx context.Context) error
	Unload(ctx context.Context) error
	IsLoaded() bool
}

// ModelInput represents input to a model
type ModelInput struct {
	Features   map[string]interface{}
	Vectors    [][]float64
	Text       []string
	Metadata   map[string]interface{}
	Timestamp  time.Time
}

// ModelOutput represents output from a model
type ModelOutput struct {
	Predictions []Prediction
	Confidence  float64
	Scores      map[string]float64
	Embeddings  [][]float64
	Metadata    map[string]interface{}
	ProcessTime time.Duration
}

// Prediction represents a model prediction
type Prediction struct {
	Class       string
	Probability float64
	Confidence  float64
	Explanation string
	Features    map[string]float64
}

// TrainingData represents training data for models
type TrainingData struct {
	Features []map[string]interface{}
	Labels   []string
	Weights  []float64
	Metadata map[string]interface{}
}

// ModelEvaluation contains model evaluation metrics
type ModelEvaluation struct {
	Accuracy    float64
	Precision   float64
	Recall      float64
	F1Score     float64
	AUC         float64
	ConfusionMatrix [][]int
	ClassMetrics    map[string]ClassMetrics
}

// ClassMetrics contains per-class evaluation metrics
type ClassMetrics struct {
	Precision float64
	Recall    float64
	F1Score   float64
	Support   int
}

// ModelInfo contains model metadata
type ModelInfo struct {
	Name         string
	Version      string
	Type         ModelType
	Description  string
	Created      time.Time
	Updated      time.Time
	Author       string
	Framework    string
	InputSchema  map[string]interface{}
	OutputSchema map[string]interface{}
}

// ModelType defines the type of model
type ModelType string

const (
	ModelTypeClassification ModelType = "classification"
	ModelTypeRegression     ModelType = "regression"
	ModelTypeClustering     ModelType = "clustering"
	ModelTypeAnomalyDetection ModelType = "anomaly_detection"
	ModelTypeTimeSeries     ModelType = "time_series"
	ModelTypeEmbedding      ModelType = "embedding"
)

// ModelMetrics tracks model performance and usage
type ModelMetrics struct {
	PredictionCount   int64
	AverageLatency    time.Duration
	ErrorRate         float64
	LastPrediction    time.Time
	TotalErrors       int64
	MemoryUsage       int64
	CPUUsage          float64
	
	// Performance metrics
	Throughput        float64 // predictions per second
	P50Latency        time.Duration
	P95Latency        time.Duration
	P99Latency        time.Duration
}

// NewModelRegistry creates a new model registry
func NewModelRegistry(config *ModelRegistryConfig) *ModelRegistry {
	if config == nil {
		config = &ModelRegistryConfig{
			MaxModels:      10,
			ModelTTL:       24 * time.Hour,
			AutoReload:     true,
			ReloadInterval: 1 * time.Hour,
			MetricsEnabled: true,
		}
	}
	
	return &ModelRegistry{
		models:  make(map[string]Model),
		config:  config,
		metrics: &ModelMetrics{},
	}
}

// RegisterModel registers a new model
func (mr *ModelRegistry) RegisterModel(name string, model Model) error {
	mr.mutex.Lock()
	defer mr.mutex.Unlock()
	
	if len(mr.models) >= mr.config.MaxModels {
		return fmt.Errorf("model registry full: max %d models", mr.config.MaxModels)
	}
	
	mr.models[name] = model
	return nil
}

// GetModel retrieves a model by name
func (mr *ModelRegistry) GetModel(name string) (Model, bool) {
	mr.mutex.RLock()
	defer mr.mutex.RUnlock()
	
	model, exists := mr.models[name]
	return model, exists
}

// GetModelsForEventType returns models suitable for a specific event type
func (mr *ModelRegistry) GetModelsForEventType(eventType string) []string {
	mr.mutex.RLock()
	defer mr.mutex.RUnlock()
	
	var models []string
	for name := range mr.models {
		// Simple heuristic - return all models for now
		models = append(models, name)
	}
	return models
}

// GetStats returns model registry statistics
func (mr *ModelRegistry) GetStats() interface{} {
	mr.mutex.RLock()
	defer mr.mutex.RUnlock()
	
	return map[string]interface{}{
		"total_models": len(mr.models),
		"max_models":   mr.config.MaxModels,
	}
}

// InferenceEngine handles model inference
type InferenceEngine struct {
	registry *ModelRegistry
	config   *InferenceConfig
}

// InferenceConfig configures inference
type InferenceConfig struct {
	Timeout        time.Duration
	MaxConcurrent  int
	CacheEnabled   bool
}

// NewInferenceEngine creates a new inference engine
func NewInferenceEngine(registry *ModelRegistry, config *InferenceConfig) *InferenceEngine {
	if config == nil {
		config = &InferenceConfig{
			Timeout:       5 * time.Second,
			MaxConcurrent: 10,
			CacheEnabled:  true,
		}
	}
	
	return &InferenceEngine{
		registry: registry,
		config:   config,
	}
}

// RunInference runs inference using the specified model
func (ie *InferenceEngine) RunInference(modelName string, features []float64) ([]float64, error) {
	_, exists := ie.registry.GetModel(modelName)
	if !exists {
		return nil, fmt.Errorf("model %s not found", modelName)
	}
	
	// Simple prediction for now
	return []float64{0.8, 0.2}, nil
}

// GetStats returns inference engine statistics  
func (ie *InferenceEngine) GetStats() interface{} {
	return map[string]interface{}{
		"total_inferences": 0,
		"average_latency":  "0ms",
	}
}

// ListModels returns all registered models
func (mr *ModelRegistry) ListModels() []string {
	mr.mutex.RLock()
	defer mr.mutex.RUnlock()
	
	names := make([]string, 0, len(mr.models))
	for name := range mr.models {
		names = append(names, name)
	}
	return names
}

// Duplicate InferenceEngine and InferenceConfig removed - using the simpler versions above
