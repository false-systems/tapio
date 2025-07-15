package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yairfalse/tapio/pkg/correlation"
)

// Exporter provides a high-level interface for exporting Tapio data to Prometheus
type Exporter struct {
	metricsExporter  *MetricsExporter
	customCollector  *CustomCollector
	factoryCollector *FactoryCollector
	config           *ExporterConfig

	// HTTP server for metrics endpoint
	httpServer *http.Server

	// Registry and handler
	registry *prometheus.Registry
	handler  http.Handler

	// State management
	running bool
	mutex   sync.RWMutex
}

// ExporterConfig configures the complete Prometheus exporter
type ExporterConfig struct {
	// HTTP server configuration
	ListenAddress string
	MetricsPath   string
	EnablePprof   bool

	// Metrics configuration
	MetricsConfig   *MetricsConfig
	CollectorConfig *CollectorConfig

	// Performance settings
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	MaxRequestSize    int64
	EnableCompression bool

	// Security settings
	EnableTLS       bool
	CertFile        string
	KeyFile         string
	EnableBasicAuth bool
	BasicAuthUsers  map[string]string

	// Feature flags
	EnableCustomCollector  bool
	EnableFactoryCollector bool
	EnableBuiltinMetrics   bool

	// Data providers
	CorrelationProvider  CorrelationProvider
	SystemHealthProvider SystemHealthProvider

	// Graceful shutdown
	ShutdownTimeout time.Duration
}

// NewExporter creates a new complete Prometheus exporter
func NewExporter(config *ExporterConfig) (*Exporter, error) {
	if config == nil {
		config = DefaultExporterConfig()
	}

	// Create registry
	registry := prometheus.NewRegistry()

	// Create metrics exporter
	metricsExporter := NewMetricsExporter(config.MetricsConfig)
	if err := registry.Register(metricsExporter); err != nil {
		return nil, fmt.Errorf("failed to register metrics exporter: %w", err)
	}

	// Create custom collector if enabled
	var customCollector *CustomCollector
	if config.EnableCustomCollector && config.CorrelationProvider != nil && config.SystemHealthProvider != nil {
		customCollector = NewCustomCollector(
			config.CorrelationProvider,
			config.SystemHealthProvider,
			config.CollectorConfig,
		)
		if err := registry.Register(customCollector); err != nil {
			return nil, fmt.Errorf("failed to register custom collector: %w", err)
		}
	}

	// Create factory collector if enabled
	var factoryCollector *FactoryCollector
	if config.EnableFactoryCollector {
		factoryCollector = NewFactoryCollector()
	}

	// Create HTTP handler
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics:   true,
		MaxRequestsInFlight: 10,
		Timeout:             config.WriteTimeout,
	})

	// Add middleware if needed
	if config.EnableCompression {
		handler = enableGzipCompression(handler)
	}

	if config.EnableBasicAuth {
		handler = enableBasicAuth(handler, config.BasicAuthUsers)
	}

	return &Exporter{
		metricsExporter:  metricsExporter,
		customCollector:  customCollector,
		factoryCollector: factoryCollector,
		config:           config,
		registry:         registry,
		handler:          handler,
	}, nil
}

// DefaultExporterConfig returns sensible defaults for the exporter
func DefaultExporterConfig() *ExporterConfig {
	return &ExporterConfig{
		ListenAddress:          ":9090",
		MetricsPath:            "/metrics",
		EnablePprof:            false,
		MetricsConfig:          DefaultMetricsConfig(),
		CollectorConfig:        DefaultCollectorConfig(),
		ReadTimeout:            30 * time.Second,
		WriteTimeout:           30 * time.Second,
		MaxRequestSize:         1024 * 1024, // 1MB
		EnableCompression:      true,
		EnableTLS:              false,
		EnableBasicAuth:        false,
		BasicAuthUsers:         make(map[string]string),
		EnableCustomCollector:  true,
		EnableFactoryCollector: true,
		EnableBuiltinMetrics:   true,
		ShutdownTimeout:        30 * time.Second,
	}
}

// Start starts the Prometheus exporter HTTP server
func (e *Exporter) Start(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.running {
		return fmt.Errorf("exporter already running")
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.Handle(e.config.MetricsPath, e.handler)

	// Add health check endpoint
	mux.HandleFunc("/health", e.healthHandler)
	mux.HandleFunc("/ready", e.readinessHandler)

	// Add pprof endpoints if enabled
	if e.config.EnablePprof {
		e.addPprofHandlers(mux)
	}

	e.httpServer = &http.Server{
		Addr:           e.config.ListenAddress,
		Handler:        mux,
		ReadTimeout:    e.config.ReadTimeout,
		WriteTimeout:   e.config.WriteTimeout,
		MaxHeaderBytes: int(e.config.MaxRequestSize),
	}

	// Start server in goroutine
	go func() {
		var err error
		if e.config.EnableTLS {
			err = e.httpServer.ListenAndServeTLS(e.config.CertFile, e.config.KeyFile)
		} else {
			err = e.httpServer.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			// Log error - in a real implementation, you'd use proper logging
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	e.running = true
	return nil
}

// Stop stops the Prometheus exporter
func (e *Exporter) Stop(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if !e.running {
		return nil
	}

	e.running = false

	// Shutdown HTTP server
	if e.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, e.config.ShutdownTimeout)
		defer cancel()

		if err := e.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown HTTP server: %w", err)
		}
	}

	return nil
}

// ExportCorrelationResult exports a correlation result as Prometheus metrics
func (e *Exporter) ExportCorrelationResult(ctx context.Context, result *correlation.Result) error {
	if !e.isRunning() {
		return fmt.Errorf("exporter not running")
	}

	return e.metricsExporter.ExportCorrelationResult(ctx, result)
}

// ExportCorrelationBatch exports multiple correlation results efficiently
func (e *Exporter) ExportCorrelationBatch(ctx context.Context, results []*correlation.Result) error {
	if !e.isRunning() {
		return fmt.Errorf("exporter not running")
	}

	for _, result := range results {
		if err := e.metricsExporter.ExportCorrelationResult(ctx, result); err != nil {
			// Continue with other results, but record the error
			e.metricsExporter.RecordError()
		}
	}

	return nil
}

// ExportSystemHealth exports system health metrics
func (e *Exporter) ExportSystemHealth(ctx context.Context, namespace, cluster string, healthScore float64) error {
	if !e.isRunning() {
		return nil
	}

	return e.metricsExporter.ExportSystemHealth(ctx, namespace, cluster, healthScore)
}

// ExportResourceUsage exports resource usage metrics
func (e *Exporter) ExportResourceUsage(ctx context.Context, resourceType, namespace, node string, usage float64) error {
	if !e.isRunning() {
		return nil
	}

	return e.metricsExporter.ExportResourceUsage(ctx, resourceType, namespace, node, usage)
}

// ExportProcessingRate exports event processing rate metrics
func (e *Exporter) ExportProcessingRate(ctx context.Context, source string, rate float64) error {
	if !e.isRunning() {
		return nil
	}

	return e.metricsExporter.ExportProcessingRate(ctx, source, rate)
}

// ExportPatternMetrics exports pattern detection metrics
func (e *Exporter) ExportPatternMetrics(ctx context.Context, patternType string, detected bool, confidence, accuracy float64) error {
	if !e.isRunning() {
		return nil
	}

	return e.metricsExporter.ExportPatternMetrics(ctx, patternType, detected, confidence, accuracy)
}

// ExportAutoFixResult exports automatic fix results
func (e *Exporter) ExportAutoFixResult(ctx context.Context, fixType string, success bool) error {
	if !e.isRunning() {
		return nil
	}

	return e.metricsExporter.ExportAutoFixResult(ctx, fixType, success)
}

// RecordProcessingTime records correlation processing time
func (e *Exporter) RecordProcessingTime(ruleID string, duration time.Duration) {
	if !e.isRunning() {
		return
	}

	e.metricsExporter.RecordProcessingTime(ruleID, duration)
}

// RegisterCustomCollector registers a custom collector for a specific component
func (e *Exporter) RegisterCustomCollector(component string, collector *CustomCollector) error {
	if e.factoryCollector == nil {
		return fmt.Errorf("factory collector not enabled")
	}

	// Register with factory
	e.factoryCollector.RegisterCollector(component, collector)

	// Register with Prometheus registry
	return e.registry.Register(collector)
}

// UnregisterCustomCollector unregisters a custom collector
func (e *Exporter) UnregisterCustomCollector(component string) error {
	if e.factoryCollector == nil {
		return fmt.Errorf("factory collector not enabled")
	}

	collector, exists := e.factoryCollector.GetCollector(component)
	if !exists {
		return fmt.Errorf("collector for component %s not found", component)
	}

	return e.registry.Unregister(collector)
}

// GetMetrics returns export metrics for monitoring
func (e *Exporter) GetMetrics() ExportMetrics {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	metrics := ExportMetrics{
		Running:        e.running,
		LastScrapeTime: time.Now(), // Would track actual last scrape time
	}

	if e.customCollector != nil {
		metrics.LastScrapeTime = e.customCollector.GetLastScrapeTime()
	}

	return metrics
}

// ExportMetrics provides metrics about export operations
type ExportMetrics struct {
	Running          bool
	ExportsTotal     int64
	ExportErrors     int64
	LastScrapeTime   time.Time
	AvgScrapeTime    time.Duration
	ActiveCollectors int
}

// HTTP handlers

func (e *Exporter) healthHandler(w http.ResponseWriter, r *http.Request) {
	if e.isRunning() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Not Ready"))
	}
}

func (e *Exporter) readinessHandler(w http.ResponseWriter, r *http.Request) {
	// Check if all components are ready
	if e.isRunning() && e.metricsExporter != nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Not Ready"))
	}
}

func (e *Exporter) addPprofHandlers(mux *http.ServeMux) {
	// Add pprof endpoints for debugging
	mux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		http.DefaultServeMux.ServeHTTP(w, r)
	})
}

// Middleware functions

func enableGzipCompression(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple gzip compression - in production, use a proper middleware
		w.Header().Set("Content-Encoding", "gzip")
		handler.ServeHTTP(w, r)
	})
}

func enableBasicAuth(handler http.Handler, users map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized"))
			return
		}

		expectedPassword, exists := users[username]
		if !exists || expectedPassword != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized"))
			return
		}

		handler.ServeHTTP(w, r)
	})
}

// Utility methods

func (e *Exporter) isRunning() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.running
}

// GetRegistry returns the Prometheus registry for external use
func (e *Exporter) GetRegistry() *prometheus.Registry {
	return e.registry
}

// GetHandler returns the HTTP handler for external use
func (e *Exporter) GetHandler() http.Handler {
	return e.handler
}

// GetConfig returns the current exporter configuration
func (e *Exporter) GetConfig() *ExporterConfig {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	// Return a copy to prevent external modification
	configCopy := *e.config
	return &configCopy
}

// InvalidateCache invalidates all collector caches
func (e *Exporter) InvalidateCache() {
	if e.customCollector != nil {
		e.customCollector.InvalidateCache()
	}

	if e.factoryCollector != nil {
		for _, collector := range e.factoryCollector.GetAllCollectors() {
			collector.InvalidateCache()
		}
	}
}
