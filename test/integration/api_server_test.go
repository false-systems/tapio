package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/internal/api"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/types"
)

func TestAPIServerIntegration(t *testing.T) {
	// Create test correlation engine
	engine := createTestCorrelationEngine(t)

	// Create test insight store
	store := correlation.NewInMemoryInsightStore()

	// Add some test insights
	testInsight := &correlation.InsightResponse{
		ID:          "test-insight-1",
		Resource:    "api-service",
		Namespace:   "default",
		Type:        "prediction",
		Severity:    "high",
		Title:       "Memory leak detected",
		Description: "API service showing memory growth pattern",
		Timestamp:   time.Now(),
		Prediction: &correlation.Prediction{
			Type:        "oom",
			Probability: 0.85,
			TimeWindow:  "7 minutes",
			Description: "Pod will OOM in 7 minutes due to memory leak in /api/users endpoint",
		},
		ActionableItems: []*correlation.ActionableItem{
			{
				Type:        "kubectl",
				Description: "Increase memory limit",
				Command:     "kubectl patch deployment api-service -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'",
				SafetyLevel: "medium",
			},
		},
	}

	store.StoreInsight(testInsight)

	// Create API server
	config := &api.Config{
		Port:               8888,
		LogLevel:           "info",
		EnableWebSocket:    true,
		EnableHealthChecks: true,
	}

	logger := zap.NewNop()
	server := api.NewServer(config, engine, store, logger)

	// Create test server
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	baseURL := testServer.URL

	t.Run("Health Check", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var health map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err)

		assert.Equal(t, "ok", health["status"])
		assert.NotNil(t, health["correlation_engine"])
		assert.NotNil(t, health["insight_store"])
	})

	t.Run("Get Resource Insights", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/insights/default/api-service")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.True(t, result["using_correlation"].(bool))

		insights := result["insights"].([]interface{})
		assert.Len(t, insights, 1)

		insight := insights[0].(map[string]interface{})
		assert.Equal(t, "api-service", insight["resource"])
		assert.Equal(t, "default", insight["namespace"])
		assert.Equal(t, "Memory leak detected", insight["title"])
		assert.Equal(t, "high", insight["severity"])
	})

	t.Run("Get Predictions", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/predictions/default/api-service")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		predictions := result["predictions"].([]interface{})
		assert.Len(t, predictions, 1)

		prediction := predictions[0].(map[string]interface{})
		assert.Equal(t, "oom", prediction["type"])
		assert.Equal(t, 0.85, prediction["probability"])
		assert.Equal(t, "7 minutes", prediction["time_window"])
	})

	t.Run("Get Actionable Items", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/fixes/default/api-service")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		items := result["actionable_items"].([]interface{})
		assert.Len(t, items, 1)

		item := items[0].(map[string]interface{})
		assert.Equal(t, "kubectl", item["type"])
		assert.Equal(t, "Increase memory limit", item["description"])
		assert.Equal(t, "medium", item["safety_level"])
		assert.Contains(t, item["command"], "kubectl patch deployment")
	})

	t.Run("Apply Fix", func(t *testing.T) {
		fixRequest := map[string]interface{}{
			"resource":   "api-service",
			"namespace":  "default",
			"fix_type":   "memory_limit",
			"auto_apply": false,
			"dry_run":    true,
		}

		jsonData, _ := json.Marshal(fixRequest)

		resp, err := http.Post(
			baseURL+"/api/v1/fixes/apply",
			"application/json",
			bytes.NewBuffer(jsonData),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "dry_run", result["status"])
		assert.NotNil(t, result["commands"])
		assert.NotNil(t, result["preview"])
	})

	t.Run("Get Cluster Overview", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/cluster/overview")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.NotNil(t, result["cluster_health"])
		assert.NotNil(t, result["namespace_summary"])
		assert.NotNil(t, result["recent_predictions"])
		assert.NotNil(t, result["active_issues"])
	})

	t.Run("Resource Not Found", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/insights/default/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		insights := result["insights"].([]interface{})
		assert.Len(t, insights, 0)
	})

	t.Run("Readiness Check", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/readyz")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "ready", result["status"])
	})
}

func TestCorrelationEngineIntegration(t *testing.T) {
	engine := createTestCorrelationEngine(t)

	t.Run("Process Events", func(t *testing.T) {
		// Create test events
		events := []*types.Event{
			{
				ID:        "test-event-1",
				Type:      "pod_restart",
				Resource:  "api-service-xyz",
				Namespace: "default",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"restart_count": 5,
					"exit_code":     137,
					"reason":        "OOMKilled",
				},
			},
			{
				ID:        "test-event-2",
				Type:      "memory_usage",
				Resource:  "api-service-xyz",
				Namespace: "default",
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"usage_bytes":   400000000,
					"limit_bytes":   500000000,
					"usage_percent": 80.0,
				},
			},
		}

		// Process events
		for _, event := range events {
			err := engine.ProcessEvent(context.Background(), event)
			assert.NoError(t, err)
		}

		// Allow time for correlation processing
		time.Sleep(100 * time.Millisecond)

		// Check for predictions
		predictions := engine.GetPredictions("api-service-xyz", "default")
		assert.NotEmpty(t, predictions)

		// Should have OOM prediction
		var oomPrediction *correlation.Prediction
		for _, pred := range predictions {
			if pred.Type == "oom" {
				oomPrediction = pred
				break
			}
		}

		assert.NotNil(t, oomPrediction)
		assert.Greater(t, oomPrediction.Probability, 0.7)
		assert.Contains(t, oomPrediction.Description, "OOM")
	})

	t.Run("Generate Insights", func(t *testing.T) {
		insights := engine.GetInsights("api-service-xyz", "default")
		assert.NotEmpty(t, insights)

		// Should have memory-related insight
		var memoryInsight *correlation.InsightResponse
		for _, insight := range insights {
			if insight.Type == "memory_analysis" {
				memoryInsight = insight
				break
			}
		}

		assert.NotNil(t, memoryInsight)
		assert.Equal(t, "high", memoryInsight.Severity)
		assert.NotEmpty(t, memoryInsight.ActionableItems)
	})
}

func createTestCorrelationEngine(t *testing.T) *correlation.PerfectEngine {
	config := &correlation.Config{
		BufferSize:            10000,
		AnalysisWindow:        5 * time.Minute,
		MaxCorrelationDepth:   5,
		EnabledCorrelators:    []string{"memory", "restart", "network", "cpu", "storage", "timeline"},
		ProcessingConcurrency: 4,
	}

	engine, err := correlation.NewPerfectEngine(config)
	require.NoError(t, err)

	// Start the engine
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go engine.Start(ctx)

	// Wait for engine to be ready
	time.Sleep(100 * time.Millisecond)

	return engine
}

func TestL7ProtocolParsers(t *testing.T) {
	t.Run("HTTP Parser", func(t *testing.T) {
		// Test HTTP request parsing
		httpRequest := []byte("GET /api/users HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n")

		parser := &correlation.HTTPParser{}
		flow, err := parser.ParseFlow(httpRequest, "request")

		require.NoError(t, err)
		assert.NotNil(t, flow)
		assert.Equal(t, "GET", flow.Request.Method)
		assert.Equal(t, "/api/users", flow.Request.Path)
		assert.Equal(t, "example.com", flow.Request.Headers["Host"])
	})

	t.Run("gRPC Parser", func(t *testing.T) {
		// Test gRPC message parsing (simplified)
		grpcData := []byte{0x00, 0x00, 0x00, 0x00, 0x05, 0x08, 0x96, 0x01}

		parser := &correlation.GRPCParser{}
		message, err := parser.ParseMessage(grpcData, "request")

		require.NoError(t, err)
		assert.NotNil(t, message)
		assert.Equal(t, "request", message.Type)
	})

	t.Run("Kafka Parser", func(t *testing.T) {
		// Test Kafka message detection
		kafkaData := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03} // Simplified Kafka header

		isKafka := correlation.DetectKafkaTraffic(kafkaData, 9092)
		assert.True(t, isKafka)

		isNotKafka := correlation.DetectKafkaTraffic(kafkaData, 80)
		assert.False(t, isNotKafka)
	})
}

func TestCLIEnhancedChecker(t *testing.T) {
	// This would require a mock Kubernetes client
	// For now, we'll test the correlation client integration

	t.Run("Correlation Client Fallback", func(t *testing.T) {
		// Test that CLI gracefully handles correlation server unavailable
		client, err := correlation.NewCorrelationClient("localhost:9999") // Non-existent server

		// Should not error on creation
		assert.NoError(t, err)

		// But should handle connection errors gracefully
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		insights, err := client.GetInsights(ctx, "test-resource", "default")

		// Should handle connection error gracefully
		assert.Error(t, err)
		assert.Empty(t, insights)
	})
}

func BenchmarkAPIServer(b *testing.B) {
	engine := createTestCorrelationEngine(b)
	store := correlation.NewInMemoryInsightStore()

	config := &api.Config{
		Port:               8888,
		LogLevel:           "error",
		EnableWebSocket:    false,
		EnableHealthChecks: false,
	}

	logger := zap.NewNop()
	server := api.NewServer(config, engine, store, logger)
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	b.ResetTimer()

	b.Run("Insights Endpoint", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := http.Get(testServer.URL + "/api/v1/insights/default/api-service")
				if err != nil {
					b.Fatal(err)
				}
				resp.Body.Close()
			}
		})
	})

	b.Run("Health Check", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := http.Get(testServer.URL + "/health")
				if err != nil {
					b.Fatal(err)
				}
				resp.Body.Close()
			}
		})
	})
}
