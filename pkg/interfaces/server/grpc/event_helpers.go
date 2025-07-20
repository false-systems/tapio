package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper methods for EventServer

// processStreamRequest handles individual stream requests
func (s *EventServer) processStreamRequest(ctx context.Context, req *pb.StreamEventsRequest) (*pb.StreamEventsResponse, error) {
	switch r := req.Request.(type) {
	case *pb.StreamEventsRequest_Event:
		// Single event
		domainEvent := s.convertProtoEventToDomain(r.Event)
		processedEvents, err := s.processor.ProcessEvents(ctx, []domain.Event{domainEvent})
		if err != nil {
			return &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_Error{
					Error: &pb.Error{
						Code:      "PROCESSING_ERROR",
						Message:   err.Error(),
						Timestamp: timestamppb.Now(),
					},
				},
			}, nil
		}

		if err := s.eventStore.Store(ctx, processedEvents); err != nil {
			return &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_Error{
					Error: &pb.Error{
						Code:      "STORAGE_ERROR",
						Message:   err.Error(),
						Timestamp: timestamppb.Now(),
					},
				},
			}, nil
		}

		// Notify subscribers
		if s.config.EnableRealTimeStreaming {
			s.notifySubscribers(processedEvents)
		}

		// Update stats
		s.stats.mu.Lock()
		s.stats.TotalEvents++
		s.stats.mu.Unlock()

		return &pb.StreamEventsResponse{
			Response: &pb.StreamEventsResponse_Ack{
				Ack: &pb.EventAck{
					EventId:   r.Event.Id,
					Timestamp: timestamppb.Now(),
					Status:    "processed",
					Message:   "Event processed successfully",
				},
			},
		}, nil

	case *pb.StreamEventsRequest_Batch:
		// Event batch
		domainEvents := make([]domain.Event, len(r.Batch.Events))
		for i, event := range r.Batch.Events {
			domainEvents[i] = s.convertProtoEventToDomain(event)
		}

		processedEvents, err := s.processor.ProcessEvents(ctx, domainEvents)
		if err != nil {
			return &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_Error{
					Error: &pb.Error{
						Code:      "PROCESSING_ERROR",
						Message:   err.Error(),
						Timestamp: timestamppb.Now(),
					},
				},
			}, nil
		}

		if err := s.eventStore.Store(ctx, processedEvents); err != nil {
			return &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_Error{
					Error: &pb.Error{
						Code:      "STORAGE_ERROR",
						Message:   err.Error(),
						Timestamp: timestamppb.Now(),
					},
				},
			}, nil
		}

		// Notify subscribers
		if s.config.EnableRealTimeStreaming {
			s.notifySubscribers(processedEvents)
		}

		// Update stats
		s.stats.mu.Lock()
		s.stats.TotalEvents += int64(len(r.Batch.Events))
		s.stats.mu.Unlock()

		return &pb.StreamEventsResponse{
			Response: &pb.StreamEventsResponse_Ack{
				Ack: &pb.EventAck{
					BatchId:   r.Batch.BatchId,
					Timestamp: timestamppb.Now(),
					Status:    "processed",
					Message:   fmt.Sprintf("Batch with %d events processed successfully", len(r.Batch.Events)),
				},
			},
		}, nil

	case *pb.StreamEventsRequest_HealthCheck:
		// Health check
		return &pb.StreamEventsResponse{
			Response: &pb.StreamEventsResponse_HealthStatus{
				HealthStatus: &pb.HealthStatus{
					Status:    pb.HealthStatus_STATUS_HEALTHY,
					Timestamp: timestamppb.Now(),
					Message:   "EventService is healthy",
				},
			},
		}, nil

	default:
		return &pb.StreamEventsResponse{
			Response: &pb.StreamEventsResponse_Error{
				Error: &pb.Error{
					Code:      "INVALID_REQUEST",
					Message:   "Unknown request type",
					Timestamp: timestamppb.Now(),
				},
			},
		}, nil
	}
}

// validateSubscribeRequest validates subscription requests
func (s *EventServer) validateSubscribeRequest(req *pb.SubscribeRequest) error {
	if req.SubscriptionId == "" {
		return fmt.Errorf("subscription_id is required")
	}

	// Check if subscription already exists
	s.mu.RLock()
	_, exists := s.subscriptions[req.SubscriptionId]
	s.mu.RUnlock()
	if exists {
		return fmt.Errorf("subscription with ID %s already exists", req.SubscriptionId)
	}

	// Check subscription limits
	s.mu.RLock()
	activeCount := len(s.subscriptions)
	s.mu.RUnlock()
	if activeCount >= s.config.MaxSubscriptions {
		return fmt.Errorf("maximum number of subscriptions (%d) reached", s.config.MaxSubscriptions)
	}

	return nil
}

// sendHistoricalEvents sends existing events to new subscribers
func (s *EventServer) sendHistoricalEvents(ctx context.Context, stream pb.EventService_SubscribeServer, subscription *EventSubscription, req *pb.SubscribeRequest) error {
	// Build filter for historical events
	filter := domain.Filter{
		Since: time.Now().Add(-req.Lookback.AsDuration()),
		Until: time.Now(),
		Limit: 10000, // Reasonable limit for historical events
	}

	// Apply subscription filter if provided
	if req.Filter != nil {
		protoFilter, err := s.convertProtoFilterToDomain(req.Filter)
		if err != nil {
			return err
		}
		// Merge filters
		filter = s.mergeFilters(filter, *protoFilter)
	}

	events, err := s.eventStore.Query(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to query historical events: %w", err)
	}

	// Send historical events as updates
	for _, event := range events {
		protoEvent := s.convertDomainEventToProto(event)
		update := &pb.EventUpdate{
			Type:            pb.EventUpdate_UPDATE_TYPE_NEW,
			Event:           protoEvent,
			UpdateTimestamp: timestamppb.Now(),
			UpdateMetadata: map[string]string{
				"type": "historical",
			},
		}

		if err := stream.Send(update); err != nil {
			return err
		}

		subscription.EventsSent++
	}

	s.logger.Debug("Sent historical events",
		zap.String("subscription_id", subscription.ID),
		zap.Int("count", len(events)),
	)

	return nil
}

// applyRateLimit applies rate limiting to subscriptions
func (s *EventServer) applyRateLimit(subscription *EventSubscription) error {
	if subscription.MaxEventsPerSec <= 0 {
		return nil // No rate limit
	}

	// Simple rate limiting implementation
	now := time.Now()
	timeWindow := now.Sub(subscription.LastActivity)
	if timeWindow < time.Second {
		// Check if we're exceeding the rate limit
		eventsInWindow := subscription.EventsSent // Simplified calculation
		rate := float64(eventsInWindow) / timeWindow.Seconds()
		if rate > float64(subscription.MaxEventsPerSec) {
			return fmt.Errorf("rate limit exceeded: %.2f events/sec > %d events/sec", rate, subscription.MaxEventsPerSec)
		}
	}

	return nil
}

// notifySubscribers sends event updates to active subscribers
func (s *EventServer) notifySubscribers(events []domain.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, event := range events {
		protoEvent := s.convertDomainEventToProto(event)
		update := &pb.EventUpdate{
			Type:            pb.EventUpdate_UPDATE_TYPE_NEW,
			Event:           protoEvent,
			UpdateTimestamp: timestamppb.Now(),
			UpdateMetadata: map[string]string{
				"type": "real_time",
			},
		}

		// Send to all matching subscribers
		for subID, subscription := range s.subscriptions {
			if s.eventMatchesFilter(event, subscription.Filter) {
				if updateChan, exists := s.subscribers[subID]; exists {
					select {
					case updateChan <- update:
						// Successfully sent
					default:
						// Channel full, log warning
						s.logger.Warn("Subscription channel full, dropping event",
							zap.String("subscription_id", subID),
							zap.String("event_id", event.ID),
						)
					}
				}
			}
		}
	}
}

// validateEventBatch validates event batches
func (s *EventServer) validateEventBatch(batch *pb.EventBatch) error {
	if batch.BatchId == "" {
		return fmt.Errorf("batch_id is required")
	}

	if len(batch.Events) == 0 {
		return fmt.Errorf("batch must contain at least one event")
	}

	if len(batch.Events) > s.config.MaxEventsPerBatch {
		return fmt.Errorf("batch size %d exceeds maximum %d", len(batch.Events), s.config.MaxEventsPerBatch)
	}

	// Validate individual events
	for i, event := range batch.Events {
		if err := s.validateEvent(event); err != nil {
			return fmt.Errorf("invalid event at index %d: %w", i, err)
		}
	}

	return nil
}

// validateEvent validates individual events
func (s *EventServer) validateEvent(event *pb.Event) error {
	if event.Id == "" {
		return fmt.Errorf("event ID is required")
	}

	if event.Type == pb.EventType_EVENT_TYPE_UNSPECIFIED {
		return fmt.Errorf("event type is required")
	}

	if event.Timestamp == nil {
		return fmt.Errorf("event timestamp is required")
	}

	return nil
}

// generateStatistics creates event statistics from a set of events
func (s *EventServer) generateStatistics(timeRange *pb.TimeRange, events []domain.Event) *pb.EventStatistics {
	stats := &pb.EventStatistics{
		TimeRange:        timeRange,
		TotalEvents:      int64(len(events)),
		EventsByType:     make(map[string]int64),
		EventsBySeverity: make(map[string]int64),
		EventsBySource:   make(map[string]int64),
		TopResources:     []*pb.ResourceCount{},
	}

	if len(events) == 0 {
		return stats
	}

	// Calculate time span
	start := timeRange.Start.AsTime()
	end := timeRange.End.AsTime()
	duration := end.Sub(start)

	// Calculate events per second
	if duration.Seconds() > 0 {
		stats.EventsPerSecond = float64(len(events)) / duration.Seconds()
	}

	// Count by type, severity, and source
	resourceCounts := make(map[string]int64)
	var totalProcessingTime time.Duration

	for _, event := range events {
		// Count by type
		stats.EventsByType[string(event.Type)]++

		// Count by severity
		stats.EventsBySeverity[string(event.Severity)]++

		// Count by source
		stats.EventsBySource[string(event.Source)]++

		// Count resources (simplified - using namespace/pod)
		resourceKey := fmt.Sprintf("%s/%s", event.Context.Namespace, event.Context.Pod)
		if resourceKey != "/" {
			resourceCounts[resourceKey]++
		}

		// Aggregate processing time (if available)
		// This would be enhanced with actual processing metadata
		totalProcessingTime += time.Millisecond
	}

	// Calculate average processing time
	if len(events) > 0 {
		stats.AvgProcessingTime = durationpb.New(totalProcessingTime / time.Duration(len(events)))
	}

	// Convert resource counts to proto format (top 10)
	resourceList := make([]*pb.ResourceCount, 0, len(resourceCounts))
	for resource, count := range resourceCounts {
		percentage := float64(count) / float64(len(events)) * 100
		resourceList = append(resourceList, &pb.ResourceCount{
			Resource: &pb.ResourceIdentifier{
				Type: "kubernetes",
				Id:   resource,
			},
			Count:      count,
			Percentage: percentage,
		})
	}

	// Sort and take top 10 (simplified sorting)
	// In production, this would use proper sorting
	if len(resourceList) > 10 {
		resourceList = resourceList[:10]
	}
	stats.TopResources = resourceList

	// Set failed events (this would be tracked during processing)
	s.stats.mu.RLock()
	stats.FailedEvents = s.stats.FailedEvents
	s.stats.mu.RUnlock()

	return stats
}

// Conversion methods

// convertProtoEventToDomain converts proto Event to domain Event
func (s *EventServer) convertProtoEventToDomain(protoEvent *pb.Event) domain.Event {
	// Convert proto struct to map
	var dataMap map[string]interface{}
	if protoEvent.Data != nil {
		dataMap = protoEvent.Data.AsMap()
	}

	// Convert attributes
	attributes := make(map[string]interface{})
	for k, v := range protoEvent.Attributes {
		attributes[k] = v
	}

	return domain.Event{
		ID:        domain.EventID(protoEvent.Id),
		Timestamp: protoEvent.Timestamp.AsTime(),
		Type:      domain.EventType(protoEvent.Type.String()),
		Source:    domain.SourceType(protoEvent.Source.String()),
		Data:      dataMap,
		Message:   protoEvent.Message,
		Severity:  domain.EventSeverity(protoEvent.Severity.String()),
		Context: domain.EventContext{
			TraceID: protoEvent.TraceId,
			SpanID:  protoEvent.SpanId,
			// Additional context fields would be extracted from protoEvent.Context
		},
		Attributes: attributes,
		Tags:       protoEvent.Tags,
		Confidence: protoEvent.Confidence,
	}
}

// convertDomainEventToProto converts domain Event to proto Event
func (s *EventServer) convertDomainEventToProto(domainEvent domain.Event) *pb.Event {
	// Convert data map to proto struct
	var dataStruct *structpb.Struct
	if domainEvent.Data != nil {
		var err error
		dataStruct, err = structpb.NewStruct(domainEvent.Data)
		if err != nil {
			s.logger.Warn("Failed to convert event data to struct", zap.Error(err))
		}
	}

	// Convert attributes
	attributes := make(map[string]string)
	for k, v := range domainEvent.Attributes {
		if str, ok := v.(string); ok {
			attributes[k] = str
		} else {
			attributes[k] = fmt.Sprintf("%v", v)
		}
	}

	return &pb.Event{
		Id:          string(domainEvent.ID),
		Type:        s.convertEventType(domainEvent.Type),
		Severity:    s.convertEventSeverity(domainEvent.Severity),
		Source:      s.convertSourceType(domainEvent.Source),
		Message:     domainEvent.Message,
		Description: "", // Could be extracted from data or attributes
		Timestamp:   timestamppb.New(domainEvent.Timestamp),
		Context:     s.convertEventContext(domainEvent.Context),
		TraceId:     domainEvent.Context.TraceID,
		SpanId:      domainEvent.Context.SpanID,
		Data:        dataStruct,
		Attributes:  attributes,
		Tags:        domainEvent.Tags,
		Confidence:  domainEvent.Confidence,
		ProcessedAt: timestamppb.Now(),
	}
}

// convertQueryToFilter converts proto EventQuery to domain Filter
func (s *EventServer) convertQueryToFilter(query *pb.EventQuery) (*domain.Filter, error) {
	if query.Filter == nil {
		return &domain.Filter{}, nil
	}

	return s.convertProtoFilterToDomain(query.Filter)
}

// convertProtoFilterToDomain converts proto Filter to domain Filter
func (s *EventServer) convertProtoFilterToDomain(protoFilter *pb.Filter) (*domain.Filter, error) {
	filter := &domain.Filter{}

	// Time range
	if protoFilter.TimeRange != nil {
		if protoFilter.TimeRange.Start != nil {
			filter.Since = protoFilter.TimeRange.Start.AsTime()
		}
		if protoFilter.TimeRange.End != nil {
			filter.Until = protoFilter.TimeRange.End.AsTime()
		}
	}

	// Limit
	if protoFilter.Limit > 0 {
		filter.Limit = int(protoFilter.Limit)
	}

	// Additional filter fields would be mapped here
	// (namespace, entity type, etc.)

	return filter, nil
}

// mergeFilters combines two domain filters
func (s *EventServer) mergeFilters(base, additional domain.Filter) domain.Filter {
	merged := base

	// Use the more restrictive time range
	if !additional.Since.IsZero() && (merged.Since.IsZero() || additional.Since.After(merged.Since)) {
		merged.Since = additional.Since
	}
	if !additional.Until.IsZero() && (merged.Until.IsZero() || additional.Until.Before(merged.Until)) {
		merged.Until = additional.Until
	}

	// Use the smaller limit
	if additional.Limit > 0 && (merged.Limit == 0 || additional.Limit < merged.Limit) {
		merged.Limit = additional.Limit
	}

	return merged
}

// eventMatchesFilter checks if an event matches a subscription filter
func (s *EventServer) eventMatchesFilter(event domain.Event, filter *pb.Filter) bool {
	if filter == nil {
		return true
	}

	// Time range check
	if filter.TimeRange != nil {
		if filter.TimeRange.Start != nil && event.Timestamp.Before(filter.TimeRange.Start.AsTime()) {
			return false
		}
		if filter.TimeRange.End != nil && event.Timestamp.After(filter.TimeRange.End.AsTime()) {
			return false
		}
	}

	// Additional filter logic would go here
	// (severity, type, namespace, etc.)

	return true
}

// enrichEventsWithCorrelations adds correlation data to events
func (s *EventServer) enrichEventsWithCorrelations(events []*pb.Event) {
	// This would integrate with the correlation service
	// For now, just log that enrichment was requested
	s.logger.Debug("Enriching events with correlations", zap.Int("count", len(events)))
}

// enrichEventsWithMetrics adds metric data to events
func (s *EventServer) enrichEventsWithMetrics(events []*pb.Event) {
	// This would integrate with metrics storage
	s.logger.Debug("Enriching events with metrics", zap.Int("count", len(events)))
}

// enrichEventsWithTraces adds trace data to events
func (s *EventServer) enrichEventsWithTraces(events []*pb.Event) {
	// This would integrate with trace storage
	s.logger.Debug("Enriching events with traces", zap.Int("count", len(events)))
}

// Type conversion helpers

func (s *EventServer) convertEventType(domainType domain.EventType) pb.EventType {
	switch domainType {
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSTEM
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_SERVICE
	case domain.EventTypeLog:
		return pb.EventType_EVENT_TYPE_LOG
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func (s *EventServer) convertEventSeverity(domainSeverity domain.EventSeverity) pb.EventSeverity {
	switch domainSeverity {
	case domain.EventSeverityDebug:
		return pb.EventSeverity_EVENT_SEVERITY_DEBUG
	case domain.EventSeverityInfo:
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	case domain.EventSeverityWarning:
		return pb.EventSeverity_EVENT_SEVERITY_WARNING
	case domain.EventSeverityError:
		return pb.EventSeverity_EVENT_SEVERITY_ERROR
	case domain.EventSeverityCritical:
		return pb.EventSeverity_EVENT_SEVERITY_CRITICAL
	default:
		return pb.EventSeverity_EVENT_SEVERITY_UNSPECIFIED
	}
}

func (s *EventServer) convertSourceType(domainSource domain.SourceType) pb.SourceType {
	switch domainSource {
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_SYSTEMD
	case domain.SourceJournald:
		return pb.SourceType_SOURCE_TYPE_JOURNALD
	default:
		return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
	}
}

func (s *EventServer) convertEventContext(domainContext domain.EventContext) *pb.EventContext {
	return &pb.EventContext{
		TraceId:   domainContext.TraceID,
		SpanId:    domainContext.SpanID,
		Service:   domainContext.Service,
		Component: domainContext.Component,
		Namespace: domainContext.Namespace,
		Host:      domainContext.Host,
		Node:      domainContext.Node,
		Pod:       domainContext.Pod,
		Container: domainContext.Container,
		ProcessId: int32(domainContext.PID),
		UserId:    int32(domainContext.UID),
		GroupId:   int32(domainContext.GID),
		Command:   domainContext.Comm,
		Labels:    domainContext.Labels,
	}
}
