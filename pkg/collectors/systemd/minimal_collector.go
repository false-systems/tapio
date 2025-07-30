//go:build linux
// +build linux

package systemd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// MinimalSystemdCollector implements minimal systemd collection following the blueprint
type MinimalSystemdCollector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// Systemd connections
	dbusConn *dbus.Conn
	journal  *sdjournal.Journal

	// State
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
	healthy bool
}

// NewMinimalSystemdCollector creates a new minimal systemd collector
func NewMinimalSystemdCollector(config collectors.CollectorConfig) (*MinimalSystemdCollector, error) {
	return &MinimalSystemdCollector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		healthy: true,
	}, nil
}

// Name returns the collector name
func (c *MinimalSystemdCollector) Name() string {
	return "systemd-minimal"
}

// Start begins collection
func (c *MinimalSystemdCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Connect to systemd D-Bus
	conn, err := dbus.NewSystemConnection()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus: %w", err)
	}
	c.dbusConn = conn

	// Open journal
	journal, err := sdjournal.NewJournal()
	if err != nil {
		c.dbusConn.Close()
		return fmt.Errorf("failed to open journal: %w", err)
	}
	c.journal = journal

	// Start collection goroutines
	c.wg.Add(2)
	go c.collectJournalEvents()
	go c.collectServiceEvents()

	c.started = true
	return nil
}

// Stop gracefully shuts down
func (c *MinimalSystemdCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	// Cancel context
	c.cancel()

	// Wait for goroutines
	c.wg.Wait()

	// Close connections
	if c.dbusConn != nil {
		c.dbusConn.Close()
	}
	if c.journal != nil {
		c.journal.Close()
	}

	close(c.events)
	c.started = false
	c.healthy = false

	return nil
}

// Events returns the event channel
func (c *MinimalSystemdCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *MinimalSystemdCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// collectJournalEvents collects events from systemd journal
func (c *MinimalSystemdCollector) collectJournalEvents() {
	defer c.wg.Done()

	// Seek to end to only get new events
	if err := c.journal.SeekTail(); err != nil {
		return
	}

	// Start following journal
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Wait for new entries
			r := c.journal.Wait(time.Second)
			if r < 0 {
				continue
			}

			// Read new entries
			for {
				n, err := c.journal.Next()
				if err != nil || n == 0 {
					break
				}

				// Get journal entry
				entry, err := c.journal.GetEntry()
				if err != nil {
					continue
				}

				// Convert to raw event
				data, err := json.Marshal(entry.Fields)
				if err != nil {
					continue
				}

				metadata := map[string]string{
					"source":      "journal",
					"unit":        entry.Fields["_SYSTEMD_UNIT"],
					"priority":    entry.Fields["PRIORITY"],
					"message":     entry.Fields["MESSAGE"],
					"hostname":    entry.Fields["_HOSTNAME"],
					"pid":         entry.Fields["_PID"],
					"uid":         entry.Fields["_UID"],
					"timestamp":   fmt.Sprintf("%d", entry.RealtimeTimestamp),
				}

				event := collectors.RawEvent{
					Timestamp: time.Now(),
					Type:      "systemd-journal",
					Data:      data,
					Metadata:  metadata,
				}

				// Send event
				select {
				case c.events <- event:
				case <-c.ctx.Done():
					return
				default:
					// Buffer full, drop event
				}
			}
		}
	}
}

// collectServiceEvents collects systemd service state changes
func (c *MinimalSystemdCollector) collectServiceEvents() {
	defer c.wg.Done()

	// Subscribe to unit state changes
	err := c.dbusConn.Subscribe()
	if err != nil {
		return
	}
	defer c.dbusConn.Unsubscribe()

	// Create signal channel
	sigChan := make(chan *dbus.Signal, 256)
	c.dbusConn.Signal(sigChan)

	for {
		select {
		case <-c.ctx.Done():
			return
		case sig := <-sigChan:
			if sig == nil {
				continue
			}

			// Filter for unit state changes
			if sig.Name != "org.freedesktop.systemd1.Manager.UnitNew" &&
				sig.Name != "org.freedesktop.systemd1.Manager.UnitRemoved" &&
				sig.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" {
				continue
			}

			// Create raw event from signal
			data, err := json.Marshal(map[string]interface{}{
				"signal": sig.Name,
				"path":   sig.Path,
				"body":   sig.Body,
			})
			if err != nil {
				continue
			}

			metadata := map[string]string{
				"source": "dbus",
				"signal": sig.Name,
				"path":   string(sig.Path),
			}

			// Extract unit name if available
			if len(sig.Body) > 0 {
				if unitName, ok := sig.Body[0].(string); ok {
					metadata["unit"] = unitName
				}
			}

			event := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "systemd-service",
				Data:      data,
				Metadata:  metadata,
			}

			// Send event
			select {
			case c.events <- event:
			case <-c.ctx.Done():
				return
			default:
				// Buffer full, drop event
			}
		}
	}
}

// DefaultSystemdConfig returns default configuration for systemd collector
func DefaultSystemdConfig() collectors.CollectorConfig {
	return collectors.CollectorConfig{
		BufferSize:     1000,
		MetricsEnabled: true,
		Labels: map[string]string{
			"collector": "systemd-minimal",
		},
	}
}