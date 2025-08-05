package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"github.com/yairfalse/tapio/pkg/intelligence/analysis"
	"go.uber.org/zap/zaptest"
)

func TestAnalysisHandlers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	// Create mock aggregator
	aggConfig := aggregator.AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: aggregator.ConflictResolutionHighestConfidence,
		TimeoutDuration:    time.Second * 30,
		MaxFindings:        100,
		EnableLearning:     false,
	}
	agg := aggregator.NewCorrelationAggregator(logger, aggConfig)
	
	// Create test server with adapter
	adapter := NewAggregatorAdapter(agg)
	config := DefaultConfig()
	server, err := NewServer(adapter, nil, logger, config)
	require.NoError(t, err)

	t.Run("AnalyzeEvent", func(t *testing.T) {
		// Test successful analysis
		req := httptest.NewRequest("POST", "/api/v1/analysis/event/evt-123", nil)
		req = mux.SetURLVars(req, map[string]string{"event_id": "evt-123"})
		w := httptest.NewRecorder()

		server.handleAnalyzeEvent(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var response AnalysisResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)
		
		assert.Equal(t, "evt-123", response.EventID)
		assert.NotEmpty(t, response.AnalysisID)
		assert.GreaterOrEqual(t, response.Confidence, 0.0)
		assert.LessOrEqual(t, response.Confidence, 1.0)
		assert.NotEmpty(t, response.Summary)
	})

	t.Run("AnalyzeEventWithFindings", func(t *testing.T) {
		// Test with findings in request body
		findings := []aggregator.Finding{
			{
				ID:         "finding-1",
				Type:       "root_cause",
				Severity:   aggregator.SeverityHigh,
				Confidence: 0.85,
				Message:    "Pod OOM killed",
			},
		}
		
		body := AnalysisRequest{
			EventID:  "evt-123",
			Findings: findings,
		}
		
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/analysis/event/evt-123", bytes.NewReader(jsonBody))
		req = mux.SetURLVars(req, map[string]string{"event_id": "evt-123"})
		w := httptest.NewRecorder()

		server.handleAnalyzeEvent(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DetectPatterns", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/patterns/detect?event_id=evt-123", nil)
		w := httptest.NewRecorder()

		server.handleDetectPatterns(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var patterns []PatternInfo
		err := json.NewDecoder(w.Body).Decode(&patterns)
		require.NoError(t, err)
		
		// Should return empty array for now (no events in storage)
		assert.NotNil(t, patterns)
	})

	t.Run("DetectPatternsNoEventID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/patterns/detect", nil)
		w := httptest.NewRecorder()

		server.handleDetectPatterns(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var errResp map[string]string
		json.NewDecoder(w.Body).Decode(&errResp)
		assert.Contains(t, errResp["error"], "event_id is required")
	})

	t.Run("DetectPatternsInvalidTimeWindow", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/patterns/detect?event_id=evt-123&time_window=invalid", nil)
		w := httptest.NewRecorder()

		server.handleDetectPatterns(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var errResp map[string]string
		json.NewDecoder(w.Body).Decode(&errResp)
		assert.Contains(t, errResp["error"], "invalid time_window format")
	})

	t.Run("CalculateConfidence", func(t *testing.T) {
		scoreCtx := analysis.ScoreContext{
			DirectEvidence:   5,
			IndirectEvidence: 2,
			DataCompleteness: 0.9,
		}
		
		body := ConfidenceRequest{
			ScoreContext: scoreCtx,
		}
		
		jsonBody, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/confidence/calculate", bytes.NewReader(jsonBody))
		w := httptest.NewRecorder()

		server.handleCalculateConfidence(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var response ConfidenceResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		require.NoError(t, err)
		
		assert.Greater(t, response.Confidence, 0.0)
		assert.LessOrEqual(t, response.Confidence, 1.0)
		assert.NotNil(t, response.Breakdown)
	})

	t.Run("CalculateConfidenceInvalidBody", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/confidence/calculate", bytes.NewReader([]byte("invalid")))
		w := httptest.NewRecorder()

		server.handleCalculateConfidence(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("AnalysisHistory", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/analysis/history?resource=my-pod", nil)
		w := httptest.NewRecorder()

		server.handleAnalysisHistory(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var analyses []AnalysisResponse
		err := json.NewDecoder(w.Body).Decode(&analyses)
		require.NoError(t, err)
		
		// Should return empty array for now (no historical data)
		assert.NotNil(t, analyses)
	})

	t.Run("AnalysisHistoryNoResource", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/analysis/history", nil)
		w := httptest.NewRecorder()

		server.handleAnalysisHistory(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var errResp map[string]string
		json.NewDecoder(w.Body).Decode(&errResp)
		assert.Contains(t, errResp["error"], "resource is required")
	})

	t.Run("AnalysisHistoryInvalidTimeframe", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/analysis/history?resource=my-pod&timeframe=invalid", nil)
		w := httptest.NewRecorder()

		server.handleAnalysisHistory(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var errResp map[string]string
		json.NewDecoder(w.Body).Decode(&errResp)
		assert.Contains(t, errResp["error"], "invalid timeframe format")
	})
}

// TestAnalysisIntegration tests the full integration flow
func TestAnalysisIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	// Create real aggregator
	aggConfig := aggregator.AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: aggregator.ConflictResolutionHighestConfidence,
		TimeoutDuration:    time.Second * 30,
		MaxFindings:        100,
		EnableLearning:     false,
	}
	agg := aggregator.NewCorrelationAggregator(logger, aggConfig)
	
	// Create test server with adapter
	adapter := NewAggregatorAdapter(agg)
	config := DefaultConfig()
	server, err := NewServer(adapter, nil, logger, config)
	require.NoError(t, err)

	// Create test findings
	findings := []aggregator.Finding{
		{
			ID:         "finding-1",
			Type:       "memory_exhaustion",
			Severity:   aggregator.SeverityCritical,
			Confidence: 0.90,
			Message:    "Container killed due to OOM",
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{
					{
						ID:        "oom-event",
						Type:      "oom_killed",
						Timestamp: time.Now().Add(-2 * time.Minute),
						Severity:  domain.EventSeverityCritical,
					},
				},
			},
			Timestamp: time.Now(),
		},
	}
	
	body := AnalysisRequest{
		EventID:  "evt-123",
		Findings: findings,
	}
	
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/analysis/event/evt-123", bytes.NewReader(jsonBody))
	req = mux.SetURLVars(req, map[string]string{"event_id": "evt-123"})
	w := httptest.NewRecorder()

	server.handleAnalyzeEvent(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	
	var response AnalysisResponse
	err = json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	
	// Verify analysis results
	assert.Equal(t, "evt-123", response.EventID)
	assert.Greater(t, response.Confidence, 0.5) // Should have decent confidence with critical finding
	assert.NotEmpty(t, response.Summary)
	
	// Should have insights for high confidence findings
	if response.Confidence > 0.8 {
		assert.NotEmpty(t, response.Insights)
	}
}