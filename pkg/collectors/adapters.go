package collectors

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
	ebpfcore "github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/collectors/k8s"
	k8score "github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"github.com/yairfalse/tapio/pkg/collectors/systemd"
	systemdcore "github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// K8sCollectorAdapter adapts k8s.Collector to CollectorInterface
type K8sCollectorAdapter struct {
	collector k8score.Collector
	name      string
}

// NewK8sCollectorAdapter creates a new adapter for K8s collector
func NewK8sCollectorAdapter(name string, collector k8score.Collector) *K8sCollectorAdapter {
	return &K8sCollectorAdapter{
		collector: collector,
		name:      name,
	}
}

func (a *K8sCollectorAdapter) Start(ctx context.Context) error {
	return a.collector.Start(ctx)
}

func (a *K8sCollectorAdapter) Stop() error {
	return a.collector.Stop()
}

func (a *K8sCollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return a.collector.Events()
}

func (a *K8sCollectorAdapter) Health() CollectorHealth {
	h := a.collector.Health()
	return CollectorHealth{
		Status:          HealthStatus(h.Status),
		Message:         h.Message,
		LastEventTime:   h.LastEventTime,
		EventsProcessed: h.EventsProcessed,
		EventsDropped:   h.EventsDropped,
		ErrorCount:      h.ErrorCount,
		Metrics:         h.Metrics,
	}
}

func (a *K8sCollectorAdapter) Statistics() CollectorStatistics {
	stats := a.collector.Statistics()
	return CollectorStatistics{
		StartTime:       stats.StartTime,
		EventsCollected: stats.EventsCollected,
		EventsDropped:   stats.EventsDropped,
		Custom:          stats.Custom,
	}
}

func (a *K8sCollectorAdapter) Name() string {
	return a.name
}

func (a *K8sCollectorAdapter) Type() string {
	return "kubernetes"
}

// EBPFCollectorAdapter adapts ebpf.Collector to CollectorInterface
type EBPFCollectorAdapter struct {
	collector ebpfcore.Collector
	name      string
}

// NewEBPFCollectorAdapter creates a new adapter for eBPF collector
func NewEBPFCollectorAdapter(name string, collector ebpfcore.Collector) *EBPFCollectorAdapter {
	return &EBPFCollectorAdapter{
		collector: collector,
		name:      name,
	}
}

func (a *EBPFCollectorAdapter) Start(ctx context.Context) error {
	return a.collector.Start(ctx)
}

func (a *EBPFCollectorAdapter) Stop() error {
	return a.collector.Stop()
}

func (a *EBPFCollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return a.collector.Events()
}

func (a *EBPFCollectorAdapter) Health() CollectorHealth {
	h := a.collector.Health()
	return CollectorHealth{
		Status:          HealthStatus(h.Status),
		Message:         h.Message,
		LastEventTime:   h.LastEventTime,
		EventsProcessed: h.EventsProcessed,
		EventsDropped:   h.EventsDropped,
		ErrorCount:      h.ErrorCount,
		Metrics:         h.Metrics,
	}
}

func (a *EBPFCollectorAdapter) Statistics() CollectorStatistics {
	stats := a.collector.Statistics()
	return CollectorStatistics{
		StartTime:       stats.StartTime,
		EventsCollected: stats.EventsCollected,
		EventsDropped:   stats.EventsDropped,
		Custom:          stats.Custom,
	}
}

func (a *EBPFCollectorAdapter) Name() string {
	return a.name
}

func (a *EBPFCollectorAdapter) Type() string {
	return "ebpf"
}

// SystemdCollectorAdapter adapts systemd.Collector to CollectorInterface
type SystemdCollectorAdapter struct {
	collector systemdcore.Collector
	name      string
}

// NewSystemdCollectorAdapter creates a new adapter for systemd collector
func NewSystemdCollectorAdapter(name string, collector systemdcore.Collector) *SystemdCollectorAdapter {
	return &SystemdCollectorAdapter{
		collector: collector,
		name:      name,
	}
}

func (a *SystemdCollectorAdapter) Start(ctx context.Context) error {
	return a.collector.Start(ctx)
}

func (a *SystemdCollectorAdapter) Stop() error {
	return a.collector.Stop()
}

func (a *SystemdCollectorAdapter) Events() <-chan domain.UnifiedEvent {
	return a.collector.Events()
}

func (a *SystemdCollectorAdapter) Health() CollectorHealth {
	h := a.collector.Health()
	return CollectorHealth{
		Status:          HealthStatus(h.Status),
		Message:         h.Message,
		LastEventTime:   h.LastEventTime,
		EventsProcessed: h.EventsProcessed,
		EventsDropped:   h.EventsDropped,
		ErrorCount:      h.ErrorCount,
		Metrics:         h.Metrics,
	}
}

func (a *SystemdCollectorAdapter) Statistics() CollectorStatistics {
	stats := a.collector.Statistics()
	return CollectorStatistics{
		StartTime:       stats.StartTime,
		EventsCollected: stats.EventsCollected,
		EventsDropped:   stats.EventsDropped,
		Custom:          stats.Custom,
	}
}

func (a *SystemdCollectorAdapter) Name() string {
	return a.name
}

func (a *SystemdCollectorAdapter) Type() string {
	return "systemd"
}

// CreateK8sCollector creates a K8s collector with default configuration
func CreateK8sCollector(name string, config k8score.Config) (CollectorInterface, error) {
	collector, err := k8s.NewCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s collector: %w", err)
	}
	return NewK8sCollectorAdapter(name, collector), nil
}

// CreateEBPFCollector creates an eBPF collector with default configuration
func CreateEBPFCollector(name string, config ebpfcore.Config) (CollectorInterface, error) {
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf collector: %w", err)
	}
	return NewEBPFCollectorAdapter(name, collector), nil
}

// CreateSystemdCollector creates a systemd collector with default configuration
func CreateSystemdCollector(name string, config systemdcore.Config) (CollectorInterface, error) {
	collector, err := systemd.NewCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create systemd collector: %w", err)
	}
	return NewSystemdCollectorAdapter(name, collector), nil
}
