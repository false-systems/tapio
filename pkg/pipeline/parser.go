package pipeline

import (
	"fmt"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Parser converts RawEvents to ObservationEvents
type Parser interface {
	// Parse converts a RawEvent to an ObservationEvent
	Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error)

	// Source returns the source this parser handles (e.g., "kernel", "dns")
	Source() string
}

// ParserRegistry manages parsers for different event sources
type ParserRegistry struct {
	mu      sync.RWMutex
	parsers map[string]Parser
}

// NewParserRegistry creates a new parser registry
func NewParserRegistry() *ParserRegistry {
	return &ParserRegistry{
		parsers: make(map[string]Parser),
	}
}

// Register adds a parser for a specific source
func (r *ParserRegistry) Register(parser Parser) error {
	if parser == nil {
		return fmt.Errorf("parser cannot be nil")
	}

	source := parser.Source()
	if source == "" {
		return fmt.Errorf("parser source cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.parsers[source]; exists {
		return fmt.Errorf("parser already registered for source: %s", source)
	}

	r.parsers[source] = parser
	return nil
}

// Get returns a parser for the given source
func (r *ParserRegistry) Get(source string) (Parser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, exists := r.parsers[source]
	return parser, exists
}

// Parse attempts to parse a RawEvent using the appropriate parser
func (r *ParserRegistry) Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("cannot parse nil event")
	}

	if raw.Source == "" {
		return nil, fmt.Errorf("event source is empty")
	}

	parser, exists := r.Get(raw.Source)
	if !exists {
		return nil, fmt.Errorf("no parser registered for source: %s", raw.Source)
	}

	return parser.Parse(raw)
}

// List returns all registered sources
func (r *ParserRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sources := make([]string, 0, len(r.parsers))
	for source := range r.parsers {
		sources = append(sources, source)
	}
	return sources
}
