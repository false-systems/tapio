package sources

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/systemd"
)

// SystemdSource implements the DataSource interface for systemd service monitoring
type SystemdSource struct {
	collector     *systemd.Collector
	serviceMonitor *systemd.ServiceMonitor
	unitWatcher   *systemd.UnitWatcher
	patternDetector *systemd.PatternDetector
	
	// Configuration
	config        *SystemdConfig
	watchedServices []string
	
	// State management
	mutex         sync.RWMutex
	isStarted     bool
	lastCollect   time.Time
	
	// Event streams
	events        chan *systemd.ServiceEvent
	ctx           context.Context
	cancel        context.CancelFunc
}

// SystemdConfig configures the systemd monitoring source
type SystemdConfig struct {
	// Services to monitor
	WatchedServices []string `yaml:"watched_services"`
	
	// Pattern detection settings
	RestartThreshold     int           `yaml:"restart_threshold"`
	RestartWindow        time.Duration `yaml:"restart_window"`
	FailureThreshold     int           `yaml:"failure_threshold"`
	DependencyTracking   bool          `yaml:"dependency_tracking"`
	
	// Performance settings
	EventBufferSize      int           `yaml:"event_buffer_size"`
	CollectionInterval   time.Duration `yaml:"collection_interval"`
	CleanupInterval      time.Duration `yaml:"cleanup_interval"`
	HistoryRetention     time.Duration `yaml:"history_retention"`
	
	// D-Bus settings
	SystemBusTimeout     time.Duration `yaml:"system_bus_timeout"`
	ReconnectInterval    time.Duration `yaml:"reconnect_interval"`
	MaxReconnectAttempts int           `yaml:"max_reconnect_attempts"`
}

// DefaultSystemdConfig returns the default configuration
func DefaultSystemdConfig() *SystemdConfig {
	return &SystemdConfig{
		WatchedServices: []string{
			"containerd.service",
			"docker.service",
			"kubelet.service",
			"kube-proxy.service",
			"systemd-resolved.service",
			"systemd-networkd.service",
			"systemd-timesyncd.service",
		},
		RestartThreshold:     3,
		RestartWindow:        5 * time.Minute,
		FailureThreshold:     2,
		DependencyTracking:   true,
		EventBufferSize:      10000,
		CollectionInterval:   1 * time.Second,
		CleanupInterval:      5 * time.Minute,
		HistoryRetention:     30 * time.Minute,
		SystemBusTimeout:     10 * time.Second,
		ReconnectInterval:    5 * time.Second,
		MaxReconnectAttempts: 3,
	}
}

// NewSystemdSource creates a new systemd monitoring source
func NewSystemdSource(config *SystemdConfig) (*SystemdSource, error) {
	if config == nil {
		config = DefaultSystemdConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	source := &SystemdSource{
		config:          config,
		watchedServices: config.WatchedServices,
		events:          make(chan *systemd.ServiceEvent, config.EventBufferSize),
		ctx:             ctx,
		cancel:          cancel,
	}
	
	// Initialize systemd components
	collector, err := systemd.NewCollector(&systemd.CollectorConfig{
		SystemBusTimeout:     config.SystemBusTimeout,
		ReconnectInterval:    config.ReconnectInterval,
		MaxReconnectAttempts: config.MaxReconnectAttempts,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create systemd collector: %w", err)
	}
	source.collector = collector
	
	serviceMonitor, err := systemd.NewServiceMonitor(&systemd.ServiceMonitorConfig{
		WatchedServices:    config.WatchedServices,
		RestartThreshold:   config.RestartThreshold,
		RestartWindow:      config.RestartWindow,
		FailureThreshold:   config.FailureThreshold,
		DependencyTracking: config.DependencyTracking,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create service monitor: %w", err)
	}
	source.serviceMonitor = serviceMonitor
	
	unitWatcher, err := systemd.NewUnitWatcher(&systemd.UnitWatcherConfig{
		WatchedServices: config.WatchedServices,
		EventBufferSize: config.EventBufferSize,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create unit watcher: %w", err)
	}
	source.unitWatcher = unitWatcher
	
	patternDetector := systemd.NewPatternDetector(&systemd.PatternDetectorConfig{
		RestartThreshold:  config.RestartThreshold,
		RestartWindow:     config.RestartWindow,
		HistoryRetention:  config.HistoryRetention,
	})
	source.patternDetector = patternDetector
	
	return source, nil
}

// GetType returns the source type
func (s *SystemdSource) GetType() correlation.SourceType {
	return correlation.SourceSystemd
}

// IsAvailable checks if systemd is available on the system
func (s *SystemdSource) IsAvailable() bool {
	return s.isStarted && s.serviceMonitor != nil
}

// Start begins systemd monitoring
func (s *SystemdSource) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.isStarted {
		return fmt.Errorf("systemd source already started")
	}
	
	// Start systemd collector
	if err := s.collector.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start systemd collector: %w", err)
	}
	
	// Start service monitoring
	if err := s.serviceMonitor.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start service monitor: %w", err)
	}
	
	// Start unit watching
	if err := s.unitWatcher.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start unit watcher: %w", err)
	}
	
	// Start pattern detection
	if err := s.patternDetector.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start pattern detector: %w", err)
	}
	
	// Start event processing goroutines
	go s.processServiceEvents()
	go s.processUnitEvents()
	go s.runCleanup()
	
	s.isStarted = true
	s.lastCollect = time.Now()
	
	return nil
}

// Stop stops systemd monitoring
func (s *SystemdSource) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if !s.isStarted {
		return nil
	}
	
	s.cancel()
	
	// Stop all components
	if s.collector != nil {
		s.collector.Stop()
	}
	if s.serviceMonitor != nil {
		s.serviceMonitor.Stop()
	}
	if s.unitWatcher != nil {
		s.unitWatcher.Stop()
	}
	if s.patternDetector != nil {
		s.patternDetector.Stop()
	}
	
	close(s.events)
	s.isStarted = false
	
	return nil
}

// Collect returns current systemd data
func (s *SystemdSource) Collect() (interface{}, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if !s.isStarted {
		return nil, fmt.Errorf("systemd source not started")
	}
	
	// Collect service states
	serviceStatesRaw, err := s.serviceMonitor.GetServiceStates()
	if err != nil {
		return nil, fmt.Errorf("failed to get service states: %w", err)
	}
	
	// Convert to interface{} map
	serviceStates := make(map[string]interface{})
	for k, v := range serviceStatesRaw {
		serviceStates[k] = v
	}
	
	// Collect unit information
	unitInfo, err := s.unitWatcher.GetUnitInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get unit info: %w", err)
	}
	
	// Collect detected patterns
	patterns := s.patternDetector.GetDetectedPatterns()
	
	// Get recent events
	events := s.drainRecentEvents()
	
	data := &correlation.SystemdData{
		Timestamp:     time.Now(),
		ServiceStates: serviceStates,
		UnitInfo:      unitInfo,
		Patterns:      patterns,
		Events:        events,
		Statistics: map[string]interface{}{
			"monitored_services": len(s.watchedServices),
			"active_services":    s.countActiveServices(serviceStatesRaw),
			"failed_services":    s.countFailedServices(serviceStatesRaw),
			"restart_patterns":   len(patterns.RestartLoops),
			"dependency_failures": len(patterns.DependencyFailures),
		},
	}
	
	s.lastCollect = time.Now()
	return data, nil
}

// GetData retrieves systemd data based on the request
func (s *SystemdSource) GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error) {
	switch dataType {
	case "service_states":
		return s.serviceMonitor.GetServiceStates()
	case "unit_info":
		return s.unitWatcher.GetUnitInfo()
	case "patterns":
		return s.patternDetector.GetDetectedPatterns(), nil
	case "events":
		return s.drainRecentEvents(), nil
	case "statistics":
		return s.getStatistics(), nil
	case "service_dependencies":
		if serviceName, ok := params["service"]; ok {
			return s.serviceMonitor.GetServiceDependencies(serviceName.(string))
		}
		return nil, fmt.Errorf("service parameter required for service_dependencies")
	default:
		return s.Collect()
	}
}

// processServiceEvents processes events from the service monitor
func (s *SystemdSource) processServiceEvents() {
	serviceEvents := s.serviceMonitor.GetEventChannel()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-serviceEvents:
			if event != nil {
				// Feed event to pattern detector
				s.patternDetector.ProcessEvent(event)
				
				// Buffer event for collection
				select {
				case s.events <- event:
				default:
					// Drop event if buffer is full
				}
			}
		}
	}
}

// processUnitEvents processes events from the unit watcher
func (s *SystemdSource) processUnitEvents() {
	unitEvents := s.unitWatcher.GetEventChannel()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-unitEvents:
			if event != nil {
				// Convert unit event to service event
				serviceEvent := s.convertUnitEventToServiceEvent(event)
				if serviceEvent != nil {
					// Feed event to pattern detector
					s.patternDetector.ProcessEvent(serviceEvent)
					
					// Buffer event for collection
					select {
					case s.events <- serviceEvent:
					default:
						// Drop event if buffer is full
					}
				}
			}
		}
	}
}

// runCleanup periodically cleans up old data
func (s *SystemdSource) runCleanup() {
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.patternDetector.Cleanup()
			s.serviceMonitor.Cleanup()
		}
	}
}

// drainRecentEvents drains recent events from the buffer
func (s *SystemdSource) drainRecentEvents() []*systemd.ServiceEvent {
	var events []*systemd.ServiceEvent
	
	for {
		select {
		case event := <-s.events:
			events = append(events, event)
		default:
			return events
		}
	}
}

// countActiveServices counts the number of active services
func (s *SystemdSource) countActiveServices(serviceStates map[string]*systemd.ServiceState) int {
	count := 0
	for _, state := range serviceStates {
		if state.ActiveState == "active" {
			count++
		}
	}
	return count
}

// countFailedServices counts the number of failed services
func (s *SystemdSource) countFailedServices(serviceStates map[string]*systemd.ServiceState) int {
	count := 0
	for _, state := range serviceStates {
		if state.ActiveState == "failed" {
			count++
		}
	}
	return count
}

// convertUnitEventToServiceEvent converts a unit event to a service event
func (s *SystemdSource) convertUnitEventToServiceEvent(unitEvent *systemd.UnitEvent) *systemd.ServiceEvent {
	// Convert unit event to service event format
	return &systemd.ServiceEvent{
		Timestamp:   unitEvent.Timestamp,
		ServiceName: unitEvent.UnitName,
		EventType:   systemd.ServiceEventType(unitEvent.EventType),
		OldState:    unitEvent.OldState,
		NewState:    unitEvent.NewState,
		Reason:      unitEvent.Reason,
		Properties:  unitEvent.Properties,
	}
}

// getStatistics returns current statistics
func (s *SystemdSource) getStatistics() map[string]interface{} {
	serviceStates, _ := s.serviceMonitor.GetServiceStates()
	patterns := s.patternDetector.GetDetectedPatterns()
	
	return map[string]interface{}{
		"monitored_services":     len(s.watchedServices),
		"active_services":        s.countActiveServices(serviceStates),
		"failed_services":        s.countFailedServices(serviceStates),
		"restart_patterns":       len(patterns.RestartLoops),
		"dependency_failures":    len(patterns.DependencyFailures),
		"memory_pressure_events": len(patterns.MemoryPressure),
		"last_collect":           s.lastCollect,
		"is_started":             s.isStarted,
	}
}

// GetEventChannel returns the event channel for real-time monitoring
func (s *SystemdSource) GetEventChannel() <-chan *systemd.ServiceEvent {
	return s.events
}

// AddWatchedService adds a service to the watch list
func (s *SystemdSource) AddWatchedService(serviceName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	for _, existing := range s.watchedServices {
		if existing == serviceName {
			return nil // Already watching
		}
	}
	
	s.watchedServices = append(s.watchedServices, serviceName)
	
	// Update monitors if already started
	if s.isStarted {
		if err := s.serviceMonitor.AddService(serviceName); err != nil {
			return fmt.Errorf("failed to add service to monitor: %w", err)
		}
		if err := s.unitWatcher.AddUnit(serviceName); err != nil {
			return fmt.Errorf("failed to add unit to watcher: %w", err)
		}
	}
	
	return nil
}

// RemoveWatchedService removes a service from the watch list
func (s *SystemdSource) RemoveWatchedService(serviceName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	for i, existing := range s.watchedServices {
		if existing == serviceName {
			s.watchedServices = append(s.watchedServices[:i], s.watchedServices[i+1:]...)
			break
		}
	}
	
	// Update monitors if already started
	if s.isStarted {
		if err := s.serviceMonitor.RemoveService(serviceName); err != nil {
			return fmt.Errorf("failed to remove service from monitor: %w", err)
		}
		if err := s.unitWatcher.RemoveUnit(serviceName); err != nil {
			return fmt.Errorf("failed to remove unit from watcher: %w", err)
		}
	}
	
	return nil
}

// GetWatchedServices returns the list of watched services
func (s *SystemdSource) GetWatchedServices() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	result := make([]string, len(s.watchedServices))
	copy(result, s.watchedServices)
	return result
}