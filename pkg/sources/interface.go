package sources

import (
	"context"

	"github.com/falseyair/tapio/pkg/collectors"
)

// DataSource represents a source of monitoring data
type DataSource interface {
	// Name returns the name of the data source
	Name() string

	// IsAvailable checks if the data source is available on the current platform
	IsAvailable(ctx context.Context) bool

	// Start begins data collection
	Start(ctx context.Context) error

	// Stop stops data collection
	Stop(ctx context.Context) error

	// Collect gathers data for the specified targets
	Collect(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error)

	// SupportsTarget checks if the source can monitor the given target
	SupportsTarget(target collectors.Target) bool
}

// SourceType represents different types of data sources
type SourceType string

const (
	SourceTypeEBPF       SourceType = "ebpf"
	SourceTypeKubernetes SourceType = "kubernetes"
	SourceTypeMock       SourceType = "mock"
)

// SourceConfig provides configuration for data sources
type SourceConfig struct {
	// Type of the source
	Type SourceType `json:"type"`

	// Enabled indicates if the source should be used
	Enabled bool `json:"enabled"`

	// Platform-specific configuration
	Config map[string]interface{} `json:"config,omitempty"`
}
