package correlation

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
	"github.com/yairfalse/tapio/pkg/integrations/transformer"
	"go.uber.org/zap"
)

// NATSSubscriberConfig configures the NATS correlation subscriber
type NATSSubscriberConfig struct {
	// NATS connection
	URL        string
	StreamName string
	Name       string // Consumer name

	// Subscription patterns
	TraceSubjects    []string // e.g., ["traces.>"]
	RawEventSubjects []string // e.g., ["events.raw.>"]

	// Processing
	BatchSize       int           // Events to process together
	BatchTimeout    time.Duration // Max time to wait for batch
	WorkerCount     int           // Concurrent correlation workers
	MaxPending      int           // Max unprocessed messages

	// Correlation
	CorrelationWindow time.Duration // How long to collect related events
	MinEventsForCorr  int           // Minimum events needed for correlation

	Logger *zap.Logger
}

// NATSSubscriber subscribes to NATS subjects and correlates events
type NATSSubscriber struct {
	config *NATSSubscriberConfig
	logger *zap.Logger

	// NATS connection
	nc *natsgo.Conn
	js natsgo.JetStreamContext

	// Correlation engine
	correlationEngine CorrelationEngine
	transformer       *transformer.EventTransformer

	// Trace-based event grouping
	traceEvents map[string][]*domain.UnifiedEvent // traceID -> events
	traceMu     sync.RWMutex

	// Processing
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Channels
	rawEventCh     chan collectors.RawEvent
	unifiedEventCh chan *domain.UnifiedEvent
	resultsCh      chan []*MultiDimCorrelationResult

	// Lifecycle
	started bool
	mu      sync.RWMutex
}

// NewNATSSubscriber creates a new NATS-based correlation subscriber
func NewNATSSubscriber(config *NATSSubscriberConfig, correlationEngine CorrelationEngine) (*NATSSubscriber, error) {
	// Set defaults
	if config.BatchSize == 0 {
		config.BatchSize = 10
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 5 * time.Second
	}
	if config.WorkerCount == 0 {
		config.WorkerCount = 4
	}
	if config.MaxPending == 0 {
		config.MaxPending = 1000
	}
	if config.CorrelationWindow == 0 {
		config.CorrelationWindow = 30 * time.Second
	}
	if config.MinEventsForCorr == 0 {
		config.MinEventsForCorr = 2
	}
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	// Connect to NATS
	nc, err := natsgo.Connect(config.URL, 
		natsgo.Name(config.Name),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Get JetStream context
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	return &NATSSubscriber{
		config:            config,
		logger:            config.Logger,
		nc:                nc,
		js:                js,
		correlationEngine: correlationEngine,
		transformer:       transformer.NewEventTransformer(),
		traceEvents:       make(map[string][]*domain.UnifiedEvent),
		rawEventCh:        make(chan collectors.RawEvent, config.MaxPending),
		unifiedEventCh:    make(chan *domain.UnifiedEvent, config.MaxPending),
		resultsCh:         make(chan []*MultiDimCorrelationResult, 100),
	}, nil
}

// Start begins consuming events from NATS and correlating them
func (s *NATSSubscriber) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("subscriber already started")
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started = true

	// Start subscribers
	if err := s.startSubscriptions(); err != nil {
		return fmt.Errorf("failed to start subscriptions: %w", err)
	}

	// Start processing workers
	s.startWorkers()

	// Start correlation timer
	s.startCorrelationScheduler()

	s.logger.Info("NATS correlation subscriber started",
		zap.Strings("trace_subjects", s.config.TraceSubjects),
		zap.Strings("raw_subjects", s.config.RawEventSubjects),
		zap.Int("workers", s.config.WorkerCount))

	return nil
}

// Stop gracefully shuts down the subscriber
func (s *NATSSubscriber) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.logger.Info("Stopping NATS correlation subscriber...")
	
	s.cancel()
	s.wg.Wait()
	
	close(s.rawEventCh)
	close(s.unifiedEventCh)
	close(s.resultsCh)
	
	s.nc.Close()
	s.started = false

	s.logger.Info("NATS correlation subscriber stopped")
	return nil
}

// Results returns the correlation results channel
func (s *NATSSubscriber) Results() <-chan []*MultiDimCorrelationResult {
	return s.resultsCh
}

// startSubscriptions creates NATS subscriptions
func (s *NATSSubscriber) startSubscriptions() error {
	// Subscribe to trace subjects for correlation
	for _, subject := range s.config.TraceSubjects {
		consumerName := fmt.Sprintf("%s-trace-%d", s.config.Name, time.Now().UnixNano())
		
		_, err := s.js.Subscribe(subject, s.handleTraceMessage,
			natsgo.Durable(consumerName),
			natsgo.MaxDeliver(3),
			natsgo.AckExplicit(),
			natsgo.MaxAckPending(s.config.MaxPending))
		if err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
		}

		s.logger.Info("Subscribed to trace subject",
			zap.String("subject", subject),
			zap.String("consumer", consumerName))
	}

	// Subscribe to raw event subjects if configured
	for _, subject := range s.config.RawEventSubjects {
		consumerName := fmt.Sprintf("%s-raw-%d", s.config.Name, time.Now().UnixNano())
		
		_, err := s.js.Subscribe(subject, s.handleRawMessage,
			natsgo.Durable(consumerName),
			natsgo.MaxDeliver(3),
			natsgo.AckExplicit(),
			natsgo.MaxAckPending(s.config.MaxPending))
		if err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
		}

		s.logger.Info("Subscribed to raw event subject",
			zap.String("subject", subject),
			zap.String("consumer", consumerName))
	}

	return nil
}

// handleTraceMessage processes messages from trace subjects
func (s *NATSSubscriber) handleTraceMessage(msg *natsgo.Msg) {
	// Extract trace ID from subject (e.g., traces.abc123def456)
	traceID := s.extractTraceIDFromSubject(msg.Subject)
	if traceID == "" {
		s.logger.Warn("Could not extract trace ID from subject", zap.String("subject", msg.Subject))
		msg.Ack()
		return
	}

	// Try to parse as unified event first
	var unifiedEvent domain.UnifiedEvent
	if err := json.Unmarshal(msg.Data, &unifiedEvent); err == nil {
		s.logger.Debug("Received unified event for trace",
			zap.String("trace_id", traceID),
			zap.String("event_id", unifiedEvent.ID),
			zap.String("source", unifiedEvent.Source))
		
		s.addEventToTrace(traceID, &unifiedEvent)
		msg.Ack()
		return
	}

	// Try to parse as raw event
	var rawEvent collectors.RawEvent
	if err := json.Unmarshal(msg.Data, &rawEvent); err == nil {
		s.logger.Debug("Received raw event for trace",
			zap.String("trace_id", traceID),
			zap.String("type", rawEvent.Type))
		
		select {
		case s.rawEventCh <- rawEvent:
		case <-s.ctx.Done():
			return
		}
		msg.Ack()
		return
	}

	s.logger.Warn("Could not parse message data", 
		zap.String("subject", msg.Subject))
	msg.Ack()
}

// handleRawMessage processes messages from raw event subjects
func (s *NATSSubscriber) handleRawMessage(msg *natsgo.Msg) {
	var rawEvent collectors.RawEvent
	if err := json.Unmarshal(msg.Data, &rawEvent); err != nil {
		s.logger.Warn("Could not parse raw event", zap.Error(err))
		msg.Ack()
		return
	}

	select {
	case s.rawEventCh <- rawEvent:
	case <-s.ctx.Done():
		return
	}
	msg.Ack()
}

// startWorkers starts processing workers
func (s *NATSSubscriber) startWorkers() {
	// Raw event transformation workers
	for i := 0; i < s.config.WorkerCount; i++ {
		s.wg.Add(1)
		go s.rawEventWorker()
	}

	// Unified event processing workers
	for i := 0; i < s.config.WorkerCount; i++ {
		s.wg.Add(1)
		go s.unifiedEventWorker()
	}
}

// rawEventWorker transforms raw events to unified events
func (s *NATSSubscriber) rawEventWorker() {
	defer s.wg.Done()

	for {
		select {
		case rawEvent := <-s.rawEventCh:
			// Transform to unified event
			unifiedEvent, err := s.transformer.Transform(s.ctx, rawEvent)
			if err != nil {
				s.logger.Warn("Failed to transform raw event",
					zap.String("type", rawEvent.Type),
					zap.Error(err))
				continue
			}

			// Add to trace group if we have a trace ID
			if unifiedEvent.TraceContext != nil && unifiedEvent.TraceContext.TraceID != "" {
				s.addEventToTrace(unifiedEvent.TraceContext.TraceID, unifiedEvent)
			} else {
				// Process individual event if no trace context
				select {
				case s.unifiedEventCh <- unifiedEvent:
				case <-s.ctx.Done():
					return
				}
			}

		case <-s.ctx.Done():
			return
		}
	}
}

// unifiedEventWorker processes individual unified events
func (s *NATSSubscriber) unifiedEventWorker() {
	defer s.wg.Done()

	for {
		select {
		case event := <-s.unifiedEventCh:
			// Process single event through correlation engine
			results, err := s.correlationEngine.Process(s.ctx, event)
			if err != nil {
				s.logger.Warn("Correlation processing failed",
					zap.String("event_id", event.ID),
					zap.Error(err))
				continue
			}

			if len(results) > 0 {
				select {
				case s.resultsCh <- results:
				case <-s.ctx.Done():
					return
				}
			}

		case <-s.ctx.Done():
			return
		}
	}
}

// addEventToTrace adds an event to a trace group
func (s *NATSSubscriber) addEventToTrace(traceID string, event *domain.UnifiedEvent) {
	s.traceMu.Lock()
	defer s.traceMu.Unlock()

	s.traceEvents[traceID] = append(s.traceEvents[traceID], event)
	
	s.logger.Debug("Added event to trace",
		zap.String("trace_id", traceID),
		zap.String("event_id", event.ID),
		zap.Int("trace_event_count", len(s.traceEvents[traceID])))
}

// startCorrelationScheduler periodically processes trace groups
func (s *NATSSubscriber) startCorrelationScheduler() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		
		ticker := time.NewTicker(s.config.CorrelationWindow / 2) // Check twice per window
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.processTraceGroups()
			case <-s.ctx.Done():
				return
			}
		}
	}()
}

// processTraceGroups correlates events within trace groups
func (s *NATSSubscriber) processTraceGroups() {
	s.traceMu.Lock()
	tracesToProcess := make(map[string][]*domain.UnifiedEvent)
	now := time.Now()

	// Find traces ready for processing
	for traceID, events := range s.traceEvents {
		if len(events) < s.config.MinEventsForCorr {
			continue
		}

		// Check if trace has aged enough for processing
		oldestEvent := events[0]
		for _, event := range events {
			if event.Timestamp.Before(oldestEvent.Timestamp) {
				oldestEvent = event
			}
		}

		if now.Sub(oldestEvent.Timestamp) >= s.config.CorrelationWindow {
			tracesToProcess[traceID] = events
			delete(s.traceEvents, traceID)
		}
	}
	s.traceMu.Unlock()

	// Process ready traces
	for traceID, events := range tracesToProcess {
		s.logger.Info("Processing trace group for correlation",
			zap.String("trace_id", traceID),
			zap.Int("event_count", len(events)))

		// Correlate events in this trace
		allResults := make([]*MultiDimCorrelationResult, 0)
		
		for _, event := range events {
			results, err := s.correlationEngine.Process(s.ctx, event)
			if err != nil {
				s.logger.Warn("Failed to correlate event in trace",
					zap.String("trace_id", traceID),
					zap.String("event_id", event.ID),
					zap.Error(err))
				continue
			}
			allResults = append(allResults, results...)
		}

		if len(allResults) > 0 {
			select {
			case s.resultsCh <- allResults:
				s.logger.Info("Published correlation results for trace",
					zap.String("trace_id", traceID),
					zap.Int("results_count", len(allResults)))
			case <-s.ctx.Done():
				return
			}
		}
	}
}

// extractTraceIDFromSubject extracts trace ID from NATS subject
// e.g., "traces.abc123def456" -> "abc123def456"
func (s *NATSSubscriber) extractTraceIDFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 2 && parts[0] == "traces" {
		return parts[1]
	}
	// Handle test subjects like "test.stream.123.traces.abc123"
	for i, part := range parts {
		if part == "traces" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}