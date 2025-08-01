package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// PublisherConfig configures the NATS event publisher
type PublisherConfig struct {
	// Connection
	URL            string
	Name           string // Client name
	ConnectTimeout time.Duration

	// JetStream
	StreamName   string
	StreamConfig *StreamConfig

	// Performance
	MaxPending   int  // Max async publishes
	AsyncPublish bool // Use async publishing

	// Resilience
	ReconnectWait   time.Duration
	MaxReconnects   int
	DisconnectErrCB func(error)
	ReconnectedCB   func()
}

// StreamConfig defines JetStream stream configuration
type StreamConfig struct {
	Subjects     []string
	MaxBytes     int64
	MaxAge       time.Duration
	MaxMsgs      int64
	MaxConsumers int
	Retention    string // "limits", "interest", "workqueue"
	Storage      string // "file", "memory"
	Replicas     int
}

// EventPublisher publishes events to NATS JetStream
type EventPublisher struct {
	nc     *natsgo.Conn
	js     natsgo.JetStreamContext
	config *PublisherConfig

	mu      sync.RWMutex
	closed  bool
	closeCh chan struct{}
}

// NewEventPublisher creates a new NATS event publisher
func NewEventPublisher(config *PublisherConfig) (*EventPublisher, error) {
	// Set defaults
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 10 * time.Second
	}
	if config.MaxPending == 0 {
		config.MaxPending = 256
	}
	if config.ReconnectWait == 0 {
		config.ReconnectWait = 2 * time.Second
	}
	if config.MaxReconnects == 0 {
		config.MaxReconnects = 60
	}
	if config.StreamName == "" {
		config.StreamName = "TAPIO_EVENTS"
	}

	// Connection options
	opts := []natsgo.Option{
		natsgo.Timeout(config.ConnectTimeout),
		natsgo.ReconnectWait(config.ReconnectWait),
		natsgo.MaxReconnects(config.MaxReconnects),
	}

	if config.Name != "" {
		opts = append(opts, natsgo.Name(config.Name))
	}

	if config.DisconnectErrCB != nil {
		opts = append(opts, natsgo.DisconnectErrHandler(func(_ *natsgo.Conn, err error) {
			config.DisconnectErrCB(err)
		}))
	}

	if config.ReconnectedCB != nil {
		opts = append(opts, natsgo.ReconnectHandler(func(_ *natsgo.Conn) {
			config.ReconnectedCB()
		}))
	}

	// Connect
	nc, err := natsgo.Connect(config.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Get JetStream context
	jsOpts := []natsgo.JSOpt{}
	if config.AsyncPublish && config.MaxPending > 0 {
		jsOpts = append(jsOpts, natsgo.PublishAsyncMaxPending(config.MaxPending))
	}

	js, err := nc.JetStream(jsOpts...)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	publisher := &EventPublisher{
		nc:      nc,
		js:      js,
		config:  config,
		closeCh: make(chan struct{}),
	}

	// Create or update stream
	if err := publisher.ensureStream(); err != nil {
		nc.Close()
		return nil, err
	}

	return publisher, nil
}

// ensureStream creates or updates the JetStream stream
func (p *EventPublisher) ensureStream() error {
	streamConfig := p.config.StreamConfig
	if streamConfig == nil {
		// Default stream config - use stream-specific subjects to avoid overlap
		prefix := "events"
		if strings.HasPrefix(p.config.StreamName, "TEST_") {
			// For tests, use unique subject prefix based on stream name
			prefix = strings.ToLower(strings.ReplaceAll(p.config.StreamName, "_", "."))
		}
		streamConfig = &StreamConfig{
			Subjects:  []string{prefix + ".>"},
			MaxBytes:  1024 * 1024 * 1024, // 1GB
			MaxAge:    7 * 24 * time.Hour,
			MaxMsgs:   10000000,
			Retention: "limits",
			Storage:   "file",
			Replicas:  1,
		}
	}

	cfg := &natsgo.StreamConfig{
		Name:         p.config.StreamName,
		Subjects:     streamConfig.Subjects,
		MaxBytes:     streamConfig.MaxBytes,
		MaxAge:       streamConfig.MaxAge,
		MaxMsgs:      streamConfig.MaxMsgs,
		MaxConsumers: streamConfig.MaxConsumers,
		Retention:    natsgo.LimitsPolicy,
		Storage:      natsgo.FileStorage,
		Replicas:     streamConfig.Replicas,
	}

	// Set retention policy
	switch streamConfig.Retention {
	case "interest":
		cfg.Retention = natsgo.InterestPolicy
	case "workqueue":
		cfg.Retention = natsgo.WorkQueuePolicy
	}

	// Set storage type
	if streamConfig.Storage == "memory" {
		cfg.Storage = natsgo.MemoryStorage
	}

	// Create or update
	info, err := p.js.StreamInfo(p.config.StreamName)
	if err != nil && err == natsgo.ErrStreamNotFound {
		// Create stream
		_, err = p.js.AddStream(cfg)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get stream info: %w", err)
	}

	// Stream exists, update if needed
	if !equalSubjects(info.Config.Subjects, cfg.Subjects) {
		_, err = p.js.UpdateStream(cfg)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
	}

	return nil
}

// PublishRawEvent publishes a raw collector event
func (p *EventPublisher) PublishRawEvent(ctx context.Context, event collectors.RawEvent) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	// Generate subject
	subject := p.generateRawEventSubject(event)

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create message with headers
	msg := &natsgo.Msg{
		Subject: subject,
		Data:    data,
		Header:  natsgo.Header{},
	}

	// Add headers
	msg.Header.Set("Event-Type", "raw")
	msg.Header.Set("Collector-Type", event.Type)
	msg.Header.Set("Timestamp", event.Timestamp.Format(time.RFC3339Nano))

	// Add trace context if present
	if event.TraceID != "" {
		msg.Header.Set("Trace-ID", event.TraceID)
	}
	if event.SpanID != "" {
		msg.Header.Set("Span-ID", event.SpanID)
	}

	// Publish to main subject
	if p.config.AsyncPublish {
		_, err = p.js.PublishMsgAsync(msg)
	} else {
		_, err = p.js.PublishMsg(msg, natsgo.Context(ctx))
	}

	if err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	// Also publish to trace subject for correlation if we have a trace ID
	if event.TraceID != "" {
		traceSubject := p.generateTraceSubject(event.TraceID)
		traceMsg := &natsgo.Msg{
			Subject: traceSubject,
			Data:    data,
			Header:  msg.Header, // Copy headers
		}
		
		// Publish to trace subject (don't fail if this fails)
		if p.config.AsyncPublish {
			p.js.PublishMsgAsync(traceMsg)
		} else {
			p.js.PublishMsg(traceMsg, natsgo.Context(ctx))
		}
	}

	return nil
}

// PublishUnifiedEvent publishes a unified domain event
func (p *EventPublisher) PublishUnifiedEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	// Generate multi-dimensional subject
	subjects := p.generateUnifiedEventSubjects(event)
	if len(subjects) == 0 {
		return fmt.Errorf("no subjects generated for event")
	}

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Use primary subject
	msg := &natsgo.Msg{
		Subject: subjects[0],
		Data:    data,
		Header:  natsgo.Header{},
	}

	// Add headers
	msg.Header.Set("Event-Type", "unified")
	msg.Header.Set("Event-ID", event.ID)
	msg.Header.Set("Source", event.Source)
	msg.Header.Set("Severity", string(event.Severity))
	msg.Header.Set("Timestamp", event.Timestamp.Format(time.RFC3339Nano))

	// Add trace context
	if event.TraceContext != nil {
		msg.Header.Set("Trace-ID", event.TraceContext.TraceID)
		msg.Header.Set("Span-ID", event.TraceContext.SpanID)
		if event.TraceContext.ParentSpanID != "" {
			msg.Header.Set("Parent-Span-ID", event.TraceContext.ParentSpanID)
		}
	}

	// Add semantic context
	if event.Semantic != nil {
		msg.Header.Set("Semantic-Intent", event.Semantic.Intent)
		msg.Header.Set("Semantic-Category", event.Semantic.Category)
	}

	// Publish to primary subject
	if p.config.AsyncPublish {
		_, err = p.js.PublishMsgAsync(msg)
	} else {
		_, err = p.js.PublishMsg(msg, natsgo.Context(ctx))
	}

	if err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	// Also publish to trace subject for correlation if we have a trace ID
	if event.TraceContext != nil && event.TraceContext.TraceID != "" {
		traceSubject := p.generateTraceSubject(event.TraceContext.TraceID)
		traceMsg := &natsgo.Msg{
			Subject: traceSubject,
			Data:    data,
			Header:  msg.Header, // Copy headers
		}
		
		// Publish to trace subject (don't fail if this fails)
		if p.config.AsyncPublish {
			p.js.PublishMsgAsync(traceMsg)
		} else {
			p.js.PublishMsg(traceMsg, natsgo.Context(ctx))
		}
	}

	return nil
}

// generateRawEventSubject creates subject for raw events
func (p *EventPublisher) generateRawEventSubject(event collectors.RawEvent) string {
	// Use appropriate prefix based on stream name
	prefix := "events"
	if strings.HasPrefix(p.config.StreamName, "TEST_") {
		prefix = strings.ToLower(strings.ReplaceAll(p.config.StreamName, "_", "."))
	}

	parts := []string{prefix, "raw", event.Type}

	// Add namespace for kubeapi events
	if event.Type == "kubeapi" && event.Metadata["namespace"] != "" {
		parts = append(parts, event.Metadata["namespace"])
	}

	// Add severity if critical
	if event.Metadata["severity"] == "critical" {
		parts = append(parts, "critical")
	}

	return strings.Join(parts, ".")
}

// generateTraceSubject creates subject for trace-based routing
func (p *EventPublisher) generateTraceSubject(traceID string) string {
	prefix := "traces"
	if strings.HasPrefix(p.config.StreamName, "TEST_") {
		basePrefix := strings.ToLower(strings.ReplaceAll(p.config.StreamName, "_", "."))
		prefix = basePrefix + ".traces"
	}
	return fmt.Sprintf("%s.%s", prefix, traceID)
}

// generateUnifiedEventSubjects creates multi-dimensional subjects
func (p *EventPublisher) generateUnifiedEventSubjects(event *domain.UnifiedEvent) []string {
	subjects := []string{}

	// Use appropriate prefix based on stream name
	prefix := "events"
	tracesPrefix := "traces"
	if strings.HasPrefix(p.config.StreamName, "TEST_") {
		basePrefix := strings.ToLower(strings.ReplaceAll(p.config.StreamName, "_", "."))
		prefix = basePrefix
		tracesPrefix = basePrefix + ".traces"
	}

	// Primary subject based on event type and entity
	if event.Entity != nil {
		parts := []string{prefix, "unified", strings.ToLower(string(event.Type))}
		if event.Entity.Namespace != "" {
			parts = append(parts, event.Entity.Namespace)
		}
		if event.Entity.Name != "" {
			parts = append(parts, event.Entity.Name)
		}
		subjects = append(subjects, strings.Join(parts, "."))
	} else {
		// Fallback to simple type-based
		subjects = append(subjects, fmt.Sprintf("%s.unified.%s", prefix, strings.ToLower(string(event.Type))))
	}

	// Severity-based routing for critical events
	if event.Severity == domain.EventSeverityCritical {
		subjects = append(subjects, fmt.Sprintf("%s.unified.critical", prefix))
	}

	// Semantic category routing
	if event.Semantic != nil && event.Semantic.Category != "" {
		subjects = append(subjects, fmt.Sprintf("%s.unified.%s", prefix, event.Semantic.Category))
	}

	// Trace-based routing for correlation
	if event.TraceContext != nil && event.TraceContext.TraceID != "" {
		subjects = append(subjects, fmt.Sprintf("%s.%s", tracesPrefix, event.TraceContext.TraceID))
	}

	return subjects
}

// Close gracefully shuts down the publisher
func (p *EventPublisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closeCh)
	p.mu.Unlock()

	// Flush any pending async publishes
	if p.config.AsyncPublish {
		select {
		case <-p.js.PublishAsyncComplete():
		case <-time.After(5 * time.Second):
			// Timeout waiting for pending publishes
		}
	}

	// Close connection
	p.nc.Close()

	return nil
}

// HealthCheck verifies NATS connection
func (p *EventPublisher) HealthCheck() error {
	if !p.nc.IsConnected() {
		return fmt.Errorf("not connected to NATS")
	}

	// Try to get stream info
	_, err := p.js.StreamInfo(p.config.StreamName)
	if err != nil {
		return fmt.Errorf("stream health check failed: %w", err)
	}

	return nil
}

// equalSubjects compares two subject slices
func equalSubjects(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
