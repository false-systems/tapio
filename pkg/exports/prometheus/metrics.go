package prometheus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsExporter exports Tapio correlation findings as Prometheus metrics
type MetricsExporter struct {
	registry *prometheus.Registry
	config   *MetricsConfig
	
	// Core correlation metrics
	correlationsTotal          *prometheus.CounterVec
	correlationsByType         *prometheus.CounterVec
	correlationsBySeverity     *prometheus.CounterVec
	correlationConfidence      *prometheus.HistogramVec
	correlationProcessingTime  *prometheus.HistogramVec
	
	// Pattern detection metrics
	patternsDetected          *prometheus.CounterVec
	patternAccuracy           *prometheus.HistogramVec
	patternConfidence         *prometheus.HistogramVec
	falsePositives            *prometheus.CounterVec
	truePositives             *prometheus.CounterVec
	
	// System health metrics
	systemHealthScore         *prometheus.GaugeVec
	resourceUsage             *prometheus.GaugeVec
	eventProcessingRate       *prometheus.GaugeVec
	
	// Alert/recommendation metrics
	recommendationsGenerated  *prometheus.CounterVec
	actionsTriggered          *prometheus.CounterVec
	autoFixesApplied          *prometheus.CounterVec
	
	// Performance metrics
	exportLatency             prometheus.Histogram
	exportErrors              prometheus.Counter
	
	// Entity tracking
	entitiesTracked           *prometheus.GaugeVec
	entitiesWithIssues        *prometheus.GaugeVec
	
	mutex sync.RWMutex
}

// MetricsConfig configures the Prometheus metrics exporter
type MetricsConfig struct {
	// Namespace and subsystem
	Namespace string
	Subsystem string
	
	// Labels
	ConstLabels map[string]string
	
	// Performance settings
	BucketConfiguration map[string][]float64
	MaxMetricAge        time.Duration
	
	// Feature flags
	EnablePatternMetrics     bool
	EnableSystemMetrics      bool
	EnableEntityMetrics      bool
	EnablePerformanceMetrics bool
	
	// Alert thresholds
	HighSeverityThreshold    float64
	CriticalSeverityThreshold float64
	
	// Rate limiting
	MaxMetricsPerSecond int
	BurstSize           int
}

// NewMetricsExporter creates a new Prometheus metrics exporter
func NewMetricsExporter(config *MetricsConfig) *MetricsExporter {
	if config == nil {
		config = DefaultMetricsConfig()
	}
	
	registry := prometheus.NewRegistry()
	
	me := &MetricsExporter{
		registry: registry,
		config:   config,
	}
	
	me.initializeMetrics()
	return me
}

// DefaultMetricsConfig returns sensible defaults for metrics export
func DefaultMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		Namespace: "tapio",
		Subsystem: "correlation",
		ConstLabels: map[string]string{
			"version": "1.0.0",
		},
		BucketConfiguration: map[string][]float64{
			"processing_time": {0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
			"confidence":      {0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99},
		},
		MaxMetricAge:              24 * time.Hour,
		EnablePatternMetrics:      true,
		EnableSystemMetrics:       true,
		EnableEntityMetrics:       true,
		EnablePerformanceMetrics:  true,
		HighSeverityThreshold:     0.8,
		CriticalSeverityThreshold: 0.9,
		MaxMetricsPerSecond:       1000,
		BurstSize:                 100,
	}
}

// initializeMetrics creates all Prometheus metric instruments
func (me *MetricsExporter) initializeMetrics() {
	factory := promauto.With(me.registry)
	
	// Core correlation metrics
	me.correlationsTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "correlations_total",
			Help:        "Total number of correlations detected",
			ConstLabels: me.config.ConstLabels,
		},
		[]string{"rule_id", "rule_name", "severity", "category"},
	)
	
	me.correlationsByType = factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "correlations_by_type_total",
			Help:        "Total correlations grouped by type",
			ConstLabels: me.config.ConstLabels,
		},
		[]string{"category", "severity"},
	)
	
	me.correlationsBySeverity = factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "correlations_by_severity_total",
			Help:        "Total correlations grouped by severity",
			ConstLabels: me.config.ConstLabels,
		},
		[]string{"severity"},
	)
	
	me.correlationConfidence = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "correlation_confidence",
			Help:        "Confidence scores of correlations",
			ConstLabels: me.config.ConstLabels,
			Buckets:     me.config.BucketConfiguration["confidence"],
		},
		[]string{"rule_id", "severity"},
	)
	
	me.correlationProcessingTime = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "correlation_processing_time_seconds",
			Help:        "Time taken to process correlations",
			ConstLabels: me.config.ConstLabels,
			Buckets:     me.config.BucketConfiguration["processing_time"],
		},
		[]string{"rule_id"},
	)
	
	// Pattern detection metrics (if enabled)
	if me.config.EnablePatternMetrics {
		me.patternsDetected = factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "patterns",
				Name:        "detected_total",
				Help:        "Total patterns detected",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"pattern_type", "severity"},
		)
		
		me.patternAccuracy = factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "patterns",
				Name:        "accuracy",
				Help:        "Pattern detection accuracy",
				ConstLabels: me.config.ConstLabels,
				Buckets:     me.config.BucketConfiguration["confidence"],
			},
			[]string{"pattern_type"},
		)
		
		me.patternConfidence = factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "patterns",
				Name:        "confidence",
				Help:        "Pattern detection confidence",
				ConstLabels: me.config.ConstLabels,
				Buckets:     me.config.BucketConfiguration["confidence"],
			},
			[]string{"pattern_type"},
		)
		
		me.falsePositives = factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "patterns",
				Name:        "false_positives_total",
				Help:        "Total false positive detections",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"pattern_type"},
		)
		
		me.truePositives = factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "patterns",
				Name:        "true_positives_total",
				Help:        "Total true positive detections",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"pattern_type"},
		)
	}
	
	// System health metrics (if enabled)
	if me.config.EnableSystemMetrics {
		me.systemHealthScore = factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "system",
				Name:        "health_score",
				Help:        "Overall system health score (0-1)",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"namespace", "cluster"},
		)
		
		me.resourceUsage = factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "system",
				Name:        "resource_usage_ratio",
				Help:        "Resource usage ratio (0-1)",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"resource_type", "namespace", "node"},
		)
		
		me.eventProcessingRate = factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "system",
				Name:        "event_processing_rate",
				Help:        "Events processed per second",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"source"},
		)
	}
	
	// Alert/recommendation metrics
	me.recommendationsGenerated = factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "recommendations_generated_total",
			Help:        "Total recommendations generated",
			ConstLabels: me.config.ConstLabels,
		},
		[]string{"severity", "category"},
	)
	
	me.actionsTriggered = factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "actions_triggered_total",
			Help:        "Total actions triggered",
			ConstLabels: me.config.ConstLabels,
		},
		[]string{"action_type", "priority"},
	)
	
	me.autoFixesApplied = factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   me.config.Namespace,
			Subsystem:   me.config.Subsystem,
			Name:        "auto_fixes_applied_total",
			Help:        "Total automatic fixes applied",
			ConstLabels: me.config.ConstLabels,
		},
		[]string{"fix_type", "success"},
	)
	
	// Entity tracking metrics (if enabled)
	if me.config.EnableEntityMetrics {
		me.entitiesTracked = factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "entities",
				Name:        "tracked_total",
				Help:        "Total entities being tracked",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"entity_type", "namespace"},
		)
		
		me.entitiesWithIssues = factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "entities",
				Name:        "with_issues_total",
				Help:        "Total entities with identified issues",
				ConstLabels: me.config.ConstLabels,
			},
			[]string{"entity_type", "severity", "namespace"},
		)
	}
	
	// Performance metrics (if enabled)
	if me.config.EnablePerformanceMetrics {
		me.exportLatency = factory.NewHistogram(
			prometheus.HistogramOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "export",
				Name:        "latency_seconds",
				Help:        "Export operation latency",
				ConstLabels: me.config.ConstLabels,
				Buckets:     me.config.BucketConfiguration["processing_time"],
			},
		)
		
		me.exportErrors = factory.NewCounter(
			prometheus.CounterOpts{
				Namespace:   me.config.Namespace,
				Subsystem:   "export",
				Name:        "errors_total",
				Help:        "Total export errors",
				ConstLabels: me.config.ConstLabels,
			},
		)
	}
}

// ExportCorrelationResult exports a correlation result as Prometheus metrics
func (me *MetricsExporter) ExportCorrelationResult(ctx context.Context, result *correlation.Result) error {
	start := time.Now()
	defer func() {
		if me.config.EnablePerformanceMetrics {
			me.exportLatency.Observe(time.Since(start).Seconds())
		}
	}()
	
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	// Core correlation metrics
	ruleLabels := prometheus.Labels{
		"rule_id":   me.sanitizeLabel(result.RuleID),
		"rule_name": me.sanitizeLabel(result.RuleName),
		"severity":  string(result.Severity),
		"category":  string(result.Category),
	}
	me.correlationsTotal.With(ruleLabels).Inc()
	
	// Type and severity breakdown
	typeLabels := prometheus.Labels{
		"category": string(result.Category),
		"severity": string(result.Severity),
	}
	me.correlationsByType.With(typeLabels).Inc()
	
	severityLabels := prometheus.Labels{
		"severity": string(result.Severity),
	}
	me.correlationsBySeverity.With(severityLabels).Inc()
	
	// Confidence metrics
	confidenceLabels := prometheus.Labels{
		"rule_id":  me.sanitizeLabel(result.RuleID),
		"severity": string(result.Severity),
	}
	me.correlationConfidence.With(confidenceLabels).Observe(result.Confidence)
	
	// Export recommendations
	if len(result.Recommendations) > 0 {
		recLabels := prometheus.Labels{
			"severity": string(result.Severity),
			"category": string(result.Category),
		}
		me.recommendationsGenerated.With(recLabels).Add(float64(len(result.Recommendations)))
	}
	
	// Export actions
	for _, action := range result.Actions {
		actionLabels := prometheus.Labels{
			"action_type": me.sanitizeLabel(action.Type),
			"priority":    me.sanitizeLabel(action.Priority),
		}
		me.actionsTriggered.With(actionLabels).Inc()
	}
	
	// Export entity metrics if enabled
	if me.config.EnableEntityMetrics {
		me.updateEntityMetrics(result)
	}
	
	return nil
}

// ExportSystemHealth exports system health metrics
func (me *MetricsExporter) ExportSystemHealth(ctx context.Context, namespace, cluster string, healthScore float64) error {
	if !me.config.EnableSystemMetrics {
		return nil
	}
	
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	labels := prometheus.Labels{
		"namespace": me.sanitizeLabel(namespace),
		"cluster":   me.sanitizeLabel(cluster),
	}
	me.systemHealthScore.With(labels).Set(healthScore)
	
	return nil
}

// ExportResourceUsage exports resource usage metrics
func (me *MetricsExporter) ExportResourceUsage(ctx context.Context, resourceType, namespace, node string, usage float64) error {
	if !me.config.EnableSystemMetrics {
		return nil
	}
	
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	labels := prometheus.Labels{
		"resource_type": me.sanitizeLabel(resourceType),
		"namespace":     me.sanitizeLabel(namespace),
		"node":          me.sanitizeLabel(node),
	}
	me.resourceUsage.With(labels).Set(usage)
	
	return nil
}

// ExportProcessingRate exports event processing rate metrics
func (me *MetricsExporter) ExportProcessingRate(ctx context.Context, source string, rate float64) error {
	if !me.config.EnableSystemMetrics {
		return nil
	}
	
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	labels := prometheus.Labels{
		"source": me.sanitizeLabel(source),
	}
	me.eventProcessingRate.With(labels).Set(rate)
	
	return nil
}

// ExportPatternMetrics exports pattern detection metrics
func (me *MetricsExporter) ExportPatternMetrics(ctx context.Context, patternType string, detected bool, confidence, accuracy float64) error {
	if !me.config.EnablePatternMetrics {
		return nil
	}
	
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	typeLabels := prometheus.Labels{
		"pattern_type": me.sanitizeLabel(patternType),
	}
	
	if detected {
		severityLabels := prometheus.Labels{
			"pattern_type": me.sanitizeLabel(patternType),
			"severity":     me.determineSeverityFromConfidence(confidence),
		}
		me.patternsDetected.With(severityLabels).Inc()
		me.truePositives.With(typeLabels).Inc()
	} else {
		me.falsePositives.With(typeLabels).Inc()
	}
	
	me.patternConfidence.With(typeLabels).Observe(confidence)
	me.patternAccuracy.With(typeLabels).Observe(accuracy)
	
	return nil
}

// ExportAutoFixResult exports automatic fix application results
func (me *MetricsExporter) ExportAutoFixResult(ctx context.Context, fixType string, success bool) error {
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	labels := prometheus.Labels{
		"fix_type": me.sanitizeLabel(fixType),
		"success":  fmt.Sprintf("%t", success),
	}
	me.autoFixesApplied.With(labels).Inc()
	
	return nil
}

// RecordProcessingTime records correlation processing time
func (me *MetricsExporter) RecordProcessingTime(ruleID string, duration time.Duration) {
	me.mutex.Lock()
	defer me.mutex.Unlock()
	
	labels := prometheus.Labels{
		"rule_id": me.sanitizeLabel(ruleID),
	}
	me.correlationProcessingTime.With(labels).Observe(duration.Seconds())
}

// RecordError records an export error
func (me *MetricsExporter) RecordError() {
	if me.config.EnablePerformanceMetrics {
		me.exportErrors.Inc()
	}
}

// updateEntityMetrics updates entity tracking metrics
func (me *MetricsExporter) updateEntityMetrics(result *correlation.Result) {
	for _, entity := range result.Evidence.Entities {
		// Track entity
		entityLabels := prometheus.Labels{
			"entity_type": me.sanitizeLabel(entity.Type),
			"namespace":   me.sanitizeLabel(entity.Namespace),
		}
		me.entitiesTracked.With(entityLabels).Set(1) // This would need proper counting logic
		
		// Track entity with issues
		issueLabels := prometheus.Labels{
			"entity_type": me.sanitizeLabel(entity.Type),
			"severity":    string(result.Severity),
			"namespace":   me.sanitizeLabel(entity.Namespace),
		}
		me.entitiesWithIssues.With(issueLabels).Inc()
	}
}

// determineSeverityFromConfidence maps confidence scores to severity levels
func (me *MetricsExporter) determineSeverityFromConfidence(confidence float64) string {
	if confidence >= me.config.CriticalSeverityThreshold {
		return "critical"
	} else if confidence >= me.config.HighSeverityThreshold {
		return "high"
	} else if confidence >= 0.5 {
		return "medium"
	}
	return "low"
}

// sanitizeLabel sanitizes label values for Prometheus compatibility
func (me *MetricsExporter) sanitizeLabel(value string) string {
	// Replace invalid characters with underscores
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, value)
	
	// Ensure it doesn't start with a number
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}
	
	return result
}

// GetRegistry returns the Prometheus registry for external use
func (me *MetricsExporter) GetRegistry() *prometheus.Registry {
	return me.registry
}

// Describe implements prometheus.Collector interface
func (me *MetricsExporter) Describe(ch chan<- *prometheus.Desc) {
	me.registry.Describe(ch)
}

// Collect implements prometheus.Collector interface
func (me *MetricsExporter) Collect(ch chan<- prometheus.Metric) {
	me.registry.Collect(ch)
}