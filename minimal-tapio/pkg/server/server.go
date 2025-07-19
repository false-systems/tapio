package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/server/api"
)

// Server represents the main Tapio server
type Server struct {
	config     *domain.Config
	restServer *api.RESTServer
	httpServer *http.Server
	mu         sync.RWMutex

	// In-memory storage for demo
	events   []domain.Event
	findings []domain.Finding
}

// NewServer creates a new server instance
func NewServer(config *domain.Config) *Server {
	return &Server{
		config:   config,
		events:   make([]domain.Event, 0),
		findings: make([]domain.Finding, 0),
	}
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	// Create REST server
	s.restServer = api.NewRESTServer(s)

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.ServerPort),
		Handler:      s.restServer.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting server on port %d", s.config.ServerPort)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(shutdownCtx)
}

// Stop stops the server
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// GetHealth returns server health status
func (s *Server) GetHealth() domain.HealthStatus {
	return domain.HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"version": "1.0.0",
			"uptime":  time.Now().Unix(),
		},
	}
}

// ProcessEvent processes an event (minimal implementation)
func (s *Server) ProcessEvent(ctx context.Context, event domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add timestamp if missing
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Store event
	s.events = append(s.events, event)

	// Simple correlation: if we see multiple errors, create a finding
	errorCount := 0
	for _, e := range s.events {
		if e.Severity == domain.SeverityError || e.Severity == domain.SeverityCritical {
			errorCount++
		}
	}

	if errorCount >= 3 {
		finding := domain.Finding{
			ID:          domain.FindingID(fmt.Sprintf("finding-%d", time.Now().UnixNano())),
			Type:        domain.FindingType("error_spike"),
			Severity:    domain.SeverityWarning,
			Title:       "Multiple Errors Detected",
			Description: fmt.Sprintf("Detected %d errors in recent events", errorCount),
			Timestamp:   time.Now(),
			Insights:    []string{"Consider investigating the root cause of these errors"},
		}
		s.findings = append(s.findings, finding)
	}

	return nil
}

// GetEvents returns stored events
func (s *Server) GetEvents(ctx context.Context, limit int) []domain.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.events) {
		limit = len(s.events)
	}

	// Return last N events
	if limit < len(s.events) {
		return s.events[len(s.events)-limit:]
	}

	return s.events
}

// GetFindings returns findings
func (s *Server) GetFindings(ctx context.Context) []domain.Finding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.findings
}

// CorrelateEvents performs simple correlation
func (s *Server) CorrelateEvents(ctx context.Context, events []domain.Event) (*domain.Correlation, error) {
	// Simple correlation: group by source
	sourceGroups := make(map[domain.SourceType][]string)

	for _, event := range events {
		sourceGroups[event.Source] = append(sourceGroups[event.Source], string(event.ID))
	}

	// If events are from same source, they might be related
	for source, ids := range sourceGroups {
		if len(ids) >= 2 {
			return &domain.Correlation{
				ID:          fmt.Sprintf("corr-%d", time.Now().UnixNano()),
				Type:        "same_source",
				EventIDs:    ids,
				Confidence:  0.7,
				Description: fmt.Sprintf("Events from same source: %s", source),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	return &domain.Correlation{
		ID:          fmt.Sprintf("corr-%d", time.Now().UnixNano()),
		Type:        "no_correlation",
		EventIDs:    []string{},
		Confidence:  0.0,
		Description: "No correlation found",
		Timestamp:   time.Now(),
	}, nil
}
