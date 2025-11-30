package intelligence

import (
	"context"

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
	// Future: NATS bridge will go here
}

// NewIntelligenceService creates a new Intelligence Service (FREE tier)
func NewIntelligenceService() IntelligenceService {
	return &service{}
}

// ProcessEvent implements IntelligenceService
func (s *service) ProcessEvent(ctx context.Context, event *domain.ObserverEvent) error {
	// Minimal implementation - just return nil
	return nil
}

// Shutdown implements IntelligenceService
func (s *service) Shutdown(ctx context.Context) error {
	// Minimal implementation - just return nil
	return nil
}
