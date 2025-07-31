package ebpf

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// KernelEvent represents a kernel event from eBPF
type KernelEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint32
	Size      uint64
	Comm      [16]byte
	CgroupID  uint64   // Add cgroup ID for pod correlation
	PodUID    [36]byte // Add pod UID
	_         [4]byte  // Padding to match C struct alignment
	Data      [64]byte
}

// PodInfo represents pod information for correlation
type PodInfo struct {
	PodUID    [36]byte
	Namespace [64]byte
	PodName   [128]byte
	CreatedAt uint64
}

// Collector implements minimal kernel monitoring via eBPF
type Collector struct {
	name        string
	objs        *kernelmonitorObjects
	links       []link.Link
	reader      *ringbuf.Reader
	events      chan collectors.RawEvent
	ctx         context.Context
	cancel      context.CancelFunc
	healthy     bool
	podTraceMap map[string]string // Map pod UID to trace ID
}

// NewCollector creates a new minimal eBPF collector
func NewCollector(name string) (*Collector, error) {
	return &Collector{
		name:        name,
		events:      make(chan collectors.RawEvent, 1000),
		healthy:     true,
		podTraceMap: make(map[string]string),
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load eBPF program
	spec, err := loadKernelmonitor()
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	c.objs = &kernelmonitorObjects{}
	if err := spec.LoadAndAssign(c.objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Populate container PIDs
	if err := c.populateContainerPIDs(); err != nil {
		return fmt.Errorf("failed to populate container PIDs: %w", err)
	}

	// Attach tracepoints for memory tracking
	mallocLink, err := link.Tracepoint("kmem", "kmalloc", c.objs.TraceMalloc, nil)
	if err != nil {
		return fmt.Errorf("failed to attach kmalloc tracepoint: %w", err)
	}
	c.links = append(c.links, mallocLink)

	freeLink, err := link.Tracepoint("kmem", "kfree", c.objs.TraceFree, nil)
	if err != nil {
		return fmt.Errorf("failed to attach kfree tracepoint: %w", err)
	}
	c.links = append(c.links, freeLink)

	// Attach process execution tracking
	execLink, err := link.Tracepoint("sched", "sched_process_exec", c.objs.TraceExec, nil)
	if err != nil {
		return fmt.Errorf("failed to attach exec tracepoint: %w", err)
	}
	c.links = append(c.links, execLink)

	// Open ring buffer
	c.reader, err = ringbuf.NewReader(c.objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}

	// Start event processing
	go c.processEvents()

	c.healthy = true
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Close links
	for _, l := range c.links {
		l.Close()
	}

	// Close ring buffer
	if c.reader != nil {
		c.reader.Close()
	}

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
	}

	close(c.events)
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// populateContainerPIDs finds and adds container PIDs to the map
func (c *Collector) populateContainerPIDs() error {
	// Find container PIDs by checking /proc for cgroup namespaces
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}

	var containerPIDs []uint32
	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}

		// Check if this is a container process by examining cgroup
		cgroupPath := fmt.Sprintf("/proc/%s/cgroup", proc.Name())
		cgroupData, err := os.ReadFile(cgroupPath)
		if err != nil {
			continue
		}

		cgroupStr := string(cgroupData)
		if strings.Contains(cgroupStr, "docker") ||
			strings.Contains(cgroupStr, "containerd") ||
			strings.Contains(cgroupStr, "kubepods") {

			if pid, err := fmt.Sscanf(proc.Name(), "%d", new(uint32)); err == nil && pid == 1 {
				var actualPID uint32
				fmt.Sscanf(proc.Name(), "%d", &actualPID)
				containerPIDs = append(containerPIDs, actualPID)
			}
		}
	}

	// Add PIDs to eBPF map
	var value uint8 = 1
	for _, pid := range containerPIDs {
		if err := c.objs.ContainerPids.Put(&pid, &value); err != nil {
			// Log but don't fail - just skip this PID
			continue
		}
	}

	return nil
}

// processEvents processes events from the ring buffer
func (c *Collector) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		record, err := c.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(KernelEvent{})) {
			continue
		}

		var event KernelEvent
		// Simple binary unmarshaling from raw bytes
		if len(record.RawSample) != int(unsafe.Sizeof(event)) {
			continue
		}
		event = *(*KernelEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Convert to RawEvent - NO BUSINESS LOGIC, just add pod correlation
		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample, // Raw eBPF event data
			Metadata: map[string]string{
				"collector": "ebpf",
				"pid":       fmt.Sprintf("%d", event.PID),
				"tid":       fmt.Sprintf("%d", event.TID),
				"comm":      c.nullTerminatedString(event.Comm[:]),
				"size":      fmt.Sprintf("%d", event.Size),
				"cgroup_id": fmt.Sprintf("%d", event.CgroupID),
				"pod_uid":   c.nullTerminatedString(event.PodUID[:]),
			},
			// Generate new trace for kernel events, or reuse pod trace if available
			TraceID: c.getOrGenerateTraceID(event),
			SpanID:  collectors.GenerateSpanID(),
		}

		select {
		case c.events <- rawEvent:
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full
		}
	}
}

// eventTypeToString converts event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 1:
		return "memory_alloc"
	case 2:
		return "memory_free"
	case 3:
		return "process_exec"
	case 4:
		return "pod_syscall"
	case 5:
		return "network_conn"
	default:
		return "unknown"
	}
}

// nullTerminatedString converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// UpdatePodInfo updates pod information in the eBPF map for correlation
func (c *Collector) UpdatePodInfo(cgroupID uint64, podUID, namespace, podName string) error {
	if c.objs == nil || c.objs.PodInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	podInfo := PodInfo{
		CreatedAt: uint64(time.Now().UnixNano()),
	}

	// Copy strings with proper bounds checking
	copy(podInfo.PodUID[:], podUID)
	copy(podInfo.Namespace[:], namespace)
	copy(podInfo.PodName[:], podName)

	// Update the eBPF map
	return c.objs.PodInfoMap.Put(cgroupID, podInfo)
}

// RemovePodInfo removes pod information from the eBPF map
func (c *Collector) RemovePodInfo(cgroupID uint64) error {
	if c.objs == nil || c.objs.PodInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	return c.objs.PodInfoMap.Delete(cgroupID)
}

// GetPodInfo retrieves pod information for a given cgroup ID
func (c *Collector) GetPodInfo(cgroupID uint64) (*PodInfo, error) {
	if c.objs == nil || c.objs.PodInfoMap == nil {
		return nil, fmt.Errorf("eBPF maps not initialized")
	}

	var podInfo PodInfo
	err := c.objs.PodInfoMap.Lookup(cgroupID, &podInfo)
	if err != nil {
		return nil, err
	}

	return &podInfo, nil
}

// getOrGenerateTraceID returns an existing trace ID for a pod or generates a new one
func (c *Collector) getOrGenerateTraceID(event KernelEvent) string {
	// If we have a pod UID, use it to maintain consistent trace ID per pod
	podUID := c.nullTerminatedString(event.PodUID[:])
	if podUID != "" && podUID != "unknown" {
		// Check if we already have a trace ID for this pod
		if traceID, exists := c.podTraceMap[podUID]; exists {
			return traceID
		}
		// Generate new trace ID for this pod
		traceID := collectors.GenerateTraceID()
		c.podTraceMap[podUID] = traceID
		return traceID
	}
	
	// For non-pod events, generate a new trace ID each time
	return collectors.GenerateTraceID()
}
