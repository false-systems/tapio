package opinions

import (
	"fmt"
	"strings"
	"time"
)

// OpinionEnricher adds intelligent defaults based on context
type OpinionEnricher struct {
	templates map[string]*OpinionTemplate
	defaults  *OpinionConfig
}

// NewOpinionEnricher creates a new enricher with default templates
func NewOpinionEnricher() *OpinionEnricher {
	return &OpinionEnricher{
		templates: loadDefaultTemplates(),
		defaults:  getDefaultOpinions(),
	}
}

// Enrich adds smart defaults and inferences to a config
func (e *OpinionEnricher) Enrich(config *OpinionConfig, metadata map[string]string) *OpinionConfig {
	// Start with a copy
	enriched := e.copyConfig(config)

	// Apply base profile if specified
	if profile, exists := metadata["profile"]; exists {
		e.applyProfile(enriched, profile)
	}

	// Apply workload-specific defaults
	if workloadType, exists := metadata["workload"]; exists {
		e.applyWorkloadDefaults(enriched, workloadType)
	}

	// Apply cluster-type defaults
	if clusterType, exists := metadata["cluster"]; exists {
		e.applyClusterDefaults(enriched, clusterType)
	}

	// Infer missing values
	e.inferMissingValues(enriched)

	// Add relationships based on dependencies
	e.inferServiceRelationships(enriched)

	// Validate and adjust conflicting values
	e.resolveConflicts(enriched)

	return enriched
}

// applyProfile applies a predefined profile
func (e *OpinionEnricher) applyProfile(config *OpinionConfig, profileName string) {
	profile, exists := e.templates[profileName]
	if !exists {
		// Try to find partial match
		for name, template := range e.templates {
			if strings.Contains(strings.ToLower(name), strings.ToLower(profileName)) {
				profile = template
				break
			}
		}
	}

	if profile != nil {
		e.mergeConfig(config, &profile.Config)
		config.BaseProfile = profile.Name
	}
}

// applyWorkloadDefaults applies defaults based on workload type
func (e *OpinionEnricher) applyWorkloadDefaults(config *OpinionConfig, workloadType string) {
	switch strings.ToLower(workloadType) {
	case "stateful", "database", "statefulset":
		// More conservative for stateful workloads
		if config.AnomalyThresholds == nil {
			config.AnomalyThresholds = make(map[string]float32)
		}
		if _, exists := config.AnomalyThresholds["memory_usage"]; !exists {
			config.AnomalyThresholds["memory_usage"] = 0.85
		}
		if _, exists := config.AnomalyThresholds["disk_usage"]; !exists {
			config.AnomalyThresholds["disk_usage"] = 0.80
		}

		// Longer correlation windows for stateful
		if config.CorrelationWindows == nil {
			config.CorrelationWindows = make(map[string]time.Duration)
		}
		if _, exists := config.CorrelationWindows["oom_restart"]; !exists {
			config.CorrelationWindows["oom_restart"] = 60 * time.Second
		}

	case "batch", "job", "cronjob":
		// More relaxed for batch workloads
		if config.AnomalyThresholds == nil {
			config.AnomalyThresholds = make(map[string]float32)
		}
		if _, exists := config.AnomalyThresholds["memory_usage"]; !exists {
			config.AnomalyThresholds["memory_usage"] = 0.95
		}
		if _, exists := config.AnomalyThresholds["cpu_usage"]; !exists {
			config.AnomalyThresholds["cpu_usage"] = 0.95
		}

		// Batch jobs can have spiky behavior
		if config.BehavioralConfig.DeviationSensitivity == 0 {
			config.BehavioralConfig.DeviationSensitivity = 0.5
		}

	case "api", "stateless", "deployment":
		// Default stateless API settings
		if config.AnomalyThresholds == nil {
			config.AnomalyThresholds = make(map[string]float32)
		}
		if _, exists := config.AnomalyThresholds["memory_usage"]; !exists {
			config.AnomalyThresholds["memory_usage"] = 0.90
		}
		if _, exists := config.AnomalyThresholds["response_time"]; !exists {
			config.AnomalyThresholds["response_time"] = 0.95
		}
	}
}

// applyClusterDefaults applies defaults based on cluster type
func (e *OpinionEnricher) applyClusterDefaults(config *OpinionConfig, clusterType string) {
	switch strings.ToLower(clusterType) {
	case "production", "prod":
		// Production is more sensitive
		if config.BehavioralConfig.DeviationSensitivity == 0 {
			config.BehavioralConfig.DeviationSensitivity = 0.9
		}
		
		// Enable all predictions in production
		config.PredictionConfig.EnableOOMPrediction = true
		config.PredictionConfig.EnableCascadePrediction = true
		config.PredictionConfig.EnableAnomalyPrediction = true

		// Shorter prediction horizon for faster response
		if config.PredictionConfig.PredictionHorizon == 0 {
			config.PredictionConfig.PredictionHorizon = 5 * time.Minute
		}

	case "staging", "stage":
		// Staging is moderately sensitive
		if config.BehavioralConfig.DeviationSensitivity == 0 {
			config.BehavioralConfig.DeviationSensitivity = 0.7
		}

	case "development", "dev":
		// Development is less sensitive
		if config.BehavioralConfig.DeviationSensitivity == 0 {
			config.BehavioralConfig.DeviationSensitivity = 0.5
		}
		
		// Relax thresholds in dev
		for key := range config.AnomalyThresholds {
			config.AnomalyThresholds[key] *= 1.1 // 10% more relaxed
			if config.AnomalyThresholds[key] > 0.99 {
				config.AnomalyThresholds[key] = 0.99
			}
		}
	}
}

// inferMissingValues fills in missing values with intelligent defaults
func (e *OpinionEnricher) inferMissingValues(config *OpinionConfig) {
	// Initialize maps if nil
	if config.ImportanceWeights == nil {
		config.ImportanceWeights = make(map[string]float32)
	}
	if config.CorrelationWindows == nil {
		config.CorrelationWindows = make(map[string]time.Duration)
	}
	if config.AnomalyThresholds == nil {
		config.AnomalyThresholds = make(map[string]float32)
	}

	// Default behavioral config
	if config.BehavioralConfig.LearningWindow == 0 {
		config.BehavioralConfig.LearningWindow = 7 * 24 * time.Hour
	}
	if config.BehavioralConfig.MinSamplesRequired == 0 {
		config.BehavioralConfig.MinSamplesRequired = 100
	}
	if config.BehavioralConfig.TrendWindow == 0 {
		config.BehavioralConfig.TrendWindow = 1 * time.Hour
	}

	// Default prediction config
	if config.PredictionConfig.MinConfidenceThreshold == 0 {
		config.PredictionConfig.MinConfidenceThreshold = 0.7
	}
	if config.PredictionConfig.PredictionHorizon == 0 {
		config.PredictionConfig.PredictionHorizon = 10 * time.Minute
	}

	// Infer service weights from names
	for service := range config.ServiceLimits {
		if _, exists := config.ImportanceWeights[service]; !exists {
			weight := e.inferServiceWeight(service)
			config.ImportanceWeights[service] = weight
		}
	}
}

// inferServiceWeight guesses importance based on service name
func (e *OpinionEnricher) inferServiceWeight(serviceName string) float32 {
	lower := strings.ToLower(serviceName)

	// Critical services
	criticalPatterns := []string{"payment", "auth", "api-gateway", "database", "redis", "postgres", "mysql"}
	for _, pattern := range criticalPatterns {
		if strings.Contains(lower, pattern) {
			return 1.0
		}
	}

	// Important services
	importantPatterns := []string{"api", "web", "app", "service", "backend"}
	for _, pattern := range importantPatterns {
		if strings.Contains(lower, pattern) {
			return 0.8
		}
	}

	// Less critical services
	lowPatterns := []string{"analytics", "metrics", "logging", "debug", "test"}
	for _, pattern := range lowPatterns {
		if strings.Contains(lower, pattern) {
			return 0.3
		}
	}

	// Default
	return 0.5
}

// inferServiceRelationships creates relationships from dependencies
func (e *OpinionEnricher) inferServiceRelationships(config *OpinionConfig) {
	// Group dependencies by source
	depsBySource := make(map[string][]ServiceDependency)
	for _, dep := range config.ServiceDependencies {
		depsBySource[dep.Source] = append(depsBySource[dep.Source], dep)
	}

	// Add cascade correlation windows
	for source, deps := range depsBySource {
		if len(deps) > 2 {
			// This service affects multiple others - likely cascade source
			windowKey := fmt.Sprintf("%s_cascade", source)
			if _, exists := config.CorrelationWindows[windowKey]; !exists {
				// Set cascade window to max dependency delay
				maxDelay := time.Duration(0)
				for _, dep := range deps {
					if dep.ExpectedDelay > maxDelay {
						maxDelay = dep.ExpectedDelay
					}
				}
				config.CorrelationWindows[windowKey] = maxDelay + 30*time.Second
			}
		}
	}
}

// resolveConflicts fixes conflicting configurations
func (e *OpinionEnricher) resolveConflicts(config *OpinionConfig) {
	// Ensure memory thresholds are reasonable
	for service, limit := range config.ServiceLimits {
		if limit.MemoryLimit > 0 {
			// Service-specific limit should be higher than general threshold
			generalThreshold, exists := config.AnomalyThresholds["memory_usage"]
			if exists && limit.MemoryLimit < generalThreshold {
				// Adjust service limit to be at least 5% higher
				config.ServiceLimits[service] = ServiceLimit{
					MemoryLimit: generalThreshold + 0.05,
					CPULimit:    limit.CPULimit,
					CustomRules: limit.CustomRules,
				}
			}
		}
	}

	// Ensure prediction windows make sense
	if config.PredictionConfig.PredictionWindows != nil {
		for key, window := range config.PredictionConfig.PredictionWindows {
			if window > config.PredictionConfig.PredictionHorizon {
				// Prediction window can't be longer than horizon
				config.PredictionConfig.PredictionWindows[key] = config.PredictionConfig.PredictionHorizon
			}
		}
	}
}

// mergeConfig merges source config into target
func (e *OpinionEnricher) mergeConfig(target, source *OpinionConfig) {
	// Merge maps
	for k, v := range source.ImportanceWeights {
		if target.ImportanceWeights == nil {
			target.ImportanceWeights = make(map[string]float32)
		}
		target.ImportanceWeights[k] = v
	}

	for k, v := range source.CorrelationWindows {
		if target.CorrelationWindows == nil {
			target.CorrelationWindows = make(map[string]time.Duration)
		}
		target.CorrelationWindows[k] = v
	}

	for k, v := range source.AnomalyThresholds {
		if target.AnomalyThresholds == nil {
			target.AnomalyThresholds = make(map[string]float32)
		}
		target.AnomalyThresholds[k] = v
	}

	// Merge behavioral config
	if source.BehavioralConfig.LearningWindow > 0 && target.BehavioralConfig.LearningWindow == 0 {
		target.BehavioralConfig.LearningWindow = source.BehavioralConfig.LearningWindow
	}
	if source.BehavioralConfig.DeviationSensitivity > 0 && target.BehavioralConfig.DeviationSensitivity == 0 {
		target.BehavioralConfig.DeviationSensitivity = source.BehavioralConfig.DeviationSensitivity
	}

	// Merge arrays
	target.ServiceDependencies = append(target.ServiceDependencies, source.ServiceDependencies...)
	target.TimeBasedRules = append(target.TimeBasedRules, source.TimeBasedRules...)
}

// copyConfig creates a deep copy of the config
func (e *OpinionEnricher) copyConfig(source *OpinionConfig) *OpinionConfig {
	config := &OpinionConfig{
		Metadata:            make(map[string]string),
		ImportanceWeights:   make(map[string]float32),
		CorrelationWindows:  make(map[string]time.Duration),
		AnomalyThresholds:   make(map[string]float32),
		ServiceLimits:       make(map[string]ServiceLimit),
		BehavioralConfig:    source.BehavioralConfig,
		PredictionConfig:    source.PredictionConfig,
		Profile:             source.Profile,
		BaseProfile:         source.BaseProfile,
	}

	// Copy maps
	for k, v := range source.Metadata {
		config.Metadata[k] = v
	}
	for k, v := range source.ImportanceWeights {
		config.ImportanceWeights[k] = v
	}
	for k, v := range source.CorrelationWindows {
		config.CorrelationWindows[k] = v
	}
	for k, v := range source.AnomalyThresholds {
		config.AnomalyThresholds[k] = v
	}
	for k, v := range source.ServiceLimits {
		config.ServiceLimits[k] = v
	}

	// Copy slices
	config.ServiceDependencies = append([]ServiceDependency{}, source.ServiceDependencies...)
	config.TimeBasedRules = append([]TimeBasedRule{}, source.TimeBasedRules...)

	return config
}

// loadDefaultTemplates loads predefined templates
func loadDefaultTemplates() map[string]*OpinionTemplate {
	return map[string]*OpinionTemplate{
		"high-traffic-api": {
			Name:        "high-traffic-api",
			Description: "For high-traffic stateless API services",
			Tags:        []string{"api", "stateless", "high-traffic"},
			Config: OpinionConfig{
				AnomalyThresholds: map[string]float32{
					"memory_usage":   0.85,
					"cpu_usage":      0.80,
					"response_time":  0.95,
					"error_rate":     0.02,
				},
				CorrelationWindows: map[string]time.Duration{
					"oom_restart":      30 * time.Second,
					"cascade_failure":  2 * time.Minute,
					"network_timeout":  10 * time.Second,
				},
				BehavioralConfig: BehavioralOpinions{
					DeviationSensitivity: 0.8,
					LearningWindow:       7 * 24 * time.Hour,
				},
			},
		},
		"stateful-database": {
			Name:        "stateful-database",
			Description: "For stateful database workloads",
			Tags:        []string{"database", "stateful", "persistent"},
			Config: OpinionConfig{
				AnomalyThresholds: map[string]float32{
					"memory_usage":    0.80,
					"cpu_usage":       0.75,
					"disk_usage":      0.85,
					"connection_pool": 0.90,
				},
				CorrelationWindows: map[string]time.Duration{
					"oom_restart":         60 * time.Second,
					"replication_lag":     30 * time.Second,
					"backup_correlation":  5 * time.Minute,
				},
				BehavioralConfig: BehavioralOpinions{
					DeviationSensitivity: 0.9,
					LearningWindow:       14 * 24 * time.Hour,
				},
			},
		},
		"batch-processing": {
			Name:        "batch-processing",
			Description: "For batch processing and cron jobs",
			Tags:        []string{"batch", "job", "cronjob"},
			Config: OpinionConfig{
				AnomalyThresholds: map[string]float32{
					"memory_usage": 0.95,
					"cpu_usage":    0.95,
					"job_duration": 2.0, // 2x normal duration
				},
				BehavioralConfig: BehavioralOpinions{
					DeviationSensitivity: 0.5,
					LearningWindow:       30 * 24 * time.Hour,
				},
			},
		},
	}
}

// getDefaultOpinions returns base default opinions
func getDefaultOpinions() *OpinionConfig {
	return &OpinionConfig{
		AnomalyThresholds: map[string]float32{
			"memory_usage": 0.90,
			"cpu_usage":    0.80,
			"error_rate":   0.05,
		},
		CorrelationWindows: map[string]time.Duration{
			"oom_restart":      30 * time.Second,
			"cascade_failure":  5 * time.Minute,
		},
		BehavioralConfig: BehavioralOpinions{
			LearningWindow:       7 * 24 * time.Hour,
			MinSamplesRequired:   100,
			DeviationSensitivity: 0.8,
			TrendWindow:          1 * time.Hour,
		},
		PredictionConfig: PredictionOpinions{
			EnableOOMPrediction:     true,
			EnableCascadePrediction: true,
			EnableAnomalyPrediction: true,
			PredictionHorizon:       10 * time.Minute,
			MinConfidenceThreshold:  0.7,
		},
	}
}