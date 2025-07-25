package grpc

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/pipeline"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RealtimeObservabilityService provides real-time event streaming
type RealtimeObservabilityService struct {
	UnimplementedRealtimeObservabilityServer

	// Real-time event pipeline
	pipeline *pipeline.RealtimeEventPipeline

	// Configuration
	batchSize      int
	streamInterval time.Duration
}

// NewRealtimeObservabilityService creates a new real-time observability service
func NewRealtimeObservabilityService(pipeline *pipeline.RealtimeEventPipeline) *RealtimeObservabilityService {
	return &RealtimeObservabilityService{
		pipeline:       pipeline,
		batchSize:      100,                   // Events per batch
		streamInterval: 10 * time.Millisecond, // Stream frequency
	}
}

// StreamEvents provides real-time event streaming to gRPC clients
func (ros *RealtimeObservabilityService) StreamEvents(req *StreamEventsRequest, stream RealtimeObservability_StreamEventsServer) error {
	log.Printf("Starting real-time event stream for client")

	// Create event buffer for batching
	events := make([]*domain.UnifiedEvent, ros.batchSize)

	// Stream events until client disconnects
	ticker := time.NewTicker(ros.streamInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			log.Printf("Client disconnected from event stream")
			return nil

		case <-ticker.C:
			// Get real-time events from pipeline
			count := ros.pipeline.GetRealtimeEvents(events)
			if count == 0 {
				continue // No events available
			}

			// Convert to gRPC format and send
			for i := 0; i < count; i++ {
				event := events[i]
				if event == nil {
					continue
				}

				// Convert UnifiedEvent to gRPC message
				grpcEvent := &Event{
					Id:        event.ID,
					Type:      string(event.Type),
					Source:    string(event.Source),
					Timestamp: timestamppb.New(event.Timestamp),
					Message:   event.Message,
					Level:     string(event.Level),
				}

				// Add trace context if available
				if event.TraceContext != nil {
					grpcEvent.TraceId = event.TraceContext.TraceID
					grpcEvent.SpanId = event.TraceContext.SpanID
				}

				// Add attributes
				if event.Attributes != nil {
					grpcEvent.Attributes = make(map[string]string)
					for k, v := range event.Attributes {
						if str, ok := v.(string); ok {
							grpcEvent.Attributes[k] = str
						} else {
							grpcEvent.Attributes[k] = fmt.Sprintf("%v", v)
						}
					}
				}

				// Stream the event
				response := &StreamEventsResponse{
					Event: grpcEvent,
				}

				if err := stream.Send(response); err != nil {
					log.Printf("Failed to send event to client: %v", err)
					return status.Error(codes.Internal, "failed to stream event")
				}
			}
		}
	}
}

// StreamCorrelations provides real-time correlation findings streaming
func (ros *RealtimeObservabilityService) StreamCorrelations(req *StreamCorrelationsRequest, stream RealtimeObservability_StreamCorrelationsServer) error {
	log.Printf("Starting real-time correlation stream for client")

	// Create correlation buffer for batching
	correlations := make([]pipeline.CorrelationOutput, ros.batchSize)

	// Stream correlations until client disconnects
	ticker := time.NewTicker(ros.streamInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			log.Printf("Client disconnected from correlation stream")
			return nil

		case <-ticker.C:
			// Get correlation outputs from pipeline
			count := ros.pipeline.GetCorrelations(correlations)
			if count == 0 {
				continue // No correlations available
			}

			// Convert to gRPC format and send
			for i := 0; i < count; i++ {
				correlation := correlations[i]

				// Convert CorrelationOutput to gRPC message
				grpcCorrelation := &CorrelationOutput{
					CorrelationId: ros.generateCorrelationID(&correlation),
					Confidence:    float32(correlation.Confidence),
					ProcessedAt:   timestamppb.New(correlation.ProcessedAt),
					ResultType:    string(correlation.ResultType),
				}

				// Add original event reference
				if correlation.OriginalEvent != nil {
					grpcCorrelation.EventId = correlation.OriginalEvent.ID
					grpcCorrelation.EventType = string(correlation.OriginalEvent.Type)
					grpcCorrelation.EventSource = string(correlation.OriginalEvent.Source)
				}

				// Add metadata
				if correlation.Metadata != nil {
					grpcCorrelation.Metadata = correlation.Metadata
				}

				// Add correlation findings if available
				if correlation.CorrelationData != nil {
					grpcCorrelation.PatternType = correlation.CorrelationData.PatternType
					grpcCorrelation.Description = correlation.CorrelationData.Description
				}

				// Stream the correlation
				response := &StreamCorrelationsResponse{
					Correlation: grpcCorrelation,
				}

				if err := stream.Send(response); err != nil {
					log.Printf("Failed to send correlation to client: %v", err)
					return status.Error(codes.Internal, "failed to stream correlation")
				}
			}
		}
	}
}

// GetEventRingMetrics returns real-time ring buffer metrics
func (ros *RealtimeObservabilityService) GetEventRingMetrics(ctx context.Context, req *GetEventRingMetricsRequest) (*GetEventRingMetricsResponse, error) {
	metrics := ros.pipeline.GetEventRingMetrics()

	return &GetEventRingMetricsResponse{
		Capacity:    metrics.Capacity,
		Size:        metrics.Size,
		WritePos:    metrics.WritePos,
		ReadPos:     metrics.ReadPos,
		Utilization: float32(metrics.Utilization),
	}, nil
}

// generateCorrelationID creates a unique ID for correlation output
func (ros *RealtimeObservabilityService) generateCorrelationID(correlation *pipeline.CorrelationOutput) string {
	if correlation.CorrelationData != nil && correlation.CorrelationData.ID != "" {
		return correlation.CorrelationData.ID
	}

	// Fallback: generate from event + timestamp
	if correlation.OriginalEvent != nil {
		return fmt.Sprintf("%s_%s_%d",
			correlation.OriginalEvent.Source,
			correlation.OriginalEvent.Type,
			correlation.ProcessedAt.Unix())
	}

	return fmt.Sprintf("correlation_%d", correlation.ProcessedAt.Unix())
}

// RegisterRealtimeObservabilityService registers the service with gRPC server
func RegisterRealtimeObservabilityService(server *grpc.Server, pipeline *pipeline.RealtimeEventPipeline) {
	service := NewRealtimeObservabilityService(pipeline)
	RegisterRealtimeObservabilityServer(server, service)
}
