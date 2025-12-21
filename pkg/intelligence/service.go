package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Tier determines the output behavior of the intelligence service.
type Tier string

const (
	// TierDebug outputs to stdout only (for development/debugging).
	// No NATS connection required.
	TierDebug Tier = "debug"

	// TierFree routes events to NATS for AHTI correlation.
	// Events are published as raw ObserverEvent.
	TierFree Tier = "free"

	// TierEnterprise enriches events with K8s context before routing to NATS.
	// Events are transformed to TapioEvent with entities/relationships.
	TierEnterprise Tier = "enterprise"
)

// Config configures the intelligence service.
type Config struct {
	// Tier determines output behavior (debug, free, enterprise)
	Tier Tier

	// NATSURL for free/enterprise tiers (e.g., "nats://localhost:4222")
	// Ignored for debug tier.
	NATSURL string

	// ContextLookup for enterprise tier (K8s enrichment)
	// Ignored for debug/free tiers.
	ContextLookup ContextLookup

	// Critical determines if failures should block event processing.
	// If true, Emit() errors will cause upstream failures.
	// If false, errors are logged but event processing continues.
	Critical bool
}

// Service is the universal event gateway for TAPIO.
// All observers emit events through this single interface.
//
// The service handles:
// - Tier-based routing (debug/free/enterprise)
// - NATS publishing (for AHTI correlation)
// - K8s context enrichment (enterprise tier)
// - Error handling and logging
type Service interface {
	// Emit processes and routes an event based on tier configuration.
	// This is the single entry point for all observer events.
	Emit(ctx context.Context, event *domain.ObserverEvent) error

	// Name returns the service identifier (for logging and metrics).
	Name() string

	// IsCritical returns whether this service is critical.
	// If true, Emit failures should stop event processing.
	// If false, failures are logged but processing continues.
	IsCritical() bool

	// Close gracefully shuts down the service.
	Close() error
}

// New creates an intelligence service for the given configuration.
func New(cfg Config) (Service, error) {
	switch cfg.Tier {
	case TierDebug:
		return newDebugService(cfg)
	case TierFree:
		return newFreeService(cfg)
	case TierEnterprise:
		return newEnterpriseService(cfg)
	default:
		return nil, fmt.Errorf("unknown tier: %s", cfg.Tier)
	}
}

// debugService implements Service for debug tier (stdout only).
type debugService struct {
	critical bool
	mu       sync.Mutex
	closed   bool
}

func newDebugService(cfg Config) (*debugService, error) {
	return &debugService{
		critical: cfg.Critical,
	}, nil
}

func (s *debugService) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Pretty print to stdout for debugging
	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	log.Debug().
		Str("type", event.Type).
		Str("subtype", event.Subtype).
		Str("id", event.ID).
		RawJSON("event", data).
		Msg("debug event")

	return nil
}

func (s *debugService) Name() string {
	return "intelligence-debug"
}

func (s *debugService) IsCritical() bool {
	return s.critical
}

func (s *debugService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// freeService implements Service for free tier (NATS bridge).
type freeService struct {
	conn     *nats.Conn
	critical bool
	mu       sync.Mutex
	closed   bool
}

func newFreeService(cfg Config) (*freeService, error) {
	if cfg.NATSURL == "" {
		return nil, fmt.Errorf("NATSURL required for free tier")
	}

	conn, err := nats.Connect(cfg.NATSURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &freeService{
		conn:     conn,
		critical: cfg.Critical,
	}, nil
}

func (s *freeService) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

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

func (s *freeService) Name() string {
	return "intelligence-free"
}

func (s *freeService) IsCritical() bool {
	return s.critical
}

func (s *freeService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	if s.conn != nil {
		s.conn.Close()
	}

	s.closed = true
	return nil
}

// enterpriseService implements Service for enterprise tier (enriched NATS).
type enterpriseService struct {
	conn      *nats.Conn
	ctxLookup ContextLookup
	critical  bool
	mu        sync.Mutex
	closed    bool
}

func newEnterpriseService(cfg Config) (*enterpriseService, error) {
	if cfg.NATSURL == "" {
		return nil, fmt.Errorf("NATSURL required for enterprise tier")
	}
	if cfg.ContextLookup == nil {
		return nil, fmt.Errorf("ContextLookup required for enterprise tier")
	}

	conn, err := nats.Connect(cfg.NATSURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &enterpriseService{
		conn:      conn,
		ctxLookup: cfg.ContextLookup,
		critical:  cfg.Critical,
	}, nil
}

func (s *enterpriseService) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get source IP from event for K8s context lookup
	srcIP := extractSourceIP(event)
	if srcIP == "" {
		// No IP to enrich - publish raw event
		return s.publishRaw(event)
	}

	// Lookup K8s context by IP
	k8sCtx, err := s.ctxLookup.GetContextByIP(srcIP)
	if err != nil {
		// Context lookup failed - log and publish raw
		log.Warn().
			Err(err).
			Str("src_ip", srcIP).
			Msg("K8s context lookup failed, publishing raw event")
		return s.publishRaw(event)
	}

	if k8sCtx == nil {
		// No context found - publish raw
		return s.publishRaw(event)
	}

	// Enrich to TapioEvent with graph entities
	tapioEvent, err := domain.EnrichWithK8sContext(event, k8sCtx)
	if err != nil {
		// Enrichment failed - log and publish raw
		log.Warn().
			Err(err).
			Str("src_ip", srcIP).
			Msg("event enrichment failed, publishing raw event")
		return s.publishRaw(event)
	}

	// Publish enriched TapioEvent
	data, err := json.Marshal(tapioEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal TapioEvent: %w", err)
	}

	subject := fmt.Sprintf("tapio.events.%s", sanitizeSubjectToken(string(tapioEvent.Type)))
	if err := s.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	return nil
}

func (s *enterpriseService) publishRaw(event *domain.ObserverEvent) error {
	subject := buildSubject(event)
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	return s.conn.Publish(subject, data)
}

func (s *enterpriseService) Name() string {
	return "intelligence-enterprise"
}

func (s *enterpriseService) IsCritical() bool {
	return s.critical
}

func (s *enterpriseService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	if s.conn != nil {
		s.conn.Close()
	}

	s.closed = true
	return nil
}

// ContextLookup provides K8s context by IP address.
// This is typically backed by a K8s informer cache.
type ContextLookup interface {
	GetContextByIP(ip string) (*domain.K8sContext, error)
}

// buildSubject constructs the NATS subject for an event.
// Pattern: tapio.events.{type}.{subtype}
func buildSubject(event *domain.ObserverEvent) string {
	eventType := sanitizeSubjectToken(event.Type)
	subtype := sanitizeSubjectToken(event.Subtype)

	if subtype == "" {
		return fmt.Sprintf("tapio.events.%s", eventType)
	}
	return fmt.Sprintf("tapio.events.%s.%s", eventType, subtype)
}

// sanitizeSubjectToken replaces invalid NATS subject characters.
func sanitizeSubjectToken(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	return s
}

// extractSourceIP gets the source IP from event data.
func extractSourceIP(event *domain.ObserverEvent) string {
	if event.NetworkData != nil && event.NetworkData.SrcIP != "" {
		return event.NetworkData.SrcIP
	}
	return ""
}

// Legacy compatibility - keep IntelligenceService interface for existing code
// TODO: Remove after full migration

// IntelligenceService is the legacy interface (deprecated).
// Use Service interface instead.
type IntelligenceService interface {
	ProcessEvent(ctx context.Context, event *domain.ObserverEvent) error
	Shutdown(ctx context.Context) error
}

// NewIntelligenceService creates a legacy intelligence service (deprecated).
// Use New(Config{Tier: TierFree, NATSURL: url}) instead.
func NewIntelligenceService(url string) (IntelligenceService, error) {
	svc, err := New(Config{
		Tier:    TierFree,
		NATSURL: url,
	})
	if err != nil {
		return nil, err
	}
	return &legacyAdapter{svc: svc}, nil
}

type legacyAdapter struct {
	svc Service
}

func (a *legacyAdapter) ProcessEvent(ctx context.Context, event *domain.ObserverEvent) error {
	return a.svc.Emit(ctx, event)
}

func (a *legacyAdapter) Shutdown(ctx context.Context) error {
	return a.svc.Close()
}
