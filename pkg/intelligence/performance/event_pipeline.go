package performance

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// EventPipeline is a high-performance event processing pipeline
type EventPipeline struct {
	// Pipeline stages
	stages     []Stage
	stageCount int

	// Ring buffers between stages
	buffers []*RingBuffer

	// Worker pools
	workers     [][]worker
	workerCount []int

	// Event pools
	eventPool *TypedPool[Event]

	// Metrics
	processed  atomic.Uint64
	dropped    atomic.Uint64
	latencyNs  atomic.Uint64
	throughput atomic.Uint64

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config PipelineConfig
}

// Event represents a pipeline event
type Event struct {
	ID        uint64
	Type      string
	Timestamp int64
	Priority  uint8
	Data      unsafe.Pointer
	Size      int
	Stage     int
	Metadata  [8]uint64 // Fixed size metadata
}

// Stage represents a processing stage
type Stage interface {
	Name() string
	Process(ctx context.Context, event *Event) (*Event, error)
	CanProcess(event *Event) bool
}

// worker represents a stage worker
type worker struct {
	id        int
	stage     Stage
	input     *RingBuffer
	output    *RingBuffer
	eventPool *TypedPool[Event]
	processed atomic.Uint64
	errors    atomic.Uint64
}

// PipelineConfig configures the event pipeline
type PipelineConfig struct {
	// Buffer sizes (must be power of 2)
	BufferSize uint64

	// Worker configuration
	WorkersPerStage int
	UseAffinity     bool

	// Performance settings
	BatchSize  int
	MaxLatency time.Duration

	// Memory settings
	EventPoolSize  int
	EnableZeroCopy bool
}

// DefaultPipelineConfig returns default configuration
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		BufferSize:      65536,
		WorkersPerStage: runtime.NumCPU(),
		UseAffinity:     true,
		BatchSize:       100,
		MaxLatency:      100 * time.Microsecond,
		EventPoolSize:   100000,
		EnableZeroCopy:  true,
	}
}

// NewEventPipeline creates a new event pipeline
func NewEventPipeline(stages []Stage, config PipelineConfig) (*EventPipeline, error) {
	if len(stages) == 0 {
		return nil, fmt.Errorf("at least one stage required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create event pool
	eventPool, err := NewTypedPool[Event](
		func() *Event { return &Event{} },
		func(e *Event) { *e = Event{} },
		config.EventPoolSize/runtime.NumCPU(),
		config.EventPoolSize,
	)
	if err != nil {
		cancel()
		return nil, err
	}

	// Create ring buffers between stages
	buffers := make([]*RingBuffer, len(stages)+1)
	for i := range buffers {
		buffers[i], err = NewRingBuffer(config.BufferSize)
		if err != nil {
			cancel()
			return nil, err
		}
	}

	// Create workers
	workers := make([][]worker, len(stages))
	workerCount := make([]int, len(stages))

	for i, stage := range stages {
		workerCount[i] = config.WorkersPerStage
		workers[i] = make([]worker, workerCount[i])

		for j := range workers[i] {
			workers[i][j] = worker{
				id:        j,
				stage:     stage,
				input:     buffers[i],
				output:    buffers[i+1],
				eventPool: eventPool,
			}
		}
	}

	return &EventPipeline{
		stages:      stages,
		stageCount:  len(stages),
		buffers:     buffers,
		workers:     workers,
		workerCount: workerCount,
		eventPool:   eventPool,
		ctx:         ctx,
		cancel:      cancel,
		config:      config,
	}, nil
}

// Start starts the pipeline
func (p *EventPipeline) Start() error {
	// Start workers for each stage
	for stageIdx, stageWorkers := range p.workers {
		for workerIdx, worker := range stageWorkers {
			p.wg.Add(1)

			// Copy values for closure
			w := worker
			stage := stageIdx
			wid := workerIdx

			go func() {
				defer p.wg.Done()

				// Set CPU affinity if enabled
				if p.config.UseAffinity {
					setCPUAffinity(stage*p.config.WorkersPerStage + wid)
				}

				p.runWorker(&w)
			}()
		}
	}

	// Start metrics collector
	p.wg.Add(1)
	go p.collectMetrics()

	return nil
}

// Stop stops the pipeline
func (p *EventPipeline) Stop() error {
	p.cancel()
	p.wg.Wait()
	return nil
}

// Submit submits an event to the pipeline
func (p *EventPipeline) Submit(event *Event) error {
	// Record start time
	event.Timestamp = time.Now().UnixNano()

	// Submit to first buffer
	ptr := unsafe.Pointer(event)
	if err := p.buffers[0].Put(ptr); err != nil {
		p.dropped.Add(1)
		return err
	}

	return nil
}

// SubmitBatch submits multiple events
func (p *EventPipeline) SubmitBatch(events []*Event) error {
	timestamp := time.Now().UnixNano()
	ptrs := make([]unsafe.Pointer, len(events))

	for i, event := range events {
		event.Timestamp = timestamp
		ptrs[i] = unsafe.Pointer(event)
	}

	// Use batch put for better performance
	added := p.buffers[0].PutBatch(ptrs)

	// Track dropped events
	dropped := len(ptrs) - added
	if dropped > 0 {
		p.dropped.Add(uint64(dropped))
		return fmt.Errorf("failed to submit %d events", dropped)
	}

	return nil
}

// GetEvent gets an event from the pool
func (p *EventPipeline) GetEvent() *Event {
	return p.eventPool.Get()
}

// PutEvent returns an event to the pool
func (p *EventPipeline) PutEvent(event *Event) {
	p.eventPool.Put(event)
}

// GetOutput retrieves processed events from the final stage
func (p *EventPipeline) GetOutput() (*Event, error) {
	ptr, err := p.buffers[len(p.buffers)-1].Get()
	if err != nil {
		return nil, err
	}

	return (*Event)(ptr), nil
}

// GetOutputBatch retrieves multiple processed events
func (p *EventPipeline) GetOutputBatch(events []*Event) int {
	// Create a slice of unsafe pointers
	ptrs := make([]unsafe.Pointer, len(events))

	// Use batch get for better performance
	count := p.buffers[len(p.buffers)-1].GetBatch(ptrs)

	// Convert pointers back to events
	for i := 0; i < count; i++ {
		events[i] = (*Event)(ptrs[i])
	}

	return count
}

// runWorker runs a single worker
func (p *EventPipeline) runWorker(w *worker) {
	batch := make([]unsafe.Pointer, p.config.BatchSize)

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		// Try to get a batch
		count := 0
		for i := 0; i < p.config.BatchSize; i++ {
			if ptr, ok := w.input.TryGet(); ok {
				batch[count] = ptr
				count++
			} else {
				break
			}
		}

		// Process batch if we have events
		if count > 0 {
			p.processBatch(w, batch[:count])
		} else {
			// No events, sleep briefly
			time.Sleep(10 * time.Microsecond)
		}
	}
}

// processBatch processes a batch of events
func (p *EventPipeline) processBatch(w *worker, batch []unsafe.Pointer) {
	for _, ptr := range batch {
		event := (*Event)(ptr)

		// Check if stage can process this event
		if !w.stage.CanProcess(event) {
			// Skip this stage
			if err := w.output.Put(ptr); err != nil {
				p.dropped.Add(1)
				w.eventPool.Put(event)
			}
			continue
		}

		// Process event
		start := time.Now().UnixNano()
		processed, err := w.stage.Process(p.ctx, event)
		elapsed := time.Now().UnixNano() - start

		if err != nil {
			w.errors.Add(1)
			w.eventPool.Put(event)
			continue
		}

		if processed == nil {
			// Event filtered out
			w.eventPool.Put(event)
			continue
		}

		// Update metrics
		w.processed.Add(1)
		p.latencyNs.Add(uint64(elapsed))

		// Pass to next stage
		if err := w.output.Put(unsafe.Pointer(processed)); err != nil {
			p.dropped.Add(1)
			w.eventPool.Put(processed)
		}
	}
}

// collectMetrics collects pipeline metrics
func (p *EventPipeline) collectMetrics() {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastProcessed uint64

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			current := p.processed.Load()
			throughput := current - lastProcessed
			p.throughput.Store(throughput)
			lastProcessed = current
		}
	}
}

// GetMetrics returns pipeline metrics
func (p *EventPipeline) GetMetrics() PipelineMetrics {
	metrics := PipelineMetrics{
		Processed:    p.processed.Load(),
		Dropped:      p.dropped.Load(),
		Throughput:   p.throughput.Load(),
		AvgLatency:   time.Duration(0),
		StageMetrics: make([]StageMetrics, len(p.stages)),
	}

	// Calculate average latency
	if metrics.Processed > 0 {
		avgNs := p.latencyNs.Load() / metrics.Processed
		metrics.AvgLatency = time.Duration(avgNs)
	}

	// Collect stage metrics
	for i, stageWorkers := range p.workers {
		var stageProcessed, stageErrors uint64

		for _, worker := range stageWorkers {
			stageProcessed += worker.processed.Load()
			stageErrors += worker.errors.Load()
		}

		metrics.StageMetrics[i] = StageMetrics{
			Name:       p.stages[i].Name(),
			Processed:  stageProcessed,
			Errors:     stageErrors,
			BufferSize: p.buffers[i].Size(),
		}
	}

	// Add final buffer size
	metrics.OutputBuffer = p.buffers[len(p.buffers)-1].Size()

	return metrics
}

// PipelineMetrics contains pipeline metrics
type PipelineMetrics struct {
	Processed    uint64
	Dropped      uint64
	Throughput   uint64
	AvgLatency   time.Duration
	StageMetrics []StageMetrics
	OutputBuffer uint64
}

// StageMetrics contains metrics for a single stage
type StageMetrics struct {
	Name       string
	Processed  uint64
	Errors     uint64
	BufferSize uint64
}

// Helper function to set CPU affinity (platform specific)
func setCPUAffinity(cpu int) {
	// This would be implemented using platform-specific syscalls
	// For now, it's a no-op
}

// PassthroughStage is a simple stage that passes events through
type PassthroughStage struct {
	name string
}

func NewPassthroughStage(name string) *PassthroughStage {
	return &PassthroughStage{name: name}
}

func (s *PassthroughStage) Name() string {
	return s.name
}

func (s *PassthroughStage) Process(ctx context.Context, event *Event) (*Event, error) {
	return event, nil
}

func (s *PassthroughStage) CanProcess(event *Event) bool {
	return true
}

// FilterStage filters events based on a predicate
type FilterStage struct {
	name      string
	predicate func(*Event) bool
}

func NewFilterStage(name string, predicate func(*Event) bool) *FilterStage {
	return &FilterStage{
		name:      name,
		predicate: predicate,
	}
}

func (s *FilterStage) Name() string {
	return s.name
}

func (s *FilterStage) Process(ctx context.Context, event *Event) (*Event, error) {
	if s.predicate(event) {
		return event, nil
	}
	return nil, nil // Filter out
}

func (s *FilterStage) CanProcess(event *Event) bool {
	return true
}

// TransformStage transforms events
type TransformStage struct {
	name      string
	transform func(*Event) error
}

func NewTransformStage(name string, transform func(*Event) error) *TransformStage {
	return &TransformStage{
		name:      name,
		transform: transform,
	}
}

func (s *TransformStage) Name() string {
	return s.name
}

func (s *TransformStage) Process(ctx context.Context, event *Event) (*Event, error) {
	if err := s.transform(event); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *TransformStage) CanProcess(event *Event) bool {
	return true
}
