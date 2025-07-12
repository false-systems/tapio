package systemd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// ServiceMonitor monitors systemd service state changes
type ServiceMonitor struct {
	collector *Collector
	config    *ServiceMonitorConfig

	// Service tracking
	watchedServices     []string
	serviceStates       map[string]*ServiceState
	serviceDependencies map[string][]string
	statesMutex         sync.RWMutex

	// Event processing
	events     chan *ServiceEvent
	signalChan <-chan *dbus.Signal

	// Pattern tracking
	restartCounts  map[string]*RestartTracker
	failureHistory map[string][]time.Time

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	isStarted bool
	mutex     sync.RWMutex
}

// ServiceMonitorConfig configures the service monitor
type ServiceMonitorConfig struct {
	WatchedServices    []string
	RestartThreshold   int
	RestartWindow      time.Duration
	FailureThreshold   int
	DependencyTracking bool
	EventBufferSize    int
}

// ServiceState represents the current state of a service
type ServiceState struct {
	Name           string
	ActiveState    string
	SubState       string
	LoadState      string
	UnitFileState  string
	Description    string
	MainPID        uint32
	ExecMainStatus int32
	StatusText     string
	MemoryCurrent  uint64
	CPUUsageNSec   uint64

	// State tracking
	LastStateChange time.Time
	RestartCount    int
	FailureCount    int
	StartTime       time.Time

	// Dependencies
	Requires []string
	Wants    []string
	After    []string
	Before   []string

	// Runtime information
	FragmentPath string
	SourcePath   string
	DropInPaths  []string
}

// ServiceEvent represents a service state change event
type ServiceEvent struct {
	Timestamp   time.Time
	ServiceName string
	EventType   ServiceEventType
	OldState    string
	NewState    string
	Reason      string
	Properties  map[string]interface{}
}

// ServiceEventType defines the type of service event
type ServiceEventType int

const (
	ServiceEventStateChange ServiceEventType = iota
	ServiceEventRestart
	ServiceEventFailure
	ServiceEventStart
	ServiceEventStop
	ServiceEventReload
)

// String returns the string representation of ServiceEventType
func (s ServiceEventType) String() string {
	switch s {
	case ServiceEventStateChange:
		return "state_change"
	case ServiceEventRestart:
		return "restart"
	case ServiceEventFailure:
		return "failure"
	case ServiceEventStart:
		return "start"
	case ServiceEventStop:
		return "stop"
	case ServiceEventReload:
		return "reload"
	default:
		return "unknown"
	}
}

// RestartTracker tracks service restart patterns
type RestartTracker struct {
	RestartTimes []time.Time
	LastRestart  time.Time
	TotalCount   int
}

// NewServiceMonitor creates a new service monitor
func NewServiceMonitor(config *ServiceMonitorConfig) (*ServiceMonitor, error) {
	if config == nil {
		config = &ServiceMonitorConfig{
			WatchedServices:    []string{},
			RestartThreshold:   3,
			RestartWindow:      5 * time.Minute,
			FailureThreshold:   2,
			DependencyTracking: true,
			EventBufferSize:    1000,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	monitor := &ServiceMonitor{
		config:              config,
		watchedServices:     config.WatchedServices,
		serviceStates:       make(map[string]*ServiceState),
		serviceDependencies: make(map[string][]string),
		events:              make(chan *ServiceEvent, config.EventBufferSize),
		restartCounts:       make(map[string]*RestartTracker),
		failureHistory:      make(map[string][]time.Time),
		ctx:                 ctx,
		cancel:              cancel,
	}

	return monitor, nil
}

// Start begins service monitoring
func (sm *ServiceMonitor) Start(ctx context.Context) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.isStarted {
		return fmt.Errorf("service monitor already started")
	}

	// Create collector if not provided
	if sm.collector == nil {
		collector, err := NewCollector(DefaultCollectorConfig())
		if err != nil {
			return fmt.Errorf("failed to create systemd collector: %w", err)
		}
		sm.collector = collector
	}

	// Subscribe to systemd signals
	signalChan, err := sm.collector.SubscribeToSignals()
	if err != nil {
		return fmt.Errorf("failed to subscribe to systemd signals: %w", err)
	}
	sm.signalChan = signalChan

	// Initial state collection
	if err := sm.collectInitialStates(); err != nil {
		return fmt.Errorf("failed to collect initial states: %w", err)
	}

	// Start signal processing
	go sm.processSignals()
	go sm.monitorServices()

	sm.isStarted = true
	return nil
}

// Stop stops service monitoring
func (sm *ServiceMonitor) Stop() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if !sm.isStarted {
		return nil
	}

	sm.cancel()
	close(sm.events)
	sm.isStarted = false

	return nil
}

// GetServiceStates returns current service states
func (sm *ServiceMonitor) GetServiceStates() (map[string]*ServiceState, error) {
	sm.statesMutex.RLock()
	defer sm.statesMutex.RUnlock()

	// Return a copy to avoid race conditions
	states := make(map[string]*ServiceState)
	for name, state := range sm.serviceStates {
		stateCopy := *state
		states[name] = &stateCopy
	}

	return states, nil
}

// GetServiceState returns the state of a specific service
func (sm *ServiceMonitor) GetServiceState(serviceName string) (*ServiceState, error) {
	sm.statesMutex.RLock()
	defer sm.statesMutex.RUnlock()

	state, exists := sm.serviceStates[serviceName]
	if !exists {
		return nil, fmt.Errorf("service %s not found", serviceName)
	}

	// Return a copy
	stateCopy := *state
	return &stateCopy, nil
}

// GetServiceDependencies returns the dependencies of a service
func (sm *ServiceMonitor) GetServiceDependencies(serviceName string) ([]string, error) {
	sm.statesMutex.RLock()
	defer sm.statesMutex.RUnlock()

	deps, exists := sm.serviceDependencies[serviceName]
	if !exists {
		return nil, fmt.Errorf("dependencies for service %s not found", serviceName)
	}

	return deps, nil
}

// GetEventChannel returns the event channel
func (sm *ServiceMonitor) GetEventChannel() <-chan *ServiceEvent {
	return sm.events
}

// AddService adds a service to the watch list
func (sm *ServiceMonitor) AddService(serviceName string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for _, existing := range sm.watchedServices {
		if existing == serviceName {
			return nil // Already watching
		}
	}

	sm.watchedServices = append(sm.watchedServices, serviceName)

	// If started, collect initial state for the new service
	if sm.isStarted {
		if err := sm.collectServiceState(serviceName); err != nil {
			return fmt.Errorf("failed to collect state for service %s: %w", serviceName, err)
		}
	}

	return nil
}

// RemoveService removes a service from the watch list
func (sm *ServiceMonitor) RemoveService(serviceName string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for i, existing := range sm.watchedServices {
		if existing == serviceName {
			sm.watchedServices = append(sm.watchedServices[:i], sm.watchedServices[i+1:]...)
			break
		}
	}

	// Clean up state
	sm.statesMutex.Lock()
	delete(sm.serviceStates, serviceName)
	delete(sm.serviceDependencies, serviceName)
	delete(sm.restartCounts, serviceName)
	delete(sm.failureHistory, serviceName)
	sm.statesMutex.Unlock()

	return nil
}

// collectInitialStates collects initial states for all watched services
func (sm *ServiceMonitor) collectInitialStates() error {
	for _, serviceName := range sm.watchedServices {
		if err := sm.collectServiceState(serviceName); err != nil {
			// Log error but continue with other services
			continue
		}
	}
	return nil
}

// collectServiceState collects the current state of a service
func (sm *ServiceMonitor) collectServiceState(serviceName string) error {
	// Get basic unit properties
	activeState, err := sm.collector.GetUnitProperty(serviceName, "ActiveState")
	if err != nil {
		return fmt.Errorf("failed to get ActiveState: %w", err)
	}

	subState, err := sm.collector.GetUnitProperty(serviceName, "SubState")
	if err != nil {
		return fmt.Errorf("failed to get SubState: %w", err)
	}

	loadState, err := sm.collector.GetUnitProperty(serviceName, "LoadState")
	if err != nil {
		return fmt.Errorf("failed to get LoadState: %w", err)
	}

	description, err := sm.collector.GetUnitProperty(serviceName, "Description")
	if err != nil {
		description = ""
	}

	fragmentPath, err := sm.collector.GetUnitProperty(serviceName, "FragmentPath")
	if err != nil {
		fragmentPath = ""
	}

	// Get service-specific properties
	mainPID, err := sm.collector.GetServiceProperty(serviceName, "MainPID")
	if err != nil {
		mainPID = uint32(0)
	}

	execMainStatus, err := sm.collector.GetServiceProperty(serviceName, "ExecMainStatus")
	if err != nil {
		execMainStatus = int32(0)
	}

	statusText, err := sm.collector.GetServiceProperty(serviceName, "StatusText")
	if err != nil {
		statusText = ""
	}

	// Get memory usage
	memoryCurrent, err := sm.collector.GetServiceProperty(serviceName, "MemoryCurrent")
	if err != nil {
		memoryCurrent = uint64(0)
	}

	// Get CPU usage
	cpuUsageNSec, err := sm.collector.GetServiceProperty(serviceName, "CPUUsageNSec")
	if err != nil {
		cpuUsageNSec = uint64(0)
	}

	// Get dependencies if enabled
	var requires, wants, after, before []string
	if sm.config.DependencyTracking {
		requires = sm.getServiceDependencies(serviceName, "Requires")
		wants = sm.getServiceDependencies(serviceName, "Wants")
		after = sm.getServiceDependencies(serviceName, "After")
		before = sm.getServiceDependencies(serviceName, "Before")
	}

	state := &ServiceState{
		Name:            serviceName,
		ActiveState:     activeState.(string),
		SubState:        subState.(string),
		LoadState:       loadState.(string),
		Description:     description.(string),
		MainPID:         mainPID.(uint32),
		ExecMainStatus:  execMainStatus.(int32),
		StatusText:      statusText.(string),
		MemoryCurrent:   memoryCurrent.(uint64),
		CPUUsageNSec:    cpuUsageNSec.(uint64),
		FragmentPath:    fragmentPath.(string),
		Requires:        requires,
		Wants:           wants,
		After:           after,
		Before:          before,
		LastStateChange: time.Now(),
	}

	// Get restart count from tracker
	if tracker, exists := sm.restartCounts[serviceName]; exists {
		state.RestartCount = tracker.TotalCount
	}

	// Get failure count from history
	if history, exists := sm.failureHistory[serviceName]; exists {
		state.FailureCount = len(history)
	}

	sm.statesMutex.Lock()
	sm.serviceStates[serviceName] = state
	sm.statesMutex.Unlock()

	return nil
}

// getServiceDependencies gets dependencies of a specific type
func (sm *ServiceMonitor) getServiceDependencies(serviceName, depType string) []string {
	deps, err := sm.collector.GetUnitProperty(serviceName, depType)
	if err != nil {
		return []string{}
	}

	if depArray, ok := deps.([]string); ok {
		return depArray
	}

	return []string{}
}

// processSignals processes D-Bus signals from systemd
func (sm *ServiceMonitor) processSignals() {
	for {
		select {
		case <-sm.ctx.Done():
			return
		case signal := <-sm.signalChan:
			sm.handleSignal(signal)
		}
	}
}

// handleSignal handles a D-Bus signal
func (sm *ServiceMonitor) handleSignal(signal *dbus.Signal) {
	switch signal.Name {
	case "org.freedesktop.systemd1.Manager.UnitNew":
		if len(signal.Body) >= 2 {
			unitName := signal.Body[0].(string)
			sm.handleUnitNew(unitName)
		}
	case "org.freedesktop.systemd1.Manager.UnitRemoved":
		if len(signal.Body) >= 2 {
			unitName := signal.Body[0].(string)
			sm.handleUnitRemoved(unitName)
		}
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		if len(signal.Body) >= 2 {
			sm.handlePropertiesChanged(signal)
		}
	}
}

// handleUnitNew handles new unit signals
func (sm *ServiceMonitor) handleUnitNew(unitName string) {
	for _, watchedService := range sm.watchedServices {
		if watchedService == unitName {
			sm.collectServiceState(unitName)
			break
		}
	}
}

// handleUnitRemoved handles unit removal signals
func (sm *ServiceMonitor) handleUnitRemoved(unitName string) {
	sm.statesMutex.Lock()
	defer sm.statesMutex.Unlock()

	if state, exists := sm.serviceStates[unitName]; exists {
		// Emit removal event
		event := &ServiceEvent{
			Timestamp:   time.Now(),
			ServiceName: unitName,
			EventType:   ServiceEventStop,
			OldState:    state.ActiveState,
			NewState:    "removed",
			Reason:      "unit_removed",
		}

		select {
		case sm.events <- event:
		default:
			// Drop event if buffer is full
		}

		delete(sm.serviceStates, unitName)
	}
}

// handlePropertiesChanged handles property change signals
func (sm *ServiceMonitor) handlePropertiesChanged(signal *dbus.Signal) {
	// Extract unit name from signal path
	_ = string(signal.Path) // unitPath - TODO: implement parsing
	// Simple extraction - in reality we'd need more robust parsing
	// For now, skip complex parsing
}

// monitorServices periodically monitors service states
func (sm *ServiceMonitor) monitorServices() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			for _, serviceName := range sm.watchedServices {
				oldState := sm.getServiceStateCopy(serviceName)
				sm.collectServiceState(serviceName)
				newState := sm.getServiceStateCopy(serviceName)

				if oldState != nil && newState != nil {
					sm.detectStateChanges(oldState, newState)
				}
			}
		}
	}
}

// getServiceStateCopy gets a copy of service state
func (sm *ServiceMonitor) getServiceStateCopy(serviceName string) *ServiceState {
	sm.statesMutex.RLock()
	defer sm.statesMutex.RUnlock()

	if state, exists := sm.serviceStates[serviceName]; exists {
		stateCopy := *state
		return &stateCopy
	}
	return nil
}

// detectStateChanges detects and reports state changes
func (sm *ServiceMonitor) detectStateChanges(oldState, newState *ServiceState) {
	if oldState.ActiveState != newState.ActiveState {
		eventType := ServiceEventStateChange

		// Detect specific event types
		if newState.ActiveState == "active" && oldState.ActiveState != "active" {
			eventType = ServiceEventStart
		} else if newState.ActiveState == "inactive" && oldState.ActiveState == "active" {
			eventType = ServiceEventStop
		} else if newState.ActiveState == "failed" {
			eventType = ServiceEventFailure
			sm.recordFailure(newState.Name)
		}

		// Check for restart pattern
		if eventType == ServiceEventStart && oldState.ActiveState == "failed" {
			eventType = ServiceEventRestart
			sm.recordRestart(newState.Name)
		}

		event := &ServiceEvent{
			Timestamp:   time.Now(),
			ServiceName: newState.Name,
			EventType:   eventType,
			OldState:    oldState.ActiveState,
			NewState:    newState.ActiveState,
			Reason:      "state_change",
		}

		select {
		case sm.events <- event:
		default:
			// Drop event if buffer is full
		}
	}
}

// recordRestart records a service restart
func (sm *ServiceMonitor) recordRestart(serviceName string) {
	sm.statesMutex.Lock()
	defer sm.statesMutex.Unlock()

	tracker, exists := sm.restartCounts[serviceName]
	if !exists {
		tracker = &RestartTracker{
			RestartTimes: make([]time.Time, 0),
		}
		sm.restartCounts[serviceName] = tracker
	}

	now := time.Now()
	tracker.RestartTimes = append(tracker.RestartTimes, now)
	tracker.LastRestart = now
	tracker.TotalCount++

	// Keep only recent restarts within the window
	cutoff := now.Add(-sm.config.RestartWindow)
	var recentRestarts []time.Time
	for _, restartTime := range tracker.RestartTimes {
		if restartTime.After(cutoff) {
			recentRestarts = append(recentRestarts, restartTime)
		}
	}
	tracker.RestartTimes = recentRestarts
}

// recordFailure records a service failure
func (sm *ServiceMonitor) recordFailure(serviceName string) {
	sm.statesMutex.Lock()
	defer sm.statesMutex.Unlock()

	history, exists := sm.failureHistory[serviceName]
	if !exists {
		history = make([]time.Time, 0)
	}

	now := time.Now()
	history = append(history, now)

	// Keep only recent failures within the window
	cutoff := now.Add(-sm.config.RestartWindow)
	var recentFailures []time.Time
	for _, failureTime := range history {
		if failureTime.After(cutoff) {
			recentFailures = append(recentFailures, failureTime)
		}
	}

	sm.failureHistory[serviceName] = recentFailures
}

// Cleanup removes old tracking data
func (sm *ServiceMonitor) Cleanup() {
	sm.statesMutex.Lock()
	defer sm.statesMutex.Unlock()

	cutoff := time.Now().Add(-sm.config.RestartWindow)

	// Clean up restart trackers
	for _, tracker := range sm.restartCounts {
		var recentRestarts []time.Time
		for _, restartTime := range tracker.RestartTimes {
			if restartTime.After(cutoff) {
				recentRestarts = append(recentRestarts, restartTime)
			}
		}
		tracker.RestartTimes = recentRestarts
	}

	// Clean up failure history
	for serviceName, history := range sm.failureHistory {
		var recentFailures []time.Time
		for _, failureTime := range history {
			if failureTime.After(cutoff) {
				recentFailures = append(recentFailures, failureTime)
			}
		}
		sm.failureHistory[serviceName] = recentFailures
	}
}
