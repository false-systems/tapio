package leakdetection

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// LeakDetector monitors and detects various types of resource leaks
type LeakDetector struct {
	mu      sync.RWMutex
	running bool
	config  *DetectorConfig
	stopCh  chan struct{}

	// Trackers for different resource types
	memoryTracker     *MemoryTracker
	goroutineTracker  *GoroutineTracker
	fileTracker       *FileTracker
	connectionTracker *ConnectionTracker

	// Callbacks
	onLeakDetected func(Leak)
	onLeakResolved func(Leak)

	// State
	detectedLeaks []Leak
	leakCounter   int64
}

// DetectorConfig configures the leak detector
type DetectorConfig struct {
	// Detection intervals
	ScanInterval  time.Duration
	AlertInterval time.Duration

	// Memory leak detection
	MemoryThreshold float64       // Memory growth rate threshold (MB/minute)
	MemoryWindow    time.Duration // Time window for memory growth analysis
	MemoryEnabled   bool

	// Goroutine leak detection
	GoroutineThreshold int           // Goroutine count threshold
	GoroutineWindow    time.Duration // Time window for goroutine growth analysis
	GoroutineEnabled   bool

	// File handle leak detection
	FileThreshold int // File handle count threshold
	FileEnabled   bool

	// Connection leak detection
	ConnectionThreshold int // Connection count threshold
	ConnectionEnabled   bool

	// General settings
	SampleSize       int // Number of samples to keep for analysis
	LeakHistoryLimit int // Maximum number of leaks to keep in history
}

// Leak represents a detected resource leak
type Leak struct {
	ID           string                 `json:"id"`
	Type         LeakType               `json:"type"`
	Severity     LeakSeverity           `json:"severity"`
	DetectedAt   time.Time              `json:"detected_at"`
	ResolvedAt   *time.Time             `json:"resolved_at,omitempty"`
	Resource     string                 `json:"resource"`
	Description  string                 `json:"description"`
	CurrentValue float64                `json:"current_value"`
	Threshold    float64                `json:"threshold"`
	GrowthRate   float64                `json:"growth_rate,omitempty"`
	Context      map[string]interface{} `json:"context,omitempty"`
	StackTrace   string                 `json:"stack_trace,omitempty"`
	Resolved     bool                   `json:"resolved"`
}

// LeakType defines types of resource leaks
type LeakType int

const (
	LeakTypeMemory LeakType = iota
	LeakTypeGoroutine
	LeakTypeFile
	LeakTypeConnection
	LeakTypeGeneral
)

func (lt LeakType) String() string {
	switch lt {
	case LeakTypeMemory:
		return "memory"
	case LeakTypeGoroutine:
		return "goroutine"
	case LeakTypeFile:
		return "file"
	case LeakTypeConnection:
		return "connection"
	case LeakTypeGeneral:
		return "general"
	default:
		return "unknown"
	}
}

// LeakSeverity defines leak severity levels
type LeakSeverity int

const (
	LeakSeverityLow LeakSeverity = iota
	LeakSeverityMedium
	LeakSeverityHigh
	LeakSeverityCritical
)

func (ls LeakSeverity) String() string {
	switch ls {
	case LeakSeverityLow:
		return "low"
	case LeakSeverityMedium:
		return "medium"
	case LeakSeverityHigh:
		return "high"
	case LeakSeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// MemoryTracker tracks memory usage patterns
type MemoryTracker struct {
	samples    []MemorySample
	mu         sync.RWMutex
	maxSamples int
}

// MemorySample represents a memory usage sample
type MemorySample struct {
	Timestamp   time.Time
	AllocMB     float64
	SysMB       float64
	HeapAllocMB float64
	HeapSysMB   float64
	HeapObjects uint64
	GCCycles    uint32
}

// GoroutineTracker tracks goroutine counts and patterns
type GoroutineTracker struct {
	samples    []GoroutineSample
	mu         sync.RWMutex
	maxSamples int
}

// GoroutineSample represents a goroutine count sample
type GoroutineSample struct {
	Timestamp  time.Time
	Count      int
	StackTrace string
}

// FileTracker tracks file handle usage
type FileTracker struct {
	samples    []FileSample
	mu         sync.RWMutex
	maxSamples int
}

// FileSample represents a file handle count sample
type FileSample struct {
	Timestamp time.Time
	OpenFiles int
}

// ConnectionTracker tracks network connection usage
type ConnectionTracker struct {
	samples    []ConnectionSample
	mu         sync.RWMutex
	maxSamples int
}

// ConnectionSample represents a connection count sample
type ConnectionSample struct {
	Timestamp   time.Time
	Connections int
	TCP         int
	UDP         int
}

// DefaultDetectorConfig returns sensible defaults
func DefaultDetectorConfig() *DetectorConfig {
	return &DetectorConfig{
		ScanInterval:        30 * time.Second,
		AlertInterval:       5 * time.Minute,
		MemoryThreshold:     10.0, // 10 MB/minute growth
		MemoryWindow:        5 * time.Minute,
		MemoryEnabled:       true,
		GoroutineThreshold:  1000,
		GoroutineWindow:     2 * time.Minute,
		GoroutineEnabled:    true,
		FileThreshold:       1000,
		FileEnabled:         true,
		ConnectionThreshold: 500,
		ConnectionEnabled:   true,
		SampleSize:          20,
		LeakHistoryLimit:    100,
	}
}

// NewLeakDetector creates a new leak detector
func NewLeakDetector(config *DetectorConfig) *LeakDetector {
	if config == nil {
		config = DefaultDetectorConfig()
	}

	return &LeakDetector{
		config:            config,
		stopCh:            make(chan struct{}),
		memoryTracker:     NewMemoryTracker(config.SampleSize),
		goroutineTracker:  NewGoroutineTracker(config.SampleSize),
		fileTracker:       NewFileTracker(config.SampleSize),
		connectionTracker: NewConnectionTracker(config.SampleSize),
		detectedLeaks:     make([]Leak, 0),
	}
}

// NewMemoryTracker creates a new memory tracker
func NewMemoryTracker(maxSamples int) *MemoryTracker {
	return &MemoryTracker{
		samples:    make([]MemorySample, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// NewGoroutineTracker creates a new goroutine tracker
func NewGoroutineTracker(maxSamples int) *GoroutineTracker {
	return &GoroutineTracker{
		samples:    make([]GoroutineSample, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// NewFileTracker creates a new file tracker
func NewFileTracker(maxSamples int) *FileTracker {
	return &FileTracker{
		samples:    make([]FileSample, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// NewConnectionTracker creates a new connection tracker
func NewConnectionTracker(maxSamples int) *ConnectionTracker {
	return &ConnectionTracker{
		samples:    make([]ConnectionSample, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// Start starts the leak detector
func (ld *LeakDetector) Start(ctx context.Context) error {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	if ld.running {
		return nil
	}

	ld.running = true

	// Start monitoring goroutines
	go ld.monitor(ctx)

	return nil
}

// Stop stops the leak detector
func (ld *LeakDetector) Stop() error {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	if !ld.running {
		return nil
	}

	ld.running = false
	close(ld.stopCh)
	return nil
}

// OnLeakDetected sets the leak detection callback
func (ld *LeakDetector) OnLeakDetected(callback func(Leak)) {
	ld.onLeakDetected = callback
}

// OnLeakResolved sets the leak resolution callback
func (ld *LeakDetector) OnLeakResolved(callback func(Leak)) {
	ld.onLeakResolved = callback
}

// monitor runs the main monitoring loop
func (ld *LeakDetector) monitor(ctx context.Context) {
	ticker := time.NewTicker(ld.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ld.stopCh:
			return
		case <-ticker.C:
			ld.scan()
		}
	}
}

// scan performs a full scan for resource leaks
func (ld *LeakDetector) scan() {
	if ld.config.MemoryEnabled {
		ld.scanMemory()
	}

	if ld.config.GoroutineEnabled {
		ld.scanGoroutines()
	}

	if ld.config.FileEnabled {
		ld.scanFiles()
	}

	if ld.config.ConnectionEnabled {
		ld.scanConnections()
	}

	// Clean up old resolved leaks
	ld.cleanupLeaks()
}

// scanMemory scans for memory leaks
func (ld *LeakDetector) scanMemory() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	sample := MemorySample{
		Timestamp:   time.Now(),
		AllocMB:     float64(m.Alloc) / 1024 / 1024,
		SysMB:       float64(m.Sys) / 1024 / 1024,
		HeapAllocMB: float64(m.HeapAlloc) / 1024 / 1024,
		HeapSysMB:   float64(m.HeapSys) / 1024 / 1024,
		HeapObjects: m.HeapObjects,
		GCCycles:    m.NumGC,
	}

	ld.memoryTracker.AddSample(sample)

	// Analyze for leaks
	if leak := ld.analyzeMemoryLeak(); leak != nil {
		ld.reportLeak(*leak)
	}
}

// scanGoroutines scans for goroutine leaks
func (ld *LeakDetector) scanGoroutines() {
	count := runtime.NumGoroutine()

	var stackTrace string
	if count > ld.config.GoroutineThreshold {
		// Capture stack trace for analysis
		stackTrace = string(debug.Stack())
	}

	sample := GoroutineSample{
		Timestamp:  time.Now(),
		Count:      count,
		StackTrace: stackTrace,
	}

	ld.goroutineTracker.AddSample(sample)

	// Analyze for leaks
	if leak := ld.analyzeGoroutineLeak(); leak != nil {
		ld.reportLeak(*leak)
	}
}

// scanFiles scans for file handle leaks
func (ld *LeakDetector) scanFiles() {
	// This would integrate with OS-specific file handle counting
	// For now, use a placeholder
	openFiles := ld.getOpenFileCount()

	sample := FileSample{
		Timestamp: time.Now(),
		OpenFiles: openFiles,
	}

	ld.fileTracker.AddSample(sample)

	// Analyze for leaks
	if leak := ld.analyzeFileLeak(); leak != nil {
		ld.reportLeak(*leak)
	}
}

// scanConnections scans for connection leaks
func (ld *LeakDetector) scanConnections() {
	// This would integrate with OS-specific connection counting
	// For now, use a placeholder
	connections := ld.getConnectionCount()

	sample := ConnectionSample{
		Timestamp:   time.Now(),
		Connections: connections,
		TCP:         connections / 2, // Placeholder
		UDP:         connections / 2, // Placeholder
	}

	ld.connectionTracker.AddSample(sample)

	// Analyze for leaks
	if leak := ld.analyzeConnectionLeak(); leak != nil {
		ld.reportLeak(*leak)
	}
}

// analyzeMemoryLeak analyzes memory usage patterns for leaks
func (ld *LeakDetector) analyzeMemoryLeak() *Leak {
	samples := ld.memoryTracker.GetSamples()
	if len(samples) < 3 {
		return nil // Need at least 3 samples for trend analysis
	}

	// Calculate memory growth rate
	recent := samples[len(samples)-1]
	old := samples[0]
	duration := recent.Timestamp.Sub(old.Timestamp)

	if duration < ld.config.MemoryWindow {
		return nil // Not enough time window
	}

	growthRate := (recent.AllocMB - old.AllocMB) / duration.Minutes()

	if growthRate > ld.config.MemoryThreshold {
		severity := ld.calculateMemorySeverity(growthRate, recent.AllocMB)

		return &Leak{
			ID:           ld.generateLeakID(),
			Type:         LeakTypeMemory,
			Severity:     severity,
			DetectedAt:   time.Now(),
			Resource:     "memory",
			Description:  "Memory usage growing consistently",
			CurrentValue: recent.AllocMB,
			Threshold:    ld.config.MemoryThreshold,
			GrowthRate:   growthRate,
			Context: map[string]interface{}{
				"growth_rate_mb_per_min": growthRate,
				"current_alloc_mb":       recent.AllocMB,
				"heap_objects":           recent.HeapObjects,
				"gc_cycles":              recent.GCCycles,
				"sample_count":           len(samples),
				"time_window_minutes":    duration.Minutes(),
			},
		}
	}

	return nil
}

// analyzeGoroutineLeak analyzes goroutine patterns for leaks
func (ld *LeakDetector) analyzeGoroutineLeak() *Leak {
	samples := ld.goroutineTracker.GetSamples()
	if len(samples) < 3 {
		return nil
	}

	recent := samples[len(samples)-1]
	old := samples[0]

	// Check for consistent growth
	if recent.Count > ld.config.GoroutineThreshold {
		growthRate := float64(recent.Count-old.Count) / old.Timestamp.Sub(recent.Timestamp).Minutes()
		severity := ld.calculateGoroutineSeverity(recent.Count, growthRate)

		return &Leak{
			ID:           ld.generateLeakID(),
			Type:         LeakTypeGoroutine,
			Severity:     severity,
			DetectedAt:   time.Now(),
			Resource:     "goroutines",
			Description:  "Goroutine count exceeds threshold",
			CurrentValue: float64(recent.Count),
			Threshold:    float64(ld.config.GoroutineThreshold),
			GrowthRate:   growthRate,
			Context: map[string]interface{}{
				"current_count": recent.Count,
				"threshold":     ld.config.GoroutineThreshold,
				"growth_rate":   growthRate,
				"sample_count":  len(samples),
			},
			StackTrace: recent.StackTrace,
		}
	}

	return nil
}

// analyzeFileLeak analyzes file handle patterns for leaks
func (ld *LeakDetector) analyzeFileLeak() *Leak {
	samples := ld.fileTracker.GetSamples()
	if len(samples) < 2 {
		return nil
	}

	recent := samples[len(samples)-1]

	if recent.OpenFiles > ld.config.FileThreshold {
		return &Leak{
			ID:           ld.generateLeakID(),
			Type:         LeakTypeFile,
			Severity:     ld.calculateFileSeverity(recent.OpenFiles),
			DetectedAt:   time.Now(),
			Resource:     "file_handles",
			Description:  "File handle count exceeds threshold",
			CurrentValue: float64(recent.OpenFiles),
			Threshold:    float64(ld.config.FileThreshold),
			Context: map[string]interface{}{
				"open_files": recent.OpenFiles,
				"threshold":  ld.config.FileThreshold,
			},
		}
	}

	return nil
}

// analyzeConnectionLeak analyzes connection patterns for leaks
func (ld *LeakDetector) analyzeConnectionLeak() *Leak {
	samples := ld.connectionTracker.GetSamples()
	if len(samples) < 2 {
		return nil
	}

	recent := samples[len(samples)-1]

	if recent.Connections > ld.config.ConnectionThreshold {
		return &Leak{
			ID:           ld.generateLeakID(),
			Type:         LeakTypeConnection,
			Severity:     ld.calculateConnectionSeverity(recent.Connections),
			DetectedAt:   time.Now(),
			Resource:     "network_connections",
			Description:  "Network connection count exceeds threshold",
			CurrentValue: float64(recent.Connections),
			Threshold:    float64(ld.config.ConnectionThreshold),
			Context: map[string]interface{}{
				"total_connections": recent.Connections,
				"tcp_connections":   recent.TCP,
				"udp_connections":   recent.UDP,
				"threshold":         ld.config.ConnectionThreshold,
			},
		}
	}

	return nil
}

// reportLeak reports a detected leak
func (ld *LeakDetector) reportLeak(leak Leak) {
	ld.mu.Lock()
	ld.detectedLeaks = append(ld.detectedLeaks, leak)
	atomic.AddInt64(&ld.leakCounter, 1)

	// Limit history size
	if len(ld.detectedLeaks) > ld.config.LeakHistoryLimit {
		ld.detectedLeaks = ld.detectedLeaks[len(ld.detectedLeaks)-ld.config.LeakHistoryLimit:]
	}
	ld.mu.Unlock()

	// Call callback
	if ld.onLeakDetected != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Log panic but don't crash
				}
			}()
			ld.onLeakDetected(leak)
		}()
	}
}

// GetLeaks returns all detected leaks
func (ld *LeakDetector) GetLeaks() []Leak {
	ld.mu.RLock()
	defer ld.mu.RUnlock()

	result := make([]Leak, len(ld.detectedLeaks))
	copy(result, ld.detectedLeaks)
	return result
}

// GetActiveLeaks returns only unresolved leaks
func (ld *LeakDetector) GetActiveLeaks() []Leak {
	ld.mu.RLock()
	defer ld.mu.RUnlock()

	var active []Leak
	for _, leak := range ld.detectedLeaks {
		if !leak.Resolved {
			active = append(active, leak)
		}
	}
	return active
}

// ResolveLeaks marks leaks as resolved
func (ld *LeakDetector) ResolveLeak(leakID string) bool {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	for i := range ld.detectedLeaks {
		if ld.detectedLeaks[i].ID == leakID && !ld.detectedLeaks[i].Resolved {
			now := time.Now()
			ld.detectedLeaks[i].Resolved = true
			ld.detectedLeaks[i].ResolvedAt = &now

			// Call callback
			if ld.onLeakResolved != nil {
				go func(leak Leak) {
					defer func() {
						if r := recover(); r != nil {
							// Log panic but don't crash
						}
					}()
					ld.onLeakResolved(leak)
				}(ld.detectedLeaks[i])
			}

			return true
		}
	}
	return false
}

// Helper methods for trackers
func (mt *MemoryTracker) AddSample(sample MemorySample) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.samples = append(mt.samples, sample)
	if len(mt.samples) > mt.maxSamples {
		mt.samples = mt.samples[1:]
	}
}

func (mt *MemoryTracker) GetSamples() []MemorySample {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	result := make([]MemorySample, len(mt.samples))
	copy(result, mt.samples)
	return result
}

func (gt *GoroutineTracker) AddSample(sample GoroutineSample) {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	gt.samples = append(gt.samples, sample)
	if len(gt.samples) > gt.maxSamples {
		gt.samples = gt.samples[1:]
	}
}

func (gt *GoroutineTracker) GetSamples() []GoroutineSample {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	result := make([]GoroutineSample, len(gt.samples))
	copy(result, gt.samples)
	return result
}

func (ft *FileTracker) AddSample(sample FileSample) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	ft.samples = append(ft.samples, sample)
	if len(ft.samples) > ft.maxSamples {
		ft.samples = ft.samples[1:]
	}
}

func (ft *FileTracker) GetSamples() []FileSample {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	result := make([]FileSample, len(ft.samples))
	copy(result, ft.samples)
	return result
}

func (ct *ConnectionTracker) AddSample(sample ConnectionSample) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.samples = append(ct.samples, sample)
	if len(ct.samples) > ct.maxSamples {
		ct.samples = ct.samples[1:]
	}
}

func (ct *ConnectionTracker) GetSamples() []ConnectionSample {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	result := make([]ConnectionSample, len(ct.samples))
	copy(result, ct.samples)
	return result
}

// Severity calculation methods
func (ld *LeakDetector) calculateMemorySeverity(growthRate, currentMB float64) LeakSeverity {
	if growthRate > 50 || currentMB > 500 {
		return LeakSeverityCritical
	} else if growthRate > 25 || currentMB > 250 {
		return LeakSeverityHigh
	} else if growthRate > 15 || currentMB > 100 {
		return LeakSeverityMedium
	}
	return LeakSeverityLow
}

func (ld *LeakDetector) calculateGoroutineSeverity(count int, growthRate float64) LeakSeverity {
	if count > 10000 || growthRate > 100 {
		return LeakSeverityCritical
	} else if count > 5000 || growthRate > 50 {
		return LeakSeverityHigh
	} else if count > 2000 || growthRate > 20 {
		return LeakSeverityMedium
	}
	return LeakSeverityLow
}

func (ld *LeakDetector) calculateFileSeverity(count int) LeakSeverity {
	if count > 5000 {
		return LeakSeverityCritical
	} else if count > 2000 {
		return LeakSeverityHigh
	} else if count > 1500 {
		return LeakSeverityMedium
	}
	return LeakSeverityLow
}

func (ld *LeakDetector) calculateConnectionSeverity(count int) LeakSeverity {
	if count > 2000 {
		return LeakSeverityCritical
	} else if count > 1000 {
		return LeakSeverityHigh
	} else if count > 750 {
		return LeakSeverityMedium
	}
	return LeakSeverityLow
}

// Utility methods
func (ld *LeakDetector) generateLeakID() string {
	timestamp := time.Now().Unix()
	counter := atomic.LoadInt64(&ld.leakCounter)
	return fmt.Sprintf("leak-%d-%d", timestamp, counter)
}

func (ld *LeakDetector) getOpenFileCount() int {
	// This would be OS-specific implementation
	// For now, return a placeholder
	return 10 // Placeholder
}

func (ld *LeakDetector) getConnectionCount() int {
	// This would be OS-specific implementation
	// For now, return a placeholder
	return 5 // Placeholder
}

func (ld *LeakDetector) cleanupLeaks() {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	filtered := make([]Leak, 0)

	for _, leak := range ld.detectedLeaks {
		// Keep recent leaks or unresolved leaks
		if leak.DetectedAt.After(cutoff) || !leak.Resolved {
			filtered = append(filtered, leak)
		}
	}

	ld.detectedLeaks = filtered
}

// GetStats returns leak detection statistics
func (ld *LeakDetector) GetStats() map[string]interface{} {
	ld.mu.RLock()
	defer ld.mu.RUnlock()

	activeCount := 0
	resolvedCount := 0
	for _, leak := range ld.detectedLeaks {
		if leak.Resolved {
			resolvedCount++
		} else {
			activeCount++
		}
	}

	return map[string]interface{}{
		"running":            ld.running,
		"total_leaks":        len(ld.detectedLeaks),
		"active_leaks":       activeCount,
		"resolved_leaks":     resolvedCount,
		"scan_interval":      ld.config.ScanInterval,
		"memory_enabled":     ld.config.MemoryEnabled,
		"goroutine_enabled":  ld.config.GoroutineEnabled,
		"file_enabled":       ld.config.FileEnabled,
		"connection_enabled": ld.config.ConnectionEnabled,
		"memory_samples":     len(ld.memoryTracker.GetSamples()),
		"goroutine_samples":  len(ld.goroutineTracker.GetSamples()),
	}
}
