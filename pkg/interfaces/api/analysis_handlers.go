package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"github.com/yairfalse/tapio/pkg/intelligence/analysis"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// AnalysisRequest represents a request to analyze an event
// swagger:model
type AnalysisRequest struct {
	// Event ID to analyze
	// required: true
	// example: evt-12345
	EventID string `json:"event_id"`
	
	// Optional findings to analyze (if not provided, will fetch from storage)
	// required: false
	Findings []aggregator.Finding `json:"findings,omitempty"`
	
	// Time window for analysis
	// required: false
	// example: 30m
	TimeWindow string `json:"time_window,omitempty"`
}

// AnalysisResponse represents the analysis result
// swagger:model
type AnalysisResponse struct {
	// Unique analysis ID
	// example: analysis-evt-12345-1234567890
	AnalysisID string `json:"analysis_id"`
	
	// Event ID that was analyzed
	// example: evt-12345
	EventID string `json:"event_id"`
	
	// Overall confidence score (0.0 to 1.0)
	// example: 0.85
	Confidence float64 `json:"confidence"`
	
	// Analysis quality score (0.0 to 1.0)
	// example: 0.75
	Quality float64 `json:"quality"`
	
	// Human-readable summary
	// example: OOM Cascade detected with 85% confidence. Memory exhaustion causing pod restarts
	Summary string `json:"summary"`
	
	// Detected patterns
	Patterns []PatternInfo `json:"patterns,omitempty"`
	
	// Generated insights
	Insights []InsightInfo `json:"insights,omitempty"`
	
	// Recommendations
	Recommendations []RecommendationInfo `json:"recommendations,omitempty"`
	
	// Analysis timestamp
	// example: 2024-01-20T15:30:00Z
	Timestamp time.Time `json:"timestamp"`
}

// PatternInfo represents detected pattern information
// swagger:model
type PatternInfo struct {
	// Pattern name
	// example: OOM Cascade
	Name string `json:"name"`
	
	// Pattern confidence
	// example: 0.9
	Confidence float64 `json:"confidence"`
	
	// Pattern description
	// example: Memory exhaustion causing pod restarts
	Description string `json:"description"`
}

// InsightInfo represents an analysis insight
// swagger:model
type InsightInfo struct {
	// Insight type
	// example: pattern_detected
	Type string `json:"type"`
	
	// Insight severity
	// example: high
	Severity string `json:"severity"`
	
	// Insight title
	// example: OOM Cascade Pattern Detected
	Title string `json:"title"`
	
	// Detailed message
	// example: Multiple pods are restarting due to memory exhaustion
	Message string `json:"message"`
	
	// Supporting evidence
	Evidence []string `json:"evidence,omitempty"`
}

// RecommendationInfo represents an actionable recommendation
// swagger:model
type RecommendationInfo struct {
	// Priority level
	// example: high
	Priority string `json:"priority"`
	
	// Recommendation type
	// example: resource_adjustment
	Type string `json:"type"`
	
	// Title
	// example: Increase Memory Limits
	Title string `json:"title"`
	
	// Description
	// example: Pod is experiencing OOM kills. Consider increasing memory limits or optimizing memory usage.
	Description string `json:"description"`
	
	// Action items
	Actions []string `json:"actions"`
}

// handleAnalyzeEvent handles POST /api/v1/analysis/event/{event_id}
// swagger:operation POST /api/v1/analysis/event/{event_id} analysis analyzeEvent
// ---
// summary: Analyze an event with correlation findings
// description: |
//   Performs deep analysis on an event and its correlation findings,
//   detecting patterns, generating insights, and providing recommendations.
// parameters:
// - name: event_id
//   in: path
//   description: Event ID to analyze
//   required: true
//   type: string
// - name: body
//   in: body
//   description: Analysis request
//   required: false
//   schema:
//     "$ref": "#/definitions/AnalysisRequest"
// responses:
//   200:
//     description: Analysis completed successfully
//     schema:
//       "$ref": "#/definitions/AnalysisResponse"
//   400:
//     description: Invalid request
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
//   404:
//     description: Event not found
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
func (s *Server) handleAnalyzeEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var span trace.Span
	if s.instrumentation != nil {
		ctx, span = s.instrumentation.StartSpan(ctx, "handleAnalyzeEvent",
			trace.WithAttributes(
				attribute.String("event_id", mux.Vars(r)["event_id"]),
			))
		defer span.End()
	}

	eventID := mux.Vars(r)["event_id"]
	
	// Parse optional request body
	var req AnalysisRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.logger.Debug("No request body provided, using defaults")
		}
	}
	
	// TODO: Fetch event from storage (Neo4j or wherever events are stored)
	// For now, create a mock event
	event := &domain.UnifiedEvent{
		ID:        eventID,
		Type:      "pod_restart",
		Timestamp: time.Now(),
		Severity:  domain.EventSeverityHigh,
	}
	
	// Get findings - either from request or fetch from storage
	var findings []aggregator.Finding
	if len(req.Findings) > 0 {
		findings = req.Findings
	} else {
		// TODO: Fetch findings from storage based on eventID
		// For now, return empty findings
		findings = []aggregator.Finding{}
	}
	
	// Run analysis
	result := s.analysisEngine.AnalyzeFindings(ctx, findings, event)
	
	// Convert to API response
	response := AnalysisResponse{
		AnalysisID: result.AnalysisID,
		EventID:    result.EventID,
		Confidence: result.Confidence,
		Quality:    result.Quality,
		Summary:    result.Summary,
		Timestamp:  result.Timestamp,
		Patterns:   make([]PatternInfo, len(result.Patterns)),
		Insights:   make([]InsightInfo, len(result.Insights)),
		Recommendations: make([]RecommendationInfo, len(result.Recommendations)),
	}
	
	// Convert patterns
	for i, p := range result.Patterns {
		response.Patterns[i] = PatternInfo{
			Name:        p.Pattern.Name,
			Confidence:  p.Confidence,
			Description: p.Pattern.Description,
		}
	}
	
	// Convert insights
	for i, ins := range result.Insights {
		response.Insights[i] = InsightInfo{
			Type:     ins.Type,
			Severity: string(ins.Severity),
			Title:    ins.Title,
			Message:  ins.Message,
			Evidence: ins.Evidence,
		}
	}
	
	// Convert recommendations
	for i, rec := range result.Recommendations {
		response.Recommendations[i] = RecommendationInfo{
			Priority:    string(rec.Priority),
			Type:        rec.Type,
			Title:       rec.Title,
			Description: rec.Description,
			Actions:     rec.Actions,
		}
	}
	
	s.respondJSON(w, http.StatusOK, response)
}

// PatternDetectRequest represents pattern detection parameters
// swagger:model
type PatternDetectRequest struct {
	// Event ID to use as reference
	// required: true
	// example: evt-12345
	EventID string `json:"event_id"`
	
	// Time window to search for patterns
	// required: false
	// example: 30m
	TimeWindow string `json:"time_window,omitempty"`
	
	// Maximum number of patterns to return
	// required: false
	// example: 10
	Limit int `json:"limit,omitempty"`
}

// handleDetectPatterns handles GET /api/v1/patterns/detect
// swagger:operation GET /api/v1/patterns/detect analysis detectPatterns
// ---
// summary: Detect patterns in recent events
// description: |
//   Analyzes recent events to detect known failure patterns such as
//   OOM cascades, configuration drift, or network partitions.
// parameters:
// - name: event_id
//   in: query
//   description: Event ID to use as reference point
//   required: true
//   type: string
// - name: time_window
//   in: query
//   description: Time window to analyze (e.g., 30m, 1h, 24h)
//   required: false
//   type: string
//   default: 30m
// responses:
//   200:
//     description: Patterns detected successfully
//     schema:
//       type: array
//       items:
//         "$ref": "#/definitions/PatternInfo"
//   400:
//     description: Invalid request parameters
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
func (s *Server) handleDetectPatterns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var span trace.Span
	if s.instrumentation != nil {
		ctx, span = s.instrumentation.StartSpan(ctx, "handleDetectPatterns")
		defer span.End()
	}

	eventID := r.URL.Query().Get("event_id")
	if eventID == "" {
		s.respondError(w, http.StatusBadRequest, "event_id is required")
		return
	}
	
	timeWindow := r.URL.Query().Get("time_window")
	if timeWindow == "" {
		timeWindow = "30m"
	}
	
	_, err := time.ParseDuration(timeWindow)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid time_window format")
		return
	}
	
	// TODO: Fetch recent events from storage
	// For now, return empty patterns
	events := []domain.UnifiedEvent{}
	
	// Detect patterns
	matcher := analysis.NewPatternMatcher()
	patterns := matcher.DetectPatterns(events)
	
	// Convert to API response
	response := make([]PatternInfo, len(patterns))
	for i, p := range patterns {
		response[i] = PatternInfo{
			Name:        p.Pattern.Name,
			Confidence:  p.Confidence,
			Description: p.Pattern.Description,
		}
	}
	
	if span != nil {
		span.SetAttributes(
			attribute.String("time_window", timeWindow),
			attribute.Int("patterns_found", len(patterns)),
		)
	}
	
	s.respondJSON(w, http.StatusOK, response)
}

// ConfidenceRequest represents confidence calculation request
// swagger:model
type ConfidenceRequest struct {
	// Score context for confidence calculation
	ScoreContext analysis.ScoreContext `json:"score_context"`
}

// ConfidenceResponse represents confidence calculation response
// swagger:model
type ConfidenceResponse struct {
	// Calculated confidence score (0.0 to 1.0)
	// example: 0.75
	Confidence float64 `json:"confidence"`
	
	// Breakdown of confidence factors
	Breakdown map[string]float64 `json:"breakdown,omitempty"`
}

// handleCalculateConfidence handles POST /api/v1/confidence/calculate
// swagger:operation POST /api/v1/confidence/calculate analysis calculateConfidence
// ---
// summary: Calculate confidence score
// description: |
//   Calculates a unified confidence score based on various factors
//   such as evidence quality, temporal factors, and pattern matching.
// parameters:
// - name: body
//   in: body
//   description: Score context
//   required: true
//   schema:
//     "$ref": "#/definitions/ConfidenceRequest"
// responses:
//   200:
//     description: Confidence calculated successfully
//     schema:
//       "$ref": "#/definitions/ConfidenceResponse"
//   400:
//     description: Invalid request
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
func (s *Server) handleCalculateConfidence(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var span trace.Span
	if s.instrumentation != nil {
		ctx, span = s.instrumentation.StartSpan(ctx, "handleCalculateConfidence")
		defer span.End()
	}

	var req ConfidenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	
	// Calculate confidence
	scorer := analysis.NewConfidenceScorer(analysis.DefaultScoringConfig())
	confidence := scorer.CalculateConfidence(req.ScoreContext)
	
	response := ConfidenceResponse{
		Confidence: confidence,
		Breakdown: map[string]float64{
			"direct_evidence":   float64(req.ScoreContext.DirectEvidence),
			"indirect_evidence": float64(req.ScoreContext.IndirectEvidence),
			"data_completeness": req.ScoreContext.DataCompleteness,
		},
	}
	
	if span != nil {
		span.SetAttributes(
			attribute.Float64("confidence", confidence),
		)
	}
	
	s.respondJSON(w, http.StatusOK, response)
}

// handleAnalysisHistory handles GET /api/v1/analysis/history
// swagger:operation GET /api/v1/analysis/history analysis getAnalysisHistory
// ---
// summary: Get historical analysis results
// description: |
//   Retrieves historical analysis results for a specific resource
//   within a given timeframe.
// parameters:
// - name: resource
//   in: query
//   description: Resource name to query
//   required: true
//   type: string
// - name: timeframe
//   in: query
//   description: Timeframe to query (e.g., 1h, 24h, 7d)
//   required: false
//   type: string
//   default: 24h
// - name: limit
//   in: query
//   description: Maximum number of results
//   required: false
//   type: integer
//   default: 100
// responses:
//   200:
//     description: Historical analyses retrieved successfully
//     schema:
//       type: array
//       items:
//         "$ref": "#/definitions/AnalysisResponse"
//   400:
//     description: Invalid request parameters
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorResponse"
func (s *Server) handleAnalysisHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var span trace.Span
	if s.instrumentation != nil {
		ctx, span = s.instrumentation.StartSpan(ctx, "handleAnalysisHistory")
		defer span.End()
	}

	resource := r.URL.Query().Get("resource")
	if resource == "" {
		s.respondError(w, http.StatusBadRequest, "resource is required")
		return
	}
	
	timeframe := r.URL.Query().Get("timeframe")
	if timeframe == "" {
		timeframe = "24h"
	}
	
	_, err := time.ParseDuration(timeframe)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid timeframe format")
		return
	}
	
	// TODO: Query Neo4j for historical analyses
	// For now, return empty array
	analyses := []AnalysisResponse{}
	
	if span != nil {
		span.SetAttributes(
			attribute.String("resource", resource),
			attribute.String("timeframe", timeframe),
			attribute.Int("results", len(analyses)),
		)
	}
	
	s.respondJSON(w, http.StatusOK, analyses)
}