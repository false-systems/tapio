package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// PrometheusMetricFactory implements MetricFactory using factory pattern for different client types
type PrometheusMetricFactory struct {
	// Dependencies
	logger *slog.Logger

	// State management
	mu              sync.RWMutex
	clients         map[string]registeredClient
	clientSequence  int64
	shutdownOnce    sync.Once
	shutdown        chan struct{}
	shutdownTimeout time.Duration

	// Factory configuration
	config FactoryConfig

	// Performance tracking
	stats FactoryStats
}

// registeredClient tracks registered clients internally
type registeredClient struct {
	id       string
	client   interface{} // MetricClient[T] where T varies
	config   interface{}
	created  time.Time
	clientType ClientType
	health   ClientHealth
	stats    ClientStats
}

// FactoryConfig configures the metric factory
type FactoryConfig struct {
	// Default timeouts
	DefaultTimeout         time.Duration
	DefaultShutdownTimeout time.Duration

	// Default retry settings
	DefaultRetryAttempts int
	DefaultRetryBackoff  time.Duration

	// Default rate limiting
	DefaultRateLimit     float64
	DefaultBurstSize     int

	// Health check configuration
	HealthCheckInterval  time.Duration
	HealthCheckTimeout   time.Duration

	// Memory management
	MaxClients          int
	ClientCleanupInterval time.Duration

	// Security defaults
	DefaultTLSConfig     *TLSConfig
	DefaultAuthConfig    *AuthConfig

	// Performance tuning
	EnableMetrics       bool
	EnableTracing       bool
	EnableProfiling     bool
}

// FactoryStats tracks factory performance metrics
type FactoryStats struct {
	ClientsCreated   int64
	ClientsDestroyed int64
	ErrorCount       int64
	LastActivity     time.Time
	Uptime           time.Duration
	MemoryUsage      int64
}

// NewPrometheusMetricFactory creates a new metric factory with advanced configuration
func NewPrometheusMetricFactory(config FactoryConfig, logger *slog.Logger) *PrometheusMetricFactory {
	// Set defaults
	if config.DefaultTimeout == 0 {
		config.DefaultTimeout = 30 * time.Second
	}
	if config.DefaultShutdownTimeout == 0 {
		config.DefaultShutdownTimeout = 10 * time.Second
	}
	if config.DefaultRetryAttempts == 0 {
		config.DefaultRetryAttempts = 3
	}
	if config.DefaultRetryBackoff == 0 {
		config.DefaultRetryBackoff = time.Second
	}
	if config.DefaultRateLimit == 0 {
		config.DefaultRateLimit = 100.0 // requests per second
	}
	if config.DefaultBurstSize == 0 {
		config.DefaultBurstSize = 10
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}
	if config.MaxClients == 0 {
		config.MaxClients = 100
	}
	if config.ClientCleanupInterval == 0 {
		config.ClientCleanupInterval = 5 * time.Minute
	}

	if logger == nil {
		logger = slog.Default().With("component", "metrics-factory")
	}

	factory := &PrometheusMetricFactory{
		logger:          logger,
		clients:         make(map[string]registeredClient),
		shutdown:        make(chan struct{}),
		shutdownTimeout: config.DefaultShutdownTimeout,
		config:          config,
		stats: FactoryStats{
			LastActivity: time.Now(),
		},
	}

	// Start background tasks
	go factory.runHealthChecks()
	go factory.runCleanupTasks()
	go factory.updateStats()

	return factory
}

// CreatePushClient creates a push-based client for gateways
func (f *PrometheusMetricFactory) CreatePushClient(config PushClientConfig) (MetricClient[PushMetric], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Validate configuration
	if err := f.validatePushConfig(config); err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("invalid push client config: %w", err)
	}

	// Apply factory defaults
	f.applyPushDefaults(&config)

	// Generate unique client ID
	clientID := f.generateClientID("push")

	// Create push client
	client, err := NewPrometheusPushClient(config, f.logger.With("client_id", clientID))
	if err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("failed to create push client: %w", err)
	}

	// Register client
	f.registerClient(clientID, client, config, ClientTypePush)

	f.logger.Info("Created push client",
		"client_id", clientID,
		"gateway_url", config.GatewayURL,
		"job_name", config.JobName)

	atomic.AddInt64(&f.stats.ClientsCreated, 1)
	f.stats.LastActivity = time.Now()

	return client, nil
}

// CreatePullClient creates a pull-based client for scraping
func (f *PrometheusMetricFactory) CreatePullClient(config PullClientConfig) (MetricClient[PullMetric], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Validate configuration
	if err := f.validatePullConfig(config); err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("invalid pull client config: %w", err)
	}

	// Apply factory defaults
	f.applyPullDefaults(&config)

	// Generate unique client ID
	clientID := f.generateClientID("pull")

	// Create pull client
	client, err := NewPrometheusPullClient(config, f.logger.With("client_id", clientID))
	if err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("failed to create pull client: %w", err)
	}

	// Register client
	f.registerClient(clientID, client, config, ClientTypePull)

	f.logger.Info("Created pull client",
		"client_id", clientID,
		"listen_address", config.ListenAddress,
		"listen_port", config.ListenPort)

	atomic.AddInt64(&f.stats.ClientsCreated, 1)
	f.stats.LastActivity = time.Now()

	return client, nil
}

// CreateStreamClient creates a streaming client for real-time metrics
func (f *PrometheusMetricFactory) CreateStreamClient(config StreamClientConfig) (MetricClient[StreamMetric], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Validate configuration
	if err := f.validateStreamConfig(config); err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("invalid stream client config: %w", err)
	}

	// Apply factory defaults
	f.applyStreamDefaults(&config)

	// Generate unique client ID
	clientID := f.generateClientID("stream")

	// Create stream client
	client, err := NewPrometheusStreamClient(config, f.logger.With("client_id", clientID))
	if err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("failed to create stream client: %w", err)
	}

	// Register client
	f.registerClient(clientID, client, config, ClientTypeStream)

	f.logger.Info("Created stream client",
		"client_id", clientID,
		"stream_endpoint", config.StreamEndpoint,
		"buffer_size", config.BufferSize)

	atomic.AddInt64(&f.stats.ClientsCreated, 1)
	f.stats.LastActivity = time.Now()

	return client, nil
}

// CreateCollectorClient creates a custom collector client
func (f *PrometheusMetricFactory) CreateCollectorClient(config CollectorConfig) (MetricClient[CustomMetric], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Validate configuration
	if err := f.validateCollectorConfig(config); err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("invalid collector client config: %w", err)
	}

	// Apply factory defaults
	f.applyCollectorDefaults(&config)

	// Generate unique client ID
	clientID := f.generateClientID("collector")

	// Create collector client
	client, err := NewPrometheusCollectorClient(config, f.logger.With("client_id", clientID))
	if err != nil {
		atomic.AddInt64(&f.stats.ErrorCount, 1)
		return nil, fmt.Errorf("failed to create collector client: %w", err)
	}

	// Register client
	f.registerClient(clientID, client, config, ClientTypeCollector)

	f.logger.Info("Created collector client",
		"client_id", clientID,
		"collector_name", config.CollectorName,
		"collection_interval", config.CollectionInterval)

	atomic.AddInt64(&f.stats.ClientsCreated, 1)
	f.stats.LastActivity = time.Now()

	return client, nil
}

// GetRegisteredClients returns all registered clients
func (f *PrometheusMetricFactory) GetRegisteredClients() []RegisteredClient {
	f.mu.RLock()
	defer f.mu.RUnlock()

	clients := make([]RegisteredClient, 0, len(f.clients))
	for _, client := range f.clients {
		clients = append(clients, RegisteredClient{
			ID:      client.id,
			Type:    client.clientType,
			Config:  client.config,
			Health:  client.health,
			Stats:   client.stats,
			Created: client.created,
		})
	}

	return clients
}

// Shutdown gracefully shuts down all clients
func (f *PrometheusMetricFactory) Shutdown(ctx context.Context) error {
	var shutdownErr error

	f.shutdownOnce.Do(func() {
		f.logger.Info("Starting factory shutdown")

		// Signal shutdown to background tasks
		close(f.shutdown)

		// Create timeout context for shutdown
		shutdownCtx, cancel := context.WithTimeout(ctx, f.shutdownTimeout)
		defer cancel()

		// Shutdown all clients
		shutdownErr = f.shutdownAllClients(shutdownCtx)

		f.logger.Info("Factory shutdown completed", "error", shutdownErr)
	})

	return shutdownErr
}

// GetStats returns factory statistics
func (f *PrometheusMetricFactory) GetStats() FactoryStats {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Update runtime stats
	stats := f.stats
	stats.Uptime = time.Since(f.stats.LastActivity)

	return stats
}

// Private methods

func (f *PrometheusMetricFactory) generateClientID(clientType string) string {
	sequence := atomic.AddInt64(&f.clientSequence, 1)
	return fmt.Sprintf("%s-client-%d", clientType, sequence)
}

func (f *PrometheusMetricFactory) registerClient(id string, client interface{}, config interface{}, clientType ClientType) {
	f.clients[id] = registeredClient{
		id:         id,
		client:     client,
		config:     config,
		created:    time.Now(),
		clientType: clientType,
		health: ClientHealth{
			Status:    "healthy",
			LastCheck: time.Now(),
			Version:   "1.0.0",
		},
		stats: ClientStats{
			LastRequest: time.Now(),
		},
	}
}

func (f *PrometheusMetricFactory) shutdownAllClients(ctx context.Context) error {
	f.mu.Lock()
	clients := make([]registeredClient, 0, len(f.clients))
	for _, client := range f.clients {
		clients = append(clients, client)
	}
	f.mu.Unlock()

	var errors []error

	for _, client := range clients {
		if closer, ok := client.client.(interface{ Close(context.Context) error }); ok {
			if err := closer.Close(ctx); err != nil {
				errors = append(errors, fmt.Errorf("failed to close client %s: %w", client.id, err))
			} else {
				atomic.AddInt64(&f.stats.ClientsDestroyed, 1)
			}
		}
	}

	// Clear clients map
	f.mu.Lock()
	f.clients = make(map[string]registeredClient)
	f.mu.Unlock()

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	return nil
}

func (f *PrometheusMetricFactory) runHealthChecks() {
	ticker := time.NewTicker(f.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-f.shutdown:
			return
		case <-ticker.C:
			f.performHealthChecks()
		}
	}
}

func (f *PrometheusMetricFactory) performHealthChecks() {
	f.mu.RLock()
	clients := make([]registeredClient, 0, len(f.clients))
	for _, client := range f.clients {
		clients = append(clients, client)
	}
	f.mu.RUnlock()

	for _, client := range clients {
		if healthChecker, ok := client.client.(interface{ Health() ClientHealth }); ok {
			health := healthChecker.Health()

			f.mu.Lock()
			if regClient, exists := f.clients[client.id]; exists {
				regClient.health = health
				f.clients[client.id] = regClient
			}
			f.mu.Unlock()
		}
	}
}

func (f *PrometheusMetricFactory) runCleanupTasks() {
	ticker := time.NewTicker(f.config.ClientCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-f.shutdown:
			return
		case <-ticker.C:
			f.performCleanupTasks()
		}
	}
}

func (f *PrometheusMetricFactory) performCleanupTasks() {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Remove unhealthy clients that haven't responded for too long
	now := time.Now()
	unhealthyThreshold := 5 * f.config.HealthCheckInterval

	for id, client := range f.clients {
		if client.health.Status == "unhealthy" &&
			now.Sub(client.health.LastCheck) > unhealthyThreshold {

			f.logger.Warn("Removing unhealthy client",
				"client_id", id,
				"last_check", client.health.LastCheck)

			delete(f.clients, id)
			atomic.AddInt64(&f.stats.ClientsDestroyed, 1)
		}
	}
}

func (f *PrometheusMetricFactory) updateStats() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-f.shutdown:
			return
		case <-ticker.C:
			f.mu.Lock()
			f.stats.LastActivity = time.Now()
			// Update memory usage estimate
			f.stats.MemoryUsage = int64(len(f.clients) * 1024) // Rough estimate
			f.mu.Unlock()
		}
	}
}

// Configuration validation methods

func (f *PrometheusMetricFactory) validatePushConfig(config PushClientConfig) error {
	if config.GatewayURL == "" {
		return fmt.Errorf("gateway URL is required")
	}
	if config.JobName == "" {
		return fmt.Errorf("job name is required")
	}
	if config.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}

func (f *PrometheusMetricFactory) validatePullConfig(config PullClientConfig) error {
	if config.ListenAddress == "" {
		return fmt.Errorf("listen address is required")
	}
	if config.ListenPort <= 0 || config.ListenPort > 65535 {
		return fmt.Errorf("listen port must be between 1 and 65535")
	}
	return nil
}

func (f *PrometheusMetricFactory) validateStreamConfig(config StreamClientConfig) error {
	if config.StreamEndpoint == "" {
		return fmt.Errorf("stream endpoint is required")
	}
	if config.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive")
	}
	return nil
}

func (f *PrometheusMetricFactory) validateCollectorConfig(config CollectorConfig) error {
	if config.CollectorName == "" {
		return fmt.Errorf("collector name is required")
	}
	if config.CollectionFunc == nil {
		return fmt.Errorf("collection function is required")
	}
	if config.CollectionInterval <= 0 {
		return fmt.Errorf("collection interval must be positive")
	}
	return nil
}

// Configuration default application methods

func (f *PrometheusMetricFactory) applyPushDefaults(config *PushClientConfig) {
	if config.Timeout == 0 {
		config.Timeout = f.config.DefaultTimeout
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = f.config.DefaultRetryAttempts
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = f.config.DefaultRetryBackoff
	}
	if config.Instance == "" {
		config.Instance = "default"
	}

	// Apply rate limiting defaults
	if config.RateLimiting.RequestsPerSecond == 0 {
		config.RateLimiting.RequestsPerSecond = f.config.DefaultRateLimit
	}
	if config.RateLimiting.BurstSize == 0 {
		config.RateLimiting.BurstSize = f.config.DefaultBurstSize
	}

	// Apply security defaults
	if config.TLSConfig == nil {
		config.TLSConfig = f.config.DefaultTLSConfig
	}
}

func (f *PrometheusMetricFactory) applyPullDefaults(config *PullClientConfig) {
	if config.ScrapeInterval == 0 {
		config.ScrapeInterval = 15 * time.Second
	}
	if config.ScrapeTimeout == 0 {
		config.ScrapeTimeout = 10 * time.Second
	}
	if config.MetricsPath == "" {
		config.MetricsPath = "/metrics"
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}

	// Apply security defaults
	if config.TLSConfig == nil {
		config.TLSConfig = f.config.DefaultTLSConfig
	}
}

func (f *PrometheusMetricFactory) applyStreamDefaults(config *StreamClientConfig) {
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = time.Second
	}

	// Apply compression defaults
	if config.Compression.Algorithm == "" {
		config.Compression.Algorithm = "gzip"
		config.Compression.Level = 6
	}

	// Apply batching defaults
	if config.Batching.MaxBatchSize == 0 {
		config.Batching.MaxBatchSize = 100
	}
	if config.Batching.FlushInterval == 0 {
		config.Batching.FlushInterval = time.Second
	}
}

func (f *PrometheusMetricFactory) applyCollectorDefaults(config *CollectorConfig) {
	if config.CollectionInterval == 0 {
		config.CollectionInterval = 30 * time.Second
	}
	if config.ErrorStrategy == "" {
		config.ErrorStrategy = "retry"
	}
	if config.MemoryLimit == 0 {
		config.MemoryLimit = 100 * 1024 * 1024 // 100MB
	}

	// Apply timeout defaults
	if config.TimeoutConfig.ConnectionTimeout == 0 {
		config.TimeoutConfig.ConnectionTimeout = f.config.DefaultTimeout
	}
	if config.TimeoutConfig.RequestTimeout == 0 {
		config.TimeoutConfig.RequestTimeout = f.config.DefaultTimeout
	}
	if config.TimeoutConfig.ShutdownTimeout == 0 {
		config.TimeoutConfig.ShutdownTimeout = f.config.DefaultShutdownTimeout
	}
}