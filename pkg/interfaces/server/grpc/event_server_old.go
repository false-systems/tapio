package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/dataflow"
	"github.com/yairfalse/tapio/pkg/domain"
	manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventServer implements the event service with real data flow integration
type EventServer struct {
	pb.UnimplementedEventServiceServer

	logger *zap.Logger
	tracer trace.Tracer

	// Core dependencies (following 5-level architecture)
	collectorMgr *manager.CollectorManager // L3: Integration
	dataFlow     *dataflow.TapioDataFlow   // L2: Intelligence

	// Event storage (would be injected in production)
	eventStore EventStore
	processor  EventProcessorInterface

	// Streaming subscriptions
	mu            sync.RWMutex
	subscriptions map[string]*EventSubscription
	subscribers   map[string]chan *pb.EventUpdate

	// Configuration
	config EventServerConfig

	// Statistics
	stats struct {
		mu           sync.RWMutex
		TotalEvents  int64
		FailedEvents int64
		startTime    time.Time
		requestCount uint64
	}
}

// EventServerConfig holds configuration for the event server
type EventServerConfig struct {
	EnableRealTimeStreaming bool
	MaxSubscriptions        int
	MaxEventsPerBatch       int
	BufferSize              int
}

// EventStore interface for event storage operations
type EventStore interface {
	Store(ctx context.Context, events []domain.Event) error
	Query(ctx context.Context, filter domain.Filter) ([]domain.Event, error)
	GetByID(ctx context.Context, id string) (*domain.Event, error)
}

// EventProcessorInterface interface for event processing
type EventProcessorInterface interface {
	ProcessEvents(ctx context.Context, events []domain.Event) ([]domain.Event, error)
}

// EventSubscription represents an active event subscription
type EventSubscription struct {
	ID              string
	Filter          *pb.Filter
	MaxEventsPerSec int
	StartTime       time.Time
	LastActivity    time.Time
	EventsSent      int64
}

// NewEventServer creates a new event service implementation
func NewEventServer(logger *zap.Logger, tracer trace.Tracer) *EventServer {
	return &EventServer{
		logger:        logger,
		tracer:        tracer,
		subscriptions: make(map[string]*EventSubscription),
		subscribers:   make(map[string]chan *pb.EventUpdate),
		config: EventServerConfig{
			EnableRealTimeStreaming: true,
			MaxSubscriptions:        1000,
			MaxEventsPerBatch:       10000,
			BufferSize:              10000,
		},
		stats: struct {
			mu           sync.RWMutex
			TotalEvents  int64
			FailedEvents int64
			startTime    time.Time
			requestCount uint64
		}{
			startTime: time.Now(),
		},
	}
}

// SetDependencies injects required dependencies following the architecture
func (s *EventServer) SetDependencies(collectorMgr *manager.CollectorManager, dataFlow *dataflow.TapioDataFlow, eventStore EventStore, processor EventProcessor) {
	s.collectorMgr = collectorMgr
	s.dataFlow = dataFlow
	s.eventStore = eventStore
	s.processor = processor
}

// NewEventServerWithRealStore creates an event server with real storage integration
func NewEventServerWithRealStore(logger *zap.Logger, tracer trace.Tracer) *EventServer {
	server := NewEventServer(logger, tracer)

	// Use in-memory implementations for now - would be replaced with real storage
	server.eventStore = &InMemoryEventStore{
		events: make(map[string]domain.Event),
		mu:     sync.RWMutex{},
	}

	server.processor = &DefaultEventProcessor{
		logger: logger,
	}

	return server
}

// SubmitEvent accepts and processes a single event
func (s *EventServer) SubmitEvent(ctx context.Context, req *pb.SubmitEventRequest) (*pb.SubmitEventResponse, error) {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(ctx, "event.submit_event")
	defer span.End()

	if req.Event == nil {
		return nil, status.Error(codes.InvalidArgument, "event is required")
	}

	s.logger.Debug("Submitting event", zap.String("event_id", req.Event.Id))

	// Convert proto event to domain event
	domainEvent := s.convertProtoEventToDomain(req.Event)

	// Process through intelligence layer if available
	if s.processor != nil {
		processedEvents, err := s.processor.ProcessEvents(ctx, []domain.Event{domainEvent})
		if err != nil {
			s.incrementFailedEvents()
			return nil, status.Errorf(codes.Internal, "failed to process event: %v", err)
		}

		if len(processedEvents) > 0 {
			domainEvent = processedEvents[0]
		}
	}

	// Store event if storage is available
	if s.eventStore != nil {
		if err := s.eventStore.Store(ctx, []domain.Event{domainEvent}); err != nil {
			s.incrementFailedEvents()
			return nil, status.Errorf(codes.Internal, "failed to store event: %v", err)
		}
	}

	// Notify real-time subscribers
	if s.config.EnableRealTimeStreaming {
		s.notifySubscribers([]domain.Event{domainEvent})
	}

	s.incrementTotalEvents()

	return &pb.SubmitEventResponse{
		EventId:   req.Event.Id,
		Status:    "accepted",
		Timestamp: timestamppb.Now(),
		Message:   "Event processed successfully",
	}, nil
}

// QueryEvents queries events based on filter criteria
func (s *EventServer) QueryEvents(ctx context.Context, req *pb.QueryEventsRequest) (*pb.QueryEventsResponse, error) {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(ctx, "event.query_events")
	defer span.End()

	s.logger.Debug("Querying events")

	// Convert query to domain filter
	filter, err := s.convertQueryToFilter(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid query: %v", err)
	}

	// Query events from storage
	var events []domain.Event
	if s.eventStore != nil {
		events, err = s.eventStore.Query(ctx, *filter)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "query failed: %v", err)
		}
	}

	// Convert to proto events
	protoEvents := make([]*pb.Event, len(events))
	for i, event := range events {
		protoEvents[i] = s.convertDomainEventToProto(event)
	}

	// Enrich with correlation data if requested
	if req.IncludeCorrelations {
		s.enrichEventsWithCorrelations(protoEvents)
	}

	// Generate statistics if requested
	var statistics *pb.EventStatistics
	if req.IncludeStatistics && req.TimeRange != nil {
		statistics = s.generateStatistics(req.TimeRange, events)
	}

	return &pb.QueryEventsResponse{
		Events:     protoEvents,
		TotalCount: int64(len(events)),
		Statistics: statistics,
		QueryTime:  timestamppb.Now(),
	}, nil
}

// StreamEvents provides real-time event streaming
func (s *EventServer) StreamEvents(stream pb.EventService_StreamEventsServer) error {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(stream.Context(), "event.stream_events")
	defer span.End()

	s.logger.Debug("Starting event stream")

	for {
		req, err := stream.Recv()
		if err != nil {
			s.logger.Debug("Event stream ended", zap.Error(err))
			return err
		}

		// Process the stream request
		response, err := s.processStreamRequest(ctx, req)
		if err != nil {
			return err
		}

		// Send response
		if err := stream.Send(response); err != nil {
			return err
		}
	}
}

// Subscribe allows clients to subscribe to real-time event updates
func (s *EventServer) Subscribe(req *pb.SubscribeRequest, stream pb.EventService_SubscribeServer) error {
	s.incrementRequestCount()

	ctx, span := s.tracer.Start(stream.Context(), "event.subscribe")
	defer span.End()

	s.logger.Debug("Creating event subscription", zap.String("subscription_id", req.SubscriptionId))

	// Validate subscription request
	if err := s.validateSubscribeRequest(req); err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid subscription: %v", err)
	}

	// Create subscription
	subscription := &EventSubscription{
		ID:              req.SubscriptionId,
		Filter:          req.Filter,
		MaxEventsPerSec: int(req.MaxEventsPerSec),
		StartTime:       time.Now(),
		LastActivity:    time.Now(),
	}

	// Create update channel
	updateChan := make(chan *pb.EventUpdate, s.config.BufferSize)

	// Register subscription
	s.mu.Lock()
	s.subscriptions[req.SubscriptionId] = subscription
	s.subscribers[req.SubscriptionId] = updateChan
	s.mu.Unlock()

	// Send historical events if requested
	if req.Lookback != nil && req.Lookback.AsDuration() > 0 {
		if err := s.sendHistoricalEvents(ctx, stream, subscription, req); err != nil {
			s.logger.Warn("Failed to send historical events", zap.Error(err))
		}
	}

	// Stream updates to client
	defer func() {
		s.mu.Lock()
		delete(s.subscriptions, req.SubscriptionId)
		delete(s.subscribers, req.SubscriptionId)
		s.mu.Unlock()
		close(updateChan)
	}()

	for {
		select {
		case update := <-updateChan:
			// Apply rate limiting
			if err := s.applyRateLimit(subscription); err != nil {
				s.logger.Warn("Rate limit exceeded", zap.String("subscription_id", req.SubscriptionId))
				continue
			}

			if err := stream.Send(update); err != nil {
				return err
			}

			subscription.EventsSent++
			subscription.LastActivity = time.Now()

		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// GetServiceStats returns statistics for the event service
func (s *EventServer) GetServiceStats() map[string]interface{} {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	s.mu.RLock()
	activeSubscriptions := len(s.subscriptions)
	s.mu.RUnlock()

	return map[string]interface{}{
		"start_time":           s.stats.startTime,
		"uptime_seconds":       time.Since(s.stats.startTime).Seconds(),
		"request_count":        s.stats.requestCount,
		"total_events":         s.stats.TotalEvents,
		"failed_events":        s.stats.FailedEvents,
		"active_subscriptions": activeSubscriptions,
		"service_type":         "event_streaming",
	}
}

// HealthCheck checks the health of the event service
func (s *EventServer) HealthCheck() error {
	// Check if core dependencies are available
	if s.eventStore == nil {
		return fmt.Errorf("event store not initialized")
	}

	if s.processor == nil {
		return fmt.Errorf("event processor not initialized")
	}

	return nil
}

// ConfigureEventIngestion configures the event ingestion settings
func (s *EventServer) ConfigureEventIngestion(config EventIngestionConfig) {
	s.logger.Info("Configuring event ingestion",
		zap.Bool("collector_ingestion", config.EnableCollectorIngestion),
		zap.Bool("ebpf_ingestion", config.EnableeBPFIngestion),
		zap.Bool("k8s_ingestion", config.EnableK8sIngestion),
		zap.Bool("otel_ingestion", config.EnableOTELIngestion),
	)

	// Configure ingestion settings
	s.config.EnableRealTimeStreaming = config.EnableRealTimeStreaming
	s.config.MaxEventsPerBatch = config.MaxEventsPerBatch
}

// EventIngestionConfig holds event ingestion configuration
type EventIngestionConfig struct {
	EnableCollectorIngestion bool
	EnableeBPFIngestion      bool
	EnableK8sIngestion       bool
	EnableOTELIngestion      bool
	EnableRealTimeStreaming  bool
	MaxEventsPerSecond       int
	MaxEventsPerBatch        int
	EnableContextEnrichment  bool
	EnableAIEnrichment       bool
}

// Helper methods

func (s *EventServer) incrementRequestCount() {
	s.stats.mu.Lock()
	s.stats.requestCount++
	s.stats.mu.Unlock()
}

func (s *EventServer) incrementTotalEvents() {
	s.stats.mu.Lock()
	s.stats.TotalEvents++
	s.stats.mu.Unlock()
}

func (s *EventServer) incrementFailedEvents() {
	s.stats.mu.Lock()
	s.stats.FailedEvents++
	s.stats.mu.Unlock()
}

// InMemoryEventStore provides a simple in-memory event store for development
type InMemoryEventStore struct {
	events map[string]domain.Event
	mu     sync.RWMutex
}

func (s *InMemoryEventStore) Store(ctx context.Context, events []domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, event := range events {
		s.events[string(event.ID)] = event
	}

	return nil
}

func (s *InMemoryEventStore) Query(ctx context.Context, filter domain.Filter) ([]domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []domain.Event

	for _, event := range s.events {
		// Simple filtering by time range
		if !filter.Since.IsZero() && event.Timestamp.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && event.Timestamp.After(filter.Until) {
			continue
		}

		results = append(results, event)

		// Apply limit
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}

	return results, nil
}

func (s *InMemoryEventStore) GetByID(ctx context.Context, id string) (*domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if event, exists := s.events[id]; exists {
		return &event, nil
	}

	return nil, fmt.Errorf("event not found: %s", id)
}

// DefaultEventProcessor provides basic event processing
type DefaultEventProcessor struct {
	logger *zap.Logger
}

func (p *DefaultEventProcessor) ProcessEvents(ctx context.Context, events []domain.Event) ([]domain.Event, error) {
	// Basic processing - add timestamp if missing, validate structure
	processedEvents := make([]domain.Event, len(events))

	for i, event := range events {
		processedEvent := event

		// Ensure timestamp is set
		if processedEvent.Timestamp.IsZero() {
			processedEvent.Timestamp = time.Now()
		}

		// Add processing metadata
		if processedEvent.Context.Metadata == nil {
			processedEvent.Context.Metadata = make(map[string]interface{})
		}
		processedEvent.Context.Metadata["processed_at"] = time.Now()
		processedEvent.Context.Metadata["processor"] = "default"

		processedEvents[i] = processedEvent
	}

	return processedEvents, nil
}
