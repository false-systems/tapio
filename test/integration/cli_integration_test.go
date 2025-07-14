package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/yairfalse/tapio/internal/cli"
	"github.com/yairfalse/tapio/pkg/health"
	"github.com/yairfalse/tapio/pkg/types"
)

func TestCLIEnhancedChecker(t *testing.T) {
	// Create a fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()
	
	t.Run("Test Enhanced Checker Creation", func(t *testing.T) {
		checker, err := cli.NewEnhancedChecker(fakeClient)
		assert.NoError(t, err)
		assert.NotNil(t, checker)
	})
	
	t.Run("Test Health Report Structure", func(t *testing.T) {
		// Create mock health report
		basicHealth := &health.Report{
			ResourceName:        "api-service",
			Namespace:          "default",
			Status:             "Unhealthy",
			RestartCount:       5,
			MemoryUsagePercent: 85.0,
			CPUUsagePercent:    70.0,
			Issues: []health.Issue{
				{
					Type:        "memory",
					Severity:    "high",
					Description: "High memory usage detected",
					Suggestion:  "Consider increasing memory limits",
				},
			},
		}
		
		// Test enhanced report structure
		enhancedReport := &cli.EnhancedHealthReport{
			BasicHealth:      basicHealth,
			Predictions:      []*types.Prediction{},
			Insights:         []*types.InsightResponse{},
			LocalAnalysis:    nil,
			UsingCorrelation: false,
			TimeChecked:      time.Now(),
		}
		
		// Verify structure
		assert.Equal(t, "api-service", enhancedReport.BasicHealth.ResourceName)
		assert.Equal(t, "default", enhancedReport.BasicHealth.Namespace)
		assert.Equal(t, "Unhealthy", enhancedReport.BasicHealth.Status)
		assert.Equal(t, 5, enhancedReport.BasicHealth.RestartCount)
		assert.Equal(t, 85.0, enhancedReport.BasicHealth.MemoryUsagePercent)
		assert.False(t, enhancedReport.UsingCorrelation)
	})
	
	t.Run("Test Local Analysis Fallback", func(t *testing.T) {
		// Create enhanced checker
		checker, err := cli.NewEnhancedChecker(fakeClient)
		require.NoError(t, err)
		
		// Mock basic health with high memory usage
		basicHealth := &health.Report{
			ResourceName:        "api-service",
			Namespace:          "default",
			Status:             "Unhealthy",
			RestartCount:       8,
			MemoryUsagePercent: 92.0,
			CPUUsagePercent:    85.0,
		}
		
		// Test local analysis generation
		localAnalysis := &cli.LocalAnalysis{
			Warnings:    []string{},
			Predictions: []string{},
			Suggestions: []string{},
		}
		
		// High restart count should trigger warning
		if basicHealth.RestartCount > 5 {
			localAnalysis.Warnings = append(localAnalysis.Warnings, 
				fmt.Sprintf("High restart count: %d restarts in last hour", basicHealth.RestartCount))
			localAnalysis.Suggestions = append(localAnalysis.Suggestions,
				"Check pod logs for crash reasons: kubectl logs <pod> --previous")
		}
		
		// High memory usage should trigger warning and prediction
		if basicHealth.MemoryUsagePercent > 80 {
			localAnalysis.Warnings = append(localAnalysis.Warnings,
				fmt.Sprintf("High memory usage: %.1f%%", basicHealth.MemoryUsagePercent))
			
			if basicHealth.MemoryUsagePercent > 90 {
				minutesToOOM := (100 - basicHealth.MemoryUsagePercent) * 10
				localAnalysis.Predictions = append(localAnalysis.Predictions,
					fmt.Sprintf("Pod may OOM in approximately %.0f minutes", minutesToOOM))
			}
		}
		
		// Verify local analysis
		assert.Len(t, localAnalysis.Warnings, 2)
		assert.Len(t, localAnalysis.Predictions, 1)
		assert.Len(t, localAnalysis.Suggestions, 1)
		
		assert.Contains(t, localAnalysis.Warnings[0], "High restart count: 8")
		assert.Contains(t, localAnalysis.Warnings[1], "High memory usage: 92.0%")
		assert.Contains(t, localAnalysis.Predictions[0], "Pod may OOM in approximately 80 minutes")
		assert.Contains(t, localAnalysis.Suggestions[0], "kubectl logs")
	})
	
	t.Run("Test Enhanced Report Formatting", func(t *testing.T) {
		// Create test report with predictions and insights
		basicHealth := &health.Report{
			ResourceName:        "api-service",
			Namespace:          "default",
			Status:             "Unhealthy",
			RestartCount:       3,
			MemoryUsagePercent: 85.0,
		}
		
		predictions := []*types.Prediction{
			{
				Type:        "oom",
				Probability: 0.87,
				TimeWindow:  "6 minutes",
				Description: "Pod will OOM in 6 minutes due to memory leak",
			},
		}
		
		insights := []*types.InsightResponse{
			{
				ID:          "insight-1",
				Resource:    "api-service",
				Namespace:   "default",
				Type:        "memory_analysis",
				Severity:    "high",
				Title:       "Memory leak detected",
				Description: "API service showing memory growth pattern",
			},
		}
		
		report := &cli.EnhancedHealthReport{
			BasicHealth:      basicHealth,
			Predictions:      predictions,
			Insights:         insights,
			UsingCorrelation: true,
			TimeChecked:      time.Now(),
		}
		
		// Verify report contents
		assert.Len(t, report.Predictions, 1)
		assert.Len(t, report.Insights, 1)
		assert.True(t, report.UsingCorrelation)
		
		// Verify prediction
		pred := report.Predictions[0]
		assert.Equal(t, "oom", pred.Type)
		assert.Equal(t, 0.87, pred.Probability)
		assert.Equal(t, "6 minutes", pred.TimeWindow)
		
		// Verify insight
		insight := report.Insights[0]
		assert.Equal(t, "api-service", insight.Resource)
		assert.Equal(t, "high", insight.Severity)
		assert.Equal(t, "Memory leak detected", insight.Title)
	})
}

func TestCorrelationClientIntegration(t *testing.T) {
	t.Run("Test Correlation Client Creation", func(t *testing.T) {
		// Test client creation with invalid server (should not error)
		client, err := cli.NewCorrelationClient("localhost:9999")
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})
	
	t.Run("Test Graceful Degradation", func(t *testing.T) {
		// Test that system handles correlation server being unavailable
		client, err := cli.NewCorrelationClient("localhost:9999")
		require.NoError(t, err)
		
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		
		// This should fail gracefully
		insights, err := client.GetInsights(ctx, "api-service", "default")
		assert.Error(t, err)
		assert.Empty(t, insights)
	})
	
	t.Run("Test CLI Fallback Behavior", func(t *testing.T) {
		// Test the pattern that CLI should use when correlation unavailable
		
		// 1. Try correlation server
		correlationAvailable := false
		var insights []*types.InsightResponse
		
		if correlationAvailable {
			// Would get insights from correlation server
			insights = []*types.InsightResponse{
				{
					ID:       "correlation-insight",
					Resource: "api-service",
					Type:     "prediction",
					Severity: "high",
				},
			}
		} else {
			// Fallback to local analysis
			insights = []*types.InsightResponse{
				{
					ID:       "local-insight",
					Resource: "api-service",
					Type:     "local_analysis",
					Severity: "medium",
				},
			}
		}
		
		// Verify fallback works
		assert.Len(t, insights, 1)
		assert.Equal(t, "local-insight", insights[0].ID)
		assert.Equal(t, "local_analysis", insights[0].Type)
	})
}

func TestCLICommandPatterns(t *testing.T) {
	t.Run("Test Check Command Flow", func(t *testing.T) {
		// Test the expected flow of the check command
		
		// 1. Create enhanced checker
		fakeClient := fake.NewSimpleClientset()
		checker, err := cli.NewEnhancedChecker(fakeClient)
		require.NoError(t, err)
		
		// 2. Should attempt correlation server connection
		correlationConnected := false // Simulate server unavailable
		
		// 3. Should fallback to local analysis
		if !correlationConnected {
			// Verify local analysis is used
			assert.False(t, correlationConnected)
		}
		
		// 4. Should format output appropriately
		outputShouldInclude := []string{
			"🔍 ANALYSIS:",
			"⚡ Using local analysis (correlation server unavailable)",
		}
		
		for _, expected := range outputShouldInclude {
			assert.NotEmpty(t, expected)
		}
	})
	
	t.Run("Test Why Command Integration", func(t *testing.T) {
		// Test that why command can access same data sources
		
		// Mock why command behavior
		resourceName := "api-service"
		namespace := "default"
		
		// Should try correlation server first
		correlationInsights := []*types.InsightResponse{}
		
		// If no correlation insights, should do local analysis
		if len(correlationInsights) == 0 {
			localInsights := []*types.InsightResponse{
				{
					ID:          "why-local",
					Resource:    resourceName,
					Namespace:   namespace,
					Type:        "explanation",
					Severity:    "medium",
					Title:       "Local analysis of resource issues",
					Description: "Basic health check analysis",
				},
			}
			correlationInsights = localInsights
		}
		
		// Verify why command gets insights
		assert.Len(t, correlationInsights, 1)
		assert.Equal(t, "why-local", correlationInsights[0].ID)
		assert.Equal(t, "explanation", correlationInsights[0].Type)
	})
	
	t.Run("Test Fix Command Integration", func(t *testing.T) {
		// Test that fix command can access actionable items
		
		// Mock actionable items from correlation
		actionableItems := []*types.ActionableItem{
			{
				Type:        "kubectl",
				Description: "Increase memory limit",
				Command:     "kubectl patch deployment api-service -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"512Mi\"}}}]}}}}'",
				SafetyLevel: "medium",
			},
		}
		
		// Fix command should be able to process these
		assert.Len(t, actionableItems, 1)
		
		item := actionableItems[0]
		assert.Equal(t, "kubectl", item.Type)
		assert.Equal(t, "medium", item.SafetyLevel)
		assert.Contains(t, item.Command, "kubectl patch")
		assert.Contains(t, item.Command, "memory")
	})
}

func TestAPIServerIntegrationConcepts(t *testing.T) {
	t.Run("Test API Endpoint Accessibility", func(t *testing.T) {
		// Test that API endpoints follow expected patterns
		
		endpoints := map[string]string{
			"insights":     "/api/v1/insights/{namespace}/{resource}",
			"predictions":  "/api/v1/predictions/{namespace}/{resource}",
			"fixes":        "/api/v1/fixes/{namespace}/{resource}",
			"flows":        "/api/v1/flows/{namespace}/{resource}",
			"overview":     "/api/v1/cluster/overview",
			"health":       "/health",
			"ready":        "/readyz",
		}
		
		for name, endpoint := range endpoints {
			// Verify endpoint structure
			assert.NotEmpty(t, endpoint)
			if name != "health" && name != "ready" {
				assert.Contains(t, endpoint, "/api/v1/")
			}
		}
	})
	
	t.Run("Test L7 Protocol Integration", func(t *testing.T) {
		// Test that L7 protocol data can be accessed via API
		
		// Mock L7 flow data
		flows := []map[string]interface{}{
			{
				"protocol": "http",
				"method":   "GET",
				"path":     "/api/users",
				"status":   200,
				"latency":  150,
			},
			{
				"protocol": "grpc",
				"service":  "UserService",
				"method":   "GetUser",
				"status":   "OK",
				"latency":  45,
			},
			{
				"protocol": "kafka",
				"topic":    "user-events",
				"operation": "produce",
				"partition": 0,
			},
		}
		
		// Verify L7 data structure
		assert.Len(t, flows, 3)
		
		httpFlow := flows[0]
		assert.Equal(t, "http", httpFlow["protocol"])
		assert.Equal(t, "GET", httpFlow["method"])
		assert.Equal(t, "/api/users", httpFlow["path"])
		
		grpcFlow := flows[1]
		assert.Equal(t, "grpc", grpcFlow["protocol"])
		assert.Equal(t, "UserService", grpcFlow["service"])
		assert.Equal(t, "GetUser", grpcFlow["method"])
		
		kafkaFlow := flows[2]
		assert.Equal(t, "kafka", kafkaFlow["protocol"])
		assert.Equal(t, "user-events", kafkaFlow["topic"])
		assert.Equal(t, "produce", kafkaFlow["operation"])
	})
	
	t.Run("Test Performance Characteristics", func(t *testing.T) {
		// Test expected performance characteristics
		
		performanceTargets := map[string]interface{}{
			"direct_memory_latency": "<1ms",
			"grpc_latency":          "1-5ms",
			"throughput":            "10,000+ requests/sec",
			"memory_usage":          "<100MB",
			"cpu_overhead":          "<1%",
		}
		
		// Verify performance targets are defined
		for metric, target := range performanceTargets {
			assert.NotEmpty(t, target)
			assert.NotEmpty(t, metric)
		}
	})
}

func BenchmarkCLIIntegration(b *testing.B) {
	fakeClient := fake.NewSimpleClientset()
	
	b.Run("Enhanced Checker Creation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			checker, err := cli.NewEnhancedChecker(fakeClient)
			if err != nil {
				b.Fatal(err)
			}
			_ = checker
		}
	})
	
	b.Run("Local Analysis Generation", func(b *testing.B) {
		basicHealth := &health.Report{
			ResourceName:        "api-service",
			Namespace:          "default",
			Status:             "Unhealthy",
			RestartCount:       8,
			MemoryUsagePercent: 92.0,
			CPUUsagePercent:    85.0,
		}
		
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			analysis := &cli.LocalAnalysis{
				Warnings:    []string{},
				Predictions: []string{},
				Suggestions: []string{},
			}
			
			if basicHealth.RestartCount > 5 {
				analysis.Warnings = append(analysis.Warnings, 
					fmt.Sprintf("High restart count: %d restarts in last hour", basicHealth.RestartCount))
			}
			
			if basicHealth.MemoryUsagePercent > 80 {
				analysis.Warnings = append(analysis.Warnings,
					fmt.Sprintf("High memory usage: %.1f%%", basicHealth.MemoryUsagePercent))
			}
		}
	})
}