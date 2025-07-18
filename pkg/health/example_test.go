package health_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/yairfalse/tapio/pkg/health"
)

func ExampleHandler() {
	// Create a health handler for your service
	healthHandler := health.NewHandler("tapio-api", "v1.0.0")
	
	// Add various health checkers
	healthHandler.AddChecker(health.NewHTTPChecker("prometheus", "http://localhost:9090/"))
	
	// Add custom checker
	healthHandler.AddChecker(health.NewCustomChecker("collector", func(ctx context.Context) health.Check {
		// Check if collectors are running
		// This is where you'd check your actual service components
		return health.Check{
			Status:  health.StatusHealthy,
			Message: "All collectors are running",
			Metadata: map[string]interface{}{
				"ebpf":     "running",
				"k8s":      "running", 
				"systemd":  "running",
				"journald": "running",
			},
		}
	}))
	
	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)
	mux.HandleFunc("/health/live", healthHandler.LivenessHandler())
	mux.HandleFunc("/health/ready", healthHandler.ReadinessHandler())
	
	// Start server
	// http.ListenAndServe(":8081", mux)
}

func ExampleHandler_ServeHTTP() {
	// Create handler
	handler := health.NewHandler("test-service", "v1.0.0")
	
	// Add a failing checker
	handler.AddChecker(health.NewCustomChecker("database", func(ctx context.Context) health.Check {
		return health.Check{
			Status:  health.StatusUnhealthy,
			Message: "Cannot connect to database",
		}
	}))
	
	// Create test request
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	
	// Serve the request
	handler.ServeHTTP(w, req)
	
	// Check response
	fmt.Println("Status Code:", w.Code)
	fmt.Println("Content-Type:", w.Header().Get("Content-Type"))
	// Output:
	// Status Code: 503
	// Content-Type: application/json
}