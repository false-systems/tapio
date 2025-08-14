package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/config"
	"go.uber.org/zap"
	"strings"
)

// Ensure NATSPublisher implements NATSPublisherInterface
var _ NATSPublisherInterface = (*NATSPublisher)(nil)

// NATSPublisher publishes events to NATS
type NATSPublisher struct {
	logger        *zap.Logger
	nc            *nats.Conn
	js            nats.JetStreamContext
	config        *config.NATSConfig
	mu            sync.RWMutex
	isConnected   bool
	shutdownOnce  sync.Once
	ctx           context.Context
	cancel        context.CancelFunc
	reconnectChan chan bool
}

// NewNATSPublisher creates a new NATS publisher
func NewNATSPublisher(logger *zap.Logger, natsConfig *config.NATSConfig) (*NATSPublisher, error) {
	if natsConfig == nil || natsConfig.URL == "" {
		// Return nil publisher for testing
		return nil, nil
	}

	// Create context for publisher lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	// Create publisher instance first
	pub := &NATSPublisher{
		logger:        logger,
		config:        natsConfig,
		ctx:           ctx,
		cancel:        cancel,
		reconnectChan: make(chan bool, 1),
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
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	pub.nc = nc
	pub.isConnected = true

	// Get JetStream context with options
	js, err := nc.JetStream(
		nats.PublishAsyncMaxPending(256),
	)
	if err != nil {
		pub.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	pub.js = js

	// Ensure OBSERVATIONS stream exists
	streamName := "OBSERVATIONS"
	observationSubjects := []string{
		"observations.kernel",
		"observations.kubeapi",
		"observations.dns",
		"observations.etcd",
		"observations.cni",
		"observations.systemd",
	}

	streamInfo, err := js.StreamInfo(streamName)
	if err != nil {
		// Create OBSERVATIONS stream with production configuration
		streamConfig, err := js.AddStream(&nats.StreamConfig{
			Name:        streamName,
			Subjects:    observationSubjects,
			Storage:     nats.FileStorage,
			Retention:   nats.LimitsPolicy,
			MaxAge:      natsConfig.MaxAge,
			MaxBytes:    natsConfig.MaxBytes,
			Duplicates:  natsConfig.DuplicateWindow,
			Replicas:    natsConfig.Replicas,
			Compression: nats.S2Compression, // Enable compression for efficiency
			Discard:     nats.DiscardOld,    // Discard old messages when limits reached
		})
		if err != nil {
			pub.Close()
			return nil, fmt.Errorf("failed to create OBSERVATIONS stream: %w", err)
		}
		logger.Info("Created OBSERVATIONS JetStream stream",
			zap.String("name", streamConfig.Config.Name),
			zap.Strings("subjects", streamConfig.Config.Subjects))
	} else {
		logger.Info("OBSERVATIONS JetStream stream already exists",
			zap.String("name", streamInfo.Config.Name),
			zap.Strings("subjects", streamInfo.Config.Subjects))
	}

	// Start health monitoring goroutine
	go pub.monitorConnection()

	return pub, nil
}

// Publish publishes a raw event to NATS with connection resilience
func (p *NATSPublisher) Publish(event collectors.RawEvent) error {
	// Check for nil safety first
	if p == nil {
		return fmt.Errorf("publisher is nil")
	}

	// Check if publisher context is cancelled
	select {
	case <-p.ctx.Done():
		return fmt.Errorf("publisher shutting down")
	default:
	}

	// Check if publisher is healthy with retry logic
	if !p.IsHealthy() {
		// Wait for potential reconnection
		select {
		case <-time.After(100 * time.Millisecond):
			// Retry health check after brief wait
			if !p.IsHealthy() {
				return fmt.Errorf("publisher not connected to NATS")
			}
		case <-p.ctx.Done():
			return fmt.Errorf("publisher shutting down")
		}
	}

	// Double check JetStream context
	if p.js == nil {
		return fmt.Errorf("JetStream context not initialized")
	}

	// Generate subject based on event source for observations stream
	subject := p.generateObservationSubject(&event)

	// Marshal raw event directly for NATS publishing
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal raw event: %w", err)
	}

	// Publish to JetStream with timeout and retry logic
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	// Use synchronous publish with context for better error handling
	pubAck, err := p.js.Publish(subject, data, nats.Context(ctx))
	if err != nil {
		// Log error and check if it's a connection issue
		p.logger.Warn("Failed to publish raw event, connection may be unstable",
			zap.String("subject", subject),
			zap.String("event_type", event.Type),
			zap.String("trace_id", event.TraceID),
			zap.Error(err),
		)

		// Don't retry on context cancellation or shutdown
		select {
		case <-p.ctx.Done():
			return fmt.Errorf("publisher shutting down during publish")
		default:
		}

		return fmt.Errorf("failed to publish raw event to subject %s: %w", subject, err)
	}

	p.logger.Debug("Published observation event",
		zap.String("subject", subject),
		zap.String("event_type", event.Type),
		zap.String("trace_id", event.TraceID),
		zap.String("stream", pubAck.Stream),
		zap.Uint64("sequence", pubAck.Sequence),
	)

	return nil
}

// generateObservationSubject creates NATS subject for observation event
// Subject format: observations.{source}
// Examples: observations.kernel, observations.kubeapi, observations.dns
func (p *NATSPublisher) generateObservationSubject(event *collectors.RawEvent) string {
	// Get collector name from metadata, fallback to event type in lowercase
	source := strings.ToLower(event.Type)
	if collectorName, ok := event.Metadata["collector_name"]; ok && collectorName != "" {
		source = strings.ToLower(collectorName)
	}

	// Subject format: observations.{source}
	return fmt.Sprintf("observations.%s", source)
}

// Close closes the NATS connection gracefully
func (p *NATSPublisher) Close() {
	if p == nil {
		return
	}

	p.shutdownOnce.Do(func() {
		// Cancel context to stop monitoring
		if p.cancel != nil {
			p.cancel()
		}

		// Since we're using synchronous publishes now, no need to wait

		// Close NATS connection
		if p.nc != nil {
			p.nc.Drain()
		}

		p.mu.Lock()
		p.isConnected = false
		p.mu.Unlock()
	})
}

// IsHealthy returns true if the publisher is connected and healthy
func (p *NATSPublisher) IsHealthy() bool {
	if p == nil || p.nc == nil {
		return false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.isConnected && p.nc.IsConnected()
}

// Connection event handlers
func (p *NATSPublisher) onDisconnect(nc *nats.Conn, err error) {
	p.mu.Lock()
	p.isConnected = false
	p.mu.Unlock()

	if err != nil {
		p.logger.Warn("NATS disconnected", zap.Error(err))
	} else {
		p.logger.Warn("NATS disconnected")
	}
}

func (p *NATSPublisher) onReconnect(nc *nats.Conn) {
	p.mu.Lock()
	p.isConnected = true
	p.mu.Unlock()

	p.logger.Info("NATS reconnected", zap.String("url", nc.ConnectedUrl()))

	// Signal reconnection for any waiting operations
	select {
	case p.reconnectChan <- true:
	default:
	}
}

func (p *NATSPublisher) onClosed(nc *nats.Conn) {
	p.mu.Lock()
	p.isConnected = false
	p.mu.Unlock()

	p.logger.Warn("NATS connection closed")
}

func (p *NATSPublisher) onError(nc *nats.Conn, sub *nats.Subscription, err error) {
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

// monitorConnection monitors the health of the NATS connection
func (p *NATSPublisher) monitorConnection() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if p.nc != nil && !p.nc.IsConnected() {
				p.mu.Lock()
				p.isConnected = false
				p.mu.Unlock()
				p.logger.Warn("NATS connection unhealthy")
			}
		}
	}
}
