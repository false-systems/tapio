package performance

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Example: Complete integration showing how to use performance components
// in your gRPC services and collectors

// PerformantEventService shows how to combine all performance components
type PerformantEventService struct {
	// High-performance event buffer
	eventBuffer *EventBatchBuffer

	// Object pool for event reuse
	eventPool *UnifiedEventPool

	// Byte slice pool for raw data
	bytePool *ByteSlicePool

	// Processing context
	ctx    context.Context
	cancel context.CancelFunc
}

// Example 1: High-performance eBPF collector
func ExampleEBPFCollector() {
	// Create performance components
	buffer, _ := NewEventBuffer(131072) // 128k events
	eventPool := NewUnifiedEventPool()
	bytePool := NewByteSlicePool()

	// Simulate eBPF event collection
	for i := 0; i < 1000; i++ {
		// Get event from pool instead of allocating
		event := eventPool.Get()

		// Fill event data
		event.ID = domain.GenerateEventID()
		event.Timestamp = time.Now()
		event.Type = "syscall"
		event.Source = "ebpf"

		// Reuse byte slice for raw data
		rawData := bytePool.Get(256)
		copy(rawData, []byte("raw eBPF data here"))
		event.RawData = rawData

		// Add kernel-specific data
		event.Kernel = &domain.KernelData{
			Syscall:    "open",
			PID:        1234,
			ReturnCode: 0,
		}

		// Put event in buffer
		if err := buffer.Put(event); err != nil {
			// Buffer full, return event to pool
			bytePool.Put(event.RawData)
			eventPool.Put(event)
		}
	}

	// Consumer side
	for !buffer.IsEmpty() {
		event, _ := buffer.Get()

		// Process event...
		fmt.Printf("Processing event: %s\n", event.ID)

		// Return resources to pools when done
		if event.RawData != nil {
			bytePool.Put(event.RawData)
		}
		eventPool.Put(event)
	}

	// Check pool efficiency
	stats := eventPool.GetStats()
	fmt.Printf("Event pool stats: allocated=%d, recycled=%d, in_use=%d\n",
		stats.Allocated, stats.Recycled, stats.InUse)
}

// Example 2: Integration with gRPC EventService
type OptimizedEventService struct {
	buffer    *EventBatchBuffer
	eventPool *UnifiedEventPool
}

func (s *OptimizedEventService) StreamEvents(req *StreamRequest, stream EventStream) error {
	// Process events in batches for efficiency
	batchSize := 100
	events := make([]*domain.UnifiedEvent, batchSize)

	ticker := time.NewTicker(10 * time.Millisecond) // 100Hz batch rate
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get batch of events
			count := s.buffer.DrainTo(events)
			if count == 0 {
				continue
			}

			// Send batch to stream
			for i := 0; i < count; i++ {
				if err := stream.Send(events[i]); err != nil {
					// Return events to pool on error
					for j := i; j < count; j++ {
						s.eventPool.Put(events[j])
					}
					return err
				}

				// Return sent event to pool
				s.eventPool.Put(events[i])
			}

		case <-req.Context.Done():
			return nil
		}
	}
}

// Example 3: Correlation engine with performance optimizations
type OptimizedCorrelationEngine struct {
	// Ring buffers for different event priorities
	criticalBuffer *EventBuffer
	normalBuffer   *EventBuffer

	// Event pool
	eventPool *UnifiedEventPool
}

func (ce *OptimizedCorrelationEngine) ProcessEvent(event *domain.UnifiedEvent) error {
	// Route events based on severity
	if event.GetSeverity() == "critical" {
		return ce.criticalBuffer.Put(event)
	}
	return ce.normalBuffer.Put(event)
}

func (ce *OptimizedCorrelationEngine) correlationWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Process critical events first
			if event, err := ce.criticalBuffer.Get(); err == nil {
				ce.correlateEvent(event)
				ce.eventPool.Put(event)
				continue
			}

			// Then normal events
			if event, err := ce.normalBuffer.Get(); err == nil {
				ce.correlateEvent(event)
				ce.eventPool.Put(event)
				continue
			}

			// No events, brief sleep
			time.Sleep(time.Microsecond)
		}
	}
}

func (ce *OptimizedCorrelationEngine) correlateEvent(event *domain.UnifiedEvent) {
	// Correlation logic here
	log.Printf("Correlating event: %s", event.ID)
}

// Example 4: Complete service setup
func SetupOptimizedService() (*PerformantEventService, error) {
	// Create all performance components
	buffer, err := NewEventBatchBuffer(65536)
	if err != nil {
		return nil, err
	}

	eventPool := NewUnifiedEventPool()
	bytePool := NewByteSlicePool()

	ctx, cancel := context.WithCancel(context.Background())

	service := &PerformantEventService{
		eventBuffer: buffer,
		eventPool:   eventPool,
		bytePool:    bytePool,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start background workers
	numWorkers := 4
	for i := 0; i < numWorkers; i++ {
		go service.processWorker(i)
	}

	return service, nil
}

func (s *PerformantEventService) processWorker(id int) {
	log.Printf("Worker %d started", id)

	// Process events in batches
	batchSize := 50
	events := make([]*domain.UnifiedEvent, batchSize)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// Get batch
			count := s.eventBuffer.DrainTo(events)
			if count == 0 {
				time.Sleep(time.Millisecond)
				continue
			}

			// Process batch
			for i := 0; i < count; i++ {
				// Your processing logic here
				_ = events[i]

				// Return to pool
				if events[i].RawData != nil {
					s.bytePool.Put(events[i].RawData)
				}
				s.eventPool.Put(events[i])
			}
		}
	}
}

// Example 5: Performance monitoring
func MonitorPerformance(buffer *EventBuffer, pool *UnifiedEventPool) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Buffer stats
		bufferStats := buffer.GetStats()
		fillPercent := float64(bufferStats.Size) / float64(bufferStats.Capacity) * 100

		// Pool stats
		poolStats := pool.GetStats()
		reuseRate := float64(poolStats.Recycled) / float64(poolStats.Allocated) * 100

		log.Printf("Performance stats:")
		log.Printf("  Buffer: %d/%d (%.1f%% full)",
			bufferStats.Size, bufferStats.Capacity, fillPercent)
		log.Printf("  Pool: %d allocated, %d recycled (%.1f%% reuse rate)",
			poolStats.Allocated, poolStats.Recycled, reuseRate)
		log.Printf("  Pool: %d currently in use", poolStats.InUse)

		// Alert on issues
		if fillPercent > 80 {
			log.Printf("WARNING: Event buffer filling up!")
		}
		if reuseRate < 50 && poolStats.Allocated > 1000 {
			log.Printf("WARNING: Low object reuse rate!")
		}
	}
}

// Simplified interfaces for examples
type StreamRequest struct {
	Context context.Context
}
