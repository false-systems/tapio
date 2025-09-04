package base

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// FilterFunc is a function that returns true if an event should be filtered (dropped)
type FilterFunc func(*domain.CollectorEvent) bool

// FilterConfig represents the configuration for filters
type FilterConfig struct {
	Version string       `yaml:"version"`
	Allow   []FilterRule `yaml:"allow,omitempty"`
	Deny    []FilterRule `yaml:"deny,omitempty"`
}

// FilterCondition represents the condition for a filter rule with typed fields
type FilterCondition struct {
	// Severity filter fields
	MinSeverity string `yaml:"min_severity,omitempty"`

	// Event type filter fields
	Types   []string `yaml:"types,omitempty"`
	Exclude bool     `yaml:"exclude,omitempty"`

	// Network filter fields
	SourceIP []string `yaml:"source_ip,omitempty"`
	DestIP   []string `yaml:"dest_ip,omitempty"`
	Ports    []int    `yaml:"ports,omitempty"`

	// DNS filter fields
	Domains    []string `yaml:"domains,omitempty"`
	QueryTypes []string `yaml:"query_types,omitempty"`

	// HTTP filter fields
	Paths       []string `yaml:"paths,omitempty"`
	Methods     []string `yaml:"methods,omitempty"`
	StatusCodes []int    `yaml:"status_codes,omitempty"`

	// Regex filter fields
	Field   string `yaml:"field,omitempty"`
	Pattern string `yaml:"pattern,omitempty"`

	// Time-based filter fields
	Hours string `yaml:"hours,omitempty"`

	// Sampling filter fields
	SampleRate float64 `yaml:"sample_rate,omitempty"`
}

// FilterRule defines a single filter rule
type FilterRule struct {
	Name        string          `yaml:"name"`
	Type        string          `yaml:"type"`
	Enabled     *bool           `yaml:"enabled,omitempty"` // nil means true
	Description string          `yaml:"description,omitempty"`
	Condition   FilterCondition `yaml:"condition"`
}

// FilterManager manages allow and deny filters for a collector
type FilterManager struct {
	mu            sync.RWMutex
	logger        *zap.Logger
	collectorName string

	// Named filters for easy management
	allowFilters map[string]FilterFunc
	denyFilters  map[string]FilterFunc

	// Config file path and watcher
	configPath      string
	watcher         *fsnotify.Watcher
	watcherStopChan chan struct{}

	// Statistics
	filterVersion   atomic.Int64
	eventsAllowed   atomic.Int64
	eventsDenied    atomic.Int64
	eventsProcessed atomic.Int64

	// Filter compilation
	compiler *FilterCompiler
}

// NewFilterManager creates a new filter manager
func NewFilterManager(collectorName string, logger *zap.Logger) *FilterManager {
	return &FilterManager{
		collectorName:   collectorName,
		logger:          logger,
		allowFilters:    make(map[string]FilterFunc),
		denyFilters:     make(map[string]FilterFunc),
		compiler:        NewFilterCompiler(logger),
		watcherStopChan: make(chan struct{}),
	}
}

// LoadFromFile loads filters from a YAML file
func (fm *FilterManager) LoadFromFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			fm.logger.Info("Filter config file not found, using no filters",
				zap.String("path", configPath))
			return nil
		}
		return fmt.Errorf("failed to read filter config: %w", err)
	}

	var config FilterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse filter config: %w", err)
	}

	return fm.ApplyConfig(&config)
}

// ApplyConfig applies a filter configuration
func (fm *FilterManager) ApplyConfig(config *FilterConfig) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Clear existing filters
	fm.allowFilters = make(map[string]FilterFunc)
	fm.denyFilters = make(map[string]FilterFunc)

	// Compile and add allow filters
	for _, rule := range config.Allow {
		if rule.Enabled != nil && !*rule.Enabled {
			continue // Skip disabled filters
		}

		filter, err := fm.compiler.CompileRule(&rule)
		if err != nil {
			fm.logger.Warn("Failed to compile allow filter",
				zap.String("name", rule.Name),
				zap.Error(err))
			continue
		}

		fm.allowFilters[rule.Name] = filter
		fm.logger.Info("Added allow filter",
			zap.String("collector", fm.collectorName),
			zap.String("filter", rule.Name),
			zap.String("type", rule.Type))
	}

	// Compile and add deny filters
	for _, rule := range config.Deny {
		if rule.Enabled != nil && !*rule.Enabled {
			continue // Skip disabled filters
		}

		filter, err := fm.compiler.CompileRule(&rule)
		if err != nil {
			fm.logger.Warn("Failed to compile deny filter",
				zap.String("name", rule.Name),
				zap.Error(err))
			continue
		}

		fm.denyFilters[rule.Name] = filter
		fm.logger.Info("Added deny filter",
			zap.String("collector", fm.collectorName),
			zap.String("filter", rule.Name),
			zap.String("type", rule.Type))
	}

	fm.filterVersion.Add(1)
	fm.logger.Info("Applied filter configuration",
		zap.String("collector", fm.collectorName),
		zap.String("version", config.Version),
		zap.Int("allow_filters", len(fm.allowFilters)),
		zap.Int("deny_filters", len(fm.denyFilters)))

	return nil
}

// WatchConfigFile starts watching the config file for changes
func (fm *FilterManager) WatchConfigFile(configPath string) error {
	fm.configPath = configPath

	// Initial load
	if err := fm.LoadFromFile(configPath); err != nil {
		fm.logger.Warn("Failed to load initial filter config",
			zap.String("path", configPath),
			zap.Error(err))
	}

	// Create watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	fm.watcher = watcher

	// Watch the directory, not the file (for ConfigMap updates)
	dir := filepath.Dir(configPath)
	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	// Start watching in background
	go fm.watchLoop(configPath)

	return nil
}

// watchLoop watches for config file changes
func (fm *FilterManager) watchLoop(configPath string) {
	filename := filepath.Base(configPath)

	for {
		select {
		case <-fm.watcherStopChan:
			return

		case event, ok := <-fm.watcher.Events:
			if !ok {
				return
			}

			// Check if it's our file
			if filepath.Base(event.Name) != filename {
				continue
			}

			// Reload on write or create events
			if event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Create == fsnotify.Create {
				fm.logger.Info("Filter config file changed, reloading",
					zap.String("collector", fm.collectorName),
					zap.String("file", configPath))

				// Small delay to ensure file write is complete
				time.Sleep(100 * time.Millisecond)

				if err := fm.LoadFromFile(configPath); err != nil {
					fm.logger.Error("Failed to reload filter config",
						zap.String("collector", fm.collectorName),
						zap.Error(err))
				}
			}

		case err, ok := <-fm.watcher.Errors:
			if !ok {
				return
			}
			fm.logger.Warn("Filter config watcher error",
				zap.String("collector", fm.collectorName),
				zap.Error(err))
		}
	}
}

// ShouldAllow checks if an event passes the filters
// Returns true if the event should be processed, false if it should be dropped
func (fm *FilterManager) ShouldAllow(event *domain.CollectorEvent) bool {
	fm.eventsProcessed.Add(1)

	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// Check allow filters first (if any exist)
	if len(fm.allowFilters) > 0 {
		allowed := false
		for _, filter := range fm.allowFilters {
			if !filter(event) { // Filter returns true to DROP
				allowed = true
				break
			}
		}
		if !allowed {
			fm.eventsDenied.Add(1)
			return false
		}
	}

	// Check deny filters
	for _, filter := range fm.denyFilters {
		if filter(event) { // Filter returns true to DROP
			fm.eventsDenied.Add(1)
			return false
		}
	}

	fm.eventsAllowed.Add(1)
	return true
}

// AddAllowFilter adds a named allow filter
func (fm *FilterManager) AddAllowFilter(name string, filter FilterFunc) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.allowFilters[name] = filter
	fm.filterVersion.Add(1)

	fm.logger.Info("Added allow filter",
		zap.String("collector", fm.collectorName),
		zap.String("filter", name))
}

// AddDenyFilter adds a named deny filter
func (fm *FilterManager) AddDenyFilter(name string, filter FilterFunc) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.denyFilters[name] = filter
	fm.filterVersion.Add(1)

	fm.logger.Info("Added deny filter",
		zap.String("collector", fm.collectorName),
		zap.String("filter", name))
}

// RemoveFilter removes a filter by name
func (fm *FilterManager) RemoveFilter(name string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	deleted := false
	if _, ok := fm.allowFilters[name]; ok {
		delete(fm.allowFilters, name)
		deleted = true
	}
	if _, ok := fm.denyFilters[name]; ok {
		delete(fm.denyFilters, name)
		deleted = true
	}

	if deleted {
		fm.filterVersion.Add(1)
		fm.logger.Info("Removed filter",
			zap.String("collector", fm.collectorName),
			zap.String("filter", name))
	}
}

// GetStatistics returns filter statistics
func (fm *FilterManager) GetStatistics() FilterStatistics {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	return FilterStatistics{
		Version:         fm.filterVersion.Load(),
		AllowFilters:    len(fm.allowFilters),
		DenyFilters:     len(fm.denyFilters),
		EventsProcessed: fm.eventsProcessed.Load(),
		EventsAllowed:   fm.eventsAllowed.Load(),
		EventsDenied:    fm.eventsDenied.Load(),
	}
}

// Stop stops watching the config file
func (fm *FilterManager) Stop() {
	if fm.watcher != nil {
		close(fm.watcherStopChan)
		fm.watcher.Close()
	}
}

// FilterStatistics contains filter statistics
type FilterStatistics struct {
	Version         int64 `json:"version"`
	AllowFilters    int   `json:"allow_filters"`
	DenyFilters     int   `json:"deny_filters"`
	EventsProcessed int64 `json:"events_processed"`
	EventsAllowed   int64 `json:"events_allowed"`
	EventsDenied    int64 `json:"events_denied"`
}

// FilterCompiler compiles filter rules into FilterFunc
type FilterCompiler struct {
	logger *zap.Logger
}

// NewFilterCompiler creates a new filter compiler
func NewFilterCompiler(logger *zap.Logger) *FilterCompiler {
	return &FilterCompiler{logger: logger}
}

// CompileRule compiles a filter rule into a FilterFunc
func (fc *FilterCompiler) CompileRule(rule *FilterRule) (FilterFunc, error) {
	switch rule.Type {
	case "severity":
		return fc.compileSeverityFilter(rule)
	case "event_type":
		return fc.compileEventTypeFilter(rule)
	case "network":
		return fc.compileNetworkFilter(rule)
	case "dns":
		return fc.compileDNSFilter(rule)
	case "http":
		return fc.compileHTTPFilter(rule)
	case "regex":
		return fc.compileRegexFilter(rule)
	case "time_based":
		return fc.compileTimeBasedFilter(rule)
	default:
		return nil, fmt.Errorf("unknown filter type: %s", rule.Type)
	}
}

// compileSeverityFilter creates a severity-based filter
func (fc *FilterCompiler) compileSeverityFilter(rule *FilterRule) (FilterFunc, error) {
	minSeverity := rule.Condition.MinSeverity
	if minSeverity == "" {
		return nil, fmt.Errorf("severity filter requires min_severity")
	}

	// Parse severity level
	var severity domain.EventSeverity
	switch strings.ToLower(minSeverity) {
	case "debug":
		severity = domain.EventSeverityDebug
	case "info":
		severity = domain.EventSeverityInfo
	case "warning", "warn":
		severity = domain.EventSeverityWarning
	case "error":
		severity = domain.EventSeverityError
	case "critical", "crit":
		severity = domain.EventSeverityCritical
	default:
		return nil, fmt.Errorf("unknown severity level: %s", minSeverity)
	}

	return func(event *domain.CollectorEvent) bool {
		return event.Severity < severity // Return true to DROP if below threshold
	}, nil
}

// compileEventTypeFilter creates an event type filter
func (fc *FilterCompiler) compileEventTypeFilter(rule *FilterRule) (FilterFunc, error) {
	types := rule.Condition.Types
	if len(types) == 0 {
		return nil, fmt.Errorf("event_type filter requires types array")
	}

	typeMap := make(map[domain.CollectorEventType]bool)
	for _, typeStr := range types {
		typeMap[domain.CollectorEventType(typeStr)] = true
	}

	exclude := rule.Condition.Exclude

	return func(event *domain.CollectorEvent) bool {
		_, exists := typeMap[event.Type]
		if exclude {
			return exists // DROP if type is in exclude list
		}
		return !exists // DROP if type is NOT in include list
	}, nil
}

// compileNetworkFilter creates a network-based filter
func (fc *FilterCompiler) compileNetworkFilter(rule *FilterRule) (FilterFunc, error) {
	return func(event *domain.CollectorEvent) bool {
		netData, ok := event.GetNetworkData()
		if !ok {
			return false // Not a network event, don't filter
		}

		// Check source IPs
		if len(rule.Condition.SourceIP) > 0 {
			for _, ip := range rule.Condition.SourceIP {
				if netData.SourceIP == ip {
					return true // DROP this IP
				}
			}
		}

		// Check destination IPs
		if len(rule.Condition.DestIP) > 0 {
			for _, ip := range rule.Condition.DestIP {
				if netData.DestIP == ip {
					return true // DROP this IP
				}
			}
		}

		// Check ports
		if len(rule.Condition.Ports) > 0 {
			for _, port := range rule.Condition.Ports {
				if int(netData.SourcePort) == port || int(netData.DestPort) == port {
					return true // DROP this port
				}
			}
		}

		return false
	}, nil
}

// compileDNSFilter creates a DNS-based filter
func (fc *FilterCompiler) compileDNSFilter(rule *FilterRule) (FilterFunc, error) {
	return func(event *domain.CollectorEvent) bool {
		dnsData, ok := event.GetDNSData()
		if !ok {
			return false // Not a DNS event, don't filter
		}

		// Check domains
		if len(rule.Condition.Domains) > 0 {
			for _, pattern := range rule.Condition.Domains {
				if matched, _ := filepath.Match(pattern, dnsData.QueryName); matched {
					return true // DROP matching domain
				}
			}
		}

		// Check query types
		if len(rule.Condition.QueryTypes) > 0 {
			for _, typeStr := range rule.Condition.QueryTypes {
				if dnsData.QueryType == typeStr {
					return true // DROP this query type
				}
			}
		}

		return false
	}, nil
}

// compileHTTPFilter creates an HTTP-based filter
func (fc *FilterCompiler) compileHTTPFilter(rule *FilterRule) (FilterFunc, error) {
	return func(event *domain.CollectorEvent) bool {
		httpData := event.EventData.HTTP
		if httpData == nil {
			return false // Not an HTTP event, don't filter
		}

		// Check paths
		if len(rule.Condition.Paths) > 0 {
			for _, pathStr := range rule.Condition.Paths {
				if strings.HasPrefix(httpData.URL, pathStr) {
					return true // DROP matching path
				}
			}
		}

		// Check methods
		if len(rule.Condition.Methods) > 0 {
			for _, methodStr := range rule.Condition.Methods {
				if httpData.Method == methodStr {
					return true // DROP this method
				}
			}
		}

		// Check status codes
		if len(rule.Condition.StatusCodes) > 0 {
			for _, codeNum := range rule.Condition.StatusCodes {
				if int(httpData.StatusCode) == codeNum {
					return true // DROP this status code
				}
			}
		}

		return false
	}, nil
}

// compileRegexFilter creates a regex-based filter
func (fc *FilterCompiler) compileRegexFilter(rule *FilterRule) (FilterFunc, error) {
	field := rule.Condition.Field
	if field == "" {
		return nil, fmt.Errorf("regex filter requires field")
	}

	pattern := rule.Condition.Pattern
	if pattern == "" {
		return nil, fmt.Errorf("regex filter requires pattern")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return func(event *domain.CollectorEvent) bool {
		// Extract field value based on field name
		var value string
		switch field {
		case "event_id":
			value = event.EventID
		case "source":
			value = event.Source
		case "type":
			value = string(event.Type)
		default:
			// Try to get from metadata
			if event.Metadata.Attributes != nil {
				value = event.Metadata.Attributes[field]
			}
		}

		return re.MatchString(value) // DROP if matches
	}, nil
}

// compileTimeBasedFilter creates a time-based filter
func (fc *FilterCompiler) compileTimeBasedFilter(rule *FilterRule) (FilterFunc, error) {
	hoursStr := rule.Condition.Hours
	if hoursStr == "" {
		return nil, fmt.Errorf("time_based filter requires hours")
	}

	// Parse hours range (e.g., "09:00-17:00")
	parts := strings.Split(hoursStr, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid hours format, expected HH:MM-HH:MM")
	}

	startTime, err := time.Parse("15:04", parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid start time: %w", err)
	}

	endTime, err := time.Parse("15:04", parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid end time: %w", err)
	}

	// Optional sample rate during this time
	sampleRate := rule.Condition.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	return func(event *domain.CollectorEvent) bool {
		now := time.Now()
		currentTime := time.Date(0, 1, 1, now.Hour(), now.Minute(), 0, 0, time.UTC)

		// Check if current time is within range
		if currentTime.After(startTime) && currentTime.Before(endTime) {
			// Apply sampling if configured
			if sampleRate < 1.0 {
				// Simple sampling: hash event ID for deterministic sampling
				hash := 0
				for _, c := range event.EventID {
					hash = hash*31 + int(c)
				}
				return float64(hash%100)/100.0 > sampleRate // DROP if above sample rate
			}
		}

		return false
	}, nil
}
