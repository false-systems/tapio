package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"go.uber.org/zap"
)

// MockAggregator for testing
type MockAggregator struct {
	mock.Mock
}

func (m *MockAggregator) QueryCorrelations(ctx context.Context, query aggregator.CorrelationQuery) (*aggregator.AggregatedResult, error) {
	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*aggregator.AggregatedResult), args.Error(1)
}

func (m *MockAggregator) ListCorrelations(ctx context.Context, limit, offset int) (*aggregator.CorrelationList, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*aggregator.CorrelationList), args.Error(1)
}

func (m *MockAggregator) GetCorrelation(ctx context.Context, id string) (*aggregator.AggregatedResult, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*aggregator.AggregatedResult), args.Error(1)
}

func (m *MockAggregator) SubmitFeedback(ctx context.Context, feedback aggregator.CorrelationFeedback) error {
	args := m.Called(ctx, feedback)
	return args.Error(0)
}

func (m *MockAggregator) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockAggregator) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockAggregator) Health(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockAggregator) GetSummary(ctx context.Context) (*aggregator.CorrelationSummary, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*aggregator.CorrelationSummary), args.Error(1)
}

func (m *MockAggregator) ProcessEvent(ctx context.Context, event interface{}) (*aggregator.AggregatedResult, error) {
	args := m.Called(ctx, event)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*aggregator.AggregatedResult), args.Error(1)
}

// Test helpers
func setupTestServer(t *testing.T) (*Server, *MockAggregator) {
	logger := zap.NewNop()

	// Create mock instrumentation
	instrumentation, err := telemetry.NewAPIInstrumentation(logger)
	require.NoError(t, err)

	// Create mock aggregator
	mockAgg := &MockAggregator{}

	// Create server
	config := DefaultConfig()
	server, err := NewServer(mockAgg, instrumentation, logger, config)
	require.NoError(t, err)

	return server, mockAgg
}

func TestNewServer(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, _ := telemetry.NewAPIInstrumentation(logger)

	tests := []struct {
		name      string
		agg       AggregatorInterface
		inst      *telemetry.APIInstrumentation
		logger    *zap.Logger
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid configuration",
			agg:       &MockAggregator{},
			inst:      instrumentation,
			logger:    logger,
			wantError: false,
		},
		{
			name:      "missing aggregator",
			agg:       nil,
			inst:      instrumentation,
			logger:    logger,
			wantError: true,
			errorMsg:  "aggregator is required",
		},
		{
			name:      "missing instrumentation",
			agg:       &MockAggregator{},
			inst:      nil,
			logger:    logger,
			wantError: false, // instrumentation is now optional
		},
		{
			name:      "missing logger",
			agg:       &MockAggregator{},
			inst:      instrumentation,
			logger:    nil,
			wantError: true,
			errorMsg:  "logger is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			server, err := NewServer(tt.agg, tt.inst, tt.logger, config)

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, server)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, server)
			}
		})
	}
}

func TestHandleWhy(t *testing.T) {
	server, mockAgg := setupTestServer(t)

	tests := []struct {
		name           string
		resourceType   string
		namespace      string
		resourceName   string
		queryParams    map[string]string
		mockReturn     *aggregator.AggregatedResult
		mockError      error
		expectedStatus int
		checkResponse  func(t *testing.T, body []byte)
	}{
		{
			name:         "successful query",
			resourceType: "pod",
			namespace:    "default",
			resourceName: "test-pod",
			mockReturn: &aggregator.AggregatedResult{
				ID: "corr-123",
				Resource: aggregator.ResourceRef{
					Type:      "pod",
					Namespace: "default",
					Name:      "test-pod",
				},
				RootCause: &aggregator.RootCause{
					Type:        "config_change",
					Description: "ConfigMap changed",
					Confidence:  0.95,
				},
				Confidence:     0.92,
				ProcessingTime: 150 * time.Millisecond,
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body []byte) {
				var result aggregator.AggregatedResult
				err := json.Unmarshal(body, &result)
				assert.NoError(t, err)
				assert.Equal(t, "corr-123", result.ID)
				assert.Equal(t, "pod", result.Resource.Type)
			},
		},
		{
			name:           "resource not found",
			resourceType:   "pod",
			namespace:      "default",
			resourceName:   "missing-pod",
			mockError:      aggregator.ErrNotFound,
			expectedStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, body []byte) {
				var errResp map[string]string
				err := json.Unmarshal(body, &errResp)
				assert.NoError(t, err)
				assert.Equal(t, "Resource not found", errResp["error"])
			},
		},
		{
			name:         "with time window",
			resourceType: "service",
			namespace:    "prod",
			resourceName: "api-service",
			queryParams: map[string]string{
				"time_window": "6h",
			},
			mockReturn: &aggregator.AggregatedResult{
				ID: "corr-456",
				Resource: aggregator.ResourceRef{
					Type:      "service",
					Namespace: "prod",
					Name:      "api-service",
				},
				Confidence: 0.85,
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock expectation
			mockAgg.On("QueryCorrelations", mock.Anything, mock.Anything).
				Return(tt.mockReturn, tt.mockError).Once()

			// Create request
			url := fmt.Sprintf("/api/v1/why/%s/%s/%s",
				tt.resourceType, tt.namespace, tt.resourceName)

			if len(tt.queryParams) > 0 {
				url += "?"
				for k, v := range tt.queryParams {
					url += fmt.Sprintf("%s=%s&", k, v)
				}
			}

			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()

			// Create router and handle request
			router := mux.NewRouter()
			router.HandleFunc("/api/v1/why/{resource_type}/{namespace}/{name}",
				server.handleWhy).Methods("GET")

			router.ServeHTTP(w, req)

			// Check response
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkResponse != nil {
				tt.checkResponse(t, w.Body.Bytes())
			}

			mockAgg.AssertExpectations(t)
		})
	}
}

func TestHandleListCorrelations(t *testing.T) {
	server, mockAgg := setupTestServer(t)

	mockReturn := &aggregator.CorrelationList{
		Correlations: []aggregator.CorrelationSummary{
			{
				ID: "corr-1",
				Resource: aggregator.ResourceRef{
					Type:      "pod",
					Namespace: "default",
					Name:      "test-pod-1",
				},
				RootCause: "config_change",
				Severity:  aggregator.SeverityHigh,
				CreatedAt: time.Now(),
			},
			{
				ID: "corr-2",
				Resource: aggregator.ResourceRef{
					Type:      "service",
					Namespace: "prod",
					Name:      "api-service",
				},
				RootCause: "resource_failure",
				Severity:  aggregator.SeverityMedium,
				CreatedAt: time.Now(),
			},
		},
		Total:  2,
		Limit:  100,
		Offset: 0,
	}

	mockAgg.On("ListCorrelations", mock.Anything, 100, 0).
		Return(mockReturn, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/correlations", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result aggregator.CorrelationList
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.Len(t, result.Correlations, 2)
	assert.Equal(t, 2, result.Total)

	mockAgg.AssertExpectations(t)
}

func TestHandleGetCorrelation(t *testing.T) {
	server, mockAgg := setupTestServer(t)

	mockReturn := &aggregator.AggregatedResult{
		ID: "corr-123",
		Resource: aggregator.ResourceRef{
			Type:      "pod",
			Namespace: "default",
			Name:      "test-pod",
		},
		Confidence: 0.92,
	}

	mockAgg.On("GetCorrelation", mock.Anything, "corr-123").
		Return(mockReturn, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/correlations/corr-123", nil)
	w := httptest.NewRecorder()

	// Create router for path params
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/correlations/{id}", server.handleGetCorrelation).Methods("GET")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result aggregator.AggregatedResult
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.Equal(t, "corr-123", result.ID)

	mockAgg.AssertExpectations(t)
}

func TestHandleCorrelationFeedback(t *testing.T) {
	server, mockAgg := setupTestServer(t)

	feedback := aggregator.CorrelationFeedback{
		UserID:    "user123",
		Useful:    true,
		CorrectRC: true,
		Comment:   "Very helpful",
	}

	mockAgg.On("SubmitFeedback", mock.Anything, feedback).
		Return(nil).Once()

	body, _ := json.Marshal(feedback)
	req := httptest.NewRequest("POST", "/api/v1/correlations/corr-123/feedback",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Create router for path params
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/correlations/{id}/feedback",
		server.handleCorrelationFeedback).Methods("POST")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "accepted", response["status"])
	assert.Equal(t, "corr-123", response["id"])

	mockAgg.AssertExpectations(t)
}

func TestHandleHealth(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var health map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &health)
	assert.NoError(t, err)
	assert.Equal(t, "healthy", health["status"])
	assert.Equal(t, "1.0.0", health["version"])
	assert.NotNil(t, health["timestamp"])
}

func TestHandleReady(t *testing.T) {
	server, mockAgg := setupTestServer(t)

	tests := []struct {
		name           string
		healthError    error
		expectedStatus int
		expectedBody   map[string]string
	}{
		{
			name:           "ready",
			healthError:    nil,
			expectedStatus: http.StatusOK,
			expectedBody:   map[string]string{"status": "ready"},
		},
		{
			name:           "not ready",
			healthError:    fmt.Errorf("aggregator not initialized"),
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody: map[string]string{
				"status": "not_ready",
				"error":  "aggregator not initialized",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAgg.On("Health", mock.Anything).Return(tt.healthError).Once()

			req := httptest.NewRequest("GET", "/ready", nil)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedBody["status"], response["status"])

			if tt.expectedBody["error"] != "" {
				assert.Equal(t, tt.expectedBody["error"], response["error"])
			}

			mockAgg.AssertExpectations(t)
		})
	}
}

func TestValidateFeedback(t *testing.T) {
	tests := []struct {
		name     string
		feedback aggregator.CorrelationFeedback
		wantErr  bool
		errMsg   string
	}{
		{
			name: "valid feedback",
			feedback: aggregator.CorrelationFeedback{
				UserID:  "user123",
				Useful:  true,
				Comment: "Great analysis",
			},
			wantErr: false,
		},
		{
			name: "missing user ID",
			feedback: aggregator.CorrelationFeedback{
				Useful: true,
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "comment too long",
			feedback: aggregator.CorrelationFeedback{
				UserID:  "user123",
				Useful:  true,
				Comment: string(make([]byte, 1001)),
			},
			wantErr: true,
			errMsg:  "comment too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFeedback(&tt.feedback)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	server, mockAgg := setupTestServer(t)

	t.Run("request size limit", func(t *testing.T) {
		// Create large body
		largeBody := make([]byte, 11*1024*1024) // 11MB
		req := httptest.NewRequest("POST", "/api/v1/correlations/123/feedback",
			bytes.NewReader(largeBody))
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		// Should fail with 413 or 400
		assert.True(t, w.Code == http.StatusRequestEntityTooLarge ||
			w.Code == http.StatusBadRequest)
	})

	t.Run("CORS headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"))
	})

	t.Run("request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
	})

	t.Run("panic recovery", func(t *testing.T) {
		// Create a handler that panics
		panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		})

		// Wrap with recovery middleware
		wrapped := server.recoveryMiddleware(panickingHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		// Should not panic
		assert.NotPanics(t, func() {
			wrapped.ServeHTTP(w, req)
		})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	// Clean up mock expectations
	mockAgg.AssertExpectations(t)
}
