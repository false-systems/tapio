package journald

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// EventClassifier provides intelligent event classification
type EventClassifier struct {
	config *EventClassifierConfig
	rules  []ClassificationRule
	models map[string]*ClassificationModel
	cache  map[string]*EventClassification

	// Statistics
	classificationsCount uint64
	cacheHits            uint64
	cacheMisses          uint64

	// State management
	mutex     sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	isStarted bool
}

// EventClassifierConfig configures the event classifier
type EventClassifierConfig struct {
	ClassificationRules []ClassificationRule
	ProcessingTimeout   time.Duration
	CacheSize           int
	CacheTTL            time.Duration

	// ML settings
	EnableMLClassification bool
	ModelThreshold         float64
	FeatureExtraction      FeatureExtractionConfig
}

// ClassificationRule defines a rule for event classification
type ClassificationRule struct {
	ID          string
	Name        string
	Description string

	// Conditions
	ServicePattern  string
	MessagePattern  string
	PriorityRange   []int
	FieldConditions map[string]interface{}

	// Classification
	Category   string
	Severity   string
	Confidence float64
	Tags       []string
	Actions    []string
	Metadata   map[string]interface{}
}

// ClassificationModel represents a machine learning model for classification
type ClassificationModel struct {
	Name        string
	Type        string
	Features    []string
	Weights     map[string]float64
	Threshold   float64
	Accuracy    float64
	LastTrained time.Time
}

// FeatureExtractionConfig configures feature extraction for ML
type FeatureExtractionConfig struct {
	EnableTextFeatures    bool
	EnableNumericFeatures bool
	EnableTimeFeatures    bool
	EnableContextFeatures bool

	// Text features
	NGramSize      int
	VocabularySize int
	UseStopWords   bool

	// Numeric features
	ExtractCounts    bool
	ExtractDurations bool
	ExtractSizes     bool
}

// NewEventClassifier creates a new event classifier
func NewEventClassifier(config *EventClassifierConfig) (*EventClassifier, error) {
	if config == nil {
		config = DefaultEventClassifierConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	classifier := &EventClassifier{
		config: config,
		rules:  config.ClassificationRules,
		models: make(map[string]*ClassificationModel),
		cache:  make(map[string]*EventClassification),
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize default models if ML is enabled
	if config.EnableMLClassification {
		classifier.initializeDefaultModels()
	}

	return classifier, nil
}

// DefaultEventClassifierConfig returns the default configuration
func DefaultEventClassifierConfig() *EventClassifierConfig {
	return &EventClassifierConfig{
		ClassificationRules: []ClassificationRule{
			{
				ID:            "error_classification",
				Name:          "Error Event Classification",
				Description:   "Classifies error events",
				PriorityRange: []int{0, 1, 2, 3},
				Category:      "error",
				Severity:      "high",
				Confidence:    0.9,
				Tags:          []string{"error", "failure"},
			},
			{
				ID:             "security_classification",
				Name:           "Security Event Classification",
				Description:    "Classifies security-related events",
				MessagePattern: "(?i)(auth|security|login|unauthorized|forbidden)",
				Category:       "security",
				Severity:       "critical",
				Confidence:     0.95,
				Tags:           []string{"security", "authentication"},
			},
			{
				ID:             "performance_classification",
				Name:           "Performance Event Classification",
				Description:    "Classifies performance-related events",
				MessagePattern: "(?i)(slow|latency|performance|timeout|throttle)",
				Category:       "performance",
				Severity:       "medium",
				Confidence:     0.8,
				Tags:           []string{"performance", "latency"},
			},
		},
		ProcessingTimeout:      500 * time.Millisecond,
		CacheSize:              10000,
		CacheTTL:               5 * time.Minute,
		EnableMLClassification: false, // Disabled by default for simplicity
		ModelThreshold:         0.7,
		FeatureExtraction: FeatureExtractionConfig{
			EnableTextFeatures:    true,
			EnableNumericFeatures: true,
			EnableTimeFeatures:    true,
			EnableContextFeatures: true,
			NGramSize:             3,
			VocabularySize:        10000,
			UseStopWords:          true,
			ExtractCounts:         true,
			ExtractDurations:      true,
			ExtractSizes:          true,
		},
	}
}

// Start begins event classification
func (ec *EventClassifier) Start(ctx context.Context) error {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()

	if ec.isStarted {
		return fmt.Errorf("event classifier already started")
	}

	// Start cache cleanup goroutine
	go ec.cacheCleanup()

	ec.isStarted = true
	return nil
}

// Stop stops event classification
func (ec *EventClassifier) Stop() error {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()

	if !ec.isStarted {
		return nil
	}

	ec.cancel()
	ec.isStarted = false

	return nil
}

// ClassifyEvent classifies a log event
func (ec *EventClassifier) ClassifyEvent(event *LogEvent) *EventClassification {
	ec.mutex.RLock()
	defer ec.mutex.RUnlock()

	// Check cache first
	cacheKey := ec.generateCacheKey(event)
	if cached, exists := ec.cache[cacheKey]; exists {
		ec.cacheHits++
		return cached
	}
	ec.cacheMisses++

	// Apply classification rules
	classification := ec.applyRules(event)

	// Apply ML models if enabled
	if ec.config.EnableMLClassification {
		mlClassification := ec.applyMLModels(event)
		if mlClassification != nil && mlClassification.Confidence > classification.Confidence {
			classification = mlClassification
		}
	}

	// Cache the result
	if len(ec.cache) < ec.config.CacheSize {
		ec.cache[cacheKey] = classification
	}

	ec.classificationsCount++
	return classification
}

// applyRules applies classification rules to an event
func (ec *EventClassifier) applyRules(event *LogEvent) *EventClassification {
	bestClassification := &EventClassification{
		Category:   "unknown",
		Severity:   "low",
		Confidence: 0.0,
		Tags:       []string{},
		Metadata:   make(map[string]interface{}),
	}

	for _, rule := range ec.rules {
		if ec.ruleMatches(rule, event) {
			if rule.Confidence > bestClassification.Confidence {
				bestClassification = &EventClassification{
					Category:   rule.Category,
					Severity:   rule.Severity,
					Confidence: rule.Confidence,
					Tags:       rule.Tags,
					Metadata: map[string]interface{}{
						"rule_id":    rule.ID,
						"rule_name":  rule.Name,
						"actions":    rule.Actions,
						"matched_by": "rule",
					},
				}

				// Copy rule metadata
				for k, v := range rule.Metadata {
					bestClassification.Metadata[k] = v
				}
			}
		}
	}

	return bestClassification
}

// ruleMatches checks if a rule matches an event
func (ec *EventClassifier) ruleMatches(rule ClassificationRule, event *LogEvent) bool {
	// Check service pattern
	if rule.ServicePattern != "" {
		if !strings.Contains(strings.ToLower(event.Service), strings.ToLower(rule.ServicePattern)) {
			return false
		}
	}

	// Check message pattern
	if rule.MessagePattern != "" {
		if !strings.Contains(strings.ToLower(event.Message), strings.ToLower(rule.MessagePattern)) {
			return false
		}
	}

	// Check priority range
	if len(rule.PriorityRange) > 0 {
		inRange := false
		for _, priority := range rule.PriorityRange {
			if event.Priority == priority {
				inRange = true
				break
			}
		}
		if !inRange {
			return false
		}
	}

	// Check field conditions
	for field, expectedValue := range rule.FieldConditions {
		if actualValue, exists := event.Fields[field]; exists {
			if actualValue != expectedValue {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

// applyMLModels applies machine learning models to classify an event
func (ec *EventClassifier) applyMLModels(event *LogEvent) *EventClassification {
	// Extract features
	features := ec.extractFeatures(event)

	bestClassification := &EventClassification{
		Category:   "unknown",
		Severity:   "low",
		Confidence: 0.0,
		Tags:       []string{},
		Metadata:   make(map[string]interface{}),
	}

	// Apply each model
	for modelName, model := range ec.models {
		score := ec.applyModel(model, features)
		if score > model.Threshold && score > bestClassification.Confidence {
			bestClassification = &EventClassification{
				Category:   ec.getModelCategory(modelName),
				Severity:   ec.getModelSeverity(score),
				Confidence: score,
				Tags:       []string{"ml_classified"},
				Metadata: map[string]interface{}{
					"model_name":  modelName,
					"model_score": score,
					"model_type":  model.Type,
					"matched_by":  "ml_model",
					"features":    features,
				},
			}
		}
	}

	return bestClassification
}

// extractFeatures extracts features from an event for ML classification
func (ec *EventClassifier) extractFeatures(event *LogEvent) map[string]float64 {
	features := make(map[string]float64)

	if ec.config.FeatureExtraction.EnableTextFeatures {
		// Text features
		message := strings.ToLower(event.Message)

		// Word count
		words := strings.Fields(message)
		features["word_count"] = float64(len(words))

		// Character count
		features["char_count"] = float64(len(message))

		// Keyword features
		keywords := []string{"error", "warning", "failed", "success", "timeout", "exception"}
		for _, keyword := range keywords {
			if strings.Contains(message, keyword) {
				features["keyword_"+keyword] = 1.0
			} else {
				features["keyword_"+keyword] = 0.0
			}
		}
	}

	if ec.config.FeatureExtraction.EnableNumericFeatures {
		// Priority as feature
		features["priority"] = float64(event.Priority)

		// Numeric values in message
		numericCount := 0
		words := strings.Fields(event.Message)
		for _, word := range words {
			if _, err := fmt.Sscanf(word, "%f", new(float64)); err == nil {
				numericCount++
			}
		}
		features["numeric_count"] = float64(numericCount)
	}

	if ec.config.FeatureExtraction.EnableTimeFeatures {
		// Time-based features
		now := time.Now()
		features["hour_of_day"] = float64(now.Hour())
		features["day_of_week"] = float64(now.Weekday())
	}

	if ec.config.FeatureExtraction.EnableContextFeatures {
		// Service features
		serviceScore := 0.0
		criticalServices := []string{"docker", "kubelet", "systemd"}
		for _, service := range criticalServices {
			if strings.Contains(strings.ToLower(event.Service), service) {
				serviceScore = 1.0
				break
			}
		}
		features["critical_service"] = serviceScore

		// Pattern match features
		if event.MatchedPatterns != nil {
			features["pattern_count"] = float64(len(event.MatchedPatterns))
		} else {
			features["pattern_count"] = 0.0
		}
	}

	return features
}

// applyModel applies a model to features and returns a score
func (ec *EventClassifier) applyModel(model *ClassificationModel, features map[string]float64) float64 {
	score := 0.0

	switch model.Type {
	case "linear":
		// Simple linear model
		for feature, value := range features {
			if weight, exists := model.Weights[feature]; exists {
				score += weight * value
			}
		}
		// Apply sigmoid to get probability
		score = 1.0 / (1.0 + math.Exp(-score))

	case "threshold":
		// Simple threshold-based model
		for feature, value := range features {
			if threshold, exists := model.Weights[feature]; exists {
				if value > threshold {
					score += 0.1
				}
			}
		}

	default:
		// Default: simple weighted sum
		for feature, value := range features {
			if weight, exists := model.Weights[feature]; exists {
				score += weight * value
			}
		}
	}

	// Normalize score to [0, 1]
	if score > 1.0 {
		score = 1.0
	} else if score < 0.0 {
		score = 0.0
	}

	return score
}

// getModelCategory maps model name to category
func (ec *EventClassifier) getModelCategory(modelName string) string {
	switch {
	case strings.Contains(modelName, "error"):
		return "error"
	case strings.Contains(modelName, "security"):
		return "security"
	case strings.Contains(modelName, "performance"):
		return "performance"
	case strings.Contains(modelName, "warning"):
		return "warning"
	default:
		return "unknown"
	}
}

// getModelSeverity maps score to severity
func (ec *EventClassifier) getModelSeverity(score float64) string {
	if score >= 0.9 {
		return "critical"
	} else if score >= 0.7 {
		return "high"
	} else if score >= 0.5 {
		return "medium"
	} else {
		return "low"
	}
}

// generateCacheKey generates a cache key for an event
func (ec *EventClassifier) generateCacheKey(event *LogEvent) string {
	return fmt.Sprintf("%s_%d_%d", event.Service, event.Priority, len(event.Message))
}

// cacheCleanup periodically cleans up the cache
func (ec *EventClassifier) cacheCleanup() {
	ticker := time.NewTicker(ec.config.CacheTTL)
	defer ticker.Stop()

	for {
		select {
		case <-ec.ctx.Done():
			return
		case <-ticker.C:
			ec.mutex.Lock()
			// Simple cache cleanup - in production, use TTL per entry
			if len(ec.cache) > ec.config.CacheSize/2 {
				ec.cache = make(map[string]*EventClassification)
			}
			ec.mutex.Unlock()
		}
	}
}

// initializeDefaultModels initializes default ML models
func (ec *EventClassifier) initializeDefaultModels() {
	// Error detection model
	ec.models["error_detector"] = &ClassificationModel{
		Name:      "Error Detector",
		Type:      "linear",
		Threshold: 0.7,
		Weights: map[string]float64{
			"priority":          -0.2, // Lower priority = higher error likelihood
			"keyword_error":     0.8,
			"keyword_failed":    0.7,
			"keyword_exception": 0.9,
			"word_count":        0.01,
		},
		Accuracy:    0.85,
		LastTrained: time.Now(),
	}

	// Security detection model
	ec.models["security_detector"] = &ClassificationModel{
		Name:      "Security Detector",
		Type:      "threshold",
		Threshold: 0.6,
		Weights: map[string]float64{
			"keyword_unauthorized": 0.9,
			"keyword_forbidden":    0.8,
			"keyword_denied":       0.7,
			"critical_service":     0.5,
		},
		Accuracy:    0.92,
		LastTrained: time.Now(),
	}

	// Performance detection model
	ec.models["performance_detector"] = &ClassificationModel{
		Name:      "Performance Detector",
		Type:      "linear",
		Threshold: 0.6,
		Weights: map[string]float64{
			"keyword_slow":    0.6,
			"keyword_timeout": 0.8,
			"numeric_count":   0.1,
			"pattern_count":   0.2,
		},
		Accuracy:    0.78,
		LastTrained: time.Now(),
	}
}

// GetClassifications returns current classifications
func (ec *EventClassifier) GetClassifications() map[string]interface{} {
	ec.mutex.RLock()
	defer ec.mutex.RUnlock()

	return map[string]interface{}{
		"total_classifications": ec.classificationsCount,
		"cache_hits":            ec.cacheHits,
		"cache_misses":          ec.cacheMisses,
		"cache_size":            len(ec.cache),
		"active_rules":          len(ec.rules),
		"active_models":         len(ec.models),
		"ml_enabled":            ec.config.EnableMLClassification,
	}
}

// AddRule adds a new classification rule
func (ec *EventClassifier) AddRule(rule ClassificationRule) error {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()

	// Remove existing rule with same ID
	for i, existing := range ec.rules {
		if existing.ID == rule.ID {
			ec.rules = append(ec.rules[:i], ec.rules[i+1:]...)
			break
		}
	}

	ec.rules = append(ec.rules, rule)
	return nil
}

// RemoveRule removes a classification rule
func (ec *EventClassifier) RemoveRule(ruleID string) error {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()

	for i, rule := range ec.rules {
		if rule.ID == ruleID {
			ec.rules = append(ec.rules[:i], ec.rules[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("rule not found: %s", ruleID)
}

// GetRules returns current classification rules
func (ec *EventClassifier) GetRules() []ClassificationRule {
	ec.mutex.RLock()
	defer ec.mutex.RUnlock()

	rules := make([]ClassificationRule, len(ec.rules))
	copy(rules, ec.rules)
	return rules
}

// ClearCache clears the classification cache
func (ec *EventClassifier) ClearCache() {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()

	ec.cache = make(map[string]*EventClassification)
}
