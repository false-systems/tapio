package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/client"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/server/adapters/correlation"
	"github.com/yairfalse/tapio/pkg/server/api"
	"github.com/yairfalse/tapio/pkg/server/logging"
)

func TestRESTAPIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	
	// Start REST server
	logger := logging.NewZapLogger("info")
	adapter := correlation.NewCorrelationAdapter(logger)
	adapter.Enable()
	
	server := api.NewRESTServer(8088, adapter)
	
	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil && err != http.ErrServerClosed {
			t.Errorf("Server error: %v", err)
		}
	}()
	
	// Wait for server to start
	time.Sleep(100 * time.Millisecond)
	
	// Create client
	restClient := client.NewRESTClient("http://localhost:8088")
	
	// Test health check
	t.Run("HealthCheck", func(t *testing.T) {
		err := restClient.HealthCheck(ctx)
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
	})
	
	// Test cluster check
	t.Run("ClusterCheck", func(t *testing.T) {
		request := client.RESTCheckRequest{}
		response, err := restClient.Check(ctx, request)
		if err != nil {
			t.Fatalf("Cluster check failed: %v", err)
		}
		
		if response.Status != "ok" {
			t.Errorf("Expected status 'ok', got %s", response.Status)
		}
	})
	
	// Test namespace check
	t.Run("NamespaceCheck", func(t *testing.T) {
		request := client.RESTCheckRequest{
			Namespace: "default",
		}
		response, err := restClient.Check(ctx, request)
		if err != nil {
			t.Fatalf("Namespace check failed: %v", err)
		}
		
		if response.Namespace != "default" {
			t.Errorf("Expected namespace 'default', got %s", response.Namespace)
		}
	})
	
	// Test submit finding
	t.Run("SubmitFinding", func(t *testing.T) {
		finding := &domain.Finding{
			ID:          domain.FindingID("test-finding-1"),
			Type:        "test",
			Severity:    domain.SeverityMedium,
			Title:       "Test Finding",
			Description: "This is a test finding",
			Timestamp:   time.Now(),
		}
		
		err := restClient.SubmitFinding(ctx, finding)
		if err != nil {
			t.Fatalf("Submit finding failed: %v", err)
		}
	})
	
	// Test correlate events
	t.Run("CorrelateEvents", func(t *testing.T) {
		events := []domain.Event{
			{
				ID:        domain.EventID("event-1"),
				Type:      domain.EventType("test"),
				Source:    domain.SourceType("test"),
				Severity:  domain.SeverityLow,
				Timestamp: time.Now(),
			},
		}
		
		correlations, err := restClient.CorrelateEvents(ctx, events)
		if err != nil {
			t.Fatalf("Correlate events failed: %v", err)
		}
		
		if correlations == nil {
			t.Error("Expected correlations, got nil")
		}
	})
	
	// Test server status
	t.Run("ServerStatus", func(t *testing.T) {
		status, err := restClient.GetStatus(ctx)
		if err != nil {
			t.Fatalf("Get status failed: %v", err)
		}
		
		if status.Status != "running" {
			t.Errorf("Expected status 'running', got %s", status.Status)
		}
		
		if !status.Correlation {
			t.Error("Expected correlation to be enabled")
		}
	})
	
	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := server.Stop(shutdownCtx); err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}
}

// TestCLIServerMode tests the CLI in server mode
func TestCLIServerMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	
	// Start REST server
	logger := logging.NewZapLogger("info")
	adapter := correlation.NewCorrelationAdapter(logger)
	adapter.Enable()
	
	server := api.NewRESTServer(8089, adapter)
	
	// Start server in background
	go func() {
		if err := server.Start(ctx); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()
	
	// Wait for server to start
	time.Sleep(100 * time.Millisecond)
	
	// Simulate CLI check command with server mode
	// This would normally be done via exec.Command but for testing
	// we'll use the client directly
	
	client := client.NewRESTClient("http://localhost:8089")
	
	// Perform check like CLI would
	request := client.RESTCheckRequest{
		Namespace: "kube-system",
	}
	
	response, err := client.Check(ctx, request)
	if err != nil {
		t.Fatalf("CLI check failed: %v", err)
	}
	
	if response.Status != "ok" {
		t.Errorf("Expected successful check, got status %s", response.Status)
	}
	
	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := server.Stop(shutdownCtx); err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}
}