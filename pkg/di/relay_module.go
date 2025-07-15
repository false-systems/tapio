package di

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/relay"
	"go.uber.org/zap"
)

// RelayModule provides relay component with dependency injection
type RelayModule struct {
	container *Container
	relay     *relay.Relay
	config    *relay.Config
}

// NewRelayModule creates a new relay module
func NewRelayModule() *RelayModule {
	return &RelayModule{}
}

// ID returns the module identifier
func (m *RelayModule) ID() string {
	return "relay"
}

// Configure sets up the module with configuration
func (m *RelayModule) Configure(container *Container) error {
	m.container = container
	
	// Get configuration from container or use defaults
	config, ok := container.Get("relay.config").(*relay.Config)
	if !ok {
		config = relay.DefaultConfig()
		
		// Override with environment variables if available
		if endpoint := container.GetString("engine.endpoint"); endpoint != "" {
			config.EngineEndpoint = endpoint
		}
		if endpoint := container.GetString("otel.endpoint"); endpoint != "" {
			config.OTELEndpoint = endpoint
		}
	}
	m.config = config
	
	return nil
}

// Provide registers relay services in the container
func (m *RelayModule) Provide() error {
	// Get logger
	logger, err := GetService[*zap.Logger](m.container, "logger")
	if err != nil {
		return fmt.Errorf("failed to get logger: %w", err)
	}
	
	// Create relay instance
	relayInstance, err := relay.NewRelay(m.config, logger)
	if err != nil {
		return fmt.Errorf("failed to create relay: %w", err)
	}
	m.relay = relayInstance
	
	// Register services
	m.container.Set("relay", relayInstance)
	m.container.Set("relay.service", relay.RelayService(relayInstance))
	m.container.Set("relay.processor", relay.EventProcessor(relayInstance))
	
	return nil
}

// Start begins relay operations
func (m *RelayModule) Start(ctx context.Context) error {
	if m.relay == nil {
		return fmt.Errorf("relay not initialized")
	}
	
	return m.relay.Start(ctx)
}

// Stop gracefully shuts down the relay
func (m *RelayModule) Stop() error {
	if m.relay == nil {
		return nil
	}
	
	return m.relay.Stop()
}

// Dependencies returns required modules
func (m *RelayModule) Dependencies() []string {
	return []string{"core"} // Needs logger from core module
}