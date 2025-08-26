//go:build linux

package namespace_collector

// cniEvent represents a network event from eBPF
type cniEvent struct {
	Timestamp uint64
	PID       uint32
	Netns     uint32
	EventType uint32
	Comm      [16]byte
	Data      [64]byte
}

// CollectorStats tracks collector metrics
type CollectorStats struct {
	EventsGenerated uint64
	EventsDropped   uint64
	LastEventTime   uint64
}

// Event types
const (
	EventTypeNetnsEnter  uint32 = 1
	EventTypeNetnsCreate uint32 = 2
	EventTypeNetnsExit   uint32 = 3
)

// PodInfo contains Kubernetes pod metadata extracted from network namespace paths
type PodInfo struct {
	PodUID    string `json:"pod_uid"`
	PodName   string `json:"pod_name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}
