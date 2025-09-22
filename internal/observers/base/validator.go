package base

import (
	"fmt"
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// EventValidator validates CollectorEvents follow Tapio standards
// Every observer MUST produce events that pass validation
type EventValidator struct {
	observerType string
	logger       *zap.Logger
	strictMode   bool // If true, validation failures panic in dev
}

// NewEventValidator creates a validator for the given observer type
func NewEventValidator(observerType string, logger *zap.Logger, strictMode bool) *EventValidator {
	// Use a nop logger if none provided to avoid nil pointer issues
	if logger == nil {
		logger = zap.NewNop()
	}
	return &EventValidator{
		observerType: observerType,
		logger:       logger,
		strictMode:   strictMode,
	}
}

// ValidateEvent validates a CollectorEvent follows all Tapio standards
// Returns error if validation fails, nil if valid
func (v *EventValidator) ValidateEvent(event *domain.CollectorEvent) error {
	var errors []string

	// 1. Basic required fields
	if err := v.validateRequiredFields(event); err != nil {
		errors = append(errors, err.Error())
	}

	// 2. Type matches data field
	if err := v.validateTypeMatchesData(event); err != nil {
		errors = append(errors, err.Error())
	}

	// 3. Observer-specific validation
	if err := v.validateObserverSpecific(event); err != nil {
		errors = append(errors, err.Error())
	}

	// 4. No map[string]interface{} abuse
	if err := v.validateNoMapAbuse(event); err != nil {
		errors = append(errors, err.Error())
	}

	// 5. Metadata standards
	if err := v.validateMetadata(event); err != nil {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		fullErr := fmt.Errorf("event validation failed for %s:\n%s",
			v.observerType, strings.Join(errors, "\n"))

		// Log the validation error if logger is available
		if v.logger != nil {
			v.logger.Error("Event validation failed",
				zap.String("observer", v.observerType),
				zap.String("event_id", event.EventID),
				zap.String("event_type", string(event.Type)),
				zap.Error(fullErr))
		}

		// In strict mode (dev), panic to catch issues early
		if v.strictMode {
			panic(fullErr)
		}

		return fullErr
	}

	return nil
}

// validateRequiredFields checks all required fields are present
func (v *EventValidator) validateRequiredFields(event *domain.CollectorEvent) error {
	var missing []string

	if event.EventID == "" {
		missing = append(missing, "EventID")
	}
	if event.Timestamp.IsZero() {
		missing = append(missing, "Timestamp")
	}
	if event.Type == "" {
		missing = append(missing, "Type")
	}
	if event.Source == "" {
		missing = append(missing, "Source")
	}
	if event.Severity == "" {
		missing = append(missing, "Severity")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}

	// Source should contain observer type
	if !strings.Contains(event.Source, v.observerType) {
		return fmt.Errorf("source '%s' should contain observer type '%s'",
			event.Source, v.observerType)
	}

	return nil
}

// validateTypeMatchesData ensures EventType matches populated data field
func (v *EventValidator) validateTypeMatchesData(event *domain.CollectorEvent) error {
	data := &event.EventData

	switch event.Type {
	// DNS Events
	case domain.EventTypeDNS, domain.EventTypeDNSQuery, domain.EventTypeDNSResponse, domain.EventTypeDNSTimeout:
		if data.DNS == nil {
			return fmt.Errorf("EventType %s requires DNS field, not Network field", event.Type)
		}
		if data.Network != nil {
			return fmt.Errorf("DNS events should use DNS field exclusively, not Network field")
		}

	// Storage Events
	case domain.EventTypeStorageIO, domain.EventTypeStorageIORead, domain.EventTypeStorageIOWrite,
		domain.EventTypeStorageIOFsync, domain.EventTypeStorageIOSlow:
		if data.StorageIO == nil {
			return fmt.Errorf("EventType %s requires StorageIO field", event.Type)
		}

	// Memory Events
	case domain.EventTypeMemoryAllocation, domain.EventTypeMemoryDeallocation,
		domain.EventTypeMemoryLeak, domain.EventTypeMemoryRSSGrowth, domain.EventTypeMemoryOOMRisk:
		if data.Memory == nil {
			return fmt.Errorf("EventType %s requires Memory field", event.Type)
		}

	// Network Events (L4)
	case domain.EventTypeTCP, domain.EventTypeNetworkConnection:
		if data.Network == nil {
			return fmt.Errorf("EventType %s requires Network field", event.Type)
		}

	// HTTP Events
	case domain.EventTypeHTTP:
		if data.HTTP == nil {
			return fmt.Errorf("EventType %s requires HTTP field, not Network field", event.Type)
		}

	// gRPC Events
	case domain.EventTypeGRPC:
		if data.GRPC == nil {
			return fmt.Errorf("EventType %s requires GRPC field, not Network field", event.Type)
		}

	// Process Events
	case domain.EventTypeKernelProcess:
		if data.Process == nil {
			return fmt.Errorf("EventType %s requires Process field", event.Type)
		}
	}

	return nil
}

// validateObserverSpecific performs observer-specific validations
func (v *EventValidator) validateObserverSpecific(event *domain.CollectorEvent) error {
	switch v.observerType {
	case "dns":
		return v.validateDNSEvent(event)
	case "storage-io":
		return v.validateStorageIOEvent(event)
	case "memory":
		return v.validateMemoryEvent(event)
	case "network":
		return v.validateNetworkEvent(event)
	case "process-signals":
		return v.validateProcessSignalsEvent(event)
	}
	return nil
}

// validateDNSEvent validates DNS observer events
func (v *EventValidator) validateDNSEvent(event *domain.CollectorEvent) error {
	if event.EventData.DNS == nil {
		return fmt.Errorf("DNS observer must populate DNS field")
	}

	dns := event.EventData.DNS
	if dns.QueryName == "" {
		return fmt.Errorf("DNS QueryName is required")
	}
	if dns.QueryType == "" {
		return fmt.Errorf("DNS QueryType is required")
	}
	if dns.ClientIP == "" || dns.ServerIP == "" {
		return fmt.Errorf("DNS ClientIP and ServerIP are required")
	}

	return nil
}

// validateStorageIOEvent validates Storage I/O observer events
func (v *EventValidator) validateStorageIOEvent(event *domain.CollectorEvent) error {
	if event.EventData.StorageIO == nil {
		return fmt.Errorf("Storage-IO observer must populate StorageIO field")
	}

	io := event.EventData.StorageIO
	if io.Operation == "" {
		return fmt.Errorf("StorageIO Operation is required")
	}
	if io.Path == "" {
		return fmt.Errorf("StorageIO Path is required")
	}

	return nil
}

// validateMemoryEvent validates Memory observer events
func (v *EventValidator) validateMemoryEvent(event *domain.CollectorEvent) error {
	if event.EventData.Memory == nil {
		return fmt.Errorf("Memory observer must populate Memory field")
	}

	mem := event.EventData.Memory
	if mem.Operation == "" {
		return fmt.Errorf("Memory Operation is required")
	}

	return nil
}

// validateNetworkEvent validates Network observer events
func (v *EventValidator) validateNetworkEvent(event *domain.CollectorEvent) error {
	// Network observer can use different fields based on protocol
	switch event.Type {
	case domain.EventTypeHTTP:
		if event.EventData.HTTP == nil {
			return fmt.Errorf("HTTP events must use HTTP field")
		}
	case domain.EventTypeDNS:
		if event.EventData.DNS == nil {
			return fmt.Errorf("DNS events from network observer must use DNS field")
		}
	case domain.EventTypeGRPC:
		if event.EventData.GRPC == nil {
			return fmt.Errorf("gRPC events must use GRPC field")
		}
	default:
		// L4 events use Network field
		if event.EventData.Network == nil {
			return fmt.Errorf("L4 network events must use Network field")
		}
	}
	return nil
}

// validateProcessSignalsEvent validates Process-Signals observer events
func (v *EventValidator) validateProcessSignalsEvent(event *domain.CollectorEvent) error {
	if event.EventData.Process == nil {
		return fmt.Errorf("Process-Signals observer must populate Process field")
	}

	proc := event.EventData.Process
	if proc.PID == 0 {
		return fmt.Errorf("Process PID is required")
	}
	if proc.Command == "" {
		return fmt.Errorf("Process Command is required")
	}

	// Check for signal-specific data in Custom field
	if strings.Contains(string(event.Type), "signal") {
		if event.EventData.Custom == nil || event.EventData.Custom["signal"] == "" {
			return fmt.Errorf("Signal events must include signal number in Custom field")
		}
	}

	return nil
}

// validateNoMapAbuse ensures Custom field isn't abused for structured data
func (v *EventValidator) validateNoMapAbuse(event *domain.CollectorEvent) error {
	if event.EventData.Custom == nil {
		return nil
	}

	// Check for protocol-specific data that should use typed fields
	var abusedFields []string
	for key := range event.EventData.Custom {
		if strings.HasPrefix(key, "dns_") && event.EventData.DNS == nil {
			abusedFields = append(abusedFields, key)
		}
		if strings.HasPrefix(key, "http_") && event.EventData.HTTP == nil {
			abusedFields = append(abusedFields, key)
		}
		if strings.HasPrefix(key, "memory_") && event.EventData.Memory == nil {
			abusedFields = append(abusedFields, key)
		}
		if strings.HasPrefix(key, "storage_") && event.EventData.StorageIO == nil {
			abusedFields = append(abusedFields, key)
		}
	}

	if len(abusedFields) > 0 {
		return fmt.Errorf("protocol-specific data in Custom field should use typed fields: %s",
			strings.Join(abusedFields, ", "))
	}

	// Warn if Custom has too many entries
	if len(event.EventData.Custom) > 10 {
		return fmt.Errorf("Custom field has %d entries - consider using typed fields",
			len(event.EventData.Custom))
	}

	return nil
}

// validateMetadata ensures metadata follows standards
func (v *EventValidator) validateMetadata(event *domain.CollectorEvent) error {
	if event.Metadata.Labels == nil {
		return fmt.Errorf("Metadata.Labels is required")
	}

	// Check required labels
	if event.Metadata.Labels["observer"] != v.observerType {
		return fmt.Errorf("Metadata label 'observer' should be '%s', got '%s'",
			v.observerType, event.Metadata.Labels["observer"])
	}

	if event.Metadata.Labels["version"] == "" {
		return fmt.Errorf("Metadata label 'version' is required")
	}

	return nil
}
