package pipeline

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NATSPublisher publishes events to NATS
type NATSPublisher struct {
	logger      *zap.Logger
	nc          *nats.Conn
	js          nats.JetStreamContext
	subjectBase string
}

// NewNATSPublisher creates a new NATS publisher
func NewNATSPublisher(logger *zap.Logger, url, subjectBase string) (*NATSPublisher, error) {
	if url == "" {
		// Return nil publisher for testing
		return nil, nil
	}

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

	// Ensure stream exists
	streamName := "TRACES"
	_, err = js.StreamInfo(streamName)
	if err != nil {
		// Create stream
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: []string{subjectBase + ".>"},
			Storage:  nats.FileStorage,
			MaxAge:   24 * time.Hour,
		})
		if err != nil {
			nc.Close()
			return nil, fmt.Errorf("failed to create stream: %w", err)
		}
	}

	return &NATSPublisher{
		logger:      logger,
		nc:          nc,
		js:          js,
		subjectBase: subjectBase,
	}, nil
}

// Publish sends unified event to NATS
func (p *NATSPublisher) Publish(event *domain.UnifiedEvent) error {
	// Generate subject based on trace ID and source
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
			zap.String("event_id", event.ID),
			zap.Error(err),
		)
		return err
	}

	p.logger.Debug("Published event",
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
	return fmt.Sprintf("%s.%s.%s", p.subjectBase, traceID, event.Source)
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
