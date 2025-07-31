package k8s

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// K8sEventType represents different K8s-related syscall events
type K8sEventType uint32

const (
	EventK8sContainerCreate K8sEventType = 10
	EventK8sContainerDelete K8sEventType = 11
	EventK8sNetnsCreate     K8sEventType = 12
	EventK8sNetnsDelete     K8sEventType = 13
	EventK8sCgroupCreate    K8sEventType = 14
	EventK8sCgroupDelete    K8sEventType = 15
	EventK8sExecInPod       K8sEventType = 16
	EventK8sVolumeMount     K8sEventType = 17
	EventK8sVolumeUmount    K8sEventType = 18
	EventK8sImagePull       K8sEventType = 19
	EventK8sNetworkSetup    K8sEventType = 20
	EventK8sPodSandbox      K8sEventType = 21
	EventK8sDnsQuery        K8sEventType = 22
	EventK8sServiceConnect  K8sEventType = 23
)

// K8sSyscallEvent represents a processed K8s syscall event
type K8sSyscallEvent struct {
	Timestamp   time.Time
	PID         uint32
	TID         uint32
	CPU         uint32
	Command     string
	ContainerID string
	PodUID      string
	Namespace   string
	EventType   K8sEventType
	Details     interface{} // Type depends on EventType
}

// ConvertK8sSyscallEvent converts K8s syscall event to collector RawEvent
func ConvertK8sSyscallEvent(event K8sSyscallEvent) collectors.RawEvent {
	metadata := map[string]string{
		"event_type":   fmt.Sprintf("%d", event.EventType),
		"pid":          fmt.Sprintf("%d", event.PID),
		"tid":          fmt.Sprintf("%d", event.TID),
		"cpu":          fmt.Sprintf("%d", event.CPU),
		"command":      event.Command,
		"container_id": event.ContainerID,
		"pod_uid":      event.PodUID,
		"namespace":    event.Namespace,
	}

	// Add event-specific metadata
	switch details := event.Details.(type) {
	case struct {
		Source string
		Target string
		FSType string
		Flags  uint64
	}:
		metadata["mount_source"] = details.Source
		metadata["mount_target"] = details.Target
		metadata["mount_fstype"] = details.FSType
		metadata["mount_flags"] = fmt.Sprintf("%d", details.Flags)

	case struct {
		SourceIP   string
		DestIP     string
		SourcePort uint16
		DestPort   uint16
		Family     uint16
	}:
		metadata["src_ip"] = details.SourceIP
		metadata["dst_ip"] = details.DestIP
		metadata["src_port"] = fmt.Sprintf("%d", details.SourcePort)
		metadata["dst_port"] = fmt.Sprintf("%d", details.DestPort)

	case struct {
		Filename string
		Argv0    string
	}:
		metadata["exec_filename"] = details.Filename
		metadata["exec_argv0"] = details.Argv0

	case struct {
		Filename string
		Flags    uint64
		Mode     uint64
	}:
		metadata["file_path"] = details.Filename
		metadata["file_flags"] = fmt.Sprintf("%d", details.Flags)
		metadata["file_mode"] = fmt.Sprintf("%d", details.Mode)
	}

	// Create raw event
	rawData := make([]byte, 8)
	binary.LittleEndian.PutUint64(rawData, uint64(event.EventType))

	return collectors.RawEvent{
		Timestamp: event.Timestamp,
		Type:      "k8s_syscall",
		Data:      rawData,
		Metadata:  metadata,
	}
}
