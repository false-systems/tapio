//go:build linux
// +build linux

package systemd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// ebpfEnhancement holds eBPF program state
type ebpfEnhancement struct {
	// TODO: Add eBPF objects, maps, and readers
}

// linuxImpl implements systemdImpl for Linux
type linuxImpl struct {
	conn      *dbus.Conn
	mu        sync.RWMutex
	connected bool
	ebpf      *ebpfEnhancement // Optional eBPF enhancement
}

// newPlatformImpl creates a Linux implementation
func newPlatformImpl() (systemdImpl, error) {
	return &linuxImpl{}, nil
}

func (l *linuxImpl) init() error {
	// Nothing to initialize
	return nil
}

func (l *linuxImpl) connect() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	conn, err := dbus.NewSystemConnection()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus: %w", err)
	}

	l.conn = conn
	l.connected = true
	return nil
}

func (l *linuxImpl) disconnect() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn != nil {
		l.conn.Close()
		l.conn = nil
	}
	l.connected = false
	return nil
}

func (l *linuxImpl) collectEvents(ctx context.Context, events chan<- collectors.RawEvent) error {
	// Try to initialize eBPF enhancement (optional)
	if err := l.initEBPF(); err == nil {
		// Start eBPF collection in background
		go l.collectEBPFEvents(ctx, events)
	}
	// If eBPF fails, we still continue with D-Bus monitoring

	// Subscribe to systemd property changes
	if err := l.conn.Subscribe(); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer l.conn.Unsubscribe()

	// Get initial state of all units
	units, err := l.conn.ListUnits()
	if err != nil {
		return fmt.Errorf("failed to list units: %w", err)
	}

	// Send initial state for each unit
	for _, unit := range units {
		data := SystemdRawData{
			EventType:   "initial_state",
			Unit:        unit.Name,
			UnitType:    extractUnitType(unit.Name),
			ActiveState: unit.ActiveState,
			SubState:    unit.SubState,
		}

		if event, err := createRawEvent(data); err == nil {
			select {
			case events <- event:
			case <-ctx.Done():
				return nil
			default:
				// Buffer full, skip
			}
		}
	}

	// Create channels for monitoring
	statusChan, errChan := l.conn.SubscribeUnits(time.Second)

	// Monitor for changes
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errChan:
			if err != nil {
				// Log error but continue
				continue
			}
		case unitStatuses := <-statusChan:
			if unitStatuses == nil {
				continue
			}

			// Map of unit name to unit state
			for unitName, unitStatus := range unitStatuses {
				if unitStatus == nil {
					continue
				}

				// Determine event type based on state change
				eventType := "state_change"
				if unitStatus.ActiveState == "active" && unitStatus.SubState == "running" {
					eventType = "started"
				} else if unitStatus.ActiveState == "inactive" {
					eventType = "stopped"
				} else if unitStatus.ActiveState == "failed" {
					eventType = "failed"
				}

				data := SystemdRawData{
					EventType:   eventType,
					Unit:        unitName,
					UnitType:    extractUnitType(unitName),
					ActiveState: unitStatus.ActiveState,
					SubState:    unitStatus.SubState,
					// Properties can be fetched separately if needed
				}

				if event, err := createRawEvent(data); err == nil {
					select {
					case events <- event:
					case <-ctx.Done():
						return nil
					default:
						// Buffer full, skip
					}
				}
			}
		}
	}
}

func (l *linuxImpl) isHealthy() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.connected
}

// extractUnitType extracts the unit type from unit name
func extractUnitType(unitName string) string {
	parts := strings.Split(unitName, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

// Placeholder eBPF methods - would be implemented with actual eBPF code
func (l *linuxImpl) initEBPF() error {
	// TODO: Initialize eBPF programs for K8s service syscall monitoring
	return fmt.Errorf("eBPF enhancement not yet implemented")
}

func (l *linuxImpl) collectEBPFEvents(ctx context.Context, events chan<- collectors.RawEvent) {
	// TODO: Collect eBPF events
}

func (l *linuxImpl) cleanupEBPF() {
	// TODO: Cleanup eBPF resources
}
