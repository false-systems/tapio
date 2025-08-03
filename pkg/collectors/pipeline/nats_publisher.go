package pipeline

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NATSPublisher publishes events to NATS
type NATSPublisher struct {
	logger *zap.Logger
	nc     *nats.Conn
	js     nats.JetStreamContext
	config *config.NATSConfig
}

// NewNATSPublisher creates a new NATS publisher
func NewNATSPublisher(logger *zap.Logger, natsConfig *config.NATSConfig) (*NATSPublisher, error) {
	if natsConfig == nil || natsConfig.URL == "" {
		// Return nil publisher for testing
		return nil, nil
	}

	// Connect with retry
	nc, err := nats.Connect(natsConfig.URL,
		nats.Name(natsConfig.Name),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(natsConfig.MaxReconnects),
		nats.ReconnectWait(natsConfig.ReconnectWait),
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
	_, err = js.StreamInfo(natsConfig.TracesStreamName)
	if err != nil {
		// Create stream
		_, err = js.AddStream(&nats.StreamConfig{
			Name:       natsConfig.TracesStreamName,
			Subjects:   natsConfig.TracesSubjects,
			Storage:    nats.FileStorage,
			MaxAge:     natsConfig.MaxAge,
			MaxBytes:   natsConfig.MaxBytes,
			Duplicates: natsConfig.DuplicateWindow,
			Replicas:   natsConfig.Replicas,
		})
		if err != nil {
			nc.Close()
			return nil, fmt.Errorf("failed to create stream: %w", err)
		}
	}

	return &NATSPublisher{
		logger: logger,
		nc:     nc,
		js:     js,
		config: natsConfig,
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
	baseSubject := "traces" // Default from config
	if len(p.config.TracesSubjects) > 0 {
		// Use first subject without wildcard
		baseSubject = p.config.TracesSubjects[0]
		if len(baseSubject) > 2 && baseSubject[len(baseSubject)-2:] == ".>" {
			baseSubject = baseSubject[:len(baseSubject)-2]
		}
	}
	return fmt.Sprintf("%s.%s.%s", baseSubject, traceID, event.Source)
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
