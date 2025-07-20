//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	pb "github.com/yairfalse/tapio/proto/tapio/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// EnhancedServer implements the Tapio gRPC server with OTEL support
type EnhancedServer struct {
	pb.UnimplementedTapioServiceServer

	// Configuration
	config *ServerConfig

	// Event processing
	eventBuffer     []domain.Event
	eventBufferLock sync.RWMutex
	correlationMgr  *correlation.CollectionManager

	// OTEL tracing
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator

	// Statistics
	stats struct {
		eventsReceived    int64
		correlationsFound int64
		tracesProcessed   int64
		activeStreams     int32
	}
	statsLock sync.RWMutex

	// Subscribers for real-time updates
	subscribers     map[string]chan *pb.Event
	subscribersLock sync.RWMutex
}

// NewEnhancedServer creates a new enhanced server with OTEL support
func NewEnhancedServer(config *ServerConfig) *EnhancedServer {
	return &EnhancedServer{
		config:         config,
		eventBuffer:    make([]domain.Event, 0, 10000),
		correlationMgr: correlation.NewCollectionManager(correlation.DefaultConfig()),
		tracer:         otel.Tracer("tapio.server"),
		propagator:     otel.GetTextMapPropagator(),
		subscribers:    make(map[string]chan *pb.Event),
	}
}

// StreamEvents implements the bidirectional streaming RPC for events
func (s *EnhancedServer) StreamEvents(stream pb.TapioService_StreamEventsServer) error {
	// Extract trace context from metadata
	ctx := stream.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		ctx = s.propagator.Extract(ctx, metadataCarrier(md))
	}

	// Start span for this stream
	ctx, span := s.tracer.Start(ctx, "server.stream_events",
		trace.WithAttributes(
			attribute.String("stream.type", "bidirectional"),
			attribute.String("service.name", "tapio-server"),
		),
	)
	defer span.End()

	// Track active stream
	s.incrementActiveStreams()
	defer s.decrementActiveStreams()

	// Stream ID for logging
	streamID := fmt.Sprintf("stream-%d", time.Now().UnixNano())
	span.SetAttributes(attribute.String("stream.id", streamID))

	log.Printf("📡 New event stream established: %s", streamID)

	// Process incoming events
	for {
		// Receive event from collector
		pbEvent, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				log.Printf("📡 Stream %s closed by client", streamID)
				return nil
			}
			span.RecordError(err)
			return status.Errorf(codes.Internal, "failed to receive event: %v", err)
		}

		// Convert protobuf event to domain event
		event := s.pbEventToDomain(pbEvent)

		// Extract OTEL context from event
		if event.Context.TraceID != "" && event.Context.SpanID != "" {
			// Link to the original trace
			span.AddLink(trace.Link{
				SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
					TraceID: trace.TraceID([]byte(event.Context.TraceID)),
					SpanID:  trace.SpanID([]byte(event.Context.SpanID)),
				}),
			})
		}

		// Process event through correlation manager
		insights := s.correlationMgr.ProcessEvents([]domain.Event{event})

		// Store event
		s.storeEvent(event)

		// Send acknowledgment with any insights
		ack := &pb.EventAck{
			EventId:   pbEvent.Id,
			Timestamp: time.Now().Unix(),
			Status:    "processed",
		}

		// Add insights if any
		if len(insights) > 0 {
			ack.Metadata = map[string]string{
				"insights_count": fmt.Sprintf("%d", len(insights)),
				"correlation_id": insights[0].ID,
			}
		}

		if err := stream.Send(ack); err != nil {
			span.RecordError(err)
			return status.Errorf(codes.Internal, "failed to send ack: %v", err)
		}

		// Broadcast to subscribers
		s.broadcastEvent(pbEvent)

		// Update statistics
		s.updateStats(len(insights))
	}
}

// GetCorrelations returns current correlations with OTEL context
func (s *EnhancedServer) GetCorrelations(ctx context.Context, req *pb.GetCorrelationsRequest) (*pb.GetCorrelationsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "server.get_correlations",
		trace.WithAttributes(
			attribute.Int("limit", int(req.Limit)),
			attribute.String("filter", req.Filter),
		),
	)
	defer span.End()

	// Get insights from correlation manager
	insights := s.correlationMgr.GetInsights()

	// Convert to protobuf format
	pbCorrelations := make([]*pb.Correlation, 0, len(insights))
	for _, insight := range insights {
		pbCorr := &pb.Correlation{
			Id:          insight.ID,
			Type:        insight.Type,
			Title:       insight.Title,
			Description: insight.Description,
			Severity:    string(insight.Severity),
			Timestamp:   insight.Timestamp.Unix(),
			Metadata:    make(map[string]string),
		}

		// Add metadata
		for k, v := range insight.Metadata {
			pbCorr.Metadata[k] = fmt.Sprintf("%v", v)
		}

		// Add trace context if available
		if traceID, ok := insight.Metadata["trace_id"].(string); ok {
			pbCorr.TraceId = traceID
		}

		pbCorrelations = append(pbCorrelations, pbCorr)
	}

	span.SetAttributes(attribute.Int("correlations.count", len(pbCorrelations)))

	return &pb.GetCorrelationsResponse{
		Correlations: pbCorrelations,
		TotalCount:   int64(len(pbCorrelations)),
	}, nil
}

// SubscribeToEvents allows clients to subscribe to real-time events
func (s *EnhancedServer) SubscribeToEvents(req *pb.SubscribeRequest, stream pb.TapioService_SubscribeToEventsServer) error {
	ctx := stream.Context()
	ctx, span := s.tracer.Start(ctx, "server.subscribe_events",
		trace.WithAttributes(
			attribute.StringSlice("event_types", req.EventTypes),
			attribute.String("severity_filter", req.SeverityFilter),
		),
	)
	defer span.End()

	// Create subscriber channel
	subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	eventChan := make(chan *pb.Event, 100)

	// Register subscriber
	s.subscribersLock.Lock()
	s.subscribers[subID] = eventChan
	s.subscribersLock.Unlock()

	// Cleanup on exit
	defer func() {
		s.subscribersLock.Lock()
		delete(s.subscribers, subID)
		s.subscribersLock.Unlock()
		close(eventChan)
	}()

	log.Printf("📢 New subscriber: %s (filters: %v)", subID, req.EventTypes)

	// Send events to subscriber
	for {
		select {
		case event := <-eventChan:
			// Apply filters
			if !s.matchesFilters(event, req) {
				continue
			}

			// Send to subscriber
			if err := stream.Send(event); err != nil {
				span.RecordError(err)
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Helper methods

func (s *EnhancedServer) pbEventToDomain(pbEvent *pb.Event) domain.Event {
	event := domain.Event{
		ID:        domain.EventID(pbEvent.Id),
		Type:      domain.EventType(pbEvent.Type),
		Source:    domain.SourceType(pbEvent.Source),
		Timestamp: time.Unix(pbEvent.Timestamp, 0),
		Severity:  domain.EventSeverity(pbEvent.Severity),
		Message:   pbEvent.Message,
		Context: domain.EventContext{
			Service:   pbEvent.Context.Service,
			Component: pbEvent.Context.Component,
			Namespace: pbEvent.Context.Namespace,
			Host:      pbEvent.Context.Host,
			TraceID:   pbEvent.TraceId,
			SpanID:    pbEvent.SpanId,
		},
	}

	// Copy metadata
	if pbEvent.Metadata != nil {
		event.Context.Metadata = make(map[string]interface{})
		for k, v := range pbEvent.Metadata {
			event.Context.Metadata[k] = v
		}
	}

	// Copy attributes
	if pbEvent.Attributes != nil {
		event.Attributes = make(map[string]interface{})
		for k, v := range pbEvent.Attributes {
			event.Attributes[k] = v
		}
	}

	return event
}

func (s *EnhancedServer) storeEvent(event domain.Event) {
	s.eventBufferLock.Lock()
	defer s.eventBufferLock.Unlock()

	s.eventBuffer = append(s.eventBuffer, event)

	// Maintain buffer size
	if len(s.eventBuffer) > 10000 {
		s.eventBuffer = s.eventBuffer[len(s.eventBuffer)-10000:]
	}
}

func (s *EnhancedServer) broadcastEvent(event *pb.Event) {
	s.subscribersLock.RLock()
	defer s.subscribersLock.RUnlock()

	for _, ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

func (s *EnhancedServer) matchesFilters(event *pb.Event, req *pb.SubscribeRequest) bool {
	// Check event type filter
	if len(req.EventTypes) > 0 {
		matched := false
		for _, t := range req.EventTypes {
			if event.Type == t {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check severity filter
	if req.SeverityFilter != "" && event.Severity != req.SeverityFilter {
		return false
	}

	return true
}

func (s *EnhancedServer) updateStats(insightsCount int) {
	s.statsLock.Lock()
	defer s.statsLock.Unlock()

	s.stats.eventsReceived++
	if insightsCount > 0 {
		s.stats.correlationsFound += int64(insightsCount)
	}
	if s.stats.eventsReceived%1000 == 0 {
		log.Printf("📊 Stats: Events=%d, Correlations=%d, ActiveStreams=%d",
			s.stats.eventsReceived, s.stats.correlationsFound, s.stats.activeStreams)
	}
}

func (s *EnhancedServer) incrementActiveStreams() {
	s.statsLock.Lock()
	defer s.statsLock.Unlock()
	s.stats.activeStreams++
}

func (s *EnhancedServer) decrementActiveStreams() {
	s.statsLock.Lock()
	defer s.statsLock.Unlock()
	s.stats.activeStreams--
}

// metadataCarrier adapts gRPC metadata to OTEL carrier
type metadataCarrier metadata.MD

func (m metadataCarrier) Get(key string) string {
	vals := metadata.MD(m).Get(key)
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func (m metadataCarrier) Set(key string, value string) {
	metadata.MD(m).Set(key, value)
}

func (m metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range metadata.MD(m) {
		keys = append(keys, k)
	}
	return keys
}

// StartEnhancedServer starts the enhanced gRPC server
func StartEnhancedServer(config *ServerConfig) error {
	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(10*1024*1024), // 10MB
		grpc.MaxSendMsgSize(10*1024*1024), // 10MB
	)

	// Create and register enhanced server
	enhancedServer := NewEnhancedServer(config)
	pb.RegisterTapioServiceServer(grpcServer, enhancedServer)

	// Start correlation manager
	if err := enhancedServer.correlationMgr.Start(); err != nil {
		return fmt.Errorf("failed to start correlation manager: %w", err)
	}

	// Listen on configured port
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.Address, config.GRPCPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	log.Printf("🚀 Enhanced Tapio Server listening on %s:%d", config.Address, config.GRPCPort)
	log.Printf("   ✅ OTEL tracing enabled")
	log.Printf("   ✅ Semantic correlation active")
	log.Printf("   ✅ Real-time subscriptions supported")

	// Start server
	return grpcServer.Serve(listener)
}
