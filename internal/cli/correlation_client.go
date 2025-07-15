package cli

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// CorrelationClient provides access to the correlation server
type CorrelationClient struct {
	conn   *grpc.ClientConn
	client correlation.CorrelationQueryClient
}

// NewCorrelationClient creates a new correlation client
func NewCorrelationClient(serverAddr string) (*CorrelationClient, error) {
	if serverAddr == "" {
		serverAddr = "localhost:9090" // Default correlation server address
	}

	conn, err := grpc.Dial(serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to correlation server: %w", err)
	}

	return &CorrelationClient{
		conn:   conn,
		client: correlation.NewCorrelationQueryClient(conn),
	}, nil
}

// Close closes the connection
func (c *CorrelationClient) Close() error {
	return c.conn.Close()
}

// GetPredictions retrieves predictions for a resource
func (c *CorrelationClient) GetPredictions(ctx context.Context, resourceName, namespace string) ([]*correlation.Prediction, error) {
	req := &correlation.GetPredictionsRequest{
		ResourceName: resourceName,
		Namespace:    namespace,
	}

	resp, err := c.client.GetPredictions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get predictions: %w", err)
	}

	return resp.Predictions, nil
}

// GetInsights retrieves all insights for a resource
func (c *CorrelationClient) GetInsights(ctx context.Context, resourceName, namespace string) ([]*correlation.InsightResponse, error) {
	req := &correlation.GetInsightsRequest{
		ResourceName: resourceName,
		Namespace:    namespace,
	}

	resp, err := c.client.GetInsights(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get insights: %w", err)
	}

	return resp.Insights, nil
}

// GetActionableItems retrieves fix suggestions
func (c *CorrelationClient) GetActionableItems(ctx context.Context, resourceName, namespace string) ([]*correlation.ActionableItem, error) {
	req := &correlation.GetActionableItemsRequest{
		ResourceName: resourceName,
		Namespace:    namespace,
		AutoFixOnly:  false,
	}

	resp, err := c.client.GetActionableItems(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get actionable items: %w", err)
	}

	return resp.Items, nil
}

// FormatPrediction formats a prediction for CLI output
func FormatPrediction(pred *correlation.Prediction) string {
	if pred == nil {
		return ""
	}

	var output string
	switch pred.Type {
	case "oom":
		if pred.TimeToEvent > 0 {
			output = fmt.Sprintf("Pod will OOM in %.0f minutes", pred.TimeToEvent.Minutes())
		} else {
			output = "OOM imminent"
		}
	case "crash":
		output = fmt.Sprintf("Pod likely to crash (%.0f%% probability)", pred.Probability*100)
	case "network_failure":
		output = fmt.Sprintf("Network issues predicted in %.0f minutes", pred.TimeToEvent.Minutes())
	default:
		output = fmt.Sprintf("%s predicted (%.0f%% confidence)", pred.Type, pred.Confidence*100)
	}

	return output
}

// FormatInsight formats an insight for CLI output
func FormatInsight(insight *correlation.InsightResponse) string {
	output := fmt.Sprintf("[%s] %s\n", insight.Severity, insight.Title)

	if insight.Description != "" {
		output += fmt.Sprintf("  %s\n", insight.Description)
	}

	if insight.Prediction != nil {
		output += fmt.Sprintf("  Prediction: %s\n", FormatPrediction(insight.Prediction))
	}

	if len(insight.ActionableItems) > 0 {
		output += "  Suggested fixes:\n"
		for i, item := range insight.ActionableItems {
			output += fmt.Sprintf("    [%d] %s\n", i+1, item.Description)
			if item.Command != "" {
				output += fmt.Sprintf("        $ %s\n", item.Command)
			}
		}
	}

	return output
}

// TryCorrelationServer attempts to use correlation server, falls back to local analysis
func TryCorrelationServer(ctx context.Context, resourceName, namespace string) ([]*correlation.InsightResponse, error) {
	// Try to connect to correlation server
	client, err := NewCorrelationClient("")
	if err != nil {
		// Server not available, return nil to trigger fallback
		return nil, nil
	}
	defer client.Close()

	// Get insights from server
	insights, err := client.GetInsights(ctx, resourceName, namespace)
	if err != nil {
		return nil, nil // Fallback to local
	}

	return insights, nil
}
