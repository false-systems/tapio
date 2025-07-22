package ebpf

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TapioGRPCClient implements the TapioClient interface for streaming events to Tapio server
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
	converter     *domain.EventConverter

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
}

// NewTapioGRPCClient creates a new Tapio gRPC client
func NewTapioGRPCClient(serverAddr string) (*TapioGRPCClient, error) {
	config := &TapioClientConfig{
		ServerAddr:    serverAddr,
		CollectorID:   "ebpf-collector",
		BufferSize:    10000,
		BatchSize:     100,
		FlushInterval: time.Second,
		RetryInterval: 5 * time.Second,
		MaxRetries:    5,
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
		converter:     domain.NewEventConverter(),
	}

	// Start connection management
	go client.connectionManager()
	go client.eventSender()

	return client, nil
}

// SendEvent sends a single event to Tapio
func (c *TapioGRPCClient) SendEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	select {
	case c.eventBuffer <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		c.eventsDropped++
		return fmt.Errorf("event buffer full, event dropped")
	}
}

// SendBatch sends a batch of events to Tapio
func (c *TapioGRPCClient) SendBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	for _, event := range events {
		if err := c.SendEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe is not implemented for the client (server functionality)
func (c *TapioGRPCClient) Subscribe(ctx context.Context, opts domain.SubscriptionOptions) (<-chan *domain.UnifiedEvent, error) {
	return nil, fmt.Errorf("subscribe not supported on client")
}

// Close closes the client connection
func (c *TapioGRPCClient) Close() error {
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

// connectionManager manages the gRPC connection and stream
func (c *TapioGRPCClient) connectionManager() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if !c.isConnected() {
				if err := c.connect(); err != nil {
					log.Printf("Failed to connect to Tapio server: %v", err)
				}
			}
		}
	}
}

// eventSender handles buffered event sending
func (c *TapioGRPCClient) eventSender() {
	batch := make([]*domain.UnifiedEvent, 0, c.batchSize)
	flushTicker := time.NewTicker(c.flushInterval)
	defer flushTicker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := c.sendBatchToStream(batch); err != nil {
			log.Printf("Failed to send batch to Tapio: %v", err)
		} else {
			c.eventsSent += uint64(len(batch))
			c.lastSent = time.Now()
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

// connect establishes a gRPC connection and stream
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
		grpc.WithTimeout(10*time.Second))
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

// sendBatchToStream sends a batch of events to the Tapio stream
func (c *TapioGRPCClient) sendBatchToStream(events []*domain.UnifiedEvent) error {
	c.mu.RLock()
	stream := c.stream
	connected := c.connected
	c.mu.RUnlock()

	if !connected || stream == nil {
		return fmt.Errorf("not connected to Tapio server")
	}

	// Convert domain events to protobuf events
	pbEvents := make([]*pb.Event, 0, len(events))
	for _, event := range events {
		pbEvent := c.convertDomainEventToProto(event)
		pbEvents = append(pbEvents, pbEvent)
	}

	// Create batch request
	batch := &pb.EventBatch{
		BatchId:     fmt.Sprintf("ebpf-batch-%d", time.Now().UnixNano()),
		CollectorId: c.collectorID,
		Events:      pbEvents,
		Metadata: map[string]string{
			"batch_size": fmt.Sprintf("%d", len(pbEvents)),
			"source":     "ebpf-collector",
		},
	}

	// Send batch
	req := &pb.TapioStreamEventsRequest{
		Request: &pb.TapioStreamEventsRequest_Batch{
			Batch: batch,
		},
	}

	return stream.Send(req)
}

// handleResponses handles responses from the Tapio server
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
		resp, err := stream.Recv()
		if err != nil {
			log.Printf("Stream receive error: %v", err)
			return
		}

		c.handleResponse(resp)
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

// convertDomainEventToProto converts a unified event to protobuf format
func (c *TapioGRPCClient) convertDomainEventToProto(unifiedEvent *domain.UnifiedEvent) *pb.Event {
	// First convert UnifiedEvent to domain.Event using the converter
	event := c.converter.FromUnifiedEvent(unifiedEvent)
	pbEvent := &pb.Event{
		Id:          string(event.ID),
		Type:        c.mapEventType(event.Type),
		Severity:    c.mapEventSeverity(event.Severity),
		Source:      c.mapSourceType(event.Source),
		Message:     event.Message,
		Timestamp:   timestamppb.New(event.Timestamp),
		CollectorId: c.collectorID,
		Confidence:  event.Confidence,
		Tags:        event.Tags,
		Attributes:  c.convertAttributes(event.Attributes),
	}

	// Add trace context if available
	if event.Context.TraceID != "" {
		pbEvent.TraceId = event.Context.TraceID
	}
	if event.Context.SpanID != "" {
		pbEvent.SpanId = event.Context.SpanID
	}

	// Create event context
	pbEvent.Context = &pb.EventContext{
		TraceId: event.Context.TraceID,
		SpanId:  event.Context.SpanID,
		Labels:  event.Context.Labels,
	}

	return pbEvent
}

// Helper mapping functions
func (c *TapioGRPCClient) mapEventType(eventType domain.EventType) pb.EventType {
	switch eventType {
	case domain.EventTypeSystem:
		return pb.EventType_EVENT_TYPE_SYSCALL
	case domain.EventTypeNetwork:
		return pb.EventType_EVENT_TYPE_NETWORK
	case domain.EventTypeProcess:
		return pb.EventType_EVENT_TYPE_PROCESS
	case domain.EventTypeMemory:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeCPU:
		return pb.EventType_EVENT_TYPE_RESOURCE_USAGE
	case domain.EventTypeDisk:
		return pb.EventType_EVENT_TYPE_FILE_SYSTEM
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
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
	case domain.SourceEBPF:
		return pb.SourceType_SOURCE_TYPE_EBPF
	case domain.SourceK8s:
		return pb.SourceType_SOURCE_TYPE_KUBERNETES_API
	case domain.SourceSystemd:
		return pb.SourceType_SOURCE_TYPE_SYSLOG
	default:
		return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
	}
}

func (c *TapioGRPCClient) convertAttributes(attrs map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range attrs {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
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
	}
}
