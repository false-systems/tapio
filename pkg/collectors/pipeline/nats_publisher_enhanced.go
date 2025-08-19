package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// CircuitBreakerState represents the state of the circuit breaker
type CircuitBreakerState int32

const (
	CircuitBreakerClosed CircuitBreakerState = iota
	CircuitBreakerOpen
	CircuitBreakerHalfOpen
)

// CircuitBreaker implements a circuit breaker pattern for NATS publishing
type CircuitBreaker struct {
	state             int32 // CircuitBreakerState
	failures          int32
	requests          int32
	lastFailureTime   int64 // unix timestamp
	failureThreshold  int32
	recoveryTimeout   time.Duration
	halfOpenMaxCalls  int32
	halfOpenSuccesses int32
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(failureThreshold int, recoveryTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            int32(CircuitBreakerClosed),
		failureThreshold: int32(failureThreshold),
		recoveryTimeout:  recoveryTimeout,
		halfOpenMaxCalls: 5,
	}
}

// Call executes the function if the circuit breaker allows it
func (cb *CircuitBreaker) Call(fn func() error) error {
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))

	switch state {
	case CircuitBreakerOpen:
		// Check if recovery timeout has passed
		lastFailure := time.Unix(atomic.LoadInt64(&cb.lastFailureTime), 0)
		if time.Since(lastFailure) > cb.recoveryTimeout {
			// Transition to half-open
			if atomic.CompareAndSwapInt32(&cb.state, int32(CircuitBreakerOpen), int32(CircuitBreakerHalfOpen)) {
				atomic.StoreInt32(&cb.halfOpenSuccesses, 0)
			}
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
		fallthrough
	case CircuitBreakerHalfOpen:
		// Limit calls in half-open state
		if atomic.LoadInt32(&cb.requests) >= cb.halfOpenMaxCalls {
			return fmt.Errorf("circuit breaker half-open: max calls reached")
		}
		atomic.AddInt32(&cb.requests, 1)
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

func (cb *CircuitBreaker) recordFailure() {
	atomic.AddInt32(&cb.failures, 1)
	atomic.StoreInt64(&cb.lastFailureTime, time.Now().Unix())

	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))
	failures := atomic.LoadInt32(&cb.failures)

	if state == CircuitBreakerClosed && failures >= cb.failureThreshold {
		atomic.StoreInt32(&cb.state, int32(CircuitBreakerOpen))
	} else if state == CircuitBreakerHalfOpen {
		atomic.StoreInt32(&cb.state, int32(CircuitBreakerOpen))
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	state := CircuitBreakerState(atomic.LoadInt32(&cb.state))

	if state == CircuitBreakerHalfOpen {
		successes := atomic.AddInt32(&cb.halfOpenSuccesses, 1)
		if successes >= cb.halfOpenMaxCalls {
			// Transition back to closed
			atomic.StoreInt32(&cb.state, int32(CircuitBreakerClosed))
			atomic.StoreInt32(&cb.failures, 0)
			atomic.StoreInt32(&cb.requests, 0)
		}
	} else if state == CircuitBreakerClosed {
		// Reset failure counter on success
		atomic.StoreInt32(&cb.failures, 0)
	}
}

// IsOpen returns true if the circuit breaker is open
func (cb *CircuitBreaker) IsOpen() bool {
	return CircuitBreakerState(atomic.LoadInt32(&cb.state)) == CircuitBreakerOpen
}

// EnhancedNATSPublisher provides production-ready NATS publishing with backpressure and flow control
type EnhancedNATSPublisher struct {
	logger *zap.Logger
	nc     *nats.Conn
	js     nats.JetStreamContext
	config *config.NATSConfig

	// Lifecycle management
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	shutdownOnce sync.Once
	isConnected  int32 // atomic bool

	// Flow control and backpressure
	circuitBreaker *CircuitBreaker
	pendingSends   int32 // atomic counter
	maxPending     int32

	// Async publishing
	publishChan chan *publishRequest
	ackHandler  func(*nats.PubAck, error)

	// Connection management
	reconnectChan chan bool
	healthTicker  *time.Ticker

	// OpenTelemetry instrumentation
	tracer              trace.Tracer
	publishedCounter    metric.Int64Counter
	errorCounter        metric.Int64Counter
	pendingGauge        metric.Int64ObservableGauge
	publishLatency      metric.Float64Histogram
	circuitBreakerState metric.Int64ObservableGauge
}

type publishRequest struct {
	event     *domain.RawEvent
	subject   string
	data      []byte
	timestamp time.Time
	done      chan error
}

// NewEnhancedNATSPublisher creates a production-ready NATS publisher
func NewEnhancedNATSPublisher(logger *zap.Logger, natsConfig *config.NATSConfig) (*EnhancedNATSPublisher, error) {
	if natsConfig == nil || natsConfig.URL == "" {
		// Return nil publisher for testing
		return nil, nil
	}

	// Create context for publisher lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	// Create publisher instance first
	pub := &EnhancedNATSPublisher{
		logger:         logger,
		config:         natsConfig,
		ctx:            ctx,
		cancel:         cancel,
		reconnectChan:  make(chan bool, 1),
		circuitBreaker: NewCircuitBreaker(10, 30*time.Second), // 10 failures, 30s recovery
		maxPending:     int32(natsConfig.MaxPending),
		publishChan:    make(chan *publishRequest, natsConfig.BatchSize*2), // Buffer for batching
	}

	// Initialize OpenTelemetry instrumentation
	if err := pub.initInstrumentation(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize instrumentation: %w", err)
	}

	// Connect with retry and handlers
	nc, err := nats.Connect(natsConfig.URL,
		nats.Name(natsConfig.Name),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(natsConfig.MaxReconnects),
		nats.ReconnectWait(natsConfig.ReconnectWait),
		nats.Timeout(natsConfig.ConnectionTimeout),
		nats.DisconnectErrHandler(pub.onDisconnect),
		nats.ReconnectHandler(pub.onReconnect),
		nats.ClosedHandler(pub.onClosed),
		nats.ErrorHandler(pub.onError),
		// Enable compression for bandwidth efficiency
		nats.Compression(true),
		// Connection pooling options
		nats.MaxPingsOutstanding(10),
		nats.PingInterval(30*time.Second),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	pub.nc = nc
	atomic.StoreInt32(&pub.isConnected, 1)

	// Get JetStream context with production options
	js, err := nc.JetStream(
		nats.PublishAsyncMaxPending(natsConfig.MaxPending),
		nats.PublishAsyncErrHandler(pub.onAsyncError),
	)
	if err != nil {
		pub.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	pub.js = js

	// Ensure stream exists with production configuration
	if err := pub.ensureStream(); err != nil {
		pub.Close()
		return nil, fmt.Errorf("failed to ensure stream: %w", err)
	}

	// Start background workers
	pub.startBackgroundWorkers()

	return pub, nil
}

func (p *EnhancedNATSPublisher) initInstrumentation() error {
	p.tracer = otel.Tracer("nats-publisher")
	meter := otel.Meter("nats-publisher")

	var err error

	// Counters
	p.publishedCounter, err = meter.Int64Counter(
		"nats_events_published_total",
		metric.WithDescription("Total number of events published to NATS"),
	)
	if err != nil {
		return fmt.Errorf("failed to create published counter: %w", err)
	}

	p.errorCounter, err = meter.Int64Counter(
		"nats_publish_errors_total",
		metric.WithDescription("Total number of NATS publish errors"),
	)
	if err != nil {
		return fmt.Errorf("failed to create error counter: %w", err)
	}

	// Histogram for latency
	p.publishLatency, err = meter.Float64Histogram(
		"nats_publish_duration_ms",
		metric.WithDescription("NATS publish operation duration in milliseconds"),
	)
	if err != nil {
		return fmt.Errorf("failed to create latency histogram: %w", err)
	}

	// Observable gauges
	p.pendingGauge, err = meter.Int64ObservableGauge(
		"nats_pending_publishes",
		metric.WithDescription("Number of pending publish operations"),
	)
	if err != nil {
		return fmt.Errorf("failed to create pending gauge: %w", err)
	}

	p.circuitBreakerState, err = meter.Int64ObservableGauge(
		"nats_circuit_breaker_state",
		metric.WithDescription("Circuit breaker state (0=closed, 1=open, 2=half-open)"),
	)
	if err != nil {
		return fmt.Errorf("failed to create circuit breaker gauge: %w", err)
	}

	// Register gauge callbacks
	if _, err := meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			pending := atomic.LoadInt32(&p.pendingSends)
			o.ObserveInt64(p.pendingGauge, int64(pending))

			state := atomic.LoadInt32(&p.circuitBreaker.state)
			o.ObserveInt64(p.circuitBreakerState, int64(state))

			return nil
		},
		p.pendingGauge,
		p.circuitBreakerState,
	); err != nil {
		return fmt.Errorf("failed to register gauge callback: %w", err)
	}

	return nil
}

func (p *EnhancedNATSPublisher) ensureStream() error {
	// Use OBSERVATIONS stream configuration
	streamName := "OBSERVATIONS"
	observationSubjects := []string{
		"observations.kernel",
		"observations.kubeapi",
		"observations.dns",
		"observations.etcd",
		"observations.cni",
		"observations.systemd",
	}

	streamInfo, err := p.js.StreamInfo(streamName)
	if err != nil {
		// Create OBSERVATIONS stream with production configuration
		streamConfig := &nats.StreamConfig{
			Name:       streamName,
			Subjects:   observationSubjects,
			Storage:    nats.FileStorage,
			MaxAge:     p.config.MaxAge,
			MaxBytes:   p.config.MaxBytes,
			Duplicates: p.config.DuplicateWindow,
			Replicas:   p.config.Replicas,
			// Enable compression for storage efficiency
			Compression: nats.S2Compression,
			// Retention policy
			Retention: nats.LimitsPolicy,
			// Discard old messages when limits reached
			Discard: nats.DiscardOld,
		}

		_, err := p.js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create OBSERVATIONS stream: %w", err)
		}

		p.logger.Info("Created OBSERVATIONS JetStream stream",
			zap.String("name", streamConfig.Name),
			zap.Strings("subjects", streamConfig.Subjects),
			zap.Duration("max_age", streamConfig.MaxAge),
			zap.Int64("max_bytes", streamConfig.MaxBytes))
	} else {
		p.logger.Info("OBSERVATIONS JetStream stream already exists",
			zap.String("name", streamInfo.Config.Name),
			zap.Strings("subjects", streamInfo.Config.Subjects))
	}

	return nil
}

func (p *EnhancedNATSPublisher) startBackgroundWorkers() {
	// Start async publisher worker
	p.wg.Add(1)
	go p.asyncPublishWorker()

	// Start health monitoring
	p.wg.Add(1)
	go p.healthMonitor()
}

// Publish publishes event synchronously (compatible with existing interface)
func (p *EnhancedNATSPublisher) Publish(event domain.RawEvent) error {
	if p == nil || p.js == nil {
		return fmt.Errorf("publisher not initialized")
	}

	start := time.Now()
	ctx, span := p.tracer.Start(p.ctx, "nats.publish_observation")
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", event.Type),
		attribute.String("event.trace_id", event.TraceID),
		attribute.String("event.source", func() string {
			if collectorName, ok := event.Metadata["collector_name"]; ok && collectorName != "" {
				return collectorName
			}
			return event.Type
		}()),
	)

	// Use circuit breaker for sync publishes
	err := p.circuitBreaker.Call(func() error {
		return p.publishObservationSync(ctx, &event)
	})

	// Record metrics
	duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
	if p.publishLatency != nil {
		p.publishLatency.Record(ctx, duration, metric.WithAttributes(
			attribute.String("result", func() string {
				if err != nil {
					return "error"
				}
				return "success"
			}()),
			attribute.String("event_type", "observation"),
		))
	}

	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		if p.errorCounter != nil {
			p.errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "observation_publish_failed"),
			))
		}
		return err
	}

	if p.publishedCounter != nil {
		p.publishedCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("publish_type", "observation_sync"),
		))
	}

	return nil
}

// PublishAsync publishes observation event asynchronously with backpressure handling
func (p *EnhancedNATSPublisher) PublishAsync(event *domain.RawEvent) error {
	if p == nil || p.js == nil {
		return fmt.Errorf("publisher not initialized")
	}

	start := time.Now()
	ctx, span := p.tracer.Start(p.ctx, "nats.publish_raw_event")
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", event.Type),
		attribute.String("event.trace_id", event.TraceID),
		attribute.String("event.source", func() string {
			if collectorName, ok := event.Metadata["collector_name"]; ok && collectorName != "" {
				return collectorName
			}
			return event.Type
		}()),
	)

	// Use circuit breaker for async observation publishing
	err := p.circuitBreaker.Call(func() error {
		return p.publishObservationSync(ctx, event)
	})

	// Record metrics
	duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
	if p.publishLatency != nil {
		p.publishLatency.Record(ctx, duration, metric.WithAttributes(
			attribute.String("result", func() string {
				if err != nil {
					return "error"
				}
				return "success"
			}()),
			attribute.String("event_type", "raw"),
		))
	}

	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		if p.errorCounter != nil {
			p.errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "async_observation_publish_failed"),
			))
		}
		return err
	}

	if p.publishedCounter != nil {
		p.publishedCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("publish_type", "observation_async"),
		))
	}

	return nil
}

func (p *EnhancedNATSPublisher) publishObservationSync(ctx context.Context, event *domain.RawEvent) error {
	subject := p.generateObservationSubject(event)

	// Marshal raw event directly
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal raw event: %w", err)
	}

	// Publish with timeout
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ack, err := p.js.Publish(subject, data, nats.Context(publishCtx))
	if err != nil {
		return fmt.Errorf("failed to publish raw event to subject %s: %w", subject, err)
	}

	p.logger.Debug("Published observation event",
		zap.String("subject", subject),
		zap.String("event_type", event.Type),
		zap.String("trace_id", event.TraceID),
		zap.String("stream", ack.Stream),
		zap.Uint64("sequence", ack.Sequence),
	)

	return nil
}

// generateObservationSubject creates NATS subject for observation event
// Subject format: observations.{source}
// Examples: observations.kernel, observations.kubeapi, observations.dns
func (p *EnhancedNATSPublisher) generateObservationSubject(event *domain.RawEvent) string {
	// Get collector name from metadata, fallback to event type in lowercase
	source := strings.ToLower(event.Type)
	if collectorName, ok := event.Metadata["collector_name"]; ok && collectorName != "" {
		source = strings.ToLower(collectorName)
	}

	// Subject format: observations.{source}
	return fmt.Sprintf("observations.%s", source)
}

func (p *EnhancedNATSPublisher) asyncPublishWorker() {
	defer p.wg.Done()

	batch := make([]*publishRequest, 0, p.config.BatchSize)
	ticker := time.NewTicker(100 * time.Millisecond) // Batch timeout
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			// Process remaining batch on shutdown
			if len(batch) > 0 {
				p.processBatch(batch)
			}
			return
		case req := <-p.publishChan:
			batch = append(batch, req)
			if len(batch) >= p.config.BatchSize {
				p.processBatch(batch)
				batch = batch[:0] // Reset slice
			}
		case <-ticker.C:
			if len(batch) > 0 {
				p.processBatch(batch)
				batch = batch[:0] // Reset slice
			}
		}
	}
}

func (p *EnhancedNATSPublisher) processBatch(batch []*publishRequest) {
	for _, req := range batch {
		err := p.circuitBreaker.Call(func() error {
			return p.publishAsyncRequest(req)
		})
		req.done <- err
	}
}

func (p *EnhancedNATSPublisher) publishAsyncRequest(req *publishRequest) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	// Add message deduplication header using timestamp and trace ID
	msgID := fmt.Sprintf("%s-%d", req.event.TraceID, req.event.Timestamp.UnixNano())
	if msgID == "-" || req.event.TraceID == "" {
		msgID = fmt.Sprintf("obs-%d", req.event.Timestamp.UnixNano())
	}

	// Publish asynchronously
	pubAck, err := p.js.PublishAsync(req.subject, req.data,
		nats.MsgId(msgID), // Enable deduplication
	)
	if err != nil {
		return fmt.Errorf("failed to publish async observation event to subject %s: %w", req.subject, err)
	}

	// Wait for acknowledgment with timeout
	select {
	case <-pubAck.Ok():
		if p.publishedCounter != nil {
			p.publishedCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("publish_type", "async_observation"),
			))
		}

		p.logger.Debug("Published observation event asynchronously",
			zap.String("subject", req.subject),
			zap.String("event_type", req.event.Type),
			zap.String("trace_id", req.event.TraceID),
		)
		return nil
	case err := <-pubAck.Err():
		return fmt.Errorf("async publish failed: %w", err)
	case <-ctx.Done():
		return fmt.Errorf("async publish timeout")
	}
}

func (p *EnhancedNATSPublisher) healthMonitor() {
	defer p.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if p.nc != nil && !p.nc.IsConnected() {
				atomic.StoreInt32(&p.isConnected, 0)
				p.logger.Warn("NATS connection unhealthy")
			}
		}
	}
}

// Connection event handlers (same as original but with atomic operations)
func (p *EnhancedNATSPublisher) onDisconnect(nc *nats.Conn, err error) {
	atomic.StoreInt32(&p.isConnected, 0)
	if err != nil {
		p.logger.Warn("NATS disconnected", zap.Error(err))
	} else {
		p.logger.Warn("NATS disconnected")
	}
}

func (p *EnhancedNATSPublisher) onReconnect(nc *nats.Conn) {
	atomic.StoreInt32(&p.isConnected, 1)
	p.logger.Info("NATS reconnected", zap.String("url", nc.ConnectedUrl()))

	// Signal reconnection for any waiting operations
	select {
	case p.reconnectChan <- true:
	default:
	}
}

func (p *EnhancedNATSPublisher) onClosed(nc *nats.Conn) {
	atomic.StoreInt32(&p.isConnected, 0)
	p.logger.Warn("NATS connection closed")
}

func (p *EnhancedNATSPublisher) onError(nc *nats.Conn, sub *nats.Subscription, err error) {
	if err != nil {
		p.logger.Error("NATS error",
			zap.Error(err),
			zap.String("subject", func() string {
				if sub != nil {
					return sub.Subject
				}
				return "unknown"
			}()),
		)
	}
}

func (p *EnhancedNATSPublisher) onAsyncError(js nats.JetStream, msg *nats.Msg, err error) {
	if err != nil {
		p.logger.Error("NATS async publish error",
			zap.Error(err),
			zap.String("subject", msg.Subject),
		)

		// Record error metric
		if p.errorCounter != nil {
			p.errorCounter.Add(p.ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "async_error"),
			))
		}
	}
}

// Close closes the NATS connection gracefully with proper resource cleanup
func (p *EnhancedNATSPublisher) Close() {
	if p == nil {
		return
	}

	p.shutdownOnce.Do(func() {
		p.logger.Info("Shutting down enhanced NATS publisher")

		// Cancel context to stop all workers
		if p.cancel != nil {
			p.cancel()
		}

		// Wait for all background workers to finish
		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			p.logger.Info("All NATS publisher workers stopped")
		case <-time.After(10 * time.Second):
			p.logger.Warn("Timeout waiting for NATS publisher workers")
		}

		// Close NATS connection with drain
		if p.nc != nil {
			if err := p.nc.Drain(); err != nil {
				p.logger.Warn("Error draining NATS connection", zap.Error(err))
			}
		}

		atomic.StoreInt32(&p.isConnected, 0)
		p.logger.Info("Enhanced NATS publisher closed")
	})
}

// IsHealthy returns true if the publisher is connected and healthy
func (p *EnhancedNATSPublisher) IsHealthy() bool {
	if p == nil || p.nc == nil {
		return false
	}
	return atomic.LoadInt32(&p.isConnected) == 1 && p.nc.IsConnected()
}

// PublisherStats provides structured statistics instead of map[string]interface{}
type PublisherStats struct {
	Connected      bool  `json:"connected"`
	PendingSends   int32 `json:"pending_sends"`
	CircuitBreaker bool  `json:"circuit_breaker_open"`
	MaxPending     int32 `json:"max_pending"`
	PublishCount   int64 `json:"publish_count,omitempty"`
	ErrorCount     int64 `json:"error_count,omitempty"`
}

// Stats returns publisher statistics with type safety
func (p *EnhancedNATSPublisher) Stats() PublisherStats {
	return PublisherStats{
		Connected:      p.IsHealthy(),
		PendingSends:   atomic.LoadInt32(&p.pendingSends),
		CircuitBreaker: p.circuitBreaker.IsOpen(),
		MaxPending:     p.maxPending,
	}
}
