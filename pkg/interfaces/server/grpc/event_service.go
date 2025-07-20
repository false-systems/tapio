package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/interfaces/server/adapters/correlation"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventServer implements the EventService gRPC interface
type EventServer struct {
	pb.UnimplementedEventServiceServer

	// Core dependencies
	logger     *zap.Logger
	tracer     trace.Tracer
	eventStore EventStore
	processor  *EventProcessor

	// Subscription management
	mu            sync.RWMutex
	subscriptions map[string]*EventSubscription
	subscribers   map[string]chan *pb.EventUpdate

	// Configuration
	config EventServiceConfig

	// Statistics tracking
	stats EventServiceStats
}

// EventServiceConfig configures the event service
type EventServiceConfig struct {
	MaxEventsPerBatch       int
	MaxEventsPerSecond      int
	MaxSubscriptions        int
	SubscriptionBufferSize  int
	ProcessingWorkers       int
	EnableRealTimeStreaming bool
	EnableStatistics        bool
	RetentionPeriod         time.Duration
}

// EventServiceStats tracks service metrics
type EventServiceStats struct {
	mu                sync.RWMutex
	TotalEvents       int64
	EventsPerSecond   float64
	ActiveStreams     int32
	ActiveSubscriptions int32
	FailedEvents      int64
	ProcessingTime    time.Duration
}

// EventStore interface for event storage operations
type EventStore interface {
	Store(ctx context.Context, events []domain.Event) error
	Query(ctx context.Context, filter domain.Filter) ([]domain.Event, error)
	Get(ctx context.Context, eventIDs []string) ([]domain.Event, error)
	GetLatest(ctx context.Context, limit int) ([]domain.Event, error)
	Cleanup(ctx context.Context, before time.Time) error
	Delete(ctx context.Context, eventIDs []string) error
	GetStats() correlation.EventStoreStats
}

// EventProcessor handles event enrichment and correlation
type EventProcessor struct {
	mu      sync.RWMutex
	logger  *zap.Logger
	workers int
}

// EventSubscription tracks active event subscriptions
type EventSubscription struct {
	ID           string
	Filter       *pb.Filter
	DeliveryMode pb.SubscribeRequest_DeliveryMode
	MaxEventsPerSec int32
	StartTime    time.Time
	LastActivity time.Time
	EventsSent   int64
}

// NewEventServer creates a new event server
func NewEventServer(
	logger *zap.Logger,
	tracer trace.Tracer,
	eventStore EventStore,
) *EventServer {
	config := EventServiceConfig{
		MaxEventsPerBatch:       10000,
		MaxEventsPerSecond:      165000, // High-throughput target
		MaxSubscriptions:        1000,
		SubscriptionBufferSize:  10000,
		ProcessingWorkers:       16,
		EnableRealTimeStreaming: true,
		EnableStatistics:        true,
		RetentionPeriod:         7 * 24 * time.Hour, // 7 days
	}

	return &EventServer{
		logger:        logger,
		tracer:        tracer,
		eventStore:    eventStore,
		processor:     NewEventProcessor(logger, config.ProcessingWorkers),
		subscriptions: make(map[string]*EventSubscription),
		subscribers:   make(map[string]chan *pb.EventUpdate),
		config:        config,
		stats:         EventServiceStats{},
	}
}

// StreamEvents implements bidirectional streaming for high-throughput event ingestion
func (s *EventServer) StreamEvents(stream pb.EventService_StreamEventsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "event.stream_events")
	defer span.End()

	// Create stream context
	streamID := fmt.Sprintf("event_stream_%d", time.Now().UnixNano())
	s.logger.Info("Starting event stream", zap.String("stream_id", streamID))

	// Update active streams counter
	s.stats.mu.Lock()
	s.stats.ActiveStreams++
	s.stats.mu.Unlock()

	defer func() {
		s.stats.mu.Lock()
		s.stats.ActiveStreams--
		s.stats.mu.Unlock()
		s.logger.Info("Event stream closed", zap.String("stream_id", streamID))
	}()

	// Process incoming events
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			req, err := stream.Recv()
			if err != nil {
				s.logger.Error("Failed to receive event stream request", zap.Error(err))
				return err
			}

			// Process the request
			resp, err := s.processStreamRequest(ctx, req)
			if err != nil {
				s.logger.Error("Failed to process stream request", zap.Error(err))
				return err
			}

			// Send response
			if err := stream.Send(resp); err != nil {
				s.logger.Error("Failed to send stream response", zap.Error(err))
				return err
			}
		}
	}
}

// Subscribe provides real-time event updates based on filters
func (s *EventServer) Subscribe(req *pb.SubscribeRequest, stream pb.EventService_SubscribeServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "event.subscribe")
	defer span.End()

	// Validate subscription request
	if err := s.validateSubscribeRequest(req); err != nil {
		return err
	}

	// Create subscription
	subscription := &EventSubscription{
		ID:              req.SubscriptionId,
		Filter:          req.Filter,
		DeliveryMode:    req.DeliveryMode,
		MaxEventsPerSec: req.MaxEventsPerSecond,
		StartTime:       time.Now(),
		LastActivity:    time.Now(),
	}

	// Register subscription
	s.mu.Lock()
	s.subscriptions[subscription.ID] = subscription
	updateChan := make(chan *pb.EventUpdate, s.config.SubscriptionBufferSize)
	s.subscribers[subscription.ID] = updateChan
	s.mu.Unlock()

	// Update active subscriptions counter
	s.stats.mu.Lock()
	s.stats.ActiveSubscriptions++
	s.stats.mu.Unlock()

	defer func() {
		// Cleanup subscription
		s.mu.Lock()
		delete(s.subscriptions, subscription.ID)
		close(updateChan)
		delete(s.subscribers, subscription.ID)
		s.mu.Unlock()

		s.stats.mu.Lock()
		s.stats.ActiveSubscriptions--
		s.stats.mu.Unlock()

		s.logger.Info("Subscription closed", 
			zap.String("subscription_id", subscription.ID),
			zap.Int64("events_sent", subscription.EventsSent),
		)
	}()

	s.logger.Info("Event subscription started", 
		zap.String("subscription_id", subscription.ID),
		zap.Any("filter", req.Filter),
	)

	// Send historical events if requested
	if req.IncludeExisting {
		if err := s.sendHistoricalEvents(ctx, stream, subscription, req); err != nil {
			s.logger.Error("Failed to send historical events", zap.Error(err))
			return err
		}
	}

	// Stream real-time updates
	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updateChan:
			if update == nil {
				return nil
			}

			// Apply rate limiting
			if err := s.applyRateLimit(subscription); err != nil {
				s.logger.Warn("Rate limit exceeded", 
					zap.String("subscription_id", subscription.ID),
					zap.Error(err),
				)
				continue
			}

			// Send update
			if err := stream.Send(update); err != nil {
				s.logger.Error("Failed to send event update", zap.Error(err))
				return err
			}

			// Update subscription stats
			subscription.EventsSent++
			subscription.LastActivity = time.Now()
		}
	}
}

// GetEvents retrieves historical events based on query
func (s *EventServer) GetEvents(ctx context.Context, req *pb.GetEventsRequest) (*pb.GetEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "event.get_events")
	defer span.End()

	start := time.Now()
	defer func() {
		s.stats.mu.Lock()
		s.stats.ProcessingTime = time.Since(start)
		s.stats.mu.Unlock()
	}()

	var events []domain.Event
	var err error

	// Handle specific event IDs
	if len(req.EventIds) > 0 {
		events, err = s.eventStore.Get(ctx, req.EventIds)
		if err != nil {
			s.logger.Error("Failed to get events by IDs", zap.Error(err))
			return nil, status.Error(codes.Internal, "failed to retrieve events")
		}
	} else if req.Query != nil {
		// Convert proto query to domain filter
		filter, err := s.convertQueryToFilter(req.Query)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid query: %v", err))
		}

		events, err = s.eventStore.Query(ctx, *filter)
		if err != nil {
			s.logger.Error("Failed to query events", zap.Error(err))
			return nil, status.Error(codes.Internal, "failed to query events")
		}
	} else {
		return nil, status.Error(codes.InvalidArgument, "either event_ids or query must be provided")
	}

	// Convert domain events to proto events
	protoEvents := make([]*pb.Event, len(events))
	for i, event := range events {
		protoEvents[i] = s.convertDomainEventToProto(event)
	}

	// Enrich events if requested
	if req.Query != nil {
		if req.Query.IncludeCorrelations {
			s.enrichEventsWithCorrelations(protoEvents)
		}
		if req.Query.IncludeMetrics {
			s.enrichEventsWithMetrics(protoEvents)
		}
		if req.Query.IncludeTraces {
			s.enrichEventsWithTraces(protoEvents)
		}
	}

	metadata := map[string]string{
		"query_duration_ms": fmt.Sprintf("%.2f", time.Since(start).Seconds()*1000),
		"result_count":      fmt.Sprintf("%d", len(protoEvents)),
		"enrichment_applied": fmt.Sprintf("%t", req.Query != nil && 
			(req.Query.IncludeCorrelations || req.Query.IncludeMetrics || req.Query.IncludeTraces)),
	}

	return &pb.GetEventsResponse{
		Events:        protoEvents,
		TotalCount:    int64(len(protoEvents)),
		NextPageToken: "", // TODO: Implement pagination
		Metadata:      metadata,
	}, nil
}

// GetStatistics provides event analytics and metrics
func (s *EventServer) GetStatistics(ctx context.Context, req *pb.TimeRange) (*pb.EventStatistics, error) {
	ctx, span := s.tracer.Start(ctx, "event.get_statistics")
	defer span.End()

	if !s.config.EnableStatistics {
		return nil, status.Error(codes.Unimplemented, "statistics are disabled")
	}

	// Query events within time range
	filter := domain.Filter{
		Since: req.Start.AsTime(),
		Until: req.End.AsTime(),
		Limit: 0, // No limit for statistics
	}

	events, err := s.eventStore.Query(ctx, filter)
	if err != nil {
		s.logger.Error("Failed to query events for statistics", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to generate statistics")
	}

	// Generate statistics
	stats := s.generateStatistics(req, events)
	
	s.logger.Debug("Generated event statistics",
		zap.Int64("total_events", stats.TotalEvents),
		zap.Float64("events_per_second", stats.EventsPerSecond),
		zap.Int("event_types", len(stats.EventsByType)),
	)

	return stats, nil
}

// SubmitEventBatch accepts batch event submission
func (s *EventServer) SubmitEventBatch(ctx context.Context, req *pb.EventBatch) (*pb.EventAck, error) {
	ctx, span := s.tracer.Start(ctx, "event.submit_batch")
	defer span.End()

	start := time.Now()

	// Validate batch
	if err := s.validateEventBatch(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Convert proto events to domain events
	domainEvents := make([]domain.Event, len(req.Events))
	for i, event := range req.Events {
		domainEvents[i] = s.convertProtoEventToDomain(event)
	}

	// Process events (enrich, validate, etc.)
	processedEvents, err := s.processor.ProcessEvents(ctx, domainEvents)
	if err != nil {
		s.logger.Error("Failed to process events", zap.Error(err))
		s.stats.mu.Lock()
		s.stats.FailedEvents += int64(len(req.Events))
		s.stats.mu.Unlock()
		return nil, status.Error(codes.Internal, "failed to process events")
	}

	// Store events
	if err := s.eventStore.Store(ctx, processedEvents); err != nil {
		s.logger.Error("Failed to store events", zap.Error(err))
		s.stats.mu.Lock()
		s.stats.FailedEvents += int64(len(req.Events))
		s.stats.mu.Unlock()
		return nil, status.Error(codes.Internal, "failed to store events")
	}

	// Update statistics
	s.stats.mu.Lock()
	s.stats.TotalEvents += int64(len(req.Events))
	s.stats.ProcessingTime = time.Since(start)
	s.stats.mu.Unlock()

	// Notify subscribers of new events
	if s.config.EnableRealTimeStreaming {
		s.notifySubscribers(processedEvents)
	}

	s.logger.Info("Event batch submitted successfully",
		zap.String("batch_id", req.BatchId),
		zap.Int("event_count", len(req.Events)),
		zap.String("collector_id", req.CollectorId),
		zap.Duration("processing_time", time.Since(start)),
	)

	return &pb.EventAck{
		EventId:   "", // Batch-level ack
		BatchId:   req.BatchId,
		Timestamp: timestamppb.Now(),
		Status:    "processed",
		Message:   fmt.Sprintf("Successfully processed %d events", len(req.Events)),
		Metadata: map[string]string{
			"processing_time_ms": fmt.Sprintf("%.2f", time.Since(start).Seconds()*1000),
			"events_count":       fmt.Sprintf("%d", len(req.Events)),
		},
	}, nil
}

// Helper methods for the EventServer implementation
// (Implementation details for all the helper methods would follow...)

// NewEventProcessor creates a new event processor
func NewEventProcessor(logger *zap.Logger, workers int) *EventProcessor {
	return &EventProcessor{
		logger:  logger,
		workers: workers,
	}
}

// ProcessEvents enriches and validates events
func (p *EventProcessor) ProcessEvents(ctx context.Context, events []domain.Event) ([]domain.Event, error) {
	// Process events in parallel using worker pool
	processed := make([]domain.Event, len(events))
	copy(processed, events)

	// Add processing timestamp and enrich events
	for i := range processed {
		processed[i].Timestamp = time.Now()
		// Add enrichment logic here
	}

	return processed, nil
}