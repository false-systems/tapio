package api_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"github.com/yairfalse/tapio/pkg/interfaces/api"
	"go.uber.org/zap"
)

// Example demonstrates how to create and start the API server
func Example() {
	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatal(err)
	}

	// Create API instrumentation
	instrumentation, err := telemetry.NewAPIInstrumentation(logger)
	if err != nil {
		log.Fatal(err)
	}

	// Create aggregator (use actual implementation in production)
	agg := createMockAggregator()

	// Configure API server
	config := api.DefaultConfig()
	config.Port = 8080
	config.EnableCORS = true
	config.AllowedOrigins = []string{"https://app.tapio.io", "http://localhost:3000"}

	// Create server
	server, err := api.NewServer(agg, instrumentation, logger, config)
	if err != nil {
		log.Fatal(err)
	}

	// Start server
	ctx := context.Background()
	logger.Info("Starting API server on :8080")
	logger.Info("API documentation available at http://localhost:8080/api/docs/")

	if err := server.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

// Example_clientUsage shows how to use the API as a client
func Example_clientUsage() {
	// Query why a pod failed
	resp, err := http.Get("http://localhost:8080/api/v1/why/pod/default/frontend-pod-123")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Got correlation analysis")
	}

	// Submit feedback
	feedback := `{"user_id": "user123", "useful": true, "comment": "Helpful!"}`
	resp2, err := http.Post(
		"http://localhost:8080/api/v1/correlations/corr-123/feedback",
		"application/json",
		nil, // Would use strings.NewReader(feedback) in real code
	)
	if err != nil {
		log.Fatal(err)
	}
	defer resp2.Body.Close()

	// Output:
	// Got correlation analysis
}

// createMockAggregator creates a mock aggregator for examples
func createMockAggregator() *aggregator.CorrelationAggregator {
	// In production, use the real aggregator implementation
	logger, _ := zap.NewProduction()
	config := aggregator.AggregatorConfig{
		MinConfidence:      0.5,
		ConflictResolution: aggregator.ConflictResolutionHighestConfidence,
		TimeoutDuration:    30 * time.Second,
		MaxFindings:        100,
		EnableLearning:     true,
	}
	return aggregator.NewCorrelationAggregator(logger, config)
}
