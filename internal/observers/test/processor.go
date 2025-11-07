package test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/runtime"
	"github.com/yairfalse/tapio/pkg/domain"
)

// EventType defines a type of event the test observer can generate
type EventType struct {
	Type    string
	Subtype string
}

// Processor generates configurable mock events for testing ObserverRuntime
type Processor struct {
	eventRate  int         // Events per second
	eventTypes []EventType // Event types to generate
	rngMu      sync.Mutex  // Protects rng (thread-safe random access)
	rng        *rand.Rand
}

// Option configures the test processor
type Option func(*Processor)

// WithEventRate sets the event generation rate (events per second)
func WithEventRate(rate int) Option {
	return func(p *Processor) {
		p.eventRate = rate
	}
}

// WithEventTypes sets the types of events to generate
func WithEventTypes(types []EventType) Option {
	return func(p *Processor) {
		p.eventTypes = types
	}
}

// NewProcessor creates a new test observer processor
func NewProcessor(opts ...Option) *Processor {
	proc := &Processor{
		eventRate: 10, // Default: 10 events/sec
		eventTypes: []EventType{
			{Type: "test", Subtype: "mock_event"},
		},
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, opt := range opts {
		opt(proc)
	}

	return proc
}

// Process unmarshals and returns the event (test observer just passes through)
func (p *Processor) Process(ctx context.Context, rawEvent []byte) (*domain.ObserverEvent, error) {
	var event domain.ObserverEvent
	if err := json.Unmarshal(rawEvent, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	return &event, nil
}

// Name returns "test"
func (p *Processor) Name() string {
	return "test"
}

// Setup initializes the processor
func (p *Processor) Setup(ctx context.Context, config runtime.Config) error {
	// No setup needed for test observer
	return nil
}

// Teardown cleans up resources
func (p *Processor) Teardown(ctx context.Context) error {
	// No cleanup needed for test observer
	return nil
}

// StartGeneration generates events at the specified rate
func (p *Processor) StartGeneration(ctx context.Context, eventCh chan<- []byte) {
	ticker := time.NewTicker(time.Second / time.Duration(p.eventRate))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			event := p.generateEvent()
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			select {
			case eventCh <- data:
			case <-ctx.Done():
				return
			}
		}
	}
}

// generateEvent creates a random event from configured types
func (p *Processor) generateEvent() *domain.ObserverEvent {
	p.rngMu.Lock()
	idx := p.rng.Intn(len(p.eventTypes))
	p.rngMu.Unlock()

	eventType := p.eventTypes[idx]

	return &domain.ObserverEvent{
		Type:      eventType.Type,
		Subtype:   eventType.Subtype,
		Timestamp: time.Now(),
	}
}
