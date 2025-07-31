package pipeline

import (
	"context"

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
	// TODO: Implement actual pipeline client
	// For now, return a no-op client
	return &noopClient{}, nil
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
