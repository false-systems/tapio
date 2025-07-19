//go:build linux
// +build linux

package linux

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/godbus/dbus/v5"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
)

// Implementation provides Linux-specific systemd functionality
type Implementation struct {
	config core.Config

	// D-Bus connection
	conn      *dbus.Conn
	connMutex sync.RWMutex

	// Event processing
	eventChan chan core.RawEvent

	// State tracking
	services    map[string]*serviceState
	servicesMux sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// System info
	systemdVersion string
}

// serviceState tracks the state of a service
type serviceState struct {
	name        string
	activeState string
	subState    string
	mainPID     int32
	lastSeen    time.Time
}

// New creates a new Linux systemd implementation
func New() *Implementation {
	return &Implementation{
		eventChan: make(chan core.RawEvent, 1000),
		services:  make(map[string]*serviceState),
	}
}

// Init initializes the implementation
func (impl *Implementation) Init(config core.Config) error {
	impl.config = config

	// Connect to D-Bus
	conn, err := dbus.NewSystemConnection()
	if err != nil {
		return fmt.Errorf("failed to connect to system D-Bus: %w", err)
	}

	impl.connMutex.Lock()
	impl.conn = conn
	impl.connMutex.Unlock()

	// Get systemd version
	version, err := conn.GetManagerProperty("Version")
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to get systemd version: %w", err)
	}
	impl.systemdVersion = strings.Trim(version, "\"")

	return nil
}

// Start starts the systemd monitoring
func (impl *Implementation) Start(ctx context.Context) error {
	impl.ctx, impl.cancel = context.WithCancel(ctx)

	// Subscribe to systemd signals
	if err := impl.subscribeToSignals(); err != nil {
		return fmt.Errorf("failed to subscribe to signals: %w", err)
	}

	// Initial service scan
	if err := impl.scanServices(); err != nil {
		return fmt.Errorf("failed to scan services: %w", err)
	}

	// Start monitoring
	impl.wg.Add(1)
	go impl.monitorSystemd()

	// Start periodic scanning
	impl.wg.Add(1)
	go impl.periodicScan()

	return nil
}

// Stop stops the systemd monitoring
func (impl *Implementation) Stop() error {
	if impl.cancel != nil {
		impl.cancel()
	}

	impl.wg.Wait()

	impl.connMutex.Lock()
	if impl.conn != nil {
		impl.conn.Close()
		impl.conn = nil
	}
	impl.connMutex.Unlock()

	close(impl.eventChan)

	return nil
}

// Events returns the event channel
func (impl *Implementation) Events() <-chan core.RawEvent {
	return impl.eventChan
}

// IsConnected returns whether connected to D-Bus
func (impl *Implementation) IsConnected() bool {
	impl.connMutex.RLock()
	defer impl.connMutex.RUnlock()
	return impl.conn != nil
}

// SystemdVersion returns the systemd version
func (impl *Implementation) SystemdVersion() string {
	return impl.systemdVersion
}

// ServicesMonitored returns the number of services being monitored
func (impl *Implementation) ServicesMonitored() int {
	impl.servicesMux.RLock()
	defer impl.servicesMux.RUnlock()
	return len(impl.services)
}

// ActiveServices returns the number of active services
func (impl *Implementation) ActiveServices() int {
	impl.servicesMux.RLock()
	defer impl.servicesMux.RUnlock()

	count := 0
	for _, svc := range impl.services {
		if svc.activeState == core.StateActive {
			count++
		}
	}
	return count
}

// FailedServices returns the number of failed services
func (impl *Implementation) FailedServices() int {
	impl.servicesMux.RLock()
	defer impl.servicesMux.RUnlock()

	count := 0
	for _, svc := range impl.services {
		if svc.activeState == core.StateFailed {
			count++
		}
	}
	return count
}

// subscribeToSignals subscribes to systemd D-Bus signals
func (impl *Implementation) subscribeToSignals() error {
	impl.connMutex.RLock()
	conn := impl.conn
	impl.connMutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected to D-Bus")
	}

	// Subscribe to unit state changes
	if err := conn.Subscribe(); err != nil {
		return fmt.Errorf("failed to subscribe to systemd signals: %w", err)
	}

	return nil
}

// scanServices performs initial scan of services
func (impl *Implementation) scanServices() error {
	impl.connMutex.RLock()
	conn := impl.conn
	impl.connMutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected to D-Bus")
	}

	// List all units
	units, err := conn.ListUnits()
	if err != nil {
		return fmt.Errorf("failed to list units: %w", err)
	}

	impl.servicesMux.Lock()
	defer impl.servicesMux.Unlock()

	for _, unit := range units {
		// Filter based on configuration
		if !impl.shouldWatchUnit(unit.Name, unit.Description) {
			continue
		}

		// Update service state
		impl.services[unit.Name] = &serviceState{
			name:        unit.Name,
			activeState: unit.ActiveState,
			subState:    unit.SubState,
			lastSeen:    time.Now(),
		}
	}

	return nil
}

// monitorSystemd monitors systemd signals
func (impl *Implementation) monitorSystemd() {
	defer impl.wg.Done()

	impl.connMutex.RLock()
	conn := impl.conn
	impl.connMutex.RUnlock()

	if conn == nil {
		return
	}

	// Get the signal channel
	sigChan, sigErrs := conn.SubscribeUnits(time.Duration(0))

	for {
		select {
		case <-impl.ctx.Done():
			return

		case changes := <-sigChan:
			for unit, change := range changes {
				impl.processUnitChange(unit, change)
			}

		case err := <-sigErrs:
			if err != nil {
				// Log error but continue
			}
		}
	}
}

// processUnitChange processes a unit state change
func (impl *Implementation) processUnitChange(unitName string, change *dbus.UnitStatus) {
	if change == nil {
		return
	}

	// Check if we should watch this unit
	if !impl.shouldWatchUnit(unitName, change.Description) {
		return
	}

	// Get previous state
	impl.servicesMux.RLock()
	oldSvc, exists := impl.services[unitName]
	impl.servicesMux.RUnlock()

	var oldState, oldSubState string
	if exists {
		oldState = oldSvc.activeState
		oldSubState = oldSvc.subState
	}

	// Create event
	event := core.RawEvent{
		Type:      impl.determineEventType(oldState, change.ActiveState),
		UnitName:  unitName,
		UnitType:  impl.extractUnitType(unitName),
		OldState:  oldState,
		NewState:  change.ActiveState,
		SubState:  change.SubState,
		Timestamp: time.Now(),
		Properties: map[string]interface{}{
			"load_state":   change.LoadState,
			"active_state": change.ActiveState,
			"sub_state":    change.SubState,
			"description":  change.Description,
		},
	}

	// Get additional properties if it's a service
	if strings.HasSuffix(unitName, ".service") {
		impl.enrichServiceEvent(&event)
	}

	// Update tracked state
	impl.servicesMux.Lock()
	impl.services[unitName] = &serviceState{
		name:        unitName,
		activeState: change.ActiveState,
		subState:    change.SubState,
		lastSeen:    time.Now(),
	}
	impl.servicesMux.Unlock()

	// Send event
	select {
	case impl.eventChan <- event:
	case <-impl.ctx.Done():
		return
	default:
		// Channel full, drop event
	}
}

// enrichServiceEvent adds service-specific information to the event
func (impl *Implementation) enrichServiceEvent(event *core.RawEvent) {
	impl.connMutex.RLock()
	conn := impl.conn
	impl.connMutex.RUnlock()

	if conn == nil {
		return
	}

	// Get service properties
	props, err := conn.GetUnitProperties(event.UnitName)
	if err != nil {
		return
	}

	// Extract relevant properties
	if mainPID, ok := props["MainPID"].(uint32); ok {
		event.MainPID = int32(mainPID)
	}

	if result, ok := props["Result"].(string); ok {
		event.Result = result
	}

	if exitCode, ok := props["ExecMainExitCode"].(int32); ok {
		event.ExitCode = exitCode
	}

	if exitStatus, ok := props["ExecMainStatus"].(int32); ok {
		event.ExitStatus = exitStatus
	}
}

// shouldWatchUnit determines if a unit should be watched
func (impl *Implementation) shouldWatchUnit(unitName, description string) bool {
	// Extract unit type
	unitType := impl.extractUnitType(unitName)

	// Check unit type filter
	typeAllowed := false
	for _, allowedType := range impl.config.UnitTypes {
		if unitType == allowedType {
			typeAllowed = true
			break
		}
	}
	if !typeAllowed {
		return false
	}

	// Check service filter
	if len(impl.config.ServiceFilter) > 0 {
		matched := false
		for _, filter := range impl.config.ServiceFilter {
			if strings.Contains(unitName, filter) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check exclusion list
	for _, exclude := range impl.config.ServiceExclude {
		if strings.Contains(unitName, exclude) {
			return false
		}
	}

	return true
}

// extractUnitType extracts the unit type from the unit name
func (impl *Implementation) extractUnitType(unitName string) string {
	parts := strings.Split(unitName, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

// determineEventType determines the event type based on state transition
func (impl *Implementation) determineEventType(oldState, newState string) core.EventType {
	if oldState == "" && newState == core.StateActive {
		return core.EventTypeStart
	}
	if oldState == core.StateActive && newState == core.StateInactive {
		return core.EventTypeStop
	}
	if newState == core.StateFailed {
		return core.EventTypeFailure
	}
	if oldState == core.StateActive && newState == core.StateActivating {
		return core.EventTypeRestart
	}
	return core.EventTypeStateChange
}

// periodicScan performs periodic service scanning
func (impl *Implementation) periodicScan() {
	defer impl.wg.Done()

	ticker := time.NewTicker(impl.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-impl.ctx.Done():
			return

		case <-ticker.C:
			if err := impl.scanServices(); err != nil {
				// Log error but continue
			}
		}
	}
}
