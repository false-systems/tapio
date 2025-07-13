package collectors

import (
	"fmt"
	"sync"
)

// CollectorFactory manages the creation of different collector types
type CollectorFactory struct {
	factories map[string]Factory
	mu        sync.RWMutex
}

// globalFactory is the default factory instance
var globalFactory = NewCollectorFactory()

// NewCollectorFactory creates a new collector factory
func NewCollectorFactory() *CollectorFactory {
	factory := &CollectorFactory{
		factories: make(map[string]Factory),
	}
	
	// Register built-in collector factories
	factory.RegisterFactory("ebpf", NewEBPFCollectorFactory())
	factory.RegisterFactory("k8s", NewK8sCollectorFactory())
	factory.RegisterFactory("systemd", NewSystemdCollectorFactory())
	
	return factory
}

// RegisterFactory registers a factory for a specific collector type
func (cf *CollectorFactory) RegisterFactory(collectorType string, factory Factory) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	cf.factories[collectorType] = factory
}

// CreateCollector creates a collector of the specified type
func (cf *CollectorFactory) CreateCollector(collectorType string, config CollectorConfig) (Collector, error) {
	cf.mu.RLock()
	factory, exists := cf.factories[collectorType]
	cf.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("unknown collector type: %s", collectorType)
	}
	
	return factory.CreateCollector(config)
}

// GetSupportedTypes returns all supported collector types
func (cf *CollectorFactory) GetSupportedTypes() []string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	
	types := make([]string, 0, len(cf.factories))
	for collectorType := range cf.factories {
		types = append(types, collectorType)
	}
	
	return types
}

// ValidateConfig validates a configuration for a specific collector type
func (cf *CollectorFactory) ValidateConfig(config CollectorConfig) error {
	cf.mu.RLock()
	factory, exists := cf.factories[config.Type]
	cf.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("unknown collector type: %s", config.Type)
	}
	
	return factory.ValidateConfig(config)
}

// Global factory functions for convenience
func RegisterFactory(collectorType string, factory Factory) {
	globalFactory.RegisterFactory(collectorType, factory)
}

func CreateCollector(collectorType string, config *Config) (Collector, error) {
	collectorConfig := DefaultCollectorConfig(collectorType, collectorType)
	
	// Override with global config settings
	collectorConfig.SamplingRate = config.SamplingRate
	collectorConfig.MaxEventsPerSec = config.MaxEventsPerSec
	collectorConfig.EventBufferSize = config.BufferSize
	collectorConfig.MaxMemoryMB = config.Resources.MaxMemoryMB
	collectorConfig.MaxCPUMilli = config.Resources.MaxCPUMilli
	
	return globalFactory.CreateCollector(collectorType, collectorConfig)
}

func GetSupportedTypes() []string {
	return globalFactory.GetSupportedTypes()
}

// Pipeline implementation for event processing
type pipeline struct {
	filters      []Filter
	transformers []Transformer
	mu           sync.RWMutex
}

// NewPipeline creates a new event processing pipeline
func NewPipeline(config PipelineConfig) Pipeline {
	p := &pipeline{
		filters:      make([]Filter, 0),
		transformers: make([]Transformer, 0),
	}
	
	// Initialize filters if enabled
	if config.EnableFiltering {
		// Add default filters based on configuration
		if filter := createSeverityFilter(config.FilterConfig); filter != nil {
			p.AddFilter(filter)
		}
		if filter := createCategoryFilter(config.FilterConfig); filter != nil {
			p.AddFilter(filter)
		}
	}
	
	// Initialize transformers if enabled
	if config.EnableTransformation {
		// Add default transformers based on configuration
		if transformer := createContextEnricher(config.TransformerConfig); transformer != nil {
			p.AddTransformer(transformer)
		}
	}
	
	return p
}

// Process runs an event through the pipeline
func (p *pipeline) Process(ctx context.Context, event *Event) (*Event, error) {
	if event == nil {
		return nil, nil
	}
	
	currentEvent := event
	
	// Apply filters
	p.mu.RLock()
	filters := make([]Filter, len(p.filters))
	copy(filters, p.filters)
	p.mu.RUnlock()
	
	for _, filter := range filters {
		if !filter.ShouldInclude(currentEvent) {
			return nil, nil // Event filtered out
		}
	}
	
	// Apply transformers
	p.mu.RLock()
	transformers := make([]Transformer, len(p.transformers))
	copy(transformers, p.transformers)
	p.mu.RUnlock()
	
	for _, transformer := range transformers {
		transformedEvent, err := transformer.Transform(currentEvent)
		if err != nil {
			return nil, fmt.Errorf("transformation failed: %w", err)
		}
		currentEvent = transformedEvent
	}
	
	return currentEvent, nil
}

// AddFilter adds a filter to the pipeline
func (p *pipeline) AddFilter(filter Filter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.filters = append(p.filters, filter)
}

// AddTransformer adds a transformer to the pipeline
func (p *pipeline) AddTransformer(transformer Transformer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.transformers = append(p.transformers, transformer)
}

// Built-in filters and transformers

// severityFilter filters events based on minimum severity
type severityFilter struct {
	minSeverity Severity
}

func createSeverityFilter(config map[string]interface{}) Filter {
	if minSev, ok := config["min_severity"].(string); ok {
		return &severityFilter{minSeverity: Severity(minSev)}
	}
	return nil
}

func (sf *severityFilter) ShouldInclude(event *Event) bool {
	return sf.getSeverityLevel(event.Severity) >= sf.getSeverityLevel(sf.minSeverity)
}

func (sf *severityFilter) Configure(config map[string]interface{}) error {
	if minSev, ok := config["min_severity"].(string); ok {
		sf.minSeverity = Severity(minSev)
	}
	return nil
}

func (sf *severityFilter) getSeverityLevel(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	case SeverityDebug:
		return 0
	default:
		return 1
	}
}

// categoryFilter filters events based on included/excluded categories
type categoryFilter struct {
	includeCategories map[Category]bool
	excludeCategories map[Category]bool
}

func createCategoryFilter(config map[string]interface{}) Filter {
	filter := &categoryFilter{
		includeCategories: make(map[Category]bool),
		excludeCategories: make(map[Category]bool),
	}
	
	if include, ok := config["include_categories"].([]string); ok {
		for _, cat := range include {
			filter.includeCategories[Category(cat)] = true
		}
	}
	
	if exclude, ok := config["exclude_categories"].([]string); ok {
		for _, cat := range exclude {
			filter.excludeCategories[Category(cat)] = true
		}
	}
	
	return filter
}

func (cf *categoryFilter) ShouldInclude(event *Event) bool {
	// Check exclude list first
	if cf.excludeCategories[event.Category] {
		return false
	}
	
	// If include list is empty, include all (except excluded)
	if len(cf.includeCategories) == 0 {
		return true
	}
	
	// Check include list
	return cf.includeCategories[event.Category]
}

func (cf *categoryFilter) Configure(config map[string]interface{}) error {
	// Reset filters
	cf.includeCategories = make(map[Category]bool)
	cf.excludeCategories = make(map[Category]bool)
	
	if include, ok := config["include_categories"].([]string); ok {
		for _, cat := range include {
			cf.includeCategories[Category(cat)] = true
		}
	}
	
	if exclude, ok := config["exclude_categories"].([]string); ok {
		for _, cat := range exclude {
			cf.excludeCategories[Category(cat)] = true
		}
	}
	
	return nil
}

// contextEnricher enriches events with additional context
type contextEnricher struct {
	enrichWithEnvironment bool
	enrichWithLabels      bool
	customLabels          map[string]string
}

func createContextEnricher(config map[string]interface{}) Transformer {
	enricher := &contextEnricher{
		customLabels: make(map[string]string),
	}
	
	if env, ok := config["enrich_environment"].(bool); ok {
		enricher.enrichWithEnvironment = env
	}
	
	if labels, ok := config["enrich_labels"].(bool); ok {
		enricher.enrichWithLabels = labels
	}
	
	if custom, ok := config["custom_labels"].(map[string]string); ok {
		enricher.customLabels = custom
	}
	
	return enricher
}

func (ce *contextEnricher) Transform(event *Event) (*Event, error) {
	// Create a copy of the event
	enrichedEvent := *event
	
	// Initialize context if nil
	if enrichedEvent.Context == nil {
		enrichedEvent.Context = &EventContext{}
	}
	
	// Initialize labels if nil
	if enrichedEvent.Labels == nil {
		enrichedEvent.Labels = make(map[string]string)
	}
	
	// Enrich with environment information
	if ce.enrichWithEnvironment {
		if enrichedEvent.Context.Environment == "" {
			enrichedEvent.Context.Environment = determineEnvironment()
		}
	}
	
	// Add custom labels
	for key, value := range ce.customLabels {
		enrichedEvent.Labels[key] = value
	}
	
	return &enrichedEvent, nil
}

func (ce *contextEnricher) Configure(config map[string]interface{}) error {
	if env, ok := config["enrich_environment"].(bool); ok {
		ce.enrichWithEnvironment = env
	}
	
	if labels, ok := config["enrich_labels"].(bool); ok {
		ce.enrichWithLabels = labels
	}
	
	if custom, ok := config["custom_labels"].(map[string]string); ok {
		ce.customLabels = custom
	}
	
	return nil
}

// determineEnvironment attempts to determine the current environment
func determineEnvironment() string {
	// This would implement logic to determine environment
	// For now, return a default value
	return "unknown"
}