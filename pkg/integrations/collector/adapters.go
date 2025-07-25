package collector

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	"github.com/yairfalse/tapio/pkg/domain"
)

// EBPFCollectorAdapter integrates eBPF collector with gRPC connectivity
// and dual-path processing for semantic correlation
type EBPFCollectorAdapter struct {
	collector     ebpf.Collector
	serverAddress string
	eventChan     chan domain.UnifiedEvent
	processor     *ebpf.DualPathProcessor
	ctx           context.Context
	cancel        context.CancelFunc
}

// Start initializes the eBPF collector with gRPC processor integration
func (a *EBPFCollectorAdapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.eventChan = make(chan domain.UnifiedEvent, 1000)

	if err := a.collector.Start(a.ctx); err != nil {
		return fmt.Errorf("eBPF collector start failed: %w", err)
	}

	processorConfig := &ebpf.ProcessorConfig{
		RawBufferSize:      10000,
		SemanticBufferSize: 5000,
		WorkerCount:        4,
		BatchSize:          100,
		FlushInterval:      time.Second,
		EnableRawPath:      false,
		EnableSemanticPath: true,
		RawRetentionPeriod: 1 * time.Hour,
		RawStorageBackend:  "memory",
		SemanticBatchSize:  50,
		TapioServerAddr:    a.serverAddress,
		MaxMemoryUsage:     512 * 1024 * 1024,
		MetricsInterval:    30 * time.Second,
	}

	a.processor = ebpf.NewDualPathProcessor(processorConfig)
	if err := a.processor.Start(); err != nil {
		return fmt.Errorf("eBPF processor start failed: %w", err)
	}

	go a.processEvents()
	return nil
}

// processEvents bridges events from collector through processor to output channel
func (a *EBPFCollectorAdapter) processEvents() {
	for {
		select {
		case event, ok := <-a.collector.Events():
			if !ok {
				close(a.eventChan)
				return
			}

			// Extract kernel data if available
			var pid, uid, gid uint32
			var comm string
			var details interface{}

			if event.Kernel != nil {
				pid = event.Kernel.PID
				uid = event.Kernel.UID
				gid = event.Kernel.GID
				comm = event.Kernel.Comm
			}

			// Use attributes as details if available
			if event.Attributes != nil {
				details = event.Attributes
			}

			rawEvent := &ebpf.RawEvent{
				Type:      ebpf.EventTypeProcess,
				Timestamp: uint64(event.Timestamp.UnixNano()),
				PID:       pid,
				UID:       uid,
				GID:       gid,
				Comm:      comm,
				Details:   details,
			}

			if err := a.processor.ProcessRawEvent(rawEvent); err != nil {
				log.Printf("eBPF event processing error: %v", err)
			}

			// Event is already UnifiedEvent, so pass it along
			select {
			case a.eventChan <- event:
			case <-a.ctx.Done():
				return
			}

		case <-a.ctx.Done():
			return
		}
	}
}

// Stop gracefully shuts down the eBPF collector and processor
func (a *EBPFCollectorAdapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}

	var errors []error

	if a.processor != nil {
		if err := a.processor.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("processor stop failed: %w", err))
		}
	}

	if err := a.collector.Stop(); err != nil {
		errors = append(errors, fmt.Errorf("collector stop failed: %w", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("stop errors: %v", errors)
	}

	return nil
}

// Events returns the event channel for this adapter
func (a *EBPFCollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return a.eventChan
}

// Health returns the health status of the eBPF collector
func (a *EBPFCollectorAdapter) Health() domain.HealthStatus {
	// Check the actual collector health
	health := a.collector.Health()

	// Convert ebpf.Health to domain.HealthStatus
	switch health.Status {
	case "healthy":
		return domain.HealthHealthy
	case "degraded":
		return domain.HealthDegraded
	case "unhealthy":
		return domain.HealthUnhealthy
	default:
		return domain.HealthUnknown
	}
}
