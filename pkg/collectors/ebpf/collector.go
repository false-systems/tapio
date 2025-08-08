package ebpf

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/bpf"
	"go.uber.org/zap"
)

// Collector implements minimal kernel monitoring via eBPF
type Collector struct {
	name        string
	logger      *zap.Logger
	objs        *bpf.KernelmonitorObjects
	links       []link.Link
	reader      *ringbuf.Reader
	events      chan collectors.RawEvent
	ctx         context.Context
	cancel      context.CancelFunc
	healthy     bool
	mu          sync.RWMutex
	podTraceMap map[string]string // Map pod UID to trace ID
	stats       CollectorStats
}

// NewCollector creates a new minimal eBPF collector
func NewCollector(name string) (*Collector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	return NewCollectorWithConfig(&Config{
		Name: name,
	}, logger)
}

// NewCollectorWithConfig creates a new eBPF collector with config
func NewCollectorWithConfig(config *Config, logger *zap.Logger) (*Collector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	c := &Collector{
		name:        config.Name,
		logger:      logger,
		events:      make(chan collectors.RawEvent, 10000), // Large buffer for kernel events
		healthy:     true,
		podTraceMap: make(map[string]string),
	}

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load eBPF program
	spec, err := bpf.LoadKernelmonitor()
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	c.objs = &bpf.KernelmonitorObjects{}
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

	// Attach network connection tracking
	tcpLink, err := link.Kprobe("tcp_v4_connect", c.objs.TraceTcpConnect, nil)
	if err != nil {
		// Log but don't fail - network tracking is optional
		c.logger.Warn("failed to attach tcp connect kprobe", zap.Error(err))
	} else {
		c.links = append(c.links, tcpLink)
	}

	// Attach file operation tracking
	openLink, err := link.Tracepoint("syscalls", "sys_enter_openat", c.objs.TraceOpenat, nil)
	if err != nil {
		// Log but don't fail - file tracking is optional
		c.logger.Warn("failed to attach openat tracepoint", zap.Error(err))
	} else {
		c.links = append(c.links, openLink)
	}

	// Open ring buffer
	c.reader, err = ringbuf.NewReader(c.objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}

	// No performance adapter - using direct channels

	// Start event processing
	go c.processEvents()

	c.healthy = true
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// No external publishers to close - collectors should not have integrations

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

	// No performance adapter to stop

	if c.events != nil {
		close(c.events)
	}
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	// Return direct channel
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

		// Parse event with memory safety checks
		event, err := c.parseKernelEventSafely(record.RawSample)
		if err != nil {
			// Log error and increment error count
			c.mu.Lock()
			c.stats.ErrorCount++
			c.mu.Unlock()
			continue
		}

		// Convert to RawEvent - NO BUSINESS LOGIC, just add correlation metadata
		metadata := map[string]string{
			"collector": "ebpf",
			"pid":       fmt.Sprintf("%d", event.PID),
			"tid":       fmt.Sprintf("%d", event.TID),
			"comm":      c.nullTerminatedString(event.Comm[:]),
			"size":      fmt.Sprintf("%d", event.Size),
			"cgroup_id": fmt.Sprintf("%d", event.CgroupID),
			"pod_uid":   c.nullTerminatedString(event.PodUID[:]),
		}

		// Add enhanced K8s metadata from eBPF maps - STANDARD for all collectors
		if event.CgroupID != 0 {
			if podInfo, err := c.GetPodInfo(event.CgroupID); err == nil {
				metadata["k8s_namespace"] = c.nullTerminatedString(podInfo.Namespace[:])
				metadata["k8s_name"] = c.nullTerminatedString(podInfo.PodName[:])
				metadata["k8s_kind"] = "Pod" // eBPF tracks pods
				metadata["k8s_uid"] = c.nullTerminatedString(podInfo.PodUID[:])
				// Labels and owner refs would need to be added via UpdatePodInfo
			}
		}

		// Add container information if available
		if containerInfo, err := c.GetContainerInfo(event.PID); err == nil {
			metadata["container_id"] = c.nullTerminatedString(containerInfo.ContainerID[:])
			metadata["container_image"] = c.nullTerminatedString(containerInfo.Image[:])
			metadata["container_started_at"] = fmt.Sprintf("%d", containerInfo.StartedAt)
		}

		// Process network events for service correlation
		if event.EventType == 5 { // EVENT_TYPE_NETWORK_CONN
			// Extract network info from event data with memory safety
			if netInfo, err := c.parseNetworkInfoSafely(event.Data[:]); err == nil {
				// Add network metadata
				metadata["src_ip"] = c.ipToString(netInfo.SAddr)
				metadata["dst_ip"] = c.ipToString(netInfo.DAddr)
				metadata["src_port"] = fmt.Sprintf("%d", netInfo.SPort)
				metadata["dst_port"] = fmt.Sprintf("%d", netInfo.DPort)
				metadata["protocol"] = c.protocolToString(netInfo.Protocol)
				metadata["direction"] = c.directionToString(netInfo.Direction)

				// Check if destination is a known service endpoint
				if serviceEndpoint, err := c.GetServiceEndpoint(netInfo.DAddr, netInfo.DPort); err == nil {
					metadata["service_name"] = c.nullTerminatedString(serviceEndpoint.ServiceName[:])
					metadata["service_namespace"] = c.nullTerminatedString(serviceEndpoint.Namespace[:])
					metadata["service_cluster_ip"] = c.nullTerminatedString(serviceEndpoint.ClusterIP[:])
				}
			}
		} else if event.EventType == 8 { // EVENT_TYPE_FILE_OPEN
			// Extract file info from event data with memory safety
			if fileInfo, err := c.parseFileInfoSafely(event.Data[:]); err == nil {
				// Add file metadata
				filename := c.nullTerminatedString(fileInfo.Filename[:])
				metadata["filename"] = filename
				metadata["flags"] = fmt.Sprintf("%d", fileInfo.Flags)
				metadata["mode"] = fmt.Sprintf("%d", fileInfo.Mode)

				// Check if this is a known ConfigMap/Secret mount
				if mountInfo, err := c.GetMountInfo(filename); err == nil {
					metadata["mount_name"] = c.nullTerminatedString(mountInfo.Name[:])
					metadata["mount_namespace"] = c.nullTerminatedString(mountInfo.Namespace[:])
					if mountInfo.IsSecret == 1 {
						metadata["mount_type"] = "secret"
					} else {
						metadata["mount_type"] = "configmap"
					}
				}
			}
		}

		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample, // Raw eBPF event data
			Metadata:  metadata,
			// Generate new trace for kernel events, or reuse pod trace if available
			TraceID: c.getOrGenerateTraceID(*event),
			SpanID:  collectors.GenerateSpanID(),
		}

		// Send through direct channel
		select {
		case c.events <- rawEvent:
			c.mu.Lock()
			c.stats.EventsCollected++
			c.stats.LastEventTime = time.Now()
			c.mu.Unlock()
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full
			c.mu.Lock()
			c.stats.EventsDropped++
			c.mu.Unlock()
		}

		// Collectors should only emit RawEvents - integrations handle publishing
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
	case 8:
		return "file_open"
	case 9:
		return "file_read"
	case 10:
		return "file_write"
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

// UpdateContainerInfo updates container information in the eBPF map for PID correlation
func (c *Collector) UpdateContainerInfo(pid uint32, containerID, podUID, image string) error {
	if c.objs == nil || c.objs.ContainerInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	containerInfo := &ContainerInfo{
		StartedAt: uint64(time.Now().Unix()),
	}

	// Copy strings with proper bounds checking
	copy(containerInfo.ContainerID[:], containerID)
	copy(containerInfo.PodUID[:], podUID)
	copy(containerInfo.Image[:], image)

	// Update the eBPF map
	return c.objs.ContainerInfoMap.Put(pid, containerInfo)
}

// RemoveContainerInfo removes container information from the eBPF map
func (c *Collector) RemoveContainerInfo(pid uint32) error {
	if c.objs == nil || c.objs.ContainerInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	return c.objs.ContainerInfoMap.Delete(pid)
}

// GetContainerInfo retrieves container information for a given PID
func (c *Collector) GetContainerInfo(pid uint32) (*ContainerInfo, error) {
	if c.objs == nil || c.objs.ContainerInfoMap == nil {
		return nil, fmt.Errorf("eBPF maps not initialized")
	}

	var containerInfo ContainerInfo
	err := c.objs.ContainerInfoMap.Lookup(pid, &containerInfo)
	if err != nil {
		return nil, err
	}

	return &containerInfo, nil
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

// UpdateServiceEndpoint updates service endpoint information for network correlation
func (c *Collector) UpdateServiceEndpoint(ip string, port uint16, serviceName, namespace, clusterIP string) error {
	if c.objs == nil || c.objs.ServiceEndpointsMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	// Convert IP string to uint32
	ipAddr := c.parseIPv4(ip)
	if ipAddr == 0 {
		return fmt.Errorf("invalid IP address: %s", ip)
	}

	// Create endpoint key
	key := c.makeEndpointKey(ipAddr, port)

	serviceEndpoint := &ServiceEndpoint{
		Port: port,
	}

	// Copy strings with proper bounds checking
	copy(serviceEndpoint.ServiceName[:], serviceName)
	copy(serviceEndpoint.Namespace[:], namespace)
	copy(serviceEndpoint.ClusterIP[:], clusterIP)

	// Update the eBPF map
	return c.objs.ServiceEndpointsMap.Put(key, serviceEndpoint)
}

// RemoveServiceEndpoint removes service endpoint information
func (c *Collector) RemoveServiceEndpoint(ip string, port uint16) error {
	if c.objs == nil || c.objs.ServiceEndpointsMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	// Convert IP string to uint32
	ipAddr := c.parseIPv4(ip)
	if ipAddr == 0 {
		return fmt.Errorf("invalid IP address: %s", ip)
	}

	// Create endpoint key
	key := c.makeEndpointKey(ipAddr, port)

	return c.objs.ServiceEndpointsMap.Delete(key)
}

// GetServiceEndpoint retrieves service information for a given IP and port
func (c *Collector) GetServiceEndpoint(ip uint32, port uint16) (*ServiceEndpoint, error) {
	if c.objs == nil || c.objs.ServiceEndpointsMap == nil {
		return nil, fmt.Errorf("eBPF maps not initialized")
	}

	// Create endpoint key
	key := c.makeEndpointKey(ip, port)

	var serviceEndpoint ServiceEndpoint
	err := c.objs.ServiceEndpointsMap.Lookup(key, &serviceEndpoint)
	if err != nil {
		return nil, err
	}

	return &serviceEndpoint, nil
}

// Helper functions for network correlation

// makeEndpointKey creates a key from IP and port for the service endpoints map
func (c *Collector) makeEndpointKey(ip uint32, port uint16) uint64 {
	return (uint64(ip) << 16) | uint64(port)
}

// parseIPv4 converts an IPv4 string to uint32
func (c *Collector) parseIPv4(ip string) uint32 {
	var a, b, c1, d uint32
	n, _ := fmt.Sscanf(ip, "%d.%d.%d.%d", &a, &b, &c1, &d)
	if n != 4 || a > 255 || b > 255 || c1 > 255 || d > 255 {
		return 0
	}
	return (a << 24) | (b << 16) | (c1 << 8) | d
}

// ipToString converts uint32 IP to string
func (c *Collector) ipToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(ip>>24)&0xff,
		(ip>>16)&0xff,
		(ip>>8)&0xff,
		ip&0xff)
}

// protocolToString converts protocol number to string
func (c *Collector) protocolToString(proto uint8) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("%d", proto)
	}
}

// directionToString converts direction flag to string
func (c *Collector) directionToString(dir uint8) string {
	if dir == 0 {
		return "outgoing"
	}
	return "incoming"
}

// UpdateMountInfo updates mount information for ConfigMap/Secret correlation
func (c *Collector) UpdateMountInfo(mountPath, name, namespace string, isSecret bool) error {
	if c.objs == nil || c.objs.MountInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	// Create mount key from path hash
	key := c.hashPath(mountPath)

	mountInfo := &MountInfo{}
	if isSecret {
		mountInfo.IsSecret = 1
	} else {
		mountInfo.IsSecret = 0
	}

	// Copy strings with proper bounds checking
	copy(mountInfo.Name[:], name)
	copy(mountInfo.Namespace[:], namespace)
	copy(mountInfo.MountPath[:], mountPath)

	// Update the eBPF map
	return c.objs.MountInfoMap.Put(key, mountInfo)
}

// RemoveMountInfo removes mount information
func (c *Collector) RemoveMountInfo(mountPath string) error {
	if c.objs == nil || c.objs.MountInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	// Create mount key from path hash
	key := c.hashPath(mountPath)

	return c.objs.MountInfoMap.Delete(key)
}

// GetMountInfo retrieves mount information for a given path
func (c *Collector) GetMountInfo(path string) (*MountInfo, error) {
	if c.objs == nil || c.objs.MountInfoMap == nil {
		return nil, fmt.Errorf("eBPF maps not initialized")
	}

	// Create mount key from path hash
	key := c.hashPath(path)

	var mountInfo MountInfo
	err := c.objs.MountInfoMap.Lookup(key, &mountInfo)
	if err != nil {
		return nil, err
	}

	return &mountInfo, nil
}

// hashPath computes a hash of a file path (simple DJB2 hash)
func (c *Collector) hashPath(path string) uint64 {
	hash := uint64(5381)
	for i := 0; i < len(path) && i < 64; i++ {
		hash = ((hash << 5) + hash) + uint64(path[i])
	}
	return hash
}

// UpdateDNSQuery updates DNS query information for service discovery correlation
func (c *Collector) UpdateDNSQuery(query, serviceName, namespace string, resolvedIP uint32, port uint16) error {
	if c.objs == nil {
		return fmt.Errorf("eBPF objects not initialized")
	}

	// DNS query correlation is handled through service_endpoints_map
	// Convert IP and port to combined key format
	key := (uint64(resolvedIP) << 32) | uint64(port)

	endpoint := ServiceEndpoint{
		Port: port,
	}

	// Copy service name and namespace to fixed-size arrays
	serviceBytes := []byte(serviceName)
	namespaceBytes := []byte(namespace)

	copy(endpoint.ServiceName[:], serviceBytes)
	copy(endpoint.Namespace[:], namespaceBytes)

	// Store cluster IP in the appropriate field
	copy(endpoint.ClusterIP[:], []byte(fmt.Sprintf("%d.%d.%d.%d",
		(resolvedIP>>24)&0xff,
		(resolvedIP>>16)&0xff,
		(resolvedIP>>8)&0xff,
		resolvedIP&0xff)))

	return c.objs.ServiceEndpointsMap.Put(key, endpoint)
}

// UpdateVolumeInfo updates PVC mount information for volume correlation
func (c *Collector) UpdateVolumeInfo(mountPath, pvcName, namespace, volumeID string) error {
	if c.objs == nil || c.objs.VolumeInfoMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	key := c.hashPath(mountPath)

	volumeInfo := &VolumeInfo{}
	copy(volumeInfo.PVCName[:], pvcName)
	copy(volumeInfo.Namespace[:], namespace)
	copy(volumeInfo.MountPath[:], mountPath)
	copy(volumeInfo.VolumeID[:], volumeID)

	return c.objs.VolumeInfoMap.Put(key, volumeInfo)
}

// UpdateProcessLineage updates process parent-child relationships for Job tracking
func (c *Collector) UpdateProcessLineage(pid, ppid, tgid uint32, startTime uint64, jobName string) error {
	if c.objs == nil || c.objs.ProcessLineageMap == nil {
		return fmt.Errorf("eBPF maps not initialized")
	}

	lineage := &ProcessLineage{
		PID:       pid,
		PPID:      ppid,
		TGID:      tgid,
		StartTime: startTime,
	}
	copy(lineage.JobName[:], jobName)

	return c.objs.ProcessLineageMap.Put(pid, lineage)
}

// Health returns detailed health information
func (c *Collector) Health() (bool, map[string]interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := map[string]interface{}{
		"healthy":          c.healthy,
		"events_collected": c.stats.EventsCollected,
		"events_dropped":   c.stats.EventsDropped,
		"error_count":      c.stats.ErrorCount,
		"last_event":       c.stats.LastEventTime,
		"ebpf_loaded":      c.objs != nil,
		"links_count":      len(c.links),
	}

	return c.healthy, health
}

// Statistics returns collector statistics
func (c *Collector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Calculate current performance metrics
	c.updatePerformanceMetrics()

	stats := map[string]interface{}{
		"events_collected":        c.stats.EventsCollected,
		"events_dropped":          c.stats.EventsDropped,
		"error_count":             c.stats.ErrorCount,
		"last_event_time":         c.stats.LastEventTime,
		"pod_trace_count":         len(c.podTraceMap),
		"perf_buffer_size":        c.stats.PerfBufferSize,
		"perf_buffer_capacity":    c.stats.PerfBufferCapacity,
		"perf_buffer_utilization": c.calculateBufferUtilization(),
		"perf_batches_processed":  c.stats.PerfBatchesProcessed,
		"perf_pool_in_use":        c.stats.PerfPoolInUse,
		"perf_events_processed":   c.stats.PerfEventsProcessed,
	}

	return stats
}

// updatePerformanceMetrics calculates current performance metrics
func (c *Collector) updatePerformanceMetrics() {
	// Set buffer capacity based on channel buffer size (this is a fixed value)
	c.stats.PerfBufferCapacity = 10000 // matches the buffer size in NewCollectorWithConfig

	// Calculate current buffer size (approximate based on channel length)
	c.stats.PerfBufferSize = uint64(len(c.events))

	// Pool in use represents active goroutines or resources
	c.stats.PerfPoolInUse = 1 // We have one main processing goroutine

	// Events processed is same as events collected for this implementation
	c.stats.PerfEventsProcessed = c.stats.EventsCollected

	// Batches processed - each read from ring buffer is a batch
	// For simplicity, assume each event is a batch
	c.stats.PerfBatchesProcessed = c.stats.EventsCollected
}

// calculateBufferUtilization returns buffer utilization as a percentage
func (c *Collector) calculateBufferUtilization() float64 {
	if c.stats.PerfBufferCapacity == 0 {
		return 0.0
	}
	return float64(c.stats.PerfBufferSize) / float64(c.stats.PerfBufferCapacity) * 100.0
}

// Removed domain-dependent functions - collectors should only emit RawEvents

// parseKernelEventSafely parses a KernelEvent from raw bytes with memory safety checks
func (c *Collector) parseKernelEventSafely(rawBytes []byte) (*KernelEvent, error) {
	expectedSize := int(unsafe.Sizeof(KernelEvent{}))

	// Validate buffer size
	if len(rawBytes) < expectedSize {
		return nil, fmt.Errorf("buffer too small: got %d bytes, expected at least %d", len(rawBytes), expectedSize)
	}

	// Check for exact size match (ensures no buffer overrun)
	if len(rawBytes) != expectedSize {
		return nil, fmt.Errorf("buffer size mismatch: got %d bytes, expected exactly %d", len(rawBytes), expectedSize)
	}

	// Check alignment - KernelEvent should be aligned to 8 bytes due to uint64 fields
	if uintptr(unsafe.Pointer(&rawBytes[0]))%8 != 0 {
		return nil, fmt.Errorf("buffer not properly aligned for KernelEvent")
	}

	// Safe to cast now
	event := *(*KernelEvent)(unsafe.Pointer(&rawBytes[0]))

	// Validate event content
	if event.EventType > 20 { // Sanity check for event type
		return nil, fmt.Errorf("invalid event type: %d", event.EventType)
	}

	return &event, nil
}

// parseNetworkInfoSafely parses NetworkInfo from raw bytes with memory safety checks
func (c *Collector) parseNetworkInfoSafely(rawBytes []byte) (*NetworkInfo, error) {
	expectedSize := int(unsafe.Sizeof(NetworkInfo{}))

	// Validate buffer size
	if len(rawBytes) < expectedSize {
		return nil, fmt.Errorf("buffer too small for NetworkInfo: got %d bytes, expected at least %d", len(rawBytes), expectedSize)
	}

	// Check alignment - NetworkInfo should be aligned to 4 bytes due to uint32 fields
	if uintptr(unsafe.Pointer(&rawBytes[0]))%4 != 0 {
		return nil, fmt.Errorf("buffer not properly aligned for NetworkInfo")
	}

	// Safe to cast
	netInfo := *(*NetworkInfo)(unsafe.Pointer(&rawBytes[0]))

	// Validate network info content
	if netInfo.Protocol > 255 || netInfo.Direction > 1 {
		return nil, fmt.Errorf("invalid network info: protocol=%d, direction=%d", netInfo.Protocol, netInfo.Direction)
	}

	return &netInfo, nil
}

// parseFileInfoSafely parses FileInfo from raw bytes with memory safety checks
func (c *Collector) parseFileInfoSafely(rawBytes []byte) (*FileInfo, error) {
	expectedSize := int(unsafe.Sizeof(FileInfo{}))

	// Validate buffer size
	if len(rawBytes) < expectedSize {
		return nil, fmt.Errorf("buffer too small for FileInfo: got %d bytes, expected at least %d", len(rawBytes), expectedSize)
	}

	// Check alignment - FileInfo should be aligned to 4 bytes due to uint32 fields
	if uintptr(unsafe.Pointer(&rawBytes[0]))%4 != 0 {
		return nil, fmt.Errorf("buffer not properly aligned for FileInfo")
	}

	// Safe to cast
	fileInfo := *(*FileInfo)(unsafe.Pointer(&rawBytes[0]))

	// Basic validation - ensure filename doesn't contain invalid characters
	for i := 0; i < len(fileInfo.Filename); i++ {
		if fileInfo.Filename[i] == 0 {
			break // Null terminator found
		}
		if fileInfo.Filename[i] < 32 && fileInfo.Filename[i] != 0 { // Non-printable characters (except null)
			return nil, fmt.Errorf("invalid filename contains non-printable character at position %d", i)
		}
	}

	return &fileInfo, nil
}
