package grpc

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Event conversions

func convertEventFromProto(event *pb.Event) *domain.Event {
	if event == nil {
		return nil
	}

	return &domain.Event{
		ID:         domain.EventID(event.Id),
		Type:       domain.EventType(event.Type),
		Severity:   domain.EventSeverity(event.Severity),
		Source:     domain.SourceType(event.Source),
		Message:    event.Message,
		Timestamp:  event.Timestamp.AsTime(),
		Attributes: convertStringMapToInterface(event.Attributes),
		Confidence: event.Confidence,
		Tags:       event.Tags,
		Context: domain.EventContext{
			TraceID: event.TraceId,
			SpanID:  event.SpanId,
		},
	}
}

func convertEventToProto(event *domain.Event) *pb.Event {
	if event == nil {
		return nil
	}

	pbEvent := &pb.Event{
		Id:           string(event.ID),
		Type:         convertEventTypeToProto(event.Type),
		Severity:     convertEventSeverityToProto(event.Severity),
		Source:       convertSourceTypeToProto(event.Source),
		Message:      event.Message,
		Description:  "", // Not available in domain.Event
		Timestamp:    timestamppb.New(event.Timestamp),
		TraceId:      event.Context.TraceID,
		SpanId:       event.Context.SpanID,
		ParentSpanId: "", // Not available in domain.Event
		Attributes:   convertInterfaceMapToString(event.Attributes),
		CollectorId:  "", // Not available in domain.Event
		Confidence:   event.Confidence,
		Tags:         event.Tags,
	}

	// Convert context
	pbEvent.Context = &pb.EventContext{
		TraceId:      event.Context.TraceID,
		SpanId:       event.Context.SpanID,
		ParentSpanId: "", // Not available in domain.Event
		// TraceFlags not available in domain.Event
		Labels: event.Context.Labels,
	}

	// Resources and metrics not available in domain.Event

	return pbEvent
}

// Correlation conversions

func convertCorrelationToProto(corr *correlation.Correlation) *pb.Correlation {
	if corr == nil {
		return nil
	}

	pbCorr := &pb.Correlation{
		Id:               corr.ID,
		Type:             pb.CorrelationType(corr.Type),
		Title:            corr.Title,
		Description:      corr.Description,
		CorrelationScore: corr.Score,
		Confidence:       corr.Confidence,
		EventIds:         corr.EventIDs,
		EventCount:       int32(len(corr.EventIDs)),
		DiscoveredAt:     timestamppb.New(corr.DiscoveredAt),
		Statistics:       corr.Statistics,
		Metadata:         corr.Metadata,
	}

	// Convert time range
	if corr.TimeRange != nil {
		pbCorr.TimeRange = &pb.TimeRange{
			Start: timestamppb.New(corr.TimeRange.Start),
			End:   timestamppb.New(corr.TimeRange.End),
		}
	}

	// Convert semantic group IDs
	pbCorr.SemanticGroupIds = corr.SemanticGroupIDs

	// Convert recommendations
	for _, action := range corr.RecommendedActions {
		pbCorr.Actions = append(pbCorr.Actions, convertRecommendedActionToProto(action))
	}

	// Visualization hints
	pbCorr.VisualizationType = corr.VisualizationType
	if corr.VisualizationData != nil {
		pbCorr.VisualizationData, _ = structpb.NewStruct(corr.VisualizationData)
	}

	return pbCorr
}

func convertCorrelationFromProto(corr *pb.Correlation) *correlation.Correlation {
	if corr == nil {
		return nil
	}

	result := &correlation.Correlation{
		ID:                corr.Id,
		Type:              correlation.CorrelationType(corr.Type),
		Title:             corr.Title,
		Description:       corr.Description,
		Score:             corr.CorrelationScore,
		Confidence:        corr.Confidence,
		EventIDs:          corr.EventIds,
		SemanticGroupIDs:  corr.SemanticGroupIds,
		DiscoveredAt:      corr.DiscoveredAt.AsTime(),
		Statistics:        corr.Statistics,
		VisualizationType: corr.VisualizationType,
		Metadata:          corr.Metadata,
	}

	// Convert time range
	if corr.TimeRange != nil {
		result.TimeRange = &correlation.TimeRange{
			Start: corr.TimeRange.Start.AsTime(),
			End:   corr.TimeRange.End.AsTime(),
		}
	}

	// Convert visualization data
	if corr.VisualizationData != nil {
		result.VisualizationData = corr.VisualizationData.AsMap()
	}

	// Convert actions
	for _, action := range corr.Actions {
		result.RecommendedActions = append(result.RecommendedActions, convertRecommendedActionFromProto(action))
	}

	return result
}

// Semantic group conversions

func convertSemanticGroupToProto(group *correlation.SemanticGroup) *pb.SemanticGroup {
	if group == nil {
		return nil
	}

	pbGroup := &pb.SemanticGroup{
		Id:              group.ID,
		Name:            group.Name,
		Description:     group.Description,
		SemanticType:    group.SemanticType,
		Intent:          group.Intent,
		ConfidenceScore: group.Confidence,
		EventIds:        group.EventIDs,
		StartTime:       timestamppb.New(group.StartTime),
		EndTime:         timestamppb.New(group.EndTime),
		Duration:        durationpb.New(group.Duration),
		TraceId:         group.TraceID,
		SpanIds:         group.SpanIDs,
		Metadata:        group.Metadata,
		Labels:          group.Labels,
	}

	// Convert events if included
	for _, event := range group.Events {
		pbGroup.Events = append(pbGroup.Events, convertEventToProto(event))
	}

	// Convert impact assessment
	if group.Impact != nil {
		pbGroup.Impact = convertImpactToProto(group.Impact)
	}

	// Convert prediction
	if group.Prediction != nil {
		pbGroup.Prediction = convertPredictionToProto(group.Prediction)
	}

	// Convert root cause
	if group.RootCause != nil {
		pbGroup.RootCause = convertRootCauseToProto(group.RootCause)
	}

	return pbGroup
}

func convertSemanticGroupFromProto(group *pb.SemanticGroup) *correlation.SemanticGroup {
	if group == nil {
		return nil
	}

	result := &correlation.SemanticGroup{
		ID:           group.Id,
		Name:         group.Name,
		Description:  group.Description,
		SemanticType: group.SemanticType,
		Intent:       group.Intent,
		Confidence:   group.ConfidenceScore,
		EventIDs:     group.EventIds,
		StartTime:    group.StartTime.AsTime(),
		EndTime:      group.EndTime.AsTime(),
		Duration:     group.Duration.AsDuration(),
		TraceID:      group.TraceId,
		SpanIDs:      group.SpanIds,
		Metadata:     group.Metadata,
		Labels:       group.Labels,
	}

	// Convert events
	for _, event := range group.Events {
		result.Events = append(result.Events, convertEventFromProto(event))
	}

	// Convert nested structures
	if group.Impact != nil {
		result.Impact = convertImpactFromProto(group.Impact)
	}
	if group.Prediction != nil {
		result.Prediction = convertPredictionFromProto(group.Prediction)
	}
	if group.RootCause != nil {
		result.RootCause = convertRootCauseFromProto(group.RootCause)
	}

	return result
}

// Impact assessment conversions

func convertImpactToProto(impact *correlation.ImpactAssessment) *pb.ImpactAssessment {
	if impact == nil {
		return nil
	}

	// Map ImpactLevel from correlation package to pb
	var level pb.ImpactAssessment_ImpactLevel
	switch impact.TechnicalSeverity {
	case "critical":
		level = pb.ImpactAssessment_IMPACT_LEVEL_CRITICAL
	case "high":
		level = pb.ImpactAssessment_IMPACT_LEVEL_HIGH
	case "medium":
		level = pb.ImpactAssessment_IMPACT_LEVEL_MEDIUM
	case "low":
		level = pb.ImpactAssessment_IMPACT_LEVEL_LOW
	default:
		level = pb.ImpactAssessment_IMPACT_LEVEL_UNSPECIFIED
	}

	return &pb.ImpactAssessment{
		Level:                   level,
		BusinessImpactScore:     float64(impact.BusinessImpact),
		TechnicalImpactScore:    0.7, // Default value
		AffectedServices:        impact.AffectedResources,
		EstimatedDuration:       durationpb.New(impact.TimeToResolution),
		AffectedUsers:           0,  // Not available in semantic_otel_tracer version
		AffectedRequests:        0,  // Not available in semantic_otel_tracer version
		EstimatedCost:           0,  // Not available in semantic_otel_tracer version
		Currency:                "", // Not available in semantic_otel_tracer version
		CascadeProbability:      float64(impact.CascadeRisk),
		PotentialCascadeTargets: []string{}, // Not available in semantic_otel_tracer version
	}
}

func convertImpactFromProto(impact *pb.ImpactAssessment) *correlation.ImpactAssessment {
	if impact == nil {
		return nil
	}

	// Map technical severity from pb
	var severity string
	switch impact.Level {
	case pb.ImpactAssessment_IMPACT_LEVEL_CRITICAL:
		severity = "critical"
	case pb.ImpactAssessment_IMPACT_LEVEL_HIGH:
		severity = "high"
	case pb.ImpactAssessment_IMPACT_LEVEL_MEDIUM:
		severity = "medium"
	case pb.ImpactAssessment_IMPACT_LEVEL_LOW:
		severity = "low"
	default:
		severity = "unknown"
	}

	return &correlation.ImpactAssessment{
		BusinessImpact:     float32(impact.BusinessImpactScore),
		TechnicalSeverity:  severity,
		CascadeRisk:        float32(impact.CascadeProbability),
		AffectedResources:  impact.AffectedServices,
		TimeToResolution:   impact.EstimatedDuration.AsDuration(),
		RecommendedActions: []string{}, // Not directly available from proto
	}
}

// Prediction conversions

func convertPredictionToProto(pred *correlation.PredictedOutcome) *pb.PredictedOutcome {
	if pred == nil {
		return nil
	}

	pbPred := &pb.PredictedOutcome{
		Scenario:            pred.Scenario,
		Probability:         pred.Probability,
		TimeToOutcome:       durationpb.New(pred.TimeToOutcome),
		PreventionActions:   pred.PreventionActions,
		Confidence:          pred.ConfidenceLevel,
		ModelVersion:        "1.0", // Default version
		PredictionTimestamp: timestamppb.New(time.Now()),
	}

	// Note: PredictedEvents not available in semantic_otel_tracer version
	pbPred.PredictedEvents = []*pb.PredictedEvent{}

	return pbPred
}

func convertPredictionFromProto(pred *pb.PredictedOutcome) *correlation.PredictedOutcome {
	if pred == nil {
		return nil
	}

	result := &correlation.PredictedOutcome{
		Scenario:          pred.Scenario,
		Probability:       pred.Probability,
		TimeToOutcome:     pred.TimeToOutcome.AsDuration(),
		PreventionActions: pred.PreventionActions,
		ConfidenceLevel:   pred.Confidence,
	}

	return result
}

// Root cause conversions

func convertRootCauseToProto(rc *correlation.RootCauseAnalysis) *pb.RootCauseAnalysis {
	if rc == nil {
		return nil
	}

	pbRC := &pb.RootCauseAnalysis{
		RootCauseSummary: "", // Not available in semantic_types version
		Confidence:       rc.Confidence,
	}

	// Note: RootCauseAnalysis in semantic_types.go has different structure
	// Map CausalChain from CausalLinkDetail to pb.CausalLink
	for _, link := range rc.CausalChain {
		pbRC.CausalChain = append(pbRC.CausalChain, &pb.CausalLink{
			FromEventId:      link.FromEvent,
			ToEventId:        link.ToEvent,
			RelationshipType: link.Mechanism,
			Confidence:       link.Probability,
			TimeDelta:        durationpb.New(link.Latency),
		})
	}

	// Convert contributing factors
	for _, factor := range rc.ContributingFactors {
		pbRC.ContributingFactors = append(pbRC.ContributingFactors, &pb.ContributingFactor{
			Factor:   factor.Factor,
			Weight:   factor.Impact,
			Evidence: factor.Type + "; " + factor.Remediation,
		})
	}

	// Convert evidence
	for _, evidence := range rc.Evidence {
		pbRC.Evidence = append(pbRC.Evidence, &pb.Evidence{
			Type:           "root_cause",
			Description:    evidence,
			Data:           nil,
			RelevanceScore: 0.8,
		})
	}

	return pbRC
}

func convertRootCauseFromProto(rc *pb.RootCauseAnalysis) *correlation.RootCauseAnalysis {
	if rc == nil {
		return nil
	}

	result := &correlation.RootCauseAnalysis{
		RootCauseEvent:      nil, // Would need to be set separately
		CausalChain:         []correlation.CausalLinkDetail{},
		ContributingFactors: []correlation.ContributingFactor{},
		Confidence:          rc.Confidence,
		Evidence:            []string{},
	}

	// Convert causal chain
	for _, link := range rc.CausalChain {
		result.CausalChain = append(result.CausalChain, correlation.CausalLinkDetail{
			FromEvent:   link.FromEventId,
			ToEvent:     link.ToEventId,
			Mechanism:   link.RelationshipType,
			Probability: link.Confidence,
			Latency:     link.TimeDelta.AsDuration(),
		})
	}

	// Convert contributing factors
	for _, factor := range rc.ContributingFactors {
		result.ContributingFactors = append(result.ContributingFactors, correlation.ContributingFactor{
			Factor:      factor.Factor,
			Impact:      factor.Weight,
			Type:        "unknown", // Extract from evidence if available
			Remediation: "",        // Extract from evidence if available
		})
	}

	// Convert evidence
	for _, evidence := range rc.Evidence {
		result.Evidence = append(result.Evidence, evidence.Description)
	}

	return result
}

// Action conversions

func convertActionToProto(action string) *pb.RecommendedAction {
	// Simple conversion from string action to RecommendedAction proto
	return &pb.RecommendedAction{
		Id:                fmt.Sprintf("action_%d", rand.Int()),
		Title:             "Recommended Action",
		Description:       action,
		Type:              pb.RecommendedAction_ACTION_TYPE_INVESTIGATE,
		Priority:          pb.RecommendedAction_PRIORITY_MEDIUM,
		Commands:          []string{action},
		Parameters:        map[string]string{},
		ExpectedResult:    "Issue resolution",
		EstimatedDuration: durationpb.New(10 * time.Minute),
		RiskLevel:         "medium",
		RiskDescription:   "Standard operational action",
	}
}

func convertRecommendedActionToProto(action correlation.RecommendedAction) *pb.RecommendedAction {
	return &pb.RecommendedAction{
		Id:                action.ID,
		Title:             action.Title,
		Description:       action.Description,
		Type:              pb.RecommendedAction_ActionType(action.Type),
		Priority:          pb.RecommendedAction_Priority(action.Priority),
		Commands:          action.Commands,
		Parameters:        action.Parameters,
		ExpectedResult:    action.ExpectedResult,
		EstimatedDuration: durationpb.New(action.EstimatedDuration),
		RiskLevel:         action.RiskLevel,
		RiskDescription:   action.RiskDescription,
	}
}

func convertActionFromProto(action *pb.RecommendedAction) string {
	// Simple conversion from RecommendedAction proto to string
	if action == nil {
		return ""
	}
	return action.Description
}

func convertRecommendedActionFromProto(action *pb.RecommendedAction) correlation.RecommendedAction {
	return correlation.RecommendedAction{
		ID:                action.Id,
		Title:             action.Title,
		Description:       action.Description,
		Type:              correlation.ActionType(action.Type),
		Priority:          correlation.ActionPriority(action.Priority),
		Commands:          action.Commands,
		Parameters:        action.Parameters,
		ExpectedResult:    action.ExpectedResult,
		EstimatedDuration: action.EstimatedDuration.AsDuration(),
		RiskLevel:         action.RiskLevel,
		RiskDescription:   action.RiskDescription,
	}
}

// Insight conversions

func convertInsightToProto(insight *correlation.CorrelationInsight) *pb.Insight {
	if insight == nil {
		return nil
	}

	pbInsight := &pb.Insight{
		Id:               insight.ID,
		Type:             insight.Type,
		Title:            insight.Title,
		Summary:          insight.Description,
		EventIds:         insight.RelatedEvents,
		CorrelationIds:   []string{}, // Not available in CorrelationInsight
		SemanticGroupIds: []string{}, // Not available in CorrelationInsight
		Confidence:       insight.Confidence,
		CreatedAt:        timestamppb.New(insight.Timestamp),
		Metadata:         convertMetadataToStringMap(insight.Metadata),
	}

	// Convert explanation from metadata if available
	if explanationData, ok := insight.Metadata["explanation"]; ok {
		if explanationMap, ok := explanationData.(map[string]interface{}); ok {
			pbInsight.Explanation = &pb.HumanExplanation{
				TechnicalExplanation: getStringFromMap(explanationMap, "technical"),
				BusinessExplanation:  getStringFromMap(explanationMap, "business"),
				ExecutiveSummary:     getStringFromMap(explanationMap, "executive"),
			}
		}
	}

	// Convert impact from metadata if available
	if impactData, ok := insight.Metadata["impact"]; ok {
		if impact, ok := impactData.(*correlation.ImpactAssessment); ok {
			pbInsight.Impact = convertImpactToProto(impact)
		}
	}

	// Convert actions
	for _, action := range insight.Actions {
		pbInsight.Actions = append(pbInsight.Actions, &pb.RecommendedAction{
			Id:          fmt.Sprintf("action_%d", rand.Int()),
			Title:       action.Title,
			Description: action.Description,
			RiskLevel:   action.Risk,
		})
	}

	return pbInsight
}

// Filter conversions

func convertFilterFromProto(filter *pb.Filter) *correlation.Filter {
	if filter == nil {
		return nil
	}

	result := &correlation.Filter{
		Query:             filter.SearchText,
		EventTypes:        make([]correlation.EventType, 0, len(filter.EventTypes)),
		Severities:        make([]correlation.EventSeverity, 0, len(filter.Severities)),
		Sources:           make([]correlation.SourceType, 0, len(filter.SourceTypes)),
		ResourceTypes:     []string{},              // Not available in proto Filter
		ResourceIDs:       []string{},              // Not available in proto Filter
		TraceIDs:          []string{},              // Not available in proto Filter
		CorrelationIDs:    []string{},              // Not available in proto Filter
		SemanticGroupIDs:  []string{},              // Not available in proto Filter
		Labels:            make(map[string]string), // Convert from label selectors
		HasCorrelations:   false,                   // Not available in proto Filter
		HasSemanticGroups: false,                   // Not available in proto Filter
		MinConfidence:     0.0,                     // Not available in proto Filter
		Limit:             0,                       // Not available in proto Filter
	}

	// Convert enums
	for _, t := range filter.EventTypes {
		result.EventTypes = append(result.EventTypes, correlation.EventType(t))
	}
	for _, s := range filter.Severities {
		result.Severities = append(result.Severities, correlation.EventSeverity(s))
	}
	for _, s := range filter.SourceTypes {
		result.Sources = append(result.Sources, correlation.SourceType(s))
	}

	// Convert time range
	if filter.TimeRange != nil {
		result.TimeRange = &correlation.TimeRange{
			Start: filter.TimeRange.Start.AsTime(),
			End:   filter.TimeRange.End.AsTime(),
		}
	}

	return result
}

func convertTimeRangeFromProto(tr *pb.TimeRange) *correlation.TimeRange {
	if tr == nil {
		return nil
	}

	return &correlation.TimeRange{
		Start: tr.Start.AsTime(),
		End:   tr.End.AsTime(),
	}
}

// EventStore Subscribe method signature
type UnsubscribeFunc func()

// Helper functions

func convertMetadataToStringMap(metadata map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range metadata {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

func getStringFromMap(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
		return fmt.Sprintf("%v", val)
	}
	return ""
}

func convertStringMapToInterface(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}

func convertEventTypeToProto(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_HTTP // Map service to HTTP for now
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeMemory:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeCPU:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeDisk:
		return pb.EventType_EVENT_TYPE_FILE_SYSTEM
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func convertEventSeverityToProto(severity domain.EventSeverity) pb.EventSeverity {
	switch severity {
	case domain.EventSeverityDebug:
		return pb.EventSeverity_EVENT_SEVERITY_DEBUG
	case domain.EventSeverityInfo:
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	case domain.EventSeverityLow:
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	case domain.EventSeverityMedium:
		return pb.EventSeverity_EVENT_SEVERITY_WARNING
	case domain.EventSeverityWarning:
		return pb.EventSeverity_EVENT_SEVERITY_WARNING
	case domain.EventSeverityHigh:
		return pb.EventSeverity_EVENT_SEVERITY_ERROR
	case domain.EventSeverityError:
		return pb.EventSeverity_EVENT_SEVERITY_ERROR
	case domain.EventSeverityCritical:
		return pb.EventSeverity_EVENT_SEVERITY_CRITICAL
	default:
		return pb.EventSeverity_EVENT_SEVERITY_UNSPECIFIED
	}
}

func convertSourceTypeToProto(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_SYSLOG // Map systemd to syslog
	case domain.SourceCNI:
		return pb.SourceType_SOURCE_TYPE_CONTAINERD // Map CNI to containerd
	default:
		return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
	}
}

func convertInterfaceMapToString(m map[string]interface{}) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string)
	for k, v := range m {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}
