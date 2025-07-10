package sniffer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/falseyair/tapio/pkg/ebpf"
)

// EBPFSniffer implements the Sniffer interface for eBPF-based monitoring
type EBPFSniffer struct {
	// Core components
	monitor      ebpf.Monitor
	eventChan    chan Event
	ctx          context.Context
	cancel       context.CancelFunc
	config       Config

	// Health tracking
	mu              sync.RWMutex
	lastEventTime   time.Time
	eventsProcessed uint64
	eventsDropped   uint64
	isRunning       bool

	// Event processing
	eventBuffer     chan *ebpf.ProcessMemoryStats
	processCache    *ProcessCache
	pidTranslator   PIDTranslator
}

// PIDTranslator converts PIDs to Kubernetes context
type PIDTranslator interface {
	GetPodInfo(pid uint32) (*EventContext, error)
}

// NewEBPFSniffer creates a new eBPF-based sniffer
func NewEBPFSniffer(monitor ebpf.Monitor, translator PIDTranslator) *EBPFSniffer {
	return &EBPFSniffer{
		monitor:       monitor,
		pidTranslator: translator,
		processCache:  NewProcessCache(64 * 1024 * 1024), // 64MB cache
	}
}

// Name returns the unique name of this sniffer
func (s *EBPFSniffer) Name() string {
	return "ebpf"
}

// Events returns the event channel
func (s *EBPFSniffer) Events() <-chan Event {
	return s.eventChan
}

// Start begins eBPF monitoring
func (s *EBPFSniffer) Start(ctx context.Context, config Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return fmt.Errorf("eBPF sniffer already running")
	}

	s.config = config
	s.eventChan = make(chan Event, config.EventBufferSize)
	s.eventBuffer = make(chan *ebpf.ProcessMemoryStats, 1000)
	
	// Create cancellable context
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Start eBPF monitor
	if err := s.monitor.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start eBPF monitor: %w", err)
	}

	s.isRunning = true
	s.lastEventTime = time.Now()

	// Start event processing goroutines
	go s.collectEvents()
	go s.processEvents()
	go s.performPredictions()

	return nil
}

// Health returns the current health status
func (s *EBPFSniffer) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := HealthStatusHealthy
	message := "eBPF monitoring active"
	
	if !s.isRunning {
		status = HealthStatusUnhealthy
		message = "eBPF sniffer not running"
	} else if time.Since(s.lastEventTime) > 5*time.Minute {
		status = HealthStatusDegraded
		message = "No events in last 5 minutes"
	}

	// Get eBPF-specific metrics
	metrics := make(map[string]interface{})
	if s.monitor != nil && s.monitor.IsAvailable() {
		if stats, err := s.monitor.GetMemoryStats(); err == nil {
			metrics["processes_tracked"] = len(stats)
		}
	}
	metrics["cache_size"] = s.processCache.Size()

	return Health{
		Status:          status,
		Message:         message,
		LastEventTime:   s.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&s.eventsProcessed),
		EventsDropped:   atomic.LoadUint64(&s.eventsDropped),
		Metrics:         metrics,
	}
}

// collectEvents collects raw events from eBPF
func (s *EBPFSniffer) collectEvents() {
	ticker := time.NewTicker(50 * time.Millisecond) // 20Hz sampling
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !s.monitor.IsAvailable() {
				continue
			}

			stats, err := s.monitor.GetMemoryStats()
			if err != nil {
				continue
			}

			// Apply sampling rate
			for i := range stats {
				if s.shouldSample() {
					select {
					case s.eventBuffer <- &stats[i]:
					default:
						atomic.AddUint64(&s.eventsDropped, 1)
					}
				}
			}
		}
	}
}

// processEvents processes raw eBPF data into events
func (s *EBPFSniffer) processEvents() {
	batch := make([]*ebpf.ProcessMemoryStats, 0, 100)
	batchTimer := time.NewTicker(10 * time.Millisecond) // 10ms batch timeout
	defer batchTimer.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		
		case stat := <-s.eventBuffer:
			batch = append(batch, stat)
			
			// Process batch if full
			if len(batch) >= 100 {
				s.processBatch(batch)
				batch = batch[:0]
			}
			
		case <-batchTimer.C:
			// Process partial batch on timeout
			if len(batch) > 0 {
				s.processBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// processBatch processes a batch of memory stats
func (s *EBPFSniffer) processBatch(batch []*ebpf.ProcessMemoryStats) {
	for _, stat := range batch {
		// Update process cache
		s.processCache.Update(stat)

		// Check for anomalies
		if event := s.checkForAnomalies(stat); event != nil {
			s.emitEvent(event)
		}
	}
}

// checkForAnomalies detects issues in process stats
func (s *EBPFSniffer) checkForAnomalies(stat *ebpf.ProcessMemoryStats) *Event {
	// Memory leak detection
	if stat.AllocationRate > 10*1024*1024 { // 10MB/s
		return s.createMemoryLeakEvent(stat)
	}

	// High memory usage
	if stat.CurrentUsage > 1024*1024*1024 { // 1GB
		return s.createHighMemoryEvent(stat)
	}

	return nil
}

// createMemoryLeakEvent creates a memory leak detection event
func (s *EBPFSniffer) createMemoryLeakEvent(stat *ebpf.ProcessMemoryStats) *Event {
	event := &Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "ebpf",
		Type:      "memory_leak",
		Severity:  SeverityHigh,
		Data: map[string]interface{}{
			"pid":             stat.PID,
			"command":         stat.Command,
			"allocation_rate": stat.AllocationRate,
			"current_usage":   stat.CurrentUsage,
			"in_container":    stat.InContainer,
		},
	}

	// Get Kubernetes context
	if ctx, err := s.pidTranslator.GetPodInfo(stat.PID); err == nil {
		event.Context = ctx
		
		// Add actionable fix
		event.Actionable = &ActionableItem{
			Title:       "Potential Memory Leak Detected",
			Description: fmt.Sprintf("Process %s is allocating memory at %.2f MB/s", stat.Command, float64(stat.AllocationRate)/(1024*1024)),
			Commands: []string{
				fmt.Sprintf("kubectl top pod %s -n %s", ctx.Pod, ctx.Namespace),
				fmt.Sprintf("kubectl logs %s -n %s | tail -100", ctx.Pod, ctx.Namespace),
				fmt.Sprintf("kubectl exec -it %s -n %s -- /bin/sh", ctx.Pod, ctx.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic commands only - no changes",
		}
	}

	return event
}

// createHighMemoryEvent creates a high memory usage event
func (s *EBPFSniffer) createHighMemoryEvent(stat *ebpf.ProcessMemoryStats) *Event {
	event := &Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "ebpf",
		Type:      "high_memory",
		Severity:  SeverityMedium,
		Data: map[string]interface{}{
			"pid":           stat.PID,
			"command":       stat.Command,
			"current_usage": stat.CurrentUsage,
			"in_container":  stat.InContainer,
		},
	}

	// Get Kubernetes context
	if ctx, err := s.pidTranslator.GetPodInfo(stat.PID); err == nil {
		event.Context = ctx
	}

	return event
}

// performPredictions runs OOM predictions periodically
func (s *EBPFSniffer) performPredictions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.predictOOMs()
		}
	}
}

// predictOOMs predicts out-of-memory conditions
func (s *EBPFSniffer) predictOOMs() {
	processes := s.processCache.GetAll()
	
	// Get memory limits (would come from Kubernetes)
	limits := make(map[uint32]uint64)
	// TODO: Get actual limits from K8s
	
	predictions, err := s.monitor.GetMemoryPredictions(limits)
	if err != nil {
		return
	}

	for pid, pred := range predictions {
		if pred.TimeToOOM < 10*time.Minute && pred.Confidence > 0.8 {
			s.emitOOMPrediction(pid, pred, processes[pid])
		}
	}
}

// emitOOMPrediction emits an OOM prediction event
func (s *EBPFSniffer) emitOOMPrediction(pid uint32, pred *ebpf.OOMPrediction, proc *ProcessInfo) {
	event := &Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Source:    "ebpf",
		Type:      "oom_prediction",
		Severity:  SeverityCritical,
		Data: map[string]interface{}{
			"pid":           pid,
			"time_to_oom":   pred.TimeToOOM.Seconds(),
			"confidence":    pred.Confidence,
			"current_usage": pred.CurrentUsage,
			"memory_limit":  pred.MemoryLimit,
		},
	}

	// Get Kubernetes context
	if ctx, err := s.pidTranslator.GetPodInfo(pid); err == nil {
		event.Context = ctx
		
		// Add actionable fix
		newLimit := uint64(float64(pred.MemoryLimit) * 1.5)
		event.Actionable = &ActionableItem{
			Title:       fmt.Sprintf("OOM Kill Predicted in %.0f minutes", pred.TimeToOOM.Minutes()),
			Description: fmt.Sprintf("Pod %s will likely be OOM killed. Current usage: %.0f MB, Limit: %.0f MB", 
				ctx.Pod, float64(pred.CurrentUsage)/(1024*1024), float64(pred.MemoryLimit)/(1024*1024)),
			Commands: []string{
				fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"%s\",\"resources\":{\"limits\":{\"memory\":\"%dMi\"}}}]}}}}'",
					ctx.Pod, ctx.Namespace, ctx.Container, newLimit/(1024*1024)),
			},
			Risk:            "medium",
			EstimatedImpact: fmt.Sprintf("Increases memory limit to %.0f MB", float64(newLimit)/(1024*1024)),
		}
	}

	s.emitEvent(&event)
}

// emitEvent sends an event to the channel
func (s *EBPFSniffer) emitEvent(event *Event) {
	select {
	case s.eventChan <- *event:
		atomic.AddUint64(&s.eventsProcessed, 1)
		s.mu.Lock()
		s.lastEventTime = time.Now()
		s.mu.Unlock()
	default:
		atomic.AddUint64(&s.eventsDropped, 1)
	}
}

// shouldSample determines if an event should be sampled
func (s *EBPFSniffer) shouldSample() bool {
	// For now, use configured sampling rate
	// In production, would use more sophisticated sampling
	return true // Always sample for MVP
}

// Stop stops the eBPF sniffer
func (s *EBPFSniffer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	// Cancel context to stop goroutines
	if s.cancel != nil {
		s.cancel()
	}

	// Stop eBPF monitor
	if err := s.monitor.Stop(); err != nil {
		return fmt.Errorf("failed to stop eBPF monitor: %w", err)
	}

	// Close channels
	close(s.eventChan)
	close(s.eventBuffer)

	s.isRunning = false
	return nil
}

// ProcessInfo holds cached process information
type ProcessInfo struct {
	PID         uint32
	Command     string
	LastSeen    time.Time
	MemoryTrend []float64 // Recent memory usage samples
}

// ProcessCache provides fast PID lookups with bounded memory
type ProcessCache struct {
	mu       sync.RWMutex
	data     map[uint32]*ProcessInfo
	maxSize  int
	evictLRU chan uint32
}

// NewProcessCache creates a new process cache
func NewProcessCache(maxSizeBytes int) *ProcessCache {
	// Assume ~200 bytes per entry
	maxEntries := maxSizeBytes / 200
	
	return &ProcessCache{
		data:     make(map[uint32]*ProcessInfo),
		maxSize:  maxEntries,
		evictLRU: make(chan uint32, 100),
	}
}

// Update updates process information
func (c *ProcessCache) Update(stat *ebpf.ProcessMemoryStats) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if proc, exists := c.data[stat.PID]; exists {
		proc.LastSeen = time.Now()
		// Update memory trend (keep last 10 samples)
		proc.MemoryTrend = append(proc.MemoryTrend, float64(stat.CurrentUsage))
		if len(proc.MemoryTrend) > 10 {
			proc.MemoryTrend = proc.MemoryTrend[1:]
		}
	} else {
		// Add new entry
		if len(c.data) >= c.maxSize {
			// Evict oldest
			c.evictOldest()
		}
		
		c.data[stat.PID] = &ProcessInfo{
			PID:         stat.PID,
			Command:     stat.Command,
			LastSeen:    time.Now(),
			MemoryTrend: []float64{float64(stat.CurrentUsage)},
		}
	}
}

// GetAll returns all cached processes
func (c *ProcessCache) GetAll() map[uint32]*ProcessInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	// Return copy to avoid race conditions
	result := make(map[uint32]*ProcessInfo, len(c.data))
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

// Size returns the cache size
func (c *ProcessCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// evictOldest removes the least recently seen process
func (c *ProcessCache) evictOldest() {
	var oldestPID uint32
	var oldestTime time.Time
	
	for pid, proc := range c.data {
		if oldestTime.IsZero() || proc.LastSeen.Before(oldestTime) {
			oldestPID = pid
			oldestTime = proc.LastSeen
		}
	}
	
	if oldestPID > 0 {
		delete(c.data, oldestPID)
	}
}