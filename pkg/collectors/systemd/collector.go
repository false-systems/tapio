package systemd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// SystemdCollector implements minimal systemd collection following the blueprint
// It collects raw systemd events without any business logic or processing
type SystemdCollector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// Platform-specific implementation
	impl systemdImpl

	// State
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
	healthy bool
}

// systemdImpl is the platform-specific interface
type systemdImpl interface {
	init() error
	connect() error
	disconnect() error
	collectEvents(ctx context.Context, events chan<- collectors.RawEvent) error
	isHealthy() bool
}

// NewCollector creates a new systemd collector
func NewCollector(config collectors.CollectorConfig) (*SystemdCollector, error) {
	impl, err := newPlatformImpl()
	if err != nil {
		return nil, fmt.Errorf("failed to create platform implementation: %w", err)
	}

	return &SystemdCollector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		impl:    impl,
		healthy: true,
	}, nil
}

// Name returns the collector name
func (c *SystemdCollector) Name() string {
	return "systemd"
}

// Start begins collection
func (c *SystemdCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Initialize platform implementation
	if err := c.impl.init(); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	// Connect to systemd
	if err := c.impl.connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Start collection
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := c.impl.collectEvents(c.ctx, c.events); err != nil {
			// Log error but don't crash
			c.mu.Lock()
			c.healthy = false
			c.mu.Unlock()
		}
	}()

	c.started = true
	return nil
}

// Stop gracefully shuts down
func (c *SystemdCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	// Cancel context
	c.cancel()

	// Wait for collection to stop
	c.wg.Wait()

	// Disconnect
	if err := c.impl.disconnect(); err != nil {
		// Log but don't fail
	}

	close(c.events)
	c.started = false
	c.healthy = false

	return nil
}

// Events returns the event channel
func (c *SystemdCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *SystemdCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy && c.impl.isHealthy()
}

// SystemdRawData represents the raw data structure we emit
// This is what gets JSON marshaled into RawEvent.Data
type SystemdRawData struct {
	EventType   string                 `json:"event_type"`       // state_change, start, stop, etc.
	Unit        string                 `json:"unit"`             // Unit name
	UnitType    string                 `json:"unit_type"`        // service, socket, timer
	ActiveState string                 `json:"active_state"`     // active, inactive, failed
	SubState    string                 `json:"sub_state"`        // running, dead, exited
	Result      string                 `json:"result,omitempty"` // success, exit-code, signal
	MainPID     int32                  `json:"main_pid,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"` // Additional properties
}

// createRawEvent creates a RawEvent from systemd data
func createRawEvent(data SystemdRawData) (collectors.RawEvent, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return collectors.RawEvent{}, err
	}

	metadata := map[string]string{
		"collector":    "systemd",
		"unit":         data.Unit,
		"unit_type":    data.UnitType,
		"event_type":   data.EventType,
		"active_state": data.ActiveState,
		"sub_state":    data.SubState,
	}

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "systemd",
		Data:      jsonData,
		Metadata:  metadata,
	}, nil
}
