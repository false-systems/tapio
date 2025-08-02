package ebpf

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

// NATSPublisher publishes eBPF events to NATS
type NATSPublisher struct {
	nc     *nats.Conn
	js     nats.JetStreamContext
	logger *zap.Logger
}

// NewNATSPublisher creates a new NATS publisher
func NewNATSPublisher(url string, logger *zap.Logger) (*NATSPublisher, error) {
	// Connect with retry
	nc, err := nats.Connect(url,
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
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	return &NATSPublisher{
		nc:     nc,
		js:     js,
		logger: logger,
	}, nil
}

// PublishEvent publishes a unified event to NATS
func (p *NATSPublisher) PublishEvent(event *domain.UnifiedEvent) error {
	// Generate subject based on trace ID
	subject := p.generateSubject(event)

	// Marshal event
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to JetStream
	_, err = p.js.Publish(subject, data)
	if err != nil {
		p.logger.Error("Failed to publish event",
			zap.String("subject", subject),
			zap.Error(err),
		)
		return err
	}

	p.logger.Debug("Published event to NATS",
		zap.String("subject", subject),
		zap.String("event_id", event.ID),
		zap.String("trace_id", p.getTraceID(event)),
	)

	return nil
}

// generateSubject creates NATS subject for event
func (p *NATSPublisher) generateSubject(event *domain.UnifiedEvent) string {
	traceID := p.getTraceID(event)
	if traceID == "" {
		// No trace ID, use event ID
		traceID = event.ID
	}

	// Subject format: traces.{traceID}.{source}
	return fmt.Sprintf("traces.%s.%s", traceID, event.Source)
}

// getTraceID extracts trace ID from event
func (p *NATSPublisher) getTraceID(event *domain.UnifiedEvent) string {
	if event.TraceContext != nil && event.TraceContext.TraceID != "" {
		return event.TraceContext.TraceID
	}

	// Check correlation hints
	for _, hint := range event.CorrelationHints {
		if len(hint) > 8 { // Basic trace ID validation
			return hint
		}
	}

	return ""
}

// Close closes the NATS connection
func (p *NATSPublisher) Close() {
	if p.nc != nil {
		p.nc.Close()
	}
}
