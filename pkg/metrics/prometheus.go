// Package metrics provides Prometheus metrics exporting for Tapio health checks
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/events_correlation"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
	"github.com/yairfalse/tapio/pkg/universal"
	"github.com/yairfalse/tapio/pkg/universal/converters"
	"github.com/yairfalse/tapio/pkg/universal/formatters"
)

// PrometheusExporter exports Tapio metrics to Prometheus format using V2 engine
type PrometheusExporter struct {
	checker           CheckerInterface
	ebpfMonitor       ebpf.Monitor
	registry          *prometheus.Registry
	v2Engine          *correlation.Engine

	// Universal format components
	formatter            *formatters.PrometheusFormatter
	ebpfConverter        *converters.EBPFConverter
	correlationConverter *converters.CorrelationConverter

	// Health metrics
	podHealthStatus    *prometheus.GaugeVec
	clusterHealthScore *prometheus.GaugeVec
	problemsTotal      *prometheus.CounterVec

	// Prediction metrics
	oomPredictionSeconds *prometheus.GaugeVec
	confidenceScore      *prometheus.GaugeVec

	// Performance metrics
	analysisLatency *prometheus.HistogramVec
	lastUpdateTime  *prometheus.GaugeVec
	
	// V2 Engine metrics
	v2EventsProcessed   *prometheus.CounterVec
	v2EventsDropped     *prometheus.CounterVec
	v2CorrelationsFound *prometheus.CounterVec
	v2ProcessingLatency *prometheus.HistogramVec
	v2EngineHealth      *prometheus.GaugeVec
}

// CheckerInterface defines the interface for checkers
type CheckerInterface interface {
	Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error)
}

// Config holds configuration for the Prometheus exporter
type Config struct {
	// RefreshInterval is how often to update metrics
	RefreshInterval time.Duration

	// IncludeEBPF enables eBPF metrics collection
	IncludeEBPF bool

	// IncludeCorrelation enables correlation engine metrics
	IncludeCorrelation bool

	// Labels to add to all metrics
	GlobalLabels map[string]string
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		RefreshInterval:    30 * time.Second,
		IncludeEBPF:        true,
		IncludeCorrelation: true,
		GlobalLabels: map[string]string{
			"app": "tapio",
		},
	}
}

// New creates a new Prometheus exporter
func New(checker CheckerInterface, config Config) (*PrometheusExporter, error) {
	registry := prometheus.NewRegistry()

	// Create formatter with config
	promConfig := &formatters.PrometheusConfig{
		Prefix:       "tapio",
		GlobalLabels: config.GlobalLabels,
	}
	formatter := formatters.NewPrometheusFormatter(promConfig)

	// Initialize V2 engine
	v2Config := correlation.DefaultConfig()
	v2Engine := correlation.NewEngine(v2Config)
	
	// Start V2 engine
	if err := v2Engine.Start(); err != nil {
		return nil, fmt.Errorf("failed to start V2 engine: %w", err)
	}

	// Register default correlation rules
	registerDefaultRules(v2Engine)

	exporter := &PrometheusExporter{
		checker:              checker,
		registry:             registry,
		v2Engine:             v2Engine,
		formatter:            formatter,
		ebpfConverter:        converters.NewEBPFConverter(),
		correlationConverter: converters.NewCorrelationConverter("prometheus", "1.0"),
	}

	// Initialize metrics
	exporter.initMetrics()

	// Register metrics with registry
	registry.MustRegister(
		exporter.podHealthStatus,
		exporter.clusterHealthScore,
		exporter.problemsTotal,
		exporter.oomPredictionSeconds,
		exporter.confidenceScore,
		exporter.analysisLatency,
		exporter.lastUpdateTime,
		exporter.v2EventsProcessed,
		exporter.v2EventsDropped,
		exporter.v2CorrelationsFound,
		exporter.v2ProcessingLatency,
		exporter.v2EngineHealth,
	)

	// Initialize eBPF monitor if enabled
	if config.IncludeEBPF {
		ebpfConfig := &ebpf.Config{
			Enabled:                true,
			EnableMemoryMonitoring: true,
			EnableNetworkMonitoring: true,
			BufferSize:             1024,
		}
		monitor := ebpf.NewMonitor(ebpfConfig)
		if err := monitor.Start(); err == nil {
			exporter.ebpfMonitor = monitor
		}
	}

	return exporter, nil
}

// initMetrics initializes all Prometheus metrics
func (pe *PrometheusExporter) initMetrics() {
	pe.podHealthStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "pod",
			Name:      "health_status",
			Help:      "Pod health status (1=healthy, 0=unhealthy)",
		},
		[]string{"pod", "namespace", "reason"},
	)

	pe.clusterHealthScore = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "cluster",
			Name:      "health_score",
			Help:      "Overall cluster health score (0-100)",
		},
		[]string{"cluster"},
	)

	pe.problemsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tapio",
			Subsystem: "problems",
			Name:      "total",
			Help:      "Total number of problems detected",
		},
		[]string{"severity", "type"},
	)

	pe.oomPredictionSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "prediction",
			Name:      "oom_seconds",
			Help:      "Predicted time to OOM in seconds",
		},
		[]string{"pod", "namespace"},
	)

	pe.confidenceScore = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "prediction",
			Name:      "confidence",
			Help:      "Prediction confidence score (0-1)",
		},
		[]string{"pod", "namespace", "type"},
	)

	pe.analysisLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tapio",
			Subsystem: "analysis",
			Name:      "latency_seconds",
			Help:      "Analysis latency in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	pe.lastUpdateTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "metrics",
			Name:      "last_update_timestamp",
			Help:      "Timestamp of last metrics update",
		},
		[]string{},
	)
	
	// V2 Engine specific metrics
	pe.v2EventsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tapio",
			Subsystem: "v2_engine",
			Name:      "events_processed_total",
			Help:      "Total number of events processed by V2 engine",
		},
		[]string{"source"},
	)
	
	pe.v2EventsDropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tapio",
			Subsystem: "v2_engine",
			Name:      "events_dropped_total",
			Help:      "Total number of events dropped by V2 engine",
		},
		[]string{"reason"},
	)
	
	pe.v2CorrelationsFound = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tapio",
			Subsystem: "v2_engine",
			Name:      "correlations_found_total",
			Help:      "Total number of correlations found by V2 engine",
		},
		[]string{"rule_id", "severity"},
	)
	
	pe.v2ProcessingLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tapio",
			Subsystem: "v2_engine",
			Name:      "processing_latency_seconds",
			Help:      "V2 engine processing latency in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"shard"},
	)
	
	pe.v2EngineHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tapio",
			Subsystem: "v2_engine",
			Name:      "health",
			Help:      "V2 engine health status (1=healthy, 0=unhealthy)",
		},
		[]string{"component"},
	)
}

// Start begins the metrics collection loop
func (pe *PrometheusExporter) Start(ctx context.Context, refreshInterval time.Duration) {
	go func() {
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		// Initial update
		pe.updateMetrics(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pe.updateMetrics(ctx)
			}
		}
	}()
}

// updateMetrics updates all metrics
func (pe *PrometheusExporter) updateMetrics(ctx context.Context) {
	startTime := time.Now()

	// Run health check
	checkReq := &types.CheckRequest{
		AllNamespaces: true,
		Verbose:       true,
	}

	result, err := pe.checker.Check(ctx, checkReq)
	if err != nil {
		return
	}

	// Process problems to create events for V2 engine
	events := pe.convertProblemsToEvents(result.Problems)
	
	// Process events through V2 engine
	if len(events) > 0 {
		processed := pe.v2Engine.ProcessBatch(events)
		pe.v2EventsProcessed.WithLabelValues("kubernetes").Add(float64(processed))
	}

	// Update basic metrics
	pe.updateHealthMetrics(result)
	pe.updatePredictionMetrics(result)
	
	// Update V2 engine metrics
	pe.updateV2EngineMetrics()

	// Update analysis latency
	pe.analysisLatency.WithLabelValues("full_check").Observe(time.Since(startTime).Seconds())

	// Update last update time
	pe.lastUpdateTime.WithLabelValues().SetToCurrentTime()
}

// convertProblemsToEvents converts problems to V2 correlation events
func (pe *PrometheusExporter) convertProblemsToEvents(problems []types.Problem) []*events_correlation.Event {
	events := make([]*events_correlation.Event, 0, len(problems))
	
	for _, problem := range problems {
		event := &events_correlation.Event{
			ID:        fmt.Sprintf("problem-%s-%d", problem.Resource.Name, time.Now().UnixNano()),
			Timestamp: time.Now(),
			Source:    events_correlation.SourceKubernetes,
			Type:      strings.ToLower(string(problem.Severity)),
			Entity: events_correlation.Entity{
				Type: problem.Resource.Kind,
				UID:  fmt.Sprintf("%s/%s", problem.Resource.Namespace, problem.Resource.Name),
				Name: problem.Resource.Name,
			},
			Attributes: map[string]interface{}{
				"title":       problem.Title,
				"description": problem.Description,
				"severity":    problem.Severity,
			},
			Fingerprint: fmt.Sprintf("k8s-problem-%s-%s", problem.Resource.Name, problem.Title),
			Labels: map[string]string{
				"namespace": problem.Resource.Namespace,
			},
		}
		
		events = append(events, event)
	}
	
	return events
}

// updateHealthMetrics updates health-related metrics
func (pe *PrometheusExporter) updateHealthMetrics(result *types.CheckResult) {
	// Reset pod health status
	pe.podHealthStatus.Reset()

	// Track overall cluster health
	healthyPods := 0
	totalPods := 0

	// Process each problem
	for _, problem := range result.Problems {
		totalPods++
		
		// Update pod health status
		if problem.Resource.Kind == "pod" {
			healthValue := 0.0
			if problem.Severity == types.SeverityInfo {
				healthValue = 1.0
				healthyPods++
			}
			
			pe.podHealthStatus.WithLabelValues(
				problem.Resource.Name,
				problem.Resource.Namespace,
				problem.Title,
			).Set(healthValue)
		}

		// Count problems by severity
		pe.problemsTotal.WithLabelValues(
			string(problem.Severity),
			problem.Resource.Kind,
		).Inc()
	}

	// Calculate cluster health score
	clusterScore := 100.0
	if totalPods > 0 {
		clusterScore = float64(healthyPods) / float64(totalPods) * 100
	}
	pe.clusterHealthScore.WithLabelValues("default").Set(clusterScore)
}

// updatePredictionMetrics updates prediction-related metrics
func (pe *PrometheusExporter) updatePredictionMetrics(result *types.CheckResult) {
	// Reset prediction metrics
	pe.oomPredictionSeconds.Reset()
	pe.confidenceScore.Reset()

	// Process predictions
	for _, problem := range result.Problems {
		if problem.Prediction != nil {
			// OOM predictions
			if strings.Contains(problem.Title, "OOM") {
				pe.oomPredictionSeconds.WithLabelValues(
					problem.Resource.Name,
					problem.Resource.Namespace,
				).Set(problem.Prediction.TimeToFailure.Seconds())

				pe.confidenceScore.WithLabelValues(
					problem.Resource.Name,
					problem.Resource.Namespace,
					"oom",
				).Set(problem.Prediction.Confidence)
			}
		}
	}
}

// updateV2EngineMetrics updates V2 engine specific metrics
func (pe *PrometheusExporter) updateV2EngineMetrics() {
	stats := pe.v2Engine.Stats()
	
	// Engine health
	healthValue := 0.0
	if stats.IsHealthy {
		healthValue = 1.0
	}
	pe.v2EngineHealth.WithLabelValues("overall").Set(healthValue)
	
	// Processing metrics per shard
	for i, shardStats := range stats.ShardStats {
		shardLabel := fmt.Sprintf("shard_%d", i)
		
		// Shard health
		shardHealthValue := 0.0
		if shardStats.IsHealthy {
			shardHealthValue = 1.0
		}
		pe.v2EngineHealth.WithLabelValues(shardLabel).Set(shardHealthValue)
		
		// Processing latency
		if shardStats.AvgProcessingTime > 0 {
			pe.v2ProcessingLatency.WithLabelValues(shardLabel).Observe(shardStats.AvgProcessingTime.Seconds())
		}
	}
	
	// Router health
	if stats.RouterStats.BackpressureActive {
		pe.v2EventsDropped.WithLabelValues("backpressure").Add(float64(stats.RouterStats.EventsDropped))
	}
}

// Handler returns the Prometheus HTTP handler
func (pe *PrometheusExporter) Handler() http.Handler {
	return promhttp.HandlerFor(pe.registry, promhttp.HandlerOpts{})
}

// Shutdown gracefully shuts down the exporter
func (pe *PrometheusExporter) Shutdown() error {
	if pe.ebpfMonitor != nil {
		pe.ebpfMonitor.Stop()
	}
	
	if pe.v2Engine != nil {
		return pe.v2Engine.Stop()
	}
	
	return nil
}

// registerDefaultRules registers default correlation rules for metrics
func registerDefaultRules(engine *correlation.Engine) {
	// High error rate detection
	engine.RegisterRule(&events_correlation.Rule{
		ID:          "metrics-high-error-rate",
		Name:        "High Error Rate Detection",
		Description: "Detects high error rates in pods",
		Category:    events_correlation.CategoryReliability,
		RequiredSources: []events_correlation.EventSource{
			events_correlation.SourceKubernetes,
		},
		Enabled: true,
		Evaluate: func(ctx *events_correlation.Context) *events_correlation.Result {
			criticalEvents := ctx.GetEvents(events_correlation.Filter{
				Type: "critical",
			})
			
			if len(criticalEvents) > 5 {
				return &events_correlation.Result{
					RuleID:     "metrics-high-error-rate",
					RuleName:   "High Error Rate Detection",
					Timestamp:  time.Now(),
					Confidence: 0.9,
					Severity:   events_correlation.SeverityCritical,
					Category:   events_correlation.CategoryReliability,
					Title:      "High Error Rate Detected",
					Description: fmt.Sprintf("Detected %d critical events indicating high error rate", len(criticalEvents)),
				}
			}
			return nil
		},
	})
}