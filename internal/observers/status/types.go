package status

import (
	"sync"
	"time"
)

type ErrorType uint16

const (
	ErrorNone    ErrorType = 0
	ErrorTimeout ErrorType = 1
	ErrorRefused ErrorType = 2
	ErrorReset   ErrorType = 3
	Error5XX     ErrorType = 4
	Error4XX     ErrorType = 5
	ErrorSlow    ErrorType = 6
	ErrorPartial ErrorType = 7
)

type StatusEvent struct {
	ServiceHash  uint32
	EndpointHash uint32
	StatusCode   uint16
	ErrorType    ErrorType
	Timestamp    uint64
	Latency      uint32
	PID          uint32
}

type HashDecoder struct {
	mu        sync.RWMutex
	services  map[uint32]string
	endpoints map[uint32]string
}

func NewHashDecoder() *HashDecoder {
	return &HashDecoder{
		services:  make(map[uint32]string),
		endpoints: make(map[uint32]string),
	}
}

func (d *HashDecoder) AddService(hash uint32, name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.services[hash] = name
}

func (d *HashDecoder) AddEndpoint(hash uint32, path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.endpoints[hash] = path
}

func (d *HashDecoder) GetService(hash uint32) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.services[hash]
}

func (d *HashDecoder) GetEndpoint(hash uint32) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.endpoints[hash]
}

type StatusAggregator struct {
	mu            sync.Mutex
	aggregates    map[uint32]*AggregatedStatus
	flushInterval time.Duration
}

type AggregatedStatus struct {
	ServiceHash  uint32
	ErrorCount   uint64
	TotalCount   uint64
	LatencySum   uint64
	LatencyCount uint64
	ErrorTypes   map[ErrorType]uint64
	LastSeen     time.Time
}

func NewStatusAggregator(flushInterval time.Duration) *StatusAggregator {
	return &StatusAggregator{
		aggregates:    make(map[uint32]*AggregatedStatus),
		flushInterval: flushInterval,
	}
}

func (a *StatusAggregator) Add(event *StatusEvent) {
	if event == nil {
		// Note: nil events are silently skipped to avoid performance impact
		// Consider enabling debug logging if this becomes an issue
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	agg, exists := a.aggregates[event.ServiceHash]
	if !exists {
		agg = &AggregatedStatus{
			ServiceHash: event.ServiceHash,
			ErrorTypes:  make(map[ErrorType]uint64),
		}
		a.aggregates[event.ServiceHash] = agg
	}

	agg.TotalCount++
	if event.ErrorType != ErrorNone {
		agg.ErrorCount++
		agg.ErrorTypes[event.ErrorType]++
	}

	if event.Latency > 0 {
		agg.LatencySum += uint64(event.Latency)
		agg.LatencyCount++
	}

	agg.LastSeen = time.Now()
}

func (a *StatusAggregator) Flush() map[uint32]*AggregatedStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := a.aggregates
	a.aggregates = make(map[uint32]*AggregatedStatus)

	return result
}

func (a *AggregatedStatus) AvgLatency() float64 {
	if a.LatencyCount == 0 {
		return 0
	}
	return float64(a.LatencySum) / float64(a.LatencyCount)
}

func (a *AggregatedStatus) ErrorRate() float64 {
	if a.TotalCount == 0 {
		return 0
	}
	return float64(a.ErrorCount) / float64(a.TotalCount)
}

type FailurePattern struct {
	Name        string
	Description string
	Detector    func(events []*StatusEvent) bool
	Severity    string
}

// filterValidEvents returns only non-nil events from the slice
// This helper reduces code duplication in pattern detectors
func filterValidEvents(events []*StatusEvent) []*StatusEvent {
	valid := make([]*StatusEvent, 0, len(events))
	for _, e := range events {
		if e != nil {
			valid = append(valid, e)
		}
	}
	return valid
}

// countEventsByType counts events matching a specific error type
// This helper reduces code duplication in pattern detectors
func countEventsByType(events []*StatusEvent, errorType ErrorType) int {
	count := 0
	for _, e := range filterValidEvents(events) {
		if e.ErrorType == errorType {
			count++
		}
	}
	return count
}

var KnownPatterns = []FailurePattern{
	{
		Name:        "CascadingTimeout",
		Description: "Timeouts propagating through services",
		Detector: func(events []*StatusEvent) bool {
			return countEventsByType(events, ErrorTimeout) > 5
		},
		Severity: "high",
	},
	{
		Name:        "RetryStorm",
		Description: "Excessive retries causing load amplification",
		Detector: func(events []*StatusEvent) bool {
			validEvents := filterValidEvents(events)
			if len(validEvents) < 10 {
				return false
			}

			counts := make(map[uint32]int)
			for _, e := range validEvents {
				counts[e.ServiceHash]++
			}

			for _, count := range counts {
				if count > len(validEvents)/2 {
					return true
				}
			}
			return false
		},
		Severity: "critical",
	},
	{
		Name:        "ServiceDown",
		Description: "Service consistently refusing connections",
		Detector: func(events []*StatusEvent) bool {
			return countEventsByType(events, ErrorRefused) > 10
		},
		Severity: "critical",
	},
}
