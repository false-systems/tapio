package performance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Example: How to integrate the ring buffer with your gRPC services

// HighThroughputEventService shows how to use the ring buffer for event streaming
type HighThroughputEventService struct {
	eventBuffer *EventBatchBuffer
	processors  int
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewHighThroughputEventService creates a service that can handle 165k+ events/sec
func NewHighThroughputEventService(bufferSize uint64, processors int) (*HighThroughputEventService, error) {
	buffer, err := NewEventBatchBuffer(bufferSize)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &HighThroughputEventService{
		eventBuffer: buffer,
		processors:  processors,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Start begins processing events
func (s *HighThroughputEventService) Start() {
	// Start multiple processors for parallel event handling
	for i := 0; i < s.processors; i++ {
		s.wg.Add(1)
		go s.processEvents(i)
	}
}

// Stop gracefully shuts down the service
func (s *HighThroughputEventService) Stop() {
	s.cancel()
	s.wg.Wait()
}

// SubmitEvent adds an event to the processing pipeline
func (s *HighThroughputEventService) SubmitEvent(event *domain.UnifiedEvent) error {
	// Try non-blocking first for better performance
	if s.eventBuffer.buffer.TryPut(event) {
		return nil
	}

	// Fall back to blocking if buffer is temporarily full
	return s.eventBuffer.buffer.Put(event)
}

// processEvents is the worker that processes events from the ring buffer
func (s *HighThroughputEventService) processEvents(workerID int) {
	defer s.wg.Done()

	batchSize := 100
	events := make([]*domain.UnifiedEvent, batchSize)

	for {
		select {
		case <-s.ctx.Done():
			// Drain remaining events before shutdown
			for !s.eventBuffer.buffer.IsEmpty() {
				count := s.eventBuffer.DrainTo(events)
				s.processBatch(events[:count])
			}
			return
		default:
			// Get batch of events
			count := s.eventBuffer.DrainTo(events)
			if count > 0 {
				s.processBatch(events[:count])
			} else {
				// No events, brief sleep to prevent spinning
				time.Sleep(time.Microsecond)
			}
		}
	}
}

// processBatch handles a batch of events
func (s *HighThroughputEventService) processBatch(events []*domain.UnifiedEvent) {
	// Your processing logic here
	// This runs in parallel across multiple workers
	for _, event := range events {
		// Process event (correlation, storage, etc.)
		_ = event
	}
}

// Example: Integration with gRPC streaming
type StreamingExample struct {
	buffer *EventBuffer
}

// StreamEvents shows how to use the buffer with gRPC streaming
func (se *StreamingExample) StreamEvents(stream EventStream) error {
	// Create a reasonably sized buffer for streaming
	buffer, err := NewEventBuffer(65536) // 64k events
	if err != nil {
		return err
	}

	// Producer goroutine (receives from collectors)
	go func() {
		// Simulate receiving events from collectors
		for {
			event := &domain.UnifiedEvent{
				ID:        domain.GenerateEventID(),
				Timestamp: time.Now(),
				Type:      "example",
			}
			buffer.TryPut(event) // Non-blocking put
		}
	}()

	// Consumer loop (sends to gRPC stream)
	for {
		event, err := buffer.Get()
		if err != nil {
			// Buffer empty, wait a bit
			time.Sleep(time.Millisecond)
			continue
		}

		// Send to gRPC stream
		if err := stream.Send(event); err != nil {
			return err
		}
	}
}

// EventStream is a simplified interface for the example
type EventStream interface {
	Send(*domain.UnifiedEvent) error
}

// Example: Using with collectors
func ExampleCollectorIntegration() {
	// Each collector can have its own buffer
	ebpfBuffer, _ := NewEventBuffer(131072) // 128k for high-volume eBPF
	k8sBuffer, _ := NewEventBuffer(16384)   // 16k for K8s events
	appBuffer, _ := NewEventBuffer(32768)   // 32k for app logs

	// Collectors write to their buffers
	// Processing service reads from all buffers

	fmt.Printf("eBPF buffer capacity: %d\n", ebpfBuffer.Capacity())
	fmt.Printf("K8s buffer capacity: %d\n", k8sBuffer.Capacity())
	fmt.Printf("App buffer capacity: %d\n", appBuffer.Capacity())
}

// Example: Monitoring buffer health
func ExampleBufferMonitoring(buffer *EventBuffer) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := buffer.GetStats()

		// Alert if buffer is getting full
		fillPercent := float64(stats.Size) / float64(stats.Capacity) * 100
		if fillPercent > 80 {
			fmt.Printf("WARNING: Buffer at %.1f%% capacity\n", fillPercent)
		}

		// Log stats
		fmt.Printf("Buffer stats: size=%d, capacity=%d, full=%v\n",
			stats.Size, stats.Capacity, stats.IsFull)
	}
}
