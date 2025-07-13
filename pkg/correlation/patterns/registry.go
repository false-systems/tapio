package patterns

import (
	"context"
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// DefaultPatternRegistry creates and registers all built-in pattern detectors
func DefaultPatternRegistry() *PatternRegistry {
	registry := NewPatternRegistry()
	
	// Register all built-in pattern detectors
	patterns := []PatternDetector{
		NewMemoryLeakDetector(),
		NewNetworkFailureCascadeDetector(),
		NewStorageIOBottleneckDetector(),
		NewContainerRuntimeFailureDetector(),
		NewServiceDependencyFailureDetector(),
	}
	
	for _, pattern := range patterns {
		if err := registry.Register(pattern); err != nil {
			// Log error but continue with other patterns
			continue
		}
	}
	
	return registry
}

// DetectAllPatterns is a convenience function to detect all patterns at once
func DetectAllPatterns(ctx context.Context, events []correlation.Event, metrics map[string]correlation.MetricSeries) ([]*PatternResult, error) {
	registry := DefaultPatternRegistry()
	return registry.DetectAll(ctx, events, metrics)
}

// GetPatternByID returns a specific pattern detector by ID
func GetPatternByID(id string) PatternDetector {
	registry := DefaultPatternRegistry()
	if detector, exists := registry.Get(id); exists {
		return detector
	}
	return nil
}

// ListAllPatterns returns all available pattern detectors
func ListAllPatterns() []PatternDetector {
	registry := DefaultPatternRegistry()
	return registry.List()
}

// PatternDetectorInfo provides information about a pattern detector
type PatternDetectorInfo struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Category    correlation.Category  `json:"category"`
	Accuracy    float64               `json:"accuracy"`
	FalsePositiveRate float64         `json:"false_positive_rate"`
	Config      PatternConfig         `json:"config"`
}

// GetPatternInfo returns information about all registered patterns
func GetPatternInfo() []PatternDetectorInfo {
	registry := DefaultPatternRegistry()
	detectors := registry.List()
	
	info := make([]PatternDetectorInfo, len(detectors))
	for i, detector := range detectors {
		info[i] = PatternDetectorInfo{
			ID:               detector.ID(),
			Name:             detector.Name(),
			Description:      detector.Description(),
			Category:         detector.Category(),
			Accuracy:         detector.GetAccuracy(),
			FalsePositiveRate: detector.GetFalsePositiveRate(),
			Config:           detector.GetConfig(),
		}
	}
	
	return info
}

// ConfigurablePatternRegistry allows runtime configuration of pattern detectors
type ConfigurablePatternRegistry struct {
	*PatternRegistry
	configs map[string]PatternConfig
	mutex   sync.RWMutex
}

// NewConfigurablePatternRegistry creates a new configurable pattern registry
func NewConfigurablePatternRegistry() *ConfigurablePatternRegistry {
	return &ConfigurablePatternRegistry{
		PatternRegistry: DefaultPatternRegistry(),
		configs:         make(map[string]PatternConfig),
	}
}

// UpdatePatternConfig updates the configuration for a specific pattern
func (cpr *ConfigurablePatternRegistry) UpdatePatternConfig(patternID string, config PatternConfig) error {
	cpr.mutex.Lock()
	defer cpr.mutex.Unlock()
	
	detector, exists := cpr.Get(patternID)
	if !exists {
		return fmt.Errorf("pattern detector %s not found", patternID)
	}
	
	if err := detector.Configure(config); err != nil {
		return fmt.Errorf("failed to configure pattern %s: %w", patternID, err)
	}
	
	cpr.configs[patternID] = config
	return nil
}

// GetPatternConfig returns the current configuration for a pattern
func (cpr *ConfigurablePatternRegistry) GetPatternConfig(patternID string) (PatternConfig, error) {
	cpr.mutex.RLock()
	defer cpr.mutex.RUnlock()
	
	if config, exists := cpr.configs[patternID]; exists {
		return config, nil
	}
	
	detector, exists := cpr.Get(patternID)
	if !exists {
		return PatternConfig{}, fmt.Errorf("pattern detector %s not found", patternID)
	}
	
	return detector.GetConfig(), nil
}

// ValidatePatternConfigs validates all pattern configurations
func (cpr *ConfigurablePatternRegistry) ValidatePatternConfigs() map[string]error {
	cpr.mutex.RLock()
	defer cpr.mutex.RUnlock()
	
	errors := make(map[string]error)
	
	for patternID, config := range cpr.configs {
		if err := validatePatternConfig(config); err != nil {
			errors[patternID] = err
		}
	}
	
	return errors
}

// validatePatternConfig validates a pattern configuration
func validatePatternConfig(config PatternConfig) error {
	if config.MinConfidence < 0 || config.MinConfidence > 1 {
		return fmt.Errorf("min_confidence must be between 0 and 1")
	}
	
	if config.MaxFalsePositive < 0 || config.MaxFalsePositive > 1 {
		return fmt.Errorf("max_false_positive must be between 0 and 1")
	}
	
	if config.LookbackWindow <= 0 {
		return fmt.Errorf("lookback_window must be positive")
	}
	
	if config.PredictionWindow <= 0 {
		return fmt.Errorf("prediction_window must be positive")
	}
	
	if config.MinPatternDuration <= 0 {
		return fmt.Errorf("min_pattern_duration must be positive")
	}
	
	if config.MinDataPoints <= 0 {
		return fmt.Errorf("min_data_points must be positive")
	}
	
	return nil
}