// Package metrics provides Prometheus metrics exporting for Tapio health checks
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/correlation/rules"
	"github.com/falseyair/tapio/pkg/correlation/sources"
	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/simple"
	"github.com/falseyair/tapio/pkg/types"
	"github.com/falseyair/tapio/pkg/universal"
	"github.com/falseyair/tapio/pkg/universal/converters"
	"github.com/falseyair/tapio/pkg/universal/formatters"
)

// PrometheusExporter exports Tapio metrics to Prometheus format
type PrometheusExporter struct {
	checker     CheckerInterface
	ebpfMonitor ebpf.Monitor
	registry    *prometheus.Registry
	correlationEngine *correlation.Engine

	// Universal format components
	formatter      *formatters.PrometheusFormatter
	ebpfConverter  *converters.EBPFConverter
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
}

// CheckerInterface defines the interface for checkers
type CheckerInterface interface {
	Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error)
}

// NewPrometheusExporter creates a new Prometheus metrics exporter
func NewPrometheusExporter(checker CheckerInterface, ebpfMonitor ebpf.Monitor) *PrometheusExporter {
	registry := prometheus.NewRegistry()

	// Create universal format components
	formatter := formatters.NewPrometheusFormatter("tapio", "", registry)
	ebpfConverter := converters.NewEBPFConverter("prometheus", "1.0")
	correlationConverter := converters.NewCorrelationConverter()

	// Create correlation engine with real data sources if checker is simple.Checker
	var correlationEngine *correlation.Engine
	if simpleChecker, ok := checker.(*simple.Checker); ok {
		// Create data sources
		dataSources := make(map[correlation.SourceType]correlation.DataSource)
		
		// Add Kubernetes data source
		k8sSource := sources.NewKubernetesDataSource(simpleChecker)
		dataSources[correlation.SourceKubernetes] = k8sSource
		
		// Add eBPF data source if available
		if ebpfMonitor != nil {
			ebpfSource := sources.NewEBPFDataSource(ebpfMonitor)
			dataSources[correlation.SourceEBPF] = ebpfSource
		}
		
		// Create data collection
		dataCollection := correlation.NewDataCollection(dataSources)
		
		// Create correlation engine
		config := correlation.DefaultEngineConfig()
		ruleRegistry := correlation.NewRuleRegistry()
		
		// Register default rules
		if err := rules.RegisterDefaultRules(ruleRegistry); err != nil {
			fmt.Printf("[WARN] Failed to register default rules: %v\n", err)
		}
		
		correlationEngine = correlation.NewEngine(config, ruleRegistry, dataCollection)
	}

	exporter := &PrometheusExporter{
		checker:     checker,
		ebpfMonitor: ebpfMonitor,
		registry:    registry,
		correlationEngine: correlationEngine,
		formatter:   formatter,
		ebpfConverter: ebpfConverter,
		correlationConverter: correlationConverter,

		// Health metrics
		podHealthStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_pod_health_status",
				Help: "Pod health status (0=healthy, 1=warning, 2=critical)",
			},
			[]string{"pod", "namespace", "status"},
		),

		clusterHealthScore: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_cluster_health_score",
				Help: "Overall cluster health score (0.0 to 1.0)",
			},
			[]string{"namespace"},
		),

		problemsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_problems_total",
				Help: "Total number of problems detected by Tapio",
			},
			[]string{"namespace", "severity", "type"},
		),

		// Prediction metrics
		oomPredictionSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_oom_prediction_seconds",
				Help: "Seconds until predicted OOM kill (0 = no prediction)",
			},
			[]string{"pod", "namespace", "container"},
		),

		confidenceScore: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_confidence_score",
				Help: "Confidence score for predictions (0.0 to 1.0)",
			},
			[]string{"pod", "namespace", "prediction_type"},
		),

		// Performance metrics
		analysisLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_analysis_duration_seconds",
				Help:    "Time taken for Tapio analysis operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "namespace"},
		),

		lastUpdateTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_last_update_timestamp",
				Help: "Timestamp of last metric update",
			},
			[]string{"component"},
		),
	}

	// Register all metrics
	exporter.registerMetrics()

	return exporter
}

// registerMetrics registers all metrics with the Prometheus registry
func (e *PrometheusExporter) registerMetrics() {
	metrics := []prometheus.Collector{
		e.podHealthStatus,
		e.clusterHealthScore,
		e.problemsTotal,
		e.oomPredictionSeconds,
		e.confidenceScore,
		e.analysisLatency,
		e.lastUpdateTime,
	}

	for _, metric := range metrics {
		e.registry.MustRegister(metric)
	}

	metricsCount := 7
	if e.ebpfMonitor != nil && e.ebpfMonitor.IsAvailable() {
		// Register additional eBPF metrics if available
		e.registerEBPFMetrics()
		metricsCount += 3
	}
	fmt.Printf("[OK] Registered %d Tapio metrics with Prometheus\n", metricsCount)
}

// UpdateMetrics updates all Prometheus metrics with current Tapio data
func (e *PrometheusExporter) UpdateMetrics(ctx context.Context) error {
	startTime := time.Now()
	defer func() {
		e.analysisLatency.WithLabelValues("full_update", "all").Observe(time.Since(startTime).Seconds())
		e.lastUpdateTime.WithLabelValues("metrics_update").SetToCurrentTime()
	}()

	// Get current health check results
	checkReq := &types.CheckRequest{All: true}
	result, err := e.checker.Check(ctx, checkReq)
	if err != nil {
		return fmt.Errorf("failed to get health check results: %w", err)
	}

	// Update metrics
	e.updateHealthMetrics(result)
	e.updatePredictionMetrics(result)

	// Update eBPF metrics if available
	if e.ebpfMonitor != nil && e.ebpfMonitor.IsAvailable() {
		e.updateEBPFMetrics()
	}

	return nil
}

// updateHealthMetrics updates pod and cluster health metrics
func (e *PrometheusExporter) updateHealthMetrics(result *types.CheckResult) {
	// Clear existing health metrics
	e.podHealthStatus.Reset()
	e.clusterHealthScore.Reset()

	// Track problems by namespace for cluster health scores
	namespaceStats := make(map[string]*types.Summary)

	// Update pod health status from problems
	for _, problem := range result.Problems {
		namespace := problem.Resource.Namespace
		pod := problem.Resource.Name

		// Set pod health status
		statusValue := getSeverityValue(problem.Severity)
		e.podHealthStatus.WithLabelValues(pod, namespace, string(problem.Severity)).Set(statusValue)

		// Track namespace statistics
		if _, exists := namespaceStats[namespace]; !exists {
			namespaceStats[namespace] = &types.Summary{}
		}

		switch problem.Severity {
		case types.SeverityCritical:
			namespaceStats[namespace].CriticalPods++
			e.problemsTotal.WithLabelValues(namespace, "critical", "pod_issue").Inc()
		case types.SeverityWarning:
			namespaceStats[namespace].WarningPods++
			e.problemsTotal.WithLabelValues(namespace, "warning", "pod_issue").Inc()
		default:
			namespaceStats[namespace].HealthyPods++
		}
		namespaceStats[namespace].TotalPods++
	}

	// Calculate and update cluster health scores by namespace
	for namespace, stats := range namespaceStats {
		if stats.TotalPods > 0 {
			// Health score: 1.0 = all healthy, 0.5 = some warnings, 0.0 = all critical
			healthScore := (float64(stats.HealthyPods) + float64(stats.WarningPods)*0.5) / float64(stats.TotalPods)
			e.clusterHealthScore.WithLabelValues(namespace).Set(healthScore)
		}
	}
}

// updatePredictionMetrics updates OOM and other prediction metrics
func (e *PrometheusExporter) updatePredictionMetrics(result *types.CheckResult) {
	// Clear prediction metrics
	e.oomPredictionSeconds.Reset()
	e.confidenceScore.Reset()

	for _, problem := range result.Problems {
		if problem.Prediction != nil {
			namespace := problem.Resource.Namespace
			pod := problem.Resource.Name

			// Update OOM prediction timing
			secondsToFailure := problem.Prediction.TimeToFailure.Seconds()
			e.oomPredictionSeconds.WithLabelValues(pod, namespace, "main").Set(secondsToFailure)

			// Update confidence score
			e.confidenceScore.WithLabelValues(pod, namespace, "oom").Set(problem.Prediction.Confidence)
		}
	}
}

func getSeverityValue(severity types.Severity) float64 {
	switch severity {
	case types.SeverityHealthy:
		return 0
	case types.SeverityWarning:
		return 1
	case types.SeverityCritical:
		return 2
	default:
		return 0
	}
}

// StartMetricsServer starts the Prometheus metrics HTTP server
func (e *PrometheusExporter) StartMetricsServer(addr string) error {
	// Create HTTP mux for multiple endpoints
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","service":"tapio-prometheus-exporter"}`))
	})

	// Info endpoint
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"service": "tapio-prometheus-exporter",
			"version": "1.0.0",
			"endpoints": ["/metrics", "/health", "/info"],
			"description": "Tapio Kubernetes intelligence metrics for Prometheus"
		}`))
	})

	fmt.Printf("🚀 Prometheus metrics server starting on %s\n", addr)
	fmt.Printf("📊 Metrics endpoint: http://%s/metrics\n", addr)
	fmt.Printf("💚 Health endpoint: http://%s/health\n", addr)

	// nolint:gosec // Prometheus exporters typically don't need timeouts
	return http.ListenAndServe(addr, mux)
}

// StartPeriodicUpdates starts a goroutine that updates metrics periodically
func (e *PrometheusExporter) StartPeriodicUpdates(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	fmt.Printf("🔄 Starting periodic metric updates every %v\n", interval)

	// Initial update
	if err := e.UpdateMetrics(ctx); err != nil {
		fmt.Printf("Warning: Initial metrics update failed: %v\n", err)
	} else {
		fmt.Println("✅ Initial metrics update completed")
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("🛑 Stopping periodic metrics updates")
			return
		case <-ticker.C:
			if err := e.UpdateMetrics(ctx); err != nil {
				fmt.Printf("Warning: Metrics update failed: %v\n", err)
			}
		}
	}
}

// registerEBPFMetrics registers eBPF-specific metrics
func (e *PrometheusExporter) registerEBPFMetrics() {
	// Real memory usage from eBPF
	realMemoryUsage := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_ebpf_memory_usage_bytes",
			Help: "Real memory usage tracked by eBPF (kernel-level)",
		},
		[]string{"pod", "namespace", "container"},
	)

	// Memory allocation rate
	allocationRate := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_ebpf_memory_allocation_rate_bytes_per_second",
			Help: "Memory allocation rate tracked by eBPF",
		},
		[]string{"pod", "namespace", "container"},
	)

	// Process count in container
	processCount := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tapio_ebpf_container_process_count",
			Help: "Number of processes in container tracked by eBPF",
		},
		[]string{"pod", "namespace", "container"},
	)

	e.registry.MustRegister(realMemoryUsage, allocationRate, processCount)
}

// updateEBPFMetrics updates metrics from eBPF data
func (e *PrometheusExporter) updateEBPFMetrics() {
	// Get memory stats from eBPF
	memStats, err := e.ebpfMonitor.GetMemoryStats()
	if err != nil {
		fmt.Printf("[WARN] Failed to get eBPF memory stats: %v\n", err)
		return
	}

	// Convert to universal format and export
	for _, stats := range memStats {
		// Convert eBPF stats to universal metric
		metric, err := e.ebpfConverter.ConvertProcessMemoryStats(&stats)
		if err != nil {
			fmt.Printf("[WARN] Failed to convert eBPF stats: %v\n", err)
			continue
		}

		// Format and export via Prometheus formatter
		if err := e.formatter.FormatMetric(metric); err != nil {
			fmt.Printf("[WARN] Failed to format metric: %v\n", err)
		}

		// Return metric to pool
		universal.PutMetric(metric)

		// Convert growth pattern if available
		if len(stats.GrowthPattern) > 0 {
			growthMetrics, err := e.ebpfConverter.ConvertMemoryGrowthToMetrics(&stats)
			if err == nil {
				for _, gm := range growthMetrics {
					e.formatter.FormatMetric(gm)
					universal.PutMetric(gm)
				}
			}
		}
	}

	// Get OOM predictions if available
	limits := make(map[uint32]uint64)
	// TODO: Get actual memory limits from Kubernetes
	predictions, err := e.ebpfMonitor.GetMemoryPredictions(limits)
	if err == nil {
		e.updateUniversalPredictions(predictions)
	}
}

// updateUniversalPredictions updates predictions using universal format
func (e *PrometheusExporter) updateUniversalPredictions(predictions map[uint32]*ebpf.OOMPrediction) {
	for pid, pred := range predictions {
		// Convert to universal format
		target := universal.Target{
			Type: universal.TargetTypeProcess,
			PID:  int32(pid),
			Name: fmt.Sprintf("pid-%d", pid),
		}

		universalPred := e.correlationConverter.ConvertOOMPrediction(
			&target,
			pred.TimeToOOM,
			pred.Confidence,
			pred.CurrentUsage,
			pred.MemoryLimit,
			0, // growth rate not provided in ebpf.OOMPrediction
		)

		// Format and export
		if err := e.formatter.FormatPrediction(universalPred); err != nil {
			fmt.Printf("[WARN] Failed to format prediction: %v\n", err)
		}

		// Return to pool
		universal.PutPrediction(universalPred)
	}
}

// UpdateMetricsWithUniversal updates metrics using the universal data format
func (e *PrometheusExporter) UpdateMetricsWithUniversal(ctx context.Context) error {
	startTime := time.Now()
	defer func() {
		e.analysisLatency.WithLabelValues("universal_update", "all").Observe(time.Since(startTime).Seconds())
		e.lastUpdateTime.WithLabelValues("universal_metrics").SetToCurrentTime()
	}()

	// Use correlation engine if available
	if e.correlationEngine != nil {
		// Run correlation analysis
		findings, err := e.correlationEngine.Execute(ctx)
		if err != nil {
			fmt.Printf("[WARN] Correlation analysis failed: %v\n", err)
		} else {
			fmt.Printf("[OK] Correlation analysis found %d findings\n", len(findings))
			
			// Convert findings to universal format predictions
			for _, finding := range findings {
				// Convert finding to universal format
				pred, err := e.correlationConverter.ConvertFinding(&finding)
				if err != nil {
					fmt.Printf("[WARN] Failed to convert finding: %v\n", err)
					continue
				}

				// Export via formatter
				if err := e.formatter.FormatPrediction(pred); err != nil {
					fmt.Printf("[WARN] Failed to format prediction: %v\n", err)
				}
				universal.PutPrediction(pred)
			}
		}
	}

	// Update eBPF metrics using universal format
	if e.ebpfMonitor != nil && e.ebpfMonitor.IsAvailable() {
		e.updateEBPFMetrics()
	}

	// Update regular metrics from checker
	if err := e.UpdateMetrics(ctx); err != nil {
		return fmt.Errorf("failed to update standard metrics: %w", err)
	}

	return nil
}