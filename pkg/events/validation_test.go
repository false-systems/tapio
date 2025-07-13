package events

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidator(t *testing.T) {
	validator := NewValidator()
	
	// Test valid event
	validEvent := NewBuilder().
		WithType("network.connection", EventCategory_CATEGORY_NETWORK).
		WithSeverity(EventSeverity_SEVERITY_INFO).
		WithSource("ebpf", "network-collector", "node-1").
		WithEntity(EntityType_ENTITY_PROCESS, "123", "nginx").
		WithProcess(1234, "nginx").
		Build()
	
	err := validator.Validate(validEvent)
	assert.NoError(t, err, "Valid event should pass validation")
	
	ReleaseEvent(validEvent)
}

func TestRequiredFieldsRule(t *testing.T) {
	rule := &RequiredFieldsRule{}
	
	// Test missing ID
	event := NewEvent()
	event.Id = ""
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event ID is required")
	
	// Test missing timestamp
	event.Id = "test-id"
	event.Timestamp = nil
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp is required")
	
	// Test missing metadata
	event.Timestamp = timestamppb.Now()
	event.Metadata = nil
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata is required")
	
	// Test missing event type
	event.Metadata = &EventMetadata{}
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event type is required")
	
	// Test missing source
	event.Metadata.Type = "test.event"
	event.Source = nil
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "source is required")
	
	// Test missing source type
	event.Source = &EventSource{}
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "source type is required")
	
	// Test valid event
	event.Source.Type = "test"
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestTimestampRule(t *testing.T) {
	rule := &TimestampRule{
		MaxFuture: 1 * time.Minute,
		MaxPast:   1 * time.Hour,
	}
	
	// Test future timestamp
	event := NewEvent()
	event.Timestamp = timestamppb.New(time.Now().Add(2 * time.Minute))
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too far in the future")
	
	// Test past timestamp
	event.Timestamp = timestamppb.New(time.Now().Add(-2 * time.Hour))
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too far in the past")
	
	// Test valid timestamp
	event.Timestamp = timestamppb.New(time.Now().Add(-30 * time.Minute))
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestEventTypeRule(t *testing.T) {
	rule := &EventTypeRule{}
	
	// Test invalid format (uppercase)
	event := NewEvent()
	event.Metadata.Type = "Network.Connection"
	event.Metadata.Category = EventCategory_CATEGORY_NETWORK
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid event type format")
	
	// Test invalid format (spaces)
	event.Metadata.Type = "network connection"
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid event type format")
	
	// Test category mismatch
	event.Metadata.Type = "network.connection"
	event.Metadata.Category = EventCategory_CATEGORY_MEMORY
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "should have category")
	
	// Test valid event type
	event.Metadata.Type = "network.connection"
	event.Metadata.Category = EventCategory_CATEGORY_NETWORK
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestEntityContextRule(t *testing.T) {
	rule := &EntityContextRule{}
	
	// Test process entity without process info
	event := NewEvent()
	event.Entity.Type = EntityType_ENTITY_PROCESS
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "process entity requires process info")
	
	// Test process entity with invalid PID
	event.Entity.Process = &ProcessInfo{Pid: 0}
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires valid PID")
	
	// Test valid process entity
	event.Entity.Process.Pid = 1234
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	// Test container entity without container info
	event.Entity.Type = EntityType_ENTITY_CONTAINER
	event.Entity.Container = nil
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container entity requires container info")
	
	// Test container entity without ID
	event.Entity.Container = &ContainerInfo{}
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires container ID")
	
	// Test valid container entity
	event.Entity.Container.Id = "container-123"
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestNetworkEventRule(t *testing.T) {
	rule := &NetworkEventRule{}
	
	// Test invalid source IP
	event := NewEvent()
	event.Data = &UnifiedEvent_Network{
		Network: &NetworkEvent{
			SrcIp: "invalid-ip",
		},
	}
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source IP")
	
	// Test invalid destination IP
	event.Data.(*UnifiedEvent_Network).Network.SrcIp = "192.168.1.1"
	event.Data.(*UnifiedEvent_Network).Network.DstIp = "invalid-ip"
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid destination IP")
	
	// Test invalid source port
	event.Data.(*UnifiedEvent_Network).Network.DstIp = "192.168.1.2"
	event.Data.(*UnifiedEvent_Network).Network.SrcPort = 70000
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source port")
	
	// Test invalid destination port
	event.Data.(*UnifiedEvent_Network).Network.SrcPort = 8080
	event.Data.(*UnifiedEvent_Network).Network.DstPort = 70000
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid destination port")
	
	// Test unknown protocol
	event.Data.(*UnifiedEvent_Network).Network.DstPort = 80
	event.Data.(*UnifiedEvent_Network).Network.Protocol = "unknown"
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown protocol")
	
	// Test valid network event
	event.Data.(*UnifiedEvent_Network).Network.Protocol = "tcp"
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestMemoryEventRule(t *testing.T) {
	rule := &MemoryEventRule{}
	
	// Test unknown operation
	event := NewEvent()
	event.Data = &UnifiedEvent_Memory{
		Memory: &MemoryEvent{
			Operation: "unknown",
		},
	}
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown memory operation")
	
	// Test RSS > VMS (invalid)
	event.Data.(*UnifiedEvent_Memory).Memory.Operation = "alloc"
	event.Data.(*UnifiedEvent_Memory).Memory.RssBytes = 2048
	event.Data.(*UnifiedEvent_Memory).Memory.VmsBytes = 1024
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RSS")
	assert.Contains(t, err.Error(), "cannot exceed VMS")
	
	// Test valid memory event
	event.Data.(*UnifiedEvent_Memory).Memory.RssBytes = 1024
	event.Data.(*UnifiedEvent_Memory).Memory.VmsBytes = 2048
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestAttributeSizeRule(t *testing.T) {
	rule := &AttributeSizeRule{
		MaxKeyLength:   10,
		MaxValueLength: 20,
		MaxAttributes:  2,
	}
	
	event := NewEvent()
	
	// Test too many attributes
	event.Attributes["key1"] = &AttributeValue{Value: &AttributeValue_StringValue{StringValue: "value1"}}
	event.Attributes["key2"] = &AttributeValue{Value: &AttributeValue_StringValue{StringValue: "value2"}}
	event.Attributes["key3"] = &AttributeValue{Value: &AttributeValue_StringValue{StringValue: "value3"}}
	
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many attributes")
	
	// Test key too long
	delete(event.Attributes, "key3")
	delete(event.Attributes, "key2")
	event.Attributes["very_long_key_name"] = &AttributeValue{Value: &AttributeValue_StringValue{StringValue: "value"}}
	
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "attribute key too long")
	
	// Test string value too long
	delete(event.Attributes, "very_long_key_name")
	event.Attributes["key3"] = &AttributeValue{Value: &AttributeValue_StringValue{StringValue: "this is a very long string value that exceeds the limit"}}
	
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "attribute value too long")
	
	// Test valid attributes
	delete(event.Attributes, "key3")
	event.Attributes["key3"] = &AttributeValue{Value: &AttributeValue_StringValue{StringValue: "short"}}
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestLabelFormatRule(t *testing.T) {
	rule := &LabelFormatRule{}
	
	event := NewEvent()
	
	// Test invalid label key (starts with number)
	event.Labels["1invalid"] = "value"
	err := rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid label key format")
	
	// Test invalid label key (contains space)
	delete(event.Labels, "1invalid")
	event.Labels["invalid key"] = "value"
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid label key format")
	
	// Test label key too long
	delete(event.Labels, "invalid key")
	longKey := ""
	for i := 0; i < 65; i++ {
		longKey += "a"
	}
	event.Labels[longKey] = "value"
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label key too long")
	
	// Test label value too long
	delete(event.Labels, longKey)
	longValue := string(make([]byte, 300))
	event.Labels["valid"] = longValue
	err = rule.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label value too long")
	
	// Test valid labels
	event.Labels = map[string]string{
		"environment": "production",
		"version":     "1.0.0",
		"region":      "us-west-2",
	}
	err = rule.Validate(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestValidatorStats(t *testing.T) {
	validator := NewValidator()
	
	initialStats := validator.GetStats()
	
	// Test successful validation
	validEvent := NewBuilder().
		WithType("test.validation", EventCategory_CATEGORY_APPLICATION).
		WithSource("test", "test-collector", "test-node").
		Build()
	
	err := validator.Validate(validEvent)
	assert.NoError(t, err)
	
	// Test failed validation
	invalidEvent := NewEvent()
	invalidEvent.Id = ""
	
	err = validator.Validate(invalidEvent)
	assert.Error(t, err)
	
	// Check updated stats
	finalStats := validator.GetStats()
	
	assert.Equal(t, initialStats.TotalValidated+2, finalStats.TotalValidated)
	assert.Equal(t, initialStats.ValidationPassed+1, finalStats.ValidationPassed)
	assert.Equal(t, initialStats.ValidationFailed+1, finalStats.ValidationFailed)
	
	// Check rule-specific stats
	assert.Greater(t, finalStats.RulesFired["required_fields"], uint64(0))
	
	ReleaseEvent(validEvent)
	ReleaseEvent(invalidEvent)
}

func TestSchemaValidator(t *testing.T) {
	validator := NewSchemaValidator()
	
	// Register a test schema
	schema := EventSchema{
		Version: "1.0",
		ValidateFunc: func(event *UnifiedEvent) error {
			if event.Metadata.Type != "test.schema" {
				return assert.AnError
			}
			return nil
		},
	}
	validator.RegisterSchema(schema)
	
	// Test event with matching schema
	event := NewEvent()
	event.Metadata.SchemaVersion = "1.0"
	event.Metadata.Type = "test.schema"
	
	err := validator.ValidateSchema(event)
	assert.NoError(t, err)
	
	// Test event with schema validation failure
	event.Metadata.Type = "wrong.type"
	err = validator.ValidateSchema(event)
	assert.Error(t, err)
	
	// Test event with unknown schema
	event.Metadata.SchemaVersion = "2.0"
	err = validator.ValidateSchema(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown schema version")
	
	// Test event without schema
	event.Metadata.SchemaVersion = ""
	err = validator.ValidateSchema(event)
	assert.NoError(t, err)
	
	ReleaseEvent(event)
}

func TestValidatorCustomRule(t *testing.T) {
	validator := NewValidator()
	
	// Add custom rule
	customRule := &TestCustomRule{}
	validator.AddRule(customRule)
	
	// Test event that should trigger custom rule
	event := NewBuilder().
		WithType("custom.test", EventCategory_CATEGORY_APPLICATION).
		WithSource("test", "test-collector", "test-node").
		Build()
	
	err := validator.Validate(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "custom rule failed")
	
	ReleaseEvent(event)
}

// TestCustomRule is a test rule for validation testing
type TestCustomRule struct{}

func (r *TestCustomRule) Name() string { return "test_custom" }

func (r *TestCustomRule) Validate(event *UnifiedEvent) error {
	if event.Metadata != nil && event.Metadata.Type == "custom.test" {
		return errors.New("custom rule failed")
	}
	return nil
}

func BenchmarkValidation(b *testing.B) {
	validator := NewValidator()
	
	event := NewBuilder().
		WithType("benchmark.validation", EventCategory_CATEGORY_APPLICATION).
		WithSeverity(EventSeverity_SEVERITY_INFO).
		WithSource("benchmark", "test-collector", "test-node").
		WithEntity(EntityType_ENTITY_PROCESS, "123", "test").
		WithProcess(1234, "test").
		WithAttribute("key1", "value1").
		WithLabel("env", "test").
		Build()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		err := validator.Validate(event)
		if err != nil {
			b.Fatal(err)
		}
	}
	
	ReleaseEvent(event)
}