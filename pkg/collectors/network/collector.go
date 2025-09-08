package network

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Collector follows the base collector pattern from CLAUDE.md
type Collector struct {
	*base.BaseCollector       // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle

	// Network-specific fields
	config   *NetworkCollectorConfig
	logger   *zap.Logger
	l7Parser *L7Parser

	// eBPF state (platform-specific, interface{} to avoid build constraints here)
	ebpfState interface{}
}

// NewCollector creates a new network collector using base components
func NewCollector(name string, config *NetworkCollectorConfig, logger *zap.Logger) (*Collector, error) {
	if config == nil {
		config = &NetworkCollectorConfig{
			BufferSize: 1000,
			EnableIPv4: true,
			EnableTCP:  true,
			EnableUDP:  true,
		}
	}

	// Create base collector components following CLAUDE.md pattern
	baseCollector := base.NewBaseCollector(name, logger)
	eventChannel := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(context.Background(), logger)

	return &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventChannel,
		LifecycleManager:    lifecycle,
		config:              config,
		logger:              logger,
		l7Parser:            NewL7Parser(logger),
	}, nil
}

// Start starts the collector
func (c *Collector) Start(ctx context.Context) error {
	// Initialize base collector start time
	c.BaseCollector.Start()

	// Start eBPF monitoring (platform-specific)
	if err := c.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start background tasks
	c.LifecycleManager.Start("L7-cleanup", c.cleanupL7Parser)

	c.logger.Info("Network collector started", zap.String("name", c.Name()))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	// Stop eBPF monitoring (platform-specific)
	c.stopEBPF()

	// Use lifecycle manager for graceful shutdown
	if err := c.LifecycleManager.Stop(5 * time.Second); err != nil {
		c.logger.Warn("Graceful shutdown timeout, forcing stop", zap.Error(err))
	}

	c.logger.Info("Network collector stopped")
	return nil
}

// Events returns the events channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// SendEvent sends an event through the collector
func (c *Collector) SendEvent(event *domain.CollectorEvent) {
	if !c.EventChannelManager.SendEvent(event) {
		// Event was dropped (EventChannelManager already logged it)
		c.RecordError(fmt.Errorf("event dropped due to full channel"))
	} else {
		// Event sent successfully
		c.RecordEvent()
	}
}

// cleanupL7Parser periodically cleans up stale L7 connection states
func (c *Collector) cleanupL7Parser() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			if c.l7Parser != nil {
				staleCount := c.l7Parser.CleanupStaleConnections(5 * time.Minute)
				if staleCount > 0 {
					c.logger.Debug("Cleaned up stale L7 connections",
						zap.Int("count", staleCount))
				}
			}
		}
	}
}

// Helper methods for L7 parsing and event type names

func (c *Collector) getEventTypeName(eventType uint32) string {
	switch eventType {
	case 1:
		return "TCP_CONNECT"
	case 2:
		return "TCP_ACCEPT"
	case 3:
		return "TCP_CLOSE"
	case 4:
		return "UDP_SEND"
	case 5:
		return "UDP_RECV"
	default:
		return "UNKNOWN"
	}
}

func (c *Collector) getProtocolName(protocol uint8) string {
	switch protocol {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	default:
		return "unknown"
	}
}

func (c *Collector) getDirectionName(direction uint8) string {
	switch direction {
	case 0:
		return "inbound"
	case 1:
		return "outbound"
	default:
		return "unknown"
	}
}

func (c *Collector) formatIP(ip []byte) string {
	if len(ip) >= 4 {
		// IPv4
		if ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] == 0 {
			return "0.0.0.0"
		}
		return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
	}
	return "unknown"
}

func (c *Collector) extractString(data []byte) string {
	// Find null terminator
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func (c *Collector) getHTTPStatusCategory(statusCode int) string {
	switch {
	case statusCode >= 100 && statusCode < 200:
		return "informational"
	case statusCode >= 200 && statusCode < 300:
		return "success"
	case statusCode >= 300 && statusCode < 400:
		return "redirection"
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	case statusCode >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

func (c *Collector) getDNSResponseCodeName(code int) string {
	if name, exists := DNSResponseCodes[code]; exists {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%d", code)
}
