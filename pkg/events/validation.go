package events

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ValidationRule defines a validation rule for events
type ValidationRule interface {
	Validate(event *UnifiedEvent) error
	Name() string
}

// Validator provides comprehensive event validation
type Validator struct {
	rules   []ValidationRule
	mu      sync.RWMutex
	stats   ValidationStats
	enabled bool
}

// ValidationStats tracks validation metrics
type ValidationStats struct {
	TotalValidated   uint64
	ValidationPassed uint64
	ValidationFailed uint64
	RulesFired       map[string]uint64
	mu               sync.RWMutex
}

// NewValidator creates a new event validator with default rules
func NewValidator() *Validator {
	v := &Validator{
		enabled: true,
		stats: ValidationStats{
			RulesFired: make(map[string]uint64),
		},
	}
	
	// Add default validation rules
	v.AddRule(&RequiredFieldsRule{})
	v.AddRule(&TimestampRule{})
	v.AddRule(&EventTypeRule{})
	v.AddRule(&EntityContextRule{})
	v.AddRule(&NetworkEventRule{})
	v.AddRule(&MemoryEventRule{})
	v.AddRule(&AttributeSizeRule{})
	v.AddRule(&LabelFormatRule{})
	
	return v
}

// AddRule adds a validation rule
func (v *Validator) AddRule(rule ValidationRule) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.rules = append(v.rules, rule)
}

// Validate runs all validation rules on an event
func (v *Validator) Validate(event *UnifiedEvent) error {
	if !v.enabled {
		return nil
	}
	
	v.stats.mu.Lock()
	v.stats.TotalValidated++
	v.stats.mu.Unlock()
	
	var errors []string
	
	v.mu.RLock()
	rules := v.rules
	v.mu.RUnlock()
	
	for _, rule := range rules {
		if err := rule.Validate(event); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", rule.Name(), err))
			
			v.stats.mu.Lock()
			v.stats.RulesFired[rule.Name()]++
			v.stats.mu.Unlock()
		}
	}
	
	if len(errors) > 0 {
		v.stats.mu.Lock()
		v.stats.ValidationFailed++
		v.stats.mu.Unlock()
		return fmt.Errorf("validation failed: %s", strings.Join(errors, "; "))
	}
	
	v.stats.mu.Lock()
	v.stats.ValidationPassed++
	v.stats.mu.Unlock()
	
	return nil
}

// GetStats returns validation statistics
func (v *Validator) GetStats() ValidationStats {
	v.stats.mu.RLock()
	defer v.stats.mu.RUnlock()
	
	// Create a copy
	stats := ValidationStats{
		TotalValidated:   v.stats.TotalValidated,
		ValidationPassed: v.stats.ValidationPassed,
		ValidationFailed: v.stats.ValidationFailed,
		RulesFired:       make(map[string]uint64),
	}
	
	for k, v := range v.stats.RulesFired {
		stats.RulesFired[k] = v
	}
	
	return stats
}

// Built-in validation rules

// RequiredFieldsRule validates that required fields are present
type RequiredFieldsRule struct{}

func (r *RequiredFieldsRule) Name() string { return "required_fields" }

func (r *RequiredFieldsRule) Validate(event *UnifiedEvent) error {
	if event.Id == "" {
		return fmt.Errorf("event ID is required")
	}
	
	if event.Timestamp == nil {
		return fmt.Errorf("timestamp is required")
	}
	
	if event.Metadata == nil {
		return fmt.Errorf("metadata is required")
	}
	
	if event.Metadata.Type == "" {
		return fmt.Errorf("event type is required")
	}
	
	if event.Source == nil {
		return fmt.Errorf("source is required")
	}
	
	if event.Source.Type == "" {
		return fmt.Errorf("source type is required")
	}
	
	return nil
}

// TimestampRule validates timestamp sanity
type TimestampRule struct {
	MaxFuture time.Duration
	MaxPast   time.Duration
}

func (r *TimestampRule) Name() string { return "timestamp" }

func (r *TimestampRule) Validate(event *UnifiedEvent) error {
	if event.Timestamp == nil {
		return nil // Required fields rule handles this
	}
	
	now := time.Now()
	eventTime := event.Timestamp.AsTime()
	
	maxFuture := r.MaxFuture
	if maxFuture == 0 {
		maxFuture = 5 * time.Minute
	}
	
	maxPast := r.MaxPast
	if maxPast == 0 {
		maxPast = 24 * time.Hour
	}
	
	if eventTime.After(now.Add(maxFuture)) {
		return fmt.Errorf("timestamp is too far in the future: %v", eventTime)
	}
	
	if eventTime.Before(now.Add(-maxPast)) {
		return fmt.Errorf("timestamp is too far in the past: %v", eventTime)
	}
	
	return nil
}

// EventTypeRule validates event type format
type EventTypeRule struct{}

func (r *EventTypeRule) Name() string { return "event_type" }

var eventTypePattern = regexp.MustCompile(`^[a-z]+(\.[a-z]+)*$`)

func (r *EventTypeRule) Validate(event *UnifiedEvent) error {
	if event.Metadata == nil || event.Metadata.Type == "" {
		return nil // Required fields rule handles this
	}
	
	if !eventTypePattern.MatchString(event.Metadata.Type) {
		return fmt.Errorf("invalid event type format: %s (expected lowercase.dot.notation)", event.Metadata.Type)
	}
	
	// Validate category matches type prefix
	typePrefix := strings.Split(event.Metadata.Type, ".")[0]
	
	categoryMap := map[string]EventCategory{
		"network":        EventCategory_CATEGORY_NETWORK,
		"memory":         EventCategory_CATEGORY_MEMORY,
		"cpu":            EventCategory_CATEGORY_CPU,
		"io":             EventCategory_CATEGORY_IO,
		"system":         EventCategory_CATEGORY_SYSTEM,
		"security":       EventCategory_CATEGORY_SECURITY,
		"application":    EventCategory_CATEGORY_APPLICATION,
		"infrastructure": EventCategory_CATEGORY_INFRASTRUCTURE,
		"observability":  EventCategory_CATEGORY_OBSERVABILITY,
	}
	
	if expectedCategory, ok := categoryMap[typePrefix]; ok {
		if event.Metadata.Category != expectedCategory {
			return fmt.Errorf("event type %s should have category %v", event.Metadata.Type, expectedCategory)
		}
	}
	
	return nil
}

// EntityContextRule validates entity context consistency
type EntityContextRule struct{}

func (r *EntityContextRule) Name() string { return "entity_context" }

func (r *EntityContextRule) Validate(event *UnifiedEvent) error {
	if event.Entity == nil {
		return nil // Entity is optional
	}
	
	// Validate entity type matches data
	switch event.Entity.Type {
	case EntityType_ENTITY_PROCESS:
		if event.Entity.Process == nil {
			return fmt.Errorf("process entity requires process info")
		}
		if event.Entity.Process.Pid == 0 {
			return fmt.Errorf("process entity requires valid PID")
		}
		
	case EntityType_ENTITY_CONTAINER:
		if event.Entity.Container == nil {
			return fmt.Errorf("container entity requires container info")
		}
		if event.Entity.Container.Id == "" {
			return fmt.Errorf("container entity requires container ID")
		}
		
	case EntityType_ENTITY_POD:
		if event.Entity.Pod == nil {
			return fmt.Errorf("pod entity requires pod info")
		}
		if event.Entity.Pod.Name == "" || event.Entity.Pod.Namespace == "" {
			return fmt.Errorf("pod entity requires name and namespace")
		}
	}
	
	return nil
}

// NetworkEventRule validates network event data
type NetworkEventRule struct{}

func (r *NetworkEventRule) Name() string { return "network_event" }

func (r *NetworkEventRule) Validate(event *UnifiedEvent) error {
	netEvent, ok := event.Data.(*UnifiedEvent_Network)
	if !ok {
		return nil // Not a network event
	}
	
	data := netEvent.Network
	
	// Validate IPs
	if data.SrcIp != "" && net.ParseIP(data.SrcIp) == nil {
		return fmt.Errorf("invalid source IP: %s", data.SrcIp)
	}
	
	if data.DstIp != "" && net.ParseIP(data.DstIp) == nil {
		return fmt.Errorf("invalid destination IP: %s", data.DstIp)
	}
	
	// Validate ports
	if data.SrcPort > 65535 {
		return fmt.Errorf("invalid source port: %d", data.SrcPort)
	}
	
	if data.DstPort > 65535 {
		return fmt.Errorf("invalid destination port: %d", data.DstPort)
	}
	
	// Validate protocol
	validProtocols := map[string]bool{
		"tcp": true, "udp": true, "icmp": true, 
		"http": true, "https": true, "grpc": true,
	}
	
	if data.Protocol != "" && !validProtocols[strings.ToLower(data.Protocol)] {
		return fmt.Errorf("unknown protocol: %s", data.Protocol)
	}
	
	return nil
}

// MemoryEventRule validates memory event data
type MemoryEventRule struct{}

func (r *MemoryEventRule) Name() string { return "memory_event" }

func (r *MemoryEventRule) Validate(event *UnifiedEvent) error {
	memEvent, ok := event.Data.(*UnifiedEvent_Memory)
	if !ok {
		return nil // Not a memory event
	}
	
	data := memEvent.Memory
	
	// Validate operation
	validOps := map[string]bool{
		"alloc": true, "free": true, "realloc": true,
		"mmap": true, "munmap": true, "oom": true,
	}
	
	if data.Operation != "" && !validOps[data.Operation] {
		return fmt.Errorf("unknown memory operation: %s", data.Operation)
	}
	
	// Validate sizes
	if data.RssBytes > data.VmsBytes && data.VmsBytes > 0 {
		return fmt.Errorf("RSS (%d) cannot exceed VMS (%d)", data.RssBytes, data.VmsBytes)
	}
	
	return nil
}

// AttributeSizeRule validates attribute sizes
type AttributeSizeRule struct {
	MaxKeyLength   int
	MaxValueLength int
	MaxAttributes  int
}

func (r *AttributeSizeRule) Name() string { return "attribute_size" }

func (r *AttributeSizeRule) Validate(event *UnifiedEvent) error {
	maxKey := r.MaxKeyLength
	if maxKey == 0 {
		maxKey = 256
	}
	
	maxValue := r.MaxValueLength
	if maxValue == 0 {
		maxValue = 4096
	}
	
	maxAttrs := r.MaxAttributes
	if maxAttrs == 0 {
		maxAttrs = 100
	}
	
	if len(event.Attributes) > maxAttrs {
		return fmt.Errorf("too many attributes: %d (max %d)", len(event.Attributes), maxAttrs)
	}
	
	for key, attr := range event.Attributes {
		if len(key) > maxKey {
			return fmt.Errorf("attribute key too long: %s (%d > %d)", key, len(key), maxKey)
		}
		
		// Check value size
		switch v := attr.Value.(type) {
		case *AttributeValue_StringValue:
			if len(v.StringValue) > maxValue {
				return fmt.Errorf("attribute value too long for key %s: %d > %d", key, len(v.StringValue), maxValue)
			}
		case *AttributeValue_BytesValue:
			if len(v.BytesValue) > maxValue {
				return fmt.Errorf("attribute bytes too long for key %s: %d > %d", key, len(v.BytesValue), maxValue)
			}
		}
	}
	
	return nil
}

// LabelFormatRule validates label format
type LabelFormatRule struct{}

func (r *LabelFormatRule) Name() string { return "label_format" }

var labelKeyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]*$`)

func (r *LabelFormatRule) Validate(event *UnifiedEvent) error {
	for key, value := range event.Labels {
		if !labelKeyPattern.MatchString(key) {
			return fmt.Errorf("invalid label key format: %s", key)
		}
		
		if len(key) > 63 {
			return fmt.Errorf("label key too long: %s (%d > 63)", key, len(key))
		}
		
		if len(value) > 256 {
			return fmt.Errorf("label value too long for key %s: %d > 256", key, len(value))
		}
	}
	
	return nil
}

// SchemaValidator validates events against schema versions
type SchemaValidator struct {
	schemas map[string]EventSchema
	mu      sync.RWMutex
}

// EventSchema defines validation rules for a specific schema version
type EventSchema struct {
	Version      string
	RequiredData []string
	ValidateFunc func(*UnifiedEvent) error
}

// NewSchemaValidator creates a new schema validator
func NewSchemaValidator() *SchemaValidator {
	return &SchemaValidator{
		schemas: make(map[string]EventSchema),
	}
}

// RegisterSchema registers a schema version
func (sv *SchemaValidator) RegisterSchema(schema EventSchema) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.schemas[schema.Version] = schema
}

// ValidateSchema validates an event against its schema
func (sv *SchemaValidator) ValidateSchema(event *UnifiedEvent) error {
	if event.Metadata == nil || event.Metadata.SchemaVersion == "" {
		return nil // No schema specified
	}
	
	sv.mu.RLock()
	schema, ok := sv.schemas[event.Metadata.SchemaVersion]
	sv.mu.RUnlock()
	
	if !ok {
		return fmt.Errorf("unknown schema version: %s", event.Metadata.SchemaVersion)
	}
	
	if schema.ValidateFunc != nil {
		return schema.ValidateFunc(event)
	}
	
	return nil
}