package sources

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/journald"
)

// JournaldSource implements the DataSource interface for journald log monitoring
type JournaldSource struct {
	reader          *journald.Reader
	eventClassifier *journald.EventClassifier
	patternMatcher  *journald.PatternMatcher
	filters         *journald.Filters

	// Configuration
	config    *JournaldConfig
	services  []string
	logLevels []string

	// State management
	mutex       sync.RWMutex
	isStarted   bool
	lastCollect time.Time

	// Event streams
	logEvents chan *journald.LogEvent
	ctx       context.Context
	cancel    context.CancelFunc

	// Performance tracking
	eventCount    uint64
	errorCount    uint64
	lastErrorTime time.Time
}

// JournaldConfig configures the journald monitoring source
type JournaldConfig struct {
	// Service filtering
	MonitoredServices []string `yaml:"monitored_services"`
	IgnoredServices   []string `yaml:"ignored_services"`

	// Log level filtering
	LogLevels   []string `yaml:"log_levels"`
	MinLogLevel string   `yaml:"min_log_level"`

	// Pattern matching
	ErrorPatterns       []string `yaml:"error_patterns"`
	WarningPatterns     []string `yaml:"warning_patterns"`
	SecurityPatterns    []string `yaml:"security_patterns"`
	PerformancePatterns []string `yaml:"performance_patterns"`

	// Event classification
	EnableClassification bool                          `yaml:"enable_classification"`
	ClassificationRules  []journald.ClassificationRule `yaml:"classification_rules"`

	// Performance settings
	EventBufferSize   int           `yaml:"event_buffer_size"`
	ReadBatchSize     int           `yaml:"read_batch_size"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
	ProcessingTimeout time.Duration `yaml:"processing_timeout"`

	// Resource limits
	MaxMemoryUsage     uint64        `yaml:"max_memory_usage"`
	MaxEventsPerSecond int           `yaml:"max_events_per_second"`
	HistoryRetention   time.Duration `yaml:"history_retention"`

	// Journald settings
	JournalPath       string        `yaml:"journal_path"`
	SeekToEnd         bool          `yaml:"seek_to_end"`
	FollowMode        bool          `yaml:"follow_mode"`
	ReconnectInterval time.Duration `yaml:"reconnect_interval"`
}

// DefaultJournaldConfig returns the default configuration
func DefaultJournaldConfig() *JournaldConfig {
	return &JournaldConfig{
		MonitoredServices: []string{
			"containerd",
			"docker",
			"kubelet",
			"kube-proxy",
			"systemd",
			"networkd",
			"resolved",
		},
		IgnoredServices: []string{
			"systemd-logind",
			"systemd-udevd",
			"cron",
		},
		LogLevels:   []string{"error", "warning", "notice", "info"},
		MinLogLevel: "warning",
		ErrorPatterns: []string{
			"error",
			"failed",
			"exception",
			"panic",
			"fatal",
			"critical",
			"emergency",
		},
		WarningPatterns: []string{
			"warning",
			"warn",
			"deprecated",
			"timeout",
			"retry",
			"fallback",
		},
		SecurityPatterns: []string{
			"authentication failed",
			"permission denied",
			"access denied",
			"unauthorized",
			"security violation",
			"intrusion",
		},
		PerformancePatterns: []string{
			"slow",
			"latency",
			"performance",
			"throttle",
			"backpressure",
			"resource exhausted",
		},
		EnableClassification: true,
		EventBufferSize:      50000,
		ReadBatchSize:        1000,
		ReadTimeout:          1 * time.Second,
		ProcessingTimeout:    500 * time.Millisecond,
		MaxMemoryUsage:       100 << 20, // 100MB
		MaxEventsPerSecond:   10000,
		HistoryRetention:     24 * time.Hour,
		JournalPath:          "/var/log/journal",
		SeekToEnd:            true,
		FollowMode:           true,
		ReconnectInterval:    5 * time.Second,
	}
}

// NewJournaldSource creates a new journald monitoring source
func NewJournaldSource(config *JournaldConfig) (*JournaldSource, error) {
	if config == nil {
		config = DefaultJournaldConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &JournaldSource{
		config:    config,
		services:  config.MonitoredServices,
		logLevels: config.LogLevels,
		logEvents: make(chan *journald.LogEvent, config.EventBufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Initialize journald components
	reader, err := journald.NewReader(&journald.ReaderConfig{
		JournalPath:       config.JournalPath,
		SeekToEnd:         config.SeekToEnd,
		FollowMode:        config.FollowMode,
		ReadBatchSize:     config.ReadBatchSize,
		ReadTimeout:       config.ReadTimeout,
		ReconnectInterval: config.ReconnectInterval,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create journald reader: %w", err)
	}
	source.reader = reader

	filters := journald.NewFilters(&journald.FiltersConfig{
		MonitoredServices: config.MonitoredServices,
		IgnoredServices:   config.IgnoredServices,
		LogLevels:         config.LogLevels,
		MinLogLevel:       config.MinLogLevel,
	})
	source.filters = filters

	patternMatcher := journald.NewPatternMatcher(&journald.PatternMatcherConfig{
		ErrorPatterns:       config.ErrorPatterns,
		WarningPatterns:     config.WarningPatterns,
		SecurityPatterns:    config.SecurityPatterns,
		PerformancePatterns: config.PerformancePatterns,
	})
	source.patternMatcher = patternMatcher

	if config.EnableClassification {
		eventClassifier, err := journald.NewEventClassifier(&journald.EventClassifierConfig{
			ClassificationRules: config.ClassificationRules,
			ProcessingTimeout:   config.ProcessingTimeout,
		})
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create event classifier: %w", err)
		}
		source.eventClassifier = eventClassifier
	}

	return source, nil
}

// GetType returns the source type
func (s *JournaldSource) GetType() correlation.SourceType {
	return correlation.SourceJournald
}

// IsAvailable checks if journald is available on the system
func (s *JournaldSource) IsAvailable() bool {
	return s.isStarted && s.reader != nil
}

// Start begins journald monitoring
func (s *JournaldSource) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isStarted {
		return fmt.Errorf("journald source already started")
	}

	// Start journald reader
	if err := s.reader.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start journald reader: %w", err)
	}

	// Start event classifier if enabled
	if s.eventClassifier != nil {
		if err := s.eventClassifier.Start(s.ctx); err != nil {
			return fmt.Errorf("failed to start event classifier: %w", err)
		}
	}

	// Start processing goroutines
	go s.processLogEntries()
	go s.monitorPerformance()

	s.isStarted = true
	s.lastCollect = time.Now()

	return nil
}

// Stop stops journald monitoring
func (s *JournaldSource) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isStarted {
		return nil
	}

	s.cancel()

	// Stop all components
	if s.reader != nil {
		s.reader.Stop()
	}
	if s.eventClassifier != nil {
		s.eventClassifier.Stop()
	}

	close(s.logEvents)
	s.isStarted = false

	return nil
}

// Collect returns current journald data
func (s *JournaldSource) Collect() (interface{}, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isStarted {
		return nil, fmt.Errorf("journald source not started")
	}

	// Get recent log events
	events := s.drainRecentEvents()

	// Get pattern matches
	patternMatchesRaw := s.patternMatcher.GetMatches()

	// Convert to interface{} map
	patternMatches := make(map[string]interface{})
	for k, v := range patternMatchesRaw {
		patternMatches[k] = v
	}

	// Get classification results if enabled
	var classifications map[string]interface{}
	if s.eventClassifier != nil {
		classifications = s.eventClassifier.GetClassifications()
	}

	// Get reader statistics
	readerStats := s.reader.GetStatistics()

	data := &correlation.JournaldData{
		Timestamp:       time.Now(),
		Events:          events,
		PatternMatches:  patternMatches,
		Classifications: classifications,
		Statistics: map[string]interface{}{
			"events_processed":   s.eventCount,
			"errors_encountered": s.errorCount,
			"last_error_time":    s.lastErrorTime,
			"reader_stats":       readerStats,
			"monitored_services": len(s.services),
			"active_patterns":    len(patternMatches),
		},
	}

	s.lastCollect = time.Now()
	return data, nil
}

// GetData retrieves journald data based on the request
func (s *JournaldSource) GetData(ctx context.Context, dataType string, params map[string]interface{}) (interface{}, error) {
	switch dataType {
	case "events":
		return s.drainRecentEvents(), nil
	case "patterns":
		return s.patternMatcher.GetMatches(), nil
	case "classifications":
		if s.eventClassifier != nil {
			return s.eventClassifier.GetClassifications(), nil
		}
		return nil, nil
	case "statistics":
		return s.getStatistics(), nil
	case "service_logs":
		if serviceName, ok := params["service"]; ok {
			return s.getServiceLogs(serviceName.(string)), nil
		}
		return nil, fmt.Errorf("service parameter required for service_logs")
	default:
		return s.Collect()
	}
}

// processLogEntries processes log entries from the journald reader
func (s *JournaldSource) processLogEntries() {
	logEntries := s.reader.GetEntryChannel()

	for {
		select {
		case <-s.ctx.Done():
			return
		case entry := <-logEntries:
			if entry != nil {
				s.processLogEntry(entry)
			}
		}
	}
}

// processLogEntry processes a single log entry
func (s *JournaldSource) processLogEntry(entry *journald.LogEntry) {
	// Apply filters
	if !s.filters.ShouldProcess(entry) {
		return
	}

	// Create log event
	logEvent := &journald.LogEvent{
		Timestamp: entry.Timestamp,
		Service:   entry.Service,
		Priority:  entry.Priority,
		Message:   entry.Message,
		Fields:    entry.Fields,
	}

	// Apply pattern matching
	patterns := s.patternMatcher.MatchEntry(entry)
	logEvent.MatchedPatterns = patterns

	// Apply classification if enabled
	if s.eventClassifier != nil {
		classification := s.eventClassifier.ClassifyEvent(logEvent)
		logEvent.Classification = classification
	}

	// Buffer event
	select {
	case s.logEvents <- logEvent:
		s.eventCount++
	default:
		// Drop event if buffer is full
		s.errorCount++
	}
}

// monitorPerformance monitors performance metrics
func (s *JournaldSource) monitorPerformance() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastEventCount uint64

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			currentCount := s.eventCount
			eventsPerSecond := (currentCount - lastEventCount) / 10

			// Check performance limits
			if int(eventsPerSecond) > s.config.MaxEventsPerSecond {
				// Implement throttling or alert
				s.errorCount++
				s.lastErrorTime = time.Now()
			}

			lastEventCount = currentCount
		}
	}
}

// drainRecentEvents drains recent events from the buffer
func (s *JournaldSource) drainRecentEvents() []*journald.LogEvent {
	var events []*journald.LogEvent

	for {
		select {
		case event := <-s.logEvents:
			events = append(events, event)
		default:
			return events
		}
	}
}

// getServiceLogs gets logs for a specific service
func (s *JournaldSource) getServiceLogs(serviceName string) []*journald.LogEvent {
	events := s.drainRecentEvents()
	var serviceEvents []*journald.LogEvent

	for _, event := range events {
		if event.Service == serviceName {
			serviceEvents = append(serviceEvents, event)
		}
	}

	return serviceEvents
}

// getStatistics returns current statistics
func (s *JournaldSource) getStatistics() map[string]interface{} {
	readerStats := s.reader.GetStatistics()
	patternMatches := s.patternMatcher.GetMatches()

	stats := map[string]interface{}{
		"events_processed":   s.eventCount,
		"errors_encountered": s.errorCount,
		"last_error_time":    s.lastErrorTime,
		"last_collect":       s.lastCollect,
		"is_started":         s.isStarted,
		"monitored_services": len(s.services),
		"active_patterns":    len(patternMatches),
		"reader_stats":       readerStats,
	}

	if s.eventClassifier != nil {
		classifications := s.eventClassifier.GetClassifications()
		stats["classifications"] = classifications
	}

	return stats
}

// GetEventChannel returns the event channel for real-time monitoring
func (s *JournaldSource) GetEventChannel() <-chan *journald.LogEvent {
	return s.logEvents
}

// AddMonitoredService adds a service to the monitoring list
func (s *JournaldSource) AddMonitoredService(serviceName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, existing := range s.services {
		if existing == serviceName {
			return nil // Already monitoring
		}
	}

	s.services = append(s.services, serviceName)
	return s.filters.AddService(serviceName)
}

// RemoveMonitoredService removes a service from the monitoring list
func (s *JournaldSource) RemoveMonitoredService(serviceName string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for i, existing := range s.services {
		if existing == serviceName {
			s.services = append(s.services[:i], s.services[i+1:]...)
			break
		}
	}

	return s.filters.RemoveService(serviceName)
}

// GetMonitoredServices returns the list of monitored services
func (s *JournaldSource) GetMonitoredServices() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make([]string, len(s.services))
	copy(result, s.services)
	return result
}
