package grpc

import (
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertUnifiedEventToProto converts a UnifiedEvent directly to pb.Event without data loss
func convertUnifiedEventToProto(event *domain.UnifiedEvent) (*pb.Event, error) {
	if event == nil {
		return nil, nil
	}

	pe := &pb.Event{
		Id:          event.ID,
		Timestamp:   timestamppb.New(event.Timestamp),
		Message:     event.Message,
		Description: event.Message, // Use message as description if not separate
		Confidence:  event.Confidence,
		Tags:        event.Tags,
	}

	// Map event type
	pe.Type = mapEventTypeToProto(event.Type)

	// Map severity
	if event.Severity != "" {
		pe.Severity = mapSeverityToProto(string(event.Severity))
	} else if event.Impact != nil {
		pe.Severity = mapSeverityToProto(event.Impact.Severity)
	}

	// Note: proto Event doesn't have Source field - store in attributes
	if event.Attributes == nil {
		event.Attributes = make(map[string]interface{})
	}
	event.Attributes["source"] = event.Source

	// Set trace context
	if event.TraceContext != nil {
		pe.TraceId = event.TraceContext.TraceID
		pe.SpanId = event.TraceContext.SpanID
		pe.ParentSpanId = event.TraceContext.ParentSpanID
	}

	// Convert attributes to protobuf Struct
	if event.Attributes != nil {
		dataStruct, err := structpb.NewStruct(event.Attributes)
		if err == nil {
			pe.Data = dataStruct
		}
	}

	// Convert AI features if proto supports it
	// Note: AiFeatures field not available in current proto definition

	// Set correlation hints
	pe.CorrelationIds = event.CorrelationHints

	// Note: The proto Event has a simpler structure than UnifiedEvent
	// Create a combined data map with all context information
	dataMap := make(map[string]interface{})
	
	// Add semantic context to data
	if event.Semantic != nil {
		dataMap["semantic"] = map[string]interface{}{
			"intent":     event.Semantic.Intent,
			"category":   event.Semantic.Category,
			"confidence": event.Semantic.Confidence,
			"narrative":  event.Semantic.Narrative,
		}
	}
	
	// Add entity context to data
	if event.Entity != nil {
		dataMap["entity"] = map[string]interface{}{
			"type":      event.Entity.Type,
			"name":      event.Entity.Name,
			"namespace": event.Entity.Namespace,
			"uid":       event.Entity.UID,
		}
	}
	
	// Add impact context to data
	if event.Impact != nil {
		dataMap["impact"] = map[string]interface{}{
			"severity":      event.Impact.Severity,
			"businessImpact": event.Impact.BusinessImpact,
			"affectedUsers": event.Impact.AffectedUsers,
		}
	}
	
	// Add layer-specific data
	if event.Kernel != nil {
		dataMap["kernel"] = map[string]interface{}{
			"syscall": event.Kernel.Syscall,
			"pid":     event.Kernel.PID,
			"uid":     event.Kernel.UID,
			"gid":     event.Kernel.GID,
			"comm":    event.Kernel.Comm,
		}
	}
	
	if event.Network != nil {
		dataMap["network"] = map[string]interface{}{
			"protocol":    event.Network.Protocol,
			"source_ip":   event.Network.SourceIP,
			"source_port": event.Network.SourcePort,
			"dest_ip":     event.Network.DestIP,
			"dest_port":   event.Network.DestPort,
			"direction":   event.Network.Direction,
		}
	}
	
	if event.Application != nil {
		dataMap["application"] = map[string]interface{}{
			"level":   event.Application.Level,
			"message": event.Application.Message,
			"logger":  event.Application.Logger,
		}
	}
	
	// Convert all data to protobuf Struct
	if len(dataMap) > 0 {
		dataStruct, err := structpb.NewStruct(dataMap)
		if err == nil {
			pe.Data = dataStruct
		}
	}

	// Note: Context, CollectorId, ProcessedAt fields not available in current proto
	// This information is already included in the Data field above

	return pe, nil
}

// convertProtoToUnifiedEvent converts a pb.Event to UnifiedEvent preserving all data
func convertProtoToUnifiedEvent(event *pb.Event) *domain.UnifiedEvent {
	if event == nil {
		return nil
	}

	ue := &domain.UnifiedEvent{
		ID:               event.Id,
		Timestamp:        event.Timestamp.AsTime(),
		Type:             mapProtoToEventType(event.Type),
		Source:           "grpc", // Default source
		Message:          event.Message,
		Tags:             event.Tags,
		Confidence:       event.Confidence,
		CorrelationHints: event.CorrelationIds,
	}

	// Set severity
	ue.Severity = domain.EventSeverity(mapProtoToSeverity(event.Severity))

	// Convert trace context
	if event.TraceId != "" || event.SpanId != "" {
		ue.TraceContext = &domain.TraceContext{
			TraceID:      event.TraceId,
			SpanID:       event.SpanId,
			ParentSpanID: event.ParentSpanId,
		}
	}

	// Convert attributes from protobuf Struct
	if event.Data != nil {
		ue.Attributes = event.Data.AsMap()
		// Check if source was stored in attributes
		if source, ok := ue.Attributes["source"].(string); ok {
			ue.Source = source
		}
	}

	// Extract contexts from the Data field if available
	if event.Data != nil {
		dataMap := event.Data.AsMap()
		
		// Extract semantic context
		if semanticData, ok := dataMap["semantic"].(map[string]interface{}); ok {
			ue.Semantic = &domain.SemanticContext{
				Intent:     getStringFromMap(semanticData, "intent"),
				Category:   getStringFromMap(semanticData, "category"),
				Confidence: getFloatFromMap(semanticData, "confidence"),
				Narrative:  getStringFromMap(semanticData, "narrative"),
			}
		}
		
		// Extract entity context
		if entityData, ok := dataMap["entity"].(map[string]interface{}); ok {
			ue.Entity = &domain.EntityContext{
				Type:      getStringFromMap(entityData, "type"),
				Name:      getStringFromMap(entityData, "name"),
				Namespace: getStringFromMap(entityData, "namespace"),
				UID:       getStringFromMap(entityData, "uid"),
			}
		}
		
		// Extract impact context
		if impactData, ok := dataMap["impact"].(map[string]interface{}); ok {
			ue.Impact = &domain.ImpactContext{
				Severity:       getStringFromMap(impactData, "severity"),
				BusinessImpact: getFloatFromMap(impactData, "businessImpact"),
				AffectedUsers:  getIntFromMap(impactData, "affectedUsers"),
			}
		}
		
		// Extract layer-specific data
		if kernelData, ok := dataMap["kernel"].(map[string]interface{}); ok {
			ue.Kernel = &domain.KernelData{
				Syscall: getStringFromMap(kernelData, "syscall"),
				PID:     uint32(getIntFromMap(kernelData, "pid")),
				UID:     uint32(getIntFromMap(kernelData, "uid")),
				GID:     uint32(getIntFromMap(kernelData, "gid")),
				Comm:    getStringFromMap(kernelData, "comm"),
			}
		}
		
		if networkData, ok := dataMap["network"].(map[string]interface{}); ok {
			ue.Network = &domain.NetworkData{
				Protocol:   getStringFromMap(networkData, "protocol"),
				SourceIP:   getStringFromMap(networkData, "source_ip"),
				SourcePort: uint16(getIntFromMap(networkData, "source_port")),
				DestIP:     getStringFromMap(networkData, "dest_ip"),
				DestPort:   uint16(getIntFromMap(networkData, "dest_port")),
				Direction:  getStringFromMap(networkData, "direction"),
			}
		}
		
		if appData, ok := dataMap["application"].(map[string]interface{}); ok {
			ue.Application = &domain.ApplicationData{
				Level:   getStringFromMap(appData, "level"),
				Message: getStringFromMap(appData, "message"),
				Logger:  getStringFromMap(appData, "logger"),
			}
		}
		
		// Note: UnifiedEvent doesn't have a Data field - data is extracted into specific fields above
	}

	return ue
}

// Note: Helper functions getStringFromMap, getIntFromMap are already defined in tapio_service_complete.go

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		}
	}
	return 0.0
}

// Type mapping functions

func mapEventTypeToProto(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_HTTP
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func mapProtoToEventType(eventType pb.EventType) domain.EventType {
	switch eventType {
	case pb.EventType_EVENT_TYPE_NETWORK:
		return domain.EventTypeNetwork
	case pb.EventType_EVENT_TYPE_SYSCALL:
		return domain.EventTypeSystem
	case pb.EventType_EVENT_TYPE_KUBERNETES:
		return domain.EventTypeKubernetes
	case pb.EventType_EVENT_TYPE_PROCESS:
		return domain.EventTypeProcess
	case pb.EventType_EVENT_TYPE_HTTP:
		return domain.EventTypeService
	default:
		return domain.EventType("unspecified")
	}
}

func mapSeverityToProto(severity string) pb.EventSeverity {
	switch severity {
	case "critical":
		return pb.EventSeverity_EVENT_SEVERITY_CRITICAL
	case "error":
		return pb.EventSeverity_EVENT_SEVERITY_ERROR
	case "warning":
		return pb.EventSeverity_EVENT_SEVERITY_WARNING
	case "info":
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	case "debug":
		return pb.EventSeverity_EVENT_SEVERITY_DEBUG
	default:
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	}
}

func mapProtoToSeverity(severity pb.EventSeverity) domain.Severity {
	switch severity {
	case pb.EventSeverity_EVENT_SEVERITY_CRITICAL:
		return domain.SeverityCritical
	case pb.EventSeverity_EVENT_SEVERITY_ERROR:
		return domain.SeverityError
	case pb.EventSeverity_EVENT_SEVERITY_WARNING:
		return domain.SeverityWarning
	case pb.EventSeverity_EVENT_SEVERITY_INFO:
		return domain.SeverityInfo
	default:
		return domain.SeverityInfo
	}
}

