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
	"github.com/yairfalse/tapio/pkg/collectors/common"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NetworkInfo represents network connection information
type NetworkInfo struct {
	SAddr     uint32 // Source IP (IPv4)
	DAddr     uint32 // Destination IP (IPv4)
	SPort     uint16 // Source port
	DPort     uint16 // Destination port
	Protocol  uint8  // IPPROTO_TCP or IPPROTO_UDP
	State     uint8  // Connection state
	Direction uint8  // 0=outgoing, 1=incoming
	_         uint8  // Padding
}

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
	Data      [64]byte // Can contain NetworkInfo for network events
}

// PodInfo represents pod information for correlation
type PodInfo struct {
	PodUID    [36]byte
	Namespace [64]byte
	PodName   [128]byte
	CreatedAt uint64
}

// ContainerInfo represents container information for PID correlation
type ContainerInfo struct {
	ContainerID [64]byte  // Docker/containerd ID
	PodUID      [36]byte  // Associated pod
	Image       [128]byte // Container image
	StartedAt   uint64    // Container start time
}

// ServiceEndpoint represents K8s service endpoint information
type ServiceEndpoint struct {
	ServiceName [64]byte // K8s service name
	Namespace   [64]byte // K8s namespace
	ClusterIP   [16]byte // Service cluster IP
	Port        uint16   // Service port
	_           [2]byte  // Padding
}

// FileInfo represents file operation information
type FileInfo struct {
	Filename [56]byte // File path (truncated)
	Flags    uint32   // Open flags
	Mode     uint32   // File mode
}

// MountInfo represents ConfigMap/Secret mount information
type MountInfo struct {
	Name      [64]byte  // ConfigMap/Secret name
	Namespace [64]byte  // K8s namespace
	MountPath [128]byte // Mount path in container
	IsSecret  uint8     // 1 if secret, 0 if configmap
	_         [7]byte   // Padding
}

// Collector implements minimal kernel monitoring via eBPF
type Collector struct {
	name          string
	objs          *kernelmonitorObjects
	links         []link.Link
	reader        *ringbuf.Reader
	events        chan collectors.RawEvent
	ctx           context.Context
	cancel        context.CancelFunc
	healthy       bool
	mu            sync.RWMutex
	podTraceMap   map[string]string                  // Map pod UID to trace ID
	natsPublisher *NATSPublisher                     // NATS publisher for events
	perfAdapter   *common.RawEventPerformanceAdapter // Performance adapter
	stats         CollectorStats
}

// CollectorStats tracks collector statistics
type CollectorStats struct {
	EventsCollected uint64
	EventsDropped   uint64
	ErrorCount      uint64
	LastEventTime   time.Time
}

// NewCollector creates a new minimal eBPF collector
func NewCollector(name string) (*Collector, error) {
	return NewCollectorWithConfig(&Config{
		Name:    name,
		NATSURL: "",
		Logger:  nil,
	})
}

// NewCollectorWithConfig creates a new eBPF collector with config
func NewCollectorWithConfig(config *Config) (*Collector, error) {
	// Create performance adapter configuration
	perfConfig := common.DefaultRawEventPerformanceConfig("ebpf-" + config.Name)
	perfConfig.BufferSize = 32768                   // Very large buffer for high-volume kernel events
	perfConfig.BatchSize = 500                      // Process more events per batch
	perfConfig.BatchTimeout = 50 * time.Millisecond // Faster processing for kernel events

	// Create performance adapter
	perfAdapter, err := common.NewRawEventPerformanceAdapter(perfConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create performance adapter: %w", err)
	}

	c := &Collector{
		name:        config.Name,
		events:      make(chan collectors.RawEvent, 1000), // Keep fallback channel
		healthy:     true,
		podTraceMap: make(map[string]string),
		perfAdapter: perfAdapter,
	}

	// Initialize NATS publisher if URL provided
	if config.NATSURL != "" && config.Logger != nil {
		publisher, err := NewNATSPublisher(config.NATSURL, config.Logger)
		if err != nil {
			// Log error but don't fail collector creation
			config.Logger.Error("Failed to create NATS publisher", zap.Error(err))
		} else {
			c.natsPublisher = publisher
		}
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

	// Attach network connection tracking
	tcpLink, err := link.Kprobe("tcp_v4_connect", c.objs.TraceTcpConnect, nil)
	if err != nil {
		// Log but don't fail - network tracking is optional
		fmt.Printf("Warning: failed to attach tcp connect kprobe: %v\n", err)
	} else {
		c.links = append(c.links, tcpLink)
	}

	// Attach file operation tracking
	openLink, err := link.Tracepoint("syscalls", "sys_enter_openat", c.objs.TraceOpenat, nil)
	if err != nil {
		// Log but don't fail - file tracking is optional
		fmt.Printf("Warning: failed to attach openat tracepoint: %v\n", err)
	} else {
		c.links = append(c.links, openLink)
	}

	// Open ring buffer
	c.reader, err = ringbuf.NewReader(c.objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}

	// Start performance adapter
	if err := c.perfAdapter.Start(); err != nil {
		// Clean up eBPF resources
		for _, l := range c.links {
			l.Close()
		}
		c.reader.Close()
		c.objs.Close()
		return fmt.Errorf("failed to start performance adapter: %w", err)
	}

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

	// Close NATS publisher
	if c.natsPublisher != nil {
		c.natsPublisher.Close()
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

	// Stop performance adapter
	if c.perfAdapter != nil {
		c.perfAdapter.Stop()
	}

	if c.events != nil {
		close(c.events)
	}
	c.healthy = false
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	// Return performance adapter's output channel if available
	if c.perfAdapter != nil {
		return c.perfAdapter.Events()
	}
	// Fallback to direct channel
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

		// Add container information if available
		if containerInfo, err := c.GetContainerInfo(event.PID); err == nil {
			metadata["container_id"] = c.nullTerminatedString(containerInfo.ContainerID[:])
			metadata["container_image"] = c.nullTerminatedString(containerInfo.Image[:])
			metadata["container_started_at"] = fmt.Sprintf("%d", containerInfo.StartedAt)
		}

		// Process network events for service correlation
		if event.EventType == 5 { // EVENT_TYPE_NETWORK_CONN
			// Extract network info from event data
			if len(event.Data) >= int(unsafe.Sizeof(NetworkInfo{})) {
				netInfo := *(*NetworkInfo)(unsafe.Pointer(&event.Data[0]))

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
			// Extract file info from event data
			if len(event.Data) >= int(unsafe.Sizeof(FileInfo{})) {
				fileInfo := *(*FileInfo)(unsafe.Pointer(&event.Data[0]))

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
			TraceID: c.getOrGenerateTraceID(event),
			SpanID:  collectors.GenerateSpanID(),
		}

		// Submit through performance adapter if available
		if c.perfAdapter != nil {
			if err := c.perfAdapter.Submit(&rawEvent); err != nil {
				c.mu.Lock()
				c.stats.EventsDropped++
				c.mu.Unlock()
			} else {
				c.mu.Lock()
				c.stats.EventsCollected++
				c.stats.LastEventTime = time.Now()
				c.mu.Unlock()
			}
		} else {
			// Fallback to direct channel send
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
		}

		// Publish to NATS if publisher available
		if c.natsPublisher != nil {
			// Convert to UnifiedEvent for NATS
			unifiedEvent := c.convertToUnifiedEvent(&rawEvent, event)
			if err := c.natsPublisher.PublishEvent(unifiedEvent); err != nil {
				// Log error but continue processing
			}
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

	stats := map[string]interface{}{
		"events_collected": c.stats.EventsCollected,
		"events_dropped":   c.stats.EventsDropped,
		"error_count":      c.stats.ErrorCount,
		"last_event_time":  c.stats.LastEventTime,
		"pod_trace_count":  len(c.podTraceMap),
	}

	// Add performance metrics if available
	if c.perfAdapter != nil {
		perfMetrics := c.perfAdapter.GetMetrics()
		stats["perf_buffer_size"] = perfMetrics.BufferSize
		stats["perf_buffer_capacity"] = perfMetrics.BufferCapacity
		stats["perf_buffer_utilization"] = perfMetrics.BufferUtilization
		stats["perf_batches_processed"] = perfMetrics.BatchesProcessed
		stats["perf_pool_in_use"] = perfMetrics.PoolInUse
		stats["perf_events_processed"] = perfMetrics.EventsProcessed
	}

	return stats
}

// convertToUnifiedEvent converts RawEvent to UnifiedEvent for NATS
func (c *Collector) convertToUnifiedEvent(raw *collectors.RawEvent, kernelEvent *KernelEvent) *domain.UnifiedEvent {
	// Build UnifiedEvent
	event := &domain.UnifiedEvent{
		ID:        domain.GenerateEventID(),
		Timestamp: raw.Timestamp,
		Type:      "ebpf",
		Source:    c.name,
		Severity:  c.getSeverityForEventType(kernelEvent.EventType),

		// Add trace context
		TraceContext: &domain.TraceContext{
			TraceID: raw.TraceID,
			SpanID:  raw.SpanID,
			Sampled: true,
		},

		// Kernel data
		Kernel: &domain.KernelData{
			PID:  kernelEvent.PID,
			TID:  kernelEvent.TID,
			Comm: c.nullTerminatedString(kernelEvent.Comm[:]),
		},

		// Copy metadata to attributes
		Attributes: make(map[string]interface{}),
	}

	// Copy all metadata to attributes
	for k, v := range raw.Metadata {
		event.Attributes[k] = v
	}

	// Add K8s context if we have pod info
	if podUID := c.nullTerminatedString(kernelEvent.PodUID[:]); podUID != "" {
		event.K8sContext = &domain.K8sContext{}
		if podInfo, err := c.GetPodInfo(kernelEvent.CgroupID); err == nil {
			event.K8sContext.Name = c.nullTerminatedString(podInfo.PodName[:])
			event.K8sContext.Namespace = c.nullTerminatedString(podInfo.Namespace[:])
		}
	}

	// Add entity context
	if event.K8sContext != nil && event.K8sContext.Name != "" {
		event.Entity = &domain.EntityContext{
			Type:      "pod",
			Name:      event.K8sContext.Name,
			Namespace: event.K8sContext.Namespace,
		}
	}

	// Add correlation hints
	event.CorrelationHints = []string{raw.TraceID}

	return event
}

// getSeverityForEventType returns severity based on event type
func (c *Collector) getSeverityForEventType(eventType uint32) domain.EventSeverity {
	switch eventType {
	case 1, 2: // memory alloc/free
		return domain.EventSeverityInfo
	case 3: // process exec
		return domain.EventSeverityWarning
	case 5: // network conn
		return domain.EventSeverityInfo
	case 8, 9, 10: // file operations
		return domain.EventSeverityInfo
	default:
		return domain.EventSeverityInfo
	}
}
