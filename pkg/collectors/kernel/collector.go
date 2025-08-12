package kernel

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
	"github.com/yairfalse/tapio/pkg/collectors/kernel/bpf"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/network"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/process"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/security"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ModularCollector implements modular kernel monitoring via eBPF
type ModularCollector struct {
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

	// Modular components
	securityCollector *security.Collector
	processCollector  *process.Collector
	networkCollector  *network.Collector
	k8sIntegration    *K8sIntegration

	// OTEL instrumentation - REQUIRED fields
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	eventsDropped   metric.Int64Counter
	ebpfOperations  metric.Int64Counter
	activeModules   metric.Int64UpDownCounter
}

// NewModularCollector creates a new modular kernel collector
func NewModularCollector(name string) (*ModularCollector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	return NewModularCollectorWithConfig(&Config{
		Name: name,
	}, logger)
}

// NewModularCollectorWithConfig creates a new modular kernel collector with config
func NewModularCollectorWithConfig(config *Config, logger *zap.Logger) (*ModularCollector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize OTEL components - MANDATORY pattern
	name := config.Name
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	eventsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription(fmt.Sprintf("Total events dropped by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events dropped counter", zap.Error(err))
	}

	ebpfOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF operations in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf operations counter", zap.Error(err))
	}

	activeModules, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_modules", name),
		metric.WithDescription(fmt.Sprintf("Active modules in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create active modules gauge", zap.Error(err))
	}

	c := &ModularCollector{
		name:            config.Name,
		logger:          logger,
		events:          make(chan collectors.RawEvent, 15000), // Larger buffer for all modules
		healthy:         true,
		podTraceMap:     make(map[string]string),
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		eventsDropped:   eventsDropped,
		ebpfOperations:  ebpfOperations,
		activeModules:   activeModules,
	}

	// Initialize modular components
	c.securityCollector = security.NewSecurityCollector(logger.Named("security"))
	c.processCollector = process.NewProcessCollector(logger.Named("process"))
	c.networkCollector = network.NewNetworkCollector(logger.Named("network"))

	// Initialize K8s integration
	c.k8sIntegration, err = NewK8sIntegration(c, logger.Named("k8s"))
	if err != nil {
		logger.Warn("Failed to initialize Kubernetes integration", zap.Error(err))
		// Continue without K8s integration - collector can work without it
	}

	return c, nil
}

// Name returns collector name
func (c *ModularCollector) Name() string {
	return c.name
}

// Start starts the modular kernel monitoring
func (c *ModularCollector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "kernel.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Load main eBPF program
	if c.ebpfOperations != nil {
		c.ebpfOperations.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "load"),
		))
	}

	spec, err := bpf.LoadKernelmonitor()
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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

	// Start modular components
	moduleCount := 0
	if err := c.securityCollector.Start(c.ctx); err != nil {
		c.logger.Error("Failed to start security collector", zap.Error(err))
		// Continue - security is optional
	} else {
		moduleCount++
	}

	if err := c.processCollector.Start(c.ctx); err != nil {
		c.logger.Error("Failed to start process collector", zap.Error(err))
		// Continue - process monitoring is optional
	} else {
		moduleCount++
	}

	if err := c.networkCollector.Start(c.ctx); err != nil {
		c.logger.Error("Failed to start network collector", zap.Error(err))
		// Continue - network monitoring is optional
	} else {
		moduleCount++
	}

	if c.activeModules != nil {
		c.activeModules.Add(ctx, int64(moduleCount), metric.WithAttributes(
			attribute.String("component", c.name),
		))
	}

	// Start K8s integration if available
	if c.k8sIntegration != nil {
		if err := c.k8sIntegration.Start(c.ctx); err != nil {
			c.logger.Warn("Failed to start Kubernetes integration", zap.Error(err))
		}
	}

	// Attach legacy tracepoints for backward compatibility
	if err := c.attachLegacyTracepoints(); err != nil {
		return fmt.Errorf("failed to attach legacy tracepoints: %w", err)
	}

	// Open ring buffer
	c.reader, err = ringbuf.NewReader(c.objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}

	// Start event processing
	go c.processEvents()
	go c.aggregateModularEvents()

	c.healthy = true
	span.SetStatus(codes.Ok, "Kernel collector started successfully")
	span.SetAttributes(
		attribute.Int("modules_active", moduleCount),
		attribute.Int("links_attached", len(c.links)),
	)
	c.logger.Info("Modular kernel collector started",
		zap.Int("active_modules", moduleCount),
		zap.Int("ebpf_links", len(c.links)),
	)
	return nil
}

// Stop stops the modular collector
func (c *ModularCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Stop modular components
	if c.securityCollector != nil {
		c.securityCollector.Stop()
	}
	if c.processCollector != nil {
		c.processCollector.Stop()
	}
	if c.networkCollector != nil {
		c.networkCollector.Stop()
	}
	if c.k8sIntegration != nil {
		c.k8sIntegration.Stop()
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

	if c.events != nil {
		close(c.events)
	}

	c.healthy = false
	c.logger.Info("Modular kernel collector stopped")
	return nil
}

// Events returns the event channel
func (c *ModularCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *ModularCollector) IsHealthy() bool {
	return c.healthy
}

// attachLegacyTracepoints attaches backward-compatible tracepoints
func (c *ModularCollector) attachLegacyTracepoints() error {
	// Memory tracking
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

	// Process execution tracking
	execLink, err := link.Tracepoint("sched", "sched_process_exec", c.objs.TraceExec, nil)
	if err != nil {
		return fmt.Errorf("failed to attach exec tracepoint: %w", err)
	}
	c.links = append(c.links, execLink)

	// Network connection tracking
	tcpLink, err := link.Kprobe("tcp_v4_connect", c.objs.TraceTcpConnect, nil)
	if err != nil {
		c.logger.Warn("failed to attach tcp connect kprobe", zap.Error(err))
	} else {
		c.links = append(c.links, tcpLink)
	}

	// File operation tracking
	openLink, err := link.Tracepoint("syscalls", "sys_enter_openat", c.objs.TraceOpenat, nil)
	if err != nil {
		c.logger.Warn("failed to attach openat tracepoint", zap.Error(err))
	} else {
		c.links = append(c.links, openLink)
	}

	return nil
}

// aggregateModularEvents aggregates events from modular components
func (c *ModularCollector) aggregateModularEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event := <-c.securityCollector.Events():
			c.forwardEvent(event)
		case event := <-c.processCollector.Events():
			c.forwardEvent(event)
		case event := <-c.networkCollector.Events():
			c.forwardEvent(event)
		}
	}
}

// forwardEvent forwards events from modular components
func (c *ModularCollector) forwardEvent(event collectors.RawEvent) {
	// Create span for forwarding
	ctx, span := c.tracer.Start(c.ctx, "kernel.forward_event")
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", event.Type),
		attribute.String("source_module", event.Metadata["module"]),
	)

	select {
	case c.events <- event:
		c.mu.Lock()
		c.stats.EventsCollected++
		c.stats.LastEventTime = time.Now()
		c.mu.Unlock()

		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", event.Type),
				attribute.String("module", event.Metadata["module"]),
			))
		}
		span.SetStatus(codes.Ok, "")
	case <-c.ctx.Done():
		return
	default:
		// Drop event if buffer full
		c.mu.Lock()
		c.stats.EventsDropped++
		c.mu.Unlock()
		if c.eventsDropped != nil {
			c.eventsDropped.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
				attribute.String("module", event.Metadata["module"]),
			))
		}
		span.SetStatus(codes.Error, "event dropped - buffer full")
	}
}

// processEvents processes events from the main ring buffer (legacy events)
func (c *ModularCollector) processEvents() {
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
			if c.errorsTotal != nil {
				c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "read_error"),
				))
			}
			continue
		}

		// Create span for event processing
		ctx, span := c.tracer.Start(c.ctx, "kernel.process_event")
		start := time.Now()

		// Parse event with memory safety checks
		event, err := c.parseKernelEventSafely(record.RawSample)
		if err != nil {
			c.mu.Lock()
			c.stats.ErrorCount++
			c.mu.Unlock()
      
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "parse_error"),
				))
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			c.logger.Debug("Failed to parse kernel event", 
			
				zap.Error(err),
				zap.Int("buffer_size", len(record.RawSample)),
				zap.String("error_type", "parse_failure"))
			continue
		}

		// Convert to RawEvent - enhanced with modular collector metadata
		metadata := map[string]string{
			"collector": "kernel",
			"module":    "legacy", // Mark as legacy event
			"pid":       fmt.Sprintf("%d", event.PID),
			"tid":       fmt.Sprintf("%d", event.TID),
			"comm":      c.nullTerminatedString(event.Comm[:]),
			"size":      fmt.Sprintf("%d", event.Size),
			"cgroup_id": fmt.Sprintf("%d", event.CgroupID),
			"pod_uid":   c.nullTerminatedString(event.PodUID[:]),
		}

		// Enhanced K8s metadata from eBPF maps
		if event.CgroupID != 0 {
			if podInfo, err := c.GetPodInfo(event.CgroupID); err == nil {
				metadata["k8s_namespace"] = c.nullTerminatedString(podInfo.Namespace[:])
				metadata["k8s_name"] = c.nullTerminatedString(podInfo.PodName[:])
				metadata["k8s_kind"] = "Pod"
				metadata["k8s_uid"] = c.nullTerminatedString(podInfo.PodUID[:])
			}
		}

		// Enhanced container correlation
		if containerInfo, err := c.GetContainerInfo(event.PID); err == nil {
			metadata["container_id"] = c.nullTerminatedString(containerInfo.ContainerID[:])
			metadata["container_image"] = c.nullTerminatedString(containerInfo.Image[:])
			metadata["container_started_at"] = fmt.Sprintf("%d", containerInfo.StartedAt)
		}

		// Enhanced network correlation
		if event.EventType == 5 { // EVENT_TYPE_NETWORK_CONN
			if netInfo, err := c.parseNetworkInfoSafely(event.Data[:]); err == nil {
				metadata["src_ip"] = c.ipToString(netInfo.SAddr)
				metadata["dst_ip"] = c.ipToString(netInfo.DAddr)
				metadata["src_port"] = fmt.Sprintf("%d", netInfo.SPort)
				metadata["dst_port"] = fmt.Sprintf("%d", netInfo.DPort)
				metadata["protocol"] = c.protocolToString(netInfo.Protocol)
				metadata["direction"] = c.directionToString(netInfo.Direction)

				// Enhanced service endpoint correlation
				if serviceEndpoint, err := c.GetServiceEndpoint(netInfo.DAddr, netInfo.DPort); err == nil {
					metadata["service_name"] = c.nullTerminatedString(serviceEndpoint.ServiceName[:])
					metadata["service_namespace"] = c.nullTerminatedString(serviceEndpoint.Namespace[:])
					metadata["service_cluster_ip"] = c.nullTerminatedString(serviceEndpoint.ClusterIP[:])
				}
			} else {
				c.logger.Debug("Failed to parse network info for network event",
					zap.Error(err),
					zap.Uint32("event_type", event.EventType),
					zap.Int("data_size", len(event.Data)))
			}
		} else if event.EventType == 8 { // EVENT_TYPE_FILE_OPEN
			if fileInfo, err := c.parseFileInfoSafely(event.Data[:]); err == nil {
				filename := c.nullTerminatedString(fileInfo.Filename[:])
				metadata["filename"] = filename
				metadata["flags"] = fmt.Sprintf("%d", fileInfo.Flags)
				metadata["mode"] = fmt.Sprintf("%d", fileInfo.Mode)

				// Enhanced mount correlation
				if mountInfo, err := c.GetMountInfo(filename); err == nil {
					metadata["mount_name"] = c.nullTerminatedString(mountInfo.Name[:])
					metadata["mount_namespace"] = c.nullTerminatedString(mountInfo.Namespace[:])
					if mountInfo.IsSecret == 1 {
						metadata["mount_type"] = "secret"
					} else {
						metadata["mount_type"] = "configmap"
					}
				}
			} else {
				c.logger.Debug("Failed to parse file info for file open event",
					zap.Error(err),
					zap.Uint32("event_type", event.EventType),
					zap.Int("data_size", len(event.Data)))
			}
		}

		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample,
			Metadata:  metadata,
			TraceID:   c.getOrGenerateTraceID(*event),
			SpanID:    collectors.GenerateSpanID(),
		}

		// Set span attributes
		span.SetAttributes(
			attribute.String("component", c.name),
			attribute.String("operation", "process_event"),
			attribute.String("event.type", rawEvent.Type),
			attribute.String("event.id", rawEvent.SpanID),
			attribute.Int("event.pid", int(event.PID)),
		)

		select {
		case c.events <- rawEvent:
			c.mu.Lock()
			c.stats.EventsCollected++
			c.stats.LastEventTime = time.Now()
			c.mu.Unlock()

			// Record success metrics
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", rawEvent.Type),
					attribute.String("module", "legacy"),
				))
			}

			// Record processing time
			duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
			if c.processingTime != nil {
				c.processingTime.Record(ctx, duration, metric.WithAttributes(
					attribute.String("event_type", rawEvent.Type),
				))
			}

			span.SetStatus(codes.Ok, "")
		case <-c.ctx.Done():
			span.End()
			return
		default:
			c.mu.Lock()
			c.stats.EventsDropped++
			c.mu.Unlock()
			if c.eventsDropped != nil {
				c.eventsDropped.Add(ctx, 1, metric.WithAttributes(
					attribute.String("reason", "buffer_full"),
					attribute.String("event_type", rawEvent.Type),
				))
			}
			span.SetAttributes(attribute.String("dropped", "buffer_full"))
			span.SetStatus(codes.Error, "event dropped - buffer full")
		}

		span.End()
	}
}

// The rest of the methods remain the same as the original collector
// (populateContainerPIDs, UpdatePodInfo, GetPodInfo, etc.)
// ... [Include all other methods from the original collector.go]

// populateContainerPIDs finds and adds container PIDs to the map
func (c *ModularCollector) populateContainerPIDs() error {
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

// eventTypeToString converts event type to string
func (c *ModularCollector) eventTypeToString(eventType uint32) string {
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
func (c *ModularCollector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// UpdatePodInfo updates pod information in the kernel eBPF map for correlation
func (c *ModularCollector) UpdatePodInfo(cgroupID uint64, podUID, namespace, podName string) error {
	if c.objs == nil || c.objs.PodInfoMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
	}

	podInfo := PodInfo{
		CreatedAt: uint64(time.Now().UnixNano()),
	}

	// Copy strings with proper bounds checking
	copy(podInfo.PodUID[:], podUID)
	copy(podInfo.Namespace[:], namespace)
	copy(podInfo.PodName[:], podName)

	// Update the kernel eBPF map
	return c.objs.PodInfoMap.Put(cgroupID, podInfo)
}

// RemovePodInfo removes pod information from the kernel eBPF map
func (c *ModularCollector) RemovePodInfo(cgroupID uint64) error {
	if c.objs == nil || c.objs.PodInfoMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
	}

	return c.objs.PodInfoMap.Delete(cgroupID)
}

// GetPodInfo retrieves pod information for a given cgroup ID
func (c *ModularCollector) GetPodInfo(cgroupID uint64) (*PodInfo, error) {
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

// UpdateContainerInfo updates container information in the kernel eBPF map for PID correlation
func (c *ModularCollector) UpdateContainerInfo(pid uint32, containerID, podUID, image string) error {
	if c.objs == nil || c.objs.ContainerInfoMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
	}

	containerInfo := &ContainerInfo{
		StartedAt: uint64(time.Now().Unix()),
	}

	// Copy strings with proper bounds checking
	copy(containerInfo.ContainerID[:], containerID)
	copy(containerInfo.PodUID[:], podUID)
	copy(containerInfo.Image[:], image)

	// Update the kernel eBPF map
	return c.objs.ContainerInfoMap.Put(pid, containerInfo)
}

// RemoveContainerInfo removes container information from the kernel eBPF map
func (c *ModularCollector) RemoveContainerInfo(pid uint32) error {
	if c.objs == nil || c.objs.ContainerInfoMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
	}

	return c.objs.ContainerInfoMap.Delete(pid)
}

// GetContainerInfo retrieves container information for a given PID
func (c *ModularCollector) GetContainerInfo(pid uint32) (*ContainerInfo, error) {
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
func (c *ModularCollector) getOrGenerateTraceID(event KernelEvent) string {
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
func (c *ModularCollector) UpdateServiceEndpoint(ip string, port uint16, serviceName, namespace, clusterIP string) error {
	if c.objs == nil || c.objs.ServiceEndpointsMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
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

	// Update the kernel eBPF map
	return c.objs.ServiceEndpointsMap.Put(key, serviceEndpoint)
}

// RemoveServiceEndpoint removes service endpoint information
func (c *ModularCollector) RemoveServiceEndpoint(ip string, port uint16) error {
	if c.objs == nil || c.objs.ServiceEndpointsMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
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
func (c *ModularCollector) GetServiceEndpoint(ip uint32, port uint16) (*ServiceEndpoint, error) {
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
func (c *ModularCollector) makeEndpointKey(ip uint32, port uint16) uint64 {
	return (uint64(ip) << 16) | uint64(port)
}

// parseIPv4 converts an IPv4 string to uint32
func (c *ModularCollector) parseIPv4(ip string) uint32 {
	var a, b, c1, d uint32
	n, _ := fmt.Sscanf(ip, "%d.%d.%d.%d", &a, &b, &c1, &d)
	if n != 4 || a > 255 || b > 255 || c1 > 255 || d > 255 {
		return 0
	}
	return (a << 24) | (b << 16) | (c1 << 8) | d
}

// ipToString converts uint32 IP to string
func (c *ModularCollector) ipToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(ip>>24)&0xff,
		(ip>>16)&0xff,
		(ip>>8)&0xff,
		ip&0xff)
}

// protocolToString converts protocol number to string
func (c *ModularCollector) protocolToString(proto uint8) string {
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
func (c *ModularCollector) directionToString(dir uint8) string {
	if dir == 0 {
		return "outgoing"
	}
	return "incoming"
}

// UpdateMountInfo updates mount information for ConfigMap/Secret correlation
func (c *ModularCollector) UpdateMountInfo(mountPath, name, namespace string, isSecret bool) error {
	if c.objs == nil || c.objs.MountInfoMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
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

	// Update the kernel eBPF map
	return c.objs.MountInfoMap.Put(key, mountInfo)
}

// RemoveMountInfo removes mount information
func (c *ModularCollector) RemoveMountInfo(mountPath string) error {
	if c.objs == nil || c.objs.MountInfoMap == nil {
		return fmt.Errorf("kernel eBPF maps not initialized")
	}

	// Create mount key from path hash
	key := c.hashPath(mountPath)

	return c.objs.MountInfoMap.Delete(key)
}

// GetMountInfo retrieves mount information for a given path
func (c *ModularCollector) GetMountInfo(path string) (*MountInfo, error) {
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
func (c *ModularCollector) hashPath(path string) uint64 {
	hash := uint64(5381)
	for i := 0; i < len(path) && i < 64; i++ {
		hash = ((hash << 5) + hash) + uint64(path[i])
	}
	return hash
}

// parseKernelEventSafely parses a KernelEvent from raw bytes with memory safety checks
func (c *ModularCollector) parseKernelEventSafely(rawBytes []byte) (*KernelEvent, error) {
	expectedSize := int(unsafe.Sizeof(KernelEvent{}))

	// Validate buffer size
	if len(rawBytes) < expectedSize {
		return nil, fmt.Errorf("buffer too small: got %d bytes, expected at least %d", len(rawBytes), expectedSize)
	}

	// Check for exact size match (ensures no buffer overrun)
	if len(rawBytes) != expectedSize {
		return nil, fmt.Errorf("buffer size mismatch: got %d bytes, expected exactly %d", len(rawBytes), expectedSize)
	}

	// Use the new SafeCast method for comprehensive validation
	safeParser := collectors.NewSafeParser()
	event, err := collectors.SafeCast[KernelEvent](safeParser, rawBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to safely parse KernelEvent: %w", err)
	}

	// Additional validation using safe parser methods
	if err := safeParser.ValidateEventType(event.EventType, 1, 20); err != nil {
		return nil, fmt.Errorf("invalid kernel event: %w", err)
	}

	return event, nil
}

// parseNetworkInfoSafely parses NetworkInfo from raw bytes with memory safety checks
func (c *ModularCollector) parseNetworkInfoSafely(rawBytes []byte) (*NetworkInfo, error) {
	// Use the new SafeCast method for comprehensive validation
	safeParser := collectors.NewSafeParser()
	netInfo, err := collectors.SafeCast[NetworkInfo](safeParser, rawBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to safely parse NetworkInfo: %w", err)
	}

	// Validate network info content using safe parser methods
	if err := safeParser.ValidateNetworkData(netInfo.Protocol, netInfo.Direction); err != nil {
		return nil, fmt.Errorf("invalid network info: %w", err)
	}

	return netInfo, nil
}

// parseFileInfoSafely parses FileInfo from raw bytes with memory safety checks
func (c *ModularCollector) parseFileInfoSafely(rawBytes []byte) (*FileInfo, error) {
	// Use the new SafeCast method for comprehensive validation
	safeParser := collectors.NewSafeParser()
	fileInfo, err := collectors.SafeCast[FileInfo](safeParser, rawBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to safely parse FileInfo: %w", err)
	}

	// Validate filename field using safe parser methods
	if err := safeParser.ValidateStringField(fileInfo.Filename[:], "filename"); err != nil {
		return nil, fmt.Errorf("invalid filename field: %w", err)
	}

	return fileInfo, nil
}

// Statistics returns collector statistics
func (c *ModularCollector) Statistics() map[string]interface{} {
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
func (c *ModularCollector) updatePerformanceMetrics() {
	// Set buffer capacity based on channel buffer size
	c.stats.PerfBufferCapacity = uint64(cap(c.events)) // matches the actual buffer size

	// Calculate current buffer size (approximate based on channel length)
	c.stats.PerfBufferSize = uint64(len(c.events))

	// Pool in use represents active modules
	poolInUse := uint64(1) // Main collector
	if c.securityCollector != nil {
		poolInUse++
	}
	if c.processCollector != nil {
		poolInUse++
	}
	if c.networkCollector != nil {
		poolInUse++
	}
	if c.k8sIntegration != nil {
		poolInUse++
	}
	c.stats.PerfPoolInUse = poolInUse

	// Events processed is same as events collected for this implementation
	c.stats.PerfEventsProcessed = c.stats.EventsCollected

	// Batches processed - each read from ring buffer is a batch
	// For simplicity, assume each event is a batch
	c.stats.PerfBatchesProcessed = c.stats.EventsCollected
}

// calculateBufferUtilization returns buffer utilization as a percentage
func (c *ModularCollector) calculateBufferUtilization() float64 {
	if c.stats.PerfBufferCapacity == 0 {
		return 0.0
	}
	return float64(c.stats.PerfBufferSize) / float64(c.stats.PerfBufferCapacity) * 100.0
}

// Health returns detailed health information for modular collector
func (c *ModularCollector) Health() (bool, map[string]interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := map[string]interface{}{
		"healthy":            c.healthy,
		"events_collected":   c.stats.EventsCollected,
		"events_dropped":     c.stats.EventsDropped,
		"error_count":        c.stats.ErrorCount,
		"last_event":         c.stats.LastEventTime,
		"kernel_ebpf_loaded": c.objs != nil,
		"links_count":        len(c.links),
		"modules": map[string]interface{}{
			"security_active": c.securityCollector != nil,
			"process_active":  c.processCollector != nil,
			"network_active":  c.networkCollector != nil,
			"k8s_active":      c.k8sIntegration != nil,
		},
	}

	return c.healthy, health
}
