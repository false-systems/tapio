package internal

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"log"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GRPCClient handles streaming events to the Tapio server
type GRPCClient struct {
	serverAddr string
	conn       *grpc.ClientConn
	client     pb.TapioServiceClient
	stream     pb.TapioService_StreamEventsClient

	mu       sync.Mutex
	isActive bool

	tracer trace.Tracer

	// Collector info
	collectorID   string
	collectorType string
}

// NewGRPCClient creates a new gRPC client
func NewGRPCClient(serverAddr string) (*GRPCClient, error) {
	return &GRPCClient{
		serverAddr:    serverAddr,
		tracer:        otel.Tracer("systemd-collector"),
		collectorType: "systemd",
		collectorID:   fmt.Sprintf("systemd-%d", time.Now().Unix()),
	}, nil
}

// Connect establishes connection to the server
func (c *GRPCClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create connection
	conn, err := grpc.DialContext(ctx, c.serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}

	c.conn = conn
	c.client = pb.NewTapioServiceClient(conn)

	// Create bidirectional stream
	stream, err := c.client.StreamEvents(ctx)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create stream: %w", err)
	}

	c.stream = stream
	c.isActive = true

	// Start receiving responses
	go c.receiveResponses(ctx)

	// Send initial heartbeat
	heartbeat := &pb.TapioStreamEventsRequest{
		Request: &pb.TapioStreamEventsRequest_Control{
			Control: &pb.StreamControl{
				Type: pb.StreamControl_CONTROL_TYPE_HEARTBEAT,
				Parameters: map[string]string{
					"collector_id":   c.collectorID,
					"collector_type": c.collectorType,
					"version":        "1.0.0",
				},
			},
		},
	}

	if err := c.stream.Send(heartbeat); err != nil {
		stream.CloseSend()
		conn.Close()
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	return nil
}

// SendEvent sends a domain event to the server
func (c *GRPCClient) SendEvent(ctx context.Context, event domain.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isActive || c.stream == nil {
		return fmt.Errorf("gRPC client not connected")
	}

	// Start a span for this event
	ctx, span := c.tracer.Start(ctx, "send_systemd_event",
		trace.WithAttributes(
			attribute.String("event.id", string(event.ID)),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", string(event.Severity)),
		),
	)
	defer span.End()

	// Convert domain event to proto event
	protoEvent, err := c.domainEventToProto(event, span)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to convert event: %w", err)
	}

	// Create stream request
	request := &pb.TapioStreamEventsRequest{
		Request: &pb.TapioStreamEventsRequest_Event{
			Event: protoEvent,
		},
	}

	// Add trace context to gRPC metadata
	md := metadata.New(map[string]string{
		"trace-id": span.SpanContext().TraceID().String(),
		"span-id":  span.SpanContext().SpanID().String(),
	})
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Send the event
	if err := c.stream.Send(request); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to send event: %w", err)
	}

	return nil
}

// SendEventBatch sends multiple events as a batch
func (c *GRPCClient) SendEventBatch(ctx context.Context, events []domain.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isActive || c.stream == nil {
		return fmt.Errorf("gRPC client not connected")
	}

	// Start a span for this batch
	ctx, span := c.tracer.Start(ctx, "send_systemd_event_batch",
		trace.WithAttributes(
			attribute.Int("batch.size", len(events)),
		),
	)
	defer span.End()

	// Convert domain events to proto events
	protoEvents := make([]*pb.Event, 0, len(events))
	for _, event := range events {
		protoEvent, err := c.domainEventToProto(event, span)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to convert event: %w", err)
		}
		protoEvents = append(protoEvents, protoEvent)
	}

	// Create batch request
	batch := &pb.EventBatch{
		BatchId:     fmt.Sprintf("batch-%d", time.Now().UnixNano()),
		Events:      protoEvents,
		CollectorId: c.collectorID,
	}

	request := &pb.TapioStreamEventsRequest{
		Request: &pb.TapioStreamEventsRequest_Batch{
			Batch: batch,
		},
	}

	// Send the batch
	if err := c.stream.Send(request); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to send batch: %w", err)
	}

	return nil
}

// domainEventToProto converts a domain event to proto event
func (c *GRPCClient) domainEventToProto(event domain.Event, span trace.Span) (*pb.Event, error) {
	// Convert event data to structpb
	dataStruct, err := structpb.NewStruct(event.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert event data: %w", err)
	}

	// Convert severity
	severity := c.domainSeverityToProto(event.Severity)

	// Convert source type
	source := c.domainSourceToProto(event.Source)

	// Create event context
	eventContext := &pb.EventContext{
		Service:   event.Context.Service,
		Component: event.Context.Component,
		Host:      event.Context.Host,
		TraceId:   span.SpanContext().TraceID().String(),
		SpanId:    span.SpanContext().SpanID().String(),
		Labels:    event.Context.Labels,
	}

	// Extract message from data if available
	message := event.Message
	if message == "" {
		if msg, ok := event.Data["message"].(string); ok {
			message = msg
		} else if unitName, ok := event.Data["service_name"].(string); ok {
			if eventType, ok := event.Data["event_type"].(string); ok {
				message = fmt.Sprintf("Service %s: %s", unitName, eventType)
			}
		}
	}

	// Convert attributes
	attributes := make(map[string]string)
	for k, v := range event.Attributes {
		attributes[k] = fmt.Sprintf("%v", v)
	}

	return &pb.Event{
		Id:          string(event.ID),
		Type:        pb.EventType_EVENT_TYPE_PROCESS, // systemd manages processes
		Severity:    severity,
		Source:      source,
		Message:     message,
		Timestamp:   timestamppb.New(event.Timestamp),
		Context:     eventContext,
		TraceId:     span.SpanContext().TraceID().String(),
		SpanId:      span.SpanContext().SpanID().String(),
		Data:        dataStruct,
		Attributes:  attributes,
		Confidence:  event.Confidence,
		Tags:        event.Tags,
		CollectorId: c.collectorID,
	}, nil
}

// domainSeverityToProto converts domain severity to proto severity
func (c *GRPCClient) domainSeverityToProto(severity domain.EventSeverity) pb.EventSeverity {
	switch severity {
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
		return pb.EventSeverity_EVENT_SEVERITY_INFO
	}
}

// domainSourceToProto converts domain source to proto source
func (c *GRPCClient) domainSourceToProto(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_SYSTEMD
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES
	default:
		return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
	}
}

// receiveResponses handles responses from the server
func (c *GRPCClient) receiveResponses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			response, err := c.stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				// Log error but continue
				log.Printf("[gRPC Client] Failed to receive response: %v", err)
				continue
			}

			// Process response based on type
			switch resp := response.Response.(type) {
			case *pb.TapioStreamEventsResponse_Ack:
				// Process acknowledgment
				if resp.Ack.EventId != "" {
					// Single event acknowledged
				} else if resp.Ack.BatchId != "" {
					// Batch acknowledged
				}

			case *pb.TapioStreamEventsResponse_Error:
				log.Printf("[gRPC Client] Server error: %s", resp.Error.Message)

			case *pb.TapioStreamEventsResponse_Control:
				// Handle control response
				if resp.Control.Success {
					// Control command succeeded
				}

			case *pb.TapioStreamEventsResponse_Correlation:
				// Handle correlation found by server
				log.Printf("[gRPC Client] Correlation found: %s - %s", resp.Correlation.Id, resp.Correlation.Description)

			case *pb.TapioStreamEventsResponse_SemanticGroup:
				// Handle semantic group update
				log.Printf("[gRPC Client] Semantic group: %s - %s", resp.SemanticGroup.Id, resp.SemanticGroup.Name)
			}
		}
	}
}

// Heartbeat sends periodic heartbeat to keep connection alive
func (c *GRPCClient) Heartbeat(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isActive || c.stream == nil {
		return fmt.Errorf("gRPC client not connected")
	}

	heartbeat := &pb.TapioStreamEventsRequest{
		Request: &pb.TapioStreamEventsRequest_Control{
			Control: &pb.StreamControl{
				Type: pb.StreamControl_CONTROL_TYPE_HEARTBEAT,
				Parameters: map[string]string{
					"timestamp": time.Now().Format(time.RFC3339),
				},
			},
		},
	}

	return c.stream.Send(heartbeat)
}

// Close closes the gRPC connection
func (c *GRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.isActive = false

	if c.stream != nil {
		c.stream.CloseSend()
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// IsConnected returns true if the client is connected
func (c *GRPCClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isActive
}
