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

	// State
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	mu               sync.RWMutex
	messagesReceived int64
	messagesAcked    int64
	messagesNacked   int64
	processingErrors int64
}

// Deprecated: Use config.NATSConfig instead
// DefaultConfig returns the new centralized config
func DefaultConfig() *config.NATSConfig {
	return config.DefaultNATSConfig()
}

// NewSubscriber creates a new NATS subscriber
func NewSubscriber(logger *zap.Logger, config *config.NATSConfig, engine *correlation.Engine) (*Subscriber, error) {
	ctx, cancel := context.WithCancel(context.Background())

	sub := &Subscriber{
		logger: logger,
		engine: engine,
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	// Connect to NATS
	if err := sub.connect(); err != nil {
		cancel()
		return nil, err
	}

	// Setup JetStream
	if err := sub.setupJetStream(); err != nil {
		sub.nc.Close()
		cancel()
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

// Stop gracefully shuts down the subscriber
func (s *Subscriber) Stop() error {
	s.logger.Info("Stopping NATS subscriber")

	// Cancel internal context
	s.cancel()

	// Unsubscribe
	if s.subscription != nil {
		if err := s.subscription.Unsubscribe(); err != nil {
			s.logger.Error("Failed to unsubscribe", zap.Error(err))
		}
	}

	// Wait for goroutines
	s.wg.Wait()

	// Close NATS connection
	if s.nc != nil {
		s.nc.Close()
	}

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

// fetchMessages continuously fetches messages from the pull subscription
func (s *Subscriber) fetchMessages(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch messages in batches
		msgs, err := s.subscription.Fetch(s.config.BatchSize, nats.MaxWait(s.config.FetchTimeout))
		if err != nil {
			if err == nats.ErrTimeout {
				continue // No messages available, keep polling
			}
			s.logger.Error("Failed to fetch messages", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}

		// Process each message
		for _, msg := range msgs {
			s.handleMessage(msg)
		}
	}
}

// handleMessage processes a single NATS message
func (s *Subscriber) handleMessage(msg *nats.Msg) {
	// Update metrics
	s.mu.Lock()
	s.messagesReceived++
	s.mu.Unlock()

	// Extract trace ID from subject
	traceID := s.extractTraceID(msg.Subject)

	// Parse event
	var event domain.UnifiedEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		s.logger.Error("Failed to unmarshal event",
			zap.Error(err),
			zap.String("subject", msg.Subject),
		)
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

	// Process through correlation engine
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()

	if err := s.engine.Process(ctx, &event); err != nil {
		s.logger.Error("Failed to process event",
			zap.Error(err),
			zap.String("event_id", event.ID),
			zap.String("trace_id", traceID),
		)

		s.mu.Lock()
		s.processingErrors++
		s.mu.Unlock()

		// Check if this is the last delivery attempt
		metadata, _ := msg.Metadata()
		if metadata != nil && metadata.NumDelivered >= uint64(s.config.MaxDeliver-1) {
			// Last attempt, acknowledge to avoid infinite redelivery
			s.logger.Warn("Max delivery attempts reached, acknowledging message",
				zap.String("event_id", event.ID),
				zap.Uint64("deliveries", metadata.NumDelivered),
			)
			s.ackMessage(msg)
		} else {
			s.nackMessage(msg)
		}
		return
	}

	// Successfully processed
	s.ackMessage(msg)

	s.logger.Debug("Event processed successfully",
		zap.String("event_id", event.ID),
		zap.String("trace_id", traceID),
		zap.String("type", string(event.Type)),
	)
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
