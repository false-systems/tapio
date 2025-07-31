package pipeline

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// Client represents a pipeline client
type Client interface {
	// Send sends an event to the pipeline
	Send(ctx context.Context, event collectors.RawEvent) error

	// Close closes the client
	Close() error
}

// ClientConfig represents pipeline client configuration
type ClientConfig struct {
	Endpoint      string
	BatchSize     int
	FlushInterval string
	Timeout       string
}

// NewClient creates a new pipeline client
func NewClient(config interface{}) (Client, error) {
	// Convert config to ClientConfig
	var clientConfig *ClientConfig

	switch cfg := config.(type) {
	case *ClientConfig:
		clientConfig = cfg
	case map[string]interface{}:
		clientConfig = &ClientConfig{
			Endpoint:      getString(cfg, "endpoint", "localhost:50051"),
			BatchSize:     getInt(cfg, "batch_size", 100),
			FlushInterval: getString(cfg, "flush_interval", "5s"),
			Timeout:       getString(cfg, "timeout", "30s"),
		}
	default:
		return nil, fmt.Errorf("unsupported config type: %T", config)
	}

	// Create gRPC client
	return NewGRPCClient(clientConfig)
}

// Helper functions for config parsing
func getString(m map[string]interface{}, key, defaultValue string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultValue
}

func getInt(m map[string]interface{}, key string, defaultValue int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	if v, ok := m[key].(int); ok {
		return v
	}
	return defaultValue
}

// noopClient is a no-op implementation for testing
type noopClient struct{}

func (n *noopClient) Send(ctx context.Context, event collectors.RawEvent) error {
	// TODO: Implement actual sending
	return nil
}

func (n *noopClient) Close() error {
	return nil
}
