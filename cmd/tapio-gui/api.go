package main

import (
	"context"
	"time"
)

// APIClient provides a simplified API client for the GUI
type APIClient struct {
	endpoint string
}

// NewAPIClient creates a new API client for the GUI
func NewAPIClient(endpoint string) *APIClient {
	return &APIClient{
		endpoint: endpoint,
	}
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// GetHealthStatus returns the health status (mock implementation)
func (c *APIClient) GetHealthStatus(ctx context.Context) *HealthResponse {
	return &HealthResponse{
		Status:    "healthy",
		Message:   "Tapio GUI connected successfully",
		Timestamp: time.Now(),
	}
}
