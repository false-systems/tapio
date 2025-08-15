package bpf_common

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// FrameworkConfig holds configuration for the unified eBPF framework
type FrameworkConfig struct {
	// Component enablement
	EnableStatistics bool `json:"enable_statistics"`
	EnableFiltering  bool `json:"enable_filtering"`
	EnableBatching   bool `json:"enable_batching"`
	EnableSampling   bool `json:"enable_sampling"`
	
	// Component configurations
	StatisticsConfig *BPFStatsCollector `json:"statistics_config,omitempty"`
	FilterConfig     *FilterConfig   `json:"filter_config,omitempty"`
	BatchConfig      *BatchConfig       `json:"batch_config,omitempty"`
	SamplingConfig   *SamplingConfig    `json:"sampling_config,omitempty"`
	
	// Integration settings
	UpdateInterval   time.Duration `json:"update_interval"`
	ShutdownTimeout  time.Duration `json:"shutdown_timeout"`
}

// DefaultFrameworkConfig returns sensible defaults for the unified framework
func DefaultFrameworkConfig() *FrameworkConfig {
	return &FrameworkConfig{
		EnableStatistics: true,
		EnableFiltering:  true,
		EnableBatching:   true,
		EnableSampling:   true,
		FilterConfig:     &FilterConfig{
			SampleRate: 10,
			BatchSize: 100,
		},
		BatchConfig:      DefaultBatchConfig(),
		SamplingConfig:   DefaultSamplingConfig(),
		UpdateInterval:   5 * time.Second,
		ShutdownTimeout:  30 * time.Second,
	}
}

// UnifiedEBPFFramework provides integrated eBPF functionality for all collectors
type UnifiedEBPFFramework struct {
	mu              sync.RWMutex
	logger          *zap.Logger
	config          *FrameworkConfig
	ctx             context.Context
	cancel          context.CancelFunc
	
	// Core components
	statsCollector  *BPFStatsCollector
	filterManager   *FilterManager
	batchProcessor  *BatchProcessor
	samplingManager *SamplingManager
	
	// Registered programs
	programs        map[string]*ProgramRegistration
	
	// Event flow coordination
	eventPipeline   *EventPipeline
}

// ProgramRegistration holds information about a registered eBPF program
type ProgramRegistration struct {
	Name            string
	ProgramType     string
	ProgramID       uint32
	BPFMaps         map[string]*ebpf.Map
	EventTypes      []string
	LastActivity    time.Time
	Active          bool
	
	// Component integrations
	HasStatistics   bool
	HasFiltering    bool
	HasBatching     bool
	HasSampling     bool
}

// EventPipeline coordinates event processing through all framework components
type EventPipeline struct {
	mu              sync.RWMutex
	framework       *UnifiedEBPFFramework
	logger          *zap.Logger
	
	// Pipeline stages
	inputChannel    chan *RawEBPFEvent
	filterChannel   chan *RawEBPFEvent
	sampleChannel   chan *RawEBPFEvent
	batchChannel    chan *BatchedEvent
	outputChannel   chan collectors.RawEvent
	
	// Worker management
	workers         []*PipelineWorker
	workerWg        sync.WaitGroup
	workerCount     int
}

// RawEBPFEvent represents an event before framework processing
type RawEBPFEvent struct {
	ProgramName     string
	EventType       string
	Data            []byte
	Metadata        map[string]string
	Timestamp       time.Time
	ConsistencyKey  uint64
	Priority        int
}

// PipelineWorker processes events through the pipeline
type PipelineWorker struct {
	id              int
	pipeline        *EventPipeline
	logger          *zap.Logger
}

// NewUnifiedEBPFFramework creates a new unified eBPF framework
func NewUnifiedEBPFFramework(config *FrameworkConfig, logger *zap.Logger) (*UnifiedEBPFFramework, error) {
	if config == nil {
		config = DefaultFrameworkConfig()
	}
	
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}
	
	framework := &UnifiedEBPFFramework{
		logger:   logger,
		config:   config,
		programs: make(map[string]*ProgramRegistration),
	}
	
	// Initialize components based on configuration
	if config.EnableStatistics {
		statsCollector, err := NewBPFStatsCollector(logger.Named("stats"), config.UpdateInterval)
		if err != nil {
			return nil, fmt.Errorf("failed to create stats collector: %w", err)
		}
		framework.statsCollector = statsCollector
	}
	
	if config.EnableFiltering {
		filterManager, err := NewFilterManager(logger.Named("filter"), nil, nil, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create dynamic filter: %w", err)
		}
		framework.filterManager = filterManager
	}
	
	if config.EnableBatching {
		batchProcessor, err := NewBatchProcessor(config.BatchConfig, framework.statsCollector, logger.Named("batch"))
		if err != nil {
			return nil, fmt.Errorf("failed to create batch processor: %w", err)
		}
		framework.batchProcessor = batchProcessor
	}
	
	if config.EnableSampling {
		samplingManager, err := NewSamplingManager(logger.Named("sampler"), config.SamplingConfig, nil, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create sampler: %w", err)
		}
		framework.samplingManager = samplingManager
	}
	
	// Create event pipeline
	eventPipeline, err := NewEventPipeline(framework, logger.Named("pipeline"))
	if err != nil {
		return nil, fmt.Errorf("failed to create event pipeline: %w", err)
	}
	framework.eventPipeline = eventPipeline
	
	return framework, nil
}

// Start initializes and starts the unified framework
func (f *UnifiedEBPFFramework) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	
	if f.ctx != nil {
		return fmt.Errorf("framework already started")
	}
	
	f.ctx, f.cancel = context.WithCancel(ctx)
	
	// Start components
	if f.statsCollector != nil {
		if err := f.statsCollector.Start(f.ctx); err != nil {
			return fmt.Errorf("failed to start stats collector: %w", err)
		}
	}
	
	if f.filterManager != nil {
		// Filter manager doesn't need explicit start
	}
	
	if f.batchProcessor != nil {
		if err := f.batchProcessor.Start(f.ctx); err != nil {
			return fmt.Errorf("failed to start batch processor: %w", err)
		}
	}
	
	if f.samplingManager != nil {
		if err := f.samplingManager.Start(f.ctx); err != nil {
			return fmt.Errorf("failed to start sampler: %w", err)
		}
	}
	
	// Start event pipeline
	if err := f.eventPipeline.Start(f.ctx); err != nil {
		return fmt.Errorf("failed to start event pipeline: %w", err)
	}
	
	f.logger.Info("Unified eBPF framework started",
		zap.Bool("statistics", f.config.EnableStatistics),
		zap.Bool("filtering", f.config.EnableFiltering),
		zap.Bool("batching", f.config.EnableBatching),
		zap.Bool("sampling", f.config.EnableSampling),
	)
	
	return nil
}

// Stop gracefully shuts down the unified framework
func (f *UnifiedEBPFFramework) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	
	if f.cancel != nil {
		f.cancel()
	}
	
	var errors []error
	
	// Stop event pipeline first
	if f.eventPipeline != nil {
		if err := f.eventPipeline.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("event pipeline stop error: %w", err))
		}
	}
	
	// Stop components
	if f.batchProcessor != nil {
		if err := f.batchProcessor.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("batch processor stop error: %w", err))
		}
	}
	
	if f.samplingManager != nil {
		// Sampling manager doesn't have explicit stop
	}
	
	if f.filterManager != nil {
		// Filter manager doesn't have explicit stop
	}
	
	if f.statsCollector != nil {
		if err := f.statsCollector.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("stats collector stop error: %w", err))
		}
	}
	
	f.logger.Info("Unified eBPF framework stopped")
	
	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}
	
	return nil
}

// RegisterProgram registers an eBPF program with the framework
func (f *UnifiedEBPFFramework) RegisterProgram(name, programType string, programID uint32, eventTypes []string) *ProgramRegistration {
	f.mu.Lock()
	defer f.mu.Unlock()
	
	registration := &ProgramRegistration{
		Name:         name,
		ProgramType:  programType,
		ProgramID:    programID,
		BPFMaps:      make(map[string]*ebpf.Map),
		EventTypes:   eventTypes,
		LastActivity: time.Now(),
		Active:       true,
		HasStatistics: f.config.EnableStatistics,
		HasFiltering: f.config.EnableFiltering,
		HasBatching:  f.config.EnableBatching,
		HasSampling:  f.config.EnableSampling,
	}
	
	f.programs[name] = registration
	
	// Register with components
	if f.statsCollector != nil {
		f.statsCollector.RegisterProgram(name, programType, programID)
	}
	
	f.logger.Info("Registered eBPF program with framework",
		zap.String("name", name),
		zap.String("type", programType),
		zap.Uint32("program_id", programID),
		zap.Strings("event_types", eventTypes),
	)
	
	return registration
}

// UnregisterProgram removes an eBPF program from the framework
func (f *UnifiedEBPFFramework) UnregisterProgram(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	
	registration, exists := f.programs[name]
	if !exists {
		return
	}
	
	// Unregister from components
	if f.statsCollector != nil {
		f.statsCollector.UnregisterProgram(name)
	}
	
	if f.filterManager != nil {
		// Unregister namespace filter map
		// Unregister network filter map
		// Unregister cgroup filter map
	}
	
	if f.samplingManager != nil {
		// Unregister sampler map
	}
	
	// Mark as inactive
	registration.Active = false
	delete(f.programs, name)
	
	f.logger.Info("Unregistered eBPF program from framework",
		zap.String("name", name),
	)
}

// RegisterBPFMap registers a BPF map for a specific program and purpose
func (f *UnifiedEBPFFramework) RegisterBPFMap(programName, mapName string, bpfMap *ebpf.Map, purpose string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	
	registration, exists := f.programs[programName]
	if !exists {
		return fmt.Errorf("program %s not registered", programName)
	}
	
	registration.BPFMaps[mapName] = bpfMap
	
	// Register with appropriate components based on purpose
	switch purpose {
	case "filter_namespace":
		if f.filterManager != nil {
			// Register namespace filter map
			return nil
		}
	case "filter_process":
		if f.filterManager != nil {
			// Register filter map
			return nil
		}
	case "filter_network":
		if f.filterManager != nil {
			// Register network filter map
			return nil
		}
	case "filter_cgroup":
		if f.filterManager != nil {
			// Register cgroup filter map
			return nil
		}
	case "sampling":
		if f.samplingManager != nil {
			// Register sampler map
			return nil
		}
	default:
		f.logger.Debug("Registered BPF map with unknown purpose",
			zap.String("program", programName),
			zap.String("map", mapName),
			zap.String("purpose", purpose),
		)
	}
	
	return nil
}

// ProcessEvent processes a raw eBPF event through the framework pipeline
func (f *UnifiedEBPFFramework) ProcessEvent(event *RawEBPFEvent) error {
	if f.eventPipeline == nil {
		return fmt.Errorf("event pipeline not initialized")
	}
	
	return f.eventPipeline.ProcessEvent(event)
}

// GetOutputChannel returns the channel for receiving processed events
func (f *UnifiedEBPFFramework) GetOutputChannel() <-chan collectors.RawEvent {
	if f.eventPipeline == nil {
		return nil
	}
	return f.eventPipeline.GetOutputChannel()
}

// GetStatistics returns comprehensive framework statistics
func (f *UnifiedEBPFFramework) GetStatistics() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()
	
	stats := make(map[string]interface{})
	
	// Program statistics
	programStats := make(map[string]interface{})
	for name, registration := range f.programs {
		programStats[name] = map[string]interface{}{
			"type":         registration.ProgramType,
			"program_id":   registration.ProgramID,
			"event_types":  registration.EventTypes,
			"active":       registration.Active,
			"last_activity": registration.LastActivity,
			"maps_count":   len(registration.BPFMaps),
		}
	}
	stats["programs"] = programStats
	
	// Component statistics
	if f.statsCollector != nil {
		stats["bpf_statistics"] = f.statsCollector.GetAllStats()
	}
	
	if f.filterManager != nil {
		stats["filtering"] = f.filterManager.GetStatistics()
	}
	
	if f.batchProcessor != nil {
		stats["batching"] = f.batchProcessor.GetStats()
	}
	
	if f.samplingManager != nil {
		stats["sampling"] = f.samplingManager.GetStatistics()
	}
	
	if f.eventPipeline != nil {
		stats["pipeline"] = f.eventPipeline.GetStatistics()
	}
	
	return stats
}

// UpdateSampleRate updates sampling rate for a specific event type across all programs
func (f *UnifiedEBPFFramework) UpdateSampleRate(eventType string, sampleRate float64) error {
	if f.samplingManager == nil {
		return fmt.Errorf("sampling not enabled")
	}
	
	return f.samplingManager.SetEventTypeRate(eventType, sampleRate)
}

// AddPIDFilter adds a PID filter
func (f *UnifiedEBPFFramework) AddPIDFilter(pid uint32, allow bool) error {
	if f.filterManager == nil {
		return fmt.Errorf("filtering not enabled")
	}
	
	return f.filterManager.AddPIDFilter(pid, allow)
}

// GetActivePrograms returns a list of active program names
func (f *UnifiedEBPFFramework) GetActivePrograms() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	
	var activePrograms []string
	for name, registration := range f.programs {
		if registration.Active {
			activePrograms = append(activePrograms, name)
		}
	}
	
	return activePrograms
}

// NewEventPipeline creates a new event processing pipeline
func NewEventPipeline(framework *UnifiedEBPFFramework, logger *zap.Logger) (*EventPipeline, error) {
	pipeline := &EventPipeline{
		framework:     framework,
		logger:        logger,
		inputChannel:  make(chan *RawEBPFEvent, 10000),
		filterChannel: make(chan *RawEBPFEvent, 10000),
		sampleChannel: make(chan *RawEBPFEvent, 10000),
		batchChannel:  make(chan *BatchedEvent, 10000),
		outputChannel: make(chan collectors.RawEvent, 10000),
		workerCount:   4,
	}
	
	pipeline.workers = make([]*PipelineWorker, pipeline.workerCount)
	for i := 0; i < pipeline.workerCount; i++ {
		pipeline.workers[i] = &PipelineWorker{
			id:       i,
			pipeline: pipeline,
			logger:   logger.With(zap.Int("worker_id", i)),
		}
	}
	
	return pipeline, nil
}

// Start begins the event pipeline processing
func (p *EventPipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Start workers
	for _, worker := range p.workers {
		p.workerWg.Add(1)
		go worker.run(ctx)
	}
	
	p.logger.Info("Event pipeline started",
		zap.Int("worker_count", p.workerCount),
	)
	
	return nil
}

// Stop shuts down the event pipeline
func (p *EventPipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Close input channel to signal shutdown
	close(p.inputChannel)
	
	// Wait for workers to finish
	p.workerWg.Wait()
	
	// Close other channels
	close(p.filterChannel)
	close(p.sampleChannel)
	close(p.batchChannel)
	close(p.outputChannel)
	
	p.logger.Info("Event pipeline stopped")
	return nil
}

// ProcessEvent adds an event to the pipeline for processing
func (p *EventPipeline) ProcessEvent(event *RawEBPFEvent) error {
	select {
	case p.inputChannel <- event:
		return nil
	default:
		return fmt.Errorf("pipeline input buffer full")
	}
}

// GetOutputChannel returns the output channel for processed events
func (p *EventPipeline) GetOutputChannel() <-chan collectors.RawEvent {
	return p.outputChannel
}

// GetStatistics returns pipeline processing statistics
func (p *EventPipeline) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"input_buffer_size":  cap(p.inputChannel),
		"filter_buffer_size": cap(p.filterChannel),
		"sample_buffer_size": cap(p.sampleChannel),
		"batch_buffer_size":  cap(p.batchChannel),
		"output_buffer_size": cap(p.outputChannel),
		"worker_count":       p.workerCount,
	}
}

// run is the main worker processing loop
func (w *PipelineWorker) run(ctx context.Context) {
	defer w.pipeline.workerWg.Done()
	
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.pipeline.inputChannel:
			if !ok {
				return
			}
			w.processEvent(event)
		}
	}
}

// processEvent processes a single event through the pipeline stages
func (w *PipelineWorker) processEvent(event *RawEBPFEvent) {
	framework := w.pipeline.framework
	
	// Stage 1: Filtering
	if framework.filterManager != nil {
		// For now, all events pass through (filtering logic would go here)
		// In a real implementation, we would check filter rules
		w.pipeline.filterChannel <- event
	} else {
		w.pipeline.sampleChannel <- event
	}
	
	// Stage 2: Sampling
	select {
	case filteredEvent := <-w.pipeline.filterChannel:
		if framework.samplingManager != nil {
			// Check if should sample (simplified - would need priority/latency)
			if framework.samplingManager.ShouldSample(filteredEvent.EventType, 1, 0, false) {
				w.pipeline.sampleChannel <- filteredEvent
			}
		} else {
			w.pipeline.sampleChannel <- filteredEvent
		}
	default:
	}
	
	// Stage 3: Batching
	select {
	case sampledEvent := <-w.pipeline.sampleChannel:
		if framework.batchProcessor != nil {
			// Convert to collectors.RawEvent
			rawEvent := collectors.RawEvent{
				Timestamp: sampledEvent.Timestamp,
				Type:      sampledEvent.EventType,
				Data:      sampledEvent.Data,
				Metadata:  sampledEvent.Metadata,
				TraceID:   collectors.GenerateTraceID(),
				SpanID:    collectors.GenerateSpanID(),
			}
			
			// Add to batch processor
			if err := framework.batchProcessor.AddEvent(rawEvent); err != nil {
				w.logger.Warn("Failed to add event to batch processor", zap.Error(err))
			}
		} else {
			// Direct output
			rawEvent := collectors.RawEvent{
				Timestamp: sampledEvent.Timestamp,
				Type:      sampledEvent.EventType,
				Data:      sampledEvent.Data,
				Metadata:  sampledEvent.Metadata,
				TraceID:   collectors.GenerateTraceID(),
				SpanID:    collectors.GenerateSpanID(),
			}
			
			select {
			case w.pipeline.outputChannel <- rawEvent:
			default:
				w.logger.Warn("Output buffer full, dropping event")
			}
		}
	default:
	}
	
	// Stage 4: Batch Output Processing
	if framework.batchProcessor != nil {
		select {
		case batch := <-framework.batchProcessor.Output():
			// Process batch and send individual events to output
			for _, batchedEvent := range batch.Events {
				select {
				case w.pipeline.outputChannel <- batchedEvent.Event:
				default:
					w.logger.Warn("Output buffer full, dropping batched event")
				}
			}
		default:
		}
	}
}