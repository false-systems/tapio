package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ServerService implements the core server service
type ServerService struct {
	// Configuration
	config *domain.Configuration

	// Managers
	connectionManager domain.ConnectionManager
	endpointManager   domain.EndpointManager
	healthChecker     domain.HealthChecker
	metricsCollector  domain.MetricsCollector
	eventPublisher    domain.EventPublisher
	configManager     domain.ConfigurationManager

	// Server state
	server     *domain.Server
	status     atomic.Value // domain.ServerStatus
	startTime  time.Time
	shutdownCh chan struct{}

	// Synchronization
	mu        sync.RWMutex
	isStarted atomic.Bool
	isStopped atomic.Bool

	// Context management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewServerService creates a new server service
func NewServerService(
	config *domain.Configuration,
	connectionManager domain.ConnectionManager,
	endpointManager domain.EndpointManager,
	healthChecker domain.HealthChecker,
	metricsCollector domain.MetricsCollector,
	eventPublisher domain.EventPublisher,
	configManager domain.ConfigurationManager,
) *ServerService {
	ctx, cancel := context.WithCancel(context.Background())

	service := &ServerService{
		config:            config,
		connectionManager: connectionManager,
		endpointManager:   endpointManager,
		healthChecker:     healthChecker,
		metricsCollector:  metricsCollector,
		eventPublisher:    eventPublisher,
		configManager:     configManager,
		shutdownCh:        make(chan struct{}),
		ctx:               ctx,
		cancel:            cancel,
	}

	// Initialize server state
	service.status.Store(domain.StatusMaintenance)
	service.initializeServer()

	return service
}

// initializeServer initializes the server state
func (s *ServerService) initializeServer() {
	s.server = &domain.Server{
		ID:        s.generateServerID(),
		Name:      s.config.Server.Name,
		Version:   s.config.Server.Version,
		Status:    domain.StatusMaintenance,
		Endpoints: make([]domain.Endpoint, 0),
		Metrics:   domain.ServerMetrics{},
		Config:    s.config.Server,
	}
}

// Start starts the server
func (s *ServerService) Start(ctx context.Context) error {
	if !s.isStarted.CompareAndSwap(false, true) {
		return domain.ErrServerAlreadyRunning()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.startTime = time.Now()
	s.server.StartTime = s.startTime
	s.status.Store(domain.StatusHealthy)
	s.server.Status = domain.StatusHealthy

	// Start background services
	s.wg.Add(3)
	go s.runMetricsCollection()
	go s.runHealthChecks()
	go s.runConnectionManagement()

	// Register endpoints
	if err := s.registerEndpoints(ctx); err != nil {
		s.status.Store(domain.StatusUnhealthy)
		s.server.Status = domain.StatusUnhealthy
		return domain.NewServerErrorWithCause(domain.ErrorCodeServerStartupFailed, "failed to register endpoints", err)
	}

	// Publish startup event
	event := &domain.Event{
		ID:        s.generateEventID(),
		Type:      domain.EventTypeStartup,
		Severity:  domain.SeverityInfo,
		Source:    "server",
		Message:   "server started successfully",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"server_id": s.server.ID,
			"version":   s.server.Version,
		},
		Context: ctx,
	}

	if err := s.eventPublisher.PublishEvent(ctx, event); err != nil {
		// Log error but don't fail startup
		fmt.Printf("failed to publish startup event: %v\n", err)
	}

	return nil
}

// Stop stops the server
func (s *ServerService) Stop(ctx context.Context) error {
	if !s.isStopped.CompareAndSwap(false, true) {
		return domain.NewServerError(domain.ErrorCodeServerNotStarted, "server is not running")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.status.Store(domain.StatusMaintenance)
	s.server.Status = domain.StatusMaintenance

	// Publish shutdown event
	event := &domain.Event{
		ID:        s.generateEventID(),
		Type:      domain.EventTypeShutdown,
		Severity:  domain.SeverityInfo,
		Source:    "server",
		Message:   "server shutdown initiated",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"server_id": s.server.ID,
			"uptime":    time.Since(s.startTime).String(),
		},
		Context: ctx,
	}

	if err := s.eventPublisher.PublishEvent(ctx, event); err != nil {
		fmt.Printf("failed to publish shutdown event: %v\n", err)
	}

	// Signal shutdown
	close(s.shutdownCh)
	s.cancel()

	// Wait for background goroutines
	s.wg.Wait()

	return nil
}

// Restart restarts the server
func (s *ServerService) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}

	// Reset state
	s.isStarted.Store(false)
	s.isStopped.Store(false)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.shutdownCh = make(chan struct{})

	return s.Start(ctx)
}

// GetHealth returns the server health status
func (s *ServerService) GetHealth(ctx context.Context) (*domain.HealthCheck, error) {
	if s.healthChecker == nil {
		return nil, domain.NewServerError(domain.ErrorCodeHealthCheckFailed, "health checker not configured")
	}

	return s.healthChecker.CheckHealth(ctx)
}

// GetStatus returns the server status
func (s *ServerService) GetStatus(ctx context.Context) (*domain.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Update current metrics
	if s.metricsCollector != nil {
		metrics, err := s.metricsCollector.CollectServerMetrics(ctx)
		if err == nil {
			s.server.Metrics = *metrics
		}
	}

	// Update endpoints
	if s.endpointManager != nil {
		endpoints, err := s.endpointManager.GetEndpoints(ctx)
		if err == nil {
			s.server.Endpoints = make([]domain.Endpoint, len(endpoints))
			for i, ep := range endpoints {
				s.server.Endpoints[i] = *ep
			}
		}
	}

	// Return a copy to avoid race conditions
	serverCopy := *s.server
	serverCopy.Status = s.status.Load().(domain.ServerStatus)

	return &serverCopy, nil
}

// GetMetrics returns server metrics
func (s *ServerService) GetMetrics(ctx context.Context) (*domain.Metrics, error) {
	if s.metricsCollector == nil {
		return nil, domain.NewServerError(domain.ErrorCodeMetricsNotAvailable, "metrics collector not configured")
	}

	return s.metricsCollector.CollectMetrics(ctx)
}

// GetConfig returns the server configuration
func (s *ServerService) GetConfig(ctx context.Context) (*domain.Configuration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	configCopy := *s.config
	return &configCopy, nil
}

// UpdateConfig updates the server configuration
func (s *ServerService) UpdateConfig(ctx context.Context, config *domain.Configuration) error {
	if config == nil {
		return domain.ErrInvalidRequest("configuration cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate configuration
	if s.configManager != nil {
		if err := s.configManager.ValidateConfiguration(ctx, config); err != nil {
			return domain.NewServerErrorWithCause(domain.ErrorCodeInvalidConfiguration, "configuration validation failed", err)
		}
	}

	// Update configuration
	s.config = config
	s.server.Config = config.Server

	// Publish configuration change event
	event := &domain.Event{
		ID:        s.generateEventID(),
		Type:      domain.EventTypeRequest,
		Severity:  domain.SeverityInfo,
		Source:    "server",
		Message:   "configuration updated",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"server_id": s.server.ID,
		},
		Context: ctx,
	}

	if err := s.eventPublisher.PublishEvent(ctx, event); err != nil {
		fmt.Printf("failed to publish configuration change event: %v\n", err)
	}

	return nil
}

// PublishEvent publishes an event
func (s *ServerService) PublishEvent(ctx context.Context, event *domain.Event) error {
	if event == nil {
		return domain.ErrInvalidRequest("event cannot be nil")
	}

	if s.eventPublisher == nil {
		return domain.NewServerError(domain.ErrorCodeServiceUnavailable, "event publisher not configured")
	}

	return s.eventPublisher.PublishEvent(ctx, event)
}

// GetConnections returns all active connections
func (s *ServerService) GetConnections(ctx context.Context) ([]*domain.Connection, error) {
	if s.connectionManager == nil {
		return nil, domain.NewServerError(domain.ErrorCodeServiceUnavailable, "connection manager not configured")
	}

	return s.connectionManager.GetConnections(ctx)
}

// CloseConnection closes a specific connection
func (s *ServerService) CloseConnection(ctx context.Context, connectionID string) error {
	if connectionID == "" {
		return domain.ErrInvalidRequest("connection ID cannot be empty")
	}

	if s.connectionManager == nil {
		return domain.NewServerError(domain.ErrorCodeServiceUnavailable, "connection manager not configured")
	}

	return s.connectionManager.CloseConnection(ctx, connectionID)
}

// Background service functions

// runMetricsCollection runs the metrics collection service
func (s *ServerService) runMetricsCollection() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			if s.metricsCollector != nil {
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				metrics, err := s.metricsCollector.CollectServerMetrics(ctx)
				cancel()

				if err == nil {
					s.mu.Lock()
					s.server.Metrics = *metrics
					s.mu.Unlock()
				}
			}
		}
	}
}

// runHealthChecks runs the health check service
func (s *ServerService) runHealthChecks() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			if s.healthChecker != nil {
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				health, err := s.healthChecker.CheckHealth(ctx)
				cancel()

				if err == nil {
					var newStatus domain.ServerStatus
					switch health.Status {
					case domain.HealthStatusPass:
						newStatus = domain.StatusHealthy
					case domain.HealthStatusWarn:
						newStatus = domain.StatusDegraded
					case domain.HealthStatusFail:
						newStatus = domain.StatusUnhealthy
					}

					oldStatus := s.status.Load().(domain.ServerStatus)
					if oldStatus != newStatus {
						s.status.Store(newStatus)
						s.mu.Lock()
						s.server.Status = newStatus
						s.mu.Unlock()

						// Publish health status change event
						event := &domain.Event{
							ID:        s.generateEventID(),
							Type:      domain.EventTypeHealth,
							Severity:  s.getSeverityFromStatus(newStatus),
							Source:    "server",
							Message:   fmt.Sprintf("health status changed from %s to %s", oldStatus, newStatus),
							Timestamp: time.Now(),
							Data: map[string]interface{}{
								"server_id":  s.server.ID,
								"old_status": oldStatus,
								"new_status": newStatus,
							},
							Context: s.ctx,
						}

						if err := s.eventPublisher.PublishEvent(s.ctx, event); err != nil {
							fmt.Printf("failed to publish health status change event: %v\n", err)
						}
					}
				}
			}
		}
	}
}

// runConnectionManagement runs the connection management service
func (s *ServerService) runConnectionManagement() {
	defer s.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			if s.connectionManager != nil {
				ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
				err := s.connectionManager.CleanupIdleConnections(ctx, 5*time.Minute)
				cancel()

				if err != nil {
					fmt.Printf("failed to cleanup idle connections: %v\n", err)
				}
			}
		}
	}
}

// registerEndpoints registers all configured endpoints
func (s *ServerService) registerEndpoints(ctx context.Context) error {
	if s.endpointManager == nil {
		return nil // No endpoint manager configured
	}

	for _, endpointConfig := range s.config.Endpoints {
		endpoint := &domain.Endpoint{
			Name:     endpointConfig.Name,
			Protocol: endpointConfig.Protocol,
			Address:  endpointConfig.Address,
			Port:     endpointConfig.Port,
			Path:     endpointConfig.Path,
			Status:   domain.EndpointActive,
			Metrics:  domain.EndpointMetrics{},
		}

		if err := s.endpointManager.RegisterEndpoint(ctx, endpoint); err != nil {
			return fmt.Errorf("failed to register endpoint %s: %w", endpointConfig.Name, err)
		}
	}

	return nil
}

// Helper functions

// generateServerID generates a unique server ID
func (s *ServerService) generateServerID() string {
	return fmt.Sprintf("server-%d", time.Now().UnixNano())
}

// generateEventID generates a unique event ID
func (s *ServerService) generateEventID() string {
	return fmt.Sprintf("event-%d", time.Now().UnixNano())
}

// getSeverityFromStatus converts a server status to event severity
func (s *ServerService) getSeverityFromStatus(status domain.ServerStatus) domain.EventSeverity {
	switch status {
	case domain.StatusHealthy:
		return domain.SeverityInfo
	case domain.StatusDegraded:
		return domain.SeverityWarning
	case domain.StatusUnhealthy:
		return domain.SeverityError
	case domain.StatusMaintenance:
		return domain.SeverityInfo
	default:
		return domain.SeverityInfo
	}
}
