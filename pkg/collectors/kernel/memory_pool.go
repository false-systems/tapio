package kernel

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// EventPool provides an object pool for kernel events to reduce GC pressure
type EventPool struct {
	pool sync.Pool
	
	// Metrics
	allocationsTotal metric.Int64Counter
	recycledTotal    metric.Int64Counter
	poolSize         metric.Int64Gauge
	gcPressure       metric.Float64Gauge
	
	// Configuration
	maxPoolSize int64
	currentSize int64
	
	// GC monitoring
	lastGCRuns     uint32
	gcMonitorTicker *time.Ticker
	gcMonitorDone   chan struct{}
}

// NewEventPool creates a new event pool with monitoring
func NewEventPool(maxPoolSize int) *EventPool {
	meter := otel.Meter("tapio/collectors/kernel")
	
	pool := &EventPool{
		maxPoolSize: int64(maxPoolSize),
		pool: sync.Pool{
			New: func() interface{} {
				return &KernelEvent{}
			},
		},
		gcMonitorDone: make(chan struct{}),
	}
	
	// Initialize metrics
	var err error
	pool.allocationsTotal, err = meter.Int64Counter(
		"kernel_pool_allocations_total",
		metric.WithDescription("Total number of pool allocations"),
	)
	if err != nil {
		// Log error but continue
	}
	
	pool.recycledTotal, err = meter.Int64Counter(
		"kernel_pool_recycled_total", 
		metric.WithDescription("Total number of recycled objects"),
	)
	if err != nil {
		// Log error but continue
	}
	
	pool.poolSize, err = meter.Int64Gauge(
		"kernel_pool_size",
		metric.WithDescription("Current pool size"),
	)
	if err != nil {
		// Log error but continue
	}
	
	pool.gcPressure, err = meter.Float64Gauge(
		"kernel_pool_gc_pressure",
		metric.WithDescription("GC pressure indicator (GCs per second)"),
	)
	if err != nil {
		// Log error but continue
	}
	
	// Start GC monitoring
	pool.startGCMonitoring()
	
	return pool
}

// Get retrieves an event from the pool
func (p *EventPool) Get() *KernelEvent {
	event := p.pool.Get().(*KernelEvent)
	
	// Reset the event
	*event = KernelEvent{}
	
	// Update metrics
	if p.allocationsTotal != nil {
		p.allocationsTotal.Add(context.Background(), 1)
	}
	
	return event
}

// Put returns an event to the pool
func (p *EventPool) Put(event *KernelEvent) {
	if event == nil {
		return
	}
	
	// Check pool size limit
	currentSize := atomic.LoadInt64(&p.currentSize)
	if currentSize >= p.maxPoolSize {
		// Pool is full, let GC handle it
		return
	}
	
	// Clear sensitive data before pooling
	p.clearEvent(event)
	
	p.pool.Put(event)
	atomic.AddInt64(&p.currentSize, 1)
	
	// Update metrics
	if p.recycledTotal != nil {
		p.recycledTotal.Add(context.Background(), 1)
	}
	if p.poolSize != nil {
		p.poolSize.Record(context.Background(), currentSize+1)
	}
}

// clearEvent clears sensitive data from the event
func (p *EventPool) clearEvent(event *KernelEvent) {
	// Clear string fields
	event.Comm = ""
	event.PodUID = ""
	event.ContainerID = ""
	
	// Clear union data
	for i := range event.Data {
		event.Data[i] = 0
	}
	
	// Reset all numeric fields
	event.Timestamp = 0
	event.PID = 0
	event.TID = 0
	event.EventType = 0
	event.Size = 0
	event.CgroupID = 0
}

// startGCMonitoring starts monitoring GC pressure
func (p *EventPool) startGCMonitoring() {
	var lastNumGC uint32
	var lastTime = time.Now()
	
	p.gcMonitorTicker = time.NewTicker(10 * time.Second)
	
	go func() {
		defer p.gcMonitorTicker.Stop()
		
		for {
			select {
			case <-p.gcMonitorTicker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				
				now := time.Now()
				duration := now.Sub(lastTime).Seconds()
				
				gcRuns := m.NumGC - lastNumGC
				gcPressure := float64(gcRuns) / duration
				
				if p.gcPressure != nil {
					p.gcPressure.Record(context.Background(), gcPressure,
						metric.WithAttributes(attribute.String("pool", "kernel_events")))
				}
				
				lastNumGC = m.NumGC
				lastTime = now
				
				// Log high GC pressure
				if gcPressure > 2.0 { // More than 2 GCs per second
					// Consider reducing pool size or increasing maxPoolSize
				}
				
			case <-p.gcMonitorDone:
				return
			}
		}
	}()
}

// Stop stops the pool and cleanup
func (p *EventPool) Stop() {
	close(p.gcMonitorDone)
	if p.gcMonitorTicker != nil {
		p.gcMonitorTicker.Stop()
	}
}

// Stats returns pool statistics
func (p *EventPool) Stats() PoolStats {
	return PoolStats{
		CurrentSize: atomic.LoadInt64(&p.currentSize),
		MaxSize:     p.maxPoolSize,
	}
}

// PoolStats represents pool statistics
type PoolStats struct {
	CurrentSize int64 `json:"current_size"`
	MaxSize     int64 `json:"max_size"`
}

// KernelEvent represents a kernel event (simplified structure for pool)
type KernelEvent struct {
	Timestamp   uint64 `json:"timestamp"`
	PID         uint32 `json:"pid"`
	TID         uint32 `json:"tid"`
	EventType   uint32 `json:"event_type"`
	Size        uint64 `json:"size"`
	Comm        string `json:"comm"`
	CgroupID    uint64 `json:"cgroup_id"`
	PodUID      string `json:"pod_uid"`
	ContainerID string `json:"container_id"`
	Data        [64]byte `json:"data"`
}

// CircuitBreaker implements circuit breaker pattern for overload protection
type CircuitBreaker struct {
	mu            sync.RWMutex
	state         CircuitState
	failureCount  uint64
	successCount  uint64
	nextAttempt   time.Time
	
	// Configuration
	failureThreshold uint64
	resetTimeout     time.Duration
	halfOpenMaxCalls uint64
	
	// Metrics
	stateGauge     metric.Int64Gauge
	failureCounter metric.Int64Counter
	successCounter metric.Int64Counter
}

type CircuitState int

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(failureThreshold uint64, resetTimeout time.Duration) *CircuitBreaker {
	meter := otel.Meter("tapio/collectors/kernel")
	
	cb := &CircuitBreaker{
		state:            CircuitStateClosed,
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
		halfOpenMaxCalls: 5,
	}
	
	// Initialize metrics
	var err error
	cb.stateGauge, err = meter.Int64Gauge(
		"kernel_circuit_breaker_state",
		metric.WithDescription("Circuit breaker state (0=closed, 1=open, 2=half-open)"),
	)
	if err != nil {
		// Log error but continue
	}
	
	cb.failureCounter, err = meter.Int64Counter(
		"kernel_circuit_breaker_failures_total",
		metric.WithDescription("Total circuit breaker failures"),
	)
	if err != nil {
		// Log error but continue
	}
	
	cb.successCounter, err = meter.Int64Counter(
		"kernel_circuit_breaker_successes_total", 
		metric.WithDescription("Total circuit breaker successes"),
	)
	if err != nil {
		// Log error but continue
	}
	
	return cb
}

// Call executes the function if circuit breaker allows it
func (cb *CircuitBreaker) Call(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitBreakerOpen
	}
	
	err := fn()
	cb.recordResult(err)
	return err
}

// allowRequest checks if request should be allowed
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	switch cb.state {
	case CircuitStateClosed:
		return true
	case CircuitStateOpen:
		return time.Now().After(cb.nextAttempt)
	case CircuitStateHalfOpen:
		return cb.successCount < cb.halfOpenMaxCalls
	default:
		return false
	}
}

// recordResult records the result of a function call
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	if err != nil {
		cb.failureCount++
		if cb.failureCounter != nil {
			cb.failureCounter.Add(context.Background(), 1)
		}
		
		if cb.state == CircuitStateHalfOpen || cb.failureCount >= cb.failureThreshold {
			cb.setState(CircuitStateOpen)
		}
	} else {
		cb.successCount++
		if cb.successCounter != nil {
			cb.successCounter.Add(context.Background(), 1)
		}
		
		if cb.state == CircuitStateHalfOpen && cb.successCount >= cb.halfOpenMaxCalls {
			cb.setState(CircuitStateClosed)
		}
	}
}

// setState changes the circuit breaker state
func (cb *CircuitBreaker) setState(state CircuitState) {
	cb.state = state
	cb.nextAttempt = time.Now().Add(cb.resetTimeout)
	
	switch state {
	case CircuitStateClosed:
		cb.failureCount = 0
		cb.successCount = 0
	case CircuitStateOpen:
		// Reset attempt time
		cb.nextAttempt = time.Now().Add(cb.resetTimeout)
	case CircuitStateHalfOpen:
		cb.successCount = 0
	}
	
	if cb.stateGauge != nil {
		cb.stateGauge.Record(context.Background(), int64(state))
	}
}

// State returns current circuit breaker state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Custom errors
var (
	ErrCircuitBreakerOpen = fmt.Errorf("circuit breaker is open")
)

