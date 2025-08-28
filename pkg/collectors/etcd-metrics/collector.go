package etcdmetrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config for etcd health monitoring
type Config struct {
	Name           string
	BufferSize     int
	Endpoints      []string // etcd endpoints ["localhost:2379"]
	DialTimeout    time.Duration
	RequestTimeout time.Duration
	Username       string // Optional auth
	Password       string

	// Check intervals
	HealthCheckInterval time.Duration

	// Thresholds for alerting
	ResponseTimeThreshold time.Duration // Alert if etcd response > X
	DbSizeThreshold       int64         // Alert if DB size > X bytes
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Name:                  "etcd-metrics",
		BufferSize:            1000,
		Endpoints:             []string{}, // Must be configured
		DialTimeout:           5 * time.Second,
		RequestTimeout:        2 * time.Second,
		HealthCheckInterval:   30 * time.Second,
		ResponseTimeThreshold: 500 * time.Millisecond,
		DbSizeThreshold:       8 * 1024 * 1024 * 1024, // 8GB
	}
}

// Collector monitors etcd cluster health
type Collector struct {
	name   string
	logger *zap.Logger
	config Config

	client  *clientv3.Client
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	healthy bool
	mu      sync.RWMutex

	events chan *domain.CollectorEvent

	// State tracking
	lastLeaderID     uint64
	lastLeaderChange time.Time

	// OpenTelemetry
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	responseTime    metric.Float64Histogram
}

// NewCollector creates a new etcd health collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("no etcd endpoints configured")
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Create etcd client
	clientConfig := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	}

	if cfg.Username != "" && cfg.Password != "" {
		clientConfig.Username = cfg.Username
		clientConfig.Password = cfg.Password
	}

	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// Initialize OTEL
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	eventsProcessed, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total etcd anomaly events detected"),
	)

	errorsTotal, _ := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in etcd monitoring"),
	)

	responseTime, _ := meter.Float64Histogram(
		fmt.Sprintf("%s_response_time_ms", name),
		metric.WithDescription("etcd response time in milliseconds"),
	)

	return &Collector{
		name:            name,
		logger:          logger,
		config:          cfg,
		client:          client,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		healthy:         true,
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		responseTime:    responseTime,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start monitoring loop
	c.wg.Add(1)
	go c.monitorLoop()

	c.logger.Info("etcd health collector started",
		zap.Strings("endpoints", c.config.Endpoints))

	return nil
}

// Stop shuts down the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.wg.Wait()
		if c.client != nil {
			c.client.Close()
		}
		close(c.events)
		c.logger.Info("etcd health collector stopped")
	}

	return nil
}

// Events returns event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// monitorLoop performs periodic health checks
func (c *Collector) monitorLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheckInterval)
	defer ticker.Stop()

	// Initial check
	c.performHealthCheck()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.performHealthCheck()
		}
	}
}

// performHealthCheck checks etcd cluster health
func (c *Collector) performHealthCheck() {
	// Check if we have required components
	if c.client == nil {
		c.handleError(fmt.Errorf("etcd client not initialized"), "client not available")
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.config.RequestTimeout)
	defer cancel()

	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, "etcd.health.check")
		defer span.End()
	}

	// 1. Test basic connectivity with a simple operation
	start := time.Now()
	_, err := c.client.Get(ctx, "health-check", clientv3.WithLimit(1))
	responseTime := time.Since(start)

	if c.responseTime != nil {
		c.responseTime.Record(c.ctx, float64(responseTime.Milliseconds()))
	}

	if err != nil {
		c.handleError(err, "connectivity check failed")
		return
	}

	// Mark as healthy if we got here
	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()

	// 2. Check response time
	if responseTime > c.config.ResponseTimeThreshold {
		c.sendEvent(&domain.CollectorEvent{
			EventID:   c.generateEventID(),
			Timestamp: time.Now(),
			Type:      domain.EventTypeETCD,
			Source:    c.name,
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				ETCD: &domain.ETCDData{
					Operation: "slow_response",
					Key:       "performance",
					Value:     fmt.Sprintf("Response time: %v", responseTime),
					Duration:  responseTime,
				},
				Custom: map[string]string{
					"response_time_ms": fmt.Sprintf("%.2f", responseTime.Seconds()*1000),
					"threshold_ms":     fmt.Sprintf("%.2f", c.config.ResponseTimeThreshold.Seconds()*1000),
				},
			},
			Metadata: domain.EventMetadata{
				Priority: domain.PriorityHigh,
				Tags:     []string{"slow-etcd", "performance"},
			},
		})
	}

	// 3. Check cluster status
	c.checkClusterStatus(ctx)
}

// checkClusterStatus checks for leader changes and member health
func (c *Collector) checkClusterStatus(ctx context.Context) {
	// Check if client is available
	if c.client == nil {
		return
	}

	// Try each endpoint to get status
	var lastErr error
	statusFound := false

	for _, endpoint := range c.config.Endpoints {
		status, err := c.client.Status(ctx, endpoint)
		if err != nil {
			lastErr = err
			c.logger.Debug("Failed to get status from endpoint",
				zap.String("endpoint", endpoint),
				zap.Error(err))
			continue
		}

		statusFound = true

		// Check for leader change
		if c.lastLeaderID != 0 && c.lastLeaderID != status.Leader {
			timeSinceLastChange := time.Since(c.lastLeaderChange)

			c.sendEvent(&domain.CollectorEvent{
				EventID:   c.generateEventID(),
				Timestamp: time.Now(),
				Type:      domain.EventTypeETCD,
				Source:    c.name,
				Severity:  domain.EventSeverityCritical,
				EventData: domain.EventDataContainer{
					ETCD: &domain.ETCDData{
						Operation: "leader_change",
						Key:       "cluster_stability",
						Value:     fmt.Sprintf("Leader changed after %v", timeSinceLastChange),
						Revision:  status.Header.Revision,
					},
					Custom: map[string]string{
						"old_leader":     fmt.Sprintf("%x", c.lastLeaderID),
						"new_leader":     fmt.Sprintf("%x", status.Leader),
						"time_as_leader": timeSinceLastChange.String(),
					},
				},
				Metadata: domain.EventMetadata{
					Priority: domain.PriorityCritical,
					Tags:     []string{"leader-election", "cluster-instability"},
				},
				CorrelationHints: &domain.CorrelationHints{
					CorrelationTags: map[string]string{
						"event_type": "etcd_leader_change",
						"impact":     "potential_api_disruption",
					},
				},
			})

			c.lastLeaderChange = time.Now()
		}

		c.lastLeaderID = status.Leader

		// Check if no leader
		if status.Leader == 0 {
			c.sendEvent(&domain.CollectorEvent{
				EventID:   c.generateEventID(),
				Timestamp: time.Now(),
				Type:      domain.EventTypeETCD,
				Source:    c.name,
				Severity:  domain.EventSeverityCritical, // Most severe level available
				EventData: domain.EventDataContainer{
					ETCD: &domain.ETCDData{
						Operation: "no_leader",
						Key:       "cluster_health",
						Value:     "Cluster has no leader!",
					},
				},
				Metadata: domain.EventMetadata{
					Priority: domain.PriorityCritical,
					Tags:     []string{"no-leader", "cluster-down", "emergency"},
				},
				CorrelationHints: &domain.CorrelationHints{
					CorrelationTags: map[string]string{
						"failure_type": "etcd_no_leader",
						"impact":       "cluster_unavailable",
					},
				},
			})
		}

		// Check database size
		if status.DbSize > c.config.DbSizeThreshold {
			c.sendEvent(&domain.CollectorEvent{
				EventID:   c.generateEventID(),
				Timestamp: time.Now(),
				Type:      domain.EventTypeETCD,
				Source:    c.name,
				Severity:  domain.EventSeverityWarning,
				EventData: domain.EventDataContainer{
					ETCD: &domain.ETCDData{
						Operation: "large_database",
						Key:       "storage",
						Value:     fmt.Sprintf("Database size: %.2f GB", float64(status.DbSize)/1024/1024/1024),
					},
					Custom: map[string]string{
						"db_size_bytes": fmt.Sprintf("%d", status.DbSize),
						"threshold":     fmt.Sprintf("%d", c.config.DbSizeThreshold),
						"action":        "consider_defragmentation",
					},
				},
				Metadata: domain.EventMetadata{
					Priority: domain.PriorityNormal,
					Tags:     []string{"storage-pressure", "maintenance-needed"},
				},
			})
		}

		// If we got status from one endpoint, that's enough
		break
	}

	if !statusFound && lastErr != nil {
		c.handleError(lastErr, "failed to get cluster status from any endpoint")
	}
}

// handleError handles errors and sends appropriate events
func (c *Collector) handleError(err error, context string) {
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()

	if c.errorsTotal != nil {
		c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
			attribute.String("context", context),
		))
	}

	c.logger.Error("etcd health check failed",
		zap.String("context", context),
		zap.Error(err))

	c.sendEvent(&domain.CollectorEvent{
		EventID:   c.generateEventID(),
		Timestamp: time.Now(),
		Type:      domain.EventTypeETCD,
		Source:    c.name,
		Severity:  domain.EventSeverityError,
		EventData: domain.EventDataContainer{
			ETCD: &domain.ETCDData{
				Operation: "health_check_failed",
				Key:       "availability",
				Value:     fmt.Sprintf("%s: %v", context, err),
			},
			Custom: map[string]string{
				"error":   err.Error(),
				"context": context,
			},
		},
		Metadata: domain.EventMetadata{
			Priority: domain.PriorityHigh,
			Tags:     []string{"etcd-error", "health-check-failed"},
		},
	})
}

// sendEvent sends event to channel
func (c *Collector) sendEvent(event *domain.CollectorEvent) {
	select {
	case c.events <- event:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
				attribute.String("severity", string(event.Severity)),
			))
		}
	case <-c.ctx.Done():
		return
	default:
		c.logger.Warn("Event buffer full, dropping event",
			zap.String("event_type", string(event.Type)))
	}
}

// generateEventID generates a unique event ID
func (c *Collector) generateEventID() string {
	return fmt.Sprintf("%s-%d", c.name, time.Now().UnixNano())
}
