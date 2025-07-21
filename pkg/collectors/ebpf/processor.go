package ebpf

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// DualPathProcessor handles both raw eBPF events (Hubble-style) and semantic processing
type DualPathProcessor struct {
	// Configuration
	config *ProcessorConfig

	// Components
	filterEngine *FilterEngine
	enricher     *EventEnricher
	correlationHandler func(*EnrichedEvent) // Injected correlation handler

	// Raw path (Hubble-style)
	rawEventBuffer chan *RawEvent
	rawEventSinks  []RawEventSink
	rawEventStore  RawEventStore

	// Semantic path (Tapio integration)
	semanticBuffer chan *EnrichedEvent
	semanticSinks  []SemanticEventSink
	tapioClient    TapioClient

	// Processing
	workers int
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Metrics
	mu                  sync.RWMutex
	rawEventsProcessed  uint64
	semanticEventsSent  uint64
	errorsCount         uint64
	lastProcessingTime  time.Time
	processingDurations map[string]time.Duration
}

// ProcessorConfig contains processor configuration
type ProcessorConfig struct {
	// Buffer sizes
	RawBufferSize      int `json:"raw_buffer_size"`
	SemanticBufferSize int `json:"semantic_buffer_size"`

	// Processing
	WorkerCount   int           `json:"worker_count"`
	BatchSize     int           `json:"batch_size"`
	FlushInterval time.Duration `json:"flush_interval"`

	// Raw path
	EnableRawPath      bool          `json:"enable_raw_path"`
	RawRetentionPeriod time.Duration `json:"raw_retention_period"`
	RawStorageBackend  string        `json:"raw_storage_backend"` // "memory", "disk", "s3"

	// Semantic path
	EnableSemanticPath bool   `json:"enable_semantic_path"`
	SemanticBatchSize  int    `json:"semantic_batch_size"`
	TapioServerAddr    string `json:"tapio_server_addr"`

	// Performance
	MaxMemoryUsage  int64         `json:"max_memory_usage"`
	EnableProfiling bool          `json:"enable_profiling"`
	MetricsInterval time.Duration `json:"metrics_interval"`
}

// Event sinks
type RawEventSink interface {
	Send(ctx context.Context, event *RawEvent) error
	SendBatch(ctx context.Context, events []*RawEvent) error
	Close() error
}

type SemanticEventSink interface {
	Send(ctx context.Context, event *domain.Event) error
	SendBatch(ctx context.Context, events []*domain.Event) error
	Close() error
}

// Storage interfaces
type RawEventStore interface {
	Store(ctx context.Context, event *RawEvent) error
	StoreBatch(ctx context.Context, events []*RawEvent) error
	Query(ctx context.Context, filter *EventFilter) ([]*RawEvent, error)
	GetByTimeRange(ctx context.Context, start, end time.Time) ([]*RawEvent, error)
	Delete(ctx context.Context, before time.Time) error
	Close() error
}

type TapioClient interface {
	SendEvent(ctx context.Context, event *domain.Event) error
	SendBatch(ctx context.Context, events []*domain.Event) error
	Subscribe(ctx context.Context, opts domain.SubscriptionOptions) (<-chan *domain.Event, error)
	Close() error
}

// NewDualPathProcessor creates a new dual-path processor
func NewDualPathProcessor(config *ProcessorConfig) *DualPathProcessor {
	if config == nil {
		config = DefaultProcessorConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	processor := &DualPathProcessor{
		config:              config,
		ctx:                 ctx,
		cancel:              cancel,
		workers:             config.WorkerCount,
		rawEventBuffer:      make(chan *RawEvent, config.RawBufferSize),
		semanticBuffer:      make(chan *EnrichedEvent, config.SemanticBufferSize),
		rawEventSinks:       make([]RawEventSink, 0),
		semanticSinks:       make([]SemanticEventSink, 0),
		processingDurations: make(map[string]time.Duration),
	}

	// Initialize components
	filterConfig := DefaultFilterConfig()
	processor.filterEngine = NewFilterEngine(filterConfig)
	processor.enricher = NewEventEnricher()
	// Correlation handler will be injected by integration layer

	// Initialize storage if raw path enabled
	if config.EnableRawPath {
		processor.rawEventStore = NewMemoryRawEventStore(config.RawRetentionPeriod)
	}

	// Initialize Tapio client if semantic path enabled
	if config.EnableSemanticPath && config.TapioServerAddr != "" {
		client, err := NewTapioGRPCClient(config.TapioServerAddr)
		if err != nil {
			log.Printf("Failed to create Tapio client: %v", err)
		} else {
			processor.tapioClient = client
		}
	}

	return processor
}

// DefaultProcessorConfig returns default processor configuration
func DefaultProcessorConfig() *ProcessorConfig {
	return &ProcessorConfig{
		RawBufferSize:      100000,
		SemanticBufferSize: 10000,
		WorkerCount:        8,
		BatchSize:          1000,
		FlushInterval:      time.Second,
		EnableRawPath:      true,
		EnableSemanticPath: true,
		RawRetentionPeriod: 24 * time.Hour,
		RawStorageBackend:  "memory",
		SemanticBatchSize:  100,
		MaxMemoryUsage:     1024 * 1024 * 1024, // 1GB
		MetricsInterval:    time.Minute,
	}
}

// Start begins processing events
func (p *DualPathProcessor) Start() error {
	log.Printf("Starting dual-path processor with %d workers", p.workers)

	// Start worker goroutines
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.processWorker(i)
	}

	// Start batch processing goroutines
	if p.config.EnableRawPath {
		p.wg.Add(1)
		go p.rawBatchProcessor()
	}

	if p.config.EnableSemanticPath {
		p.wg.Add(1)
		go p.semanticBatchProcessor()
	}

	// Start metrics reporter
	p.wg.Add(1)
	go p.metricsReporter()

	return nil
}

// Stop gracefully stops the processor
func (p *DualPathProcessor) Stop() error {
	log.Println("Stopping dual-path processor...")

	p.cancel()

	// Close channels
	close(p.rawEventBuffer)
	close(p.semanticBuffer)

	// Wait for workers to finish
	p.wg.Wait()

	// Close sinks
	for _, sink := range p.rawEventSinks {
		sink.Close()
	}
	for _, sink := range p.semanticSinks {
		sink.Close()
	}

	// Close storage and clients
	if p.rawEventStore != nil {
		p.rawEventStore.Close()
	}
	if p.tapioClient != nil {
		p.tapioClient.Close()
	}

	log.Println("Dual-path processor stopped")
	return nil
}

// ProcessRawEvent processes a raw eBPF event through both paths
func (p *DualPathProcessor) ProcessRawEvent(event *RawEvent) error {
	start := time.Now()
	defer func() {
		p.mu.Lock()
		p.processingDurations["raw_event"] = time.Since(start)
		p.rawEventsProcessed++
		p.lastProcessingTime = time.Now()
		p.mu.Unlock()
	}()

	// Apply initial filtering
	if !p.filterEngine.ProcessRawEvent(event) {
		return nil // Filtered out
	}

	// Raw path: Send to raw event buffer for Hubble-style access
	if p.config.EnableRawPath {
		select {
		case p.rawEventBuffer <- event:
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
			// Buffer full, drop event or apply backpressure
			log.Printf("Raw event buffer full, dropping event")
		}
	}

	// Semantic path: Enrich and filter for semantic processing
	if p.config.EnableSemanticPath {
		enriched, err := p.enricher.EnrichEvent(p.ctx, event)
		if err != nil {
			p.mu.Lock()
			p.errorsCount++
			p.mu.Unlock()
			return fmt.Errorf("failed to enrich event: %w", err)
		}

		// Apply semantic filtering and sampling
		decision := p.filterEngine.ProcessEnrichedEvent(enriched)
		if decision.SendSemantic && decision.ShouldSample {
			select {
			case p.semanticBuffer <- enriched:
			case <-p.ctx.Done():
				return p.ctx.Err()
			default:
				log.Printf("Semantic event buffer full, dropping event")
			}
		}
	}

	return nil
}

// processWorker is the main event processing worker
func (p *DualPathProcessor) processWorker(workerID int) {
	defer p.wg.Done()

	log.Printf("Worker %d started", workerID)
	defer log.Printf("Worker %d stopped", workerID)

	for {
		select {
		case <-p.ctx.Done():
			return
		case event, ok := <-p.rawEventBuffer:
			if !ok {
				return
			}

			// Store raw event if enabled
			if p.rawEventStore != nil {
				if err := p.rawEventStore.Store(p.ctx, event); err != nil {
					log.Printf("Worker %d: Failed to store raw event: %v", workerID, err)
				}
			}

			// Send to raw event sinks
			for _, sink := range p.rawEventSinks {
				if err := sink.Send(p.ctx, event); err != nil {
					log.Printf("Worker %d: Failed to send to raw sink: %v", workerID, err)
				}
			}
		}
	}
}

// rawBatchProcessor handles batched processing of raw events
func (p *DualPathProcessor) rawBatchProcessor() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]*RawEvent, 0, p.config.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// Send batch to storage
		if p.rawEventStore != nil {
			if err := p.rawEventStore.StoreBatch(p.ctx, batch); err != nil {
				log.Printf("Failed to store raw event batch: %v", err)
			}
		}

		// Send batch to sinks
		for _, sink := range p.rawEventSinks {
			if err := sink.SendBatch(p.ctx, batch); err != nil {
				log.Printf("Failed to send raw event batch: %v", err)
			}
		}

		batch = batch[:0] // Reset batch
	}

	for {
		select {
		case <-p.ctx.Done():
			flush() // Final flush
			return
		case <-ticker.C:
			flush()
		}
	}
}

// semanticBatchProcessor handles batched processing of semantic events
func (p *DualPathProcessor) semanticBatchProcessor() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]*EnrichedEvent, 0, p.config.SemanticBatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// Convert to domain events
		domainEvents := make([]*domain.Event, 0, len(batch))
		for _, enriched := range batch {
			domainEvent := enriched.ToDomainEvent()
			domainEvents = append(domainEvents, domainEvent)
		}

		// Send to Tapio client
		if p.tapioClient != nil {
			if err := p.tapioClient.SendBatch(p.ctx, domainEvents); err != nil {
				log.Printf("Failed to send batch to Tapio: %v", err)
			} else {
				p.mu.Lock()
				p.semanticEventsSent += uint64(len(domainEvents))
				p.mu.Unlock()
			}
		}

		// Send to semantic sinks
		for _, sink := range p.semanticSinks {
			if err := sink.SendBatch(p.ctx, domainEvents); err != nil {
				log.Printf("Failed to send semantic batch: %v", err)
			}
		}

		// Send enriched events to correlation handler (injected from integration layer)
		if p.correlationHandler != nil {
			for _, enriched := range batch {
				p.correlationHandler(enriched)
			}
		}

		batch = batch[:0] // Reset batch
	}

	for {
		select {
		case <-p.ctx.Done():
			flush() // Final flush
			return
		case enriched, ok := <-p.semanticBuffer:
			if !ok {
				flush()
				return
			}

			batch = append(batch, enriched)
			if len(batch) >= p.config.SemanticBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// metricsReporter periodically reports processing metrics
func (p *DualPathProcessor) metricsReporter() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.reportMetrics()
		}
	}
}

// reportMetrics logs current processing metrics
func (p *DualPathProcessor) reportMetrics() {
	p.mu.RLock()
	rawProcessed := p.rawEventsProcessed
	semanticSent := p.semanticEventsSent
	errors := p.errorsCount
	p.mu.RUnlock()

	stats := p.filterEngine.GetStatistics()

	log.Printf("Processor metrics: raw_processed=%d, semantic_sent=%d, errors=%d, filter_ratio=%.2f",
		rawProcessed, semanticSent, errors, stats["filter_ratio"])
}

// AddRawEventSink adds a sink for raw events
func (p *DualPathProcessor) AddRawEventSink(sink RawEventSink) {
	p.rawEventSinks = append(p.rawEventSinks, sink)
}

// AddSemanticEventSink adds a sink for semantic events
func (p *DualPathProcessor) AddSemanticEventSink(sink SemanticEventSink) {
	p.semanticSinks = append(p.semanticSinks, sink)
}

// QueryRawEvents queries stored raw events (Hubble-style API)
func (p *DualPathProcessor) QueryRawEvents(ctx context.Context, filter *EventFilter) ([]*RawEvent, error) {
	if p.rawEventStore == nil {
		return nil, fmt.Errorf("raw event store not available")
	}

	return p.rawEventStore.Query(ctx, filter)
}

// GetStatistics returns processor statistics
func (p *DualPathProcessor) GetStatistics() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"raw_events_processed": p.rawEventsProcessed,
		"semantic_events_sent": p.semanticEventsSent,
		"errors_count":         p.errorsCount,
		"last_processing_time": p.lastProcessingTime,
		"raw_buffer_size":      len(p.rawEventBuffer),
		"semantic_buffer_size": len(p.semanticBuffer),
		"worker_count":         p.workers,
	}

	// Add filter statistics
	filterStats := p.filterEngine.GetStatistics()
	for k, v := range filterStats {
		stats["filter_"+k] = v
	}

	return stats
}

// Memory-based raw event store implementation
type MemoryRawEventStore struct {
	mu            sync.RWMutex
	events        []*RawEvent
	maxSize       int
	retentionTime time.Duration
	lastCleanup   time.Time
}

func NewMemoryRawEventStore(retention time.Duration) *MemoryRawEventStore {
	return &MemoryRawEventStore{
		events:        make([]*RawEvent, 0),
		maxSize:       1000000, // 1M events max
		retentionTime: retention,
		lastCleanup:   time.Now(),
	}
}

func (s *MemoryRawEventStore) Store(ctx context.Context, event *RawEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)

	// Periodic cleanup
	if time.Since(s.lastCleanup) > time.Hour {
		s.cleanup()
	}

	return nil
}

func (s *MemoryRawEventStore) StoreBatch(ctx context.Context, events []*RawEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, events...)

	// Periodic cleanup
	if time.Since(s.lastCleanup) > time.Hour {
		s.cleanup()
	}

	return nil
}

func (s *MemoryRawEventStore) Query(ctx context.Context, filter *EventFilter) ([]*RawEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*RawEvent, 0)

	for _, event := range s.events {
		if s.matchesFilter(event, filter) {
			results = append(results, event)
		}
	}

	return results, nil
}

func (s *MemoryRawEventStore) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*RawEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*RawEvent, 0)

	for _, event := range s.events {
		eventTime := time.Unix(0, int64(event.Timestamp))
		if eventTime.After(start) && eventTime.Before(end) {
			results = append(results, event)
		}
	}

	return results, nil
}

func (s *MemoryRawEventStore) Delete(ctx context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	beforeNanos := uint64(before.UnixNano())
	kept := make([]*RawEvent, 0)

	for _, event := range s.events {
		if event.Timestamp >= beforeNanos {
			kept = append(kept, event)
		}
	}

	s.events = kept
	return nil
}

func (s *MemoryRawEventStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = nil
	return nil
}

func (s *MemoryRawEventStore) cleanup() {
	cutoff := time.Now().Add(-s.retentionTime)
	s.Delete(context.Background(), cutoff)
	s.lastCleanup = time.Now()

	// Size-based cleanup if still too large
	if len(s.events) > s.maxSize {
		// Keep only the newest events
		keep := s.maxSize / 2
		s.events = s.events[len(s.events)-keep:]
	}
}

func (s *MemoryRawEventStore) matchesFilter(event *RawEvent, filter *EventFilter) bool {
	if filter == nil {
		return true
	}

	// Check event types
	if len(filter.EventTypes) > 0 {
		found := false
		for _, t := range filter.EventTypes {
			if t == event.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check PIDs
	if len(filter.PIDs) > 0 {
		found := false
		for _, pid := range filter.PIDs {
			if pid == event.PID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check process names
	if len(filter.Comms) > 0 {
		found := false
		for _, comm := range filter.Comms {
			if comm == event.Comm {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// SetCorrelationHandler allows integration layer to inject correlation functionality
func (p *DualPathProcessor) SetCorrelationHandler(handler func(*EnrichedEvent)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.correlationHandler = handler
}
