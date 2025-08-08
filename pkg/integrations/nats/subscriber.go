package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

// Subscriber handles NATS integration for the correlation engine
type Subscriber struct {
	logger *zap.Logger

	// NATS connection
	nc           *nats.Conn
	js           nats.JetStreamContext
	subscription *nats.Subscription

	// Correlation engine
	engine *correlation.Engine

	// Configuration
	config *config.NATSConfig

	// State and lifecycle management
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	shutdownCtx context.Context
	shutdown    context.CancelFunc

	// Metrics
	mu               sync.RWMutex
	messagesReceived int64
	messagesAcked    int64
	messagesNacked   int64
	processingErrors int64

	// Resource cleanup tracking
	resourcesMu sync.Mutex
	resources   []func() error // cleanup functions
}

// Deprecated: Use config.NATSConfig instead
// DefaultConfig returns the new centralized config
func DefaultConfig() *config.NATSConfig {
	return config.DefaultNATSConfig()
}

// NewSubscriber creates a new NATS subscriber with proper resource management
func NewSubscriber(logger *zap.Logger, config *config.NATSConfig, engine *correlation.Engine) (*Subscriber, error) {
	ctx, cancel := context.WithCancel(context.Background())
	shutdownCtx, shutdown := context.WithCancel(context.Background())

	sub := &Subscriber{
		logger:      logger,
		engine:      engine,
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
		shutdownCtx: shutdownCtx,
		shutdown:    shutdown,
		resources:   make([]func() error, 0),
	}

	// Connect to NATS
	if err := sub.connect(); err != nil {
		cancel()
		shutdown()
		return nil, err
	}

	// Setup JetStream
	if err := sub.setupJetStream(); err != nil {
		sub.cleanupResources()
		cancel()
		shutdown()
		return nil, err
	}

	return sub, nil
}

// Start begins processing messages from NATS
func (s *Subscriber) Start(ctx context.Context) error {
	s.logger.Info("Starting NATS subscriber",
		zap.String("stream", s.config.TracesStreamName),
		zap.String("subject", s.config.GetTracesSubject()),
		zap.String("consumer", s.config.ConsumerName),
	)

	// Use PullSubscribe since we created a pull consumer
	sub, err := s.js.PullSubscribe(
		s.config.GetTracesSubject(),
		s.config.ConsumerName,
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	s.subscription = sub

	// Start metrics reporter
	s.wg.Add(1)
	go s.metricsReporter()

	// Start message fetching loop
	s.wg.Add(1)
	go s.fetchMessages(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	return s.Stop()
}

// Stop gracefully shuts down the subscriber with proper resource cleanup
func (s *Subscriber) Stop() error {
	s.logger.Info("Stopping NATS subscriber")

	// Signal shutdown
	s.shutdown()

	// Cancel internal context
	s.cancel()

	// Create timeout context for cleanup operations
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Unsubscribe with timeout
	if s.subscription != nil {
		done := make(chan error, 1)
		go func() {
			done <- s.subscription.Unsubscribe()
		}()

		select {
		case err := <-done:
			if err != nil {
				s.logger.Error("Failed to unsubscribe", zap.Error(err))
			}
		case <-cleanupCtx.Done():
			s.logger.Warn("Timeout during unsubscribe operation")
		}
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Debug("All goroutines stopped gracefully")
	case <-cleanupCtx.Done():
		s.logger.Warn("Timeout waiting for goroutines to stop")
	}

	// Clean up all resources
	s.cleanupResources()

	s.logger.Info("NATS subscriber stopped",
		zap.Int64("messages_received", s.messagesReceived),
		zap.Int64("messages_acked", s.messagesAcked),
		zap.Int64("messages_nacked", s.messagesNacked),
		zap.Int64("processing_errors", s.processingErrors),
	)

	return nil
}

// connect establishes NATS connection
func (s *Subscriber) connect() error {
	opts := []nats.Option{
		nats.Name(s.config.Name),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(s.config.MaxReconnects),
		nats.ReconnectWait(s.config.ReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			s.logger.Error("NATS disconnected", zap.Error(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			s.logger.Info("NATS reconnected", zap.String("url", nc.ConnectedUrl()))
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			s.logger.Error("NATS error", zap.Error(err))
		}),
	}

	nc, err := nats.Connect(s.config.URL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	s.nc = nc

	// Add connection cleanup to resource list
	s.addResource(func() error {
		if s.nc != nil && s.nc.IsConnected() {
			s.nc.Close()
		}
		return nil
	})

	return nil
}

// setupJetStream creates or updates the stream and consumer
func (s *Subscriber) setupJetStream() error {
	js, err := s.nc.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	s.js = js

	// Create or update stream
	streamConfig := &nats.StreamConfig{
		Name:       s.config.TracesStreamName,
		Subjects:   s.config.TracesSubjects,
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		MaxAge:     s.config.MaxAge,
		MaxBytes:   s.config.MaxBytes,
		Duplicates: s.config.DuplicateWindow,
		Replicas:   s.config.Replicas,
	}

	stream, err := js.StreamInfo(s.config.TracesStreamName)
	if err == nats.ErrStreamNotFound {
		// Create new stream
		_, err = js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		s.logger.Info("Created JetStream stream", zap.String("name", s.config.TracesStreamName))
	} else if err != nil {
		return fmt.Errorf("failed to get stream info: %w", err)
	} else {
		// Update existing stream
		streamConfig.Subjects = stream.Config.Subjects // Preserve existing subjects
		_, err = js.UpdateStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
	}

	// Create consumer
	consumerConfig := &nats.ConsumerConfig{
		Durable:       s.config.ConsumerName,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       s.config.AckWait,
		MaxDeliver:    s.config.MaxDeliver,
		FilterSubject: s.config.GetTracesSubject(),
		ReplayPolicy:  nats.ReplayInstantPolicy,
	}

	_, err = js.ConsumerInfo(s.config.TracesStreamName, s.config.ConsumerName)
	if err == nats.ErrConsumerNotFound {
		_, err = js.AddConsumer(s.config.TracesStreamName, consumerConfig)
		if err != nil {
			return fmt.Errorf("failed to create consumer: %w", err)
		}
		s.logger.Info("Created JetStream consumer", zap.String("name", s.config.ConsumerName))
	} else if err != nil {
		return fmt.Errorf("failed to get consumer info: %w", err)
	}

	return nil
}

// fetchMessages continuously fetches messages from the pull subscription with bounded retry
func (s *Subscriber) fetchMessages(ctx context.Context) {
	defer s.wg.Done()

	// Bounded retry configuration
	const (
		maxConsecutiveErrors = 10
		baseBackoffDelay     = time.Second
		maxBackoffDelay      = 30 * time.Second
		backoffMultiplier    = 2.0
	)

	consecutiveErrors := 0
	backoffDelay := baseBackoffDelay

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("fetchMessages context cancelled, shutting down")
			return
		default:
		}

		// Check if we've hit the error limit
		if consecutiveErrors >= maxConsecutiveErrors {
			s.logger.Error("Too many consecutive fetch errors, backing off",
				zap.Int("consecutive_errors", consecutiveErrors),
				zap.Duration("backoff_delay", backoffDelay))

			// Wait with context cancellation support
			timer := time.NewTimer(backoffDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				// Continue after backoff
			}

			// Exponential backoff with jitter
			backoffDelay = time.Duration(float64(backoffDelay) * backoffMultiplier)
			if backoffDelay > maxBackoffDelay {
				backoffDelay = maxBackoffDelay
			}
		}

		// Bound batch size to prevent memory exhaustion
		batchSize := s.config.BatchSize
		const maxBatchSize = 1000
		if batchSize > maxBatchSize {
			batchSize = maxBatchSize
			s.logger.Warn("Batch size exceeds maximum, capping",
				zap.Int("requested", s.config.BatchSize),
				zap.Int("capped_to", maxBatchSize))
		}

		// Fetch messages in bounded batches
		msgs, err := s.subscription.Fetch(batchSize, nats.MaxWait(s.config.FetchTimeout))

		if err != nil {
			if err == nats.ErrTimeout {
				// Reset error counters on successful timeout (normal operation)
				consecutiveErrors = 0
				backoffDelay = baseBackoffDelay
				continue // No messages available, keep polling
			}

			consecutiveErrors++
			s.logger.Warn("Failed to fetch messages",
				zap.Error(err),
				zap.Int("consecutive_errors", consecutiveErrors))

			// Short delay before retry to avoid tight loop
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				// Continue after short delay
			}
			continue
		}

		// Reset error counters on successful fetch
		consecutiveErrors = 0
		backoffDelay = baseBackoffDelay

		// Process each message with timeout protection
		for i, msg := range msgs {
			select {
			case <-ctx.Done():
				s.logger.Info("Context cancelled during message processing",
					zap.Int("processed", i),
					zap.Int("total", len(msgs)))
				return
			default:
				s.handleMessage(msg)
			}
		}
	}
}

// handleMessage processes a single NATS message with timeout protection
func (s *Subscriber) handleMessage(msg *nats.Msg) {
	// Update metrics atomically
	s.mu.Lock()
	s.messagesReceived++
	s.mu.Unlock()

	// Create processing context with timeout
	processCtx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()

	// Extract trace ID from subject
	traceID := s.extractTraceID(msg.Subject)

	// Parse event with validation
	var event domain.UnifiedEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("Failed to unmarshal event",
			zap.Error(err),
			zap.String("subject", msg.Subject),
			zap.String("trace_id", traceID),
		)
		s.nackMessage(msg)
		return
	}

	// Validate essential event fields
	if event.ID == "" {
		s.logger.Warn("Event missing required ID field",
			zap.String("subject", msg.Subject),
			zap.String("trace_id", traceID))
		s.nackMessage(msg)
		return
	}

	// Ensure event has trace context
	if event.TraceContext == nil && traceID != "" {
		event.TraceContext = &domain.TraceContext{
			TraceID: traceID,
		}
	} else if event.TraceContext != nil && traceID != "" && event.TraceContext.TraceID == "" {
		event.TraceContext.TraceID = traceID
	}

	// Process through correlation engine with timeout
	processingDone := make(chan error, 1)
	go func() {
		processingDone <- s.engine.Process(processCtx, &event)
	}()

	select {
	case err := <-processingDone:
		if err != nil {
			s.logger.Error("Failed to process event",
				zap.Error(err),
				zap.String("event_id", event.ID),
				zap.String("trace_id", traceID),
			)

			s.mu.Lock()
			s.processingErrors++
			s.mu.Unlock()

			// Check if this is the last delivery attempt
			if metadata, err := msg.Metadata(); err == nil && metadata != nil {
				if metadata.NumDelivered >= uint64(s.config.MaxDeliver-1) {
					// Last attempt, acknowledge to avoid infinite redelivery
					s.logger.Warn("Max delivery attempts reached, acknowledging message",
						zap.String("event_id", event.ID),
						zap.Uint64("deliveries", metadata.NumDelivered),
					)
					s.ackMessage(msg)
					return
				}
			} else {
				s.logger.Warn("Failed to get message metadata", zap.Error(err))
			}

			s.nackMessage(msg)
			return
		}

		// Successfully processed
		s.ackMessage(msg)
		s.logger.Debug("Event processed successfully",
			zap.String("event_id", event.ID),
			zap.String("trace_id", traceID),
			zap.String("type", string(event.Type)),
		)

	case <-processCtx.Done():
		// Processing timeout
		s.logger.Error("Event processing timeout",
			zap.String("event_id", event.ID),
			zap.String("trace_id", traceID),
			zap.Duration("timeout", 20*time.Second))

		s.mu.Lock()
		s.processingErrors++
		s.mu.Unlock()

		// Nack message for redelivery due to timeout
		s.nackMessage(msg)
	}
}

// extractTraceID extracts trace ID from NATS subject
func (s *Subscriber) extractTraceID(subject string) string {
	// Subject format: traces.{traceID}.{source}
	parts := strings.Split(subject, ".")
	if len(parts) >= 2 && parts[0] == "traces" {
		return parts[1]
	}
	return ""
}

// ackMessage acknowledges a message
func (s *Subscriber) ackMessage(msg *nats.Msg) {
	if err := msg.Ack(); err != nil {
		s.logger.Error("Failed to acknowledge message", zap.Error(err))
		return
	}

	s.mu.Lock()
	s.messagesAcked++
	s.mu.Unlock()
}

// nackMessage negatively acknowledges a message for redelivery
func (s *Subscriber) nackMessage(msg *nats.Msg) {
	if err := msg.Nak(); err != nil {
		s.logger.Error("Failed to nack message", zap.Error(err))
		return
	}

	s.mu.Lock()
	s.messagesNacked++
	s.mu.Unlock()
}

// metricsReporter periodically logs metrics
func (s *Subscriber) metricsReporter() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	var lastReceived, lastAcked, lastNacked int64
	lastReport := time.Now()

	for {
		select {
		case <-ticker.C:
			s.mu.RLock()
			received := s.messagesReceived
			acked := s.messagesAcked
			nacked := s.messagesNacked
			errors := s.processingErrors
			s.mu.RUnlock()

			// Calculate rates
			duration := time.Since(lastReport)
			receiveRate := float64(received-lastReceived) / duration.Seconds()
			ackRate := float64(acked-lastAcked) / duration.Seconds()
			nackRate := float64(nacked-lastNacked) / duration.Seconds()

			// Get subscription info
			pending, _, _ := s.subscription.Pending()

			s.logger.Info("NATS subscriber metrics",
				zap.Int64("total_received", received),
				zap.Int64("total_acked", acked),
				zap.Int64("total_nacked", nacked),
				zap.Int64("processing_errors", errors),
				zap.Float64("receive_rate", receiveRate),
				zap.Float64("ack_rate", ackRate),
				zap.Float64("nack_rate", nackRate),
				zap.Int("pending_messages", pending),
			)

			lastReceived = received
			lastAcked = acked
			lastNacked = nacked
			lastReport = time.Now()

		case <-s.ctx.Done():
			return
		}
	}
}

// GetMetrics returns current subscriber metrics
func (s *Subscriber) GetMetrics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pending, _, _ := s.subscription.Pending()

	return map[string]interface{}{
		"messages_received": s.messagesReceived,
		"messages_acked":    s.messagesAcked,
		"messages_nacked":   s.messagesNacked,
		"processing_errors": s.processingErrors,
		"pending_messages":  pending,
		"connected":         s.nc.IsConnected(),
	}
}

// addResource adds a cleanup function to the resource list
func (s *Subscriber) addResource(cleanup func() error) {
	s.resourcesMu.Lock()
	defer s.resourcesMu.Unlock()
	s.resources = append(s.resources, cleanup)
}

// cleanupResources calls all registered cleanup functions
func (s *Subscriber) cleanupResources() {
	s.resourcesMu.Lock()
	defer s.resourcesMu.Unlock()

	for i := len(s.resources) - 1; i >= 0; i-- {
		if err := s.resources[i](); err != nil {
			s.logger.Error("Failed to cleanup resource",
				zap.Int("resource_index", i), zap.Error(err))
		}
	}
	s.resources = nil
}
