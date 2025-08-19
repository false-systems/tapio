//go:build linux
// +build linux

package kernel

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/bpf_common"
	"github.com/yairfalse/tapio/pkg/collectors/kernel/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 kernelMonitor ./bpf_src/kernel_monitor.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfState struct {
	objs   *bpf.KernelmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// Container patterns for extracting container IDs from cgroup paths
var (
	// Docker pattern: /docker/[container_id]
	dockerPattern = regexp.MustCompile(`/docker/([a-f0-9]{64})`)
	// containerd pattern: /containerd.service/[container_id]
	containerdPattern = regexp.MustCompile(`/containerd[^/]*/([a-f0-9]{64})`)
	// cri-o pattern: /crio-[container_id].scope
	crioPattern = regexp.MustCompile(`/crio-([a-f0-9]{64})\.scope`)
	// Kubernetes pod UID pattern from cgroup
	podUIDPattern = regexp.MustCompile(`/pod([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})`)
)

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "kernel.ebpf.start")
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to remove memory limit")
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := bpf.KernelmonitorObjects{}
	if err := bpf.LoadKernelmonitorObjects(&objs, nil); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to load eBPF objects")
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &ebpfState{objs: &objs}

	// Attach eBPF programs to tracepoints with retry logic
	// Monitor process events
	var processLink link.Link
	retryConfig := bpf_common.DefaultRetryConfig()
	retryConfig.MaxAttempts = DefaultMaxRetryAttempts
	retryConfig.InitialDelay = DefaultRetryInitialDelay

	err = bpf_common.RetryWithBackoff(ctx, retryConfig, func(ctx context.Context) error {
		var attachErr error
		processLink, attachErr = link.Tracepoint("syscalls", "sys_enter_execve", objs.TraceExec, nil)
		return attachErr
	})
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching execve tracepoint after %d retries: %w", retryConfig.MaxAttempts, err)
	}

	// Monitor file operations
	fileLink, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenat, nil)
	if err != nil {
		processLink.Close()
		objs.Close()
		return fmt.Errorf("attaching openat tracepoint: %w", err)
	}

	// Monitor TCP IPv4 connections
	tcpV4Link, err := link.Kprobe("tcp_v4_connect", objs.TraceTcpV4Connect, nil)
	if err != nil {
		c.logger.Warn("Failed to attach TCP v4 kprobe, continuing without network monitoring", zap.Error(err))
		tcpV4Link = nil
	}

	// Monitor TCP IPv6 connections
	tcpV6Link, err := link.Kprobe("tcp_v6_connect", objs.TraceTcpV6Connect, nil)
	if err != nil {
		c.logger.Warn("Failed to attach TCP v6 kprobe, continuing without IPv6 monitoring", zap.Error(err))
		tcpV6Link = nil
	}

	// Monitor UDP connections (both IPv4 and IPv6)
	udpLink, err := link.Kprobe("udp_sendmsg", objs.TraceUdpSend, nil)
	if err != nil {
		c.logger.Warn("Failed to attach UDP kprobe, continuing without UDP monitoring", zap.Error(err))
		udpLink = nil
	}

	// Ensure cleanup on any error from this point
	cleanupLinks := func() {
		if udpLink != nil {
			udpLink.Close()
		}
		if tcpV6Link != nil {
			tcpV6Link.Close()
		}
		if tcpV4Link != nil {
			tcpV4Link.Close()
		}
		if fileLink != nil {
			fileLink.Close()
		}
		if processLink != nil {
			processLink.Close()
		}
		objs.Close()
	}

	c.ebpfState.(*ebpfState).reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		cleanupLinks()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	// Collect all valid links
	var allLinks []link.Link
	allLinks = append(allLinks, processLink, fileLink)
	if tcpV4Link != nil {
		allLinks = append(allLinks, tcpV4Link)
	}
	if tcpV6Link != nil {
		allLinks = append(allLinks, tcpV6Link)
	}
	if udpLink != nil {
		allLinks = append(allLinks, udpLink)
	}

	c.ebpfState.(*ebpfState).links = allLinks

	c.logger.Info("eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(c.ebpfState.(*ebpfState).links)),
		zap.Bool("tcp_v4_enabled", tcpV4Link != nil),
		zap.Bool("tcp_v6_enabled", tcpV6Link != nil),
		zap.Bool("udp_enabled", udpLink != nil),
	)

	return nil
}

// stopEBPF cleans up eBPF resources - Linux only
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)

	// Close reader
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	for _, link := range state.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.logger.Info("eBPF monitoring stopped", zap.String("collector", c.name))
}

// readEBPFEvents processes eBPF ring buffer events - Linux only
func (c *Collector) readEBPFEvents() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	if state.reader == nil {
		return
	}

	ctx := c.ctx
	for {
		select {
		case <-ctx.Done():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the event with timing
			startTime := time.Now()
			if len(record.RawSample) < int(unsafe.Sizeof(KernelEvent{})) {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				continue
			}

			var event KernelEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
				c.logger.Error("Failed to parse kernel event", zap.Error(err))
				continue
			}

			// Enrich event with container/pod information
			c.enrichEventWithContainerInfo(&event)

			// Convert to raw event - simple and efficient
			rawEvent := c.convertToRawEvent(event)

			// Record processing time
			if c.processingTime != nil {
				duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
				c.processingTime.Record(ctx, duration)
			}

			// Update buffer usage gauge
			if c.bufferUsage != nil {
				c.bufferUsage.Record(ctx, int64(len(c.events)))
			}

			// Send to event channel
			select {
			case c.events <- rawEvent:
				if c.eventsProcessed != nil {
					c.eventsProcessed.Add(ctx, 1)
				}
			case <-ctx.Done():
				return
			default:
				// Buffer full, drop event
				if c.droppedEvents != nil {
					c.droppedEvents.Add(ctx, 1)
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1)
				}
			}
		}
	}
}

// enrichEventWithContainerInfo enriches the kernel event with container and pod information
func (c *Collector) enrichEventWithContainerInfo(event *KernelEvent) {
	// If we already have pod UID, skip enrichment
	if event.PodUID[0] != 0 {
		return
	}

	// Try to get container info from cgroup ID
	if event.CgroupID != 0 {
		// Try to read cgroup path from /proc/PID/cgroup
		cgroupPath := c.getCgroupPath(event.PID)
		if cgroupPath != "" {
			// Extract container ID
			containerID := c.extractContainerID(cgroupPath)
			if containerID != "" {
				c.logger.Debug("Found container ID for PID",
					zap.Uint32("pid", event.PID),
					zap.String("container_id", containerID),
					zap.Uint64("cgroup_id", event.CgroupID),
				)

				// Store container ID in event data for later processing
				// We'll use the first 12 chars of container ID as a marker
				if len(containerID) >= 12 {
					copy(event.Data[:12], containerID[:12])
				}
			}

			// Extract pod UID
			podUID := c.extractPodUID(cgroupPath)
			if podUID != "" {
				c.logger.Debug("Found pod UID for PID",
					zap.Uint32("pid", event.PID),
					zap.String("pod_uid", podUID),
				)
				copy(event.PodUID[:], podUID)
			}
		}
	}
}

// getCgroupPath reads the cgroup path for a given PID
func (c *Collector) getCgroupPath(pid uint32) string {
	cgroupFile := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := ioutil.ReadFile(cgroupFile)
	if err != nil {
		return ""
	}

	// Parse cgroup file content
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		// Look for the systemd or unified cgroup path
		if strings.Contains(line, "::") {
			parts := strings.SplitN(line, "::", 2)
			if len(parts) == 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// extractContainerID extracts container ID from cgroup path
func (c *Collector) extractContainerID(cgroupPath string) string {
	// Try Docker pattern
	if matches := dockerPattern.FindStringSubmatch(cgroupPath); len(matches) > 1 {
		return matches[1]
	}

	// Try containerd pattern
	if matches := containerdPattern.FindStringSubmatch(cgroupPath); len(matches) > 1 {
		return matches[1]
	}

	// Try cri-o pattern
	if matches := crioPattern.FindStringSubmatch(cgroupPath); len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// extractPodUID extracts Kubernetes pod UID from cgroup path
func (c *Collector) extractPodUID(cgroupPath string) string {
	if matches := podUIDPattern.FindStringSubmatch(cgroupPath); len(matches) > 1 {
		return matches[1]
	}

	// Alternative: check for kubepods in path and extract UID
	if strings.Contains(cgroupPath, "kubepods") {
		// Try to extract pod UID from various formats
		// Format: /kubepods/besteffort/pod[uid]/[container_id]
		// or: /kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod[uid].slice
		if matches := regexp.MustCompile(`pod([a-f0-9]{8}_[a-f0-9]{4}_[a-f0-9]{4}_[a-f0-9]{4}_[a-f0-9]{12})`).FindStringSubmatch(cgroupPath); len(matches) > 1 {
			// Convert underscores to hyphens
			return strings.ReplaceAll(matches[1], "_", "-")
		}
	}

	return ""
}

// getContainerRuntime tries to detect the container runtime
func (c *Collector) getContainerRuntime() string {
	// Check for Docker
	if _, err := filepath.Glob("/var/run/docker.sock"); err == nil {
		return "docker"
	}

	// Check for containerd
	if _, err := filepath.Glob("/run/containerd/containerd.sock"); err == nil {
		return "containerd"
	}

	// Check for cri-o
	if _, err := filepath.Glob("/var/run/crio/crio.sock"); err == nil {
		return "crio"
	}

	return "unknown"
}

// convertToRawEvent converts eBPF event to raw event format - Linux only
func (c *Collector) convertToRawEvent(event KernelEvent) domain.RawEvent {
	// Convert timestamp to time.Time format expected by RawEvent
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Convert the raw eBPF event to bytes for the Data field
	eventBytes := (*[unsafe.Sizeof(event)]byte)(unsafe.Pointer(&event))[:]
	dataCopy := make([]byte, len(eventBytes))
	copy(dataCopy, eventBytes)

	return domain.RawEvent{
		Timestamp: timestamp,
		Source:    c.name,   // kernel collector name
		Data:      dataCopy, // Raw eBPF event data
	}
}
