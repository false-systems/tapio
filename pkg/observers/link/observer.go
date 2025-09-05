package link

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Observer tracks network link failures across OSI layers (negative observer)
type Observer struct {
	*base.BaseObserver        // Embed for stats/health
	*base.EventChannelManager // Embed for events
	*base.LifecycleManager    // Embed for lifecycle

	// Link-specific fields
	config     *Config
	logger     *zap.Logger
	name       string
	correlator *LinkCorrelator

	// eBPF state (platform-specific)
	ebpfState interface{}

	// State tracking
	mu          sync.RWMutex
	pendingSYNs map[uint64]*SYNAttempt // Waiting for SYN-ACK
	pendingARPs map[uint32]*ARPRequest // Waiting for ARP reply
	linkStates  map[string]*LinkState  // Track link health

	// OpenTelemetry instrumentation
	tracer           trace.Tracer
	linkFailures     metric.Int64Counter
	synTimeouts      metric.Int64Counter
	arpFailures      metric.Int64Counter
	policyBlocks     metric.Int64Counter
	retransmissions  metric.Int64Counter
	connectionResets metric.Int64Counter
	failureLatency   metric.Float64Histogram
	eventsProcessed  metric.Int64Counter
	errorsTotal      metric.Int64Counter
}

// Config holds observer configuration
type Config struct {
	BufferSize    int           `json:"buffer_size"`
	FlushInterval time.Duration `json:"flush_interval"`

	// Timeouts for failure detection
	SYNTimeout        time.Duration `json:"syn_timeout"`        // Default: 5s
	ARPTimeout        time.Duration `json:"arp_timeout"`        // Default: 1s
	RetransmitTimeout time.Duration `json:"retransmit_timeout"` // Default: 200ms

	// Correlation settings
	CorrelationWindow time.Duration `json:"correlation_window"` // How far back to look

	// Detection thresholds
	MaxRetransmits      int     `json:"max_retransmits"`       // Before declaring failure
	PacketLossThreshold float64 `json:"packet_loss_threshold"` // Percentage

	// eBPF configuration
	RingBufferSize int  `json:"ring_buffer_size"`
	EnableL2Track  bool `json:"enable_l2_track"` // Track ARP/L2
	EnableL3Track  bool `json:"enable_l3_track"` // Track IP/ICMP
	EnableL4Track  bool `json:"enable_l4_track"` // Track TCP/UDP
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		BufferSize:          10000,
		FlushInterval:       10 * time.Second,
		SYNTimeout:          5 * time.Second,
		ARPTimeout:          1 * time.Second,
		RetransmitTimeout:   200 * time.Millisecond,
		CorrelationWindow:   30 * time.Second,
		MaxRetransmits:      3,
		PacketLossThreshold: 5.0, // 5% loss triggers alert
		RingBufferSize:      8 * 1024 * 1024,
		EnableL2Track:       true,
		EnableL3Track:       true,
		EnableL4Track:       true,
	}
}

// NewObserver creates a new link failure observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize OpenTelemetry
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics
	linkFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_link_failures_total", name),
		metric.WithDescription("Total network link failures detected"),
	)
	if err != nil {
		logger.Warn("Failed to create link failures counter", zap.Error(err))
	}

	synTimeouts, err := meter.Int64Counter(
		fmt.Sprintf("%s_syn_timeouts_total", name),
		metric.WithDescription("Total TCP SYN timeouts"),
	)
	if err != nil {
		logger.Warn("Failed to create SYN timeouts counter", zap.Error(err))
	}

	arpFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_arp_failures_total", name),
		metric.WithDescription("Total ARP resolution failures"),
	)
	if err != nil {
		logger.Warn("Failed to create ARP failures counter", zap.Error(err))
	}

	policyBlocks, err := meter.Int64Counter(
		fmt.Sprintf("%s_policy_blocks_total", name),
		metric.WithDescription("Total connections blocked by network policies"),
	)
	if err != nil {
		logger.Warn("Failed to create policy blocks counter", zap.Error(err))
	}

	retransmissions, err := meter.Int64Counter(
		fmt.Sprintf("%s_retransmissions_total", name),
		metric.WithDescription("Total packet retransmissions"),
	)
	if err != nil {
		logger.Warn("Failed to create retransmissions counter", zap.Error(err))
	}

	connectionResets, err := meter.Int64Counter(
		fmt.Sprintf("%s_connection_resets_total", name),
		metric.WithDescription("Total TCP connection resets"),
	)
	if err != nil {
		logger.Warn("Failed to create connection resets counter", zap.Error(err))
	}

	failureLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_failure_detection_latency_ms", name),
		metric.WithDescription("Time to detect link failure in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create failure latency histogram", zap.Error(err))
	}

	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription("Total errors in observer"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	return &Observer{
		BaseObserver:        base.NewBaseObserver(name, 5*time.Minute),
		EventChannelManager: base.NewEventChannelManager(config.BufferSize, name, logger),
		LifecycleManager:    base.NewLifecycleManager(context.Background(), logger),
		config:              config,
		logger:              logger.Named(name),
		name:                name,
		correlator:          NewLinkCorrelator(logger, config),
		pendingSYNs:         make(map[uint64]*SYNAttempt),
		pendingARPs:         make(map[uint32]*ARPRequest),
		linkStates:          make(map[string]*LinkState),
		tracer:              tracer,
		linkFailures:        linkFailures,
		synTimeouts:         synTimeouts,
		arpFailures:         arpFailures,
		policyBlocks:        policyBlocks,
		retransmissions:     retransmissions,
		connectionResets:    connectionResets,
		failureLatency:      failureLatency,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
	}, nil
}

// Name returns the observer name
func (o *Observer) Name() string {
	return o.name
}

// Start starts the observer
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting link observer",
		zap.Bool("l2_track", o.config.EnableL2Track),
		zap.Bool("l3_track", o.config.EnableL3Track),
		zap.Bool("l4_track", o.config.EnableL4Track),
		zap.Duration("syn_timeout", o.config.SYNTimeout),
		zap.Duration("arp_timeout", o.config.ARPTimeout),
	)

	// Start eBPF monitoring (platform-specific)
	if err := o.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start background tasks
	o.LifecycleManager.Start("event-processor", func() {
		o.processEvents()
	})

	o.LifecycleManager.Start("timeout-checker", func() {
		o.checkTimeouts()
	})

	o.LifecycleManager.Start("correlator", func() {
		o.runCorrelator()
	})

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Link observer started successfully")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping link observer")

	// Stop eBPF monitoring
	o.stopEBPF()

	// Stop background tasks
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.BaseObserver.SetHealthy(false)
	o.logger.Info("Link observer stopped")
	return nil
}

// Events returns the events channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// Statistics returns observer statistics
func (o *Observer) Statistics() *domain.CollectorStats {
	return o.BaseObserver.Statistics()
}

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	return o.BaseObserver.Health()
}

// checkTimeouts checks for SYN and ARP timeouts
func (o *Observer) checkTimeouts() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			o.checkSYNTimeouts()
			o.checkARPTimeouts()
		}
	}
}

// checkSYNTimeouts checks for TCP SYN timeouts
func (o *Observer) checkSYNTimeouts() {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := time.Now()
	for key, syn := range o.pendingSYNs {
		if now.Sub(syn.Timestamp) > o.config.SYNTimeout {
			// SYN timeout detected
			o.handleSYNTimeout(syn)
			delete(o.pendingSYNs, key)
		}
	}
}

// checkARPTimeouts checks for ARP timeouts
func (o *Observer) checkARPTimeouts() {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := time.Now()
	for key, arp := range o.pendingARPs {
		if now.Sub(arp.Timestamp) > o.config.ARPTimeout {
			// ARP timeout detected
			o.handleARPTimeout(arp)
			delete(o.pendingARPs, key)
		}
	}
}

// handleSYNTimeout processes a SYN timeout failure
func (o *Observer) handleSYNTimeout(syn *SYNAttempt) {
	if o.synTimeouts != nil {
		o.synTimeouts.Add(o.LifecycleManager.Context(), 1,
			metric.WithAttributes(
				attribute.String("src_ip", syn.SrcIP),
				attribute.String("dst_ip", syn.DstIP),
				attribute.Int("dst_port", int(syn.DstPort)),
			))
	}

	// Create failure event
	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("link-syn-timeout-%d", syn.Timestamp.UnixNano()),
		Timestamp: syn.Timestamp,
		Type:      domain.CollectorEventType("link.syn_timeout"),
		Source:    o.name,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: "syn_timeout",
				Protocol:  "TCP",
				SrcIP:     syn.SrcIP,
				DstIP:     syn.DstIP,
				SrcPort:   syn.SrcPort,
				DstPort:   syn.DstPort,
			},
			Custom: map[string]string{
				"failure_type": "L4_timeout",
				"timeout_ms":   fmt.Sprintf("%d", o.config.SYNTimeout.Milliseconds()),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"layer":    "L4",
				"protocol": "TCP",
				"failure":  "syn_timeout",
			},
		},
	}

	o.SendEvent(event)

	// Send to correlator for root cause analysis
	o.correlator.AnalyzeFailure(&LinkFailure{
		Type:      "syn_timeout",
		Layer:     4,
		Timestamp: syn.Timestamp,
		SrcIP:     syn.SrcIP,
		DstIP:     syn.DstIP,
		SrcPort:   syn.SrcPort,
		DstPort:   syn.DstPort,
	})
}

// handleARPTimeout processes an ARP timeout failure
func (o *Observer) handleARPTimeout(arp *ARPRequest) {
	if o.arpFailures != nil {
		o.arpFailures.Add(o.LifecycleManager.Context(), 1,
			metric.WithAttributes(
				attribute.String("src_ip", arp.SrcIP),
				attribute.String("target_ip", arp.TargetIP),
			))
	}

	// Create failure event
	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("link-arp-timeout-%d", arp.Timestamp.UnixNano()),
		Timestamp: arp.Timestamp,
		Type:      domain.CollectorEventType("link.arp_timeout"),
		Source:    o.name,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType: "arp_timeout",
				Protocol:  "ARP",
				SrcIP:     arp.SrcIP,
				DstIP:     arp.TargetIP,
			},
			Custom: map[string]string{
				"failure_type": "L2_timeout",
				"timeout_ms":   fmt.Sprintf("%d", o.config.ARPTimeout.Milliseconds()),
				"interface":    arp.Interface,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"layer":    "L2",
				"protocol": "ARP",
				"failure":  "arp_timeout",
			},
		},
	}

	o.SendEvent(event)

	// Send to correlator
	o.correlator.AnalyzeFailure(&LinkFailure{
		Type:      "arp_timeout",
		Layer:     2,
		Timestamp: arp.Timestamp,
		SrcIP:     arp.SrcIP,
		DstIP:     arp.TargetIP,
		Interface: arp.Interface,
	})
}

// runCorrelator runs the correlation engine
func (o *Observer) runCorrelator() {
	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case diagnosis := <-o.correlator.Results():
			o.handleDiagnosis(diagnosis)
		}
	}
}

// handleDiagnosis processes a root cause diagnosis
func (o *Observer) handleDiagnosis(diagnosis *LinkDiagnosis) {
	// Create diagnostic event with root cause
	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("link-diagnosis-%d", time.Now().UnixNano()),
		Timestamp: diagnosis.Timestamp,
		Type:      domain.CollectorEventType("link.diagnosis"),
		Source:    o.name,
		Severity:  diagnosis.Severity,
		EventData: domain.EventDataContainer{
			Custom: map[string]string{
				"pattern":    diagnosis.Pattern,
				"confidence": fmt.Sprintf("%.2f", diagnosis.Confidence),
				"summary":    diagnosis.Summary,
				"resolution": diagnosis.Resolution,
				"impact":     diagnosis.Impact,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"diagnosis": diagnosis.Pattern,
				"layer":     fmt.Sprintf("L%d", diagnosis.Layer),
			},
		},
	}

	o.SendEvent(event)
}

// SendEvent sends an event through the observer
func (o *Observer) SendEvent(event *domain.CollectorEvent) {
	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		if o.eventsProcessed != nil {
			o.eventsProcessed.Add(o.LifecycleManager.Context(), 1)
		}
	} else {
		o.BaseObserver.RecordDrop()
		o.logger.Debug("Event dropped due to full channel")
	}
}
