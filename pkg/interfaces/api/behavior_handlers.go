package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"github.com/yairfalse/tapio/pkg/intelligence/behavior"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// BehaviorHandlers provides HTTP handlers for the behavior prediction system
type BehaviorHandlers struct {
	logger  *zap.Logger
	engine  *behavior.Engine
	store   *neo4j.BehaviorStore
	limiter *rate.Limiter

	// OTEL instrumentation
	tracer           trace.Tracer
	requestsTotal    metric.Int64Counter
	requestDuration  metric.Float64Histogram
	feedbackReceived metric.Int64Counter
	errorsTotal      metric.Int64Counter
}

// BehaviorHandlersConfig configures the behavior handlers
type BehaviorHandlersConfig struct {
	Engine     *behavior.Engine
	Store      *neo4j.BehaviorStore
	RateLimit  int // Requests per second
	BurstLimit int
}

// NewBehaviorHandlers creates new behavior API handlers
func NewBehaviorHandlers(logger *zap.Logger, config BehaviorHandlersConfig) (*BehaviorHandlers, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if config.Engine == nil {
		return nil, fmt.Errorf("behavior engine is required")
	}
	if config.Store == nil {
		return nil, fmt.Errorf("behavior store is required")
	}

	// Set defaults
	if config.RateLimit == 0 {
		config.RateLimit = 100
	}
	if config.BurstLimit == 0 {
		config.BurstLimit = config.RateLimit * 2
	}

	// Initialize OTEL
	tracer := otel.Tracer("api.behavior_handlers")
	meter := otel.Meter("api.behavior_handlers")

	requestsTotal, err := meter.Int64Counter(
		"behavior_api_requests_total",
		metric.WithDescription("Total number of behavior API requests"),
	)
	if err != nil {
		logger.Warn("Failed to create requests counter", zap.Error(err))
	}

	requestDuration, err := meter.Float64Histogram(
		"behavior_api_request_duration_ms",
		metric.WithDescription("Behavior API request duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create request duration histogram", zap.Error(err))
	}

	feedbackReceived, err := meter.Int64Counter(
		"behavior_feedback_received_total",
		metric.WithDescription("Total number of feedback items received via API"),
	)
	if err != nil {
		logger.Warn("Failed to create feedback counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		"behavior_api_errors_total",
		metric.WithDescription("Total number of behavior API errors"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	return &BehaviorHandlers{
		logger:           logger,
		engine:           config.Engine,
		store:            config.Store,
		limiter:          rate.NewLimiter(rate.Limit(config.RateLimit), config.BurstLimit),
		tracer:           tracer,
		requestsTotal:    requestsTotal,
		requestDuration:  requestDuration,
		feedbackReceived: feedbackReceived,
		errorsTotal:      errorsTotal,
	}, nil
}

// RegisterRoutes registers all behavior API routes
func (h *BehaviorHandlers) RegisterRoutes(router *mux.Router) {
	// API v1 routes
	v1 := router.PathPrefix("/api/v1/behavior").Subrouter()

	// Health check
	v1.HandleFunc("/health", h.handleHealth).Methods("GET")

	// Feedback endpoints
	v1.HandleFunc("/feedback", h.handleSubmitFeedback).Methods("POST")
	v1.HandleFunc("/feedback/{id}", h.handleGetFeedback).Methods("GET")

	// Prediction endpoints
	v1.HandleFunc("/predictions/{id}", h.handleGetPrediction).Methods("GET")
	v1.HandleFunc("/predictions/{id}/context", h.handleGetPredictionContext).Methods("GET")

	// Pattern management
	v1.HandleFunc("/patterns", h.handleListPatterns).Methods("GET")
	v1.HandleFunc("/patterns/{id}", h.handleGetPattern).Methods("GET")

	// Metrics endpoint
	v1.HandleFunc("/metrics", h.handleGetMetrics).Methods("GET")
}

// handleHealth handles health check requests
func (h *BehaviorHandlers) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.health")
	defer span.End()

	h.recordRequest(ctx, "health", time.Now())

	health := h.engine.GetHealth()

	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"engine":    health,
	}

	h.writeJSON(w, http.StatusOK, response)
}

// FeedbackRequest represents a feedback submission request
type FeedbackRequest struct {
	PredictionID       string              `json:"prediction_id"`
	PatternID          string              `json:"pattern_id"`
	FeedbackType       domain.FeedbackType `json:"feedback_type"`
	PredictionOccurred bool                `json:"prediction_occurred"`
	AccuracyScore      float64             `json:"accuracy_score,omitempty"`
	UserID             string              `json:"user_id"`
	Comments           string              `json:"comments,omitempty"`
}

// handleSubmitFeedback handles feedback submission
func (h *BehaviorHandlers) handleSubmitFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.submit_feedback")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "submit_feedback", start)

	// Rate limiting
	if !h.limiter.Allow() {
		h.recordError(ctx, "rate_limited")
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Parse request
	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.recordError(ctx, "invalid_request")
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.PredictionID == "" {
		h.recordError(ctx, "missing_prediction_id")
		http.Error(w, "prediction_id is required", http.StatusBadRequest)
		return
	}
	if req.PatternID == "" {
		h.recordError(ctx, "missing_pattern_id")
		http.Error(w, "pattern_id is required", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		h.recordError(ctx, "missing_user_id")
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Validate feedback type
	switch req.FeedbackType {
	case domain.FeedbackCorrect, domain.FeedbackIncorrect, domain.FeedbackPartial:
		// Valid
	default:
		h.recordError(ctx, "invalid_feedback_type")
		http.Error(w, "Invalid feedback_type. Must be 'correct', 'incorrect', or 'partial'", http.StatusBadRequest)
		return
	}

	// Create feedback domain object
	feedback := &domain.BehaviorFeedback{
		ID:                 fmt.Sprintf("feedback-%d", time.Now().UnixNano()),
		PredictionID:       req.PredictionID,
		PatternID:          req.PatternID,
		FeedbackType:       req.FeedbackType,
		PredictionOccurred: req.PredictionOccurred,
		AccuracyScore:      req.AccuracyScore,
		Timestamp:          time.Now(),
		UserID:             req.UserID,
		Comments:           req.Comments,
	}

	// Apply feedback to engine
	if err := h.engine.ApplyFeedback(ctx, feedback); err != nil {
		h.recordError(ctx, "apply_feedback_failed")
		h.logger.Error("Failed to apply feedback", zap.Error(err))
		http.Error(w, "Failed to apply feedback", http.StatusInternalServerError)
		return
	}

	// Store feedback in Neo4j
	if err := h.store.StoreFeedback(ctx, feedback); err != nil {
		h.recordError(ctx, "store_feedback_failed")
		h.logger.Error("Failed to store feedback", zap.Error(err))
		// Don't fail the request - feedback was applied to engine
	}

	// Record metrics
	if h.feedbackReceived != nil {
		h.feedbackReceived.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pattern_id", req.PatternID),
			attribute.String("feedback_type", string(req.FeedbackType)),
		))
	}

	span.SetAttributes(
		attribute.String("feedback.id", feedback.ID),
		attribute.String("prediction.id", req.PredictionID),
		attribute.String("feedback.type", string(req.FeedbackType)),
	)

	// Return success response
	response := map[string]interface{}{
		"feedback_id": feedback.ID,
		"status":      "accepted",
		"timestamp":   feedback.Timestamp.Unix(),
	}

	h.writeJSON(w, http.StatusCreated, response)
}

// handleGetFeedback retrieves feedback by ID
func (h *BehaviorHandlers) handleGetFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.get_feedback")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "get_feedback", start)

	vars := mux.Vars(r)
	feedbackID := vars["id"]

	if feedbackID == "" {
		h.recordError(ctx, "missing_feedback_id")
		http.Error(w, "Feedback ID is required", http.StatusBadRequest)
		return
	}

	// For now, return not implemented
	// In production, query from Neo4j
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

// handleGetPrediction retrieves a prediction by ID
func (h *BehaviorHandlers) handleGetPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.get_prediction")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "get_prediction", start)

	vars := mux.Vars(r)
	predictionID := vars["id"]

	if predictionID == "" {
		h.recordError(ctx, "missing_prediction_id")
		http.Error(w, "Prediction ID is required", http.StatusBadRequest)
		return
	}

	// For now, return not implemented
	// In production, query from Neo4j
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

// handleGetPredictionContext retrieves prediction context from the graph
func (h *BehaviorHandlers) handleGetPredictionContext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.get_prediction_context")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "get_prediction_context", start)

	vars := mux.Vars(r)
	predictionID := vars["id"]

	if predictionID == "" {
		h.recordError(ctx, "missing_prediction_id")
		http.Error(w, "Prediction ID is required", http.StatusBadRequest)
		return
	}

	// Get context from Neo4j
	context, err := h.store.GetPredictionContext(ctx, predictionID)
	if err != nil {
		h.recordError(ctx, "get_context_failed")
		h.logger.Error("Failed to get prediction context",
			zap.String("prediction_id", predictionID),
			zap.Error(err))
		http.Error(w, "Failed to retrieve context", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusOK, context)
}

// handleListPatterns lists all available patterns
func (h *BehaviorHandlers) handleListPatterns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.list_patterns")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "list_patterns", start)

	patterns := h.engine.GetPatterns()

	// Convert to API response format
	response := make([]map[string]interface{}, 0, len(patterns))
	for _, p := range patterns {
		response = append(response, map[string]interface{}{
			"id":             p.ID,
			"name":           p.Name,
			"category":       p.Category,
			"severity":       p.Severity,
			"description":    p.Description,
			"enabled":        p.Enabled,
			"min_confidence": p.MinConfidence,
		})
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"patterns": response,
		"total":    len(response),
	})
}

// handleGetPattern retrieves a specific pattern
func (h *BehaviorHandlers) handleGetPattern(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.get_pattern")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "get_pattern", start)

	vars := mux.Vars(r)
	patternID := vars["id"]

	if patternID == "" {
		h.recordError(ctx, "missing_pattern_id")
		http.Error(w, "Pattern ID is required", http.StatusBadRequest)
		return
	}

	pattern, exists := h.engine.GetPattern(patternID)
	if !exists {
		h.recordError(ctx, "pattern_not_found")
		http.Error(w, "Pattern not found", http.StatusNotFound)
		return
	}

	h.writeJSON(w, http.StatusOK, pattern)
}

// handleGetMetrics returns current system metrics
func (h *BehaviorHandlers) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, "behavior_api.get_metrics")
	defer span.End()

	start := time.Now()
	defer h.recordRequest(ctx, "get_metrics", start)

	metrics := h.engine.GetMetrics()

	h.writeJSON(w, http.StatusOK, metrics)
}

// Helper methods

// writeJSON writes a JSON response
func (h *BehaviorHandlers) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}

// recordRequest records request metrics
func (h *BehaviorHandlers) recordRequest(ctx context.Context, endpoint string, start time.Time) {
	duration := time.Since(start).Milliseconds()

	if h.requestsTotal != nil {
		h.requestsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", endpoint),
		))
	}

	if h.requestDuration != nil {
		h.requestDuration.Record(ctx, float64(duration), metric.WithAttributes(
			attribute.String("endpoint", endpoint),
		))
	}
}

// recordError records error metrics
func (h *BehaviorHandlers) recordError(ctx context.Context, errorType string) {
	if h.errorsTotal != nil {
		h.errorsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
		))
	}
}
