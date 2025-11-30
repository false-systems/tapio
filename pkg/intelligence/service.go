package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/domain"
)

// IntelligenceService processes ObserverEvents and routes them
// to appropriate outputs based on deployment tier.
//
// Deployment Tiers:
// - Simple: No intelligence service (observers use OTLPEmitter directly)
// - FREE: Intelligence service with NATS bridge only
// - ENTERPRISE: Intelligence service with enrichment + NATS
type IntelligenceService interface {
	// ProcessEvent processes an observer event
	ProcessEvent(ctx context.Context, event *domain.ObserverEvent) error

	// Shutdown gracefully stops the service
	Shutdown(ctx context.Context) error
}

// service implements IntelligenceService for FREE tier (NATS bridge only)
type service struct {
	conn   *nats.Conn
	mu     sync.Mutex
	closed bool
}

// NewIntelligenceService creates a new Intelligence Service (FREE tier)
// url: NATS server URL (e.g., "nats://localhost:4222")
func NewIntelligenceService(url string) (IntelligenceService, error) {
	// Connect to NATS server
	conn, err := nats.Connect(url,
		nats.MaxReconnects(-1),            // Infinite reconnects
		nats.ReconnectWait(2*time.Second), // 2 second reconnect delay
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &service{
		conn:   conn,
		closed: false,
	}, nil
}

// ProcessEvent implements IntelligenceService
func (s *service) ProcessEvent(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

	// Check context cancellation first
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Build NATS subject
	subject := buildSubject(event)

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to NATS
	if err := s.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	return nil
}

// Shutdown implements IntelligenceService
func (s *service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil // Already closed
	}

	if s.conn != nil {
		s.conn.Close()
	}

	s.closed = true
	return nil
}

// buildSubject constructs the NATS subject for an event.
// Pattern: tapio.events.{type}.{subtype}
// If subtype is empty, pattern is: tapio.events.{type}
func buildSubject(event *domain.ObserverEvent) string {
	// Sanitize type and subtype (replace invalid chars with underscore)
	eventType := sanitizeSubjectToken(event.Type)
	subtype := sanitizeSubjectToken(event.Subtype)

	if subtype == "" {
		return fmt.Sprintf("tapio.events.%s", eventType)
	}
	return fmt.Sprintf("tapio.events.%s.%s", eventType, subtype)
}

// sanitizeSubjectToken replaces invalid NATS subject characters with underscore.
// NATS subjects can only contain: A-Z, a-z, 0-9, ., -, _
func sanitizeSubjectToken(s string) string {
	if s == "" {
		return ""
	}

	// Replace spaces and special chars with underscore
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")

	return s
}
