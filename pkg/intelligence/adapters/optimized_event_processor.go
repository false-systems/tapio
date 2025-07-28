package adapters

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// OptimizedEventProcessor provides efficient event processing with channel merging and exponential backoff
type OptimizedEventProcessor struct {
	// Input channels
	networkEvents chan *domain.UnifiedEvent
	memoryEvents  chan *domain.UnifiedEvent
	processEvents chan *domain.UnifiedEvent
	fileEvents    chan *domain.UnifiedEvent

	// Merged output
	mergedEvents chan *domain.UnifiedEvent

	// Processing
	engine interfaces.CorrelationEngine

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Reconnection with exponential backoff
	reconnectConfig ReconnectConfig

	// Metrics
	processedCount uint64
	errorCount     uint64
}

// ReconnectConfig configures exponential backoff for reconnection
type ReconnectConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	MaxRetries   int
	JitterFactor float64
}

// NewOptimizedEventProcessor creates a new optimized event processor
func NewOptimizedEventProcessor(engine interfaces.CorrelationEngine) *OptimizedEventProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &OptimizedEventProcessor{
		networkEvents: make(chan *domain.UnifiedEvent, 1000),
		memoryEvents:  make(chan *domain.UnifiedEvent, 1000),
		processEvents: make(chan *domain.UnifiedEvent, 1000),
		fileEvents:    make(chan *domain.UnifiedEvent, 100),
		mergedEvents:  make(chan *domain.UnifiedEvent, 5000),
		engine:        engine,
		ctx:           ctx,
		cancel:        cancel,
		reconnectConfig: ReconnectConfig{
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     30 * time.Second,
			Multiplier:   2.0,
			MaxRetries:   10,
			JitterFactor: 0.1,
		},
	}
}

// Start begins processing events
func (oep *OptimizedEventProcessor) Start() error {
	if err := oep.engine.Start(); err != nil {
		return err
	}

	// Start channel merger
	oep.wg.Add(1)
	go oep.channelMerger()

	// Start event processor
	oep.wg.Add(1)
	go oep.eventProcessor()

	return nil
}

// Stop gracefully shuts down the processor
func (oep *OptimizedEventProcessor) Stop() error {
	oep.cancel()

	// Close input channels
	close(oep.networkEvents)
	close(oep.memoryEvents)
	close(oep.processEvents)
	close(oep.fileEvents)

	// Wait for workers to finish
	oep.wg.Wait()

	// Close merged channel
	close(oep.mergedEvents)

	return oep.engine.Stop()
}

// channelMerger efficiently merges multiple input channels using select
func (oep *OptimizedEventProcessor) channelMerger() {
	defer oep.wg.Done()

	// Track which channels are still open
	networkOpen := true
	memoryOpen := true
	processOpen := true
	fileOpen := true

	for networkOpen || memoryOpen || processOpen || fileOpen {
		select {
		case event, ok := <-oep.networkEvents:
			if !ok {
				networkOpen = false
				continue
			}
			oep.sendToMerged(event)

		case event, ok := <-oep.memoryEvents:
			if !ok {
				memoryOpen = false
				continue
			}
			oep.sendToMerged(event)

		case event, ok := <-oep.processEvents:
			if !ok {
				processOpen = false
				continue
			}
			oep.sendToMerged(event)

		case event, ok := <-oep.fileEvents:
			if !ok {
				fileOpen = false
				continue
			}
			oep.sendToMerged(event)

		case <-oep.ctx.Done():
			return
		}
	}
}

// sendToMerged sends event to merged channel with non-blocking write
func (oep *OptimizedEventProcessor) sendToMerged(event *domain.UnifiedEvent) {
	select {
	case oep.mergedEvents <- event:
		// Successfully sent
	default:
		// Channel full, apply backpressure or drop
		// In production, this would trigger backpressure mechanisms
	}
}

// eventProcessor processes events from the merged channel
func (oep *OptimizedEventProcessor) eventProcessor() {
	defer oep.wg.Done()

	for {
		select {
		case event := <-oep.mergedEvents:
			if event == nil {
				continue
			}

			// Process with retry and exponential backoff
			err := oep.processEventWithRetry(event)
			if err != nil {
				oep.errorCount++
			} else {
				oep.processedCount++
			}

		case <-oep.ctx.Done():
			// Process remaining events before shutdown
			oep.drainEvents()
			return
		}
	}
}

// processEventWithRetry processes an event with exponential backoff retry
func (oep *OptimizedEventProcessor) processEventWithRetry(event *domain.UnifiedEvent) error {
	delay := oep.reconnectConfig.InitialDelay

	for attempt := 0; attempt <= oep.reconnectConfig.MaxRetries; attempt++ {
		// Try to process the event
		ctx, cancel := context.WithTimeout(oep.ctx, 5*time.Second)
		err := oep.engine.ProcessEvent(ctx, event)
		cancel()

		if err == nil {
			return nil
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			return err
		}

		// Check if we've exhausted retries
		if attempt == oep.reconnectConfig.MaxRetries {
			return errors.New("max retries exceeded")
		}

		// Apply exponential backoff with jitter
		jitter := oep.calculateJitter(delay)
		sleepDuration := delay + jitter

		select {
		case <-time.After(sleepDuration):
			// Continue to next retry
		case <-oep.ctx.Done():
			return errors.New("context cancelled during retry")
		}

		// Increase delay for next attempt
		delay = time.Duration(float64(delay) * oep.reconnectConfig.Multiplier)
		if delay > oep.reconnectConfig.MaxDelay {
			delay = oep.reconnectConfig.MaxDelay
		}
	}

	return errors.New("unexpected retry loop exit")
}

// calculateJitter adds randomized jitter to prevent thundering herd
func (oep *OptimizedEventProcessor) calculateJitter(delay time.Duration) time.Duration {
	if oep.reconnectConfig.JitterFactor <= 0 {
		return 0
	}

	// Use timestamp for simple randomization
	jitterRange := float64(delay) * oep.reconnectConfig.JitterFactor
	jitter := (float64(time.Now().UnixNano()%1000) / 1000.0) * jitterRange

	return time.Duration(jitter)
}

// drainEvents processes remaining events during shutdown
func (oep *OptimizedEventProcessor) drainEvents() {
	deadline := time.Now().Add(10 * time.Second) // 10 second drain timeout

	for time.Now().Before(deadline) {
		select {
		case event := <-oep.mergedEvents:
			if event == nil {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = oep.engine.ProcessEvent(ctx, event)
			cancel()

		default:
			return
		}
	}
}

// AddNetworkEvent adds a network event
func (oep *OptimizedEventProcessor) AddNetworkEvent(event *domain.UnifiedEvent) error {
	select {
	case oep.networkEvents <- event:
		return nil
	case <-oep.ctx.Done():
		return errors.New("processor is shutting down")
	default:
		return errors.New("network event channel full")
	}
}

// AddMemoryEvent adds a memory event
func (oep *OptimizedEventProcessor) AddMemoryEvent(event *domain.UnifiedEvent) error {
	select {
	case oep.memoryEvents <- event:
		return nil
	case <-oep.ctx.Done():
		return errors.New("processor is shutting down")
	default:
		return errors.New("memory event channel full")
	}
}

// AddProcessEvent adds a process event
func (oep *OptimizedEventProcessor) AddProcessEvent(event *domain.UnifiedEvent) error {
	select {
	case oep.processEvents <- event:
		return nil
	case <-oep.ctx.Done():
		return errors.New("processor is shutting down")
	default:
		return errors.New("process event channel full")
	}
}

// AddFileEvent adds a file event
func (oep *OptimizedEventProcessor) AddFileEvent(event *domain.UnifiedEvent) error {
	select {
	case oep.fileEvents <- event:
		return nil
	case <-oep.ctx.Done():
		return errors.New("processor is shutting down")
	default:
		return errors.New("file event channel full")
	}
}

// GetMetrics returns processor metrics
func (oep *OptimizedEventProcessor) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"events_processed": oep.processedCount,
		"errors":           oep.errorCount,
		"network_queue":    len(oep.networkEvents),
		"memory_queue":     len(oep.memoryEvents),
		"process_queue":    len(oep.processEvents),
		"file_queue":       len(oep.fileEvents),
		"merged_queue":     len(oep.mergedEvents),
	}
}

// Helper functions

func isRetryableError(err error) bool {
	// Define which errors are retryable
	// In production, this would check specific error types

	if err == nil {
		return false
	}

	// Timeout errors are retryable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Connection errors are retryable
	errStr := err.Error()
	retryableStrings := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"temporary failure",
		"timeout",
	}

	for _, s := range retryableStrings {
		if contains(errStr, s) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsAt(s, substr, 0)
}

func containsAt(s, substr string, start int) bool {
	if start+len(substr) > len(s) {
		return false
	}

	for i := 0; i < len(substr); i++ {
		if s[start+i] != substr[i] {
			return false
		}
	}

	return true
}

// PrioritizedEventProcessor extends OptimizedEventProcessor with priority-based processing
type PrioritizedEventProcessor struct {
	*OptimizedEventProcessor

	// Priority queues
	criticalEvents chan *domain.UnifiedEvent
	highEvents     chan *domain.UnifiedEvent
	normalEvents   chan *domain.UnifiedEvent
	lowEvents      chan *domain.UnifiedEvent
}

// NewPrioritizedEventProcessor creates a processor with priority-based channel selection
func NewPrioritizedEventProcessor(engine interfaces.CorrelationEngine) *PrioritizedEventProcessor {
	return &PrioritizedEventProcessor{
		OptimizedEventProcessor: NewOptimizedEventProcessor(engine),
		criticalEvents:          make(chan *domain.UnifiedEvent, 100),
		highEvents:              make(chan *domain.UnifiedEvent, 500),
		normalEvents:            make(chan *domain.UnifiedEvent, 1000),
		lowEvents:               make(chan *domain.UnifiedEvent, 2000),
	}
}

// priorityChannelMerger merges channels with priority order
func (pep *PrioritizedEventProcessor) priorityChannelMerger() {
	defer pep.wg.Done()

	for {
		// Try critical events first
		select {
		case event := <-pep.criticalEvents:
			pep.sendToMerged(event)
			continue
		default:
		}

		// Then high priority
		select {
		case event := <-pep.highEvents:
			pep.sendToMerged(event)
			continue
		default:
		}

		// Then normal priority
		select {
		case event := <-pep.normalEvents:
			pep.sendToMerged(event)
			continue
		default:
		}

		// Finally low priority
		select {
		case event := <-pep.lowEvents:
			pep.sendToMerged(event)
		case <-pep.ctx.Done():
			return
		default:
			// No events available, sleep briefly
			time.Sleep(time.Microsecond)
		}
	}
}

// WithExponentialBackoff configures exponential backoff settings
func WithExponentialBackoff(config ReconnectConfig) func(*OptimizedEventProcessor) {
	return func(oep *OptimizedEventProcessor) {
		oep.reconnectConfig = config
	}
}
