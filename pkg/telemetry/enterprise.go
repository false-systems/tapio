package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"


	"github.com/yairfalse/tapio/pkg/resilience"
)

// EnterpriseServer provides enterprise-grade OpenTelemetry HTTP endpoints
type EnterpriseServer struct {
	exporter      *OpenTelemetryExporter
	spanManager   *SpanManager
	healthChecker *resilience.HealthChecker
	
	// HTTP server components
	mux    *http.ServeMux
	server *http.Server
	
	// Enterprise features
	authenticator  Authenticator
	rateLimiter    RateLimiter
	accessLogger   AccessLogger
	metricsHandler *MetricsHandler
	
	// Configuration
	config EnterpriseConfig
}

// EnterpriseConfig configures the enterprise server
type EnterpriseConfig struct {
	ListenAddr      string
	TLSEnabled      bool
	CertFile        string
	KeyFile         string
	AuthEnabled     bool
	RateLimitRPS    int
	EnableMetrics   bool
	EnableTraces    bool
	CORS            CORSConfig
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
}

// CORSConfig configures CORS settings
type CORSConfig struct {
	Enabled          bool
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// Authenticator handles authentication
type Authenticator interface {
	Authenticate(r *http.Request) (*AuthContext, error)
}

// AuthContext contains authentication information
type AuthContext struct {
	UserID   string
	TenantID string
	Scopes   []string
}

// RateLimiter handles rate limiting
type RateLimiter interface {
	Allow(clientID string) bool
	Remaining(clientID string) int
}

// AccessLogger logs HTTP requests
type AccessLogger interface {
	Log(r *http.Request, status int, duration time.Duration)
}

// MetricsHandler handles metrics endpoints
type MetricsHandler struct {
	exporter    *OpenTelemetryExporter
	spanManager *SpanManager
}

// NewEnterpriseServer creates a new enterprise OpenTelemetry server
func NewEnterpriseServer(exporter *OpenTelemetryExporter, spanManager *SpanManager, config EnterpriseConfig) (*EnterpriseServer, error) {
	// Set defaults
	if config.ListenAddr == "" {
		config.ListenAddr = ":4317"
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 120 * time.Second
	}

	mux := http.NewServeMux()
	
	server := &http.Server{
		Addr:         config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		IdleTimeout:  config.IdleTimeout,
	}

	es := &EnterpriseServer{
		exporter:    exporter,
		spanManager: spanManager,
		mux:         mux,
		server:      server,
		config:      config,
		metricsHandler: &MetricsHandler{
			exporter:    exporter,
			spanManager: spanManager,
		},
	}

	// Setup health checker for server endpoints
	es.setupHealthChecker()
	
	// Register all endpoints
	es.registerEndpoints()

	return es, nil
}

// setupHealthChecker configures health checks for the server
func (es *EnterpriseServer) setupHealthChecker() {
	es.healthChecker = resilience.NewHealthChecker(5*time.Second, 30*time.Second)

	// Register OpenTelemetry components health check
	es.healthChecker.RegisterComponent(resilience.Component{
		Name:        "otel-exporter",
		Description: "OpenTelemetry exporter status",
		Critical:    true,
		HealthCheck: func(ctx context.Context) error {
			metrics := es.exporter.GetMetrics()
			if time.Since(metrics.LastUpdateTime) > 5*time.Minute {
				return fmt.Errorf("no telemetry updates for %v", time.Since(metrics.LastUpdateTime))
			}
			return nil
		},
	})

	// Register span manager health check
	es.healthChecker.RegisterComponent(resilience.Component{
		Name:        "span-manager",
		Description: "Span manager status",
		Critical:    true,
		HealthCheck: func(ctx context.Context) error {
			metrics := es.spanManager.GetMetrics()
			if metrics.SpansFailed > 0 && metrics.SpansFailed > metrics.SpansExported/10 {
				return fmt.Errorf("high span failure rate: %d failed vs %d exported", 
					metrics.SpansFailed, metrics.SpansExported)
			}
			return nil
		},
	})

	// Register OTLP collector connectivity
	es.healthChecker.RegisterComponent(resilience.Component{
		Name:        "otlp-collector",
		Description: "OTLP collector connectivity",
		Critical:    false,
		HealthCheck: func(ctx context.Context) error {
			return es.exporter.pingOTLPEndpoint(ctx)
		},
	})
}

// registerEndpoints registers all HTTP endpoints
func (es *EnterpriseServer) registerEndpoints() {
	// Apply middleware chain
	middlewares := []Middleware{
		es.corsMiddleware,
		es.authMiddleware,
		es.rateLimitMiddleware,
		es.loggingMiddleware,
		es.circuitBreakerMiddleware,
	}

	// OpenTelemetry Protocol endpoints
	if es.config.EnableTraces {
		es.mux.Handle("/v1/traces", es.applyMiddlewares(http.HandlerFunc(es.handleTraces), middlewares...))
		es.mux.Handle("/v1/trace", es.applyMiddlewares(http.HandlerFunc(es.handleTraces), middlewares...)) // Alternative path
	}

	if es.config.EnableMetrics {
		es.mux.Handle("/v1/metrics", es.applyMiddlewares(http.HandlerFunc(es.handleMetrics), middlewares...))
	}

	// Health and status endpoints
	es.mux.Handle("/health", http.HandlerFunc(es.handleHealth))
	es.mux.Handle("/health/live", http.HandlerFunc(es.handleLiveness))
	es.mux.Handle("/health/ready", http.HandlerFunc(es.handleReadiness))
	
	// Info and discovery endpoints
	es.mux.Handle("/info", http.HandlerFunc(es.handleInfo))
	es.mux.Handle("/status", http.HandlerFunc(es.handleStatus))
	es.mux.Handle("/metrics/internal", http.HandlerFunc(es.handleInternalMetrics))
	
	// Configuration endpoints
	es.mux.Handle("/config", es.applyMiddlewares(http.HandlerFunc(es.handleConfig), middlewares...))
	es.mux.Handle("/endpoints", http.HandlerFunc(es.handleEndpoints))
}

// Middleware type for HTTP middleware
type Middleware func(http.Handler) http.Handler

// applyMiddlewares applies middleware chain to a handler
func (es *EnterpriseServer) applyMiddlewares(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// corsMiddleware handles CORS headers
func (es *EnterpriseServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !es.config.CORS.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		cors := es.config.CORS
		
		// Set CORS headers
		if len(cors.AllowedOrigins) > 0 {
			w.Header().Set("Access-Control-Allow-Origin", cors.AllowedOrigins[0])
		}
		if len(cors.AllowedMethods) > 0 {
			w.Header().Set("Access-Control-Allow-Methods", joinStrings(cors.AllowedMethods, ", "))
		}
		if len(cors.AllowedHeaders) > 0 {
			w.Header().Set("Access-Control-Allow-Headers", joinStrings(cors.AllowedHeaders, ", "))
		}
		if len(cors.ExposedHeaders) > 0 {
			w.Header().Set("Access-Control-Expose-Headers", joinStrings(cors.ExposedHeaders, ", "))
		}
		if cors.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if cors.MaxAge > 0 {
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cors.MaxAge))
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware handles authentication
func (es *EnterpriseServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !es.config.AuthEnabled || es.authenticator == nil {
			next.ServeHTTP(w, r)
			return
		}

		authCtx, err := es.authenticator.Authenticate(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add auth context to request
		ctx := context.WithValue(r.Context(), "auth", authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimitMiddleware handles rate limiting
func (es *EnterpriseServer) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if es.rateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		clientID := getClientID(r)
		if !es.rateLimiter.Allow(clientID) {
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(es.rateLimiter.Remaining(clientID)))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware handles access logging
func (es *EnterpriseServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if es.accessLogger == nil {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		wrapper := &responseWrapper{ResponseWriter: w, statusCode: 200}
		
		next.ServeHTTP(wrapper, r)
		
		es.accessLogger.Log(r, wrapper.statusCode, time.Since(start))
	})
}

// circuitBreakerMiddleware protects endpoints with circuit breakers
func (es *EnterpriseServer) circuitBreakerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !es.exporter.circuitBreaker.CanExecute() {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		
		// Execute the handler
		next.ServeHTTP(w, r)
		es.exporter.circuitBreaker.RecordSuccess()
	})
}

// handleTraces handles OTLP trace requests
func (es *EnterpriseServer) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	
	// For now, acknowledge receipt
	// In a full implementation, this would parse OTLP format and create spans
	w.WriteHeader(http.StatusOK)
	
	// Create a span for the trace ingestion
	span, err := es.spanManager.CreateSpan(r.Context(), "tapio.traces.ingest")
	if err != nil {
		fmt.Printf("Failed to create ingestion span: %v\n", err)
		return
	}
	defer es.spanManager.FinishSpan(span)
	
	span.SetAttribute("http.method", r.Method)
	span.SetAttribute("http.path", r.URL.Path)
	span.SetAttribute("content.length", r.ContentLength)
}

// handleMetrics handles OTLP metric requests
func (es *EnterpriseServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	
	// Create a span for the metric ingestion
	span, err := es.spanManager.CreateSpan(r.Context(), "tapio.metrics.ingest")
	if err != nil {
		fmt.Printf("Failed to create metric ingestion span: %v\n", err)
		return
	}
	defer es.spanManager.FinishSpan(span)
	
	span.SetAttribute("http.method", r.Method)
	span.SetAttribute("http.path", r.URL.Path)
}

// handleHealth handles comprehensive health checks
func (es *EnterpriseServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	results := es.healthChecker.CheckAll(r.Context())
	overallStatus := es.healthChecker.GetStatus(r.Context())
	
	response := HealthResponse{
		Status:    string(overallStatus),
		Timestamp: time.Now(),
		Service:   "tapio-opentelemetry-exporter",
		Version:   "1.0.0",
		Checks:    make(map[string]CheckResult),
	}
	
	for _, result := range results {
		response.Checks[result.Name] = CheckResult{
			Status:   string(result.Status),
			Message:  result.Message,
			Error:    result.Error,
			Duration: result.Duration,
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	
	statusCode := http.StatusOK
	if overallStatus != resilience.HealthStatusHealthy {
		statusCode = http.StatusServiceUnavailable
	}
	w.WriteHeader(statusCode)
	
	json.NewEncoder(w).Encode(response)
}

// handleLiveness handles Kubernetes liveness probe
func (es *EnterpriseServer) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "alive",
		"timestamp": time.Now(),
	})
}

// handleReadiness handles Kubernetes readiness probe
func (es *EnterpriseServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	status := es.healthChecker.GetStatus(r.Context())
	
	w.Header().Set("Content-Type", "application/json")
	
	if status == resilience.HealthStatusHealthy {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ready",
			"timestamp": time.Now(),
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not ready",
			"timestamp": time.Now(),
			"reason": string(status),
		})
	}
}

// handleInfo handles service information
func (es *EnterpriseServer) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := ServiceInfo{
		Service:     "tapio-opentelemetry-exporter",
		Version:     "1.0.0",
		Description: "Tapio Kubernetes intelligence OpenTelemetry exporter",
		Endpoints: []string{
			"/v1/traces",
			"/v1/metrics", 
			"/health",
			"/health/live",
			"/health/ready",
			"/info",
			"/status",
			"/metrics/internal",
			"/config",
			"/endpoints",
		},
		Features: Features{
			CircuitBreaker: true,
			RateLimiting:   es.rateLimiter != nil,
			Authentication: es.config.AuthEnabled,
			CORS:          es.config.CORS.Enabled,
			TLS:           es.config.TLSEnabled,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(info)
}

// handleStatus handles detailed status information
func (es *EnterpriseServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	exporterMetrics := es.exporter.GetMetrics()
	spanMetrics := es.spanManager.GetMetrics()
	
	status := StatusResponse{
		Timestamp: time.Now(),
		Uptime:    time.Since(exporterMetrics.LastUpdateTime),
		Exporter:  exporterMetrics,
		SpanManager: spanMetrics,
		CircuitBreaker: CircuitBreakerStatus{
			State:       "closed", // simplified for now
			// Metrics:     nil, // simplified for now
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// handleInternalMetrics handles internal metrics in Prometheus format
func (es *EnterpriseServer) handleInternalMetrics(w http.ResponseWriter, r *http.Request) {
	es.metricsHandler.ServeMetrics(w, r)
}

// handleConfig handles configuration information
func (es *EnterpriseServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	config := ConfigResponse{
		OTLPEndpoint:   es.exporter.config.OTLPEndpoint,
		ServiceName:    es.exporter.config.ServiceName,
		ServiceVersion: es.exporter.config.ServiceVersion,
		EnableTraces:   es.config.EnableTraces,
		EnableMetrics:  es.config.EnableMetrics,
		BatchSize:      es.exporter.config.BatchSize,
		BatchTimeout:   es.exporter.config.BatchTimeout,
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(config)
}

// handleEndpoints handles endpoint discovery
func (es *EnterpriseServer) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints := EndpointsResponse{
		OTLP: OTLPEndpoints{
			Traces:  fmt.Sprintf("http://%s/v1/traces", es.config.ListenAddr),
			Metrics: fmt.Sprintf("http://%s/v1/metrics", es.config.ListenAddr),
		},
		Management: ManagementEndpoints{
			Health:    fmt.Sprintf("http://%s/health", es.config.ListenAddr),
			Info:      fmt.Sprintf("http://%s/info", es.config.ListenAddr),
			Status:    fmt.Sprintf("http://%s/status", es.config.ListenAddr),
			Metrics:   fmt.Sprintf("http://%s/metrics/internal", es.config.ListenAddr),
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(endpoints)
}

// Start starts the enterprise server
func (es *EnterpriseServer) Start() error {
	fmt.Printf("🚀 OpenTelemetry enterprise server starting on %s\n", es.config.ListenAddr)
	fmt.Printf("📡 OTLP traces endpoint: http://%s/v1/traces\n", es.config.ListenAddr)
	fmt.Printf("📊 OTLP metrics endpoint: http://%s/v1/metrics\n", es.config.ListenAddr)
	fmt.Printf("💚 Health endpoint: http://%s/health\n", es.config.ListenAddr)
	fmt.Printf("ℹ️  Info endpoint: http://%s/info\n", es.config.ListenAddr)

	// Start health checks
	es.healthChecker.StartBackgroundChecks(context.Background())

	if es.config.TLSEnabled {
		return es.server.ListenAndServeTLS(es.config.CertFile, es.config.KeyFile)
	}
	return es.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (es *EnterpriseServer) Shutdown(ctx context.Context) error {
	fmt.Println("🛑 Shutting down OpenTelemetry enterprise server...")
	return es.server.Shutdown(ctx)
}

// Response types
type HealthResponse struct {
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Service   string                 `json:"service"`
	Version   string                 `json:"version"`
	Checks    map[string]CheckResult `json:"checks"`
}

type CheckResult struct {
	Status   string        `json:"status"`
	Message  string        `json:"message,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

type ServiceInfo struct {
	Service     string   `json:"service"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Endpoints   []string `json:"endpoints"`
	Features    Features `json:"features"`
}

type Features struct {
	CircuitBreaker bool `json:"circuit_breaker"`
	RateLimiting   bool `json:"rate_limiting"`
	Authentication bool `json:"authentication"`
	CORS          bool `json:"cors"`
	TLS           bool `json:"tls"`
}

type StatusResponse struct {
	Timestamp      time.Time                      `json:"timestamp"`
	Uptime         time.Duration                  `json:"uptime"`
	Exporter       ExporterMetrics                `json:"exporter"`
	SpanManager    SpanManagerMetrics             `json:"span_manager"`
	CircuitBreaker CircuitBreakerStatus           `json:"circuit_breaker"`
}

type CircuitBreakerStatus struct {
	State   string             `json:"state"`
	Metrics resilience.Metrics `json:"metrics"`
}

type ConfigResponse struct {
	OTLPEndpoint   string        `json:"otlp_endpoint"`
	ServiceName    string        `json:"service_name"`
	ServiceVersion string        `json:"service_version"`
	EnableTraces   bool          `json:"enable_traces"`
	EnableMetrics  bool          `json:"enable_metrics"`
	BatchSize      int           `json:"batch_size"`
	BatchTimeout   time.Duration `json:"batch_timeout"`
}

type EndpointsResponse struct {
	OTLP       OTLPEndpoints       `json:"otlp"`
	Management ManagementEndpoints `json:"management"`
}

type OTLPEndpoints struct {
	Traces  string `json:"traces"`
	Metrics string `json:"metrics"`
}

type ManagementEndpoints struct {
	Health  string `json:"health"`
	Info    string `json:"info"`
	Status  string `json:"status"`
	Metrics string `json:"metrics"`
}

// Helper types and functions
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func getClientID(r *http.Request) string {
	// Try various methods to identify client
	if clientID := r.Header.Get("X-Client-ID"); clientID != "" {
		return clientID
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		return auth[:min(len(auth), 10)] // Use first 10 chars of auth header
	}
	return r.RemoteAddr
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ServeMetrics serves internal metrics in Prometheus format
func (mh *MetricsHandler) ServeMetrics(w http.ResponseWriter, r *http.Request) {
	_ = mh.exporter.GetMetrics()
	spanMetrics := mh.spanManager.GetMetrics()
	
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	
	// Export metrics in Prometheus format
	fmt.Fprintf(w, "# HELP tapio_otel_spans_created_total Total number of spans created\n")
	fmt.Fprintf(w, "# TYPE tapio_otel_spans_created_total counter\n")
	fmt.Fprintf(w, "tapio_otel_spans_created_total %d\n", spanMetrics.SpansCreated)
	
	fmt.Fprintf(w, "# HELP tapio_otel_spans_exported_total Total number of spans exported\n")
	fmt.Fprintf(w, "# TYPE tapio_otel_spans_exported_total counter\n")
	fmt.Fprintf(w, "tapio_otel_spans_exported_total %d\n", spanMetrics.SpansExported)
	
	fmt.Fprintf(w, "# HELP tapio_otel_spans_failed_total Total number of spans failed\n")
	fmt.Fprintf(w, "# TYPE tapio_otel_spans_failed_total counter\n")
	fmt.Fprintf(w, "tapio_otel_spans_failed_total %d\n", spanMetrics.SpansFailed)
	
	fmt.Fprintf(w, "# HELP tapio_otel_active_spans Current number of active spans\n")
	fmt.Fprintf(w, "# TYPE tapio_otel_active_spans gauge\n")
	fmt.Fprintf(w, "tapio_otel_active_spans %d\n", spanMetrics.ActiveSpans)
	
	fmt.Fprintf(w, "# HELP tapio_otel_circuit_breaker_state Circuit breaker state (0=closed, 1=open, 2=half-open)\n")
	fmt.Fprintf(w, "# TYPE tapio_otel_circuit_breaker_state gauge\n")
	cbState := 0 // simplified for now (always closed)
	fmt.Fprintf(w, "tapio_otel_circuit_breaker_state %d\n", cbState)
}