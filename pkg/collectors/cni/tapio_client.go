package cni

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TapioGRPCClient implements the TapioClient interface for streaming CNI events to Tapio server
type TapioGRPCClient struct {
	// Configuration
	serverAddr  string
	collectorID string

	// gRPC connection
	conn   *grpc.ClientConn
	client pb.TapioServiceClient
	stream pb.TapioService_StreamEventsClient

	// State management
	connected bool
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc

	// Buffering
	eventBuffer   chan *domain.UnifiedEvent
	batchSize     int
	flushInterval time.Duration

	// OTEL integration
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator

	// Metrics
	eventsSent    uint64
	eventsDropped uint64
	reconnects    uint64
	lastSent      time.Time
}

// TapioClientConfig contains configuration for the Tapio client
type TapioClientConfig struct {
	ServerAddr    string        `json:"server_addr"`
	CollectorID   string        `json:"collector_id"`
	BufferSize    int           `json:"buffer_size"`
	BatchSize     int           `json:"batch_size"`
	FlushInterval time.Duration `json:"flush_interval"`
	RetryInterval time.Duration `json:"retry_interval"`
	MaxRetries    int           `json:"max_retries"`
	EnableOTEL    bool          `json:"enable_otel"`
}

// NewTapioGRPCClient creates a new Tapio gRPC client for CNI collector
func NewTapioGRPCClient(serverAddr string) (*TapioGRPCClient, error) {
	config := &TapioClientConfig{
		ServerAddr:    serverAddr,
		CollectorID:   "cni-collector",
		BufferSize:    10000,
		BatchSize:     100,
		FlushInterval: time.Second,
		RetryInterval: 5 * time.Second,
		MaxRetries:    5,
		EnableOTEL:    true,
	}

	return NewTapioGRPCClientWithConfig(config)
}

// NewTapioGRPCClientWithConfig creates a new Tapio gRPC client with custom configuration
func NewTapioGRPCClientWithConfig(config *TapioClientConfig) (*TapioGRPCClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &TapioGRPCClient{
		serverAddr:    config.ServerAddr,
		collectorID:   config.CollectorID,
		eventBuffer:   make(chan *domain.UnifiedEvent, config.BufferSize),
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		ctx:           ctx,
		cancel:        cancel,
	}

	// Initialize OTEL if enabled
	if config.EnableOTEL {
		client.initializeOTEL()
	}

	// Start connection management
	go client.connectionManager()
	go client.eventSender()

	return client, nil
}

// initializeOTEL sets up OpenTelemetry tracing
func (c *TapioGRPCClient) initializeOTEL() {
	// Initialize tracer for CNI collector
	c.tracer = otel.Tracer("tapio.cni.collector")
	c.propagator = otel.GetTextMapPropagator()
}

// SendEvent sends a single UnifiedEvent to Tapio with OTEL tracing
func (c *TapioGRPCClient) SendEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	// Create OTEL span for event sending
	ctx, span := c.createEventSpan(ctx, event, "cni.collector.send_event")
	defer func() {
		if span != nil {
			span.End()
		}
	}()

	select {
	case c.eventBuffer <- event:
		if span != nil {
			span.SetAttributes(
				attribute.String("event.id", event.ID),
				attribute.String("event.type", string(event.Type)),
				attribute.String("event.source", string(event.Source)),
			)
		}
		return nil
	case <-c.ctx.Done():
		// Client is being closed
		err := fmt.Errorf("client is closed")
		if span != nil {
			span.RecordError(err)
		}
		return err
	case <-ctx.Done():
		if span != nil {
			span.RecordError(ctx.Err())
		}
		return ctx.Err()
	default:
		c.eventsDropped++
		err := fmt.Errorf("event buffer full, event dropped")
		if span != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("event.dropped", true))
		}
		return err
	}
}

// SendBatch sends a batch of UnifiedEvents to Tapio with OTEL tracing
func (c *TapioGRPCClient) SendBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, "cni.collector.send_batch",
			trace.WithAttributes(
				attribute.Int("batch.size", len(events)),
				attribute.String("collector.id", c.collectorID),
			),
		)
		defer span.End()
	}

	for i, event := range events {
		if err := c.SendEvent(ctx, event); err != nil {
			if span != nil {
				span.RecordError(err)
				span.SetAttributes(
					attribute.Int("batch.failed_at_index", i),
					attribute.Int("batch.events_processed", i),
				)
			}
			return err
		}
	}

	if span != nil {
		span.SetAttributes(attribute.Int("batch.events_sent", len(events)))
	}
	return nil
}

// Subscribe is not implemented for the client (server functionality)
func (c *TapioGRPCClient) Subscribe(ctx context.Context, opts domain.SubscriptionOptions) (<-chan *domain.UnifiedEvent, error) {
	return nil, fmt.Errorf("subscribe not supported on client")
}

// Close closes the client connection
func (c *TapioGRPCClient) Close() error {
	var span trace.Span
	if c.tracer != nil {
		_, span = c.tracer.Start(context.Background(), "cni.collector.close")
		defer span.End()
	}

	c.cancel()
	close(c.eventBuffer)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream != nil {
		c.stream.CloseSend()
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// connectionManager manages the gRPC connection and stream with OTEL tracing
func (c *TapioGRPCClient) connectionManager() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if !c.isConnected() {
				var span trace.Span
				if c.tracer != nil {
					_, span = c.tracer.Start(c.ctx, "cni.collector.connection_attempt",
						trace.WithAttributes(
							attribute.String("server.addr", c.serverAddr),
						),
					)
				}

				if err := c.connect(); err != nil {
					log.Printf("Failed to connect to Tapio server: %v", err)
					if span != nil {
						span.RecordError(err)
						span.SetAttributes(attribute.Bool("connection.successful", false))
					}
				} else {
					if span != nil {
						span.SetAttributes(attribute.Bool("connection.successful", true))
					}
				}
				if span != nil {
					span.End()
				}
			}
		}
	}
}

// eventSender handles buffered event sending with OTEL tracing
func (c *TapioGRPCClient) eventSender() {
	batch := make([]*domain.UnifiedEvent, 0, c.batchSize)
	flushTicker := time.NewTicker(c.flushInterval)
	defer flushTicker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if c.tracer != nil {
			ctx, span := c.tracer.Start(c.ctx, "cni.collector.flush_batch",
				trace.WithAttributes(
					attribute.Int("batch.size", len(batch)),
					attribute.String("collector.id", c.collectorID),
				),
			)
			defer span.End()

			if err := c.sendBatchToStream(ctx, batch); err != nil {
				log.Printf("Failed to send batch to Tapio: %v", err)
				span.RecordError(err)
				span.SetAttributes(attribute.Bool("batch.successful", false))
			} else {
				c.eventsSent += uint64(len(batch))
				c.lastSent = time.Now()
				span.SetAttributes(
					attribute.Bool("batch.successful", true),
					attribute.Int64("batch.events_sent_total", int64(c.eventsSent)),
				)
			}
		} else {
			if err := c.sendBatchToStream(c.ctx, batch); err != nil {
				log.Printf("Failed to send batch to Tapio: %v", err)
			} else {
				c.eventsSent += uint64(len(batch))
				c.lastSent = time.Now()
			}
		}

		batch = batch[:0] // Reset batch
	}

	for {
		select {
		case <-c.ctx.Done():
			flush() // Final flush
			return
		case event, ok := <-c.eventBuffer:
			if !ok {
				flush()
				return
			}

			batch = append(batch, event)
			if len(batch) >= c.batchSize {
				flush()
			}
		case <-flushTicker.C:
			flush()
		}
	}
}

// connect establishes a gRPC connection and stream with OTEL tracing
func (c *TapioGRPCClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close existing connection if any
	if c.conn != nil {
		c.conn.Close()
	}

	// Create new connection
	conn, err := grpc.Dial(c.serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(10*time.Second),
		grpc.WithUnaryInterceptor(c.otelUnaryClientInterceptor()),
		grpc.WithStreamInterceptor(c.otelStreamClientInterceptor()))
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.serverAddr, err)
	}

	c.conn = conn
	c.client = pb.NewTapioServiceClient(conn)

	// Create bidirectional stream
	stream, err := c.client.StreamEvents(c.ctx)
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to create stream: %w", err)
	}

	c.stream = stream
	c.connected = true
	c.reconnects++

	log.Printf("Connected to Tapio server at %s", c.serverAddr)

	// Start response handler
	go c.handleResponses()

	return nil
}

// sendBatchToStream sends a batch of UnifiedEvents to the Tapio stream with OTEL context
func (c *TapioGRPCClient) sendBatchToStream(ctx context.Context, events []*domain.UnifiedEvent) error {
	c.mu.RLock()
	stream := c.stream
	connected := c.connected
	c.mu.RUnlock()

	if !connected || stream == nil {
		return fmt.Errorf("not connected to Tapio server")
	}

	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, "cni.collector.send_batch_to_stream",
			trace.WithAttributes(
				attribute.Int("batch.size", len(events)),
				attribute.String("collector.id", c.collectorID),
			),
		)
		defer span.End()
	}

	// Convert UnifiedEvents to protobuf events
	pbEvents := make([]*pb.Event, 0, len(events))
	for _, event := range events {
		pbEvent := c.convertUnifiedEventToProto(ctx, event)
		pbEvents = append(pbEvents, pbEvent)
	}

	// Create batch request
	batch := &pb.EventBatch{
		BatchId:     fmt.Sprintf("cni-batch-%d", time.Now().UnixNano()),
		CollectorId: c.collectorID,
		Events:      pbEvents,
		Metadata: map[string]string{
			"batch_size": fmt.Sprintf("%d", len(pbEvents)),
			"source":     "cni-collector",
		},
	}

	// Propagate OTEL context
	if c.propagator != nil {
		md := metadata.New(nil)
		c.propagator.Inject(ctx, &metadataCarrier{md: &md})
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	// Send batch
	req := &pb.TapioStreamEventsRequest{
		Request: &pb.TapioStreamEventsRequest_Batch{
			Batch: batch,
		},
	}

	err := stream.Send(req)
	if err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("stream.send.successful", false))
		}
	} else {
		if span != nil {
			span.SetAttributes(
				attribute.Bool("stream.send.successful", true),
				attribute.String("batch.id", batch.BatchId),
			)
		}
	}

	return err
}

// handleResponses handles responses from the Tapio server with OTEL tracing
func (c *TapioGRPCClient) handleResponses() {
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
	}()

	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return
	}

	for {
		var span trace.Span
		if c.tracer != nil {
			_, span = c.tracer.Start(c.ctx, "cni.collector.handle_response")
		}

		resp, err := stream.Recv()
		if err != nil {
			log.Printf("Stream receive error: %v", err)
			if span != nil {
				span.RecordError(err)
				span.End()
			}
			return
		}

		c.handleResponse(resp)
		if span != nil {
			span.SetAttributes(attribute.String("response.type", c.getResponseType(resp)))
			span.End()
		}
	}
}

// handleResponse processes a response from the Tapio server
func (c *TapioGRPCClient) handleResponse(resp *pb.TapioStreamEventsResponse) {
	switch r := resp.Response.(type) {
	case *pb.TapioStreamEventsResponse_Ack:
		// Event acknowledgment
		log.Printf("Received ack for batch %s", r.Ack.BatchId)

	case *pb.TapioStreamEventsResponse_Error:
		// Error response
		log.Printf("Received error from Tapio: %s", r.Error.Message)

	case *pb.TapioStreamEventsResponse_Control:
		// Control message
		log.Printf("Received control message: %s", r.Control.Message)

	case *pb.TapioStreamEventsResponse_Correlation:
		// Correlation result
		log.Printf("Received correlation: %s", r.Correlation.Title)

	case *pb.TapioStreamEventsResponse_SemanticGroup:
		// Semantic group result
		log.Printf("Received semantic group: %s", r.SemanticGroup.Name)
	}
}

// getResponseType returns the type of response for OTEL attributes
func (c *TapioGRPCClient) getResponseType(resp *pb.TapioStreamEventsResponse) string {
	switch resp.Response.(type) {
	case *pb.TapioStreamEventsResponse_Ack:
		return "ack"
	case *pb.TapioStreamEventsResponse_Error:
		return "error"
	case *pb.TapioStreamEventsResponse_Control:
		return "control"
	case *pb.TapioStreamEventsResponse_Correlation:
		return "correlation"
	case *pb.TapioStreamEventsResponse_SemanticGroup:
		return "semantic_group"
	default:
		return "unknown"
	}
}

// convertUnifiedEventToProto converts a UnifiedEvent to protobuf format with OTEL context
func (c *TapioGRPCClient) convertUnifiedEventToProto(ctx context.Context, event *domain.UnifiedEvent) *pb.Event {
	// Determine message and severity from the appropriate source
	message := c.extractMessage(event)
	severity := c.extractSeverity(event)
	confidence := c.extractConfidence(event)
	tags := c.extractTags(event)

	pbEvent := &pb.Event{
		Id:          event.ID,
		Type:        c.mapEventType(event.Type),
		Severity:    c.mapEventSeverity(severity),
		Source:      c.mapSourceType(domain.SourceType(event.Source)),
		Message:     message,
		Timestamp:   timestamppb.New(event.Timestamp),
		CollectorId: c.collectorID,
		Confidence:  float64(confidence),
		Tags:        tags,
		Attributes:  c.convertEventAttributes(event),
	}

	// Add trace context if available from OTEL span
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		pbEvent.TraceId = spanCtx.TraceID().String()
		pbEvent.SpanId = spanCtx.SpanID().String()
	}

	// Add trace context from event if available
	if event.TraceContext != nil {
		pbEvent.TraceId = event.TraceContext.TraceID
		pbEvent.SpanId = event.TraceContext.SpanID
	}

	// Add CNI-specific context
	if event.Entity != nil {
		clusterName := ""
		nodeName := ""
		if event.Entity.Labels != nil {
			clusterName = event.Entity.Labels["cluster"]
			nodeName = event.Entity.Labels["node"]
		}

		pbEvent.Context = &pb.EventContext{
			TraceId:   pbEvent.TraceId,
			SpanId:    pbEvent.SpanId,
			Cluster:   clusterName,
			Namespace: event.Entity.Namespace,
			Node:      nodeName,
			Pod:       event.Entity.Name, // For CNI, the entity often represents a pod
			Labels:    event.Entity.Labels,
		}
	}

	return pbEvent
}

// Helper mapping functions for CNI events
func (c *TapioGRPCClient) mapEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeKubernetes:
		return pb.EventType_EVENT_TYPE_KUBERNETES
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeService:
		return pb.EventType_EVENT_TYPE_HTTP
	case domain.EventTypeMemory:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeCPU:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeDisk:
		return pb.EventType_EVENT_TYPE_FILE_SYSTEM
	default:
		return pb.EventType_EVENT_TYPE_NETWORK // Default for CNI events
	}
}

func (c *TapioGRPCClient) mapEventSeverity(severity domain.EventSeverity) pb.EventSeverity {
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

func (c *TapioGRPCClient) mapSourceType(source domain.SourceType) pb.SourceType {
	switch source {
	case domain.SourceCNI:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_JOURNALD
	default:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API // Default for CNI
	}
}

// Helper methods to extract data from UnifiedEvent structure
func (c *TapioGRPCClient) extractMessage(event *domain.UnifiedEvent) string {
	if event.Kubernetes != nil && event.Kubernetes.Message != "" {
		return event.Kubernetes.Message
	}
	if event.Application != nil && event.Application.Message != "" {
		return event.Application.Message
	}
	if event.Semantic != nil && event.Semantic.Narrative != "" {
		return event.Semantic.Narrative
	}
	// Construct message from network data if available
	if event.Network != nil {
		if event.Network.Protocol != "" && event.Network.SourceIP != "" {
			return fmt.Sprintf("Network traffic: %s from %s to %s", 
				event.Network.Protocol, event.Network.SourceIP, event.Network.DestIP)
		}
	}
	return fmt.Sprintf("CNI event %s from %s", event.Type, event.Source)
}

func (c *TapioGRPCClient) extractSeverity(event *domain.UnifiedEvent) domain.EventSeverity {
	// Use the UnifiedEvent's GetSeverity method
	severity := event.GetSeverity()
	return domain.EventSeverity(severity)
}

func (c *TapioGRPCClient) extractConfidence(event *domain.UnifiedEvent) float32 {
	if event.Semantic != nil {
		return float32(event.Semantic.Confidence)
	}
	return 1.0 // Default confidence
}

func (c *TapioGRPCClient) extractTags(event *domain.UnifiedEvent) []string {
	if event.Semantic != nil && len(event.Semantic.Tags) > 0 {
		return event.Semantic.Tags
	}
	// Generate default tags based on event type and source
	tags := []string{string(event.Type), event.Source}
	if event.Entity != nil && event.Entity.Type != "" {
		tags = append(tags, event.Entity.Type)
	}
	return tags
}

func (c *TapioGRPCClient) convertEventAttributes(event *domain.UnifiedEvent) map[string]string {
	attributes := make(map[string]string)

	// Add entity attributes
	if event.Entity != nil {
		if event.Entity.UID != "" {
			attributes["entity.uid"] = event.Entity.UID
		}
		if event.Entity.Type != "" {
			attributes["entity.type"] = event.Entity.Type
		}
		if event.Entity.Name != "" {
			attributes["entity.name"] = event.Entity.Name
		}
		if event.Entity.Namespace != "" {
			attributes["entity.namespace"] = event.Entity.Namespace
		}

		// Add entity labels with prefix
		for k, v := range event.Entity.Labels {
			attributes["entity.label."+k] = v
		}

		// Add entity attributes with prefix
		for k, v := range event.Entity.Attributes {
			attributes["entity."+k] = v
		}
	}

	// Add Kubernetes-specific attributes
	if event.Kubernetes != nil {
		if event.Kubernetes.EventType != "" {
			attributes["k8s.event_type"] = event.Kubernetes.EventType
		}
		if event.Kubernetes.Reason != "" {
			attributes["k8s.reason"] = event.Kubernetes.Reason
		}
		if event.Kubernetes.Object != "" {
			attributes["k8s.object"] = event.Kubernetes.Object
		}
		if event.Kubernetes.ObjectKind != "" {
			attributes["k8s.object_kind"] = event.Kubernetes.ObjectKind
		}
		if event.Kubernetes.Action != "" {
			attributes["k8s.action"] = event.Kubernetes.Action
		}
		if event.Kubernetes.APIVersion != "" {
			attributes["k8s.api_version"] = event.Kubernetes.APIVersion
		}

		// Add K8s labels with prefix
		for k, v := range event.Kubernetes.Labels {
			attributes["k8s.label."+k] = v
		}

		// Add K8s annotations with prefix
		for k, v := range event.Kubernetes.Annotations {
			attributes["k8s.annotation."+k] = v
		}
	}

	// Add network data (CNI-specific)
	if event.Network != nil {
		if event.Network.Protocol != "" {
			attributes["network.protocol"] = event.Network.Protocol
		}
		if event.Network.SourceIP != "" {
			attributes["network.source_ip"] = event.Network.SourceIP
		}
		if event.Network.DestIP != "" {
			attributes["network.dest_ip"] = event.Network.DestIP
		}
		if event.Network.Direction != "" {
			attributes["network.direction"] = event.Network.Direction
		}
		if event.Network.StatusCode != 0 {
			attributes["network.status_code"] = fmt.Sprintf("%d", event.Network.StatusCode)
		}
		if event.Network.SourcePort != 0 {
			attributes["network.source_port"] = fmt.Sprintf("%d", event.Network.SourcePort)
		}
		if event.Network.DestPort != 0 {
			attributes["network.dest_port"] = fmt.Sprintf("%d", event.Network.DestPort)
		}
		if event.Network.BytesSent != 0 {
			attributes["network.bytes_sent"] = fmt.Sprintf("%d", event.Network.BytesSent)
		}
		if event.Network.BytesRecv != 0 {
			attributes["network.bytes_recv"] = fmt.Sprintf("%d", event.Network.BytesRecv)
		}
		if event.Network.Latency != 0 {
			attributes["network.latency_ns"] = fmt.Sprintf("%d", event.Network.Latency)
		}
		if event.Network.Method != "" {
			attributes["network.method"] = event.Network.Method
		}
		if event.Network.Path != "" {
			attributes["network.path"] = event.Network.Path
		}
	}

	// Add application data if present
	if event.Application != nil {
		if event.Application.Level != "" {
			attributes["app.level"] = event.Application.Level
		}
		if event.Application.Logger != "" {
			attributes["app.logger"] = event.Application.Logger
		}
		if event.Application.ErrorType != "" {
			attributes["app.error_type"] = event.Application.ErrorType
		}

		// Add application custom data
		for k, v := range event.Application.Custom {
			attributes["app."+k] = fmt.Sprintf("%v", v)
		}
	}

	return attributes
}

// createEventSpan creates an OTEL span for event operations
func (c *TapioGRPCClient) createEventSpan(ctx context.Context, event *domain.UnifiedEvent, operation string) (context.Context, trace.Span) {
	if c.tracer == nil {
		return ctx, nil
	}

	return c.tracer.Start(ctx, operation,
		trace.WithAttributes(
			attribute.String("event.id", event.ID),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.source", string(event.Source)),
			attribute.String("collector.id", c.collectorID),
		),
	)
}

// isConnected checks if the client is connected
func (c *TapioGRPCClient) isConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetStatistics returns client statistics
func (c *TapioGRPCClient) GetStatistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"connected":       c.connected,
		"events_sent":     c.eventsSent,
		"events_dropped":  c.eventsDropped,
		"reconnects":      c.reconnects,
		"buffer_size":     len(c.eventBuffer),
		"buffer_capacity": cap(c.eventBuffer),
		"last_sent":       c.lastSent,
		"server_addr":     c.serverAddr,
		"collector_id":    c.collectorID,
	}
}

// OTEL gRPC interceptors
func (c *TapioGRPCClient) otelUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	if c.tracer == nil {
		return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, span := c.tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
				attribute.String("collector.id", c.collectorID),
			),
		)
		defer span.End()

		// Propagate trace context
		if c.propagator != nil {
			md, _ := metadata.FromOutgoingContext(ctx)
			if md == nil {
				md = metadata.New(nil)
			}
			c.propagator.Inject(ctx, &metadataCarrier{md: &md})
			ctx = metadata.NewOutgoingContext(ctx, md)
		}

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String("rpc.grpc.status_code", "ERROR"))
		} else {
			span.SetAttributes(attribute.String("rpc.grpc.status_code", "OK"))
		}

		return err
	}
}

func (c *TapioGRPCClient) otelStreamClientInterceptor() grpc.StreamClientInterceptor {
	if c.tracer == nil {
		return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return streamer(ctx, desc, cc, method, opts...)
		}
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, span := c.tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
				attribute.String("collector.id", c.collectorID),
			),
		)

		// Propagate trace context
		if c.propagator != nil {
			md, _ := metadata.FromOutgoingContext(ctx)
			if md == nil {
				md = metadata.New(nil)
			}
			c.propagator.Inject(ctx, &metadataCarrier{md: &md})
			ctx = metadata.NewOutgoingContext(ctx, md)
		}

		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			span.RecordError(err)
			span.End()
			return nil, err
		}

		return &tracedClientStream{
			ClientStream: stream,
			span:         span,
		}, nil
	}
}

// metadataCarrier adapts gRPC metadata for OTEL propagation
type metadataCarrier struct {
	md *metadata.MD
}

func (mc *metadataCarrier) Get(key string) string {
	values := (*mc.md).Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (mc *metadataCarrier) Set(key string, value string) {
	(*mc.md).Set(key, value)
}

func (mc *metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(*mc.md))
	for k := range *mc.md {
		keys = append(keys, k)
	}
	return keys
}

// tracedClientStream wraps gRPC client stream with tracing
type tracedClientStream struct {
	grpc.ClientStream
	span trace.Span
}

func (s *tracedClientStream) CloseSend() error {
	err := s.ClientStream.CloseSend()
	if err != nil {
		s.span.RecordError(err)
	}
	s.span.End()
	return err
}