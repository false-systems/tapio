package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	correlationAdapter "github.com/yairfalse/tapio/pkg/server/adapters/correlation"
	"github.com/yairfalse/tapio/pkg/server/domain"
)

func TestServerWithAdapter_HealthCheck(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	// Test health endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "healthy", response["status"])
}

func TestServerWithAdapter_GetResourceInsights(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Add test insights
	adapter.AddInsight(&correlationAdapter.Insight{
		ID:          "test-insight-1",
		Title:       "Test Insight",
		Description: "Test description",
		Severity:    "high",
		Category:    "resource",
		Resource:    "test-deployment",
		Namespace:   "default",
		Timestamp:   time.Now(),
	})

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	// Test get insights endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/insights/default/test-deployment", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "test-deployment", response["resource"])
	assert.Equal(t, "default", response["namespace"])
	assert.Equal(t, float64(1), response["count"])
}

func TestServerWithAdapter_ProcessEvent(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	// Create test event
	event := domain.Event{
		Type:     "test_event",
		Severity: "info",
		Source:   "test",
		Message:  "Test event",
		Entity: domain.Entity{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
		},
		Metadata: map[string]interface{}{
			"test": "data",
		},
	}

	// Marshal event
	eventJSON, err := json.Marshal(event)
	require.NoError(t, err)

	// Test process event endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/events", bytes.NewBuffer(eventJSON))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "accepted", response["status"])
	assert.NotEmpty(t, response["event_id"])
}

func TestServerWithAdapter_GetPatterns(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Add test pattern
	adapter.AddPattern(&correlationAdapter.Pattern{
		ID:          "test-pattern",
		Name:        "Test Pattern",
		Description: "Test pattern description",
		Type:        "resource",
		Enabled:     true,
	})

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	// Test get patterns endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/patterns", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, float64(1), response["count"])
}

func TestServerWithAdapter_GetStats(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	// Test stats endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/stats", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, true, response["enabled"])
}

func TestServerWithAdapter_CorrelateEvents(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	// Create test events
	events := struct {
		Events []domain.Event `json:"events"`
	}{
		Events: []domain.Event{
			{
				ID:       "event-1",
				Type:     "memory_warning",
				Severity: "warning",
				Source:   "kubelet",
				Message:  "High memory usage",
			},
			{
				ID:       "event-2",
				Type:     "pod_restart",
				Severity: "error",
				Source:   "kubelet",
				Message:  "Pod restarted",
			},
		},
	}

	// Marshal events
	eventsJSON, err := json.Marshal(events)
	require.NoError(t, err)

	// Test correlate endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/correlate", bytes.NewBuffer(eventsJSON))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.NotEmpty(t, response["correlation_id"])
	assert.Equal(t, float64(2), response["event_count"])
}

func TestServerWithAdapter_AdminEndpoints(t *testing.T) {
	// Create mock adapter
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Create server
	server := NewServerWithAdapter(adapter, nil)

	t.Run("GetStatus", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/admin/status", nil)
		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, true, response["enabled"])
	})

	t.Run("DisableCorrelation", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/admin/correlation/disable", nil)
		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, adapter.IsEnabled())
	})

	t.Run("EnableCorrelation", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/admin/correlation/enable", nil)
		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, adapter.IsEnabled())
	})
}

func TestServerWithAdapter_Middleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("CORS", func(t *testing.T) {
		adapter := correlationAdapter.NewMockCorrelationAdapter()
		config := &Config{
			EnableCORS: true,
		}
		server := NewServerWithAdapter(adapter, config)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("OPTIONS", "/api/v1/insights", nil)
		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Authentication", func(t *testing.T) {
		adapter := correlationAdapter.NewMockCorrelationAdapter()
		config := &Config{
			AuthEnabled: true,
		}
		server := NewServerWithAdapter(adapter, config)

		// Without auth header
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/admin/status", nil)
		server.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// With auth header
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/admin/status", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		server.router.ServeHTTP(w, req)

		// Should pass auth (though token validation is mocked)
		assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	})
}

// Benchmark tests
func BenchmarkServerWithAdapter_ProcessEvent(b *testing.B) {
	adapter := correlationAdapter.NewMockCorrelationAdapter()
	server := NewServerWithAdapter(adapter, nil)

	event := domain.Event{
		Type:     "test_event",
		Severity: "info",
		Source:   "test",
		Message:  "Test event",
	}
	eventJSON, _ := json.Marshal(event)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/events", bytes.NewBuffer(eventJSON))
		req.Header.Set("Content-Type", "application/json")
		server.router.ServeHTTP(w, req)
	}
}

func BenchmarkServerWithAdapter_GetInsights(b *testing.B) {
	adapter := correlationAdapter.NewMockCorrelationAdapter()

	// Add many insights
	for i := 0; i < 100; i++ {
		adapter.AddInsight(&correlationAdapter.Insight{
			ID:          fmt.Sprintf("insight-%d", i),
			Title:       "Test Insight",
			Description: "Test description",
			Severity:    "high",
			Category:    "resource",
			Resource:    "test-deployment",
			Namespace:   "default",
			Timestamp:   time.Now(),
		})
	}

	server := NewServerWithAdapter(adapter, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/insights/default/test-deployment", nil)
		server.router.ServeHTTP(w, req)
	}
}
