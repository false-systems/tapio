package grpc

import (
	"math/rand"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Event conversions

func convertEventFromProto(event *pb.Event) *domain.Event {
	if event == nil {
		return nil
	}

	return &domain.Event{
		ID:           event.Id,
		Type:         domain.EventType(event.Type),
		Severity:     domain.EventSeverity(event.Severity),
		Source:       domain.SourceType(event.Source),
		Message:      event.Message,
		Description:  event.Description,
		Timestamp:    event.Timestamp.AsTime(),
		TraceID:      event.TraceId,
		SpanID:       event.SpanId,
		ParentSpanID: event.ParentSpanId,
		Attributes:   event.Attributes,
		CollectorID:  event.CollectorId,
		Confidence:   event.Confidence,
		Tags:         event.Tags,
	}
}

func convertEventToProto(event *domain.Event) *pb.Event {
	if event == nil {
		return nil
	}

	pbEvent := &pb.Event{
		Id:           event.ID,
		Type:         pb.EventType(event.Type),
		Severity:     pb.EventSeverity(event.Severity),
		Source:       pb.SourceType(event.Source),
		Message:      event.Message,
		Description:  event.Description,
		Timestamp:    timestamppb.New(event.Timestamp),
		TraceId:      event.TraceID,
		SpanId:       event.SpanID,
		ParentSpanId: event.ParentSpanID,
		Attributes:   event.Attributes,
		CollectorId:  event.CollectorID,
		Confidence:   event.Confidence,
		Tags:         event.Tags,
	}

	// Convert context
	pbEvent.Context = &pb.EventContext{
		TraceId:      event.TraceID,
		SpanId:       event.SpanID,
		ParentSpanId: event.ParentSpanID,
		TraceFlags:   event.TraceFlags,
		Labels:       event.Labels,
	}

	// Convert resources
	for _, res := range event.Resources {
		pbEvent.Resources = append(pbEvent.Resources, &pb.ResourceIdentifier{
			Type:       res.Type,
			Id:         res.ID,
			Name:       res.Name,
			Namespace:  res.Namespace,
			Region:     res.Region,
			Attributes: res.Attributes,
		})
	}

	// Convert metrics
	for _, metric := range event.Metrics {
		pbEvent.Metrics = append(pbEvent.Metrics, &pb.MetricValue{
			Name:  metric.Name,
			Value: metric.Value,
			Unit:  metric.Unit,
			Type:  metric.Type,
		})
	}

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
		pbCorr.Actions = append(pbCorr.Actions, convertActionToProto(action))
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
		result.RecommendedActions = append(result.RecommendedActions, convertActionFromProto(action))
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

	return &pb.ImpactAssessment{
		Level:                   pb.ImpactAssessment_ImpactLevel(impact.Level),
		BusinessImpactScore:     impact.BusinessImpactScore,
		TechnicalImpactScore:    impact.TechnicalImpactScore,
		AffectedServices:        impact.AffectedServices,
		EstimatedDuration:       durationpb.New(impact.EstimatedDuration),
		AffectedUsers:           impact.AffectedUsers,
		AffectedRequests:        impact.AffectedRequests,
		EstimatedCost:           impact.EstimatedCost,
		Currency:                impact.Currency,
		CascadeProbability:      impact.CascadeProbability,
		PotentialCascadeTargets: impact.PotentialCascadeTargets,
	}
}

func convertImpactFromProto(impact *pb.ImpactAssessment) *correlation.ImpactAssessment {
	if impact == nil {
		return nil
	}

	return &correlation.ImpactAssessment{
		Level:                   correlation.ImpactLevel(impact.Level),
		BusinessImpactScore:     impact.BusinessImpactScore,
		TechnicalImpactScore:    impact.TechnicalImpactScore,
		AffectedServices:        impact.AffectedServices,
		EstimatedDuration:       impact.EstimatedDuration.AsDuration(),
		AffectedUsers:           impact.AffectedUsers,
		AffectedRequests:        impact.AffectedRequests,
		EstimatedCost:           impact.EstimatedCost,
		Currency:                impact.Currency,
		CascadeProbability:      impact.CascadeProbability,
		PotentialCascadeTargets: impact.PotentialCascadeTargets,
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
		Confidence:          pred.Confidence,
		ModelVersion:        pred.ModelVersion,
		PredictionTimestamp: timestamppb.New(pred.PredictionTimestamp),
	}

	// Convert predicted events
	for _, event := range pred.PredictedEvents {
		pbPred.PredictedEvents = append(pbPred.PredictedEvents, &pb.PredictedEvent{
			Type:          pb.EventType(event.Type),
			Severity:      pb.EventSeverity(event.Severity),
			Description:   event.Description,
			Probability:   event.Probability,
			EstimatedTime: durationpb.New(event.EstimatedTime),
		})
	}

	return pbPred
}

func convertPredictionFromProto(pred *pb.PredictedOutcome) *correlation.PredictedOutcome {
	if pred == nil {
		return nil
	}

	result := &correlation.PredictedOutcome{
		Scenario:            pred.Scenario,
		Probability:         pred.Probability,
		TimeToOutcome:       pred.TimeToOutcome.AsDuration(),
		PreventionActions:   pred.PreventionActions,
		Confidence:          pred.Confidence,
		ModelVersion:        pred.ModelVersion,
		PredictionTimestamp: pred.PredictionTimestamp.AsTime(),
	}

	// Convert predicted events
	for _, event := range pred.PredictedEvents {
		result.PredictedEvents = append(result.PredictedEvents, correlation.PredictedEvent{
			Type:          correlation.EventType(event.Type),
			Severity:      correlation.EventSeverity(event.Severity),
			Description:   event.Description,
			Probability:   event.Probability,
			EstimatedTime: event.EstimatedTime.AsDuration(),
		})
	}

	return result
}

// Root cause conversions

func convertRootCauseToProto(rc *correlation.RootCauseAnalysis) *pb.RootCauseAnalysis {
	if rc == nil {
		return nil
	}

	pbRC := &pb.RootCauseAnalysis{
		RootCauseSummary: rc.Summary,
		Confidence:       rc.Confidence,
	}

	// Convert causal factors
	for _, factor := range rc.CausalFactors {
		pbRC.CausalFactors = append(pbRC.CausalFactors, &pb.CausalFactor{
			Id:                 factor.ID,
			Description:        factor.Description,
			ContributionWeight: factor.ContributionWeight,
			Category:           factor.Category,
		})
	}

	// Convert causal chain
	for _, link := range rc.CausalChain {
		pbRC.CausalChain = append(pbRC.CausalChain, &pb.CausalLink{
			FromEventId:      link.FromEventID,
			ToEventId:        link.ToEventID,
			RelationshipType: link.RelationshipType,
			Confidence:       link.Confidence,
			TimeDelta:        durationpb.New(link.TimeDelta),
		})
	}

	// Convert contributing factors
	for _, factor := range rc.ContributingFactors {
		pbRC.ContributingFactors = append(pbRC.ContributingFactors, &pb.ContributingFactor{
			Factor:   factor.Factor,
			Weight:   factor.Weight,
			Evidence: factor.Evidence,
		})
	}

	// Convert evidence
	for _, evidence := range rc.Evidence {
		data, _ := structpb.NewStruct(evidence.Data)
		pbRC.Evidence = append(pbRC.Evidence, &pb.Evidence{
			Type:           evidence.Type,
			Description:    evidence.Description,
			Data:           data,
			RelevanceScore: evidence.RelevanceScore,
		})
	}

	return pbRC
}

func convertRootCauseFromProto(rc *pb.RootCauseAnalysis) *correlation.RootCauseAnalysis {
	if rc == nil {
		return nil
	}

	result := &correlation.RootCauseAnalysis{
		Summary:    rc.RootCauseSummary,
		Confidence: rc.Confidence,
	}

	// Convert causal factors
	for _, factor := range rc.CausalFactors {
		result.CausalFactors = append(result.CausalFactors, correlation.CausalFactor{
			ID:                 factor.Id,
			Description:        factor.Description,
			ContributionWeight: factor.ContributionWeight,
			Category:           factor.Category,
		})
	}

	// Convert causal chain
	for _, link := range rc.CausalChain {
		result.CausalChain = append(result.CausalChain, correlation.CausalLink{
			FromEventID:      link.FromEventId,
			ToEventID:        link.ToEventId,
			RelationshipType: link.RelationshipType,
			Confidence:       link.Confidence,
			TimeDelta:        link.TimeDelta.AsDuration(),
		})
	}

	// Convert contributing factors
	for _, factor := range rc.ContributingFactors {
		result.ContributingFactors = append(result.ContributingFactors, correlation.ContributingFactor{
			Factor:   factor.Factor,
			Weight:   factor.Weight,
			Evidence: factor.Evidence,
		})
	}

	// Convert evidence
	for _, evidence := range rc.Evidence {
		var data map[string]interface{}
		if evidence.Data != nil {
			data = evidence.Data.AsMap()
		}
		result.Evidence = append(result.Evidence, correlation.Evidence{
			Type:           evidence.Type,
			Description:    evidence.Description,
			Data:           data,
			RelevanceScore: evidence.RelevanceScore,
		})
	}

	return result
}

// Action conversions

func convertActionToProto(action correlation.RecommendedAction) *pb.RecommendedAction {
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

func convertActionFromProto(action *pb.RecommendedAction) correlation.RecommendedAction {
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

func convertInsightToProto(insight *correlation.Insight) *pb.Insight {
	if insight == nil {
		return nil
	}

	pbInsight := &pb.Insight{
		Id:               insight.ID,
		Type:             insight.Type,
		Title:            insight.Title,
		Summary:          insight.Summary,
		EventIds:         insight.EventIDs,
		CorrelationIds:   insight.CorrelationIDs,
		SemanticGroupIds: insight.SemanticGroupIDs,
		Confidence:       insight.Confidence,
		CreatedAt:        timestamppb.New(insight.CreatedAt),
		Metadata:         insight.Metadata,
	}

	// Convert explanation
	if insight.Explanation != nil {
		pbInsight.Explanation = &pb.HumanExplanation{
			TechnicalExplanation: insight.Explanation.Technical,
			BusinessExplanation:  insight.Explanation.Business,
			ExecutiveSummary:     insight.Explanation.Executive,
		}

		// Convert visualizations
		for _, viz := range insight.Explanation.Visualizations {
			data, _ := structpb.NewStruct(viz.Data)
			pbInsight.Explanation.Visualizations = append(pbInsight.Explanation.Visualizations, &pb.Visualization{
				Type:    viz.Type,
				Title:   viz.Title,
				Data:    data,
				Options: viz.Options,
			})
		}

		// Convert key metrics
		for _, metric := range insight.Explanation.KeyMetrics {
			pbInsight.Explanation.KeyMetrics = append(pbInsight.Explanation.KeyMetrics, &pb.KeyMetric{
				Name:     metric.Name,
				Value:    metric.Value,
				Unit:     metric.Unit,
				Trend:    metric.Trend,
				Severity: metric.Severity,
			})
		}
	}

	// Convert impact
	if insight.Impact != nil {
		pbInsight.Impact = convertImpactToProto(insight.Impact)
	}

	// Convert actions
	for _, action := range insight.Actions {
		pbInsight.Actions = append(pbInsight.Actions, convertActionToProto(action))
	}

	return pbInsight
}

// Filter conversions

func convertFilterFromProto(filter *pb.Filter) *correlation.Filter {
	if filter == nil {
		return nil
	}

	result := &correlation.Filter{
		Query:             filter.Query,
		EventTypes:        make([]correlation.EventType, 0, len(filter.EventTypes)),
		Severities:        make([]correlation.EventSeverity, 0, len(filter.Severities)),
		Sources:           make([]correlation.SourceType, 0, len(filter.Sources)),
		ResourceTypes:     filter.ResourceTypes,
		ResourceIDs:       filter.ResourceIds,
		TraceIDs:          filter.TraceIds,
		CorrelationIDs:    filter.CorrelationIds,
		SemanticGroupIDs:  filter.SemanticGroupIds,
		Labels:            filter.Labels,
		HasCorrelations:   filter.HasCorrelations,
		HasSemanticGroups: filter.HasSemanticGroups,
		MinConfidence:     filter.MinConfidence,
		Limit:             int(filter.Limit),
	}

	// Convert enums
	for _, t := range filter.EventTypes {
		result.EventTypes = append(result.EventTypes, correlation.EventType(t))
	}
	for _, s := range filter.Severities {
		result.Severities = append(result.Severities, correlation.EventSeverity(s))
	}
	for _, s := range filter.Sources {
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
