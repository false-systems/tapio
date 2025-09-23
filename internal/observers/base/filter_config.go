package base

import (
	"github.com/yairfalse/tapio/pkg/domain"
)

// FilterFunc is a function that determines if an event should be allowed
type FilterFunc func(*domain.CollectorEvent) bool

// FilterConfig represents the configuration for filters loaded from file
type FilterConfig struct {
	Version      int                  `yaml:"version"`
	AllowFilters []FilterRule         `yaml:"allow_filters"`
	DenyFilters  []FilterRule         `yaml:"deny_filters"`
	Metadata     FilterConfigMetadata `yaml:"metadata,omitempty"`
}

// FilterConfigMetadata contains metadata about the filter configuration
type FilterConfigMetadata struct {
	Description string   `yaml:"description,omitempty"`
	Author      string   `yaml:"author,omitempty"`
	LastUpdated string   `yaml:"last_updated,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

// FilterCondition represents a single filter condition
type FilterCondition struct {
	// Field to check (e.g., "severity", "type", "source")
	Field string `yaml:"field"`

	// Operator to use for comparison
	// Supported: "equals", "not_equals", "contains", "not_contains",
	//           "greater_than", "less_than", "regex", "in", "not_in"
	Operator string `yaml:"operator"`

	// Value to compare against
	Value interface{} `yaml:"value"`

	// CaseSensitive determines if string comparisons are case-sensitive
	CaseSensitive bool `yaml:"case_sensitive,omitempty"`
}

// FilterRule represents a single filter rule
type FilterRule struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description,omitempty"`
	Type        string           `yaml:"type"` // severity, event_type, network, dns, http, regex, time_based
	Conditions  FilterConditions `yaml:"conditions"`
	Enabled     bool             `yaml:"enabled,omitempty"`
}

// FilterConditions holds type-safe filter conditions
type FilterConditions struct {
	// Severity filters
	MinSeverity string `yaml:"min_severity,omitempty"`
	MaxSeverity string `yaml:"max_severity,omitempty"`

	// Event type filters
	Types []string `yaml:"types,omitempty"`

	// Network filters
	SourcePort int    `yaml:"source_port,omitempty"`
	DestPort   int    `yaml:"dest_port,omitempty"`
	Protocol   string `yaml:"protocol,omitempty"`

	// DNS filters
	DomainPattern string `yaml:"domain_pattern,omitempty"`

	// HTTP filters
	StatusCode int    `yaml:"status_code,omitempty"`
	Method     string `yaml:"method,omitempty"`
	URLPattern string `yaml:"url_pattern,omitempty"`

	// Regex filters
	Field   string `yaml:"field,omitempty"`
	Pattern string `yaml:"pattern,omitempty"`

	// Time-based filters
	StartTime string `yaml:"start_time,omitempty"`
	EndTime   string `yaml:"end_time,omitempty"`
}

// FilterStatistics contains filter statistics
type FilterStatistics struct {
	Version         int   `json:"version"`
	AllowFilters    int   `json:"allow_filters"`
	DenyFilters     int   `json:"deny_filters"`
	EventsProcessed int64 `json:"events_processed"`
	EventsAllowed   int64 `json:"events_allowed"`
	EventsDenied    int64 `json:"events_denied"`
}
