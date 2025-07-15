package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// EngineClient provides a clean interface for CLI to communicate with tapio-engine
type EngineClient struct {
	endpoint   string
	conn       *grpc.ClientConn
	timeout    time.Duration
	connected  bool
}

// Config holds client configuration
type Config struct {
	Endpoint string        `json:"endpoint"`
	Timeout  time.Duration `json:"timeout"`
	TLS      bool          `json:"tls"`
}

// HealthCheckResponse represents engine health status
type HealthCheckResponse struct {
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Details map[string]string `json:"details"`
}

// CheckRequest represents a health check request
type CheckRequest struct {
	Target    string            `json:"target"`
	Namespace string            `json:"namespace"`
	All       bool              `json:"all"`
	Options   map[string]string `json:"options"`
}

// CheckResponse represents a health check response
type CheckResponse struct {
	Status      string                   `json:"status"`
	Issues      []Issue                  `json:"issues"`
	Suggestions []Suggestion             `json:"suggestions"`
	Summary     string                   `json:"summary"`
	Metadata    map[string]interface{}   `json:"metadata"`
}

// Issue represents a detected problem
type Issue struct {
	Type        string `json:"type"`
	Resource    string `json:"resource"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Details     string `json:"details"`
	Remediation string `json:"remediation"`
}

// Suggestion represents a suggested action
type Suggestion struct {
	Title   string `json:"title"`
	Command string `json:"command"`
	Steps   []string `json:"steps"`
}

// NewEngineClient creates a new engine client
func NewEngineClient(config Config) *EngineClient {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Endpoint == "" {
		config.Endpoint = "localhost:9090"
	}

	return &EngineClient{
		endpoint: config.Endpoint,
		timeout:  config.Timeout,
	}
}

// Connect establishes connection to the engine
func (c *EngineClient) Connect(ctx context.Context) error {
	var opts []grpc.DialOption
	
	// For now, use insecure connection (TLS support can be added later)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	opts = append(opts, grpc.WithTimeout(c.timeout))

	conn, err := grpc.DialContext(ctx, c.endpoint, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to engine at %s: %w", c.endpoint, err)
	}

	c.conn = conn
	c.connected = true
	return nil
}

// Close closes the connection
func (c *EngineClient) Close() error {
	if c.conn != nil {
		c.connected = false
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns connection status
func (c *EngineClient) IsConnected() bool {
	return c.connected && c.conn != nil
}

// HealthCheck performs a health check against the engine
func (c *EngineClient) HealthCheck(ctx context.Context) (*HealthCheckResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("not connected to engine")
	}

	// For now, return a mock response
	// In a real implementation, this would make a gRPC call
	return &HealthCheckResponse{
		Status:  "healthy",
		Message: "Engine is running",
		Details: map[string]string{
			"version": "1.0.0",
			"uptime":  "5m30s",
		},
	}, nil
}

// Check performs a kubernetes health check
func (c *EngineClient) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if !c.connected {
		return nil, fmt.Errorf("not connected to engine")
	}

	// For now, return a mock response
	// In a real implementation, this would make a gRPC call to the engine
	return &CheckResponse{
		Status: "healthy",
		Issues: []Issue{},
		Suggestions: []Suggestion{},
		Summary: "All systems operational",
		Metadata: map[string]interface{}{
			"target": req.Target,
			"namespace": req.Namespace,
			"timestamp": time.Now().Unix(),
		},
	}, nil
}

// DefaultConfig returns default client configuration
func DefaultConfig() Config {
	return Config{
		Endpoint: "localhost:9090",
		Timeout:  30 * time.Second,
		TLS:      false,
	}
}