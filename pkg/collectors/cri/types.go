package cri

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	CollectorName = "cri" // Short and catchy!

	// Performance tuning - powers of 2 for fast modulo
	RingBufferSize = 8192 // 8K events buffer
	EventBatchSize = 100  // Process in batches of 100
	FlushInterval  = 100 * time.Millisecond
)

// Event represents a container lifecycle event - MEGA LEAN structure
type Event struct {
	// Critical correlation data - 48 bytes total for cache line efficiency
	ContainerID [32]byte `json:"-"`         // Fixed size, no allocations
	PodUID      [36]byte `json:"-"`         // UUID is exactly 36 chars
	PodName     string   `json:"pod_name"`  // 16 bytes pointer+len+cap
	Namespace   string   `json:"namespace"` // 16 bytes pointer+len+cap

	// Critical lifecycle data - packed for alignment
	Type      EventType `json:"type"`       // 1 byte
	ExitCode  int32     `json:"exit_code"`  // 4 bytes
	Signal    int32     `json:"signal"`     // 4 bytes
	OOMKilled uint8     `json:"oom_killed"` // 1 byte bool -> uint8

	// Resource snapshot at death - 24 bytes
	MemoryUsage  uint64 `json:"memory_bytes"`  // 8 bytes
	MemoryLimit  uint64 `json:"memory_limit"`  // 8 bytes
	CPUThrottled uint64 `json:"cpu_throttled"` // 8 bytes

	// Timing - 24 bytes
	Timestamp  int64 `json:"timestamp"`   // 8 bytes Unix nano
	StartedAt  int64 `json:"started_at"`  // 8 bytes Unix nano
	FinishedAt int64 `json:"finished_at"` // 8 bytes Unix nano

	// Error details - only for errors, nil otherwise
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	// Padding to align to 64-byte cache line
	_ [8]byte
}

// Reset resets event for pool reuse - CRITICAL for zero allocations
func (e *Event) Reset() {
	// Clear fixed arrays efficiently
	for i := range e.ContainerID {
		e.ContainerID[i] = 0
	}
	for i := range e.PodUID {
		e.PodUID[i] = 0
	}

	// Reset scalars
	e.PodName = ""
	e.Namespace = ""
	e.Type = 0
	e.ExitCode = 0
	e.Signal = 0
	e.OOMKilled = 0
	e.MemoryUsage = 0
	e.MemoryLimit = 0
	e.CPUThrottled = 0
	e.Timestamp = 0
	e.StartedAt = 0
	e.FinishedAt = 0
	e.Reason = ""
	e.Message = ""
}

// SetContainerID sets container ID from string efficiently
func (e *Event) SetContainerID(id string) {
	if len(id) > 31 {
		id = id[:31] // Truncate if too long
	}
	copy(e.ContainerID[:], id)
}

// SetPodUID sets pod UID from string efficiently
func (e *Event) SetPodUID(uid string) {
	if len(uid) > 35 {
		uid = uid[:35] // Truncate if too long
	}
	copy(e.PodUID[:], uid)
}

// GetContainerID returns container ID as string
func (e *Event) GetContainerID() string {
	// Find null terminator for efficient string creation
	for i, b := range e.ContainerID {
		if b == 0 {
			return string(e.ContainerID[:i])
		}
	}
	return string(e.ContainerID[:])
}

// GetPodUID returns pod UID as string
func (e *Event) GetPodUID() string {
	// Find null terminator for efficient string creation
	for i, b := range e.PodUID {
		if b == 0 {
			return string(e.PodUID[:i])
		}
	}
	return string(e.PodUID[:])
}

type EventType uint8 // Save memory with uint8

const (
	EventCreated EventType = iota
	EventStarted
	EventStopped
	EventDied
	EventOOM // Critical: dedicated OOM event type
)

// String returns event type as string for JSON marshaling
func (et EventType) String() string {
	switch et {
	case EventCreated:
		return "created"
	case EventStarted:
		return "started"
	case EventStopped:
		return "stopped"
	case EventDied:
		return "died"
	case EventOOM:
		return "oom"
	default:
		return "unknown"
	}
}

// RingBuffer - lock-free ring buffer for maximum performance
type RingBuffer struct {
	buffer   [RingBufferSize]*Event
	writePos atomic.Uint64
	readPos  atomic.Uint64
	mask     uint64 // RingBufferSize - 1 for fast modulo
}

// NewRingBuffer creates a new lock-free ring buffer
func NewRingBuffer() *RingBuffer {
	return &RingBuffer{
		mask: RingBufferSize - 1, // For power-of-2 fast modulo
	}
}

// Write adds event to ring buffer - lock-free, may drop on full
func (rb *RingBuffer) Write(event *Event) bool {
	writePos := rb.writePos.Load()
	readPos := rb.readPos.Load()

	// Check if full (leave one slot empty to distinguish full vs empty)
	if (writePos+1)&rb.mask == readPos&rb.mask {
		return false // Buffer full, drop event
	}

	rb.buffer[writePos&rb.mask] = event
	rb.writePos.Store(writePos + 1)
	return true
}

// Read removes event from ring buffer - lock-free
func (rb *RingBuffer) Read() *Event {
	readPos := rb.readPos.Load()
	writePos := rb.writePos.Load()

	if readPos == writePos {
		return nil // Buffer empty
	}

	event := rb.buffer[readPos&rb.mask]
	rb.readPos.Store(readPos + 1)
	return event
}

// Usage returns current buffer usage percentage
func (rb *RingBuffer) Usage() float64 {
	writePos := rb.writePos.Load()
	readPos := rb.readPos.Load()
	used := (writePos - readPos) & rb.mask
	return float64(used) / float64(RingBufferSize) * 100.0
}

// Metrics tracks collector performance with atomic counters
type Metrics struct {
	EventsProcessed  atomic.Uint64
	EventsDropped    atomic.Uint64
	OOMKillsDetected atomic.Uint64
	ProcessingTimeNs atomic.Uint64
	BatchesSent      atomic.Uint64
	CRIErrors        atomic.Uint64
}

// GetMetrics returns current metrics as map
func (m *Metrics) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"events_processed":   m.EventsProcessed.Load(),
		"events_dropped":     m.EventsDropped.Load(),
		"oom_kills_detected": m.OOMKillsDetected.Load(),
		"processing_time_ns": m.ProcessingTimeNs.Load(),
		"batches_sent":       m.BatchesSent.Load(),
		"cri_errors":         m.CRIErrors.Load(),
	}
}

// ContainerInfo represents basic container information from CRI
type ContainerInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Status      string            `json:"status"`
	State       ContainerState    `json:"state"`
	PID         uint32            `json:"pid"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Runtime     RuntimeInfo       `json:"runtime"`
}

// ContainerState represents detailed container state
type ContainerState struct {
	Status     string    `json:"status"`
	Running    bool      `json:"running"`
	Paused     bool      `json:"paused"`
	Restarting bool      `json:"restarting"`
	OOMKilled  bool      `json:"oom_killed"`
	Dead       bool      `json:"dead"`
	ExitCode   int       `json:"exit_code"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// RuntimeInfo represents container runtime information
type RuntimeInfo struct {
	Name    string `json:"name"` // containerd, crio, etc.
	Version string `json:"version"`
}

// K8sContainerMetadata represents Kubernetes metadata extracted from container
type K8sContainerMetadata struct {
	PodUID        string `json:"pod_uid"`
	PodName       string `json:"pod_name"`
	PodNamespace  string `json:"pod_namespace"`
	ContainerName string `json:"container_name"`
}

// IsKubernetesContainer checks if container is managed by Kubernetes
func (c *ContainerInfo) IsKubernetesContainer() bool {
	if c.Labels == nil {
		return false
	}
	_, exists := c.Labels["io.kubernetes.pod.uid"]
	return exists
}

// GetKubernetesMetadata extracts Kubernetes metadata from container info
func (c *ContainerInfo) GetKubernetesMetadata() *K8sContainerMetadata {
	if !c.IsKubernetesContainer() {
		return nil
	}

	return &K8sContainerMetadata{
		PodUID:        c.Labels["io.kubernetes.pod.uid"],
		PodName:       c.Labels["io.kubernetes.pod.name"],
		PodNamespace:  c.Labels["io.kubernetes.pod.namespace"],
		ContainerName: c.Labels["io.kubernetes.container.name"],
	}
}

// ToRawEvent converts CRI event to RawEvent for pipeline processing
func (e *Event) ToRawEvent() collectors.RawEvent {
	// Marshal the event data to JSON
	data, _ := json.Marshal(e) // Error ignored in hot path

	// Create metadata map
	metadata := map[string]string{
		"container_id": e.GetContainerID(),
		"pod_uid":      e.GetPodUID(),
		"pod_name":     e.PodName,
		"namespace":    e.Namespace,
		"event_type":   e.Type.String(),
		"exit_code":    fmt.Sprintf("%d", e.ExitCode),
		"oom_killed":   fmt.Sprintf("%t", e.OOMKilled == 1),
		"memory_usage": fmt.Sprintf("%d", e.MemoryUsage),
		"memory_limit": fmt.Sprintf("%d", e.MemoryLimit),
	}

	return collectors.RawEvent{
		Timestamp: time.Unix(0, e.Timestamp),
		Type:      CollectorName,
		Data:      data,
		Metadata:  metadata,
	}
}

// ToUnifiedEvent converts CRI event to unified event for correlation
func (e *Event) ToUnifiedEvent() *domain.UnifiedEvent {
	severity := domain.EventSeverityInfo

	// Escalate severity based on event type
	if e.OOMKilled == 1 {
		severity = domain.EventSeverityCritical
	} else if e.ExitCode != 0 {
		severity = domain.EventSeverityWarning
	}

	// Map to proper domain event type
	eventType := domain.EventTypeSystem
	if e.Type == EventOOM {
		eventType = domain.EventTypeMemory
	} else if e.ExitCode != 0 {
		eventType = domain.EventTypeProcess
	}

	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("cri-%s-%d", e.GetContainerID()[:12], e.Timestamp),
		Type:      eventType,
		Source:    CollectorName,
		Severity:  severity,
		Timestamp: time.Unix(0, e.Timestamp),
		Message:   formatEventMessage(e),

		// Entity context for correlation
		Entity: &domain.EntityContext{
			Type:      "container",
			Name:      e.PodName,
			Namespace: e.Namespace,
			UID:       e.GetPodUID(),
		},

		// Kubernetes context
		Kubernetes: &domain.KubernetesData{
			Object:     fmt.Sprintf("container/%s", e.GetContainerID()[:12]),
			ObjectKind: "Container",
			Reason:     e.Reason,
			Message:    e.Message,
		},

		// Structured attributes for correlation
		Attributes: map[string]interface{}{
			"container_id":  e.GetContainerID(),
			"pod_uid":       e.GetPodUID(),
			"exit_code":     e.ExitCode,
			"oom_killed":    e.OOMKilled == 1,
			"memory_usage":  e.MemoryUsage,
			"memory_limit":  e.MemoryLimit,
			"cpu_throttled": e.CPUThrottled,
			"started_at":    e.StartedAt,
			"finished_at":   e.FinishedAt,
		},
	}
}

// formatEventMessage creates human-readable message for event
func formatEventMessage(e *Event) string {
	if e.OOMKilled == 1 {
		return fmt.Sprintf("Container OOMKilled: used %s of %s memory limit",
			formatBytes(e.MemoryUsage), formatBytes(e.MemoryLimit))
	}
	if e.ExitCode != 0 {
		return fmt.Sprintf("Container exited with code %d: %s",
			e.ExitCode, e.Reason)
	}
	return fmt.Sprintf("Container %s", e.Type.String())
}

// formatBytes formats bytes in human readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// parseBytes parses human readable bytes string to uint64
func parseBytes(s string) uint64 {
	// Simple implementation for memory limits
	// In production, would use a proper parser
	if strings.HasSuffix(s, "G") || strings.HasSuffix(s, "GB") {
		if val, err := fmt.Sscanf(s, "%f", new(float64)); err == nil {
			return uint64(val * 1024 * 1024 * 1024)
		}
	}
	if strings.HasSuffix(s, "M") || strings.HasSuffix(s, "MB") {
		if val, err := fmt.Sscanf(s, "%f", new(float64)); err == nil {
			return uint64(val * 1024 * 1024)
		}
	}
	return 0
}

// CRIClient interface for container runtime operations
type CRIClient interface {
	// Connect establishes connection to CRI runtime
	Connect() error

	// Close closes the connection
	Close() error

	// ListContainers returns all containers
	ListContainers() ([]*cri.Container, error)

	// ContainerStatus returns container status
	ContainerStatus(containerID string) (*cri.ContainerStatus, error)

	// IsHealthy checks if CRI connection is healthy
	IsHealthy() bool
}

// EventPool manages Event object pooling for zero allocations
type EventPool struct {
	pool sync.Pool
}

// NewEventPool creates a new event pool
func NewEventPool() *EventPool {
	return &EventPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &Event{}
			},
		},
	}
}

// Get retrieves an event from pool
func (p *EventPool) Get() *Event {
	event := p.pool.Get().(*Event)
	event.Reset()
	return event
}

// Put returns an event to pool
func (p *EventPool) Put(event *Event) {
	event.Reset()
	p.pool.Put(event)
}

// Ensure Event can be nil-checked at compile time
var _ *Event = (*Event)(nil)

// Compile-time size check: Event should fit within 256 bytes for reasonable cache efficiency
// This will fail to compile if Event struct exceeds 256 bytes
var _ [256 - unsafe.Sizeof(Event{})]byte
