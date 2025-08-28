//go:build linux
// +build linux

package kernel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 kernelMonitor ./bpf_src/kernel_monitor.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfComponents struct {
	objs   *bpf.KernelmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memory limit", zap.Error(err))
	}

	// Load pre-compiled eBPF programs
	objs := &bpf.KernelmonitorObjects{}
	if err := bpf.LoadKernelmonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	state := &ebpfComponents{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach to ConfigMap/Secret access events (entry point)
	configLink, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenat, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching openat enter tracepoint: %w", err)
	}
	state.links = append(state.links, configLink)

	// Attach to ConfigMap/Secret access events (exit point for error capture)
	configExitLink, err := link.Tracepoint("syscalls", "sys_exit_openat", objs.TraceOpenatExit, nil)
	if err != nil {
		c.logger.Warn("Failed to attach openat exit tracepoint, continuing without error tracking", zap.Error(err))
		// Don't fail completely - we can still capture enter events
	} else {
		state.links = append(state.links, configExitLink)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		for _, l := range state.links {
			l.Close()
		}
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	c.ebpfState = state
	c.logger.Info("eBPF programs loaded and attached successfully",
		zap.Int("attached_programs", len(state.links)))
	return nil
}

// stopEBPF stops eBPF monitoring - Linux only
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState

	// Close reader first
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.ebpfState = nil
	c.logger.Info("eBPF programs stopped")
}

// processEvents reads and processes eBPF events
func (c *Collector) processEvents() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState
	if state.reader == nil {
		return
	}

	c.logger.Info("Starting eBPF event processing")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping eBPF event processing")
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "ring_buffer_read"),
					))
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Process the raw event
			c.processRawEvent(record.RawSample)
		}
	}
}

// processRawEvent processes a single raw eBPF event
func (c *Collector) processRawEvent(data []byte) {
	start := time.Now()

	// Parse kernel event based on size
	if len(data) < int(unsafe.Sizeof(KernelEvent{})) {
		if c.droppedEvents != nil {
			c.droppedEvents.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("reason", "invalid_size"),
			))
		}
		return
	}

	// Convert raw bytes to KernelEvent
	event := (*KernelEvent)(unsafe.Pointer(&data[0]))

	// Create structured event data
	eventData := KernelEventData{
		PID:       event.PID,
		TID:       event.TID,
		CgroupID:  event.CgroupID,
		EventType: event.EventType,
		Comm:      bytesToString(event.Comm[:]),
		PodUID:    bytesToString(event.PodUID[:]),
	}

	// Process specific event types
	switch event.EventType {
	case uint32(EventTypeConfigMapAccess):
		eventData.ConfigType = "configmap"
		eventData.MountPath, eventData.ErrorCode = c.extractMountPathAndError(event.Data[:])
		c.enrichConfigMapEvent(&eventData)

	case uint32(EventTypeSecretAccess):
		eventData.ConfigType = "secret"
		eventData.MountPath, eventData.ErrorCode = c.extractMountPathAndError(event.Data[:])
		c.enrichSecretEvent(&eventData)

	case uint32(EventTypeConfigAccessFailed):
		eventData.ConfigType = "failed"
		eventData.MountPath, eventData.ErrorCode = c.extractMountPathAndError(event.Data[:])
		eventData.ErrorDesc = getErrorDescription(eventData.ErrorCode)
		c.enrichFailedAccessEvent(&eventData)

	case uint32(EventTypePodSyscall):
		// This is for correlation only - minimal processing
		eventData.ConfigType = "syscall"
	}

	// Note: We're not marshaling to JSON for the domain event anymore
	// The eventData is already structured properly

	// Create domain event
	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("%s-%d-%d", c.name, event.Timestamp, event.PID),
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Source:    c.name,
		Type:      domain.CollectorEventType(c.getEventType(event.EventType)),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				EventType: c.getEventType(event.EventType),
				PID:       int32(event.PID),
				UID:       0, // Not captured in current implementation
				GID:       0, // Not captured in current implementation
				Command:   eventData.Comm,
				CgroupID:  event.CgroupID,
			},
			Custom: map[string]string{
				"config_type": eventData.ConfigType,
				"config_name": eventData.ConfigName,
				"mount_path":  eventData.MountPath,
				"tid":         fmt.Sprintf("%d", event.TID),
				"error_code":  fmt.Sprintf("%d", eventData.ErrorCode),
				"error_desc":  eventData.ErrorDesc,
			},
		},
		Metadata: domain.EventMetadata{
			PodUID:       eventData.PodUID,
			PodNamespace: eventData.Namespace,
		},
	}

	// Send event to channel
	select {
	case c.events <- domainEvent:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("event_type", domainEvent.Type),
				attribute.String("config_type", eventData.ConfigType),
			))
		}
	default:
		// Channel full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("reason", "channel_full"),
			))
		}
	}

	// Record processing time
	if c.processingTime != nil {
		duration := time.Since(start).Milliseconds()
		c.processingTime.Record(c.ctx, float64(duration), metric.WithAttributes(
			attribute.String("event_type", domainEvent.Type),
		))
	}

	// Update buffer usage
	if c.bufferUsage != nil {
		usage := int64(len(c.events))
		c.bufferUsage.Record(c.ctx, usage, metric.WithAttributes(
			attribute.String("collector", c.name),
		))
	}
}

// extractMountPathAndError extracts mount path and error code from event data
func (c *Collector) extractMountPathAndError(data []byte) (string, int32) {
	if len(data) < 64 {
		return "", 0
	}

	// The first 60 bytes contain the mount_path from config_info struct
	mountPath := bytesToString(data[:60])

	// The last 4 bytes contain the error code (int32)
	var errorCode int32
	if len(data) >= 64 {
		errorCode = int32(binary.LittleEndian.Uint32(data[60:64]))
	}

	return mountPath, errorCode
}

// enrichConfigMapEvent adds ConfigMap-specific enrichment
func (c *Collector) enrichConfigMapEvent(eventData *KernelEventData) {
	// Parse ConfigMap information from mount path
	// Example path: /var/lib/kubelet/pods/{pod-uid}/volumes/kubernetes.io~configmap/{configmap-name}
	if strings.Contains(eventData.MountPath, "kubernetes.io~configmap") {
		parts := strings.Split(eventData.MountPath, "/")
		for i, part := range parts {
			if part == "kubernetes.io~configmap" && i+1 < len(parts) {
				eventData.ConfigName = parts[i+1]
				break
			}
		}
	}

	// Extract namespace from pod UID correlation if available
	// This would typically be done through additional map lookups in production
	if eventData.PodUID != "" {
		// In production, we'd look up the pod info from our maps
		eventData.Namespace = "default" // Placeholder
	}
}

// enrichSecretEvent adds Secret-specific enrichment
func (c *Collector) enrichSecretEvent(eventData *KernelEventData) {
	// Parse Secret information from mount path
	// Example path: /var/lib/kubelet/pods/{pod-uid}/volumes/kubernetes.io~secret/{secret-name}
	if strings.Contains(eventData.MountPath, "kubernetes.io~secret") {
		parts := strings.Split(eventData.MountPath, "/")
		for i, part := range parts {
			if part == "kubernetes.io~secret" && i+1 < len(parts) {
				eventData.ConfigName = parts[i+1]
				break
			}
		}
	}

	// Extract namespace from pod UID correlation if available
	if eventData.PodUID != "" {
		// In production, we'd look up the pod info from our maps
		eventData.Namespace = "default" // Placeholder
	}
}

// enrichFailedAccessEvent adds failed access-specific enrichment
func (c *Collector) enrichFailedAccessEvent(eventData *KernelEventData) {
	// Determine if it was a ConfigMap or Secret access attempt
	if strings.Contains(eventData.MountPath, "kubernetes.io~configmap") {
		eventData.ConfigType = "configmap-failed"
		parts := strings.Split(eventData.MountPath, "/")
		for i, part := range parts {
			if part == "kubernetes.io~configmap" && i+1 < len(parts) {
				eventData.ConfigName = parts[i+1]
				break
			}
		}
	} else if strings.Contains(eventData.MountPath, "kubernetes.io~secret") {
		eventData.ConfigType = "secret-failed"
		parts := strings.Split(eventData.MountPath, "/")
		for i, part := range parts {
			if part == "kubernetes.io~secret" && i+1 < len(parts) {
				eventData.ConfigName = parts[i+1]
				break
			}
		}
	}

	// Extract namespace from pod UID correlation if available
	if eventData.PodUID != "" {
		// In production, we'd look up the pod info from our maps
		eventData.Namespace = "default" // Placeholder
	}

	// Record the failed access metric
	if c.errorsTotal != nil {
		c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "config_access_failed"),
			attribute.String("config_type", eventData.ConfigType),
			attribute.Int("error_code", int(eventData.ErrorCode)),
		))
	}
}

// getEventType converts numeric event type to string
func (c *Collector) getEventType(eventType uint32) string {
	switch eventType {
	case uint32(EventTypeConfigMapAccess):
		return "configmap_access"
	case uint32(EventTypeSecretAccess):
		return "secret_access"
	case uint32(EventTypeConfigAccessFailed):
		return "config_access_failed"
	case uint32(EventTypePodSyscall):
		return "pod_syscall"
	default:
		return fmt.Sprintf("unknown_%d", eventType)
	}
}

// getErrorDescription returns human-readable error description for errno
func getErrorDescription(errorCode int32) string {
	switch errorCode {
	case 0:
		return "Success"
	case 2: // ENOENT
		return "No such file or directory"
	case 13: // EACCES
		return "Permission denied"
	case 5: // EIO
		return "Input/output error"
	case 14: // EFAULT
		return "Bad address"
	case 22: // EINVAL
		return "Invalid argument"
	case 28: // ENOSPC
		return "No space left on device"
	case 30: // EROFS
		return "Read-only file system"
	default:
		return fmt.Sprintf("Error code %d", errorCode)
	}
}

// bytesToString converts byte array to string
func bytesToString(data []byte) string {
	n := bytes.IndexByte(data, 0)
	if n == -1 {
		n = len(data)
	}
	return string(data[:n])
}

// parseKernelEvent safely parses kernel event from buffer
func parseKernelEvent(buffer []byte) (*KernelEvent, error) {
	if len(buffer) < int(unsafe.Sizeof(KernelEvent{})) {
		return nil, fmt.Errorf("buffer too small: %d bytes", len(buffer))
	}

	var event KernelEvent
	reader := bytes.NewReader(buffer)
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	return &event, nil
}

// AddContainerPID adds a PID to the container tracking map
func (c *Collector) AddContainerPID(pid uint32) error {
	if c.ebpfState == nil {
		return fmt.Errorf("eBPF not initialized")
	}

	state := c.ebpfState
	if state.objs == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	flag := uint8(1)
	if err := state.objs.ContainerPids.Put(pid, flag); err != nil {
		return fmt.Errorf("failed to add container PID %d: %w", pid, err)
	}

	return nil
}

// RemoveContainerPID removes a PID from the container tracking map
func (c *Collector) RemoveContainerPID(pid uint32) error {
	if c.ebpfState == nil {
		return fmt.Errorf("eBPF not initialized")
	}

	state := c.ebpfState
	if state.objs == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	if err := state.objs.ContainerPids.Delete(pid); err != nil {
		return fmt.Errorf("failed to remove container PID %d: %w", pid, err)
	}

	return nil
}

// AddPodInfo adds pod information to the correlation map
func (c *Collector) AddPodInfo(cgroupID uint64, podInfo PodInfo) error {
	if c.ebpfState == nil {
		return fmt.Errorf("eBPF not initialized")
	}

	state := c.ebpfState
	if state.objs == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	// Convert Go struct to C-compatible format
	var cPodInfo [248]byte // Size of C struct pod_info

	// Copy pod UID (36 bytes)
	copy(cPodInfo[0:36], podInfo.PodUID)
	// Copy namespace (64 bytes)
	copy(cPodInfo[36:100], podInfo.Namespace)
	// Copy pod name (128 bytes)
	copy(cPodInfo[100:228], podInfo.PodName)
	// Copy created_at (8 bytes)
	createdAt := uint64(podInfo.CreatedAt)
	for i := 0; i < 8; i++ {
		cPodInfo[228+i] = byte(createdAt >> (i * 8))
	}

	if err := state.objs.PodInfoMap.Put(cgroupID, cPodInfo); err != nil {
		return fmt.Errorf("failed to add pod info for cgroup %d: %w", cgroupID, err)
	}

	return nil
}

// AddMountInfo adds ConfigMap/Secret mount information
func (c *Collector) AddMountInfo(pathHash uint64, mountInfo MountInfo) error {
	if c.ebpfState == nil {
		return fmt.Errorf("eBPF not initialized")
	}

	state := c.ebpfState
	if state.objs == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	// Convert Go struct to C-compatible format
	var cMountInfo [200]byte // Size of C struct mount_info

	// Copy name (64 bytes)
	copy(cMountInfo[0:64], mountInfo.Name)
	// Copy namespace (64 bytes)
	copy(cMountInfo[64:128], mountInfo.Namespace)
	// Copy mount path (128 bytes)
	copy(cMountInfo[128:256], mountInfo.MountPath)
	// Set is_secret flag
	if mountInfo.IsSecret {
		cMountInfo[256] = 1
	} else {
		cMountInfo[256] = 0
	}

	if err := state.objs.MountInfoMap.Put(pathHash, cMountInfo); err != nil {
		return fmt.Errorf("failed to add mount info for path hash %d: %w", pathHash, err)
	}

	return nil
}
