package progress

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Event represents a progress event
type Event struct {
	Type      EventType
	Phase     string
	Total     int64
	Current   int64
	Message   string
	Error     error
	Timestamp time.Time
	Fields    map[string]interface{}
}

// EventType represents the type of progress event
type EventType int

const (
	EventStart EventType = iota
	EventUpdate
	EventComplete
	EventError
	EventLog
)

// Tracker manages progress tracking with channels
type Tracker struct {
	events     chan Event
	done       chan struct{}
	wg         sync.WaitGroup
	handlers   []Handler
	mu         sync.RWMutex
	phases     map[string]*PhaseInfo
	startTime  time.Time
	totalBytes atomic.Int64

	// Metrics
	metrics *Metrics
}

// PhaseInfo tracks information about a phase
type PhaseInfo struct {
	Name      string
	Total     int64
	Current   int64
	StartTime time.Time
	EndTime   time.Time
	Status    PhaseStatus
	Error     error
}

// PhaseStatus represents the status of a phase
type PhaseStatus int

const (
	PhaseStatusPending PhaseStatus = iota
	PhaseStatusRunning
	PhaseStatusComplete
	PhaseStatusError
)

// Handler processes progress events
type Handler interface {
	HandleEvent(event Event)
	Close() error
}

// Metrics tracks progress metrics
type Metrics struct {
	mu               sync.RWMutex
	phaseDurations   map[string]time.Duration
	bytesTransferred int64
	errors           []error
	eventsProcessed  int64
	avgEventRate     float64
}

// NewTracker creates a new progress tracker
func NewTracker(bufferSize int) *Tracker {
	return &Tracker{
		events:    make(chan Event, bufferSize),
		done:      make(chan struct{}),
		phases:    make(map[string]*PhaseInfo),
		startTime: time.Now(),
		metrics: &Metrics{
			phaseDurations: make(map[string]time.Duration),
		},
	}
}

// Start starts the progress tracker
func (t *Tracker) Start(ctx context.Context) {
	t.wg.Add(1)
	go t.run(ctx)
}

// Stop stops the progress tracker
func (t *Tracker) Stop() {
	close(t.done)
	t.wg.Wait()

	// Close all handlers
	for _, h := range t.handlers {
		h.Close()
	}
}

// AddHandler adds a progress handler
func (t *Tracker) AddHandler(handler Handler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handlers = append(t.handlers, handler)
}

// StartPhase starts a new phase
func (t *Tracker) StartPhase(phase string, total int64) {
	t.mu.Lock()
	t.phases[phase] = &PhaseInfo{
		Name:      phase,
		Total:     total,
		Current:   0,
		StartTime: time.Now(),
		Status:    PhaseStatusRunning,
	}
	t.mu.Unlock()

	t.sendEvent(Event{
		Type:      EventStart,
		Phase:     phase,
		Total:     total,
		Timestamp: time.Now(),
	})
}

// UpdatePhase updates phase progress
func (t *Tracker) UpdatePhase(phase string, current int64) {
	t.mu.Lock()
	if info, ok := t.phases[phase]; ok {
		info.Current = current
		t.totalBytes.Add(current - info.Current)
	}
	t.mu.Unlock()

	t.sendEvent(Event{
		Type:      EventUpdate,
		Phase:     phase,
		Current:   current,
		Timestamp: time.Now(),
	})
}

// CompletePhase marks a phase as complete
func (t *Tracker) CompletePhase(phase string) {
	t.mu.Lock()
	if info, ok := t.phases[phase]; ok {
		info.Status = PhaseStatusComplete
		info.EndTime = time.Now()
		duration := info.EndTime.Sub(info.StartTime)
		t.metrics.phaseDurations[phase] = duration
	}
	t.mu.Unlock()

	t.sendEvent(Event{
		Type:      EventComplete,
		Phase:     phase,
		Timestamp: time.Now(),
	})
}

// ErrorPhase marks a phase as failed
func (t *Tracker) ErrorPhase(phase string, err error) {
	t.mu.Lock()
	if info, ok := t.phases[phase]; ok {
		info.Status = PhaseStatusError
		info.Error = err
		info.EndTime = time.Now()
	}
	t.metrics.errors = append(t.metrics.errors, err)
	t.mu.Unlock()

	t.sendEvent(Event{
		Type:      EventError,
		Phase:     phase,
		Error:     err,
		Timestamp: time.Now(),
	})
}

// Log sends a log message
func (t *Tracker) Log(level, message string, fields ...interface{}) {
	fieldMap := make(map[string]interface{})
	for i := 0; i < len(fields)-1; i += 2 {
		if key, ok := fields[i].(string); ok {
			fieldMap[key] = fields[i+1]
		}
	}

	t.sendEvent(Event{
		Type:      EventLog,
		Message:   message,
		Fields:    fieldMap,
		Timestamp: time.Now(),
	})
}

// GetMetrics returns progress metrics
func (t *Tracker) GetMetrics() Metrics {
	t.metrics.mu.RLock()
	defer t.metrics.mu.RUnlock()

	return Metrics{
		phaseDurations:   t.metrics.phaseDurations,
		bytesTransferred: t.totalBytes.Load(),
		errors:           t.metrics.errors,
		eventsProcessed:  t.metrics.eventsProcessed,
		avgEventRate:     t.metrics.avgEventRate,
	}
}

// GetPhaseInfo returns information about a phase
func (t *Tracker) GetPhaseInfo(phase string) (*PhaseInfo, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	info, ok := t.phases[phase]
	return info, ok
}

// NewReader creates a progress-tracking reader
func (t *Tracker) NewReader(r io.Reader, phase string, total int64) io.Reader {
	t.StartPhase(phase, total)
	return &trackingReader{
		reader:  r,
		tracker: t,
		phase:   phase,
		total:   total,
	}
}

// NewWriter creates a progress-tracking writer
func (t *Tracker) NewWriter(w io.Writer, phase string, total int64) io.Writer {
	t.StartPhase(phase, total)
	return &trackingWriter{
		writer:  w,
		tracker: t,
		phase:   phase,
		total:   total,
	}
}

// run processes events
func (t *Tracker) run(ctx context.Context) {
	defer t.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	eventCount := int64(0)
	lastEventCount := int64(0)
	lastCheck := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			return
		case event := <-t.events:
			eventCount++
			t.metrics.eventsProcessed = eventCount

			// Dispatch to handlers
			for _, h := range t.handlers {
				h.HandleEvent(event)
			}
		case <-ticker.C:
			// Calculate event rate
			now := time.Now()
			duration := now.Sub(lastCheck).Seconds()
			if duration > 0 {
				rate := float64(eventCount-lastEventCount) / duration
				t.metrics.mu.Lock()
				t.metrics.avgEventRate = rate
				t.metrics.mu.Unlock()

				lastEventCount = eventCount
				lastCheck = now
			}
		}
	}
}

// sendEvent sends an event to the channel
func (t *Tracker) sendEvent(event Event) {
	select {
	case t.events <- event:
	case <-t.done:
	default:
		// Drop event if buffer is full
		fmt.Printf("Warning: dropping progress event (buffer full)\n")
	}
}

// trackingReader wraps a reader with progress tracking
type trackingReader struct {
	reader  io.Reader
	tracker *Tracker
	phase   string
	total   int64
	read    int64
	mu      sync.Mutex
}

func (r *trackingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.read += int64(n)
		current := r.read
		r.mu.Unlock()

		r.tracker.UpdatePhase(r.phase, current)
	}

	if err == io.EOF {
		r.tracker.CompletePhase(r.phase)
	} else if err != nil {
		r.tracker.ErrorPhase(r.phase, err)
	}

	return n, err
}

// trackingWriter wraps a writer with progress tracking
type trackingWriter struct {
	writer  io.Writer
	tracker *Tracker
	phase   string
	total   int64
	written int64
	mu      sync.Mutex
}

func (w *trackingWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	if n > 0 {
		w.mu.Lock()
		w.written += int64(n)
		current := w.written
		w.mu.Unlock()

		w.tracker.UpdatePhase(w.phase, current)
	}

	if err != nil {
		w.tracker.ErrorPhase(w.phase, err)
	} else if w.written >= w.total && w.total > 0 {
		w.tracker.CompletePhase(w.phase)
	}

	return n, err
}

// MultiReader creates a reader that tracks progress across multiple phases
func (t *Tracker) MultiReader(readers map[string]io.Reader) io.Reader {
	var trackingReaders []io.Reader
	for phase, r := range readers {
		trackingReaders = append(trackingReaders, t.NewReader(r, phase, -1))
	}
	return io.MultiReader(trackingReaders...)
}
