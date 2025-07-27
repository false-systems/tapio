package patterns

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PatternLoader loads patterns from various sources
type PatternLoader struct {
	library *K8sPatternLibrary
}

// NewPatternLoader creates a new pattern loader
func NewPatternLoader(library *K8sPatternLibrary) *PatternLoader {
	return &PatternLoader{
		library: library,
	}
}

// LoadFromFile loads patterns from a YAML or JSON file
func (l *PatternLoader) LoadFromFile(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read pattern file: %w", err)
	}

	return l.LoadFromBytes(filename, data)
}

// LoadFromBytes loads patterns from byte data
func (l *PatternLoader) LoadFromBytes(name string, data []byte) error {
	var patterns PatternFile

	// Determine file type from name and unmarshal
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".yaml", ".yml":
		err := yaml.Unmarshal(data, &patterns)
		if err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	case ".json":
		err := json.Unmarshal(data, &patterns)
		if err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	default:
		// Try YAML first, then JSON
		if err := yaml.Unmarshal(data, &patterns); err != nil {
			if err := json.Unmarshal(data, &patterns); err != nil {
				return fmt.Errorf("failed to parse as YAML or JSON")
			}
		}
	}

	// Validate and convert patterns
	for _, def := range patterns.Patterns {
		if def.ID == "" {
			return fmt.Errorf("pattern missing ID")
		}

		pattern, err := l.convertPatternDefinition(def)
		if err != nil {
			return fmt.Errorf("failed to convert pattern %s: %w", def.ID, err)
		}
		if pattern != nil { // Skip disabled patterns
			l.library.addPattern(pattern)
		}
	}

	return nil
}

// LoadFromDirectory loads all pattern files from a directory
func (l *PatternLoader) LoadFromDirectory(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}

	// Also load JSON files
	jsonFiles, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	files = append(files, jsonFiles...)

	for _, file := range files {
		if err := l.LoadFromFile(file); err != nil {
			return fmt.Errorf("failed to load %s: %w", file, err)
		}
	}

	return nil
}

// PatternFile represents a file containing pattern definitions
type PatternFile struct {
	Version  string              `yaml:"version" json:"version"`
	Patterns []PatternDefinition `yaml:"patterns" json:"patterns"`
}

// PatternDefinition represents a pattern in configuration
type PatternDefinition struct {
	ID           string                `yaml:"id" json:"id"`
	Name         string                `yaml:"name" json:"name"`
	Category     string                `yaml:"category" json:"category"`
	Description  string                `yaml:"description" json:"description"`
	Indicators   []IndicatorDefinition `yaml:"indicators" json:"indicators"`
	Impact       ImpactDefinition      `yaml:"impact" json:"impact"`
	Correlations []string              `yaml:"correlations" json:"correlations"`
	Enabled      bool                  `yaml:"enabled" json:"enabled"`
	Tags         []string              `yaml:"tags" json:"tags"`
}

// IndicatorDefinition represents an indicator in configuration
type IndicatorDefinition struct {
	Type       string      `yaml:"type" json:"type"`
	Field      string      `yaml:"field" json:"field"`
	Condition  string      `yaml:"condition" json:"condition"`
	Value      interface{} `yaml:"value" json:"value"`
	TimeWindow string      `yaml:"time_window" json:"time_window"`
	Threshold  float64     `yaml:"threshold" json:"threshold"`
}

// ImpactDefinition represents impact in configuration
type ImpactDefinition struct {
	Severity        string `yaml:"severity" json:"severity"`
	Scope           string `yaml:"scope" json:"scope"`
	UserImpact      bool   `yaml:"user_impact" json:"user_impact"`
	DataRisk        bool   `yaml:"data_risk" json:"data_risk"`
	PerformanceRisk bool   `yaml:"performance_risk" json:"performance_risk"`
}

// convertPatternDefinition converts a pattern definition to internal format
func (l *PatternLoader) convertPatternDefinition(def PatternDefinition) (*K8sPattern, error) {
	// Skip disabled patterns
	if !def.Enabled && def.Enabled != false { // Check if explicitly set to false
		return nil, nil
	}

	pattern := &K8sPattern{
		ID:           def.ID,
		Name:         def.Name,
		Category:     PatternCategory(def.Category),
		Description:  def.Description,
		Correlations: def.Correlations,
	}

	// Convert indicators
	for _, indDef := range def.Indicators {
		indicator := PatternIndicator{
			Type:      IndicatorType(indDef.Type),
			Field:     indDef.Field,
			Condition: indDef.Condition,
			Value:     indDef.Value,
			Threshold: indDef.Threshold,
		}

		// Parse time window
		if indDef.TimeWindow != "" {
			duration, err := time.ParseDuration(indDef.TimeWindow)
			if err != nil {
				return nil, fmt.Errorf("invalid time window %s: %w", indDef.TimeWindow, err)
			}
			indicator.TimeWindow = duration
		}

		pattern.Indicators = append(pattern.Indicators, indicator)
	}

	// Convert impact
	pattern.Impact = PatternImpact{
		Severity:        def.Impact.Severity,
		Scope:           def.Impact.Scope,
		UserImpact:      def.Impact.UserImpact,
		DataRisk:        def.Impact.DataRisk,
		PerformanceRisk: def.Impact.PerformanceRisk,
	}

	return pattern, nil
}

// Example pattern file content:
/*
version: "1.0"
patterns:
  - id: custom-oom-pattern
    name: Custom OOM Kill Pattern
    category: resource
    description: Detects custom out-of-memory scenarios
    enabled: true
    tags: ["memory", "critical", "custom"]
    indicators:
      - type: event
        field: reason
        condition: equals
        value: OOMKilled
      - type: frequency
        field: container.restart
        threshold: 3
        time_window: 5m
    impact:
      severity: critical
      scope: pod
      user_impact: true
      performance_risk: true
    correlations:
      - memory-leak
      - resource-exhaustion
*/

// LoadFromAPI loads patterns from an API endpoint
func (l *PatternLoader) LoadFromAPI(endpoint string, apiKey string) error {
	// This would make HTTP requests to fetch patterns
	// Implementation depends on your API design
	return fmt.Errorf("API loading not implemented yet")
}

// LoadFromDatabase loads patterns from a database
func (l *PatternLoader) LoadFromDatabase(connectionString string) error {
	// This would connect to a database and fetch patterns
	// Implementation depends on your database choice
	return fmt.Errorf("database loading not implemented yet")
}

// WatchForUpdates monitors pattern sources for updates
func (l *PatternLoader) WatchForUpdates(dir string, callback func(pattern *K8sPattern)) error {
	// This would use fsnotify or similar to watch for file changes
	// and reload patterns dynamically
	return fmt.Errorf("watch not implemented yet")
}
