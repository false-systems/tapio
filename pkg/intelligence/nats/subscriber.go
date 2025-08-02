package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
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
	config Config

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

// Config configures the NATS subscriber
type Config struct {
	// NATS connection
	URL          string
	StreamName   string
	ConsumerName string

	// Subscription
	Subject    string        // e.g., "traces.>"
	QueueGroup string        // For load balancing
	MaxPending int           // Max unprocessed messages
	AckWait    time.Duration // Time to process before redelivery
	MaxDeliver int           // Max delivery attempts

	// Processing
	WorkerCount int // Concurrent message processors
}

// DefaultConfig returns production-ready defaults
func DefaultConfig() Config {
	return Config{
		URL:          "nats://localhost:4222",
		StreamName:   "TRACES",
		ConsumerName: "correlation-service",
		Subject:      "traces.>",
		QueueGroup:   "correlation",
		MaxPending:   1000,
		AckWait:      30 * time.Second,
		MaxDeliver:   3,
		WorkerCount:  10,
	}
}

// NewSubscriber creates a new NATS subscriber
func NewSubscriber(logger *zap.Logger, config Config, engine *correlation.Engine) (*Subscriber, error) {
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
		zap.String("stream", s.config.StreamName),
		zap.String("subject", s.config.Subject),
		zap.String("consumer", s.config.ConsumerName),
	)

	// Subscribe to messages
	sub, err := s.js.QueueSubscribe(
		s.config.Subject,
		s.config.QueueGroup,
		s.handleMessage,
		nats.Durable(s.config.ConsumerName),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.MaxAckPending(s.config.MaxPending),
		nats.AckWait(s.config.AckWait),
		nats.MaxDeliver(s.config.MaxDeliver),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	s.subscription = sub

	// Start metrics reporter
	s.wg.Add(1)
	go s.metricsReporter()

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
		nats.Name("correlation-service"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(time.Second),
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
		Name:       s.config.StreamName,
		Subjects:   []string{s.config.Subject},
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		MaxAge:     24 * time.Hour,
		MaxBytes:   10 * 1024 * 1024 * 1024, // 10GB
		Duplicates: 30 * time.Minute,
		Replicas:   1,
	}

	stream, err := js.StreamInfo(s.config.StreamName)
	if err == nats.ErrStreamNotFound {
		// Create new stream
		_, err = js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		s.logger.Info("Created JetStream stream", zap.String("name", s.config.StreamName))
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
		FilterSubject: s.config.Subject,
		ReplayPolicy:  nats.ReplayInstantPolicy,
	}

	_, err = js.ConsumerInfo(s.config.StreamName, s.config.ConsumerName)
	if err == nats.ErrConsumerNotFound {
		_, err = js.AddConsumer(s.config.StreamName, consumerConfig)
		if err != nil {
			return fmt.Errorf("failed to create consumer: %w", err)
		}
		s.logger.Info("Created JetStream consumer", zap.String("name", s.config.ConsumerName))
	} else if err != nil {
		return fmt.Errorf("failed to get consumer info: %w", err)
	}

	return nil
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
