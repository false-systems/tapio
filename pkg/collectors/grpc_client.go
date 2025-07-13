package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	
	"github.com/yairfalse/tapio/pkg/grpc"
)

// GRPCStreamingClient wraps the gRPC client for collector→server streaming
type GRPCStreamingClient struct {
	client       *grpc.Client
	stream       grpc.EventStream
	
	// Connection state
	connected    atomic.Bool
	reconnecting atomic.Bool
	mu           sync.RWMutex
	
	// Statistics
	eventsSent    uint64
	bytesStreamed uint64
	reconnects    uint64
	errors        uint64
	
	// Configuration
	maxRetries   int
	retryBackoff time.Duration
}

// EventStreamBatch represents a batch of events to stream
type EventStreamBatch struct {
	Events    []*Event
	Timestamp time.Time
	NodeID    string
}

// NewGRPCStreamingClient creates a new gRPC streaming client
func NewGRPCStreamingClient(client *grpc.Client) *GRPCStreamingClient {
	return &GRPCStreamingClient{
		client:       client,
		maxRetries:   10,
		retryBackoff: 100 * time.Millisecond,
	}
}

// Start establishes the streaming connection
func (c *GRPCStreamingClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.connected.Load() {
		return nil
	}
	
	// Establish streaming connection
	stream, err := c.client.CreateEventStream(ctx)
	if err != nil {
		return fmt.Errorf("failed to create event stream: %w", err)
	}
	
	c.stream = stream
	c.connected.Store(true)
	
	// Start connection monitor
	go c.monitorConnection(ctx)
	
	return nil
}

// Stop closes the streaming connection
func (c *GRPCStreamingClient) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.connected.Load() {
		return nil
	}
	
	if c.stream != nil {
		if err := c.stream.Close(); err != nil {
			return fmt.Errorf("failed to close stream: %w", err)
		}
	}
	
	c.connected.Store(false)
	return nil
}

// StreamEvents sends a batch of events
func (c *GRPCStreamingClient) StreamEvents(batch *EventStreamBatch) error {
	if !c.connected.Load() {
		return fmt.Errorf("not connected to server")
	}
	
	// Convert to gRPC format
	grpcBatch := c.convertToGRPCBatch(batch)
	
	// Send with retries
	err := c.sendWithRetries(grpcBatch)
	if err != nil {
		atomic.AddUint64(&c.errors, 1)
		return err
	}
	
	// Update statistics
	atomic.AddUint64(&c.eventsSent, uint64(len(batch.Events)))
	atomic.AddUint64(&c.bytesStreamed, uint64(estimateBatchSize(batch.Events)))
	
	return nil
}

// sendWithRetries sends events with exponential backoff retry
func (c *GRPCStreamingClient) sendWithRetries(batch *grpc.EventBatch) error {
	backoff := c.retryBackoff
	
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		err := c.stream.Send(batch)
		if err == nil {
			return nil
		}
		
		// Check if error is retryable
		if !isRetryableError(err) {
			return err
		}
		
		// If we're reconnecting, wait for that to complete
		if c.reconnecting.Load() {
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		
		// For connection errors, trigger reconnection
		if isConnectionError(err) {
			c.triggerReconnection()
		}
		
		time.Sleep(backoff)
		backoff *= 2
	}
	
	return fmt.Errorf("failed after %d retries", c.maxRetries)
}

// monitorConnection monitors the gRPC connection health
func (c *GRPCStreamingClient) monitorConnection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
			
		case <-ticker.C:
			if !c.isHealthy() {
				c.triggerReconnection()
			}
		}
	}
}

// triggerReconnection initiates a reconnection
func (c *GRPCStreamingClient) triggerReconnection() {
	if !c.reconnecting.CompareAndSwap(false, true) {
		return // Already reconnecting
	}
	
	go c.reconnect()
}

// reconnect attempts to reconnect to the server
func (c *GRPCStreamingClient) reconnect() {
	defer c.reconnecting.Store(false)
	
	atomic.AddUint64(&c.reconnects, 1)
	
	// Close existing stream
	c.mu.Lock()
	if c.stream != nil {
		c.stream.Close()
	}
	c.connected.Store(false)
	c.mu.Unlock()
	
	// Attempt reconnection with backoff
	backoff := c.retryBackoff
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		stream, err := c.client.CreateEventStream(ctx)
		cancel()
		
		if err == nil {
			c.mu.Lock()
			c.stream = stream
			c.connected.Store(true)
			c.mu.Unlock()
			return
		}
		
		time.Sleep(backoff)
		backoff *= 2
	}
}

// isHealthy checks if the connection is healthy
func (c *GRPCStreamingClient) isHealthy() bool {
	if !c.connected.Load() {
		return false
	}
	
	// Send a ping to check connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return c.client.Ping(ctx) == nil
}

// GetStats returns client statistics
func (c *GRPCStreamingClient) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"connected":       c.connected.Load(),
		"reconnecting":    c.reconnecting.Load(),
		"events_sent":     atomic.LoadUint64(&c.eventsSent),
		"bytes_streamed":  atomic.LoadUint64(&c.bytesStreamed),
		"reconnects":      atomic.LoadUint64(&c.reconnects),
		"errors":          atomic.LoadUint64(&c.errors),
		"events_per_sec":  c.calculateEventRate(),
	}
}

// calculateEventRate calculates events per second
func (c *GRPCStreamingClient) calculateEventRate() float64 {
	// TODO: Implement sliding window rate calculation
	return 0.0
}

// convertToGRPCBatch converts internal batch to gRPC format
func (c *GRPCStreamingClient) convertToGRPCBatch(batch *EventStreamBatch) *grpc.EventBatch {
	grpcEvents := make([]*grpc.Event, 0, len(batch.Events))
	
	for _, event := range batch.Events {
		grpcEvent := &grpc.Event{
			Id:        event.ID,
			Timestamp: event.Timestamp.UnixNano(),
			Type:      string(event.Type),
			Severity:  string(event.Severity),
			Source: &grpc.EventSource{
				Collector: event.Source.Collector,
				Component: event.Source.Component,
				Node:      event.Source.Node,
			},
			Data:     convertDataToGRPC(event.Data),
			Metadata: convertMetadataToGRPC(event.Metadata),
		}
		grpcEvents = append(grpcEvents, grpcEvent)
	}
	
	return &grpc.EventBatch{
		Events:    grpcEvents,
		NodeId:    batch.NodeID,
		Timestamp: batch.Timestamp.UnixNano(),
	}
}

// convertDataToGRPC converts event data to gRPC format
func convertDataToGRPC(data map[string]interface{}) map[string]*grpc.Value {
	result := make(map[string]*grpc.Value)
	
	for key, value := range data {
		result[key] = convertValueToGRPC(value)
	}
	
	return result
}

// convertValueToGRPC converts a single value to gRPC format
func convertValueToGRPC(value interface{}) *grpc.Value {
	switch v := value.(type) {
	case string:
		return &grpc.Value{Kind: &grpc.Value_StringValue{StringValue: v}}
	case float64:
		return &grpc.Value{Kind: &grpc.Value_NumberValue{NumberValue: v}}
	case bool:
		return &grpc.Value{Kind: &grpc.Value_BoolValue{BoolValue: v}}
	case map[string]interface{}:
		return &grpc.Value{Kind: &grpc.Value_StructValue{
			StructValue: &grpc.Struct{Fields: convertDataToGRPC(v)},
		}}
	case []interface{}:
		values := make([]*grpc.Value, len(v))
		for i, item := range v {
			values[i] = convertValueToGRPC(item)
		}
		return &grpc.Value{Kind: &grpc.Value_ListValue{
			ListValue: &grpc.ListValue{Values: values},
		}}
	default:
		// Convert to string as fallback
		return &grpc.Value{Kind: &grpc.Value_StringValue{
			StringValue: fmt.Sprintf("%v", value),
		}}
	}
}

// convertMetadataToGRPC converts event metadata to gRPC format
func convertMetadataToGRPC(metadata EventMetadata) *grpc.EventMetadata {
	return &grpc.EventMetadata{
		Importance:   float64(metadata.Importance),
		Reliability:  float64(metadata.Reliability),
		Correlations: metadata.Correlation,
	}
}

// Helper functions for error handling

func isRetryableError(err error) bool {
	// TODO: Implement proper error classification
	return true
}

func isConnectionError(err error) bool {
	// TODO: Implement proper connection error detection
	return false
}