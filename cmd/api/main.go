package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yairfalse/tapio/pkg/domain"
	neo4jint "github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"github.com/yairfalse/tapio/pkg/intelligence/behavior"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// APIServer represents the Tapio API server with full OTEL integration
// Level 4 - Can import from all lower levels
type APIServer struct {
	logger *zap.Logger
	config *APIConfig

	// Neo4j client
	neo4jClient *neo4jint.Client

	// Intelligence engine for correlations
	behaviorEngine *behavior.Engine

	// HTTP components
	router *chi.Mux
	server *http.Server

	// OTEL instrumentation - REQUIRED fields per CLAUDE.md
	tracer            trace.Tracer
	requestsTotal     metric.Int64Counter
	requestDuration   metric.Float64Histogram
	queriesTotal      metric.Int64Counter
	queriesErrors     metric.Int64Counter
	correlationsTotal metric.Int64Counter
	activeConnections metric.Int64UpDownCounter

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// APIConfig holds API server configuration
type APIConfig struct {
	Port    string
	Neo4j   *neo4jint.Config
	LogPath string
}

// ObservationQueryRequest represents a query for observations
type ObservationQueryRequest struct {
	CorrelationKeys map[string]string `json:"correlation_keys"`
	TimeRange       *TimeRange        `json:"time_range,omitempty"`
	Sources         []string          `json:"sources,omitempty"`
	Types           []string          `json:"types,omitempty"`
	Limit           int               `json:"limit,omitempty"`
}

// TimeRange represents a time range for queries
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ObservationQueryResponse contains query results
type ObservationQueryResponse struct {
	Observations []*domain.ObservationEvent `json:"observations"`
	Total        int                        `json:"total"`
	Query        *ObservationQueryRequest   `json:"query"`
	Duration     string                     `json:"duration"`
}

// CorrelationRequest requests behavior analysis
type CorrelationRequest struct {
	ObservationID string                   `json:"observation_id,omitempty"`
	Observation   *domain.ObservationEvent `json:"observation,omitempty"`
	TimeWindow    string                   `json:"time_window,omitempty"`
}

// CorrelationResponse contains correlation analysis results
type CorrelationResponse struct {
	Prediction    *domain.BehaviorPrediction `json:"prediction,omitempty"`
	Result        *domain.PredictionResult   `json:"result,omitempty"`
	RelatedEvents []*domain.ObservationEvent `json:"related_events"`
	Pattern       string                     `json:"pattern,omitempty"`
	Confidence    float64                    `json:"confidence"`
	Duration      string                     `json:"duration"`
}

// WhyResponse provides root cause analysis (legacy endpoint)
type WhyResponse struct {
	Pod         string          `json:"pod"`
	Namespace   string          `json:"namespace"`
	RootCause   string          `json:"root_cause"`
	Timeline    []TimelineEvent `json:"timeline"`
	Impact      []string        `json:"impact"`
	Suggestions []string        `json:"suggestions"`
}

// TimelineEvent represents an event in the timeline
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Event     string    `json:"event"`
	Details   string    `json:"details"`
}

// ImpactResponse provides impact analysis (legacy endpoint)
type ImpactResponse struct {
	Service       string   `json:"service"`
	Namespace     string   `json:"namespace"`
	AffectedPods  []string `json:"affected_pods"`
	AffectedApps  []string `json:"affected_apps"`
	DownstreamDep []string `json:"downstream_dependencies"`
	Severity      string   `json:"severity"`
}

// HealthResponse provides health status
type HealthResponse struct {
	Status      string            `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Services    map[string]string `json:"services"`
	Version     string            `json:"version"`
	Uptime      string            `json:"uptime"`
	Connections int               `json:"active_connections"`
}

// NewAPIServer creates a new API server with full OTEL integration
func NewAPIServer(logger *zap.Logger) (*APIServer, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Load configuration
	config := &APIConfig{
		Port: getEnvOrDefault("PORT", "8080"),
		Neo4j: &neo4jint.Config{
			URI:      getEnvOrDefault("NEO4J_URI", "bolt://localhost:7687"),
			Username: getEnvOrDefault("NEO4J_USER", "neo4j"),
			Password: getEnvOrDefault("NEO4J_PASSWORD", "password"),
			Database: getEnvOrDefault("NEO4J_DATABASE", "neo4j"),
		},
		LogPath: getEnvOrDefault("LOG_PATH", "/tmp/tapio-api.log"),
	}

	ctx, cancel := context.WithCancel(context.Background())

	server := &APIServer{
		logger: logger,
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize OTEL instrumentation - MANDATORY per CLAUDE.md
	if err := server.initOTEL(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize OTEL: %w", err)
	}

	// Initialize Neo4j client
	if err := server.initNeo4j(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize Neo4j: %w", err)
	}

	// Initialize behavior engine
	if err := server.initBehaviorEngine(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize behavior engine: %w", err)
	}

	// Setup HTTP router and middleware
	server.setupRouter()

	logger.Info("API server initialized successfully",
		zap.String("port", config.Port),
		zap.String("neo4j_uri", config.Neo4j.URI),
	)

	return server, nil
}

// getEnvOrDefault returns environment variable value or default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// initOTEL initializes OpenTelemetry instrumentation
func (s *APIServer) initOTEL() error {
	// Initialize OTEL components - MANDATORY pattern per CLAUDE.md
	name := "api-server"
	s.tracer = otel.Tracer(name)
	meter := otel.Meter(name)

	var err error

	// Create metrics with descriptive names and descriptions
	s.requestsTotal, err = meter.Int64Counter(
		fmt.Sprintf("%s_requests_total", name),
		metric.WithDescription("Total HTTP requests processed by API server"),
	)
	if err != nil {
		s.logger.Warn("Failed to create requests counter", zap.Error(err))
	}

	s.requestDuration, err = meter.Float64Histogram(
		fmt.Sprintf("%s_request_duration_ms", name),
		metric.WithDescription("Request processing duration in milliseconds"),
	)
	if err != nil {
		s.logger.Warn("Failed to create request duration histogram", zap.Error(err))
	}

	s.queriesTotal, err = meter.Int64Counter(
		fmt.Sprintf("%s_queries_total", name),
		metric.WithDescription("Total Neo4j queries executed"),
	)
	if err != nil {
		s.logger.Warn("Failed to create queries counter", zap.Error(err))
	}

	s.queriesErrors, err = meter.Int64Counter(
		fmt.Sprintf("%s_queries_errors_total", name),
		metric.WithDescription("Total Neo4j query errors"),
	)
	if err != nil {
		s.logger.Warn("Failed to create query errors counter", zap.Error(err))
	}

	s.correlationsTotal, err = meter.Int64Counter(
		fmt.Sprintf("%s_correlations_total", name),
		metric.WithDescription("Total correlation requests processed"),
	)
	if err != nil {
		s.logger.Warn("Failed to create correlations counter", zap.Error(err))
	}

	s.activeConnections, err = meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_connections", name),
		metric.WithDescription("Number of active HTTP connections"),
	)
	if err != nil {
		s.logger.Warn("Failed to create active connections counter", zap.Error(err))
	}

	return nil
}

// initNeo4j initializes Neo4j client
func (s *APIServer) initNeo4j() error {
	client, err := neo4jint.NewClient(*s.config.Neo4j, s.logger)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	s.neo4jClient = client
	return nil
}

// initBehaviorEngine initializes the behavior correlation engine
func (s *APIServer) initBehaviorEngine() error {
	engine, err := behavior.NewEngine(s.logger)
	if err != nil {
		return fmt.Errorf("failed to create behavior engine: %w", err)
	}
	s.behaviorEngine = engine
	return nil
}

// setupRouter configures HTTP router and middleware
func (s *APIServer) setupRouter() {
	s.router = chi.NewRouter()

	// Apply middleware with OTEL integration
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
	s.router.Use(s.corsMiddleware)

	// API v1 routes - ObservationEvent focused
	s.router.Route("/api/v1", func(api chi.Router) {
		// Observation query endpoints
		api.Post("/observations/query", s.handleObservationQuery)
		api.Options("/observations/query", s.handleOptions)
		api.Get("/observations/{id}", s.handleObservationGet)
		api.Options("/observations/{id}", s.handleOptions)
		api.Get("/observations", s.handleObservationsList)
		api.Options("/observations", s.handleOptions)

		// Correlation and behavior analysis endpoints
		api.Post("/correlations/analyze", s.handleCorrelationAnalyze)
		api.Options("/correlations/analyze", s.handleOptions)
		api.Get("/correlations/patterns", s.handlePatternsList)
		api.Options("/correlations/patterns", s.handleOptions)

		// Legacy endpoints for backward compatibility
		api.Get("/why", s.handleWhy)
		api.Options("/why", s.handleOptions)
		api.Get("/impact", s.handleImpact)
		api.Options("/impact", s.handleOptions)

		// System endpoints
		api.Get("/health", s.handleHealth)
		api.Options("/health", s.handleOptions)
		api.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
			promhttp.Handler().ServeHTTP(w, r)
		})
		api.Get("/version", s.handleVersion)
		api.Options("/version", s.handleOptions)
	})
}

// handleOptions handles CORS preflight requests
func (s *APIServer) handleOptions(w http.ResponseWriter, r *http.Request) {
	// CORS headers are already set by corsMiddleware
	w.WriteHeader(http.StatusOK)
}

// handleObservationQuery handles complex observation queries
func (s *APIServer) handleObservationQuery(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_observation_query")
	defer span.End()

	start := time.Now()
	defer func() {
		duration := time.Since(start).Milliseconds()
		if s.requestDuration != nil {
			s.requestDuration.Record(ctx, float64(duration), metric.WithAttributes(
				attribute.String("endpoint", "observation_query"),
				attribute.String("method", r.Method),
			))
		}
	}()

	// Record request metrics
	if s.requestsTotal != nil {
		s.requestsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "observation_query"),
			attribute.String("method", r.Method),
		))
	}

	var req ObservationQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, "Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if len(req.CorrelationKeys) == 0 {
		span.SetStatus(codes.Error, "At least one correlation key required")
		http.Error(w, "At least one correlation key is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Limit == 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	// Query observations
	observations, total, err := s.queryObservations(ctx, &req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Failed to query observations", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := &ObservationQueryResponse{
		Observations: observations,
		Total:        total,
		Query:        &req,
		Duration:     time.Since(start).String(),
	}

	span.SetAttributes(
		attribute.Int("results_count", len(observations)),
		attribute.Int("total_count", total),
		attribute.Int("query_limit", req.Limit),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleObservationGet retrieves a specific observation by ID
func (s *APIServer) handleObservationGet(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_observation_get")
	defer span.End()

	id := chi.URLParam(r, "id")

	if id == "" {
		span.SetStatus(codes.Error, "Missing observation ID")
		http.Error(w, "Observation ID is required", http.StatusBadRequest)
		return
	}

	observation, err := s.getObservationByID(ctx, id)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Failed to get observation", zap.String("id", id), zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if observation == nil {
		span.SetStatus(codes.Error, "Observation not found")
		http.Error(w, "Observation not found", http.StatusNotFound)
		return
	}

	span.SetAttributes(attribute.String("observation.id", observation.ID))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(observation)
}

// handleObservationsList lists observations with optional filters
func (s *APIServer) handleObservationsList(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_observations_list")
	defer span.End()

	// Parse query parameters
	query := r.URL.Query()
	req := ObservationQueryRequest{
		CorrelationKeys: make(map[string]string),
		Limit:           100,
	}

	// Parse correlation keys from query parameters
	if pid := query.Get("pid"); pid != "" {
		req.CorrelationKeys["pid"] = pid
	}
	if containerID := query.Get("container_id"); containerID != "" {
		req.CorrelationKeys["container_id"] = containerID
	}
	if podName := query.Get("pod_name"); podName != "" {
		req.CorrelationKeys["pod_name"] = podName
	}
	if namespace := query.Get("namespace"); namespace != "" {
		req.CorrelationKeys["namespace"] = namespace
	}
	if serviceName := query.Get("service_name"); serviceName != "" {
		req.CorrelationKeys["service_name"] = serviceName
	}
	if nodeName := query.Get("node_name"); nodeName != "" {
		req.CorrelationKeys["node_name"] = nodeName
	}

	// Parse sources and types
	if sources := query.Get("sources"); sources != "" {
		req.Sources = strings.Split(sources, ",")
	}
	if types := query.Get("types"); types != "" {
		req.Types = strings.Split(types, ",")
	}

	// Parse limit
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = limit
		}
	}

	// Parse time range
	if startStr := query.Get("start"); startStr != "" {
		if start, err := time.Parse(time.RFC3339, startStr); err == nil {
			if req.TimeRange == nil {
				req.TimeRange = &TimeRange{}
			}
			req.TimeRange.Start = start
		}
	}
	if endStr := query.Get("end"); endStr != "" {
		if end, err := time.Parse(time.RFC3339, endStr); err == nil {
			if req.TimeRange == nil {
				req.TimeRange = &TimeRange{}
			}
			req.TimeRange.End = end
		}
	}

	// Validate that at least one filter is provided
	if len(req.CorrelationKeys) == 0 && len(req.Sources) == 0 && len(req.Types) == 0 && req.TimeRange == nil {
		span.SetStatus(codes.Error, "At least one filter required")
		http.Error(w, "At least one filter parameter is required", http.StatusBadRequest)
		return
	}

	// Set max limit
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	// Query observations
	observations, total, err := s.queryObservations(ctx, &req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Failed to list observations", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := &ObservationQueryResponse{
		Observations: observations,
		Total:        total,
		Query:        &req,
		Duration:     time.Since(time.Now()).String(),
	}

	span.SetAttributes(
		attribute.Int("results_count", len(observations)),
		attribute.Int("total_count", total),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCorrelationAnalyze performs behavior analysis on observations
func (s *APIServer) handleCorrelationAnalyze(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_correlation_analyze")
	defer span.End()

	start := time.Now()
	defer func() {
		if s.correlationsTotal != nil {
			s.correlationsTotal.Add(ctx, 1)
		}
	}()

	var req CorrelationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, "Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var observation *domain.ObservationEvent
	var err error

	// Get observation to analyze
	if req.Observation != nil {
		observation = req.Observation
	} else if req.ObservationID != "" {
		observation, err = s.getObservationByID(ctx, req.ObservationID)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			s.logger.Error("Failed to get observation for analysis", zap.String("id", req.ObservationID), zap.Error(err))
			http.Error(w, "Failed to get observation", http.StatusInternalServerError)
			return
		}
		if observation == nil {
			span.SetStatus(codes.Error, "Observation not found")
			http.Error(w, "Observation not found", http.StatusNotFound)
			return
		}
	} else {
		span.SetStatus(codes.Error, "Either observation or observation_id required")
		http.Error(w, "Either observation or observation_id is required", http.StatusBadRequest)
		return
	}

	// Process through behavior engine
	result, err := s.behaviorEngine.Process(ctx, observation)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Failed to analyze observation", zap.String("observation_id", observation.ID), zap.Error(err))
		http.Error(w, "Correlation analysis failed", http.StatusInternalServerError)
		return
	}

	// Build response
	response := &CorrelationResponse{
		Result:   result,
		Duration: time.Since(start).String(),
	}

	if result != nil && result.Prediction != nil {
		// Convert domain.Prediction to domain.BehaviorPrediction for response
		behaviorPrediction := &domain.BehaviorPrediction{
			ID:               result.Prediction.ID,
			PatternID:        result.Prediction.PatternID,
			PatternName:      result.Prediction.PatternName,
			GeneratedAt:      result.Prediction.CreatedAt,
			Confidence:       result.Prediction.Confidence,
			TimeHorizon:      result.Prediction.TimeHorizon,
			PotentialImpacts: []string{result.Prediction.Impact},
			RecommendedActions: func() []string {
				if result.Prediction.Remediation != nil {
					return result.Prediction.Remediation.ManualSteps
				}
				return []string{}
			}(),
			AffectedResources: func() []string {
				resources := make([]string, len(result.Prediction.Resources))
				for i, r := range result.Prediction.Resources {
					resources[i] = fmt.Sprintf("%s:%s", r.Kind, r.Name)
				}
				return resources
			}(),
			Evidence: result.Prediction.Evidence,
			Metadata: make(map[string]string),
		}
		response.Prediction = behaviorPrediction
		response.Pattern = result.Prediction.PatternName
		response.Confidence = result.Prediction.Confidence
		// Get related events if available
		if len(result.RelatedEvents) > 0 {
			relatedEvents := make([]*domain.ObservationEvent, 0, len(result.RelatedEvents))
			for _, eventID := range result.RelatedEvents {
				if relatedEvent, err := s.getObservationByID(ctx, eventID); err == nil && relatedEvent != nil {
					relatedEvents = append(relatedEvents, relatedEvent)
				}
			}
			response.RelatedEvents = relatedEvents
		}
	}

	span.SetAttributes(
		attribute.String("observation.id", observation.ID),
		attribute.Float64("confidence", response.Confidence),
		attribute.String("pattern", response.Pattern),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handlePatternsList lists available behavior patterns
func (s *APIServer) handlePatternsList(w http.ResponseWriter, r *http.Request) {
	_, span := s.tracer.Start(r.Context(), "api.handle_patterns_list")
	defer span.End()

	patterns := s.behaviorEngine.GetPatterns()

	span.SetAttributes(attribute.Int("patterns_count", len(patterns)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"patterns": patterns,
		"count":    len(patterns),
	})
}

// handleVersion returns API version information
func (s *APIServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version":     "1.0.0",
		"api_version": "v1",
		"timestamp":   time.Now(),
	})
}

// Legacy endpoint for backward compatibility
func (s *APIServer) handleWhy(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_why_legacy")
	defer span.End()
	pod := r.URL.Query().Get("pod")
	namespace := r.URL.Query().Get("namespace")

	if pod == "" {
		span.SetStatus(codes.Error, "Pod parameter required")
		http.Error(w, "pod parameter is required", http.StatusBadRequest)
		return
	}

	if namespace == "" {
		namespace = "default"
	}

	// Use newer observation-based analysis
	response, err := s.queryWhyPodFailedModern(ctx, pod, namespace)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Error querying why pod failed", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(
		attribute.String("pod", pod),
		attribute.String("namespace", namespace),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleImpact provides service impact analysis (legacy endpoint)
func (s *APIServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_impact_legacy")
	defer span.End()

	service := r.URL.Query().Get("service")
	namespace := r.URL.Query().Get("namespace")

	if service == "" {
		span.SetStatus(codes.Error, "Service parameter required")
		http.Error(w, "service parameter is required", http.StatusBadRequest)
		return
	}

	if namespace == "" {
		namespace = "default"
	}

	// Use newer observation-based analysis
	response, err := s.queryServiceImpactModern(ctx, service, namespace)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Error querying service impact", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(
		attribute.String("service", service),
		attribute.String("namespace", namespace),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth provides comprehensive health status
func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "api.handle_health")
	defer span.End()

	services := make(map[string]string)

	// Check Neo4j connectivity
	var neo4jErr error
	if s.neo4jClient != nil {
		neo4jErr = s.neo4jClient.VerifyConnectivity(ctx)
	} else {
		neo4jErr = fmt.Errorf("Neo4j client not initialized")
	}

	if neo4jErr != nil {
		services["neo4j"] = "unhealthy"
		s.logger.Warn("Neo4j health check failed", zap.Error(neo4jErr))
	} else {
		services["neo4j"] = "healthy"
	}

	// Check behavior engine
	if s.behaviorEngine != nil {
		healthy, details := s.behaviorEngine.Health(ctx)
		if healthy {
			services["behavior_engine"] = "healthy"
		} else {
			services["behavior_engine"] = "degraded"
			s.logger.Warn("Behavior engine health check failed", zap.Any("details", details))
		}
	} else {
		services["behavior_engine"] = "unhealthy"
	}

	// Determine overall status
	status := "healthy"
	for _, serviceStatus := range services {
		if serviceStatus == "unhealthy" {
			status = "unhealthy"
			break
		} else if serviceStatus == "degraded" && status == "healthy" {
			status = "degraded"
		}
	}

	// Calculate uptime
	uptime := time.Since(time.Now().Add(-24 * time.Hour)) // Placeholder - should track actual start time

	response := HealthResponse{
		Status:      status,
		Timestamp:   time.Now(),
		Services:    services,
		Version:     "1.0.0",
		Uptime:      uptime.String(),
		Connections: 0, // TODO: Track actual connection count
	}

	span.SetAttributes(
		attribute.String("health.status", status),
		attribute.String("neo4j.status", services["neo4j"]),
		attribute.String("behavior_engine.status", services["behavior_engine"]),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Middleware functions

// loggingMiddleware logs HTTP requests with OTEL integration
func (s *APIServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create span for the request
		ctx, span := s.tracer.Start(r.Context(), "http.request")
		span.SetAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.Path),
			attribute.String("http.user_agent", r.UserAgent()),
		)
		defer span.End()

		// Update request context with span
		r = r.WithContext(ctx)

		// Track active connections
		if s.activeConnections != nil {
			s.activeConnections.Add(ctx, 1)
			defer s.activeConnections.Add(ctx, -1)
		}

		next.ServeHTTP(w, r)

		duration := time.Since(start)
		s.logger.Info("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.Duration("duration", duration),
		)
	})
}

// metricsMiddleware records request metrics
func (s *APIServer) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		next.ServeHTTP(w, r)

		duration := time.Since(start).Milliseconds()

		// Record metrics
		if s.requestsTotal != nil {
			s.requestsTotal.Add(r.Context(), 1, metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("endpoint", r.URL.Path),
			))
		}

		if s.requestDuration != nil {
			s.requestDuration.Record(r.Context(), float64(duration), metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("endpoint", r.URL.Path),
			))
		}
	})
}

// corsMiddleware handles CORS headers
func (s *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Server lifecycle methods

// Start starts the API server
func (s *APIServer) Start() error {
	_, span := s.tracer.Start(s.ctx, "api.server.start")
	defer span.End()

	s.server = &http.Server{
		Addr:         ":" + s.config.Port,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("API Server starting", zap.String("port", s.config.Port))
	span.SetAttributes(attribute.String("port", s.config.Port))

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("server failed to start: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the API server
func (s *APIServer) Stop(ctx context.Context) error {
	shutdownCtx, span := s.tracer.Start(ctx, "api.server.stop")
	defer span.End()

	s.logger.Info("API Server shutting down...")

	// Shutdown HTTP server
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		span.SetStatus(codes.Error, err.Error())
		s.logger.Error("Server shutdown error", zap.Error(err))
	}

	// Stop behavior engine
	if s.behaviorEngine != nil {
		if err := s.behaviorEngine.Stop(); err != nil {
			s.logger.Error("Failed to stop behavior engine", zap.Error(err))
		}
	}

	// Close Neo4j client
	if s.neo4jClient != nil {
		if err := s.neo4jClient.Close(shutdownCtx); err != nil {
			s.logger.Error("Failed to close Neo4j client", zap.Error(err))
		}
	}

	// Cancel context
	s.cancel()

	s.logger.Info("API Server stopped")
	return nil
}

// queryObservations queries Neo4j for observations based on correlation keys and filters
func (s *APIServer) queryObservations(ctx context.Context, req *ObservationQueryRequest) ([]*domain.ObservationEvent, int, error) {
	ctx, span := s.tracer.Start(ctx, "api.query_observations")
	defer span.End()

	if s.neo4jClient == nil {
		return nil, 0, fmt.Errorf("Neo4j client not available")
	}

	// Record query metrics
	if s.queriesTotal != nil {
		s.queriesTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("query_type", "observations"),
		))
	}

	// Build Cypher query
	var cypher strings.Builder
	params := make(map[string]interface{})

	cypher.WriteString("MATCH (o:Observation) ")

	// Build WHERE clause based on correlation keys
	var conditions []string
	for key, value := range req.CorrelationKeys {
		paramName := fmt.Sprintf("key_%s", key)
		conditions = append(conditions, fmt.Sprintf("o.%s = $%s", key, paramName))
		params[paramName] = value
	}

	// Add source filters
	if len(req.Sources) > 0 {
		conditions = append(conditions, "o.source IN $sources")
		params["sources"] = req.Sources
	}

	// Add type filters
	if len(req.Types) > 0 {
		conditions = append(conditions, "o.type IN $types")
		params["types"] = req.Types
	}

	// Add time range filter
	if req.TimeRange != nil {
		if !req.TimeRange.Start.IsZero() {
			conditions = append(conditions, "o.timestamp >= $start_time")
			params["start_time"] = req.TimeRange.Start.Unix()
		}
		if !req.TimeRange.End.IsZero() {
			conditions = append(conditions, "o.timestamp <= $end_time")
			params["end_time"] = req.TimeRange.End.Unix()
		}
	}

	if len(conditions) > 0 {
		cypher.WriteString("WHERE ")
		cypher.WriteString(strings.Join(conditions, " AND "))
		cypher.WriteString(" ")
	}

	// Add ordering and limit
	cypher.WriteString("RETURN o ORDER BY o.timestamp DESC")
	if req.Limit > 0 {
		cypher.WriteString(" LIMIT $limit")
		params["limit"] = req.Limit
	}

	// Execute query
	result, err := s.neo4jClient.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		res, err := tx.Run(ctx, cypher.String(), params)
		if err != nil {
			return nil, err
		}

		var observations []*domain.ObservationEvent
		for res.Next(ctx) {
			record := res.Record()
			if node, ok := record.Get("o"); ok {
				if nodeValue, ok := node.(neo4j.Node); ok {
					observation := s.convertNodeToObservation(nodeValue)
					if observation != nil {
						observations = append(observations, observation)
					}
				}
			}
		}

		return observations, res.Err()
	})

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if s.queriesErrors != nil {
			s.queriesErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("query_type", "observations"),
				attribute.String("error", err.Error()),
			))
		}
		return nil, 0, fmt.Errorf("failed to query observations: %w", err)
	}

	observations := result.([]*domain.ObservationEvent)
	total := len(observations)

	span.SetAttributes(
		attribute.Int("results_count", len(observations)),
		attribute.Int("total_count", total),
	)

	return observations, total, nil
}

// getObservationByID retrieves a single observation by its ID from Neo4j
func (s *APIServer) getObservationByID(ctx context.Context, id string) (*domain.ObservationEvent, error) {
	ctx, span := s.tracer.Start(ctx, "api.get_observation_by_id")
	defer span.End()

	if s.neo4jClient == nil {
		return nil, fmt.Errorf("Neo4j client not available")
	}

	span.SetAttributes(attribute.String("observation.id", id))

	// Record query metrics
	if s.queriesTotal != nil {
		s.queriesTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("query_type", "observation_by_id"),
		))
	}

	cypher := "MATCH (o:Observation {id: $id}) RETURN o LIMIT 1"
	params := map[string]interface{}{
		"id": id,
	}

	result, err := s.neo4jClient.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}

		if res.Next(ctx) {
			record := res.Record()
			if node, ok := record.Get("o"); ok {
				if nodeValue, ok := node.(neo4j.Node); ok {
					return s.convertNodeToObservation(nodeValue), nil
				}
			}
		}

		return nil, res.Err()
	})

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if s.queriesErrors != nil {
			s.queriesErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("query_type", "observation_by_id"),
				attribute.String("error", err.Error()),
			))
		}
		return nil, fmt.Errorf("failed to get observation: %w", err)
	}

	observation := result.(*domain.ObservationEvent)
	return observation, nil
}

// convertNodeToObservation converts a Neo4j node to ObservationEvent
func (s *APIServer) convertNodeToObservation(node neo4j.Node) *domain.ObservationEvent {
	props := node.Props

	// Helper function to safely extract string from properties
	getString := func(key string) string {
		if val, ok := props[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return ""
	}

	// Helper function to safely extract timestamp
	getTimestamp := func(key string) time.Time {
		if val, ok := props[key]; ok {
			switch v := val.(type) {
			case int64:
				return time.Unix(v, 0)
			case float64:
				return time.Unix(int64(v), 0)
			case time.Time:
				return v
			case string:
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					return t
				}
			}
		}
		return time.Time{}
	}

	// Helper function to safely extract int32 pointer
	getInt32Ptr := func(key string) *int32 {
		if val, ok := props[key]; ok {
			switch v := val.(type) {
			case int32:
				return &v
			case int64:
				i32 := int32(v)
				return &i32
			case int:
				i32 := int32(v)
				return &i32
			case float64:
				i32 := int32(v)
				return &i32
			case string:
				// Try to parse string as int
				if parsed, err := strconv.ParseInt(v, 10, 32); err == nil {
					i32 := int32(parsed)
					return &i32
				}
			}
		}
		return nil
	}

	// Helper function to safely extract string pointer
	getStringPtr := func(key string) *string {
		if val := getString(key); val != "" {
			return &val
		}
		return nil
	}

	observation := &domain.ObservationEvent{
		ID:          getString("id"),
		Type:        getString("type"),
		Source:      getString("source"),
		Timestamp:   getTimestamp("timestamp"),
		PID:         getInt32Ptr("pid"),
		ContainerID: getStringPtr("container_id"),
		PodName:     getStringPtr("pod_name"),
		Namespace:   getStringPtr("namespace"),
		ServiceName: getStringPtr("service_name"),
		NodeName:    getStringPtr("node_name"),
		Action:      getStringPtr("action"),
		Target:      getStringPtr("target"),
		Result:      getStringPtr("result"),
		Reason:      getStringPtr("reason"),
		Data:        make(map[string]string), // TODO: Extract data from properties
	}

	return observation
}

// queryWhyPodFailedModern provides root cause analysis using modern observation-based approach
func (s *APIServer) queryWhyPodFailedModern(ctx context.Context, pod, namespace string) (*WhyResponse, error) {
	ctx, span := s.tracer.Start(ctx, "api.query_why_pod_failed_modern")
	defer span.End()

	span.SetAttributes(
		attribute.String("pod", pod),
		attribute.String("namespace", namespace),
	)

	// Query observations related to this pod
	req := &ObservationQueryRequest{
		CorrelationKeys: map[string]string{
			"pod_name":  pod,
			"namespace": namespace,
		},
		TimeRange: &TimeRange{
			Start: time.Now().Add(-1 * time.Hour), // Look back 1 hour
			End:   time.Now(),
		},
		Limit: 50,
	}

	observations, _, err := s.queryObservations(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query pod observations: %w", err)
	}

	// Analyze observations to determine root cause
	timeline := make([]TimelineEvent, 0, len(observations))
	impact := []string{}
	suggestions := []string{}
	rootCause := "Unable to determine root cause"

	// Process observations to build timeline and analysis
	for _, obs := range observations {
		details := obs.Type
		if obs.Reason != nil {
			details += ": " + *obs.Reason
		}

		timeline = append(timeline, TimelineEvent{
			Timestamp: obs.Timestamp,
			Event:     obs.Type,
			Details:   details,
		})

		// Analyze for common failure patterns
		switch obs.Type {
		case "pod_failed", "container_failed":
			rootCause = "Pod or container failure detected"
			suggestions = append(suggestions, "Check pod logs and resource limits")
		case "oom_killed":
			rootCause = "Pod killed due to out-of-memory condition"
			suggestions = append(suggestions, "Increase memory limits or optimize memory usage")
		case "image_pull_error":
			rootCause = "Failed to pull container image"
			suggestions = append(suggestions, "Verify image name and registry accessibility")
		case "network_error":
			rootCause = "Network connectivity issues detected"
			suggestions = append(suggestions, "Check network policies and service endpoints")
		}
	}

	// Determine impact
	if len(observations) > 0 {
		impact = append(impact, fmt.Sprintf("Pod %s in namespace %s", pod, namespace))
	}

	response := &WhyResponse{
		Pod:         pod,
		Namespace:   namespace,
		RootCause:   rootCause,
		Timeline:    timeline,
		Impact:      impact,
		Suggestions: suggestions,
	}

	span.SetAttributes(
		attribute.String("root_cause", rootCause),
		attribute.Int("timeline_events", len(timeline)),
	)

	return response, nil
}

// queryServiceImpactModern provides service impact analysis using modern observation-based approach
func (s *APIServer) queryServiceImpactModern(ctx context.Context, service, namespace string) (*ImpactResponse, error) {
	ctx, span := s.tracer.Start(ctx, "api.query_service_impact_modern")
	defer span.End()

	span.SetAttributes(
		attribute.String("service", service),
		attribute.String("namespace", namespace),
	)

	// Query observations related to this service
	req := &ObservationQueryRequest{
		CorrelationKeys: map[string]string{
			"service_name": service,
			"namespace":    namespace,
		},
		TimeRange: &TimeRange{
			Start: time.Now().Add(-1 * time.Hour), // Look back 1 hour
			End:   time.Now(),
		},
		Limit: 100,
	}

	observations, _, err := s.queryObservations(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query service observations: %w", err)
	}

	// Analyze impact
	affectedPods := make(map[string]bool)
	affectedApps := make(map[string]bool)
	downstreamDeps := make(map[string]bool)
	severity := "low"

	for _, obs := range observations {
		// Extract affected resources from correlation keys
		if obs.PodName != nil {
			affectedPods[*obs.PodName] = true
		}

		// Determine severity based on observation types
		switch obs.Type {
		case "service_unavailable", "pod_failed":
			severity = "high"
		case "performance_degradation", "high_latency":
			if severity != "high" {
				severity = "medium"
			}
		}
	}

	// Convert maps to slices
	pods := make([]string, 0, len(affectedPods))
	for pod := range affectedPods {
		pods = append(pods, pod)
	}

	apps := make([]string, 0, len(affectedApps))
	for app := range affectedApps {
		apps = append(apps, app)
	}

	deps := make([]string, 0, len(downstreamDeps))
	for dep := range downstreamDeps {
		deps = append(deps, dep)
	}

	response := &ImpactResponse{
		Service:       service,
		Namespace:     namespace,
		AffectedPods:  pods,
		AffectedApps:  apps,
		DownstreamDep: deps,
		Severity:      severity,
	}

	span.SetAttributes(
		attribute.String("severity", severity),
		attribute.Int("affected_pods", len(pods)),
	)

	return response, nil
}

// Main function
func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	defer logger.Sync()

	// Create API server
	server, err := NewAPIServer(logger)
	if err != nil {
		logger.Fatal("Failed to create API server", zap.Error(err))
	}

	// Start server in goroutine
	go func() {
		if err := server.Start(); err != nil {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	logger.Info("Received shutdown signal")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop server gracefully
	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("Server shutdown error", zap.Error(err))
	}
}
