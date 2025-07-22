package internal

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// eventProcessor implements core.EventProcessor
type eventProcessor struct{}

func newEventProcessor() core.EventProcessor {
	return &eventProcessor{}
}

// ProcessEvent converts a raw eBPF event to a UnifiedEvent
func (p *eventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (domain.UnifiedEvent, error) {
	// Build UnifiedEvent using the builder pattern
	// Note: ID is auto-generated, timestamp is auto-set to now
	builder := domain.NewUnifiedEvent().
		WithSource(string(domain.SourceEBPF))

	// Add semantic context based on event type
	builder = p.addSemanticContext(builder, raw)

	// Add entity context
	if raw.Comm != "" {
		builder = builder.WithEntity("process", raw.Comm, "")
	}

	// Add kernel-specific data
	if p.isKernelEvent(raw) {
		builder = builder.WithKernelData(raw.Type, raw.PID)
		// Set additional kernel fields directly after build
	}

	// Add network data if applicable
	if p.isNetworkEvent(raw) {
		builder = p.addNetworkData(builder, raw)
	}

	// Add application data if applicable
	if p.isApplicationEvent(raw) {
		builder = p.addApplicationData(builder, raw)
	}

	// Build the event first
	event := builder.Build()

	// Add additional kernel data if applicable
	if event.Kernel != nil {
		event.Kernel.Comm = raw.Comm
		event.Kernel.TID = raw.TID
		event.Kernel.UID = raw.UID
		event.Kernel.GID = raw.GID
		event.Kernel.CPUCore = int(raw.CPU)
	}

	// Add impact assessment
	impact := p.assessImpact(raw)
	if event.Impact == nil {
		event.Impact = &domain.ImpactContext{}
	}
	event.Impact.Severity = impact.severity
	event.Impact.BusinessImpact = impact.score

	return *event, nil
}

// addSemanticContext adds semantic context based on event type
func (p *eventProcessor) addSemanticContext(builder *domain.UnifiedEventBuilder, raw core.RawEvent) *domain.UnifiedEventBuilder {
	switch raw.Type {
	case "oom_kill":
		return builder.WithSemantic("oom-kill", "availability", "memory", "critical").
			WithType(domain.EventTypeMemory)

	case "memory_pressure":
		return builder.WithSemantic("memory-pressure", "performance", "memory", "high").
			WithType(domain.EventTypeMemory)

	case "process_start":
		return builder.WithSemantic("process-start", "lifecycle", "process").
			WithType(domain.EventTypeProcess)

	case "process_exit":
		return builder.WithSemantic("process-exit", "lifecycle", "process").
			WithType(domain.EventTypeProcess)

	case "network", "tcp_connect", "tcp_accept":
		return builder.WithSemantic("network-activity", "connectivity", "network").
			WithType(domain.EventTypeNetwork)

	case "syscall":
		return builder.WithSemantic("syscall", "system", "kernel").
			WithType(domain.EventTypeSystem)

	default:
		return builder.WithSemantic(raw.Type, "system", "kernel").
			WithType(domain.EventTypeSystem)
	}
}

// isKernelEvent checks if this is a kernel-level event
func (p *eventProcessor) isKernelEvent(raw core.RawEvent) bool {
	switch raw.Type {
	case "syscall", "kprobe", "tracepoint", "oom_kill":
		return true
	default:
		return false
	}
}

// isNetworkEvent checks if this is a network event
func (p *eventProcessor) isNetworkEvent(raw core.RawEvent) bool {
	switch raw.Type {
	case "network", "tcp_connect", "tcp_accept", "tcp_close":
		return true
	default:
		return false
	}
}

// isApplicationEvent checks if this is an application-level event
func (p *eventProcessor) isApplicationEvent(raw core.RawEvent) bool {
	switch raw.Type {
	case "process_start", "process_exit":
		return true
	default:
		return false
	}
}

// addNetworkData adds network-specific data to the builder
func (p *eventProcessor) addNetworkData(builder *domain.UnifiedEventBuilder, raw core.RawEvent) *domain.UnifiedEventBuilder {
	if srcIP, ok := raw.Decoded["source_ip"].(string); ok {
		if dstIP, ok := raw.Decoded["dest_ip"].(string); ok {
			srcPort := uint16(0)
			dstPort := uint16(0)
			if sp, ok := raw.Decoded["source_port"].(int); ok {
				srcPort = uint16(sp)
			}
			if dp, ok := raw.Decoded["dest_port"].(int); ok {
				dstPort = uint16(dp)
			}
			protocol := "tcp"
			if p, ok := raw.Decoded["protocol"].(string); ok {
				protocol = p
			}
			builder = builder.WithNetworkData(protocol, srcIP, srcPort, dstIP, dstPort)
		}
	}
	return builder
}

// addApplicationData adds application-specific data to the builder
func (p *eventProcessor) addApplicationData(builder *domain.UnifiedEventBuilder, raw core.RawEvent) *domain.UnifiedEventBuilder {
	level := "info"
	message := fmt.Sprintf("Process %s (%d) event: %s", raw.Comm, raw.PID, raw.Type)
	builder = builder.WithApplicationData(level, message)
	return builder
}

// impactInfo holds impact assessment information
type impactInfo struct {
	severity string
	score    float64
}

// assessImpact assesses the impact of an event
func (p *eventProcessor) assessImpact(raw core.RawEvent) impactInfo {
	switch raw.Type {
	case "oom_kill", "kernel_panic":
		return impactInfo{severity: "critical", score: 0.95}
	case "memory_pressure", "cpu_throttle":
		return impactInfo{severity: "high", score: 0.7}
	case "syscall_error":
		if retCode, ok := raw.Decoded["return_code"].(int32); ok && retCode < 0 {
			return impactInfo{severity: "medium", score: 0.5}
		}
		return impactInfo{severity: "low", score: 0.3}
	default:
		return impactInfo{severity: "low", score: 0.2}
	}
}
