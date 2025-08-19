package parsers

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/domain"
)

// KernelEvent represents the structure of kernel events in Data field
type KernelEvent struct {
	EventType   string `json:"event_type"`
	PID         int32  `json:"pid"`
	ContainerID string `json:"container_id"`
	Syscall     string `json:"syscall"`
	Path        string `json:"path,omitempty"`
	Result      int    `json:"result"`
	Timestamp   int64  `json:"timestamp_ns"`
	ProcessName string `json:"process_name"`
	Namespace   string `json:"namespace,omitempty"`
}

// KernelParser parses kernel events from eBPF
type KernelParser struct{}

// NewKernelParser creates a new kernel event parser
func NewKernelParser() *KernelParser {
	return &KernelParser{}
}

// Source returns the source this parser handles
func (p *KernelParser) Source() string {
	return "kernel"
}

// Parse converts a kernel RawEvent to an ObservationEvent
func (p *KernelParser) Parse(raw *domain.RawEvent) (*domain.ObservationEvent, error) {
	if raw == nil {
		return nil, fmt.Errorf("cannot parse nil event")
	}

	if raw.Source != "kernel" {
		return nil, fmt.Errorf("invalid source: expected kernel, got %s", raw.Source)
	}

	// Parse the kernel event from Data
	var kernelEvent KernelEvent
	if err := json.Unmarshal(raw.Data, &kernelEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal kernel event: %w", err)
	}

	// Create observation event
	obs := &domain.ObservationEvent{
		ID:        uuid.New().String(),
		Timestamp: raw.Timestamp,
		Source:    "kernel",
		Type:      fmt.Sprintf("syscall.%s", kernelEvent.Syscall),
		PID:       &kernelEvent.PID,
	}

	// Add container ID if present
	if kernelEvent.ContainerID != "" {
		obs.ContainerID = &kernelEvent.ContainerID
	}

	// Add namespace if present
	if kernelEvent.Namespace != "" {
		obs.Namespace = &kernelEvent.Namespace
	}

	// Set action and target
	action := kernelEvent.Syscall
	obs.Action = &action

	if kernelEvent.Path != "" {
		obs.Target = &kernelEvent.Path
	}

	// Set result
	result := "success"
	if kernelEvent.Result != 0 {
		result = fmt.Sprintf("error:%d", kernelEvent.Result)
	}
	obs.Result = &result

	// Add additional data
	obs.Data = map[string]string{
		"process_name": kernelEvent.ProcessName,
		"event_type":   kernelEvent.EventType,
	}

	return obs, nil
}
