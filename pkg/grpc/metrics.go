package grpc

import (
	"sync"
	"sync/atomic"
	"time"
)

// ServerMetrics tracks server performance and health metrics
type ServerMetrics struct {
	// Request metrics
	requestsReceived   uint64
	responsesSent      uint64
	responseSendFailed uint64

	// Event processing metrics
	eventsProcessed  uint64
	eventsFailed     uint64
	eventsThrottled  uint64
	batchesProcessed uint64

	// Timing metrics
	totalProcessingTime int64 // nanoseconds
	minProcessingTime   int64 // nanoseconds
	maxProcessingTime   int64 // nanoseconds

	// Connection metrics
	connectionsEstablished uint64
	connectionsClosed      uint64
	connectionErrors       uint64
	activeConnections      int64

	// Collector metrics
	collectorsRegistered map[string]uint64
	collectorsMu         sync.RWMutex

	// Memory metrics
	memoryUsage    uint64
	maxMemoryUsage uint64

	// Server lifecycle
	serverStarted bool
	serverStopped bool
	startTime     time.Time

	// Rate metrics (calculated)
	lastMetricsUpdate time.Time
	eventsPerSecond   float64
	requestsPerSecond float64

	// Load metrics
	load              float64
	availableCapacity uint32

	// Error tracking
	errorCounts map[string]uint64
	errorMu     sync.RWMutex

	// Performance tracking
	latencyHistogram  *LatencyHistogram
	throughputHistory []ThroughputSample
	throughputMu      sync.RWMutex
}

// MetricsStats provides a snapshot of server metrics
type MetricsStats struct {
	// Request metrics
	RequestsReceived   uint64
	ResponsesSent      uint64
	ResponseSendFailed uint64

	// Event processing metrics
	EventsProcessed  uint64
	EventsFailed     uint64
	EventsThrottled  uint64
	BatchesProcessed uint64

	// Rate metrics
	EventsPerSecond   float64
	RequestsPerSecond float64

	// Timing metrics
	AvgProcessingTime time.Duration
	MinProcessingTime time.Duration
	MaxProcessingTime time.Duration

	// Connection metrics
	ConnectionsEstablished uint64
	ConnectionsClosed      uint64
	ConnectionErrors       uint64
	ActiveConnections      int64

	// Collector metrics
	CollectorsByType map[string]uint64

	// Memory metrics
	MemoryUsage    uint64
	MaxMemoryUsage uint64

	// Server metrics
	Uptime            time.Duration
	Load              float64
	AvailableCapacity uint32

	// Error metrics
	ErrorCounts map[string]uint64

	// Quality metrics
	SuccessRate  float64
	ThrottleRate float64
}

// LatencyHistogram tracks latency distribution
type LatencyHistogram struct {
	buckets []LatencyBucket
	mu      sync.RWMutex
}

// LatencyBucket represents a latency bucket
type LatencyBucket struct {
	UpperBound time.Duration
	Count      uint64
}

// ThroughputSample represents a throughput measurement
type ThroughputSample struct {
	Timestamp    time.Time
	EventsPerSec float64
	Load         float64
}

// NewServerMetrics creates a new server metrics instance
func NewServerMetrics() *ServerMetrics {
	return &ServerMetrics{
		collectorsRegistered: make(map[string]uint64),
		errorCounts:          make(map[string]uint64),
		latencyHistogram:     NewLatencyHistogram(),
		throughputHistory:    make([]ThroughputSample, 0, 300), // 5 minutes at 1-second intervals
		lastMetricsUpdate:    time.Now(),
		minProcessingTime:    int64(time.Hour), // Initialize to high value
	}
}

// NewLatencyHistogram creates a new latency histogram
func NewLatencyHistogram() *LatencyHistogram {
	buckets := []LatencyBucket{
		{UpperBound: 1 * time.Microsecond},
		{UpperBound: 10 * time.Microsecond},
		{UpperBound: 100 * time.Microsecond},
		{UpperBound: 1 * time.Millisecond},
		{UpperBound: 10 * time.Millisecond},
		{UpperBound: 100 * time.Millisecond},
		{UpperBound: 1 * time.Second},
		{UpperBound: 10 * time.Second},
	}

	return &LatencyHistogram{
		buckets: buckets,
	}
}

// Server lifecycle events

// ServerStarted records server start
func (sm *ServerMetrics) ServerStarted() {
	sm.serverStarted = true
	sm.startTime = time.Now()
}

// ServerStopped records server stop
func (sm *ServerMetrics) ServerStopped() {
	sm.serverStopped = true
}

// Request/Response metrics

// RequestReceived increments the requests received counter
func (sm *ServerMetrics) RequestReceived() {
	atomic.AddUint64(&sm.requestsReceived, 1)
}

// ResponseSent increments the responses sent counter
func (sm *ServerMetrics) ResponseSent() {
	atomic.AddUint64(&sm.responsesSent, 1)
}

// ResponseSendFailed increments the response send failures counter
func (sm *ServerMetrics) ResponseSendFailed() {
	atomic.AddUint64(&sm.responseSendFailed, 1)
	sm.recordError("response_send_failed")
}

// Event processing metrics

// EventsProcessed records successful event processing
func (sm *ServerMetrics) EventsProcessed(count uint64, processingTime time.Duration) {
	atomic.AddUint64(&sm.eventsProcessed, count)
	atomic.AddUint64(&sm.batchesProcessed, 1)

	// Update processing time metrics
	nanos := processingTime.Nanoseconds()
	atomic.AddInt64(&sm.totalProcessingTime, nanos)

	// Update min processing time
	for {
		current := atomic.LoadInt64(&sm.minProcessingTime)
		if nanos >= current {
			break
		}
		if atomic.CompareAndSwapInt64(&sm.minProcessingTime, current, nanos) {
			break
		}
	}

	// Update max processing time
	for {
		current := atomic.LoadInt64(&sm.maxProcessingTime)
		if nanos <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&sm.maxProcessingTime, current, nanos) {
			break
		}
	}

	// Record in latency histogram
	sm.latencyHistogram.Record(processingTime)
}

// EventProcessingFailed records failed event processing
func (sm *ServerMetrics) EventProcessingFailed(count uint64) {
	atomic.AddUint64(&sm.eventsFailed, count)
	sm.recordError("event_processing_failed")
}

// EventsThrottled records throttled events
func (sm *ServerMetrics) EventsThrottled(count uint64) {
	atomic.AddUint64(&sm.eventsThrottled, count)
}

// Connection metrics

// ConnectionEstablished records a new connection
func (sm *ServerMetrics) ConnectionEstablished() {
	atomic.AddUint64(&sm.connectionsEstablished, 1)
	atomic.AddInt64(&sm.activeConnections, 1)
}

// ConnectionClosed records a closed connection
func (sm *ServerMetrics) ConnectionClosed() {
	atomic.AddUint64(&sm.connectionsClosed, 1)
	atomic.AddInt64(&sm.activeConnections, -1)
}

// ConnectionError records a connection error
func (sm *ServerMetrics) ConnectionError() {
	atomic.AddUint64(&sm.connectionErrors, 1)
	sm.recordError("connection_error")
}

// UpdateConnectionStats updates connection statistics
func (sm *ServerMetrics) UpdateConnectionStats(stats ConnectionStats) {
	atomic.StoreInt64(&sm.activeConnections, int64(stats.ActiveConnections))
}

// Collector metrics

// CollectorRegistered records a collector registration
func (sm *ServerMetrics) CollectorRegistered(collectorType string) {
	sm.collectorsMu.Lock()
	sm.collectorsRegistered[collectorType]++
	sm.collectorsMu.Unlock()
}

// Memory metrics

// UpdateMemoryUsage updates memory usage
func (sm *ServerMetrics) UpdateMemoryUsage(usage uint64) {
	atomic.StoreUint64(&sm.memoryUsage, usage)

	// Update max memory usage
	for {
		current := atomic.LoadUint64(&sm.maxMemoryUsage)
		if usage <= current {
			break
		}
		if atomic.CompareAndSwapUint64(&sm.maxMemoryUsage, current, usage) {
			break
		}
	}
}

// Rate calculation and load metrics

// UpdateRates calculates and updates rate metrics
func (sm *ServerMetrics) UpdateRates() {
	now := time.Now()
	elapsed := now.Sub(sm.lastMetricsUpdate).Seconds()

	if elapsed < 1.0 {
		return // Update at most once per second
	}

	// Calculate events per second
	currentEvents := atomic.LoadUint64(&sm.eventsProcessed)
	eventsInPeriod := float64(currentEvents) / elapsed
	sm.eventsPerSecond = eventsInPeriod

	// Calculate requests per second
	currentRequests := atomic.LoadUint64(&sm.requestsReceived)
	requestsInPeriod := float64(currentRequests) / elapsed
	sm.requestsPerSecond = requestsInPeriod

	// Calculate load (0.0 to 1.0)
	maxEventsPerSec := 165000.0 // Target capacity
	sm.load = eventsInPeriod / maxEventsPerSec
	if sm.load > 1.0 {
		sm.load = 1.0
	}

	// Calculate available capacity
	availableEvents := maxEventsPerSec - eventsInPeriod
	if availableEvents < 0 {
		availableEvents = 0
	}
	sm.availableCapacity = uint32(availableEvents)

	// Add throughput sample
	sample := ThroughputSample{
		Timestamp:    now,
		EventsPerSec: eventsInPeriod,
		Load:         sm.load,
	}

	sm.throughputMu.Lock()
	sm.throughputHistory = append(sm.throughputHistory, sample)
	// Keep only last 300 samples (5 minutes)
	if len(sm.throughputHistory) > 300 {
		sm.throughputHistory = sm.throughputHistory[1:]
	}
	sm.throughputMu.Unlock()

	sm.lastMetricsUpdate = now
}

// Error tracking

// recordError records an error occurrence
func (sm *ServerMetrics) recordError(errorType string) {
	sm.errorMu.Lock()
	sm.errorCounts[errorType]++
	sm.errorMu.Unlock()
}

// GetStats returns a snapshot of all metrics
func (sm *ServerMetrics) GetStats() MetricsStats {
	sm.UpdateRates()

	// Get atomic values
	eventsProcessed := atomic.LoadUint64(&sm.eventsProcessed)
	eventsFailed := atomic.LoadUint64(&sm.eventsFailed)
	eventsThrottled := atomic.LoadUint64(&sm.eventsThrottled)
	batchesProcessed := atomic.LoadUint64(&sm.batchesProcessed)
	totalProcessingTime := atomic.LoadInt64(&sm.totalProcessingTime)
	minProcessingTime := atomic.LoadInt64(&sm.minProcessingTime)
	maxProcessingTime := atomic.LoadInt64(&sm.maxProcessingTime)

	// Calculate average processing time
	var avgProcessingTime time.Duration
	if batchesProcessed > 0 {
		avgProcessingTime = time.Duration(totalProcessingTime / int64(batchesProcessed))
	}

	// Calculate success rate
	totalEvents := eventsProcessed + eventsFailed
	var successRate float64
	if totalEvents > 0 {
		successRate = float64(eventsProcessed) / float64(totalEvents)
	}

	// Calculate throttle rate
	totalProcessingAttempts := eventsProcessed + eventsFailed + eventsThrottled
	var throttleRate float64
	if totalProcessingAttempts > 0 {
		throttleRate = float64(eventsThrottled) / float64(totalProcessingAttempts)
	}

	// Get uptime
	var uptime time.Duration
	if sm.serverStarted && !sm.startTime.IsZero() {
		uptime = time.Since(sm.startTime)
	}

	// Copy collector stats
	sm.collectorsMu.RLock()
	collectorsByType := make(map[string]uint64)
	for k, v := range sm.collectorsRegistered {
		collectorsByType[k] = v
	}
	sm.collectorsMu.RUnlock()

	// Copy error counts
	sm.errorMu.RLock()
	errorCounts := make(map[string]uint64)
	for k, v := range sm.errorCounts {
		errorCounts[k] = v
	}
	sm.errorMu.RUnlock()

	return MetricsStats{
		RequestsReceived:       atomic.LoadUint64(&sm.requestsReceived),
		ResponsesSent:          atomic.LoadUint64(&sm.responsesSent),
		ResponseSendFailed:     atomic.LoadUint64(&sm.responseSendFailed),
		EventsProcessed:        eventsProcessed,
		EventsFailed:           eventsFailed,
		EventsThrottled:        eventsThrottled,
		BatchesProcessed:       batchesProcessed,
		EventsPerSecond:        sm.eventsPerSecond,
		RequestsPerSecond:      sm.requestsPerSecond,
		AvgProcessingTime:      avgProcessingTime,
		MinProcessingTime:      time.Duration(minProcessingTime),
		MaxProcessingTime:      time.Duration(maxProcessingTime),
		ConnectionsEstablished: atomic.LoadUint64(&sm.connectionsEstablished),
		ConnectionsClosed:      atomic.LoadUint64(&sm.connectionsClosed),
		ConnectionErrors:       atomic.LoadUint64(&sm.connectionErrors),
		ActiveConnections:      atomic.LoadInt64(&sm.activeConnections),
		CollectorsByType:       collectorsByType,
		MemoryUsage:            atomic.LoadUint64(&sm.memoryUsage),
		MaxMemoryUsage:         atomic.LoadUint64(&sm.maxMemoryUsage),
		Uptime:                 uptime,
		Load:                   sm.load,
		AvailableCapacity:      sm.availableCapacity,
		ErrorCounts:            errorCounts,
		SuccessRate:            successRate,
		ThrottleRate:           throttleRate,
	}
}

// GetThroughputHistory returns recent throughput history
func (sm *ServerMetrics) GetThroughputHistory() []ThroughputSample {
	sm.throughputMu.RLock()
	defer sm.throughputMu.RUnlock()

	// Return a copy
	history := make([]ThroughputSample, len(sm.throughputHistory))
	copy(history, sm.throughputHistory)
	return history
}

// Latency histogram methods

// Record adds a latency measurement to the histogram
func (lh *LatencyHistogram) Record(latency time.Duration) {
	lh.mu.Lock()
	defer lh.mu.Unlock()

	for i := range lh.buckets {
		if latency <= lh.buckets[i].UpperBound {
			lh.buckets[i].Count++
			return
		}
	}

	// If latency exceeds all buckets, add to the last bucket
	if len(lh.buckets) > 0 {
		lh.buckets[len(lh.buckets)-1].Count++
	}
}

// GetPercentile calculates the specified percentile from the histogram
func (lh *LatencyHistogram) GetPercentile(percentile float64) time.Duration {
	lh.mu.RLock()
	defer lh.mu.RUnlock()

	// Calculate total count
	var totalCount uint64
	for _, bucket := range lh.buckets {
		totalCount += bucket.Count
	}

	if totalCount == 0 {
		return 0
	}

	// Find the bucket containing the percentile
	targetCount := uint64(float64(totalCount) * percentile / 100.0)
	var cumulativeCount uint64

	for _, bucket := range lh.buckets {
		cumulativeCount += bucket.Count
		if cumulativeCount >= targetCount {
			return bucket.UpperBound
		}
	}

	// Return the last bucket's upper bound if not found
	if len(lh.buckets) > 0 {
		return lh.buckets[len(lh.buckets)-1].UpperBound
	}

	return 0
}

// Reset clears all histogram data
func (lh *LatencyHistogram) Reset() {
	lh.mu.Lock()
	defer lh.mu.Unlock()

	for i := range lh.buckets {
		lh.buckets[i].Count = 0
	}
}
