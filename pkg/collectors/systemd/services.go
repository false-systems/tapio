package systemd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/yairfalse/tapio/pkg/collectors/unified"
)

// ServiceMonitor monitors systemd services with high-performance event processing
type ServiceMonitor struct {
	// D-Bus connection
	dbus *DBusConnection

	// Configuration
	config ServiceMonitorConfig

	// Service tracking with efficient lookups
	services   map[string]*ServiceInfo
	servicesMu sync.RWMutex

	// Container runtime tracking
	containerServices map[string]*ContainerServiceInfo
	containerMu       sync.RWMutex

	// Event processing
	eventChan   chan *ServiceEvent
	eventBuffer *EventBuffer

	// Pattern detection
	restartPatterns *RestartPatternDetector

	// Dependency tracking
	dependencies *ServiceDependencyGraph

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State
	started atomic.Bool
	stopped atomic.Bool

	// Metrics
	eventsGenerated uint64
	eventsDropped   uint64
	servicesTracked uint64
}

// ServiceMonitorConfig configures the service monitor
type ServiceMonitorConfig struct {
	// Monitoring scope
	MonitorAllServices     bool
	ServiceWhitelist       []string
	ServiceBlacklist       []string
	ContainerRuntimeFilter []string // docker, containerd, cri-o

	// Event settings
	EventBufferSize    int
	MaxEventsPerSecond int
	EventBatchSize     int

	// Pattern detection
	RestartThreshold int           // Number of restarts to trigger pattern detection
	RestartWindow    time.Duration // Time window for restart counting
	AnomalyDetection bool

	// Dependency tracking
	TrackDependencies bool
	DependencyDepth   int // How deep to traverse dependency tree

	// Performance
	PollInterval         time.Duration
	StateRefreshInterval time.Duration
	WorkerCount          int
}

// DefaultServiceMonitorConfig returns optimized configuration
func DefaultServiceMonitorConfig() ServiceMonitorConfig {
	return ServiceMonitorConfig{
		MonitorAllServices:     false,
		ServiceWhitelist:       []string{},
		ServiceBlacklist:       []string{"systemd-", "user@"},
		ContainerRuntimeFilter: []string{"docker", "containerd", "cri-o", "kubelet"},
		EventBufferSize:        10000,
		MaxEventsPerSecond:     1000,
		EventBatchSize:         50,
		RestartThreshold:       3,
		RestartWindow:          5 * time.Minute,
		AnomalyDetection:       true,
		TrackDependencies:      true,
		DependencyDepth:        3,
		PollInterval:           1 * time.Second,
		StateRefreshInterval:   30 * time.Second,
		WorkerCount:            4,
	}
}

// ServiceInfo tracks detailed information about a service
type ServiceInfo struct {
	// Basic info
	Name        string
	Description string
	Type        string // service, socket, timer, etc.

	// Current state
	ActiveState string
	SubState    string
	LoadState   string

	// Process information
	MainPID    uint32
	ControlPID uint32
	StatusText string

	// Resource usage
	MemoryCurrent uint64
	MemoryPeak    uint64
	CPUUsageNSec  uint64
	TasksCurrent  uint64

	// Timing
	ActiveEnterTimestamp   time.Time
	ActiveExitTimestamp    time.Time
	InactiveEnterTimestamp time.Time
	InactiveExitTimestamp  time.Time

	// Restart tracking
	NRestarts        uint32
	RestartUSec      uint64
	TimeoutStartUSec uint64
	TimeoutStopUSec  uint64

	// Exit status
	ExecMainStatus int32
	ExecMainCode   int32

	// State tracking
	LastStateChange time.Time
	StateHistory    []StateTransition
	RestartHistory  []time.Time

	// Dependencies
	Requires   []string
	RequiredBy []string
	Wants      []string
	WantedBy   []string
	After      []string
	Before     []string

	// Monitoring metadata
	IsContainerRuntime bool
	IsCritical         bool
	MonitoringSince    time.Time
}

// ContainerServiceInfo tracks container runtime services
type ContainerServiceInfo struct {
	ServiceInfo
	Runtime        string // docker, containerd, cri-o
	ContainerCount int
	HealthStatus   string
}

// ServiceEvent represents a service state change or event
type ServiceEvent struct {
	Timestamp time.Time
	Service   string
	EventType ServiceEventType
	Severity  unified.Severity

	// State transition
	OldState string
	NewState string
	SubState string

	// Additional context
	PID        uint32
	ExitCode   int32
	ExitStatus string
	Signal     string

	// Resource info at event time
	MemoryUsage uint64
	CPUUsage    uint64

	// Pattern detection results
	RestartCount int
	IsAnomaly    bool
	Pattern      string

	// Related services (for dependency tracking)
	AffectedServices []string

	// Raw properties from D-Bus
	Properties map[string]interface{}
}

// ServiceEventType defines types of service events
type ServiceEventType string

const (
	ServiceStarted      ServiceEventType = "started"
	ServiceStopped      ServiceEventType = "stopped"
	ServiceFailed       ServiceEventType = "failed"
	ServiceRestarting   ServiceEventType = "restarting"
	ServiceReloaded     ServiceEventType = "reloaded"
	ServiceActivating   ServiceEventType = "activating"
	ServiceDeactivating ServiceEventType = "deactivating"
	ServiceRestarted    ServiceEventType = "restarted"
	ServiceAnomaly      ServiceEventType = "anomaly"
)

// StateTransition records a service state change
type StateTransition struct {
	Timestamp time.Time
	From      string
	To        string
	Reason    string
}

// EventBuffer provides efficient event batching
type EventBuffer struct {
	events  []*ServiceEvent
	mu      sync.Mutex
	maxSize int
	flushCh chan struct{}
}

// NewServiceMonitor creates a new service monitor
func NewServiceMonitor(dbus *DBusConnection, config ServiceMonitorConfig) (*ServiceMonitor, error) {
	ctx, cancel := context.WithCancel(context.Background())

	sm := &ServiceMonitor{
		dbus:              dbus,
		config:            config,
		services:          make(map[string]*ServiceInfo),
		containerServices: make(map[string]*ContainerServiceInfo),
		eventChan:         make(chan *ServiceEvent, config.EventBufferSize),
		ctx:               ctx,
		cancel:            cancel,
		eventBuffer: &EventBuffer{
			maxSize: config.EventBatchSize,
			flushCh: make(chan struct{}, 1),
		},
	}

	// Initialize pattern detector
	sm.restartPatterns = NewRestartPatternDetector(RestartPatternConfig{
		Window:           config.RestartWindow,
		Threshold:        config.RestartThreshold,
		AnomalyDetection: config.AnomalyDetection,
	})

	// Initialize dependency graph
	if config.TrackDependencies {
		sm.dependencies = NewServiceDependencyGraph()
	}

	return sm, nil
}

// Start begins monitoring services
func (sm *ServiceMonitor) Start() error {
	if !sm.started.CompareAndSwap(false, true) {
		return fmt.Errorf("service monitor already started")
	}

	// Subscribe to systemd signals
	if err := sm.dbus.SubscribeToSystemdSignals(); err != nil {
		return fmt.Errorf("failed to subscribe to signals: %w", err)
	}

	// Initial service discovery
	if err := sm.discoverServices(); err != nil {
		return fmt.Errorf("failed to discover services: %w", err)
	}

	// Start background workers
	sm.wg.Add(4)
	go sm.signalProcessor()
	go sm.statePoller()
	go sm.eventProcessor()
	go sm.patternAnalyzer()

	// Start event workers
	for i := 0; i < sm.config.WorkerCount; i++ {
		sm.wg.Add(1)
		go sm.eventWorker()
	}

	return nil
}

// Stop gracefully stops the service monitor
func (sm *ServiceMonitor) Stop() error {
	if !sm.stopped.CompareAndSwap(false, true) {
		return nil
	}

	sm.cancel()
	sm.wg.Wait()

	close(sm.eventChan)

	return nil
}

// GetEvents returns the event channel
func (sm *ServiceMonitor) GetEvents() <-chan *ServiceEvent {
	return sm.eventChan
}

// discoverServices performs initial service discovery
func (sm *ServiceMonitor) discoverServices() error {
	conn, err := sm.dbus.GetConnection()
	if err != nil {
		return err
	}

	// List all units
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	call := obj.Call("org.freedesktop.systemd1.Manager.ListUnits", 0)
	if call.Err != nil {
		return fmt.Errorf("failed to list units: %w", call.Err)
	}

	var units [][]interface{}
	if err := call.Store(&units); err != nil {
		return fmt.Errorf("failed to parse units: %w", err)
	}

	// Process units
	for _, unit := range units {
		if len(unit) < 10 {
			continue
		}

		name := unit[0].(string)
		if !sm.shouldMonitorService(name) {
			continue
		}

		// Create service info
		info := &ServiceInfo{
			Name:            name,
			Description:     unit[1].(string),
			LoadState:       unit[2].(string),
			ActiveState:     unit[3].(string),
			SubState:        unit[4].(string),
			MonitoringSince: time.Now(),
		}

		// Check if it's a container runtime
		if sm.isContainerRuntime(name) {
			containerInfo := &ContainerServiceInfo{
				ServiceInfo: *info,
				Runtime:     sm.identifyRuntime(name),
			}
			sm.containerMu.Lock()
			sm.containerServices[name] = containerInfo
			sm.containerMu.Unlock()
		} else {
			sm.servicesMu.Lock()
			sm.services[name] = info
			sm.servicesMu.Unlock()
		}

		atomic.AddUint64(&sm.servicesTracked, 1)

		// Get detailed properties
		go sm.updateServiceDetails(name)
	}

	// Build dependency graph if enabled
	if sm.config.TrackDependencies {
		sm.buildDependencyGraph()
	}

	return nil
}

// shouldMonitorService determines if a service should be monitored
func (sm *ServiceMonitor) shouldMonitorService(name string) bool {
	// Skip non-service units
	if !strings.HasSuffix(name, ".service") {
		return false
	}

	// Check blacklist
	for _, pattern := range sm.config.ServiceBlacklist {
		if strings.Contains(name, pattern) {
			return false
		}
	}

	// If monitoring all services, include it
	if sm.config.MonitorAllServices {
		return true
	}

	// Check whitelist
	if len(sm.config.ServiceWhitelist) > 0 {
		for _, pattern := range sm.config.ServiceWhitelist {
			if strings.Contains(name, pattern) {
				return true
			}
		}
		return false
	}

	// Check if it's a container runtime
	return sm.isContainerRuntime(name)
}

// isContainerRuntime checks if a service is a container runtime
func (sm *ServiceMonitor) isContainerRuntime(name string) bool {
	for _, runtime := range sm.config.ContainerRuntimeFilter {
		if strings.Contains(name, runtime) {
			return true
		}
	}
	return false
}

// identifyRuntime identifies which container runtime a service belongs to
func (sm *ServiceMonitor) identifyRuntime(name string) string {
	runtimes := []string{"docker", "containerd", "cri-o", "kubelet"}
	for _, runtime := range runtimes {
		if strings.Contains(name, runtime) {
			return runtime
		}
	}
	return "unknown"
}

// updateServiceDetails fetches detailed properties for a service
func (sm *ServiceMonitor) updateServiceDetails(serviceName string) {
	conn, err := sm.dbus.GetConnection()
	if err != nil {
		return
	}

	// Get unit object path
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var unitPath dbus.ObjectPath
	err = obj.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, serviceName).Store(&unitPath)
	if err != nil {
		return
	}

	// Get service properties
	unitObj := conn.Object("org.freedesktop.systemd1", unitPath)
	props := make(map[string]dbus.Variant)

	err = unitObj.Call("org.freedesktop.DBus.Properties.GetAll", 0,
		"org.freedesktop.systemd1.Service").Store(&props)
	if err != nil {
		return
	}

	// Update service info
	sm.updateServiceFromProperties(serviceName, props)
}

// updateServiceFromProperties updates service info from D-Bus properties
func (sm *ServiceMonitor) updateServiceFromProperties(serviceName string, props map[string]dbus.Variant) {
	sm.servicesMu.Lock()
	info, exists := sm.services[serviceName]
	sm.servicesMu.Unlock()

	if !exists {
		sm.containerMu.Lock()
		if containerInfo, ok := sm.containerServices[serviceName]; ok {
			info = &containerInfo.ServiceInfo
		}
		sm.containerMu.Unlock()

		if info == nil {
			return
		}
	}

	// Update properties
	if v, ok := props["MainPID"]; ok {
		info.MainPID = v.Value().(uint32)
	}
	if v, ok := props["MemoryCurrent"]; ok {
		info.MemoryCurrent = v.Value().(uint64)
	}
	if v, ok := props["CPUUsageNSec"]; ok {
		info.CPUUsageNSec = v.Value().(uint64)
	}
	if v, ok := props["NRestarts"]; ok {
		info.NRestarts = v.Value().(uint32)
	}
	if v, ok := props["ExecMainStatus"]; ok {
		info.ExecMainStatus = v.Value().(int32)
	}
	if v, ok := props["StatusText"]; ok {
		info.StatusText = v.Value().(string)
	}

	// Update dependencies if tracking is enabled
	if sm.config.TrackDependencies {
		sm.updateDependencies(serviceName, props)
	}
}

// signalProcessor processes D-Bus signals for service state changes
func (sm *ServiceMonitor) signalProcessor() {
	defer sm.wg.Done()

	signals := sm.dbus.GetSignals()

	for {
		select {
		case <-sm.ctx.Done():
			return

		case signal := <-signals:
			if signal == nil {
				continue
			}

			sm.processSignal(signal)
		}
	}
}

// processSignal processes a single D-Bus signal
func (sm *ServiceMonitor) processSignal(signal *dbus.Signal) {
	switch signal.Name {
	case "org.freedesktop.systemd1.Manager.UnitNew":
		if len(signal.Body) >= 2 {
			unitName := signal.Body[0].(string)
			if sm.shouldMonitorService(unitName) {
				sm.handleUnitNew(unitName)
			}
		}

	case "org.freedesktop.systemd1.Manager.UnitRemoved":
		if len(signal.Body) >= 2 {
			unitName := signal.Body[0].(string)
			sm.handleUnitRemoved(unitName)
		}

	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		if strings.HasPrefix(string(signal.Path), "/org/freedesktop/systemd1/unit/") {
			sm.handlePropertiesChanged(signal)
		}
	}
}

// handlePropertiesChanged processes property change signals
func (sm *ServiceMonitor) handlePropertiesChanged(signal *dbus.Signal) {
	if len(signal.Body) < 2 {
		return
	}

	// Extract unit name from path
	unitPath := string(signal.Path)
	unitName := sm.extractUnitNameFromPath(unitPath)
	if unitName == "" || !sm.shouldMonitorService(unitName) {
		return
	}

	// Get changed properties
	changedProps, ok := signal.Body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}

	// Check for state changes
	var stateChanged bool
	var newActiveState, newSubState string

	if v, ok := changedProps["ActiveState"]; ok {
		newActiveState = v.Value().(string)
		stateChanged = true
	}
	if v, ok := changedProps["SubState"]; ok {
		newSubState = v.Value().(string)
		stateChanged = true
	}

	if stateChanged {
		sm.handleStateChange(unitName, newActiveState, newSubState, changedProps)
	}

	// Update service properties
	sm.updateServiceFromProperties(unitName, changedProps)
}

// handleStateChange processes service state changes
func (sm *ServiceMonitor) handleStateChange(serviceName, newActiveState, newSubState string, props map[string]dbus.Variant) {
	sm.servicesMu.RLock()
	info, exists := sm.services[serviceName]
	sm.servicesMu.RUnlock()

	if !exists {
		return
	}

	oldState := info.ActiveState

	// Update state
	info.ActiveState = newActiveState
	info.SubState = newSubState
	info.LastStateChange = time.Now()

	// Add to state history
	transition := StateTransition{
		Timestamp: time.Now(),
		From:      oldState,
		To:        newActiveState,
		Reason:    sm.extractStateChangeReason(props),
	}
	info.StateHistory = append(info.StateHistory, transition)

	// Limit history size
	if len(info.StateHistory) > 100 {
		info.StateHistory = info.StateHistory[len(info.StateHistory)-100:]
	}

	// Generate event
	event := &ServiceEvent{
		Timestamp:    time.Now(),
		Service:      serviceName,
		EventType:    sm.determineEventType(oldState, newActiveState),
		Severity:     sm.determineSeverity(oldState, newActiveState, info),
		OldState:     oldState,
		NewState:     newActiveState,
		SubState:     newSubState,
		PID:          info.MainPID,
		MemoryUsage:  info.MemoryCurrent,
		CPUUsage:     info.CPUUsageNSec,
		RestartCount: int(info.NRestarts),
		Properties:   sm.variantMapToInterface(props),
	}

	// Check for restart patterns
	if event.EventType == ServiceRestarted || event.EventType == ServiceFailed {
		info.RestartHistory = append(info.RestartHistory, time.Now())
		if pattern := sm.restartPatterns.DetectPattern(serviceName, info.RestartHistory); pattern != nil {
			event.Pattern = pattern.Type
			event.IsAnomaly = pattern.IsAnomaly
		}
	}

	// Check affected services if dependency tracking is enabled
	if sm.config.TrackDependencies && (event.EventType == ServiceFailed || event.EventType == ServiceStopped) {
		event.AffectedServices = sm.dependencies.GetDependents(serviceName)
	}

	// Send event
	select {
	case sm.eventChan <- event:
		atomic.AddUint64(&sm.eventsGenerated, 1)
	default:
		atomic.AddUint64(&sm.eventsDropped, 1)
	}
}

// determineEventType determines the event type from state transition
func (sm *ServiceMonitor) determineEventType(oldState, newState string) ServiceEventType {
	switch {
	case oldState == "inactive" && newState == "active":
		return ServiceStarted
	case oldState == "active" && newState == "inactive":
		return ServiceStopped
	case oldState == "active" && newState == "failed":
		return ServiceFailed
	case oldState == "activating" && newState == "active":
		return ServiceStarted
	case oldState == "deactivating" && newState == "inactive":
		return ServiceStopped
	case newState == "activating":
		return ServiceActivating
	case newState == "deactivating":
		return ServiceDeactivating
	case oldState == "failed" && newState == "activating":
		return ServiceRestarting
	case oldState == "inactive" && newState == "active" && oldState != "":
		return ServiceRestarted
	default:
		return ServiceEventType(fmt.Sprintf("%s->%s", oldState, newState))
	}
}

// determineSeverity determines event severity based on service importance and state
func (sm *ServiceMonitor) determineSeverity(oldState, newState string, info *ServiceInfo) unified.Severity {
	// Critical services get higher severity
	if info.IsCritical || info.IsContainerRuntime {
		if newState == "failed" {
			return unified.SeverityCritical
		}
		if newState == "inactive" && oldState == "active" {
			return unified.SeverityHigh
		}
	}

	// Normal severity mapping
	switch newState {
	case "failed":
		return unified.SeverityHigh
	case "inactive":
		if oldState == "active" {
			return unified.SeverityMedium
		}
		return unified.SeverityLow
	case "active":
		return unified.SeverityLow
	default:
		return unified.SeverityDebug
	}
}

// Helper methods

// extractUnitNameFromPath extracts unit name from D-Bus object path
func (sm *ServiceMonitor) extractUnitNameFromPath(path string) string {
	// Path format: /org/freedesktop/systemd1/unit/docker_2eservice
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		return ""
	}

	// Decode systemd encoding (e.g., docker_2eservice -> docker.service)
	encoded := parts[len(parts)-1]
	return sm.decodeUnitName(encoded)
}

// decodeUnitName decodes systemd's D-Bus encoding
func (sm *ServiceMonitor) decodeUnitName(encoded string) string {
	// Simple decoder for common cases
	decoded := strings.ReplaceAll(encoded, "_2e", ".")
	decoded = strings.ReplaceAll(decoded, "_2d", "-")
	decoded = strings.ReplaceAll(decoded, "_5f", "_")
	return decoded
}

// extractStateChangeReason extracts reason for state change from properties
func (sm *ServiceMonitor) extractStateChangeReason(props map[string]dbus.Variant) string {
	if v, ok := props["Result"]; ok {
		return v.Value().(string)
	}
	if v, ok := props["StatusText"]; ok {
		return v.Value().(string)
	}
	return ""
}

// variantMapToInterface converts D-Bus variants to regular map
func (sm *ServiceMonitor) variantMapToInterface(variants map[string]dbus.Variant) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range variants {
		result[k] = v.Value()
	}
	return result
}

// handleUnitNew handles new unit creation
func (sm *ServiceMonitor) handleUnitNew(unitName string) {
	// Add to monitoring
	info := &ServiceInfo{
		Name:            unitName,
		MonitoringSince: time.Now(),
	}

	if sm.isContainerRuntime(unitName) {
		containerInfo := &ContainerServiceInfo{
			ServiceInfo: *info,
			Runtime:     sm.identifyRuntime(unitName),
		}
		sm.containerMu.Lock()
		sm.containerServices[unitName] = containerInfo
		sm.containerMu.Unlock()
	} else {
		sm.servicesMu.Lock()
		sm.services[unitName] = info
		sm.servicesMu.Unlock()
	}

	atomic.AddUint64(&sm.servicesTracked, 1)

	// Get initial properties
	go sm.updateServiceDetails(unitName)
}

// handleUnitRemoved handles unit removal
func (sm *ServiceMonitor) handleUnitRemoved(unitName string) {
	sm.servicesMu.Lock()
	delete(sm.services, unitName)
	sm.servicesMu.Unlock()

	sm.containerMu.Lock()
	delete(sm.containerServices, unitName)
	sm.containerMu.Unlock()

	atomic.AddUint64(&sm.servicesTracked, ^uint64(0)) // Decrement
}

// statePoller periodically polls service states
func (sm *ServiceMonitor) statePoller() {
	defer sm.wg.Done()

	ticker := time.NewTicker(sm.config.PollInterval)
	defer ticker.Stop()

	refreshTicker := time.NewTicker(sm.config.StateRefreshInterval)
	defer refreshTicker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return

		case <-ticker.C:
			// Quick state checks for critical services
			sm.checkCriticalServices()

		case <-refreshTicker.C:
			// Full state refresh
			sm.refreshAllServices()
		}
	}
}

// checkCriticalServices performs quick checks on critical services
func (sm *ServiceMonitor) checkCriticalServices() {
	sm.containerMu.RLock()
	criticalServices := make([]string, 0, len(sm.containerServices))
	for name := range sm.containerServices {
		criticalServices = append(criticalServices, name)
	}
	sm.containerMu.RUnlock()

	for _, service := range criticalServices {
		go sm.updateServiceDetails(service)
	}
}

// refreshAllServices performs full state refresh
func (sm *ServiceMonitor) refreshAllServices() {
	sm.servicesMu.RLock()
	services := make([]string, 0, len(sm.services))
	for name := range sm.services {
		services = append(services, name)
	}
	sm.servicesMu.RUnlock()

	// Update in batches to avoid overwhelming D-Bus
	batchSize := 10
	for i := 0; i < len(services); i += batchSize {
		end := i + batchSize
		if end > len(services) {
			end = len(services)
		}

		batch := services[i:end]
		for _, service := range batch {
			go sm.updateServiceDetails(service)
		}

		// Small delay between batches
		time.Sleep(100 * time.Millisecond)
	}
}

// eventProcessor processes and filters events
func (sm *ServiceMonitor) eventProcessor() {
	defer sm.wg.Done()

	// Rate limiter
	rateLimiter := time.NewTicker(time.Second / time.Duration(sm.config.MaxEventsPerSecond))
	defer rateLimiter.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return

		case <-rateLimiter.C:
			// Process events with rate limiting
			sm.processEventBatch()
		}
	}
}

// processEventBatch processes a batch of events
func (sm *ServiceMonitor) processEventBatch() {
	batch := sm.eventBuffer.GetBatch()
	if batch == nil {
		return
	}

	for _, event := range batch.events {
		// Apply filtering
		if sm.shouldFilterEvent(event) {
			continue
		}

		// Send event
		select {
		case sm.eventChan <- event:
			atomic.AddUint64(&sm.eventsGenerated, 1)
		default:
			atomic.AddUint64(&sm.eventsDropped, 1)
		}
	}
}

// shouldFilterEvent determines if an event should be filtered
func (sm *ServiceMonitor) shouldFilterEvent(event *ServiceEvent) bool {
	// Filter by severity
	if event.Severity < unified.SeverityLow {
		return true
	}

	// Filter repetitive events
	// This would need deduplication logic

	return false
}

// patternAnalyzer analyzes patterns in service behavior
func (sm *ServiceMonitor) patternAnalyzer() {
	defer sm.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return

		case <-ticker.C:
			sm.analyzePatterns()
		}
	}
}

// analyzePatterns performs pattern analysis
func (sm *ServiceMonitor) analyzePatterns() {
	// Get anomalous services
	anomalous := sm.restartPatterns.GetAnomalousServices()

	for _, service := range anomalous {
		pattern := sm.restartPatterns.GetPattern(service)
		if pattern == nil {
			continue
		}

		// Generate anomaly event
		event := &ServiceEvent{
			Timestamp: time.Now(),
			Service:   service,
			EventType: ServiceAnomaly,
			Severity:  unified.SeverityHigh,
			Pattern:   pattern.Type,
			IsAnomaly: true,
		}

		select {
		case sm.eventChan <- event:
		default:
		}
	}

	// Cleanup old patterns
	sm.restartPatterns.CleanupOldPatterns()
}

// eventWorker processes events concurrently
func (sm *ServiceMonitor) eventWorker() {
	defer sm.wg.Done()

	for {
		select {
		case <-sm.ctx.Done():
			return
		default:
			// Process events from buffer
			// This would implement the actual event processing logic
		}
	}
}

// buildDependencyGraph builds the service dependency graph
func (sm *ServiceMonitor) buildDependencyGraph() {
	sm.servicesMu.RLock()
	services := make([]string, 0, len(sm.services))
	for name := range sm.services {
		services = append(services, name)
	}
	sm.servicesMu.RUnlock()

	for _, service := range services {
		sm.updateDependencies(service, nil)
	}
}

// updateDependencies updates service dependencies
func (sm *ServiceMonitor) updateDependencies(serviceName string, props map[string]dbus.Variant) {
	if sm.dependencies == nil {
		return
	}

	// If props not provided, fetch them
	if props == nil {
		conn, err := sm.dbus.GetConnection()
		if err != nil {
			return
		}

		// Get unit properties
		obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		var unitPath dbus.ObjectPath
		err = obj.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, serviceName).Store(&unitPath)
		if err != nil {
			return
		}

		unitObj := conn.Object("org.freedesktop.systemd1", unitPath)
		props = make(map[string]dbus.Variant)
		err = unitObj.Call("org.freedesktop.DBus.Properties.GetAll", 0,
			"org.freedesktop.systemd1.Unit").Store(&props)
		if err != nil {
			return
		}
	}

	// Extract dependencies
	if v, ok := props["Requires"]; ok {
		if deps, ok := v.Value().([]string); ok {
			for _, dep := range deps {
				sm.dependencies.AddDependency(serviceName, dep)
			}
		}
	}

	if v, ok := props["Wants"]; ok {
		if deps, ok := v.Value().([]string); ok {
			for _, dep := range deps {
				sm.dependencies.AddDependency(serviceName, dep)
			}
		}
	}
}

// GetServiceInfo returns information about a specific service
func (sm *ServiceMonitor) GetServiceInfo(serviceName string) *ServiceInfo {
	sm.servicesMu.RLock()
	info := sm.services[serviceName]
	sm.servicesMu.RUnlock()

	if info == nil {
		sm.containerMu.RLock()
		if containerInfo, ok := sm.containerServices[serviceName]; ok {
			info = &containerInfo.ServiceInfo
		}
		sm.containerMu.RUnlock()
	}

	return info
}

// GetAllServices returns all monitored services
func (sm *ServiceMonitor) GetAllServices() map[string]*ServiceInfo {
	result := make(map[string]*ServiceInfo)

	sm.servicesMu.RLock()
	for name, info := range sm.services {
		result[name] = info
	}
	sm.servicesMu.RUnlock()

	sm.containerMu.RLock()
	for name, containerInfo := range sm.containerServices {
		result[name] = &containerInfo.ServiceInfo
	}
	sm.containerMu.RUnlock()

	return result
}

// GetStats returns monitoring statistics
func (sm *ServiceMonitor) GetStats() ServiceMonitorStats {
	sm.servicesMu.RLock()
	regularServices := len(sm.services)
	sm.servicesMu.RUnlock()

	sm.containerMu.RLock()
	containerServices := len(sm.containerServices)
	sm.containerMu.RUnlock()

	return ServiceMonitorStats{
		ServicesMonitored: uint64(regularServices + containerServices),
		ContainerServices: uint64(containerServices),
		EventsGenerated:   atomic.LoadUint64(&sm.eventsGenerated),
		EventsDropped:     atomic.LoadUint64(&sm.eventsDropped),
		ServicesTracked:   atomic.LoadUint64(&sm.servicesTracked),
	}
}

// ServiceMonitorStats contains monitoring statistics
type ServiceMonitorStats struct {
	ServicesMonitored uint64
	ContainerServices uint64
	EventsGenerated   uint64
	EventsDropped     uint64
	ServicesTracked   uint64
}

// EventBuffer methods

// Add adds an event to the buffer
func (eb *EventBuffer) Add(event *ServiceEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.events = append(eb.events, event)

	if len(eb.events) >= eb.maxSize {
		// Trigger flush
		select {
		case eb.flushCh <- struct{}{}:
		default:
		}
	}
}

// GetBatch retrieves and clears events from buffer
func (eb *EventBuffer) GetBatch() *EventBatch {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if len(eb.events) == 0 {
		return nil
	}

	batch := &EventBatch{
		events:    eb.events,
		timestamp: time.Now(),
	}

	eb.events = make([]*ServiceEvent, 0, eb.maxSize)

	return batch
}

// EventBatch represents a batch of events
type EventBatch struct {
	events    []*ServiceEvent
	timestamp time.Time
}
