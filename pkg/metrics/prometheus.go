package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	
	"github.com/falseyair/tapio/pkg/types"
)

// PrometheusExporter exports Tapio metrics to Prometheus
type PrometheusExporter struct {
	checker    CheckerInterface
	collector  interface{} // *ebpf.Collector
	registry   *prometheus.Registry
	
	// Prediction metrics
	oomPredictionSeconds *prometheus.GaugeVec
	confidenceScore      *prometheus.GaugeVec
	memoryGrowthRate     *prometheus.GaugeVec
	
	// Health metrics
	podHealthStatus      *prometheus.GaugeVec
	clusterHealthScore   *prometheus.GaugeVec
	problemsTotal        *prometheus.CounterVec
	
	// eBPF metrics
	ebpfMemoryAllocations *prometheus.GaugeVec
	ebpfMemoryFrees       *prometheus.GaugeVec
	ebpfProcessCount      *prometheus.GaugeVec
	ebpfLeakDetected      *prometheus.GaugeVec
	
	// Performance metrics
	analysisLatency      *prometheus.HistogramVec
	lastUpdateTime       *prometheus.GaugeVec
}

// CheckerInterface defines the interface for checkers
type CheckerInterface interface {
	Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error)
}

// NewPrometheusExporter creates a new Prometheus metrics exporter
func NewPrometheusExporter(checker CheckerInterface, collector interface{}) *PrometheusExporter {
	registry := prometheus.NewRegistry()
	
	exporter := &PrometheusExporter{
		checker:   checker,
		collector: collector,
		registry:  registry,
		
		// Prediction metrics - These are the GAME CHANGERS
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
		
		memoryGrowthRate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_memory_growth_rate_bytes_per_sec",
				Help: "Memory growth rate in bytes per second from eBPF",
			},
			[]string{"pod", "namespace", "container"},
		),
		
		// Health metrics - Simple but essential
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
		
		// eBPF metrics - Kernel-level insights
		ebpfMemoryAllocations: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_ebpf_memory_allocations_total",
				Help: "Total memory allocations tracked by eBPF",
			},
			[]string{"pod", "namespace", "process"},
		),
		
		ebpfMemoryFrees: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_ebpf_memory_frees_total", 
				Help: "Total memory frees tracked by eBPF",
			},
			[]string{"pod", "namespace", "process"},
		),
		
		ebpfProcessCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_ebpf_process_count",
				Help: "Number of processes tracked by eBPF for this pod",
			},
			[]string{"pod", "namespace"},
		),
		
		ebpfLeakDetected: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tapio_ebpf_leak_detected",
				Help: "Memory leak detected by eBPF (1=detected, 0=not detected)",
			},
			[]string{"pod", "namespace", "leak_type"},
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
		e.oomPredictionSeconds,
		e.confidenceScore,
		e.memoryGrowthRate,
		e.podHealthStatus,
		e.clusterHealthScore,
		e.problemsTotal,
		e.ebpfMemoryAllocations,
		e.ebpfMemoryFrees,
		e.ebpfProcessCount,
		e.ebpfLeakDetected,
		e.analysisLatency,
		e.lastUpdateTime,
	}
	
	for _, metric := range metrics {
		e.registry.MustRegister(metric)
	}
	
	fmt.Println("✅ Registered 12 Tapio metrics with Prometheus")
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
	
	// Update all metric categories
	e.updateHealthMetrics(result)
	e.updatePredictionMetrics(result)
	
	// if e.collector != nil {
	//	e.updateEBPFMetrics()
	// }
	
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
	
	// Also include healthy pods from the summary
	e.updateHealthFromSummary(result.Summary, namespaceStats)
	
	// Calculate and update cluster health scores by namespace
	for namespace, stats := range namespaceStats {
		if stats.TotalPods > 0 {
			// Health score: 1.0 = all healthy, 0.5 = some warnings, 0.0 = all critical
			healthScore := (float64(stats.HealthyPods) + float64(stats.WarningPods)*0.5) / float64(stats.TotalPods)
			e.clusterHealthScore.WithLabelValues(namespace).Set(healthScore)
		}
	}
}

// updateHealthFromSummary adds healthy pods to namespace stats
func (e *PrometheusExporter) updateHealthFromSummary(summary types.Summary, namespaceStats map[string]*types.Summary) {
	// For simplicity, assume healthy pods are in "default" namespace
	// In production, you'd need better namespace tracking
	defaultNS := "default"
	if _, exists := namespaceStats[defaultNS]; !exists {
		namespaceStats[defaultNS] = &types.Summary{}
	}
	
	// Add healthy pods that weren't already counted in problems
	namespaceStats[defaultNS].HealthyPods += summary.HealthyPods
	namespaceStats[defaultNS].TotalPods += summary.HealthyPods
}

// updatePredictionMetrics updates OOM and other prediction metrics
func (e *PrometheusExporter) updatePredictionMetrics(result *types.CheckResult) {
	// Clear prediction metrics
	e.oomPredictionSeconds.Reset()
	e.confidenceScore.Reset()
	e.memoryGrowthRate.Reset()
	
	for _, problem := range result.Problems {
		if problem.Prediction != nil {
			namespace := problem.Resource.Namespace
			pod := problem.Resource.Name
			
			// Update OOM prediction timing
			secondsToFailure := problem.Prediction.TimeToFailure.Seconds()
			e.oomPredictionSeconds.WithLabelValues(pod, namespace, "main").Set(secondsToFailure)
			
			// Update confidence score
			e.confidenceScore.WithLabelValues(pod, namespace, "oom").Set(problem.Prediction.Confidence)
			
			// Extract growth rate from eBPF data if available
			// if e.collector != nil {
			//	e.addEBPFGrowthRateForPod(pod, namespace)
			// }
		}
	}
}

/*
// addEBPFGrowthRateForPod adds memory growth rate for a specific pod
func (e *PrometheusExporter) addEBPFGrowthRateForPod(pod, namespace string) {
	processStats := e.collector.GetContainerProcesses()
	
	for _, stats := range processStats {
		if e.isProcessForPod(stats, pod, namespace) && len(stats.GrowthPattern) > 1 {
			// Calculate growth rate from recent data points
			recent := stats.GrowthPattern
			if len(recent) >= 2 {
				// Use last two points for growth calculation
				lastTwo := recent[len(recent)-2:]
				timeDiff := lastTwo[1].Timestamp.Sub(lastTwo[0].Timestamp).Seconds()
				if timeDiff > 0 {
					usageDiff := int64(lastTwo[1].Usage) - int64(lastTwo[0].Usage)
					growthRate := float64(usageDiff) / timeDiff
					e.memoryGrowthRate.WithLabelValues(pod, namespace, "main").Set(growthRate)
				}
			}
			break // Only process one match per pod
		}
	}
}
*/

/*
// updateEBPFMetrics updates eBPF-specific metrics
func (e *PrometheusExporter) updateEBPFMetrics() {
	processStats := e.collector.GetContainerProcesses()
	
	// Clear eBPF metrics
	e.ebpfProcessCount.Reset()
	e.ebpfLeakDetected.Reset()
	e.ebpfMemoryAllocations.Reset()
	e.ebpfMemoryFrees.Reset()
	
	// Group processes by pod (simplified mapping)
	podProcesses := make(map[string][]*ebpf.ProcessMemoryStats)
	
	for _, stats := range processStats {
		// Simplified pod identification - in production use proper mapping
		podKey := e.generatePodKey(stats)
		podProcesses[podKey] = append(podProcesses[podKey], stats)
	}
	
	for podKey, processes := range podProcesses {
		namespace, pod := e.parsePodKey(podKey)
		
		// Update process count
		e.ebpfProcessCount.WithLabelValues(pod, namespace).Set(float64(len(processes)))
		
		// Update allocation/free metrics and leak detection
		for _, stats := range processes {
			processName := stats.Command
			
			// Update allocation/free totals
			e.ebpfMemoryAllocations.WithLabelValues(pod, namespace, processName).Set(float64(stats.TotalAllocated))
			e.ebpfMemoryFrees.WithLabelValues(pod, namespace, processName).Set(float64(stats.TotalFreed))
			
			// Detect and report memory leaks
			e.updateLeakDetection(pod, namespace, stats)
		}
	}
}

// updateLeakDetection analyzes and reports memory leak detection
func (e *PrometheusExporter) updateLeakDetection(pod, namespace string, stats *ebpf.ProcessMemoryStats) {
	if stats.TotalAllocated > 0 && stats.TotalFreed > 0 {
		leakRatio := 1.0 - (float64(stats.TotalFreed) / float64(stats.TotalAllocated))
		
		// Different thresholds for different leak types
		if leakRatio > 0.5 {
			e.ebpfLeakDetected.WithLabelValues(pod, namespace, "major_leak").Set(1)
		} else if leakRatio > 0.2 {
			e.ebpfLeakDetected.WithLabelValues(pod, namespace, "minor_leak").Set(1)
		} else {
			e.ebpfLeakDetected.WithLabelValues(pod, namespace, "no_leak").Set(0)
		}
	}
}
*/

/*
// Helper functions for pod identification and parsing
func (e *PrometheusExporter) generatePodKey(stats *ebpf.ProcessMemoryStats) string {
	if stats.InContainer {
		// Use container PID to generate a pod-like identifier
		return fmt.Sprintf("default/container-proc-%d", stats.ContainerPID)
	}
	return fmt.Sprintf("host/process-%d", stats.PID)
}

func (e *PrometheusExporter) parsePodKey(key string) (namespace, pod string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "unknown", key
}

func (e *PrometheusExporter) isProcessForPod(stats *ebpf.ProcessMemoryStats, pod, namespace string) bool {
	// Simplified heuristic - in production, use proper pod-to-process mapping
	return stats.InContainer && (strings.Contains(stats.Command, pod) || 
	       strings.Contains(pod, stats.Command))
}
*/

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
		w.Write([]byte(`{"status":"healthy","service":"tapio-prometheus-exporter"}`))
	})
	
	// Info endpoint
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"service": "tapio-prometheus-exporter",
			"version": "1.0.0",
			"endpoints": ["/metrics", "/health", "/info"],
			"description": "Tapio Kubernetes intelligence metrics for Prometheus"
		}`))
	})
	
	// Root endpoint with basic info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
		<html>
		<head><title>Tapio Prometheus Exporter</title></head>
		<body>
			<h1>🌲 Tapio Prometheus Exporter</h1>
			<p>Kubernetes intelligence metrics for enterprise monitoring</p>
			<ul>
				<li><a href="/metrics">Prometheus Metrics</a></li>
				<li><a href="/health">Health Check</a></li>
				<li><a href="/info">Service Info</a></li>
			</ul>
		</body>
		</html>
		`))
	})
	
	fmt.Printf("🚀 Prometheus metrics server starting on %s\n", addr)
	fmt.Printf("📊 Metrics endpoint: http://%s/metrics\n", addr)
	fmt.Printf("💚 Health endpoint: http://%s/health\n", addr)
	
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