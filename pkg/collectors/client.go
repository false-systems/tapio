package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/grpc"
	"github.com/yairfalse/tapio/pkg/events"
)

// GRPCStreamingClient wraps the gRPC client for collector-specific functionality
type GRPCStreamingClient struct {
	client     *grpc.Client
	config     GRPCConfig
	
	// Connection state
	connected  atomic.Bool
	
	// Event handling
	eventQueue chan *Event
	batchSize  int
	
	// Statistics
	eventsSent     uint64
	batchesSent    uint64
	sendErrors     uint64
	reconnections  uint64
	
	// Lifecycle
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	started       atomic.Bool
	stopped       atomic.Bool
}

// NewGRPCStreamingClient creates a new gRPC streaming client for collectors
func NewGRPCStreamingClient(grpcClient *grpc.Client) *GRPCStreamingClient {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &GRPCStreamingClient{
		client:     grpcClient,
		eventQueue: make(chan *Event, 10000), // Buffer for events
		batchSize:  100,                      // Default batch size
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins the gRPC streaming client
func (c *GRPCStreamingClient) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return fmt.Errorf("client already started")
	}
	
	// Start the underlying gRPC client
	if err := c.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start gRPC client: %w", err)
	}
	
	// Start event processing
	c.wg.Add(1)
	go c.processEvents()
	
	// Start connection monitoring
	c.wg.Add(1)
	go c.monitorConnection()
	
	return nil
}

// Stop gracefully stops the gRPC streaming client
func (c *GRPCStreamingClient) Stop() error {
	if !c.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	// Cancel context
	c.cancel()
	
	// Close event queue
	close(c.eventQueue)
	
	// Wait for goroutines to finish
	c.wg.Wait()
	
	// Stop the underlying gRPC client
	return c.client.Stop()
}

// SendEvent sends a single event to the server
func (c *GRPCStreamingClient) SendEvent(event *Event) error {
	if c.stopped.Load() {
		return fmt.Errorf("client is stopped")
	}
	
	select {
	case c.eventQueue <- event:
		return nil
	default:
		atomic.AddUint64(&c.sendErrors, 1)
		return fmt.Errorf("event queue full")
	}
}

// SendBatch sends a batch of events to the server
func (c *GRPCStreamingClient) SendBatch(events []*Event) error {
	if c.stopped.Load() {
		return fmt.Errorf("client is stopped")
	}
	
	for _, event := range events {
		select {
		case c.eventQueue <- event:
		default:
			atomic.AddUint64(&c.sendErrors, 1)
			return fmt.Errorf("event queue full")
		}
	}
	
	return nil
}

// GetStats returns client statistics
func (c *GRPCStreamingClient) GetStats() ClientStats {
	grpcStats := c.client.GetStats()
	
	return ClientStats{
		Connected:       c.connected.Load(),
		EventsSent:      atomic.LoadUint64(&c.eventsSent),
		BatchesSent:     atomic.LoadUint64(&c.batchesSent),
		SendErrors:      atomic.LoadUint64(&c.sendErrors),
		Reconnections:   atomic.LoadUint64(&c.reconnections),
		EventsPerSecond: grpcStats.EventsPerSecond,
		QueueLength:     len(c.eventQueue),
		QueueCapacity:   cap(c.eventQueue),
	}
}

// IsConnected returns true if connected to the server
func (c *GRPCStreamingClient) IsConnected() bool {
	return c.connected.Load()
}

// processEvents handles events from the queue and sends them to the server
func (c *GRPCStreamingClient) processEvents() {
	defer c.wg.Done()
	
	batch := make([]*Event, 0, c.batchSize)
	ticker := time.NewTicker(100 * time.Millisecond) // Batch timeout
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			// Send remaining events in batch before stopping
			if len(batch) > 0 {
				c.sendBatchToServer(batch)
			}
			return
			
		case event, ok := <-c.eventQueue:
			if !ok {
				// Channel closed, send remaining events
				if len(batch) > 0 {
					c.sendBatchToServer(batch)
				}
				return
			}
			
			// Add event to batch
			batch = append(batch, event)
			
			// Send batch if it's full
			if len(batch) >= c.batchSize {
				c.sendBatchToServer(batch)
				batch = batch[:0] // Reset batch
				ticker.Reset(100 * time.Millisecond)
			}
			
		case <-ticker.C:
			// Send batch on timeout
			if len(batch) > 0 {
				c.sendBatchToServer(batch)
				batch = batch[:0] // Reset batch
			}
		}
	}
}

// sendBatchToServer sends a batch of events to the gRPC server
func (c *GRPCStreamingClient) sendBatchToServer(collectorEvents []*Event) {
	if len(collectorEvents) == 0 {
		return
	}
	
	// Convert collector events to gRPC events
	grpcEvents := make([]*events.UnifiedEvent, len(collectorEvents))
	for i, event := range collectorEvents {
		grpcEvent, err := c.convertToGRPCEvent(event)
		if err != nil {
			atomic.AddUint64(&c.sendErrors, 1)
			continue
		}
		grpcEvents[i] = grpcEvent
	}
	
	// Send events using the gRPC client
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	
	if err := c.client.SendEvents(ctx, grpcEvents); err != nil {
		atomic.AddUint64(&c.sendErrors, 1)
		return
	}
	
	// Update statistics
	atomic.AddUint64(&c.eventsSent, uint64(len(collectorEvents)))
	atomic.AddUint64(&c.batchesSent, 1)
}

// convertToGRPCEvent converts a collector event to a gRPC event
func (c *GRPCStreamingClient) convertToGRPCEvent(collectorEvent *Event) (*events.UnifiedEvent, error) {
	// Create gRPC event using the unified event builder
	builder := events.NewBuilder().
		WithType(collectorEvent.Type, c.convertCategory(collectorEvent.Category)).
		WithSeverity(c.convertSeverity(collectorEvent.Severity)).
		WithSource(collectorEvent.SourceType, collectorEvent.Source, collectorEvent.CollectorID)
	
	// Add entity context if available
	if collectorEvent.Context != nil {
		builder = builder.WithEntity(
			c.determineEntityType(collectorEvent.Context),
			c.determineEntityID(collectorEvent.Context),
			c.determineEntityName(collectorEvent.Context),
		)
	}
	
	// Add attributes
	for key, value := range collectorEvent.Attributes {
		builder = builder.WithAttribute(key, value)
	}
	
	// Add labels
	for key, value := range collectorEvent.Labels {
		builder = builder.WithLabel(key, value)
	}
	
	// Add specific event data based on category
	switch collectorEvent.Category {
	case CategoryNetwork:
		if networkData := c.extractNetworkData(collectorEvent); networkData != nil {
			builder = builder.WithNetworkData(networkData)
		}
	case CategoryMemory:
		if memoryData := c.extractMemoryData(collectorEvent); memoryData != nil {
			builder = builder.WithMemoryData(memoryData)
		}
	// Add more category-specific conversions as needed
	}
	
	return builder.Build(), nil
}

// convertCategory converts collector category to gRPC category
func (c *GRPCStreamingClient) convertCategory(category Category) events.EventCategory {
	switch category {
	case CategoryNetwork:
		return events.EventCategory_CATEGORY_NETWORK
	case CategoryMemory:
		return events.EventCategory_CATEGORY_MEMORY
	case CategoryCPU:
		return events.EventCategory_CATEGORY_CPU
	case CategoryDisk:
		return events.EventCategory_CATEGORY_STORAGE
	case CategoryProcess:
		return events.EventCategory_CATEGORY_PROCESS
	case CategoryKubernetes:
		return events.EventCategory_CATEGORY_KUBERNETES
	case CategorySecurity:
		return events.EventCategory_CATEGORY_SECURITY
	case CategoryApplication:
		return events.EventCategory_CATEGORY_APPLICATION
	case CategorySystem:
		return events.EventCategory_CATEGORY_SYSTEM
	default:
		return events.EventCategory_CATEGORY_UNKNOWN
	}
}

// convertSeverity converts collector severity to gRPC severity
func (c *GRPCStreamingClient) convertSeverity(severity Severity) events.EventSeverity {
	switch severity {
	case SeverityCritical:
		return events.EventSeverity_SEVERITY_CRITICAL
	case SeverityHigh:
		return events.EventSeverity_SEVERITY_HIGH
	case SeverityMedium:
		return events.EventSeverity_SEVERITY_MEDIUM
	case SeverityLow:
		return events.EventSeverity_SEVERITY_LOW
	case SeverityDebug:
		return events.EventSeverity_SEVERITY_DEBUG
	default:
		return events.EventSeverity_SEVERITY_INFO
	}
}

// determineEntityType determines the entity type from event context
func (c *GRPCStreamingClient) determineEntityType(ctx *EventContext) events.EntityType {
	if ctx.Pod != "" {
		return events.EntityType_ENTITY_POD
	}
	if ctx.Container != "" {
		return events.EntityType_ENTITY_CONTAINER
	}
	if ctx.Node != "" {
		return events.EntityType_ENTITY_NODE
	}
	if ctx.Service != "" {
		return events.EntityType_ENTITY_SERVICE
	}
	if ctx.PID > 0 {
		return events.EntityType_ENTITY_PROCESS
	}
	return events.EntityType_ENTITY_UNKNOWN
}

// determineEntityID determines the entity ID from event context
func (c *GRPCStreamingClient) determineEntityID(ctx *EventContext) string {
	if ctx.Pod != "" {
		return ctx.Pod
	}
	if ctx.Container != "" {
		return ctx.Container
	}
	if ctx.Node != "" {
		return ctx.Node
	}
	if ctx.Service != "" {
		return ctx.Service
	}
	if ctx.PID > 0 {
		return fmt.Sprintf("%d", ctx.PID)
	}
	return "unknown"
}

// determineEntityName determines the entity name from event context
func (c *GRPCStreamingClient) determineEntityName(ctx *EventContext) string {
	if ctx.ProcessName != "" {
		return ctx.ProcessName
	}
	return c.determineEntityID(ctx)
}

// extractNetworkData extracts network event data
func (c *GRPCStreamingClient) extractNetworkData(event *Event) *events.NetworkEvent {
	if event.Context == nil {
		return nil
	}
	
	return &events.NetworkEvent{
		Protocol:      event.Context.Protocol,
		SrcIp:        event.Context.SrcIP,
		SrcPort:      uint32(event.Context.SrcPort),
		DstIp:        event.Context.DstIP,
		DstPort:      uint32(event.Context.DstPort),
		BytesSent:    c.extractDataField(event, "bytes_sent", uint64(0)).(uint64),
		BytesReceived: c.extractDataField(event, "bytes_received", uint64(0)).(uint64),
		State:        c.extractDataField(event, "state", "UNKNOWN").(string),
	}
}

// extractMemoryData extracts memory event data
func (c *GRPCStreamingClient) extractMemoryData(event *Event) *events.MemoryEvent {
	return &events.MemoryEvent{
		MemoryUsage:  c.extractDataField(event, "memory_usage", uint64(0)).(uint64),
		MemoryLimit:  c.extractDataField(event, "memory_limit", uint64(0)).(uint64),
		MemoryPressure: c.extractDataField(event, "memory_pressure", float32(0.0)).(float32),
		OomScore:     c.extractDataField(event, "oom_score", int32(0)).(int32),
		PageFaults:   c.extractDataField(event, "page_faults", uint64(0)).(uint64),
	}
}

// extractDataField extracts a field from event data with a default value
func (c *GRPCStreamingClient) extractDataField(event *Event, key string, defaultValue interface{}) interface{} {
	if value, exists := event.Data[key]; exists {
		return value
	}
	return defaultValue
}

// monitorConnection monitors the gRPC connection status
func (c *GRPCStreamingClient) monitorConnection() {
	defer c.wg.Done()
	
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			stats := c.client.GetStats()
			wasConnected := c.connected.Load()
			isConnected := stats.Connected
			
			c.connected.Store(isConnected)
			
			// Track reconnections
			if !wasConnected && isConnected {
				atomic.AddUint64(&c.reconnections, 1)
			}
		}
	}
}

// ClientStats provides statistics for the gRPC streaming client
type ClientStats struct {
	Connected       bool    `json:"connected"`
	EventsSent      uint64  `json:"events_sent"`
	BatchesSent     uint64  `json:"batches_sent"`
	SendErrors      uint64  `json:"send_errors"`
	Reconnections   uint64  `json:"reconnections"`
	EventsPerSecond float64 `json:"events_per_second"`
	QueueLength     int     `json:"queue_length"`
	QueueCapacity   int     `json:"queue_capacity"`
}

// EventHandler handles events and sends them to the gRPC server
type EventHandler struct {
	client *GRPCStreamingClient
}

// NewEventHandler creates a new event handler
func NewEventHandler(client *GRPCStreamingClient) EventHandler {
	return &EventHandler{
		client: client,
	}
}

// HandleEvent processes a single event
func (h *EventHandler) HandleEvent(ctx context.Context, event *Event) error {
	return h.client.SendEvent(event)
}

// HandleBatch processes a batch of events
func (h *EventHandler) HandleBatch(ctx context.Context, events []*Event) error {
	return h.client.SendBatch(events)
}