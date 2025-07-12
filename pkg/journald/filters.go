package journald

import (
	"fmt"
	"strings"
	"sync"
)

// Filters provides smart log filtering capabilities
type Filters struct {
	config          *FiltersConfig
	serviceFilters  map[string]bool
	ignoredServices map[string]bool
	logLevels       map[string]bool
	minLogLevel     int

	// Dynamic filters
	dynamicFilters []FilterRule
	mutex          sync.RWMutex
}

// FiltersConfig configures the log filters
type FiltersConfig struct {
	MonitoredServices []string
	IgnoredServices   []string
	LogLevels         []string
	MinLogLevel       string

	// Content filters
	IncludePatterns []string
	ExcludePatterns []string

	// Rate limiting
	EnableRateLimit    bool
	MaxEventsPerSecond map[string]int // per service
	BurstLimit         int
}

// FilterRule represents a dynamic filter rule
type FilterRule struct {
	ID        string
	Type      FilterType
	Pattern   string
	Action    FilterAction
	Condition FilterCondition
	Metadata  map[string]interface{}
}

// FilterType defines the type of filter
type FilterType int

const (
	FilterTypeService FilterType = iota
	FilterTypeMessage
	FilterTypePriority
	FilterTypeField
	FilterTypeRegex
)

// FilterAction defines what to do with matching entries
type FilterAction int

const (
	FilterActionInclude FilterAction = iota
	FilterActionExclude
	FilterActionModify
	FilterActionTag
)

// FilterCondition defines when to apply the filter
type FilterCondition struct {
	Field    string
	Operator string
	Value    interface{}
}

// NewFilters creates a new filters instance
func NewFilters(config *FiltersConfig) *Filters {
	filters := &Filters{
		config:          config,
		serviceFilters:  make(map[string]bool),
		ignoredServices: make(map[string]bool),
		logLevels:       make(map[string]bool),
		dynamicFilters:  make([]FilterRule, 0),
	}

	// Initialize service filters
	for _, service := range config.MonitoredServices {
		filters.serviceFilters[service] = true
	}

	for _, service := range config.IgnoredServices {
		filters.ignoredServices[service] = true
	}

	// Initialize log level filters
	for _, level := range config.LogLevels {
		filters.logLevels[level] = true
	}

	filters.minLogLevel = priorityNameToLevel(config.MinLogLevel)

	return filters
}

// ShouldProcess determines if a log entry should be processed
func (f *Filters) ShouldProcess(entry *LogEntry) bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	// Check ignored services first
	if f.ignoredServices[entry.Service] {
		return false
	}

	// Check monitored services
	if len(f.serviceFilters) > 0 && !f.serviceFilters[entry.Service] {
		return false
	}

	// Check log level
	if entry.Priority > f.minLogLevel {
		return false
	}

	if len(f.logLevels) > 0 && !f.logLevels[entry.PriorityName] {
		return false
	}

	// Apply dynamic filters
	for _, rule := range f.dynamicFilters {
		if f.matchesRule(entry, rule) {
			switch rule.Action {
			case FilterActionExclude:
				return false
			case FilterActionInclude:
				return true
			}
		}
	}

	// Apply content filters
	if !f.passesContentFilters(entry) {
		return false
	}

	return true
}

// AddService adds a service to the monitored list
func (f *Filters) AddService(serviceName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.serviceFilters[serviceName] = true
	delete(f.ignoredServices, serviceName)

	return nil
}

// RemoveService removes a service from the monitored list
func (f *Filters) RemoveService(serviceName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	delete(f.serviceFilters, serviceName)

	return nil
}

// IgnoreService adds a service to the ignored list
func (f *Filters) IgnoreService(serviceName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.ignoredServices[serviceName] = true
	delete(f.serviceFilters, serviceName)

	return nil
}

// AddDynamicFilter adds a dynamic filter rule
func (f *Filters) AddDynamicFilter(rule FilterRule) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// Remove existing rule with same ID
	for i, existing := range f.dynamicFilters {
		if existing.ID == rule.ID {
			f.dynamicFilters = append(f.dynamicFilters[:i], f.dynamicFilters[i+1:]...)
			break
		}
	}

	f.dynamicFilters = append(f.dynamicFilters, rule)
	return nil
}

// RemoveDynamicFilter removes a dynamic filter rule
func (f *Filters) RemoveDynamicFilter(ruleID string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	for i, rule := range f.dynamicFilters {
		if rule.ID == ruleID {
			f.dynamicFilters = append(f.dynamicFilters[:i], f.dynamicFilters[i+1:]...)
			return nil
		}
	}

	return nil
}

// SetMinLogLevel sets the minimum log level
func (f *Filters) SetMinLogLevel(level string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.minLogLevel = priorityNameToLevel(level)
	return nil
}

// AddLogLevel adds a log level to the filter
func (f *Filters) AddLogLevel(level string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.logLevels[level] = true
	return nil
}

// RemoveLogLevel removes a log level from the filter
func (f *Filters) RemoveLogLevel(level string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	delete(f.logLevels, level)
	return nil
}

// matchesRule checks if an entry matches a filter rule
func (f *Filters) matchesRule(entry *LogEntry, rule FilterRule) bool {
	switch rule.Type {
	case FilterTypeService:
		return entry.Service == rule.Pattern
	case FilterTypeMessage:
		return strings.Contains(strings.ToLower(entry.Message), strings.ToLower(rule.Pattern))
	case FilterTypePriority:
		if level := priorityNameToLevel(rule.Pattern); level >= 0 {
			return entry.Priority == level
		}
		return false
	case FilterTypeField:
		if value, exists := entry.Fields[rule.Condition.Field]; exists {
			return f.evaluateCondition(value, rule.Condition)
		}
		return false
	case FilterTypeRegex:
		// For now, simple contains check
		return strings.Contains(entry.Message, rule.Pattern)
	default:
		return false
	}
}

// evaluateCondition evaluates a filter condition
func (f *Filters) evaluateCondition(value interface{}, condition FilterCondition) bool {
	switch condition.Operator {
	case "equals":
		return value == condition.Value
	case "contains":
		if str, ok := value.(string); ok {
			if pattern, ok := condition.Value.(string); ok {
				return strings.Contains(strings.ToLower(str), strings.ToLower(pattern))
			}
		}
	case "greater_than":
		if num, ok := value.(int); ok {
			if threshold, ok := condition.Value.(int); ok {
				return num > threshold
			}
		}
	case "less_than":
		if num, ok := value.(int); ok {
			if threshold, ok := condition.Value.(int); ok {
				return num < threshold
			}
		}
	}
	return false
}

// passesContentFilters checks if entry passes content filters
func (f *Filters) passesContentFilters(entry *LogEntry) bool {
	message := strings.ToLower(entry.Message)

	// Check include patterns
	if len(f.config.IncludePatterns) > 0 {
		found := false
		for _, pattern := range f.config.IncludePatterns {
			if strings.Contains(message, strings.ToLower(pattern)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check exclude patterns
	for _, pattern := range f.config.ExcludePatterns {
		if strings.Contains(message, strings.ToLower(pattern)) {
			return false
		}
	}

	return true
}

// priorityNameToLevel converts priority name to numeric level
func priorityNameToLevel(name string) int {
	switch strings.ToLower(name) {
	case "emergency":
		return 0
	case "alert":
		return 1
	case "critical":
		return 2
	case "error":
		return 3
	case "warning":
		return 4
	case "notice":
		return 5
	case "info":
		return 6
	case "debug":
		return 7
	default:
		return 7 // Default to debug level
	}
}

// GetStatistics returns filter statistics
func (f *Filters) GetStatistics() map[string]interface{} {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	return map[string]interface{}{
		"monitored_services": len(f.serviceFilters),
		"ignored_services":   len(f.ignoredServices),
		"log_levels":         len(f.logLevels),
		"min_log_level":      f.minLogLevel,
		"dynamic_filters":    len(f.dynamicFilters),
	}
}

// GetMonitoredServices returns the list of monitored services
func (f *Filters) GetMonitoredServices() []string {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	services := make([]string, 0, len(f.serviceFilters))
	for service := range f.serviceFilters {
		services = append(services, service)
	}

	return services
}

// GetIgnoredServices returns the list of ignored services
func (f *Filters) GetIgnoredServices() []string {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	services := make([]string, 0, len(f.ignoredServices))
	for service := range f.ignoredServices {
		services = append(services, service)
	}

	return services
}

// GetDynamicFilters returns the current dynamic filters
func (f *Filters) GetDynamicFilters() []FilterRule {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	filters := make([]FilterRule, len(f.dynamicFilters))
	copy(filters, f.dynamicFilters)

	return filters
}

// ApplyPreset applies a predefined filter preset
func (f *Filters) ApplyPreset(presetName string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	switch presetName {
	case "errors_only":
		f.minLogLevel = 3 // error level
		f.logLevels = map[string]bool{
			"emergency": true,
			"alert":     true,
			"critical":  true,
			"error":     true,
		}
	case "warnings_and_errors":
		f.minLogLevel = 4 // warning level
		f.logLevels = map[string]bool{
			"emergency": true,
			"alert":     true,
			"critical":  true,
			"error":     true,
			"warning":   true,
		}
	case "system_services":
		f.serviceFilters = map[string]bool{
			"systemd":           true,
			"kernel":            true,
			"systemd-networkd":  true,
			"systemd-resolved":  true,
			"systemd-timesyncd": true,
		}
	case "container_services":
		f.serviceFilters = map[string]bool{
			"docker":     true,
			"containerd": true,
			"kubelet":    true,
			"kube-proxy": true,
		}
	default:
		return fmt.Errorf("unknown preset: %s", presetName)
	}

	return nil
}

// ClearFilters clears all filters
func (f *Filters) ClearFilters() {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.serviceFilters = make(map[string]bool)
	f.ignoredServices = make(map[string]bool)
	f.logLevels = make(map[string]bool)
	f.dynamicFilters = make([]FilterRule, 0)
	f.minLogLevel = 7 // debug level
}
