package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ContextLookup provides K8s context by IP address.
// This is typically backed by a K8s informer cache.
type ContextLookup interface {
	GetContextByIP(ip string) (*domain.K8sContext, error)
}

// EnterpriseService enriches events with K8s context and graph entities.
// This creates TapioEvent (not ObserverEvent) for Ahti graph correlation.
type EnterpriseService struct {
	nc        *nats.Conn
	ctxLookup ContextLookup

	mu     sync.Mutex
	closed bool
}

// NewEnterpriseService creates an enterprise-tier intelligence service.
// url: NATS server URL
// ctxLookup: K8s context lookup (typically backed by informer cache)
func NewEnterpriseService(url string, ctxLookup ContextLookup) (*EnterpriseService, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}
	return &EnterpriseService{
		nc:        nc,
		ctxLookup: ctxLookup,
	}, nil
}

// ProcessEvent enriches and publishes TapioEvent to NATS.
func (s *EnterpriseService) ProcessEvent(ctx context.Context, event *domain.ObserverEvent) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if event == nil {
		return fmt.Errorf("nil event")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

	// Get source IP from event
	srcIP := extractSourceIP(event)
	if srcIP == "" {
		return fmt.Errorf("no source IP in event")
	}

	// Lookup K8s context by IP
	k8sCtx, err := s.ctxLookup.GetContextByIP(srcIP)
	if err != nil {
		return fmt.Errorf("context lookup failed: %w", err)
	}
	if k8sCtx == nil {
		return fmt.Errorf("no K8s context found for IP %s", srcIP)
	}

	// Enrich to TapioEvent with graph entities
	tapioEvent, err := domain.EnrichWithK8sContext(event, k8sCtx)
	if err != nil {
		return fmt.Errorf("enrichment failed: %w", err)
	}

	// Publish TapioEvent to NATS
	data, err := json.Marshal(tapioEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal TapioEvent: %w", err)
	}

	subject := fmt.Sprintf("tapio.events.%s", tapioEvent.Type)
	if err := s.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	return nil
}

// Shutdown closes the NATS connection.
func (s *EnterpriseService) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.nc.Close()
	s.closed = true
	return nil
}

// extractSourceIP gets the source IP from event data.
func extractSourceIP(event *domain.ObserverEvent) string {
	if event.NetworkData != nil && event.NetworkData.SrcIP != "" {
		return event.NetworkData.SrcIP
	}
	return ""
}
