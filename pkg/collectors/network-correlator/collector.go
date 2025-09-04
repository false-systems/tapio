package networkcorrelator

import (
	"context"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" networkmonitor bpf_src/network_monitor.c -- -I../../bpf_common

// Config for network-correlator
type Config struct {
	Name       string `json:"name"`
	BufferSize int    `json:"buffer_size"`

	// Timeouts for failure detection
	SYNTimeout time.Duration `json:"syn_timeout"` // Default: 5s
	ARPTimeout time.Duration `json:"arp_timeout"` // Default: 1s

	// Correlation settings
	CorrelationWindow time.Duration `json:"correlation_window"` // How far back to look for related events

	// Which interfaces to monitor
	Interfaces []string `json:"interfaces"` // Empty = all interfaces

	// K8s integration
	EnableK8sMetadata bool `json:"enable_k8s_metadata"` // Enrich with pod names
	EnablePolicyCheck bool `json:"enable_policy_check"` // Check NetworkPolicies
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Name:              "network-correlator",
		BufferSize:        10000,
		SYNTimeout:        5 * time.Second,
		ARPTimeout:        1 * time.Second,
		CorrelationWindow: 30 * time.Second,
		EnableK8sMetadata: true,
		EnablePolicyCheck: true,
	}
}

// Collector implements the network failure correlator
type Collector struct {
	*base.BaseCollector       // Statistics, Health, OTEL
	*base.EventChannelManager // Event publishing
	*base.LifecycleManager    // Goroutine management

	config *Config
	logger *zap.Logger

	// eBPF
	collection *ebpf.Collection
	tcLinks    []link.Link     // TC attachments
	perfReader *ringbuf.Reader // Ring buffer reader

	// Correlation engine
	correlator *CorrelationEngine

	// State tracking
	pendingSYNs map[uint64]*SYNAttempt // Waiting for response
	pendingARPs map[uint32]*ARPRequest // Waiting for reply

	// Timeout checking
	timeoutTicker *time.Ticker
}

// NewCollector creates the network-correlator
func NewCollector(name string, config *Config, logger *zap.Logger) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	// Initialize base components
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 30 * time.Second,
		ErrorRateThreshold: 0.05,
		Logger:             logger,
	}

	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)
	eventManager := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycle := base.NewLifecycleManager(context.Background(), logger)

	c := &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycle,
		config:              config,
		logger:              logger,
		pendingSYNs:         make(map[uint64]*SYNAttempt),
		pendingARPs:         make(map[uint32]*ARPRequest),
		correlator:          NewCorrelationEngine(logger),
	}

	return c, nil
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Info("Starting network-correlator",
		zap.String("name", c.config.Name),
		zap.Duration("syn_timeout", c.config.SYNTimeout))

	// Platform-specific eBPF loading
	if err := c.startPlatformSpecific(ctx); err != nil {
		// On non-Linux, just log and continue in limited mode
		c.logger.Warn("eBPF not available, running in limited mode", zap.Error(err))
	}

	// Start lifecycle manager with cleanup function
	c.LifecycleManager.Start("network-correlator", func() {
		c.logger.Info("Network-correlator cleanup")
	})

	// Start timeout checker goroutine
	go func() {
		if err := c.checkTimeouts(ctx); err != nil {
			c.logger.Error("Timeout checker stopped", zap.Error(err))
		}
	}()

	// Start correlation engine goroutine
	go func() {
		if err := c.correlator.Run(ctx); err != nil {
			c.logger.Error("Correlator stopped", zap.Error(err))
		}
	}()

	// Start result publisher goroutine
	go func() {
		if err := c.publishResults(ctx); err != nil {
			c.logger.Error("Publisher stopped", zap.Error(err))
		}
	}()

	c.BaseCollector.SetHealthy(true)
	c.logger.Info("Network-correlator started successfully")

	return nil
}

// Check for timeouts periodically
func (c *Collector) checkTimeouts(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.checkSYNTimeouts()
			c.checkARPTimeouts()
		}
	}
}

// Check for SYN timeouts
func (c *Collector) checkSYNTimeouts() {
	now := time.Now()
	timeout := c.config.SYNTimeout

	for hash, attempt := range c.pendingSYNs {
		if now.Sub(attempt.Timestamp) > timeout {
			// Create timeout event
			event := &NetworkEvent{
				EventType:   EventTCPSYNTimeout,
				Timestamp:   now,
				SrcIP:       attempt.SrcIP,
				DstIP:       attempt.DstIP,
				SrcPort:     attempt.SrcPort,
				DstPort:     attempt.DstPort,
				Duration:    now.Sub(attempt.Timestamp),
				FailureCode: TimeoutNoResponse,
			}

			// Send to correlator
			c.correlator.tcpEvents <- event

			// Clean up
			delete(c.pendingSYNs, hash)

			c.logger.Debug("SYN timeout detected",
				zap.String("src", attempt.SrcIP.String()),
				zap.String("dst", attempt.DstIP.String()),
				zap.Uint16("port", attempt.DstPort))
		}
	}
}

// Check for ARP timeouts
func (c *Collector) checkARPTimeouts() {
	now := time.Now()
	timeout := c.config.ARPTimeout

	for ip, request := range c.pendingARPs {
		if now.Sub(request.Timestamp) > timeout {
			// Create ARP timeout event
			event := &NetworkEvent{
				EventType:   EventARPTimeout,
				Timestamp:   now,
				SrcIP:       request.RequesterIP,
				DstIP:       request.TargetIP,
				Duration:    now.Sub(request.Timestamp),
				FailureCode: ARPNoResponse,
			}

			// Send to correlator
			c.correlator.arpEvents <- event

			// Clean up
			delete(c.pendingARPs, ip)

			c.logger.Debug("ARP timeout detected",
				zap.String("target", request.TargetIP.String()))
		}
	}
}

// Publish correlation results
func (c *Collector) publishResults(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case rootCause := <-c.correlator.results:
			// Convert to domain event
			event := c.correlator.EmitCorrelatedEvent(rootCause)

			// Publish via EventChannelManager
			if !c.EventChannelManager.SendEvent(event) {
				c.BaseCollector.RecordDrop()
				c.logger.Warn("Event channel full, dropped correlated event",
					zap.String("pattern", rootCause.Pattern))
			} else {
				c.logger.Info("Published root cause",
					zap.String("pattern", rootCause.Pattern),
					zap.String("summary", rootCause.Summary),
					zap.Float32("confidence", rootCause.Confidence))
			}
		}
	}
}

// Stop the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping network-correlator")

	// Stop lifecycle manager with 30 second timeout
	c.LifecycleManager.Stop(30 * time.Second)

	// Platform-specific cleanup
	c.stopPlatformSpecific()

	c.BaseCollector.SetHealthy(false)
	c.logger.Info("Network-correlator stopped")

	return nil
}

// Interface implementations for orchestrator
func (c *Collector) Name() string {
	return c.config.Name
}

func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}
