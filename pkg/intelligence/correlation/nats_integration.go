package correlation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

// NATSCorrelationIntegration connects NATS to the existing correlation system
type NATSCorrelationIntegration struct {
	nc                *nats.Conn
	js                nats.JetStreamContext
	correlationSystem *SimpleCorrelationSystem
	logger            *zap.Logger
	subscription      *nats.Subscription
}

// NATSIntegrationConfig configures NATS integration
type NATSIntegrationConfig struct {
	NATSURL           string
	StreamName        string
	ConsumerName      string
	TraceSubjects     []string
	CorrelationSystem *SimpleCorrelationSystem
	Logger            *zap.Logger
}

// NewNATSCorrelationIntegration creates NATS integration with existing correlation
func NewNATSCorrelationIntegration(config *NATSIntegrationConfig) (*NATSCorrelationIntegration, error) {
	// Connect to NATS
	nc, err := nats.Connect(config.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Get JetStream context
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to get JetStream: %w", err)
	}

	integration := &NATSCorrelationIntegration{
		nc:                nc,
		js:                js,
		correlationSystem: config.CorrelationSystem,
		logger:            config.Logger,
	}

	// Create/update stream
	if err := integration.ensureStream(config.StreamName, config.TraceSubjects); err != nil {
		nc.Close()
		return nil, err
	}

	// Create consumer
	if err := integration.createConsumer(config.StreamName, config.ConsumerName); err != nil {
		nc.Close()
		return nil, err
	}

	return integration, nil
}

// Start begins processing events from NATS
func (n *NATSCorrelationIntegration) Start(ctx context.Context) error {
	// Subscribe to trace subjects
	sub, err := n.js.Subscribe("traces.>", n.handleMessage,
		nats.Durable("correlation-consumer"),
		nats.ManualAck(),
		nats.AckExplicit(),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	n.subscription = sub
	n.logger.Info("NATS correlation integration started",
		zap.String("subject", "traces.>"),
	)

	// Wait for context cancellation
	<-ctx.Done()
	return n.Stop()
}

// Stop stops the NATS integration
func (n *NATSCorrelationIntegration) Stop() error {
	if n.subscription != nil {
		if err := n.subscription.Unsubscribe(); err != nil {
			n.logger.Error("Failed to unsubscribe", zap.Error(err))
		}
	}

	if n.nc != nil {
		n.nc.Close()
	}

	return nil
}

// handleMessage processes messages from NATS
func (n *NATSCorrelationIntegration) handleMessage(msg *nats.Msg) {
	// Extract trace ID from subject
	traceID := n.extractTraceIDFromSubject(msg.Subject)

	// Parse event
	var event domain.UnifiedEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		n.logger.Error("Failed to unmarshal event",
			zap.Error(err),
			zap.String("subject", msg.Subject),
		)
		msg.Nak() // Negative acknowledgment
		return
	}

	// Ensure event has trace context
	if event.TraceContext == nil && traceID != "" {
		event.TraceContext = &domain.TraceContext{
			TraceID: traceID,
		}
	}

	// Process through correlation system
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := n.correlationSystem.ProcessEvent(ctx, &event); err != nil {
		n.logger.Error("Failed to process event",
			zap.Error(err),
			zap.String("event_id", event.ID),
			zap.String("trace_id", traceID),
		)
		msg.Nak()
		return
	}

	// Acknowledge successful processing
	msg.Ack()

	n.logger.Debug("Event processed successfully",
		zap.String("event_id", event.ID),
		zap.String("trace_id", traceID),
		zap.String("type", string(event.Type)),
	)
}

// extractTraceIDFromSubject extracts trace ID from NATS subject
func (n *NATSCorrelationIntegration) extractTraceIDFromSubject(subject string) string {
	// Subject format: traces.{traceID}.{source}
	parts := strings.Split(subject, ".")
	if len(parts) >= 2 && parts[0] == "traces" {
		return parts[1]
	}
	return ""
}

// ensureStream creates or updates the JetStream stream
func (n *NATSCorrelationIntegration) ensureStream(streamName string, subjects []string) error {
	// Check if stream exists
	stream, err := n.js.StreamInfo(streamName)
	if err != nil && err != nats.ErrStreamNotFound {
		return fmt.Errorf("failed to get stream info: %w", err)
	}

	if stream == nil {
		// Create new stream
		_, err = n.js.AddStream(&nats.StreamConfig{
			Name:       streamName,
			Subjects:   subjects,
			Storage:    nats.FileStorage,
			Retention:  nats.LimitsPolicy,
			MaxAge:     24 * time.Hour,
			Duplicates: 30 * time.Minute,
			Replicas:   1,
		})
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		n.logger.Info("Created JetStream stream",
			zap.String("name", streamName),
			zap.Strings("subjects", subjects),
		)
	}

	return nil
}

// createConsumer creates a durable consumer
func (n *NATSCorrelationIntegration) createConsumer(streamName, consumerName string) error {
	_, err := n.js.AddConsumer(streamName, &nats.ConsumerConfig{
		Durable:       consumerName,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	})
	if err != nil && err != nats.ErrConsumerNameAlreadyInUse {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	return nil
}

// PublishCorrelationResult publishes correlation results back to NATS
func (n *NATSCorrelationIntegration) PublishCorrelationResult(result interface{}) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	// Publish to correlation results subject
	subject := "correlations.results"
	if _, err := n.js.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish result: %w", err)
	}

	return nil
}
