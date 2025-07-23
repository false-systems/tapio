package grpc

import (
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/protobuf/types/known/durationpb"
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
		RawData:     event.RawData,
	}

	// Map event type
	pe.Type = mapEventTypeToProto(event.Type)

	// Map severity
	if event.Severity != "" {
		pe.Severity = mapSeverityToProto(string(event.Severity))
	} else if event.Impact != nil {
		pe.Severity = mapSeverityToProto(event.Impact.Severity)
	}

	// Map source
	pe.Source = mapSourceToProto(event.Source)

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

	// Convert AI features
	if event.AiFeatures != nil {
		pe.AiFeatures = make(map[string]float32)
		for k, v := range event.AiFeatures {
			pe.AiFeatures[k] = v
		}
	}

	// Set correlation hints
	pe.CorrelationIds = event.CorrelationHints

	// Convert SemanticContext
	if event.Semantic != nil {
		pe.SemanticContext = &pb.SemanticContext{
			Intent:           event.Semantic.Intent,
			Category:         event.Semantic.Category,
			Tags:             event.Semantic.Tags,
			Narrative:        event.Semantic.Narrative,
			Confidence:       event.Semantic.Confidence,
			Domain:           event.Semantic.Domain,
			Concepts:         event.Semantic.Concepts,
			Embedding:        event.Semantic.Embedding,
			EventType:        event.Semantic.EventType,
			IntentConfidence: event.Semantic.IntentConfidence,
			SemanticFeatures: event.Semantic.SemanticFeatures,
			OntologyTags:     event.Semantic.OntologyTags,
			Description:      event.Semantic.Description,
		}
	}

	// Convert EntityContext
	if event.Entity != nil {
		pe.EntityContext = &pb.EntityContext{
			Type:       event.Entity.Type,
			Name:       event.Entity.Name,
			Namespace:  event.Entity.Namespace,
			Uid:        event.Entity.UID,
			Labels:     event.Entity.Labels,
			Attributes: event.Entity.Attributes,
		}
	}

	// Convert ImpactContext
	if event.Impact != nil {
		pe.ImpactContext = &pb.ImpactContext{
			Severity:         event.Impact.Severity,
			BusinessImpact:   event.Impact.BusinessImpact,
			AffectedServices: event.Impact.AffectedServices,
			AffectedUsers:    int32(event.Impact.AffectedUsers),
			SloImpact:        event.Impact.SLOImpact,
			CustomerFacing:   event.Impact.CustomerFacing,
			RevenueImpacting: event.Impact.RevenueImpacting,
		}
	}

	// Convert CorrelationContext
	if event.Correlation != nil {
		pe.CorrelationContext = &pb.CorrelationContext{
			CorrelationId: event.Correlation.CorrelationID,
			GroupId:       event.Correlation.GroupID,
			ParentEventId: event.Correlation.ParentEventID,
			CausalChain:   event.Correlation.CausalChain,
			RelatedEvents: event.Correlation.RelatedEvents,
			Pattern:       event.Correlation.Pattern,
			Stage:         event.Correlation.Stage,
		}
	}

	// Convert layer-specific data
	convertLayerDataToProto(event, pe)

	// Convert analysis contexts
	convertAnalysisContextsToProto(event, pe)

	// Convert CollectorMetadata
	if event.CollectorMetadata != nil {
		pe.CollectorMetadata = &pb.CollectorMetadata{
			CollectorId:   event.CollectorMetadata.CollectorID,
			CollectorType: event.CollectorMetadata.CollectorType,
			Version:       event.CollectorMetadata.Version,
			Capabilities:  event.CollectorMetadata.Capabilities,
			Attributes:    event.CollectorMetadata.Attributes,
		}
		if event.CollectorMetadata.LastSeen != nil {
			pe.CollectorMetadata.LastSeen = timestamppb.New(*event.CollectorMetadata.LastSeen)
		}
		if event.CollectorMetadata.Health != nil {
			pe.CollectorMetadata.Health = convertHealthStatusToProto(event.CollectorMetadata.Health)
		}
	}

	// Set EventContext
	pe.Context = &pb.EventContext{}
	if event.Entity != nil {
		pe.Context.Namespace = event.Entity.Namespace
	}
	if event.TraceContext != nil {
		pe.Context.TraceId = event.TraceContext.TraceID
		pe.Context.SpanId = event.TraceContext.SpanID
		pe.Context.ParentSpanId = event.TraceContext.ParentSpanID
	}

	// Set collector_id and processed_at
	if event.CollectorMetadata != nil {
		pe.CollectorId = event.CollectorMetadata.CollectorID
	} else {
		pe.CollectorId = event.Source
	}
	pe.ProcessedAt = timestamppb.New(event.Timestamp)

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
		Source:           mapProtoToSource(event.Source),
		Message:          event.Message,
		Tags:             event.Tags,
		Confidence:       event.Confidence,
		RawData:          event.RawData,
		CorrelationHints: event.CorrelationIds,
	}

	// Set severity
	ue.Severity = mapProtoToSeverity(event.Severity)

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
	}

	// Convert AI features
	if event.AiFeatures != nil {
		ue.AiFeatures = make(map[string]float32)
		for k, v := range event.AiFeatures {
			ue.AiFeatures[k] = v
		}
	}

	// Convert SemanticContext
	if event.SemanticContext != nil {
		ue.Semantic = &domain.SemanticContext{
			Intent:           event.SemanticContext.Intent,
			Category:         event.SemanticContext.Category,
			Tags:             event.SemanticContext.Tags,
			Narrative:        event.SemanticContext.Narrative,
			Confidence:       event.SemanticContext.Confidence,
			Domain:           event.SemanticContext.Domain,
			Concepts:         event.SemanticContext.Concepts,
			Embedding:        event.SemanticContext.Embedding,
			EventType:        event.SemanticContext.EventType,
			IntentConfidence: event.SemanticContext.IntentConfidence,
			SemanticFeatures: event.SemanticContext.SemanticFeatures,
			OntologyTags:     event.SemanticContext.OntologyTags,
			Description:      event.SemanticContext.Description,
		}
	}

	// Convert EntityContext
	if event.EntityContext != nil {
		ue.Entity = &domain.EntityContext{
			Type:       event.EntityContext.Type,
			Name:       event.EntityContext.Name,
			Namespace:  event.EntityContext.Namespace,
			UID:        event.EntityContext.Uid,
			Labels:     event.EntityContext.Labels,
			Attributes: event.EntityContext.Attributes,
		}
	}

	// Convert ImpactContext
	if event.ImpactContext != nil {
		ue.Impact = &domain.ImpactContext{
			Severity:         event.ImpactContext.Severity,
			BusinessImpact:   event.ImpactContext.BusinessImpact,
			AffectedServices: event.ImpactContext.AffectedServices,
			AffectedUsers:    int(event.ImpactContext.AffectedUsers),
			SLOImpact:        event.ImpactContext.SloImpact,
			CustomerFacing:   event.ImpactContext.CustomerFacing,
			RevenueImpacting: event.ImpactContext.RevenueImpacting,
		}
	}

	// Convert CorrelationContext
	if event.CorrelationContext != nil {
		ue.Correlation = &domain.CorrelationContext{
			CorrelationID: event.CorrelationContext.CorrelationId,
			GroupID:       event.CorrelationContext.GroupId,
			ParentEventID: event.CorrelationContext.ParentEventId,
			CausalChain:   event.CorrelationContext.CausalChain,
			RelatedEvents: event.CorrelationContext.RelatedEvents,
			Pattern:       event.CorrelationContext.Pattern,
			Stage:         event.CorrelationContext.Stage,
		}
	}

	// Convert layer-specific data
	convertProtoToLayerData(event, ue)

	// Convert analysis contexts
	convertProtoToAnalysisContexts(event, ue)

	// Convert CollectorMetadata
	if event.CollectorMetadata != nil {
		ue.CollectorMetadata = &domain.CollectorMetadata{
			CollectorID:   event.CollectorMetadata.CollectorId,
			CollectorType: event.CollectorMetadata.CollectorType,
			Version:       event.CollectorMetadata.Version,
			Capabilities:  event.CollectorMetadata.Capabilities,
			Attributes:    event.CollectorMetadata.Attributes,
		}
		if event.CollectorMetadata.LastSeen != nil {
			lastSeen := event.CollectorMetadata.LastSeen.AsTime()
			ue.CollectorMetadata.LastSeen = &lastSeen
		}
		if event.CollectorMetadata.Health != nil {
			ue.CollectorMetadata.Health = convertProtoToHealthStatus(event.CollectorMetadata.Health)
		}
	}

	return ue
}

// Helper functions for layer-specific data conversion

func convertLayerDataToProto(ue *domain.UnifiedEvent, pe *pb.Event) {
	// Convert KernelData
	if ue.Kernel != nil {
		pe.KernelData = &pb.KernelData{
			Syscall:     ue.Kernel.Syscall,
			Pid:         ue.Kernel.PID,
			Tid:         ue.Kernel.TID,
			Uid:         ue.Kernel.UID,
			Gid:         ue.Kernel.GID,
			Comm:        ue.Kernel.Comm,
			ReturnCode:  ue.Kernel.ReturnCode,
			Args:        ue.Kernel.Args,
			StackTrace:  ue.Kernel.StackTrace,
			CpuCore:     int32(ue.Kernel.CPUCore),
			BpfProgram:  ue.Kernel.BPFProgram,
			BpfMapStats: convertIntMapToInt32Map(ue.Kernel.BPFMapStats),
		}
		// Convert KprobeDetails if available
		if ue.Kernel.KprobeDetails != nil {
			pe.KernelData.KprobeDetails = &pb.KprobeDetails{
				ProbeName:      ue.Kernel.KprobeDetails.ProbeName,
				ProbeType:      ue.Kernel.KprobeDetails.ProbeType,
				FunctionName:   ue.Kernel.KprobeDetails.FunctionName,
				Offset:         ue.Kernel.KprobeDetails.Offset,
				RegisterValues: ue.Kernel.KprobeDetails.RegisterValues,
			}
		}
		// Convert SecurityContext if available
		if ue.Kernel.SecurityContext != nil {
			pe.KernelData.SecurityContext = &pb.SecurityContext{
				SelinuxContext:  ue.Kernel.SecurityContext.SELinuxContext,
				AppArmorProfile: ue.Kernel.SecurityContext.AppArmorProfile,
				SeccompProfile:  ue.Kernel.SecurityContext.SeccompProfile,
				Capabilities:    ue.Kernel.SecurityContext.Capabilities,
			}
		}
	}

	// Convert NetworkData
	if ue.Network != nil {
		pe.NetworkData = &pb.NetworkData{
			Protocol:       ue.Network.Protocol,
			SourceIp:       ue.Network.SourceIP,
			SourcePort:     uint32(ue.Network.SourcePort),
			DestIp:         ue.Network.DestIP,
			DestPort:       uint32(ue.Network.DestPort),
			Direction:      ue.Network.Direction,
			BytesSent:      ue.Network.BytesSent,
			BytesRecv:      ue.Network.BytesRecv,
			LatencyNs:      ue.Network.Latency,
			StatusCode:     int32(ue.Network.StatusCode),
			Method:         ue.Network.Method,
			Path:           ue.Network.Path,
			Headers:        ue.Network.Headers,
			ContainerId:    ue.Network.ContainerID,
			InterfaceName:  ue.Network.InterfaceName,
			VirtualNetwork: ue.Network.VirtualNetwork,
		}
		// Convert NetworkPolicyInfo if available
		if ue.Network.NetworkPolicyInfo != nil {
			pe.NetworkData.NetworkPolicyInfo = &pb.NetworkPolicyInfo{
				PolicyName:      ue.Network.NetworkPolicyInfo.PolicyName,
				PolicyType:      ue.Network.NetworkPolicyInfo.PolicyType,
				PolicyNamespace: ue.Network.NetworkPolicyInfo.PolicyNamespace,
				Direction:       ue.Network.NetworkPolicyInfo.Direction,
				Action:          ue.Network.NetworkPolicyInfo.Action,
				Reason:          ue.Network.NetworkPolicyInfo.Reason,
			}
		}
		// Convert IPTablesRule if available
		if ue.Network.IPTablesRule != nil {
			pe.NetworkData.IptablesRule = &pb.IPTablesRule{
				Table:    ue.Network.IPTablesRule.Table,
				Chain:    ue.Network.IPTablesRule.Chain,
				RuleNum:  ue.Network.IPTablesRule.RuleNum,
				Action:   ue.Network.IPTablesRule.Action,
				Protocol: ue.Network.IPTablesRule.Protocol,
				Source:   ue.Network.IPTablesRule.Source,
				Dest:     ue.Network.IPTablesRule.Dest,
				Matched:  ue.Network.IPTablesRule.Matched,
			}
		}
	}

	// Convert ApplicationData
	if ue.Application != nil {
		pe.ApplicationData = &pb.ApplicationData{
			Level:      ue.Application.Level,
			Message:    ue.Application.Message,
			Logger:     ue.Application.Logger,
			ErrorType:  ue.Application.ErrorType,
			StackTrace: ue.Application.StackTrace,
			UserId:     ue.Application.UserID,
			SessionId:  ue.Application.SessionID,
			RequestId:  ue.Application.RequestID,
		}
		if ue.Application.Custom != nil {
			customStruct, err := structpb.NewStruct(ue.Application.Custom)
			if err == nil {
				pe.ApplicationData.Custom = customStruct
			}
		}
	}

	// Convert KubernetesData
	if ue.Kubernetes != nil {
		pe.KubernetesData = &pb.KubernetesData{
			EventType:       ue.Kubernetes.EventType,
			Reason:          ue.Kubernetes.Reason,
			Object:          ue.Kubernetes.Object,
			ObjectKind:      ue.Kubernetes.ObjectKind,
			Message:         ue.Kubernetes.Message,
			Action:          ue.Kubernetes.Action,
			ApiVersion:      ue.Kubernetes.APIVersion,
			ResourceVersion: ue.Kubernetes.ResourceVersion,
			Labels:          ue.Kubernetes.Labels,
			Annotations:     ue.Kubernetes.Annotations,
			ClusterName:     ue.Kubernetes.ClusterName,
		}
		// Convert CustomResourceInfo if available
		if ue.Kubernetes.CustomResourceInfo != nil {
			pe.KubernetesData.CustomResourceInfo = &pb.CustomResourceInfo{
				Group:     ue.Kubernetes.CustomResourceInfo.Group,
				Version:   ue.Kubernetes.CustomResourceInfo.Version,
				Kind:      ue.Kubernetes.CustomResourceInfo.Kind,
				Name:      ue.Kubernetes.CustomResourceInfo.Name,
				Namespace: ue.Kubernetes.CustomResourceInfo.Namespace,
				Operation: ue.Kubernetes.CustomResourceInfo.Operation,
				UserInfo:  ue.Kubernetes.CustomResourceInfo.UserInfo,
			}
		}
		// Convert WebhookInfo if available
		if ue.Kubernetes.WebhookInfo != nil {
			pe.KubernetesData.WebhookInfo = &pb.WebhookInfo{
				Name:          ue.Kubernetes.WebhookInfo.Name,
				Type:          ue.Kubernetes.WebhookInfo.Type,
				Allowed:       ue.Kubernetes.WebhookInfo.Allowed,
				Result:        ue.Kubernetes.WebhookInfo.Result,
				FailurePolicy: ue.Kubernetes.WebhookInfo.FailurePolicy,
				MatchPolicy:   ue.Kubernetes.WebhookInfo.MatchPolicy,
			}
		}
	}

	// Convert MetricsData
	if ue.Metrics != nil {
		pe.MetricsData = &pb.MetricsData{
			MetricName:  ue.Metrics.MetricName,
			Value:       ue.Metrics.Value,
			Unit:        ue.Metrics.Unit,
			Labels:      ue.Metrics.Labels,
			Aggregation: ue.Metrics.Aggregation,
			PeriodMs:    ue.Metrics.Period,
		}
	}
}

func convertProtoToLayerData(pe *pb.Event, ue *domain.UnifiedEvent) {
	// Convert KernelData
	if pe.KernelData != nil {
		ue.Kernel = &domain.KernelData{
			Syscall:     pe.KernelData.Syscall,
			PID:         pe.KernelData.Pid,
			TID:         pe.KernelData.Tid,
			UID:         pe.KernelData.Uid,
			GID:         pe.KernelData.Gid,
			Comm:        pe.KernelData.Comm,
			ReturnCode:  pe.KernelData.ReturnCode,
			Args:        pe.KernelData.Args,
			StackTrace:  pe.KernelData.StackTrace,
			CPUCore:     int(pe.KernelData.CpuCore),
			BPFProgram:  pe.KernelData.BpfProgram,
			BPFMapStats: convertInt32MapToIntMap(pe.KernelData.BpfMapStats),
		}
	}

	// Convert NetworkData
	if pe.NetworkData != nil {
		ue.Network = &domain.NetworkData{
			Protocol:       pe.NetworkData.Protocol,
			SourceIP:       pe.NetworkData.SourceIp,
			SourcePort:     uint16(pe.NetworkData.SourcePort),
			DestIP:         pe.NetworkData.DestIp,
			DestPort:       uint16(pe.NetworkData.DestPort),
			Direction:      pe.NetworkData.Direction,
			BytesSent:      pe.NetworkData.BytesSent,
			BytesRecv:      pe.NetworkData.BytesRecv,
			Latency:        pe.NetworkData.LatencyNs,
			StatusCode:     int(pe.NetworkData.StatusCode),
			Method:         pe.NetworkData.Method,
			Path:           pe.NetworkData.Path,
			Headers:        pe.NetworkData.Headers,
			ContainerID:    pe.NetworkData.ContainerId,
			InterfaceName:  pe.NetworkData.InterfaceName,
			VirtualNetwork: pe.NetworkData.VirtualNetwork,
		}
	}

	// Convert ApplicationData
	if pe.ApplicationData != nil {
		ue.Application = &domain.ApplicationData{
			Level:      pe.ApplicationData.Level,
			Message:    pe.ApplicationData.Message,
			Logger:     pe.ApplicationData.Logger,
			ErrorType:  pe.ApplicationData.ErrorType,
			StackTrace: pe.ApplicationData.StackTrace,
			UserID:     pe.ApplicationData.UserId,
			SessionID:  pe.ApplicationData.SessionId,
			RequestID:  pe.ApplicationData.RequestId,
		}
		if pe.ApplicationData.Custom != nil {
			ue.Application.Custom = pe.ApplicationData.Custom.AsMap()
		}
	}

	// Convert KubernetesData
	if pe.KubernetesData != nil {
		ue.Kubernetes = &domain.KubernetesData{
			EventType:       pe.KubernetesData.EventType,
			Reason:          pe.KubernetesData.Reason,
			Object:          pe.KubernetesData.Object,
			ObjectKind:      pe.KubernetesData.ObjectKind,
			Message:         pe.KubernetesData.Message,
			Action:          pe.KubernetesData.Action,
			APIVersion:      pe.KubernetesData.ApiVersion,
			ResourceVersion: pe.KubernetesData.ResourceVersion,
			Labels:          pe.KubernetesData.Labels,
			Annotations:     pe.KubernetesData.Annotations,
			ClusterName:     pe.KubernetesData.ClusterName,
		}
	}

	// Convert MetricsData
	if pe.MetricsData != nil {
		ue.Metrics = &domain.MetricsData{
			MetricName:  pe.MetricsData.MetricName,
			Value:       pe.MetricsData.Value,
			Unit:        pe.MetricsData.Unit,
			Labels:      pe.MetricsData.Labels,
			Aggregation: pe.MetricsData.Aggregation,
			Period:      pe.MetricsData.PeriodMs,
		}
	}
}

func convertAnalysisContextsToProto(ue *domain.UnifiedEvent, pe *pb.Event) {
	// Convert AnomalyInfo
	if ue.Anomaly != nil {
		pe.AnomalyInfo = &pb.AnomalyInfo{
			Score:        ue.Anomaly.Score,
			Type:         ue.Anomaly.Type,
			Description:  ue.Anomaly.Description,
			Confidence:   ue.Anomaly.Confidence,
			AnomalyScore: ue.Anomaly.AnomalyScore,
		}
		// Convert dimensions if available
		if ue.Anomaly.Dimensions != nil {
			pe.AnomalyInfo.Dimensions = make(map[string]string)
			for k, v := range ue.Anomaly.Dimensions {
				pe.AnomalyInfo.Dimensions[k] = v
			}
		}
		// Convert baseline comparison if available
		if ue.Anomaly.BaselineComparison != nil {
			pe.AnomalyInfo.BaselineComparison = &pb.BaselineComparison{
				BaselineValue:      ue.Anomaly.BaselineComparison.BaselineValue,
				CurrentValue:       ue.Anomaly.BaselineComparison.CurrentValue,
				Deviation:          ue.Anomaly.BaselineComparison.Deviation,
				DeviationPercent:   ue.Anomaly.BaselineComparison.DeviationPercent,
				StandardDeviations: ue.Anomaly.BaselineComparison.StandardDeviations,
			}
		}
	}

	// Convert BehavioralContext
	if ue.Behavioral != nil {
		pe.BehavioralContext = &pb.BehavioralContext{
			Pattern:    ue.Behavioral.Pattern,
			Frequency:  ue.Behavioral.Frequency,
			Confidence: ue.Behavioral.Confidence,
		}
		if ue.Behavioral.Metadata != nil {
			metaStruct, err := structpb.NewStruct(ue.Behavioral.Metadata)
			if err == nil {
				pe.BehavioralContext.Metadata = metaStruct
			}
		}
	}

	// Convert TemporalContext
	if ue.Temporal != nil {
		pe.TemporalContext = &pb.TemporalContext{
			Period:      durationpb.New(ue.Temporal.Period),
			Frequency:   ue.Temporal.Frequency,
			Duration:    durationpb.New(ue.Temporal.Duration),
			Periodicity: ue.Temporal.Periodicity,
			Seasonality: convertFloat64MapToDoubleMap(ue.Temporal.Seasonality),
		}
		// Convert temporal patterns if available
		if ue.Temporal.Patterns != nil {
			pe.TemporalContext.Patterns = make([]*pb.TemporalPattern, 0, len(ue.Temporal.Patterns))
			for _, pattern := range ue.Temporal.Patterns {
				pe.TemporalContext.Patterns = append(pe.TemporalContext.Patterns, &pb.TemporalPattern{
					Type:        pattern.Type,
					Description: pattern.Description,
					Confidence:  pattern.Confidence,
					Parameters:  pattern.Parameters,
				})
			}
		}
	}

	// Convert StateInfo
	if ue.State != nil {
		pe.StateInfo = &pb.StateInfo{
			Current:    ue.State.Current,
			Previous:   ue.State.Previous,
			Transition: ue.State.Transition,
			Duration:   durationpb.New(ue.State.Duration),
			Metadata:   ue.State.Metadata,
		}
		// Convert time series data if available
		if ue.State.TimeSeriesData != nil {
			pe.StateInfo.TimeSeriesData = make([]*pb.TimeSeriesPoint, 0, len(ue.State.TimeSeriesData))
			for _, point := range ue.State.TimeSeriesData {
				pe.StateInfo.TimeSeriesData = append(pe.StateInfo.TimeSeriesData, &pb.TimeSeriesPoint{
					Timestamp: timestamppb.New(point.Timestamp),
					Value:     point.Value,
					State:     point.State,
					Metadata:  point.Metadata,
				})
			}
		}
	}

	// Convert CausalityContext
	if ue.Causality != nil {
		pe.CausalityContext = &pb.CausalityContext{
			RootCause:   ue.Causality.RootCause,
			CausalChain: ue.Causality.CausalChain,
			Confidence:  ue.Causality.Confidence,
		}
		if ue.Causality.Metadata != nil {
			metaStruct, err := structpb.NewStruct(ue.Causality.Metadata)
			if err == nil {
				pe.CausalityContext.Metadata = metaStruct
			}
		}
	}
}

func convertProtoToAnalysisContexts(pe *pb.Event, ue *domain.UnifiedEvent) {
	// Convert AnomalyInfo
	if pe.AnomalyInfo != nil {
		ue.Anomaly = &domain.AnomalyInfo{
			Score:        pe.AnomalyInfo.Score,
			Type:         pe.AnomalyInfo.Type,
			Description:  pe.AnomalyInfo.Description,
			Confidence:   pe.AnomalyInfo.Confidence,
			AnomalyScore: pe.AnomalyInfo.AnomalyScore,
		}
	}

	// Convert BehavioralContext
	if pe.BehavioralContext != nil {
		ue.Behavioral = &domain.BehavioralContext{
			Pattern:    pe.BehavioralContext.Pattern,
			Frequency:  pe.BehavioralContext.Frequency,
			Confidence: pe.BehavioralContext.Confidence,
		}
		if pe.BehavioralContext.Metadata != nil {
			ue.Behavioral.Metadata = pe.BehavioralContext.Metadata.AsMap()
		}
	}

	// Convert TemporalContext
	if pe.TemporalContext != nil {
		ue.Temporal = &domain.TemporalContext{
			Frequency:   pe.TemporalContext.Frequency,
			Periodicity: pe.TemporalContext.Periodicity,
			Seasonality: convertDoubleMapToFloat64Map(pe.TemporalContext.Seasonality),
		}
		if pe.TemporalContext.Period != nil {
			ue.Temporal.Period = pe.TemporalContext.Period.AsDuration()
		}
		if pe.TemporalContext.Duration != nil {
			ue.Temporal.Duration = pe.TemporalContext.Duration.AsDuration()
		}
	}

	// Convert StateInfo
	if pe.StateInfo != nil {
		ue.State = &domain.StateInfo{
			Current:    pe.StateInfo.Current,
			Previous:   pe.StateInfo.Previous,
			Transition: pe.StateInfo.Transition,
			Metadata:   pe.StateInfo.Metadata,
		}
		if pe.StateInfo.Duration != nil {
			ue.State.Duration = pe.StateInfo.Duration.AsDuration()
		}
	}

	// Convert CausalityContext
	if pe.CausalityContext != nil {
		ue.Causality = &domain.CausalityContext{
			RootCause:   pe.CausalityContext.RootCause,
			CausalChain: pe.CausalityContext.CausalChain,
			Confidence:  pe.CausalityContext.Confidence,
		}
		if pe.CausalityContext.Metadata != nil {
			ue.Causality.Metadata = pe.CausalityContext.Metadata.AsMap()
		}
	}
}

// Utility conversion functions

func convertIntMapToInt32Map(m map[string]int) map[string]int32 {
	if m == nil {
		return nil
	}
	result := make(map[string]int32)
	for k, v := range m {
		result[k] = int32(v)
	}
	return result
}

func convertInt32MapToIntMap(m map[string]int32) map[string]int {
	if m == nil {
		return nil
	}
	result := make(map[string]int)
	for k, v := range m {
		result[k] = int(v)
	}
	return result
}

func convertFloat64MapToDoubleMap(m map[string]float64) map[string]float64 {
	return m // Same type, just return
}

func convertDoubleMapToFloat64Map(m map[string]float64) map[string]float64 {
	return m // Same type, just return
}

func convertHealthStatusToProto(health *domain.HealthStatus) *pb.HealthStatus {
	if health == nil {
		return nil
	}
	return &pb.HealthStatus{
		Status:    pb.HealthStatus_Status(pb.HealthStatus_Status_value[health.Status]),
		Message:   health.Message,
		LastCheck: timestamppb.New(health.LastCheck),
		Details:   health.Details,
	}
}

func convertProtoToHealthStatus(health *pb.HealthStatus) *domain.HealthStatus {
	if health == nil {
		return nil
	}
	return &domain.HealthStatus{
		Status:    health.Status.String(),
		Message:   health.Message,
		LastCheck: health.LastCheck.AsTime(),
		Details:   health.Details,
	}
}

// Mapping functions

func mapEventTypeToProto(eventType domain.EventType) pb.EventType {
	// Map domain event types to protobuf event types
	switch eventType {
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeSyscall:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeContainer:
		return pb.EventType_EVENT_TYPE_CONTAINER
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeHTTP:
		return pb.EventType_EVENT_TYPE_HTTP
	case domain.EventTypeDatabase:
		return pb.EventType_EVENT_TYPE_DATABASE
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func mapProtoToEventType(eventType pb.EventType) domain.EventType {
	switch eventType {
	case pb.EventType_EVENT_TYPE_NETWORK:
		return domain.EventTypeNetwork
	case pb.EventType_EVENT_TYPE_SYSCALL:
		return domain.EventTypeSyscall
	case pb.EventType_EVENT_TYPE_PROCESS:
		return domain.EventTypeProcess
	case pb.EventType_EVENT_TYPE_CONTAINER:
		return domain.EventTypeContainer
	case pb.EventType_EVENT_TYPE_KUBERNETES:
		return domain.EventTypeKubernetes
	case pb.EventType_EVENT_TYPE_HTTP:
		return domain.EventTypeHTTP
	case pb.EventType_EVENT_TYPE_DATABASE:
		return domain.EventTypeDatabase
	default:
		return domain.EventTypeUnspecified
	}
}

func mapSeverityToProto(severity string) pb.EventSeverity {
	switch severity {
	case "debug", "DEBUG":
		return pb.EventSeverity_EVENT_SEVERITY_DEBUG
	case "info", "INFO":
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	case "warning", "warn", "WARNING", "WARN":
		return pb.EventSeverity_EVENT_SEVERITY_WARNING
	case "error", "ERROR":
		return pb.EventSeverity_EVENT_SEVERITY_ERROR
	case "critical", "CRITICAL":
		return pb.EventSeverity_EVENT_SEVERITY_CRITICAL
	default:
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	}
}

func mapProtoToSeverity(severity pb.EventSeverity) domain.EventSeverity {
	switch severity {
	case pb.EventSeverity_EVENT_SEVERITY_DEBUG:
		return domain.EventSeverity("debug")
	case pb.EventSeverity_EVENT_SEVERITY_INFO:
		return domain.EventSeverity("info")
	case pb.EventSeverity_EVENT_SEVERITY_WARNING:
		return domain.EventSeverity("warning")
	case pb.EventSeverity_EVENT_SEVERITY_ERROR:
		return domain.EventSeverity("error")
	case pb.EventSeverity_EVENT_SEVERITY_CRITICAL:
		return domain.EventSeverity("critical")
	default:
		return domain.EventSeverity("info")
	}
}

func mapSourceToProto(source string) pb.SourceType {
	switch source {
	case "ebpf":
		return pb.SourceType_SOURCE_TYPE_EBPF
	case "kubernetes", "k8s":
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case "docker":
		return pb.SourceType_SOURCE_TYPE_DOCKER
	case "otel":
		return pb.SourceType_SOURCE_TYPE_OTEL
	default:
		return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
	}
}

func mapProtoToSource(source pb.SourceType) string {
	switch source {
	case pb.SourceType_SOURCE_TYPE_EBPF:
		return "ebpf"
	case pb.SourceType_SOURCE_TYPE_KUBERNETES_API:
		return "kubernetes"
	case pb.SourceType_SOURCE_TYPE_DOCKER:
		return "docker"
	case pb.SourceType_SOURCE_TYPE_OTEL:
		return "otel"
	default:
		return "unknown"
	}
}
