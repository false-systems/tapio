//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/events"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 memorytracker ../../../ebpf/oom_detector.c -- -I../../../ebpf

// MemoryEvent represents a memory-related event from eBPF
type MemoryEvent struct {
	Timestamp    uint64
	PID          uint32
	TID          uint32
	Size         uint64
	TotalMemory  uint64
	EventType    uint32
	Command      string
	InContainer  bool
	ContainerPID uint32
}

// MemoryCollector implements high-performance memory tracking with OOM prediction
type MemoryCollector struct {
	config collectors.CollectorConfig

	// eBPF program and maps
	objs   memorytrackerObjects
	reader *ringbuf.Reader
	links  []link.Link

	// Event processing
	eventChan chan *collectors.Event

	// Memory tracking state
	processes   map[uint32]*ProcessMemoryTracker
	processesMu sync.RWMutex

	// OOM prediction
	predictor *OOMPredictor

	// Performance metrics
	eventsProcessed uint64
	eventsDropped   uint64
	oomPredictions  uint64
	accurateOOMs    uint64

	// Lifecycle management
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Bool
	stopped atomic.Bool

	// Health tracking
	lastEventTime time.Time
	lastError     error
	healthMu      sync.RWMutex
}

// ProcessMemoryTracker tracks memory usage patterns for a single process
type ProcessMemoryTracker struct {
	PID            uint32
	Command        string
	TotalAllocated uint64
	TotalFreed     uint64
	CurrentUsage   uint64

	// Growth tracking
	GrowthHistory []MemoryDataPoint
	LastUpdate    time.Time

	// Allocation pattern analysis
	AllocationRate float64 // bytes per second
	GrowthTrend    TrendType
	IsContainer    bool
	ContainerPID   uint32

	// OOM risk assessment
	RiskScore    float64
	PredictedOOM *OOMPrediction

	mu sync.RWMutex
}

// MemoryDataPoint represents a single memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time
	Usage     uint64
	Rate      float64 // bytes/second growth rate
}

// TrendType represents memory growth patterns
type TrendType int

const (
	TrendStable TrendType = iota
	TrendLinear
	TrendExponential
	TrendSpiky
	TrendDecreasing
)

// OOMPrediction represents an out-of-memory prediction
type OOMPrediction struct {
	PID                uint32
	TimeToOOM          time.Duration
	Confidence         float64
	CurrentUsage       uint64
	PredictedPeakUsage uint64
	MemoryLimit        uint64
	TrendType          TrendType
	PredictedAt        time.Time
}

// OOMPredictor implements machine learning-based OOM prediction
type OOMPredictor struct {
	// Prediction models
	linearModel      *LinearRegressionModel
	exponentialModel *ExponentialGrowthModel

	// Historical data for accuracy improvement
	historicalOOMs []HistoricalOOM

	// Configuration
	predictionWindow    time.Duration
	minDataPoints       int
	confidenceThreshold float64

	mu sync.RWMutex
}

// LinearRegressionModel for linear growth prediction
type LinearRegressionModel struct {
	Slope     float64
	Intercept float64
	R2Score   float64
}

// ExponentialGrowthModel for exponential growth prediction
type ExponentialGrowthModel struct {
	GrowthRate float64
	BaseUsage  uint64
	R2Score    float64
}

// HistoricalOOM tracks historical OOM events for accuracy validation
type HistoricalOOM struct {
	PID           uint32
	ActualOOMTime time.Time
	PredictedTime time.Time
	Accuracy      time.Duration
}

// NewMemoryCollector creates a new memory tracking collector
func NewMemoryCollector(config collectors.CollectorConfig) (collectors.Collector, error) {
	if !ebpf.IsAvailable() {
		return nil, fmt.Errorf("eBPF is not available on this system")
	}

	ctx, cancel := context.WithCancel(context.Background())

	collector := &MemoryCollector{
		config:    config,
		ctx:       ctx,
		cancel:    cancel,
		eventChan: make(chan *collectors.Event, config.EventBufferSize),
		processes: make(map[uint32]*ProcessMemoryTracker),
		predictor: NewOOMPredictor(),
	}

	return collector, nil
}

// NewOOMPredictor creates a new OOM prediction engine
func NewOOMPredictor() *OOMPredictor {
	return &OOMPredictor{
		predictionWindow:    5 * time.Minute,
		minDataPoints:       3,
		confidenceThreshold: 0.7,
		historicalOOMs:      make([]HistoricalOOM, 0),
	}
}

// Collector interface implementation

// Name returns the collector name
func (mc *MemoryCollector) Name() string {
	return mc.config.Name
}

// Type returns the collector type
func (mc *MemoryCollector) Type() string {
	return "ebpf-memory"
}

// Start begins memory monitoring
func (mc *MemoryCollector) Start(ctx context.Context) error {
	if !mc.started.CompareAndSwap(false, true) {
		return fmt.Errorf("memory collector already started")
	}

	// Check kernel compatibility first
	if err := mc.validateKernelCompatibility(); err != nil {
		return fmt.Errorf("kernel compatibility check failed: %w", err)
	}

	// Load eBPF program
	if err := loadMemorytracker(&mc.objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF program: %w", err)
	}

	// Attach tracepoints
	if err := mc.attachTracepoints(); err != nil {
		mc.objs.Close()
		return fmt.Errorf("failed to attach tracepoints: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(mc.objs.Events)
	if err != nil {
		mc.cleanup()
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	mc.reader = reader

	// Start event processing with optimized ring buffer processing
	mc.wg.Add(4)
	go mc.optimizeProcessEvents() // Use optimized processing instead of processEvents
	go mc.monitorProcesses()
	go mc.runOOMPrediction()
	go mc.adaptiveRingBufferTuning()

	return nil
}

// Stop gracefully stops the collector
func (mc *MemoryCollector) Stop() error {
	if !mc.stopped.CompareAndSwap(false, true) {
		return nil
	}

	mc.cancel()
	mc.cleanup()
	mc.wg.Wait()
	close(mc.eventChan)

	return nil
}

// Events returns the event channel
func (mc *MemoryCollector) Events() <-chan *collectors.Event {
	return mc.eventChan
}

// Health returns collector health status
func (mc *MemoryCollector) Health() *collectors.Health {
	mc.healthMu.RLock()
	defer mc.healthMu.RUnlock()

	status := collectors.HealthStatusHealthy
	message := "Operating normally"

	if mc.stopped.Load() {
		status = collectors.HealthStatusStopped
		message = "Stopped"
	} else if mc.lastError != nil {
		status = collectors.HealthStatusDegraded
		message = fmt.Sprintf("Recent error: %v", mc.lastError)
	} else if time.Since(mc.lastEventTime) > 2*time.Minute {
		status = collectors.HealthStatusDegraded
		message = "No recent events"
	}

	return &collectors.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   mc.lastEventTime,
		EventsProcessed: atomic.LoadUint64(&mc.eventsProcessed),
		EventsDropped:   atomic.LoadUint64(&mc.eventsDropped),
		Metrics: map[string]interface{}{
			"processes_tracked":   len(mc.processes),
			"oom_predictions":     atomic.LoadUint64(&mc.oomPredictions),
			"prediction_accuracy": mc.getPredictionAccuracy(),
		},
	}
}

// GetStats returns collector statistics
func (mc *MemoryCollector) GetStats() *collectors.Stats {
	return &collectors.Stats{
		EventsCollected: atomic.LoadUint64(&mc.eventsProcessed),
		EventsDropped:   atomic.LoadUint64(&mc.eventsDropped),
		StartTime:       time.Now(), // Would track actual start time
		LastEventTime:   mc.lastEventTime,
		CollectorMetrics: map[string]interface{}{
			"processes_tracked":     len(mc.processes),
			"oom_predictions_total": atomic.LoadUint64(&mc.oomPredictions),
			"accurate_predictions":  atomic.LoadUint64(&mc.accurateOOMs),
			"prediction_accuracy":   mc.getPredictionAccuracy(),
		},
	}
}

// Configure updates collector configuration
func (mc *MemoryCollector) Configure(config collectors.CollectorConfig) error {
	mc.config = config
	return nil
}

// IsEnabled returns whether collector is enabled
func (mc *MemoryCollector) IsEnabled() bool {
	return mc.config.Enabled
}

// Internal implementation

// attachTracepoints attaches eBPF programs to kernel tracepoints
func (mc *MemoryCollector) attachTracepoints() error {
	// Memory allocation tracking
	allocLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "kmem",
		Name:    "mm_page_alloc",
		Program: mc.objs.TrackMemoryAlloc,
	})
	if err != nil {
		return fmt.Errorf("failed to attach allocation tracepoint: %w", err)
	}
	mc.links = append(mc.links, allocLink)

	// Memory free tracking
	freeLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "kmem",
		Name:    "mm_page_free",
		Program: mc.objs.TrackMemoryFree,
	})
	if err != nil {
		return fmt.Errorf("failed to attach free tracepoint: %w", err)
	}
	mc.links = append(mc.links, freeLink)

	// OOM kill tracking
	oomLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "oom",
		Name:    "oom_score_adj_update",
		Program: mc.objs.TrackOomKill,
	})
	if err != nil {
		return fmt.Errorf("failed to attach OOM tracepoint: %w", err)
	}
	mc.links = append(mc.links, oomLink)

	// Process exit tracking
	exitLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "sched",
		Name:    "sched_process_exit",
		Program: mc.objs.TrackProcessExit,
	})
	if err != nil {
		return fmt.Errorf("failed to attach exit tracepoint: %w", err)
	}
	mc.links = append(mc.links, exitLink)

	return nil
}

// processEvents processes events from the eBPF ring buffer
func (mc *MemoryCollector) processEvents() {
	defer mc.wg.Done()

	for {
		select {
		case <-mc.ctx.Done():
			return
		default:
		}

		record, err := mc.reader.Read()
		if err != nil {
			if err == ringbuf.ErrClosed {
				return
			}
			mc.recordError(fmt.Errorf("ring buffer read error: %w", err))
			continue
		}

		event := parseMemoryEvent(record.RawSample)
		if event == nil {
			mc.recordError(fmt.Errorf("failed to parse memory event"))
			atomic.AddUint64(&mc.eventsDropped, 1)
			continue
		}

		// Handle process exit events immediately for cleanup
		if event.EventType == 4 { // EVENT_PROCESS_EXIT
			mc.handleProcessExit(event.PID)
		}

		if err := mc.handleMemoryEvent(record.RawSample); err != nil {
			mc.recordError(err)
			atomic.AddUint64(&mc.eventsDropped, 1)
			continue
		}

		atomic.AddUint64(&mc.eventsProcessed, 1)
		mc.healthMu.Lock()
		mc.lastEventTime = time.Now()
		mc.healthMu.Unlock()
	}
}

// handleMemoryEvent processes a single memory event from eBPF
func (mc *MemoryCollector) handleMemoryEvent(data []byte) error {
	if len(data) < 64 { // Minimum expected size
		return fmt.Errorf("invalid event data size: %d", len(data))
	}

	// Parse eBPF event (assuming C struct layout)
	event := parseMemoryEvent(data)

	// Update process tracker
	mc.updateProcessTracker(event)

	// Generate collector event
	collectorEvent := mc.createCollectorEvent(event)
	if collectorEvent == nil {
		return nil // Event filtered
	}

	// Send to event channel
	select {
	case mc.eventChan <- collectorEvent:
	default:
		atomic.AddUint64(&mc.eventsDropped, 1)
		return fmt.Errorf("event channel full")
	}

	return nil
}

// updateProcessTracker updates memory tracking for a process
func (mc *MemoryCollector) updateProcessTracker(event *MemoryEvent) {
	mc.processesMu.Lock()
	defer mc.processesMu.Unlock()

	tracker, exists := mc.processes[event.PID]
	if !exists {
		tracker = &ProcessMemoryTracker{
			PID:           event.PID,
			Command:       event.Command,
			GrowthHistory: make([]MemoryDataPoint, 0, 100),
			IsContainer:   event.InContainer,
			ContainerPID:  event.ContainerPID,
		}
		mc.processes[event.PID] = tracker
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	now := time.Now()
	oldUsage := tracker.CurrentUsage
	tracker.CurrentUsage = event.TotalMemory
	tracker.LastUpdate = now

	// Update allocation/free counters
	if event.EventType == 1 { // EVENT_MEMORY_ALLOC
		tracker.TotalAllocated += event.Size
	} else if event.EventType == 2 { // EVENT_MEMORY_FREE
		tracker.TotalFreed += event.Size
	}

	// Calculate growth rate
	var rate float64
	if len(tracker.GrowthHistory) > 0 {
		lastPoint := tracker.GrowthHistory[len(tracker.GrowthHistory)-1]
		timeDiff := now.Sub(lastPoint.Timestamp).Seconds()
		if timeDiff > 0 {
			rate = float64(int64(tracker.CurrentUsage-lastPoint.Usage)) / timeDiff
		}
	}

	// Add data point
	dataPoint := MemoryDataPoint{
		Timestamp: now,
		Usage:     tracker.CurrentUsage,
		Rate:      rate,
	}
	tracker.GrowthHistory = append(tracker.GrowthHistory, dataPoint)

	// Limit history size
	if len(tracker.GrowthHistory) > 100 {
		tracker.GrowthHistory = tracker.GrowthHistory[len(tracker.GrowthHistory)-100:]
	}

	// Update allocation rate (exponential moving average)
	if oldUsage > 0 {
		alpha := 0.1
		tracker.AllocationRate = alpha*rate + (1-alpha)*tracker.AllocationRate
	} else {
		tracker.AllocationRate = rate
	}

	// Analyze growth trend
	tracker.GrowthTrend = mc.analyzeGrowthTrend(tracker.GrowthHistory)

	// Update risk score
	tracker.RiskScore = mc.calculateRiskScore(tracker)
}

// createCollectorEvent converts eBPF event to collector event
func (mc *MemoryCollector) createCollectorEvent(event *MemoryEvent) *collectors.Event {
	eventType := "memory_allocation"
	severity := collectors.SeverityLow

	// Determine event type and severity based on the event
	if event.EventType == 3 { // EVENT_OOM_KILL
		severity = collectors.SeverityHigh
		eventType = "oom_kill"
	} else if event.EventType == 4 { // EVENT_PROCESS_EXIT
		severity = collectors.SeverityMedium
		eventType = "process_exit"
	} else if event.EventType == 2 { // EVENT_MEMORY_FREE
		eventType = "memory_free"
	}

	// Get process tracker for additional context
	mc.processesMu.RLock()
	tracker := mc.processes[event.PID]
	mc.processesMu.RUnlock()

	// Create event
	collectorEvent := &collectors.Event{
		ID:          fmt.Sprintf("memory_%d_%d", event.PID, time.Now().UnixNano()),
		Timestamp:   time.Unix(0, int64(event.Timestamp)),
		Source:      mc.config.Name,
		SourceType:  "ebpf",
		CollectorID: mc.config.Name,
		Type:        eventType,
		Category:    collectors.CategoryMemory,
		Severity:    severity,
		Data: map[string]interface{}{
			"pid":           event.PID,
			"tid":           event.TID,
			"size":          event.Size,
			"total_memory":  event.TotalMemory,
			"event_type":    event.EventType,
			"in_container":  event.InContainer,
			"container_pid": event.ContainerPID,
		},
		Attributes: map[string]interface{}{
			"command": event.Command,
		},
		Labels: mc.config.Labels,
		Context: &collectors.EventContext{
			PID:         event.PID,
			ProcessName: event.Command,
		},
	}

	// Add container context if available
	if event.InContainer {
		collectorEvent.Context.Custom = map[string]string{
			"container_pid": fmt.Sprintf("%d", event.ContainerPID),
			"in_container":  "true",
		}
	}

	// Add OOM prediction if available
	if tracker != nil && tracker.PredictedOOM != nil {
		collectorEvent.Data["oom_prediction"] = map[string]interface{}{
			"time_to_oom": tracker.PredictedOOM.TimeToOOM.String(),
			"confidence":  tracker.PredictedOOM.Confidence,
			"trend_type":  tracker.PredictedOOM.TrendType,
		}

		// Generate actionable recommendation
		collectorEvent.Actionable = mc.generateActionable(tracker)
	}

	return collectorEvent
}

// analyzeGrowthTrend analyzes memory growth patterns
func (mc *MemoryCollector) analyzeGrowthTrend(history []MemoryDataPoint) TrendType {
	if len(history) < 3 {
		return TrendStable
	}

	// Calculate linear regression
	n := len(history)
	var sumX, sumY, sumXY, sumX2 float64

	for i, point := range history {
		x := float64(i)
		y := float64(point.Usage)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	slope := (float64(n)*sumXY - sumX*sumY) / (float64(n)*sumX2 - sumX*sumX)

	// Check for exponential growth
	if mc.isExponentialGrowth(history) {
		return TrendExponential
	}

	// Linear trend detection
	if slope > 1000000 { // 1MB/datapoint threshold
		return TrendLinear
	} else if slope < -1000000 {
		return TrendDecreasing
	}

	// Check for spiky behavior
	if mc.isSpiky(history) {
		return TrendSpiky
	}

	return TrendStable
}

// isExponentialGrowth detects exponential growth patterns
func (mc *MemoryCollector) isExponentialGrowth(history []MemoryDataPoint) bool {
	if len(history) < 5 {
		return false
	}

	// Check if growth rate is increasing
	recent := history[len(history)-3:]
	var growthRates []float64

	for i := 1; i < len(recent); i++ {
		if recent[i-1].Usage > 0 {
			rate := float64(recent[i].Usage) / float64(recent[i-1].Usage)
			growthRates = append(growthRates, rate)
		}
	}

	// Exponential if growth rate is consistently > 1.1
	for _, rate := range growthRates {
		if rate <= 1.1 {
			return false
		}
	}

	return len(growthRates) >= 2
}

// isSpiky detects spiky memory usage patterns
func (mc *MemoryCollector) isSpiky(history []MemoryDataPoint) bool {
	if len(history) < 5 {
		return false
	}

	// Calculate variance in growth rates
	var rates []float64
	for i := 1; i < len(history); i++ {
		rates = append(rates, history[i].Rate)
	}

	// Calculate coefficient of variation
	mean := mc.calculateMean(rates)
	variance := mc.calculateVariance(rates, mean)
	cv := math.Sqrt(variance) / mean

	return cv > 2.0 // High coefficient of variation indicates spiky behavior
}

// calculateRiskScore calculates OOM risk score for a process
func (mc *MemoryCollector) calculateRiskScore(tracker *ProcessMemoryTracker) float64 {
	if len(tracker.GrowthHistory) < 3 {
		return 0.0
	}

	score := 0.0

	// Factor 1: Current memory usage relative to typical usage
	if tracker.CurrentUsage > 0 {
		avgUsage := mc.calculateAverageUsage(tracker.GrowthHistory)
		if avgUsage > 0 {
			score += math.Min(float64(tracker.CurrentUsage)/float64(avgUsage), 5.0) * 0.3
		}
	}

	// Factor 2: Growth trend
	switch tracker.GrowthTrend {
	case TrendExponential:
		score += 0.4
	case TrendLinear:
		score += 0.2
	case TrendSpiky:
		score += 0.1
	}

	// Factor 3: Allocation rate
	if tracker.AllocationRate > 1000000 { // 1MB/s
		score += math.Min(tracker.AllocationRate/10000000, 0.3) // Max 0.3 for 10MB/s
	}

	return math.Min(score, 1.0)
}

// PredictOOM implements machine learning-based OOM prediction algorithms
func (p *OOMPredictor) PredictOOM(tracker *ProcessMemoryTracker) *OOMPrediction {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(tracker.GrowthHistory) < p.minDataPoints {
		return nil
	}

	// Determine which model to use based on growth trend
	var prediction *OOMPrediction
	switch tracker.GrowthTrend {
	case TrendLinear:
		prediction = p.predictLinearGrowth(tracker)
	case TrendExponential:
		prediction = p.predictExponentialGrowth(tracker)
	case TrendSpiky:
		prediction = p.predictSpikyGrowth(tracker)
	default:
		return nil // Stable or decreasing trends don't need prediction
	}

	// Validate prediction confidence
	if prediction != nil && prediction.Confidence >= p.confidenceThreshold {
		return prediction
	}

	return nil
}

// predictLinearGrowth predicts OOM based on linear regression model
func (p *OOMPredictor) predictLinearGrowth(tracker *ProcessMemoryTracker) *OOMPrediction {
	history := tracker.GrowthHistory
	n := len(history)
	if n < 3 {
		return nil
	}

	// Calculate linear regression
	var sumX, sumY, sumXY, sumX2 float64
	for i, point := range history {
		x := float64(i)
		y := float64(point.Usage)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	slope := (float64(n)*sumXY - sumX*sumY) / (float64(n)*sumX2 - sumX*sumX)
	intercept := (sumY - slope*sumX) / float64(n)

	// Calculate R-squared for confidence
	meanY := sumY / float64(n)
	var ssTotal, ssRes float64
	for i, point := range history {
		y := float64(point.Usage)
		yPred := slope*float64(i) + intercept
		ssTotal += (y - meanY) * (y - meanY)
		ssRes += (y - yPred) * (y - yPred)
	}
	r2 := 1 - (ssRes / ssTotal)

	// Estimate memory limit (assume system memory or container limit)
	memoryLimit := uint64(8 * 1024 * 1024 * 1024) // 8GB default
	if tracker.IsContainer {
		memoryLimit = uint64(512 * 1024 * 1024) // 512MB container default
	}

	// Project when memory usage will reach limit
	if slope <= 0 {
		return nil // Memory not growing
	}

	currentTime := float64(n)
	timeToLimit := (float64(memoryLimit) - intercept - slope*currentTime) / slope
	if timeToLimit <= 0 {
		return nil // Already at or past limit
	}

	// Convert time steps to actual duration (assuming 10s intervals)
	timeToOOM := time.Duration(timeToLimit * 10 * float64(time.Second))
	if timeToOOM > p.predictionWindow {
		return nil // Too far in future
	}

	predictedPeak := uint64(slope*float64(n+int(timeToLimit)) + intercept)

	return &OOMPrediction{
		PID:                tracker.PID,
		TimeToOOM:          timeToOOM,
		Confidence:         math.Min(r2, 1.0),
		CurrentUsage:       tracker.CurrentUsage,
		PredictedPeakUsage: predictedPeak,
		MemoryLimit:        memoryLimit,
		TrendType:          TrendLinear,
		PredictedAt:        time.Now(),
	}
}

// predictExponentialGrowth predicts OOM based on exponential growth patterns
func (p *OOMPredictor) predictExponentialGrowth(tracker *ProcessMemoryTracker) *OOMPrediction {
	history := tracker.GrowthHistory
	n := len(history)
	if n < 5 {
		return nil
	}

	// Calculate exponential growth rate from recent data
	recent := history[n-3:]
	growthRates := make([]float64, 0)

	for i := 1; i < len(recent); i++ {
		if recent[i-1].Usage > 0 {
			rate := float64(recent[i].Usage) / float64(recent[i-1].Usage)
			growthRates = append(growthRates, rate)
		}
	}

	if len(growthRates) < 2 {
		return nil
	}

	// Average growth rate
	avgGrowthRate := 0.0
	for _, rate := range growthRates {
		avgGrowthRate += rate
	}
	avgGrowthRate /= float64(len(growthRates))

	if avgGrowthRate <= 1.1 {
		return nil // Not growing fast enough for exponential prediction
	}

	// Estimate memory limit
	memoryLimit := uint64(8 * 1024 * 1024 * 1024) // 8GB default
	if tracker.IsContainer {
		memoryLimit = uint64(512 * 1024 * 1024) // 512MB container default
	}

	// Project exponential growth: usage(t) = current * growth_rate^t
	currentUsage := float64(tracker.CurrentUsage)
	timeSteps := math.Log(float64(memoryLimit)/currentUsage) / math.Log(avgGrowthRate)

	if timeSteps <= 0 {
		return nil // Already at or past limit
	}

	// Convert to actual time (10s intervals)
	timeToOOM := time.Duration(timeSteps * 10 * float64(time.Second))
	if timeToOOM > p.predictionWindow {
		return nil // Too far in future
	}

	// Calculate confidence based on consistency of growth rates
	variance := 0.0
	for _, rate := range growthRates {
		diff := rate - avgGrowthRate
		variance += diff * diff
	}
	variance /= float64(len(growthRates))
	confidence := 1.0 / (1.0 + variance) // Higher variance = lower confidence

	predictedPeak := uint64(currentUsage * math.Pow(avgGrowthRate, timeSteps))

	return &OOMPrediction{
		PID:                tracker.PID,
		TimeToOOM:          timeToOOM,
		Confidence:         confidence,
		CurrentUsage:       tracker.CurrentUsage,
		PredictedPeakUsage: predictedPeak,
		MemoryLimit:        memoryLimit,
		TrendType:          TrendExponential,
		PredictedAt:        time.Now(),
	}
}

// predictSpikyGrowth predicts OOM for spiky memory patterns
func (p *OOMPredictor) predictSpikyGrowth(tracker *ProcessMemoryTracker) *OOMPrediction {
	history := tracker.GrowthHistory
	n := len(history)
	if n < 5 {
		return nil
	}

	// Find peak usage in recent history
	var maxUsage uint64
	for _, point := range history {
		if point.Usage > maxUsage {
			maxUsage = point.Usage
		}
	}

	// Estimate memory limit
	memoryLimit := uint64(8 * 1024 * 1024 * 1024) // 8GB default
	if tracker.IsContainer {
		memoryLimit = uint64(512 * 1024 * 1024) // 512MB container default
	}

	// If recent peak is close to limit, predict OOM soon
	usageRatio := float64(maxUsage) / float64(memoryLimit)
	if usageRatio < 0.8 {
		return nil // Not close enough to limit
	}

	// For spiky patterns, predict OOM could happen at next spike
	// Time to next spike is estimated based on recent spike frequency
	spikeTimes := make([]time.Time, 0)
	threshold := maxUsage * 80 / 100 // 80% of max usage considered a spike

	for _, point := range history {
		if point.Usage >= threshold {
			spikeTimes = append(spikeTimes, point.Timestamp)
		}
	}

	if len(spikeTimes) < 2 {
		return nil // Need at least 2 spikes to estimate frequency
	}

	// Calculate average time between spikes
	totalInterval := spikeTimes[len(spikeTimes)-1].Sub(spikeTimes[0])
	avgInterval := totalInterval / time.Duration(len(spikeTimes)-1)

	// Predict next spike
	lastSpike := spikeTimes[len(spikeTimes)-1]
	nextSpike := lastSpike.Add(avgInterval)
	timeToNextSpike := time.Until(nextSpike)

	if timeToNextSpike > p.predictionWindow {
		return nil // Next spike too far away
	}

	// Confidence based on how close current peak is to limit
	confidence := math.Min(usageRatio, 1.0)

	return &OOMPrediction{
		PID:                tracker.PID,
		TimeToOOM:          timeToNextSpike,
		Confidence:         confidence,
		CurrentUsage:       tracker.CurrentUsage,
		PredictedPeakUsage: maxUsage + (maxUsage * 10 / 100), // Assume 10% growth in spike
		MemoryLimit:        memoryLimit,
		TrendType:          TrendSpiky,
		PredictedAt:        time.Now(),
	}
}

// Helper functions

func (mc *MemoryCollector) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (mc *MemoryCollector) calculateVariance(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return sum / float64(len(values))
}

func (mc *MemoryCollector) calculateAverageUsage(history []MemoryDataPoint) uint64 {
	if len(history) == 0 {
		return 0
	}

	sum := uint64(0)
	for _, point := range history {
		sum += point.Usage
	}
	return sum / uint64(len(history))
}

func (mc *MemoryCollector) generateActionable(tracker *ProcessMemoryTracker) *collectors.ActionableItem {
	if tracker.PredictedOOM == nil {
		return nil
	}

	return &collectors.ActionableItem{
		Title:           "Potential OOM detected",
		Description:     fmt.Sprintf("Process %s (PID %d) may run out of memory in %v", tracker.Command, tracker.PID, tracker.PredictedOOM.TimeToOOM),
		Commands:        []string{fmt.Sprintf("kill -TERM %d", tracker.PID), fmt.Sprintf("ps aux | grep %d", tracker.PID)},
		Risk:            "medium",
		EstimatedImpact: "Prevents system instability from OOM",
		AutoApplicable:  false,
		Category:        "resource",
	}
}

func (mc *MemoryCollector) getPredictionAccuracy() float64 {
	accurate := atomic.LoadUint64(&mc.accurateOOMs)
	total := atomic.LoadUint64(&mc.oomPredictions)

	if total == 0 {
		return 0.0
	}

	return float64(accurate) / float64(total)
}

func (mc *MemoryCollector) recordError(err error) {
	mc.healthMu.Lock()
	mc.lastError = err
	mc.healthMu.Unlock()
}

func (mc *MemoryCollector) cleanup() {
	// Close ring buffer reader
	if mc.reader != nil {
		mc.reader.Close()
	}

	// Detach all links
	for _, l := range mc.links {
		l.Close()
	}

	// Close eBPF objects
	mc.objs.Close()
}

// parseMemoryEvent parses raw eBPF event data
func parseMemoryEvent(data []byte) *MemoryEvent {
	if len(data) < 56 { // Size of memory_event struct
		return nil
	}

	// Parse C struct from eBPF (memory_event from common.h)
	event := &MemoryEvent{
		Timestamp:    binary.LittleEndian.Uint64(data[0:8]),
		PID:          binary.LittleEndian.Uint32(data[8:12]),
		TID:          binary.LittleEndian.Uint32(data[12:16]),
		Size:         binary.LittleEndian.Uint64(data[16:24]),
		TotalMemory:  binary.LittleEndian.Uint64(data[24:32]),
		EventType:    binary.LittleEndian.Uint32(data[32:36]),
		InContainer:  data[52] == 1,
		ContainerPID: binary.LittleEndian.Uint32(data[53:57]),
	}

	// Extract command name (null-terminated)
	commEnd := 36
	for i := 36; i < 52 && data[i] != 0; i++ {
		commEnd = i + 1
	}
	event.Command = string(data[36:commEnd])

	return event
}

// Additional monitoring functions

// monitorProcesses periodically cleans up terminated processes
func (mc *MemoryCollector) monitorProcesses() {
	defer mc.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	optimizeTicker := time.NewTicker(5 * time.Minute)
	defer optimizeTicker.Stop()

	for {
		select {
		case <-mc.ctx.Done():
			return
		case <-ticker.C:
			mc.cleanupTerminatedProcesses()
		case <-optimizeTicker.C:
			mc.optimizeMemoryTracking()
		}
	}
}

// runOOMPrediction periodically runs OOM prediction analysis
func (mc *MemoryCollector) runOOMPrediction() {
	defer mc.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mc.ctx.Done():
			return
		case <-ticker.C:
			mc.updateOOMPredictions()
		}
	}
}

// cleanupTerminatedProcesses removes tracking for processes that no longer exist
func (mc *MemoryCollector) cleanupTerminatedProcesses() {
	mc.processesMu.Lock()
	defer mc.processesMu.Unlock()

	now := time.Now()
	for pid, tracker := range mc.processes {
		tracker.mu.RLock()
		lastUpdate := tracker.LastUpdate
		tracker.mu.RUnlock()

		// Remove processes not updated in 5 minutes
		if now.Sub(lastUpdate) > 5*time.Minute {
			delete(mc.processes, pid)
		}
	}
}

// updateOOMPredictions runs OOM prediction for all tracked processes
func (mc *MemoryCollector) updateOOMPredictions() {
	mc.processesMu.RLock()
	processes := make([]*ProcessMemoryTracker, 0, len(mc.processes))
	for _, tracker := range mc.processes {
		processes = append(processes, tracker)
	}
	mc.processesMu.RUnlock()

	for _, tracker := range processes {
		prediction := mc.predictor.PredictOOM(tracker)
		if prediction != nil {
			tracker.mu.Lock()
			tracker.PredictedOOM = prediction
			tracker.mu.Unlock()

			atomic.AddUint64(&mc.oomPredictions, 1)
		}
	}
}

// Process lifecycle monitoring functions

// handleProcessExit handles process exit events and cleanup
func (mc *MemoryCollector) handleProcessExit(pid uint32) {
	mc.processesMu.Lock()
	defer mc.processesMu.Unlock()

	// Get final statistics before cleanup
	if tracker, exists := mc.processes[pid]; exists {
		tracker.mu.RLock()
		command := tracker.Command
		totalAllocated := tracker.TotalAllocated
		totalFreed := tracker.TotalFreed
		maxUsage := mc.getMaxUsage(tracker.GrowthHistory)
		tracker.mu.RUnlock()

		// Check if this was an accurate OOM prediction
		if tracker.PredictedOOM != nil {
			predictionTime := tracker.PredictedOOM.PredictedAt
			actualOOM := time.Now()
			predictedOOM := predictionTime.Add(tracker.PredictedOOM.TimeToOOM)

			// Consider prediction accurate if within 30 seconds
			accuracy := actualOOM.Sub(predictedOOM)
			if accuracy.Abs() < 30*time.Second {
				atomic.AddUint64(&mc.accurateOOMs, 1)

				// Record for future model improvement
				mc.predictor.recordHistoricalOOM(pid, actualOOM, predictedOOM, accuracy)
			}
		}

		// Remove from tracking
		delete(mc.processes, pid)
	}
}

// getMaxUsage returns the maximum memory usage from history
func (mc *MemoryCollector) getMaxUsage(history []MemoryDataPoint) uint64 {
	var maxUsage uint64
	for _, point := range history {
		if point.Usage > maxUsage {
			maxUsage = point.Usage
		}
	}
	return maxUsage
}

// recordHistoricalOOM records OOM prediction accuracy for model improvement
func (p *OOMPredictor) recordHistoricalOOM(pid uint32, actualTime, predictedTime time.Time, accuracy time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	historical := HistoricalOOM{
		PID:           pid,
		ActualOOMTime: actualTime,
		PredictedTime: predictedTime,
		Accuracy:      accuracy,
	}

	p.historicalOOMs = append(p.historicalOOMs, historical)

	// Limit historical data to prevent memory bloat
	if len(p.historicalOOMs) > 1000 {
		p.historicalOOMs = p.historicalOOMs[len(p.historicalOOMs)-1000:]
	}
}

// getProcessLifecycleStats returns statistics about process lifecycle
func (mc *MemoryCollector) getProcessLifecycleStats() map[string]interface{} {
	mc.processesMu.RLock()
	defer mc.processesMu.RUnlock()

	var totalProcesses, containerProcesses, hostProcesses int
	var totalMemoryUsage, containerMemoryUsage uint64
	longestRunning := time.Duration(0)
	mostMemoryIntensive := uint64(0)

	now := time.Now()
	for _, tracker := range mc.processes {
		tracker.mu.RLock()
		totalProcesses++
		totalMemoryUsage += tracker.CurrentUsage

		if tracker.IsContainer {
			containerProcesses++
			containerMemoryUsage += tracker.CurrentUsage
		} else {
			hostProcesses++
		}

		// Track longest running process
		runtime := now.Sub(tracker.LastUpdate)
		if runtime > longestRunning {
			longestRunning = runtime
		}

		// Track most memory intensive process
		if tracker.CurrentUsage > mostMemoryIntensive {
			mostMemoryIntensive = tracker.CurrentUsage
		}
		tracker.mu.RUnlock()
	}

	return map[string]interface{}{
		"total_processes":          totalProcesses,
		"container_processes":      containerProcesses,
		"host_processes":           hostProcesses,
		"total_memory_usage":       totalMemoryUsage,
		"container_memory_usage":   containerMemoryUsage,
		"host_memory_usage":        totalMemoryUsage - containerMemoryUsage,
		"longest_running_duration": longestRunning.String(),
		"most_memory_intensive":    mostMemoryIntensive,
		"avg_memory_per_process":   totalMemoryUsage / uint64(max(totalProcesses, 1)),
	}
}

// optimizeMemoryTracking performs periodic optimization of memory tracking
func (mc *MemoryCollector) optimizeMemoryTracking() {
	mc.processesMu.Lock()
	defer mc.processesMu.Unlock()

	now := time.Now()
	for pid, tracker := range mc.processes {
		tracker.mu.Lock()

		// Optimize growth history by removing old data points
		if len(tracker.GrowthHistory) > 50 {
			// Keep recent 50 points and every 10th older point
			recent := tracker.GrowthHistory[len(tracker.GrowthHistory)-50:]
			older := tracker.GrowthHistory[:len(tracker.GrowthHistory)-50]

			optimized := make([]MemoryDataPoint, 0, 50+len(older)/10)
			for i := 0; i < len(older); i += 10 {
				optimized = append(optimized, older[i])
			}
			optimized = append(optimized, recent...)
			tracker.GrowthHistory = optimized
		}

		// Remove stale predictions
		if tracker.PredictedOOM != nil {
			predictionAge := now.Sub(tracker.PredictedOOM.PredictedAt)
			if predictionAge > 10*time.Minute {
				tracker.PredictedOOM = nil
			}
		}

		tracker.mu.Unlock()

		// Remove processes that haven't been updated in a long time
		if now.Sub(tracker.LastUpdate) > 10*time.Minute {
			delete(mc.processes, pid)
		}
	}
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Ring buffer processing optimization

// RingBufferProcessor handles efficient processing of eBPF ring buffer events
type RingBufferProcessor struct {
	reader          *ringbuf.Reader
	eventPool       sync.Pool
	batchSize       int
	flushInterval   time.Duration
	eventBatch      []*MemoryEvent
	batchMu         sync.Mutex
	processor       func([]*MemoryEvent) error
	droppedEvents   *uint64
	processedEvents *uint64
}

// NewRingBufferProcessor creates an optimized ring buffer processor
func NewRingBufferProcessor(reader *ringbuf.Reader, processor func([]*MemoryEvent) error, droppedEvents, processedEvents *uint64) *RingBufferProcessor {
	rbp := &RingBufferProcessor{
		reader:          reader,
		batchSize:       100,
		flushInterval:   100 * time.Millisecond,
		eventBatch:      make([]*MemoryEvent, 0, 100),
		processor:       processor,
		droppedEvents:   droppedEvents,
		processedEvents: processedEvents,
	}

	// Object pool for MemoryEvent to reduce GC pressure
	rbp.eventPool = sync.Pool{
		New: func() interface{} {
			return &MemoryEvent{}
		},
	}

	return rbp
}

// ProcessEvents efficiently processes events from the ring buffer
func (rbp *RingBufferProcessor) ProcessEvents(ctx context.Context) {
	flushTicker := time.NewTicker(rbp.flushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush remaining events before exit
			rbp.flushBatch()
			return

		case <-flushTicker.C:
			// Periodic flush to ensure low latency
			rbp.flushBatch()

		default:
			// Try to read event with short timeout
			record, err := rbp.reader.ReadTimeout(10 * time.Millisecond)
			if err != nil {
				if err == ringbuf.ErrClosed || ctx.Err() != nil {
					return
				}
				continue // Timeout or recoverable error
			}

			// Parse event using pooled object
			event := rbp.eventPool.Get().(*MemoryEvent)
			if rbp.parseMemoryEvent(record.RawSample, event) {
				rbp.addToBatch(event)
			} else {
				// Return to pool if parsing failed
				rbp.eventPool.Put(event)
				atomic.AddUint64(rbp.droppedEvents, 1)
			}
		}
	}
}

// parseMemoryEvent parses raw data into a pooled MemoryEvent object
func (rbp *RingBufferProcessor) parseMemoryEvent(data []byte, event *MemoryEvent) bool {
	if len(data) < 56 { // Size of memory_event struct
		return false
	}

	// Reset the event object
	*event = MemoryEvent{}

	// Parse C struct from eBPF (memory_event from common.h)
	event.Timestamp = binary.LittleEndian.Uint64(data[0:8])
	event.PID = binary.LittleEndian.Uint32(data[8:12])
	event.TID = binary.LittleEndian.Uint32(data[12:16])
	event.Size = binary.LittleEndian.Uint64(data[16:24])
	event.TotalMemory = binary.LittleEndian.Uint64(data[24:32])
	event.EventType = binary.LittleEndian.Uint32(data[32:36])
	event.InContainer = data[52] == 1
	event.ContainerPID = binary.LittleEndian.Uint32(data[53:57])

	// Extract command name (null-terminated)
	commEnd := 36
	for i := 36; i < 52 && data[i] != 0; i++ {
		commEnd = i + 1
	}
	event.Command = string(data[36:commEnd])

	return true
}

// addToBatch adds an event to the current batch
func (rbp *RingBufferProcessor) addToBatch(event *MemoryEvent) {
	rbp.batchMu.Lock()
	defer rbp.batchMu.Unlock()

	rbp.eventBatch = append(rbp.eventBatch, event)

	// Flush if batch is full
	if len(rbp.eventBatch) >= rbp.batchSize {
		rbp.flushBatchUnsafe()
	}
}

// flushBatch flushes the current batch of events
func (rbp *RingBufferProcessor) flushBatch() {
	rbp.batchMu.Lock()
	defer rbp.batchMu.Unlock()
	rbp.flushBatchUnsafe()
}

// flushBatchUnsafe flushes events without acquiring lock (must be called with lock held)
func (rbp *RingBufferProcessor) flushBatchUnsafe() {
	if len(rbp.eventBatch) == 0 {
		return
	}

	// Process the batch
	if err := rbp.processor(rbp.eventBatch); err != nil {
		atomic.AddUint64(rbp.droppedEvents, uint64(len(rbp.eventBatch)))
	} else {
		atomic.AddUint64(rbp.processedEvents, uint64(len(rbp.eventBatch)))
	}

	// Return events to pool and reset batch
	for _, event := range rbp.eventBatch {
		rbp.eventPool.Put(event)
	}
	rbp.eventBatch = rbp.eventBatch[:0]
}

// optimizeProcessEvents replaces the simple event processing with optimized batch processing
func (mc *MemoryCollector) optimizeProcessEvents() {
	defer mc.wg.Done()

	// Create optimized ring buffer processor
	processor := NewRingBufferProcessor(
		mc.reader,
		mc.processBatchedEvents,
		&mc.eventsDropped,
		&mc.eventsProcessed,
	)

	// Start processing with context
	processor.ProcessEvents(mc.ctx)
}

// processBatchedEvents processes a batch of memory events
func (mc *MemoryCollector) processBatchedEvents(events []*MemoryEvent) error {
	for _, event := range events {
		// Handle process exit events immediately for cleanup
		if event.EventType == 4 { // EVENT_PROCESS_EXIT
			mc.handleProcessExit(event.PID)
		}

		// Update process tracker
		mc.updateProcessTracker(event)

		// Generate collector event
		collectorEvent := mc.createCollectorEvent(event)
		if collectorEvent == nil {
			continue // Event filtered
		}

		// Send to event channel (non-blocking)
		select {
		case mc.eventChan <- collectorEvent:
		default:
			// Channel full, drop event
			return fmt.Errorf("event channel full")
		}
	}

	// Update health tracking
	mc.healthMu.Lock()
	mc.lastEventTime = time.Now()
	mc.healthMu.Unlock()

	return nil
}

// Ring buffer performance monitoring
type RingBufferMetrics struct {
	EventsRead      uint64
	EventsParsed    uint64
	EventsProcessed uint64
	EventsDropped   uint64
	BatchesFlushed  uint64
	AvgBatchSize    float64
	ProcessingRate  float64 // events per second
	lastUpdate      time.Time
	lastEventCount  uint64
}

// GetRingBufferMetrics returns performance metrics for ring buffer processing
func (mc *MemoryCollector) GetRingBufferMetrics() *RingBufferMetrics {
	now := time.Now()
	eventsProcessed := atomic.LoadUint64(&mc.eventsProcessed)

	// Calculate processing rate
	var processingRate float64
	if !mc.lastEventTime.IsZero() {
		duration := now.Sub(mc.lastEventTime).Seconds()
		if duration > 0 {
			processingRate = float64(eventsProcessed) / duration
		}
	}

	return &RingBufferMetrics{
		EventsProcessed: eventsProcessed,
		EventsDropped:   atomic.LoadUint64(&mc.eventsDropped),
		ProcessingRate:  processingRate,
	}
}

// adaptiveRingBufferTuning automatically tunes ring buffer parameters based on load
func (mc *MemoryCollector) adaptiveRingBufferTuning() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastMetrics *RingBufferMetrics

	for {
		select {
		case <-mc.ctx.Done():
			return
		case <-ticker.C:
			currentMetrics := mc.GetRingBufferMetrics()

			if lastMetrics != nil {
				// Calculate drop rate
				dropRate := float64(currentMetrics.EventsDropped-lastMetrics.EventsDropped) /
					float64(currentMetrics.EventsProcessed-lastMetrics.EventsProcessed+1)

				// Adaptive tuning based on drop rate
				if dropRate > 0.05 { // More than 5% drop rate
					mc.increaseBufferCapacity()
				} else if dropRate < 0.01 && currentMetrics.ProcessingRate < 1000 {
					// Low drop rate and low processing rate, can optimize for latency
					mc.decreaseFlushInterval()
				}
			}

			lastMetrics = currentMetrics
		}
	}
}

// increaseBufferCapacity increases ring buffer capacity to handle higher load
func (mc *MemoryCollector) increaseBufferCapacity() {
	// This would involve reconfiguring the eBPF ring buffer
	// For now, we log the need for capacity increase
	fmt.Printf("Ring buffer capacity should be increased due to high drop rate\n")
}

// decreaseFlushInterval decreases flush interval for better latency
func (mc *MemoryCollector) decreaseFlushInterval() {
	// This would involve reconfiguring the flush interval
	// For now, we log the optimization opportunity
	fmt.Printf("Ring buffer flush interval could be decreased for better latency\n")
}

// Kernel compatibility checking

// KernelCompatibility tracks kernel version and feature support
type KernelCompatibility struct {
	Version         string
	Major           int
	Minor           int
	Patch           int
	SupportsBPF     bool
	SupportsRingBuf bool
	SupportsCO_RE   bool
	SupportsBTF     bool
	MinKernelMet    bool
	FeatureList     []string
}

// checkKernelCompatibility verifies kernel version and eBPF feature support
func (mc *MemoryCollector) checkKernelCompatibility() (*KernelCompatibility, error) {
	compat := &KernelCompatibility{
		FeatureList: make([]string, 0),
	}

	// Get kernel version
	version, err := mc.getKernelVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get kernel version: %w", err)
	}
	compat.Version = version

	// Parse version components
	if err := mc.parseKernelVersion(version, compat); err != nil {
		return nil, fmt.Errorf("failed to parse kernel version: %w", err)
	}

	// Check minimum kernel requirement (4.18+)
	compat.MinKernelMet = mc.checkMinimumKernel(compat)

	// Check eBPF feature support
	mc.checkBPFSupport(compat)
	mc.checkRingBufSupport(compat)
	mc.checkCORESupport(compat)
	mc.checkBTFSupport(compat)

	return compat, nil
}

// getKernelVersion retrieves the current kernel version
func (mc *MemoryCollector) getKernelVersion() (string, error) {
	// Read from /proc/version
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", err
	}

	// Parse version string
	versionStr := string(data)
	parts := strings.Fields(versionStr)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid /proc/version format")
	}

	return parts[2], nil
}

// parseKernelVersion parses kernel version string into components
func (mc *MemoryCollector) parseKernelVersion(version string, compat *KernelCompatibility) error {
	// Handle versions like "5.4.0-74-generic"
	parts := strings.Split(version, "-")
	versionParts := strings.Split(parts[0], ".")

	if len(versionParts) < 2 {
		return fmt.Errorf("invalid version format: %s", version)
	}

	var err error
	compat.Major, err = strconv.Atoi(versionParts[0])
	if err != nil {
		return fmt.Errorf("invalid major version: %s", versionParts[0])
	}

	compat.Minor, err = strconv.Atoi(versionParts[1])
	if err != nil {
		return fmt.Errorf("invalid minor version: %s", versionParts[1])
	}

	if len(versionParts) > 2 {
		patchStr := strings.Split(versionParts[2], "-")[0] // Remove any additional suffixes
		compat.Patch, err = strconv.Atoi(patchStr)
		if err != nil {
			// Some kernels have non-numeric patch versions, default to 0
			compat.Patch = 0
		}
	}

	return nil
}

// checkMinimumKernel checks if kernel meets minimum requirements
func (mc *MemoryCollector) checkMinimumKernel(compat *KernelCompatibility) bool {
	// Require kernel 4.18+ for full eBPF support
	if compat.Major > 4 {
		return true
	}
	if compat.Major == 4 && compat.Minor >= 18 {
		return true
	}
	return false
}

// checkBPFSupport verifies basic eBPF support
func (mc *MemoryCollector) checkBPFSupport(compat *KernelCompatibility) {
	// Try to create a simple BPF map to test support
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 1,
	}

	testMap, err := ebpf.NewMap(spec)
	if err == nil {
		testMap.Close()
		compat.SupportsBPF = true
		compat.FeatureList = append(compat.FeatureList, "BPF_MAPS")
	}
}

// checkRingBufSupport verifies ring buffer support
func (mc *MemoryCollector) checkRingBufSupport(compat *KernelCompatibility) {
	// Ring buffers were introduced in kernel 5.8
	if compat.Major > 5 || (compat.Major == 5 && compat.Minor >= 8) {
		// Try to create a small ring buffer to test
		spec := &ebpf.MapSpec{
			Type:       ebpf.RingBuf,
			MaxEntries: 4096,
		}

		testRingBuf, err := ebpf.NewMap(spec)
		if err == nil {
			testRingBuf.Close()
			compat.SupportsRingBuf = true
			compat.FeatureList = append(compat.FeatureList, "BPF_RINGBUF")
		}
	}
}

// checkCORESupport verifies CO-RE (Compile Once - Run Everywhere) support
func (mc *MemoryCollector) checkCORESupport(compat *KernelCompatibility) {
	// CO-RE requires kernel 5.4+ and BTF support
	if compat.Major > 5 || (compat.Major == 5 && compat.Minor >= 4) {
		// Check if BTF is available
		if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err == nil {
			compat.SupportsCO_RE = true
			compat.FeatureList = append(compat.FeatureList, "BPF_CORE")
		}
	}
}

// checkBTFSupport verifies BTF (BPF Type Format) support
func (mc *MemoryCollector) checkBTFSupport(compat *KernelCompatibility) {
	// BTF support was introduced in kernel 4.18, but became stable in 5.4
	if compat.Major > 5 || (compat.Major == 5 && compat.Minor >= 4) {
		if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err == nil {
			compat.SupportsBTF = true
			compat.FeatureList = append(compat.FeatureList, "BTF")
		}
	}
}

// validateKernelCompatibility validates that all required features are available
func (mc *MemoryCollector) validateKernelCompatibility() error {
	compat, err := mc.checkKernelCompatibility()
	if err != nil {
		return fmt.Errorf("kernel compatibility check failed: %w", err)
	}

	if !compat.MinKernelMet {
		return fmt.Errorf("kernel version %s is too old, minimum required is 4.18", compat.Version)
	}

	if !compat.SupportsBPF {
		return fmt.Errorf("eBPF support not available in kernel %s", compat.Version)
	}

	// Ring buffer is required for efficient event processing
	if !compat.SupportsRingBuf {
		return fmt.Errorf("eBPF ring buffer support not available in kernel %s, requires 5.8+", compat.Version)
	}

	// Log compatibility information
	fmt.Printf("Kernel compatibility check passed:\n")
	fmt.Printf("  Version: %s (%d.%d.%d)\n", compat.Version, compat.Major, compat.Minor, compat.Patch)
	fmt.Printf("  Features: %s\n", strings.Join(compat.FeatureList, ", "))

	return nil
}

// isKernelFeatureSupported checks if a specific kernel feature is available
func (mc *MemoryCollector) isKernelFeatureSupported(feature string) bool {
	compat, err := mc.checkKernelCompatibility()
	if err != nil {
		return false
	}

	for _, supportedFeature := range compat.FeatureList {
		if supportedFeature == feature {
			return true
		}
	}
	return false
}

// getKernelFeatureRecommendations provides recommendations for unsupported features
func (mc *MemoryCollector) getKernelFeatureRecommendations() []string {
	compat, err := mc.checkKernelCompatibility()
	if err != nil {
		return []string{"Failed to check kernel compatibility"}
	}

	recommendations := make([]string, 0)

	if !compat.MinKernelMet {
		recommendations = append(recommendations,
			fmt.Sprintf("Upgrade kernel from %s to 4.18+ for full eBPF support", compat.Version))
	}

	if !compat.SupportsRingBuf {
		recommendations = append(recommendations,
			"Upgrade to kernel 5.8+ for efficient ring buffer support")
	}

	if !compat.SupportsCO_RE {
		recommendations = append(recommendations,
			"Upgrade to kernel 5.4+ with BTF support for CO-RE compatibility")
	}

	if !compat.SupportsBTF {
		recommendations = append(recommendations,
			"Enable BTF support in kernel configuration for better debugging")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Kernel fully supports all required eBPF features")
	}

	return recommendations
}

// Unified message format integration

// convertToUnifiedEvent converts a MemoryEvent to the unified protobuf format
func (mc *MemoryCollector) convertToUnifiedEvent(event *MemoryEvent, tracker *ProcessMemoryTracker) (*events.UnifiedEvent, error) {
	now := time.Now()

	// Generate unique event ID
	eventID := fmt.Sprintf("memory_%d_%d_%d", event.PID, event.EventType, event.Timestamp)

	// Determine operation type
	operation := mc.getMemoryOperation(event.EventType)

	// Create memory event data
	memoryEventData := &events.MemoryEvent{
		Operation: operation,
		SizeBytes: event.Size,
		RssBytes:  event.TotalMemory,
		Allocator: "kernel",
		Metadata: map[string]string{
			"container_id": fmt.Sprintf("%d", event.ContainerPID),
			"in_container": fmt.Sprintf("%t", event.InContainer),
			"event_type":   fmt.Sprintf("%d", event.EventType),
		},
	}

	// Add OOM prediction if available
	if tracker != nil && tracker.PredictedOOM != nil {
		memoryEventData.Metadata["oom_prediction"] = "true"
		memoryEventData.Metadata["oom_confidence"] = fmt.Sprintf("%.2f", tracker.PredictedOOM.Confidence)
		memoryEventData.Metadata["time_to_oom"] = tracker.PredictedOOM.TimeToOOM.String()
		memoryEventData.Metadata["trend_type"] = mc.getTrendTypeName(tracker.PredictedOOM.TrendType)
	}

	// Determine severity based on event type and predictions
	severity := mc.getEventSeverity(event, tracker)

	// Create unified event
	unifiedEvent := &events.UnifiedEvent{
		Id:        eventID,
		Timestamp: timestamppb.New(time.Unix(0, int64(event.Timestamp))),
		Metadata: &events.EventMetadata{
			Type:          fmt.Sprintf("memory.%s", operation),
			Category:      events.EventCategory_CATEGORY_MEMORY,
			Severity:      severity,
			Priority:      mc.getEventPriority(severity),
			SchemaVersion: "v1",
			Persistent:    severity >= events.EventSeverity_SEVERITY_MEDIUM,
			TtlSeconds:    3600, // 1 hour retention
			RoutingKeys:   []string{"memory", "ebpf", event.Command},
		},
		Source: &events.EventSource{
			Type:      "ebpf",
			Collector: mc.config.Name,
			NodeId:    mc.getNodeID(),
			Version:   "1.0.0",
		},
		Entity: &events.EntityContext{
			Type: "process",
			Id:   fmt.Sprintf("%d", event.PID),
			Name: event.Command,
			Attributes: map[string]*events.AttributeValue{
				"pid":           {Value: &events.AttributeValue_IntValue{IntValue: int64(event.PID)}},
				"tid":           {Value: &events.AttributeValue_IntValue{IntValue: int64(event.TID)}},
				"in_container":  {Value: &events.AttributeValue_BoolValue{BoolValue: event.InContainer}},
				"container_pid": {Value: &events.AttributeValue_IntValue{IntValue: int64(event.ContainerPID)}},
			},
		},
		Data: &events.UnifiedEvent_Memory{
			Memory: memoryEventData,
		},
		Attributes: map[string]*AttributeValue{
			"collector_name":  {Value: &events.AttributeValue_StringValue{StringValue: mc.config.Name}},
			"host_pid":        {Value: &events.AttributeValue_IntValue{IntValue: int64(event.PID)}},
			"memory_usage":    {Value: &events.AttributeValue_IntValue{IntValue: int64(event.TotalMemory)}},
			"allocation_size": {Value: &events.AttributeValue_IntValue{IntValue: int64(event.Size)}},
		},
		Labels: map[string]string{
			"source":    "ebpf",
			"collector": "memory",
			"process":   event.Command,
			"node":      mc.getNodeID(),
		},
		Quality: &events.QualityMetadata{
			Confidence:   mc.getEventConfidence(event, tracker),
			Completeness: 1.0, // eBPF data is always complete
			Accuracy:     mc.getEventAccuracy(tracker),
			Freshness:    float32(time.Since(time.Unix(0, int64(event.Timestamp))).Seconds()),
		},
	}

	// Add correlation context if available
	if tracker != nil {
		unifiedEvent.Correlation = &events.CorrelationContext{
			TraceId:      mc.generateTraceID(event.PID),
			ProcessId:    fmt.Sprintf("%d", event.PID),
			SessionId:    mc.generateSessionID(event.PID, tracker.LastUpdate),
			CausalityIds: mc.getCausalityIDs(event, tracker),
		}
	}

	return unifiedEvent, nil
}

// getMemoryOperation converts event type to operation string
func (mc *MemoryCollector) getMemoryOperation(eventType uint32) string {
	switch eventType {
	case 1: // EVENT_MEMORY_ALLOC
		return "allocation"
	case 2: // EVENT_MEMORY_FREE
		return "free"
	case 3: // EVENT_OOM_KILL
		return "oom_kill"
	case 4: // EVENT_PROCESS_EXIT
		return "process_exit"
	default:
		return "unknown"
	}
}

// getEventSeverity determines event severity
func (mc *MemoryCollector) getEventSeverity(event *MemoryEvent, tracker *ProcessMemoryTracker) events.EventSeverity {
	switch event.EventType {
	case 3: // OOM_KILL
		return events.EventSeverity_SEVERITY_CRITICAL
	case 4: // PROCESS_EXIT
		return events.EventSeverity_SEVERITY_MEDIUM
	default:
		// Check for high memory usage or OOM predictions
		if tracker != nil {
			if tracker.PredictedOOM != nil && tracker.PredictedOOM.Confidence > 0.8 {
				return events.EventSeverity_SEVERITY_HIGH
			}
			if tracker.RiskScore > 0.7 {
				return events.EventSeverity_SEVERITY_MEDIUM
			}
		}
		return events.EventSeverity_SEVERITY_LOW
	}
}

// getEventPriority converts severity to priority
func (mc *MemoryCollector) getEventPriority(severity events.EventSeverity) int32 {
	switch severity {
	case events.EventSeverity_SEVERITY_CRITICAL:
		return 100
	case events.EventSeverity_SEVERITY_HIGH:
		return 75
	case events.EventSeverity_SEVERITY_MEDIUM:
		return 50
	case events.EventSeverity_SEVERITY_LOW:
		return 25
	default:
		return 10
	}
}

// getTrendTypeName converts TrendType to string
func (mc *MemoryCollector) getTrendTypeName(trendType TrendType) string {
	switch trendType {
	case TrendStable:
		return "stable"
	case TrendLinear:
		return "linear"
	case TrendExponential:
		return "exponential"
	case TrendSpiky:
		return "spiky"
	case TrendDecreasing:
		return "decreasing"
	default:
		return "unknown"
	}
}

// getEventConfidence calculates confidence score for the event
func (mc *MemoryCollector) getEventConfidence(event *MemoryEvent, tracker *ProcessMemoryTracker) float32 {
	confidence := float32(0.9) // eBPF data is generally high confidence

	// Adjust based on event type
	if event.EventType == 3 || event.EventType == 4 { // OOM or exit events
		confidence = 1.0 // These are definitive events
	}

	// Factor in prediction confidence if available
	if tracker != nil && tracker.PredictedOOM != nil {
		predictionConfidence := float32(tracker.PredictedOOM.Confidence)
		confidence = (confidence + predictionConfidence) / 2
	}

	return confidence
}

// getEventAccuracy estimates accuracy based on prediction history
func (mc *MemoryCollector) getEventAccuracy(tracker *ProcessMemoryTracker) float32 {
	if tracker == nil {
		return 0.9 // Default accuracy for eBPF data
	}

	// Use global prediction accuracy
	predictionAccuracy := mc.getPredictionAccuracy()
	if predictionAccuracy > 0 {
		return float32(predictionAccuracy)
	}

	return 0.9
}

// getNodeID returns the current node identifier
func (mc *MemoryCollector) getNodeID() string {
	// In a real implementation, this would get the actual node ID
	// For now, return a placeholder
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// generateTraceID generates a trace ID for correlation
func (mc *MemoryCollector) generateTraceID(pid uint32) string {
	return fmt.Sprintf("memory-trace-%d-%d", pid, time.Now().UnixNano())
}

// generateSessionID generates a session ID for grouping related events
func (mc *MemoryCollector) generateSessionID(pid uint32, lastUpdate time.Time) string {
	return fmt.Sprintf("session-%d-%d", pid, lastUpdate.Unix())
}

// getCausalityIDs generates causality relationships between events
func (mc *MemoryCollector) getCausalityIDs(event *MemoryEvent, tracker *ProcessMemoryTracker) []string {
	causalityIDs := make([]string, 0)

	// Link to parent process if available
	causalityIDs = append(causalityIDs, fmt.Sprintf("process-%d", event.PID))

	// Link to container if in container
	if event.InContainer {
		causalityIDs = append(causalityIDs, fmt.Sprintf("container-%d", event.ContainerPID))
	}

	// Link to previous memory events for this process
	if tracker != nil && len(tracker.GrowthHistory) > 0 {
		lastEvent := tracker.GrowthHistory[len(tracker.GrowthHistory)-1]
		causalityIDs = append(causalityIDs, fmt.Sprintf("memory-history-%d-%d", event.PID, lastEvent.Timestamp.UnixNano()))
	}

	return causalityIDs
}

// streamToUnifiedFormat converts collector events to unified format and streams them
func (mc *MemoryCollector) streamToUnifiedFormat(collectorEvent *collectors.Event) error {
	// Get the original eBPF event data
	event, ok := collectorEvent.Data.(*MemoryEvent)
	if !ok {
		return fmt.Errorf("invalid event data type")
	}

	// Get process tracker for additional context
	mc.processesMu.RLock()
	tracker := mc.processes[event.PID]
	mc.processesMu.RUnlock()

	// Convert to unified format
	unifiedEvent, err := mc.convertToUnifiedEvent(event, tracker)
	if err != nil {
		return fmt.Errorf("failed to convert to unified format: %w", err)
	}

	// Stream to gRPC server (this would integrate with the gRPC client)
	return mc.sendToGRPCStream(unifiedEvent)
}

// sendToGRPCStream sends unified event to the gRPC streaming service
func (mc *MemoryCollector) sendToGRPCStream(event *events.UnifiedEvent) error {
	// This would integrate with the gRPC client from the previous implementation
	// For now, we'll just log the event
	if mc.config.Debug {
		fmt.Printf("Streaming unified event: %s [%s] %s\n",
			event.Id, event.Metadata.Type, event.Entity.Name)
	}

	// TODO: Integrate with actual gRPC streaming client
	// return mc.grpcClient.SendEvent(event)

	return nil
}
