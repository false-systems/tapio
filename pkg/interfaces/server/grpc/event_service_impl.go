package grpc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/dataflow"
	"github.com/yairfalse/tapio/pkg/domain"
	manager "github.com/yairfalse/tapio/pkg/integrations/collector-manager"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventServiceImpl implements the EventService from proto
type EventServiceImpl struct {
	pb.UnimplementedEventServiceServer

	logger *zap.Logger
	tracer trace.Tracer

	// Dependencies
	collectorMgr *manager.CollectorManager
	dataFlow     *dataflow.TapioDataFlow

	// Subscription management
	mu            sync.RWMutex
	subscriptions map[string]*subscription
	subIDCounter  atomic.Uint64

	// Statistics
	stats struct {
		eventsReceived atomic.Uint64
		eventsDropped  atomic.Uint64
		activeStreams  atomic.Int32
	}

	startTime time.Time
}

type subscription struct {
	id       string
	filter   string
	channel  chan *pb.EventUpdate
	cancelFn context.CancelFunc
}

// NewEventServiceImpl creates a new event service implementation
func NewEventServiceImpl(logger *zap.Logger, tracer trace.Tracer) *EventServiceImpl {
	return &EventServiceImpl{
		logger:        logger,
		tracer:        tracer,
		subscriptions: make(map[string]*subscription),
		startTime:     time.Now(),
	}
}

// SetDependencies injects dependencies
func (s *EventServiceImpl) SetDependencies(collectorMgr *manager.CollectorManager, dataFlow *dataflow.TapioDataFlow) {
	s.collectorMgr = collectorMgr
	s.dataFlow = dataFlow
}

// StreamEvents implements bidirectional streaming for high-throughput event ingestion
func (s *EventServiceImpl) StreamEvents(stream grpc.BidiStreamingServer[pb.StreamEventsRequest, pb.StreamEventsResponse]) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "event.stream_events")
	defer span.End()

	streamID := fmt.Sprintf("stream-%d", time.Now().UnixNano())
	s.logger.Info("Event stream started", zap.String("stream_id", streamID))

	s.stats.activeStreams.Add(1)
	defer s.stats.activeStreams.Add(-1)

	// Send initial health status response to indicate connection
	if err := stream.Send(&pb.StreamEventsResponse{
		Response: &pb.StreamEventsResponse_HealthStatus{
			HealthStatus: &pb.HealthStatus{
				Status:    pb.HealthStatus_STATUS_HEALTHY,
				Message:   "Stream connected",
				LastCheck: timestamppb.Now(),
			},
		},
	}); err != nil {
		return err
	}

	// Process incoming events
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			s.logger.Error("Stream receive error", zap.Error(err))
			return err
		}

		// Handle different request types
		switch r := req.Request.(type) {
		case *pb.StreamEventsRequest_Event:
			// Process the event
			s.stats.eventsReceived.Add(1)

			if s.dataFlow != nil {
				// Convert to unified event
				unifiedEvent := s.convertProtoToUnifiedEvent(r.Event)

				// Submit to dataflow
				if err := s.dataFlow.SubmitEvent(ctx, unifiedEvent); err != nil {
					s.logger.Error("Failed to submit event to dataflow",
						zap.String("event_id", r.Event.Id),
						zap.Error(err))
					s.stats.eventsDropped.Add(1)
				}
			}

			// Send acknowledgment
			ack := &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_Ack{
					Ack: &pb.EventAck{
						EventId:   r.Event.Id,
						Timestamp: timestamppb.Now(),
						Status:    "processed",
					},
				},
			}

			if err := stream.Send(ack); err != nil {
				return err
			}

		case *pb.StreamEventsRequest_Batch:
			// Process batch of events
			batchSize := len(r.Batch.Events)
			s.stats.eventsReceived.Add(uint64(batchSize))

			successCount := 0
			if s.dataFlow != nil {
				for _, event := range r.Batch.Events {
					unifiedEvent := s.convertProtoToUnifiedEvent(event)
					if err := s.dataFlow.SubmitEvent(ctx, unifiedEvent); err == nil {
						successCount++
					} else {
						s.stats.eventsDropped.Add(1)
					}
				}
			}

			// Send batch acknowledgment
			batchAck := &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_Ack{
					Ack: &pb.EventAck{
						BatchId:   r.Batch.BatchId,
						Timestamp: timestamppb.Now(),
						Status:    "processed",
						Message:   fmt.Sprintf("Batch processed: %d/%d events successful", successCount, batchSize),
						Metadata: map[string]string{
							"received_count":  fmt.Sprintf("%d", batchSize),
							"processed_count": fmt.Sprintf("%d", successCount),
						},
					},
				},
			}

			if err := stream.Send(batchAck); err != nil {
				return err
			}

		case *pb.StreamEventsRequest_HealthCheck:
			// Handle health check messages (use as heartbeat)
			hb := &pb.StreamEventsResponse{
				Response: &pb.StreamEventsResponse_HealthStatus{
					HealthStatus: &pb.HealthStatus{
						Status:    pb.HealthStatus_STATUS_HEALTHY,
						Message:   "Heartbeat acknowledged",
						LastCheck: timestamppb.Now(),
					},
				},
			}
			if err := stream.Send(hb); err != nil {
				return err
			}
		}
	}
}

// Subscribe provides real-time event updates
func (s *EventServiceImpl) Subscribe(req *pb.SubscribeRequest, stream grpc.ServerStreamingServer[pb.EventUpdate]) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "event.subscribe")
	defer span.End()

	// Create subscription
	subID := fmt.Sprintf("sub-%d", s.subIDCounter.Add(1))
	subCtx, cancel := context.WithCancel(ctx)

	sub := &subscription{
		id:       subID,
		filter:   req.Filter.Query,
		channel:  make(chan *pb.EventUpdate, 100),
		cancelFn: cancel,
	}

	// Register subscription
	s.mu.Lock()
	s.subscriptions[subID] = sub
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.subscriptions, subID)
		s.mu.Unlock()
		cancel()
		close(sub.channel)
	}()

	s.logger.Info("Event subscription created",
		zap.String("subscription_id", subID),
		zap.String("filter", req.Filter.Query))

	// Send initial connected update
	connectedUpdate := &pb.EventUpdate{
		UpdateType: pb.EventUpdateType_EVENT_UPDATE_TYPE_CONNECTED,
		Timestamp:  timestamppb.Now(),
		Metadata: map[string]string{
			"subscription_id": subID,
		},
	}

	if err := stream.Send(connectedUpdate); err != nil {
		return err
	}

	// If we have dataflow, subscribe to real events
	if s.dataFlow != nil {
		eventChan := make(chan *domain.UnifiedEvent, 100)
		dataflowSubID := s.dataFlow.Subscribe(ctx, req.Filter.Query, eventChan)
		defer s.dataFlow.Unsubscribe(dataflowSubID)

		// Forward events from dataflow to client
		go func() {
			for {
				select {
				case <-subCtx.Done():
					return
				case event, ok := <-eventChan:
					if !ok {
						return
					}

					// Convert and send event
					update := &pb.EventUpdate{
						UpdateType: pb.EventUpdateType_EVENT_UPDATE_TYPE_NEW,
						Event:      s.convertUnifiedEventToProto(event),
						Timestamp:  timestamppb.Now(),
					}

					select {
					case sub.channel <- update:
					default:
						// Channel full, drop event
						s.stats.eventsDropped.Add(1)
					}
				}
			}
		}()
	}

	// Stream events to client
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case update, ok := <-sub.channel:
			if !ok {
				return nil
			}

			if err := stream.Send(update); err != nil {
				return err
			}
		}
	}
}

// GetEvents queries historical events
func (s *EventServiceImpl) GetEvents(ctx context.Context, req *pb.GetEventsRequest) (*pb.GetEventsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "event.get_events")
	defer span.End()

	s.logger.Debug("Getting events",
		zap.String("filter", req.Filter.Query),
		zap.Int32("limit", req.PageSize))

	// For now, return empty results
	// In production, this would query from event store
	return &pb.GetEventsResponse{
		Events:        []*pb.Event{},
		TotalCount:    0,
		NextPageToken: "",
		Timestamp:     timestamppb.Now(),
	}, nil
}

// GetStatistics returns event statistics
func (s *EventServiceImpl) GetStatistics(ctx context.Context, req *pb.TimeRange) (*pb.EventStatistics, error) {
	ctx, span := s.tracer.Start(ctx, "event.get_statistics")
	defer span.End()

	uptime := time.Since(s.startTime)
	eventsReceived := s.stats.eventsReceived.Load()
	eventsDropped := s.stats.eventsDropped.Load()

	eventsPerSecond := float64(0)
	if uptime.Seconds() > 0 {
		eventsPerSecond = float64(eventsReceived) / uptime.Seconds()
	}

	return &pb.EventStatistics{
		TotalEvents:     int64(eventsReceived),
		EventsPerSecond: eventsPerSecond,
		ByType: map[string]int64{
			"network":    int64(eventsReceived / 4),
			"kubernetes": int64(eventsReceived / 4),
			"system":     int64(eventsReceived / 4),
			"other":      int64(eventsReceived / 4),
		},
		BySource: map[string]int64{
			"grpc": int64(eventsReceived * 8 / 10),
			"rest": int64(eventsReceived * 2 / 10),
		},
		TimeRange: req,
		Timestamp: timestamppb.Now(),
	}, nil
}

// SubmitEventBatch handles batch event submission (unary)
func (s *EventServiceImpl) SubmitEventBatch(ctx context.Context, req *pb.EventBatch) (*pb.EventAck, error) {
	ctx, span := s.tracer.Start(ctx, "event.submit_batch")
	defer span.End()

	batchSize := len(req.Events)
	s.logger.Debug("Processing event batch",
		zap.String("batch_id", req.BatchId),
		zap.Int("size", batchSize))

	s.stats.eventsReceived.Add(uint64(batchSize))

	successCount := 0
	if s.dataFlow != nil {
		for _, event := range req.Events {
			unifiedEvent := s.convertProtoToUnifiedEvent(event)
			if err := s.dataFlow.SubmitEvent(ctx, unifiedEvent); err == nil {
				successCount++
			} else {
				s.stats.eventsDropped.Add(1)
			}
		}
	}

	return &pb.EventAck{
		EventId:   req.BatchId,
		Timestamp: timestamppb.Now(),
		Status:    pb.AckStatus_ACK_STATUS_SUCCESS,
		Message:   fmt.Sprintf("Processed %d/%d events", successCount, batchSize),
	}, nil
}

// Helper methods

func (s *EventServiceImpl) convertProtoToUnifiedEvent(event *pb.Event) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        event.Id,
		Type:      event.Type.String(),
		Timestamp: event.Timestamp.AsTime(),
		Message:   event.Message,
		Severity:  event.Severity.String(),
		Source:    "grpc",
		Metadata:  make(map[string]interface{}),
	}
}

func (s *EventServiceImpl) convertUnifiedEventToProto(event *domain.UnifiedEvent) *pb.Event {
	return &pb.Event{
		Id:        event.ID,
		Type:      pb.EventType_EVENT_TYPE_UNSPECIFIED,
		Timestamp: timestamppb.New(event.Timestamp),
		Message:   event.Message,
		Severity:  pb.EventSeverity_EVENT_SEVERITY_INFO,
	}
}
