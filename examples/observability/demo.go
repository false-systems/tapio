package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/exports/otel"
	"github.com/yairfalse/tapio/pkg/exports/prometheus"
)

// DemoServer demonstrates Tapio OTEL and Prometheus export integration
type DemoServer struct {
	otelExporter       *otel.Exporter
	prometheusExporter *prometheus.Exporter

	// Demo state
	correlationCounter int64
	patternTypes       []string
	severityLevels     []correlation.Severity
}

// NewDemoServer creates a new demo server
func NewDemoServer() (*DemoServer, error) {
	// Initialize OTEL exporter
	otelConfig := &otel.ExporterConfig{
		ServiceName:    "tapio-demo",
		ServiceVersion: "1.0.0",
		OTLPEndpoint:   getEnv("TAPIO_OTEL_ENDPOINT", "http://localhost:4318/v1/traces"),
		SamplingRate:   1.0, // Sample all traces for demo
	}

	otelExporter, err := otel.NewExporter(otelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL exporter: %w", err)
	}

	// Initialize Prometheus exporter
	promConfig := &prometheus.ExporterConfig{
		ListenAddress: getEnv("TAPIO_PROMETHEUS_ADDR", ":9091"),
		MetricsPath:   "/metrics",
	}

	promExporter, err := prometheus.NewExporter(promConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}

	return &DemoServer{
		otelExporter:       otelExporter,
		prometheusExporter: promExporter,
		patternTypes:       []string{"memory_leak", "network_failure", "storage_bottleneck", "cpu_throttle", "oom_prediction"},
		severityLevels:     []correlation.Severity{"low", "medium", "high", "critical"},
	}, nil
}

// Start starts the demo server
func (ds *DemoServer) Start(ctx context.Context) error {
	// Start exporters
	if err := ds.otelExporter.Start(ctx); err != nil {
		return fmt.Errorf("failed to start OTEL exporter: %w", err)
	}

	if err := ds.prometheusExporter.Start(ctx); err != nil {
		return fmt.Errorf("failed to start Prometheus exporter: %w", err)
	}

	// Setup HTTP handlers
	http.HandleFunc("/health", ds.healthHandler)
	http.HandleFunc("/ready", ds.readinessHandler)
	http.HandleFunc("/api/correlations", ds.correlationsHandler)
	http.HandleFunc("/api/correlations/generate", ds.generateCorrelationHandler)
	http.HandleFunc("/api/patterns/validate", ds.validatePatternHandler)
	http.HandleFunc("/api/system/health", ds.systemHealthHandler)

	// Start background demo data generation
	go ds.generateDemoData(ctx)

	// Start HTTP server
	port := getEnv("PORT", "8080")
	log.Printf("Starting demo server on port %s", port)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: http.DefaultServeMux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

// Stop stops the demo server
func (ds *DemoServer) Stop(ctx context.Context) error {
	var lastErr error

	if err := ds.otelExporter.Stop(ctx); err != nil {
		lastErr = err
		log.Printf("Error stopping OTEL exporter: %v", err)
	}

	if err := ds.prometheusExporter.Stop(ctx); err != nil {
		lastErr = err
		log.Printf("Error stopping Prometheus exporter: %v", err)
	}

	return lastErr
}

// HTTP Handlers

func (ds *DemoServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (ds *DemoServer) readinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (ds *DemoServer) correlationsHandler(w http.ResponseWriter, r *http.Request) {
	// Generate and export a correlation for demo
	result := ds.generateSampleCorrelation()

	// Export to both OTEL and Prometheus
	ctx := r.Context()

	if err := ds.otelExporter.ExportCorrelationResult(ctx, result); err != nil {
		log.Printf("Failed to export to OTEL: %v", err)
	}

	if err := ds.prometheusExporter.ExportCorrelationResult(ctx, result); err != nil {
		log.Printf("Failed to export to Prometheus: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (ds *DemoServer) generateCorrelationHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	countStr := r.URL.Query().Get("count")
	count := 1
	if countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 {
			count = c
		}
	}

	severityFilter := r.URL.Query().Get("severity")
	patternFilter := r.URL.Query().Get("pattern")

	var results []*correlation.Result
	for i := 0; i < count; i++ {
		result := ds.generateSampleCorrelation()

		// Apply filters if specified
		if severityFilter != "" && string(result.Severity) != severityFilter {
			continue
		}
		if patternFilter != "" && result.RuleID != patternFilter {
			continue
		}

		results = append(results, result)
	}

	// Export batch
	ctx := r.Context()

	if err := ds.otelExporter.ExportCorrelationBatch(ctx, results); err != nil {
		log.Printf("Failed to export batch to OTEL: %v", err)
	}

	if err := ds.prometheusExporter.ExportCorrelationBatch(ctx, results); err != nil {
		log.Printf("Failed to export batch to Prometheus: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"generated": len(results),
		"results":   results,
	})
}

func (ds *DemoServer) validatePatternHandler(w http.ResponseWriter, r *http.Request) {
	patternType := r.URL.Query().Get("type")
	if patternType == "" {
		patternType = ds.patternTypes[rand.Intn(len(ds.patternTypes))]
	}

	// Simulate pattern validation
	detected := rand.Float64() > 0.3
	confidence := 0.5 + rand.Float64()*0.5
	accuracy := 0.7 + rand.Float64()*0.3

	// Export pattern metrics
	ctx := r.Context()
	if err := ds.prometheusExporter.ExportPatternMetrics(ctx, patternType, detected, confidence, accuracy); err != nil {
		log.Printf("Failed to export pattern metrics: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pattern_type": patternType,
		"detected":     detected,
		"confidence":   confidence,
		"accuracy":     accuracy,
	})
}

func (ds *DemoServer) systemHealthHandler(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	cluster := r.URL.Query().Get("cluster")
	if cluster == "" {
		cluster = "demo-cluster"
	}

	// Generate system health metrics
	healthScore := 0.7 + rand.Float64()*0.3
	cpuUsage := rand.Float64() * 0.8
	memoryUsage := rand.Float64() * 0.9

	ctx := r.Context()

	// Export system health
	if err := ds.prometheusExporter.ExportSystemHealth(ctx, namespace, cluster, healthScore); err != nil {
		log.Printf("Failed to export system health: %v", err)
	}

	// Export resource usage
	if err := ds.prometheusExporter.ExportResourceUsage(ctx, "cpu", namespace, "node-1", cpuUsage); err != nil {
		log.Printf("Failed to export CPU usage: %v", err)
	}

	if err := ds.prometheusExporter.ExportResourceUsage(ctx, "memory", namespace, "node-1", memoryUsage); err != nil {
		log.Printf("Failed to export memory usage: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"namespace":    namespace,
		"cluster":      cluster,
		"health_score": healthScore,
		"cpu_usage":    cpuUsage,
		"memory_usage": memoryUsage,
	})
}

// Demo data generation

func (ds *DemoServer) generateDemoData(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Generate random correlation
			result := ds.generateSampleCorrelation()

			// Export to both systems
			if err := ds.otelExporter.ExportCorrelationResult(ctx, result); err != nil {
				log.Printf("Background export to OTEL failed: %v", err)
			}

			if err := ds.prometheusExporter.ExportCorrelationResult(ctx, result); err != nil {
				log.Printf("Background export to Prometheus failed: %v", err)
			}

			// Generate system health data
			healthScore := 0.8 + rand.Float64()*0.2
			if err := ds.prometheusExporter.ExportSystemHealth(ctx, "kube-system", "demo-cluster", healthScore); err != nil {
				log.Printf("Background system health export failed: %v", err)
			}

		case <-ctx.Done():
			return
		}
	}
}

func (ds *DemoServer) generateSampleCorrelation() *correlation.Result {
	ds.correlationCounter++

	patternType := ds.patternTypes[rand.Intn(len(ds.patternTypes))]
	severity := ds.severityLevels[rand.Intn(len(ds.severityLevels))]

	return &correlation.Result{
		RuleID:      patternType,
		RuleName:    fmt.Sprintf("%s_detection", patternType),
		Timestamp:   time.Now(),
		Confidence:  0.5 + rand.Float64()*0.5,
		Severity:    severity,
		Category:    correlation.CategoryPerformance,
		Title:       fmt.Sprintf("%s detected in demo environment", patternType),
		Description: fmt.Sprintf("Demo correlation #%d: %s pattern detected with automated analysis", ds.correlationCounter, patternType),
		Evidence: correlation.Evidence{
			Events: []correlation.Event{
				{
					ID:        fmt.Sprintf("evt-%d", ds.correlationCounter),
					Timestamp: time.Now().Add(-time.Duration(rand.Intn(300)) * time.Second),
					Source:    correlation.SourceEBPF,
					Type:      patternType,
					Entity: correlation.Entity{
						Type:      "pod",
						Name:      fmt.Sprintf("demo-app-%d", rand.Intn(10)),
						Namespace: "default",
						Node:      fmt.Sprintf("node-%d", rand.Intn(3)+1),
					},
				},
			},
			Metrics: map[string]float64{
				"cpu_usage":    rand.Float64() * 100,
				"memory_usage": rand.Float64() * 100,
				"confidence":   0.7 + rand.Float64()*0.3,
			},
		},
		Recommendations: []string{
			fmt.Sprintf("Monitor %s patterns more closely", patternType),
			"Consider scaling resources if pattern persists",
		},
	}
}

// Utility functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	// Setup graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create and start demo server
	server, err := NewDemoServer()
	if err != nil {
		log.Fatalf("Failed to create demo server: %v", err)
	}

	// Start server
	if err := server.Start(ctx); err != nil {
		log.Printf("Server stopped: %v", err)
	}

	// Stop server
	if err := server.Stop(context.Background()); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Demo server shutdown complete")
}
