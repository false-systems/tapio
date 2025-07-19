package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Client represents a REST API client
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new REST client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Health checks the server health
func (c *Client) Health(ctx context.Context) (*domain.HealthStatus, error) {
	var health domain.HealthStatus

	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return nil, fmt.Errorf("failed to check health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &health, nil
}

// SubmitEvent submits an event to the server
func (c *Client) SubmitEvent(ctx context.Context, event domain.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/v1/events",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("failed to submit event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("event submission failed with status: %d", resp.StatusCode)
	}

	return nil
}

// GetEvents retrieves events from the server
func (c *Client) GetEvents(ctx context.Context, limit int) ([]domain.Event, error) {
	url := fmt.Sprintf("%s/api/v1/events?limit=%d", c.baseURL, limit)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get events failed with status: %d", resp.StatusCode)
	}

	var events []domain.Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, fmt.Errorf("failed to decode events: %w", err)
	}

	return events, nil
}

// GetFindings retrieves findings from the server
func (c *Client) GetFindings(ctx context.Context) ([]domain.Finding, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/findings")
	if err != nil {
		return nil, fmt.Errorf("failed to get findings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get findings failed with status: %d", resp.StatusCode)
	}

	var findings []domain.Finding
	if err := json.NewDecoder(resp.Body).Decode(&findings); err != nil {
		return nil, fmt.Errorf("failed to decode findings: %w", err)
	}

	return findings, nil
}

// Status gets the server status
func (c *Client) Status(ctx context.Context) (map[string]interface{}, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get status failed with status: %d", resp.StatusCode)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status: %w", err)
	}

	return status, nil
}
