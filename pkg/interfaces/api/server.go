package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"github.com/yairfalse/tapio/pkg/intelligence/analysis"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Server provides HTTP API for correlation queries
type Server struct {
	router          *mux.Router
	aggregator      AggregatorInterface
	analysisEngine  *analysis.Engine
	logger          *zap.Logger
	instrumentation *telemetry.APIInstrumentation
	config          Config
}

// Config holds API server configuration
type Config struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxRequestSize  int64
	EnableCORS      bool
	AllowedOrigins  []string
	EnableMetrics   bool
	MetricsEndpoint string
}

// DefaultConfig returns default API configuration
func DefaultConfig() Config {
	return Config{
		Port:            8080,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxRequestSize:  10 * 1024 * 1024, // 10MB
		EnableCORS:      true,
		AllowedOrigins:  []string{"*"},
		EnableMetrics:   true,
		MetricsEndpoint: "/metrics",
	}
}

// NewServer creates a new API server
func NewServer(
	aggregator AggregatorInterface,
	instrumentation *telemetry.APIInstrumentation,
	logger *zap.Logger,
	config Config,
) (*Server, error) {
	if aggregator == nil {
		return nil, fmt.Errorf("aggregator is required")
	}
	if instrumentation == nil {
		return nil, fmt.Errorf("instrumentation is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Create analysis engine
	analysisEngine := analysis.NewEngine(logger)

	s := &Server{
		router:          mux.NewRouter(),
		aggregator:      aggregator,
		analysisEngine:  analysisEngine,
		logger:          logger,
		instrumentation: instrumentation,
		config:          config,
	}

	// Setup routes
	s.setupRoutes()

	// Setup middleware
	s.setupMiddleware()

	return s, nil
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// API v1 routes
	v1 := s.router.PathPrefix("/api/v1").Subrouter()

	// Correlation endpoints
	v1.HandleFunc("/why/{resource_type}/{namespace}/{name}", s.handleWhy).Methods("GET")
	v1.HandleFunc("/correlations", s.handleListCorrelations).Methods("GET")
	v1.HandleFunc("/correlations/{id}", s.handleGetCorrelation).Methods("GET")
	v1.HandleFunc("/correlations/{id}/feedback", s.handleCorrelationFeedback).Methods("POST")

	// Analysis endpoints
	v1.HandleFunc("/analysis/event/{event_id}", s.handleAnalyzeEvent).Methods("POST")
	v1.HandleFunc("/patterns/detect", s.handleDetectPatterns).Methods("GET")
	v1.HandleFunc("/confidence/calculate", s.handleCalculateConfidence).Methods("POST")
	v1.HandleFunc("/analysis/history", s.handleAnalysisHistory).Methods("GET")

	// Health and metrics
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.router.HandleFunc("/ready", s.handleReady).Methods("GET")

	// Documentation
	s.setupDocsRoutes()
}

// setupMiddleware configures middleware
func (s *Server) setupMiddleware() {
	// Request size limiting
	s.router.Use(s.limitRequestSize)

	// Request ID and tracing
	s.router.Use(s.tracingMiddleware)

	// Logging
	s.router.Use(s.loggingMiddleware)

	// CORS if enabled
	if s.config.EnableCORS {
		s.router.Use(s.corsMiddleware)
	}

	// Recovery from panics
	s.router.Use(s.recoveryMiddleware)
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.config.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	s.logger.Info("Starting API server",
		zap.String("addr", addr),
		zap.Bool("cors", s.config.EnableCORS),
		zap.Bool("metrics", s.config.EnableMetrics))

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		s.logger.Info("Shutting down API server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}
}

// handleWhy handles GET /api/v1/why/{resource_type}/{namespace}/{name}
func (s *Server) handleWhy(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.StartSpan(r.Context(), "handleWhy")
	defer span.End()

	vars := mux.Vars(r)
	resourceType := vars["resource_type"]
	namespace := vars["namespace"]
	name := vars["name"]

	span.SetAttributes(
		attribute.String("resource.type", resourceType),
		attribute.String("resource.namespace", namespace),
		attribute.String("resource.name", name),
	)

	// Record metric
	s.instrumentation.RecordAPICall(ctx, "why", resourceType)

	// Query correlations
	query := aggregator.CorrelationQuery{
		ResourceType: resourceType,
		Namespace:    namespace,
		Name:         name,
		TimeWindow:   24 * time.Hour, // Default to last 24 hours
	}

	// Parse optional query params
	if window := r.URL.Query().Get("time_window"); window != "" {
		if duration, err := time.ParseDuration(window); err == nil {
			query.TimeWindow = duration
		}
	}

	result, err := s.aggregator.QueryCorrelations(ctx, query)
	if err != nil {
		s.handleError(w, r, err, span)
		return
	}

	s.respondJSON(w, http.StatusOK, result)
}

// handleListCorrelations handles GET /api/v1/correlations
func (s *Server) handleListCorrelations(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.StartSpan(r.Context(), "handleListCorrelations")
	defer span.End()

	// Record metric
	s.instrumentation.RecordAPICall(ctx, "list_correlations", "")

	// Parse query parameters
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := parseInt(l, 1, 1000); err == nil {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := parseInt(o, 0, 10000); err == nil {
			offset = parsed
		}
	}

	// Query correlations
	results, err := s.aggregator.ListCorrelations(ctx, limit, offset)
	if err != nil {
		s.handleError(w, r, err, span)
		return
	}

	s.respondJSON(w, http.StatusOK, results)
}

// handleGetCorrelation handles GET /api/v1/correlations/{id}
func (s *Server) handleGetCorrelation(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.StartSpan(r.Context(), "handleGetCorrelation")
	defer span.End()

	vars := mux.Vars(r)
	id := vars["id"]

	span.SetAttributes(attribute.String("correlation.id", id))

	// Record metric
	s.instrumentation.RecordAPICall(ctx, "get_correlation", "")

	result, err := s.aggregator.GetCorrelation(ctx, id)
	if err != nil {
		s.handleError(w, r, err, span)
		return
	}

	s.respondJSON(w, http.StatusOK, result)
}

// handleCorrelationFeedback handles POST /api/v1/correlations/{id}/feedback
func (s *Server) handleCorrelationFeedback(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.StartSpan(r.Context(), "handleCorrelationFeedback")
	defer span.End()

	vars := mux.Vars(r)
	id := vars["id"]

	span.SetAttributes(attribute.String("correlation.id", id))

	// Parse feedback
	var feedback aggregator.CorrelationFeedback
	if err := json.NewDecoder(r.Body).Decode(&feedback); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid feedback format")
		return
	}

	// Validate feedback
	if err := validateFeedback(&feedback); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Record metric
	s.instrumentation.RecordFeedback(ctx, feedback.Useful)

	// Submit feedback
	if err := s.aggregator.SubmitFeedback(ctx, id, feedback); err != nil {
		s.handleError(w, r, err, span)
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"status": "accepted",
		"id":     id,
	})
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	}

	s.respondJSON(w, http.StatusOK, health)
}

// handleReady handles GET /ready
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check if aggregator is ready
	if err := s.aggregator.Health(r.Context()); err != nil {
		s.respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"error":  err.Error(),
		})
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

// Helper methods

func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode response", zap.Error(err))
	}
}

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]string{
		"error": message,
	})
}

func (s *Server) handleError(w http.ResponseWriter, r *http.Request, err error, span trace.Span) {
	s.logger.Error("Request failed",
		zap.Error(err),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method))

	span.RecordError(err)

	// Determine status code based on error type
	status := http.StatusInternalServerError
	message := "Internal server error"

	// Check for specific error types
	if err == aggregator.ErrNotFound {
		status = http.StatusNotFound
		message = "Resource not found"
	}

	s.respondError(w, status, message)
}

// parseInt parses integer with bounds checking
func parseInt(s string, min, max int) (int, error) {
	var val int
	if _, err := fmt.Sscanf(s, "%d", &val); err != nil {
		return 0, err
	}
	if val < min || val > max {
		return 0, fmt.Errorf("value %d out of range [%d, %d]", val, min, max)
	}
	return val, nil
}

// validateFeedback validates correlation feedback
func validateFeedback(f *aggregator.CorrelationFeedback) error {
	if f.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if f.Comment != "" && len(f.Comment) > 1000 {
		return fmt.Errorf("comment too long (max 1000 chars)")
	}
	return nil
}
