//go:build linux
// +build linux

package helmcorrelator

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
)

// ebpfComponents holds Linux-specific eBPF components
type ebpfComponents struct {
	objs   *helmmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and starts eBPF monitoring on Linux
func (c *Collector) startEBPF() error {
	c.logger.Debug("Starting eBPF Helm monitoring")

	// Remove memory lock limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock limit", zap.Error(err))
	}

	// Load pre-compiled eBPF objects
	objs := &helmmonitorObjects{}
	if err := loadHelmmonitorObjects(objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			c.logger.Error("eBPF verifier error", zap.String("details", ve.Error()))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Attach to tracepoints
	links := []link.Link{}

	// Attach exec tracepoint
	execLink, err := link.Tracepoint("sched", "sched_process_exec", objs.TraceExec)
	if err != nil {
		objs.Close()
		return fmt.Errorf("failed to attach exec tracepoint: %w", err)
	}
	links = append(links, execLink)

	// Attach exit tracepoint
	exitLink, err := link.Tracepoint("sched", "sched_process_exit", objs.TraceExit)
	if err != nil {
		for _, l := range links {
			l.Close()
		}
		objs.Close()
		return fmt.Errorf("failed to attach exit tracepoint: %w", err)
	}
	links = append(links, exitLink)

	// Attach file operations tracepoint
	if c.config.TrackFiles {
		openLink, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenat)
		if err != nil {
			c.logger.Warn("Failed to attach file tracking", zap.Error(err))
			// Continue without file tracking
		} else {
			links = append(links, openLink)
		}
	}

	// Attach TCP tracking for API calls
	if c.config.TrackAPI {
		tcpLink, err := link.Kprobe("tcp_sendmsg", objs.TraceTcpSend)
		if err != nil {
			c.logger.Warn("Failed to attach TCP tracking", zap.Error(err))
			// Continue without API tracking
		} else {
			links = append(links, tcpLink)
		}
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		for _, l := range links {
			l.Close()
		}
		objs.Close()
		return fmt.Errorf("failed to create ringbuf reader: %w", err)
	}

	c.ebpfState = &ebpfComponents{
		objs:   objs,
		links:  links,
		reader: reader,
	}

	// Start reading events
	c.LifecycleManager.GoRoutine(func() {
		c.readEBPFEvents()
	})

	c.logger.Info("eBPF Helm monitoring started",
		zap.Int("attached_programs", len(links)),
		zap.Bool("track_files", c.config.TrackFiles),
		zap.Bool("track_api", c.config.TrackAPI),
	)

	return nil
}

// stopEBPF stops eBPF monitoring
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	components := c.ebpfState.(*ebpfComponents)

	// Close ring buffer reader
	if components.reader != nil {
		components.reader.Close()
	}

	// Detach programs
	for _, l := range components.links {
		l.Close()
	}

	// Close eBPF objects
	if components.objs != nil {
		components.objs.Close()
	}

	c.logger.Debug("eBPF Helm monitoring stopped")
}

// readEBPFEvents reads events from the eBPF ring buffer
func (c *Collector) readEBPFEvents() {
	components := c.ebpfState.(*ebpfComponents)
	if components == nil || components.reader == nil {
		return
	}

	c.logger.Debug("Starting eBPF event reader")

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			c.logger.Debug("Stopping eBPF event reader")
			return

		default:
			record, err := components.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					c.logger.Debug("Ring buffer closed")
					return
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the event
			if len(record.RawSample) < int(unsafe.Sizeof(helmmonitorHelmEvent{})) {
				c.logger.Warn("Received truncated event", zap.Int("size", len(record.RawSample)))
				continue
			}

			event := (*helmmonitorHelmEvent)(unsafe.Pointer(&record.RawSample[0]))
			c.processEBPFEvent(event)
		}
	}
}

// processEBPFEvent processes a single eBPF event
func (c *Collector) processEBPFEvent(event *helmmonitorHelmEvent) {
	switch event.EventType {
	case 1: // EVENT_PROCESS_EXEC
		c.handleProcessExec(event)
	case 2: // EVENT_PROCESS_EXIT
		c.handleProcessExit(event)
	case 3: // EVENT_FILE_OPEN
		c.handleFileOpen(event)
	case 4: // EVENT_TCP_SEND
		c.handleTCPSend(event)
	case 5: // EVENT_WRITE_OUTPUT
		c.handleWriteOutput(event)
	default:
		c.logger.Warn("Unknown event type", zap.Uint32("type", event.EventType))
	}
}

// handleProcessExec handles process execution events
func (c *Collector) handleProcessExec(event *helmmonitorHelmEvent) {
	// Extract process info
	comm := nullTerminatedString(event.Data[0][:16])
	filename := nullTerminatedString(event.Data[0][16:272])
	args := nullTerminatedString(event.Data[0][272:784])

	c.logger.Debug("Helm process started",
		zap.String("comm", comm),
		zap.String("filename", filename),
		zap.Uint32("pid", event.Pid),
	)

	// Create operation tracking
	op := &HelmOperation{
		ID:        fmt.Sprintf("helm-%d-%d", event.Pid, event.Timestamp),
		PID:       event.Pid,
		UID:       event.Uid,
		GID:       event.Gid,
		Command:   fmt.Sprintf("%s %s", filename, args),
		Binary:    comm,
		StartTime: time.Unix(0, int64(event.Timestamp)),
		Arguments: make(map[string]string),
		FilesRead: make([]FileAccess, 0),
		APICalls:  make([]APICall, 0),
	}

	// Parse command to extract action and release name
	c.parseHelmCommand(op, args)

	// Store operation
	c.operations.Store(event.Pid, op)

	// Update metrics
	c.RecordMetric("helm_operations_started", 1)
}

// handleProcessExit handles process exit events
func (c *Collector) handleProcessExit(event *helmmonitorHelmEvent) {
	// Get the tracked operation
	if opInterface, exists := c.operations.Load(event.Pid); exists {
		op := opInterface.(*HelmOperation)

		// Extract exit info from event data
		exitCode := *(*int32)(unsafe.Pointer(&event.Data[0][0]))
		signal := *(*int32)(unsafe.Pointer(&event.Data[0][4]))
		durationNs := *(*uint64)(unsafe.Pointer(&event.Data[0][8]))

		op.EndTime = time.Unix(0, int64(event.Timestamp))
		op.ExitCode = exitCode
		op.Signal = signal
		op.Duration = time.Duration(durationNs)

		c.logger.Info("Helm process exited",
			zap.String("command", op.Command),
			zap.Int32("exit_code", exitCode),
			zap.Duration("duration", op.Duration),
		)

		// Check if operation failed
		if exitCode != 0 {
			op.Failed = true
			c.logger.Warn("Helm operation failed",
				zap.String("operation_id", op.ID),
				zap.String("release", op.ReleaseName),
				zap.Int32("exit_code", exitCode),
			)

			// Trigger correlation
			c.correlateOperation(op)

			// Update metrics
			c.RecordMetric("helm_operations_failed", 1)
		} else {
			c.RecordMetric("helm_operations_succeeded", 1)
		}

		// Record operation duration
		c.RecordMetric("helm_operation_duration_seconds", float64(op.Duration.Seconds()))
	}
}

// handleFileOpen handles file open events
func (c *Collector) handleFileOpen(event *helmmonitorHelmEvent) {
	// Get the tracked operation
	if opInterface, exists := c.operations.Load(event.Pid); exists {
		op := opInterface.(*HelmOperation)

		// Extract file info
		path := nullTerminatedString(event.Data[0][:256])
		flags := *(*uint32)(unsafe.Pointer(&event.Data[0][256]))
		mode := *(*uint32)(unsafe.Pointer(&event.Data[0][260]))
		fileType := event.Data[0][268]

		fileAccess := FileAccess{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Path:      path,
			Size:      0, // Would need stat to get size
			FileType:  c.mapFileType(fileType),
		}

		op.FilesRead = append(op.FilesRead, fileAccess)

		// Track values files specially
		if strings.Contains(path, "values") && strings.HasSuffix(path, ".yaml") {
			op.ValuesFiles = append(op.ValuesFiles, path)
		}

		c.logger.Debug("Helm file access",
			zap.String("path", path),
			zap.String("type", fileAccess.FileType),
			zap.Uint32("flags", flags),
			zap.Uint32("mode", mode),
		)
	}
}

// handleTCPSend handles TCP send events (API calls)
func (c *Collector) handleTCPSend(event *helmmonitorHelmEvent) {
	// Get the tracked operation
	if opInterface, exists := c.operations.Load(event.Pid); exists {
		op := opInterface.(*HelmOperation)

		// Extract TCP info
		saddr := *(*uint32)(unsafe.Pointer(&event.Data[0][0]))
		daddr := *(*uint32)(unsafe.Pointer(&event.Data[0][4]))
		sport := *(*uint16)(unsafe.Pointer(&event.Data[0][8]))
		dport := *(*uint16)(unsafe.Pointer(&event.Data[0][10]))
		size := *(*uint32)(unsafe.Pointer(&event.Data[0][12]))

		// Only track API server connections (usually port 443 or 6443)
		if dport == 443 || dport == 6443 {
			apiCall := APICall{
				Timestamp: time.Unix(0, int64(event.Timestamp)),
				Method:    "UNKNOWN", // Would need to inspect payload
				Path:      fmt.Sprintf("%s:%d", ipToString(daddr), dport),
			}

			op.APICalls = append(op.APICalls, apiCall)

			c.logger.Debug("Helm API call",
				zap.String("dest", apiCall.Path),
				zap.Uint32("size", size),
			)
		}
	}
}

// handleWriteOutput handles write events (stdout/stderr)
func (c *Collector) handleWriteOutput(event *helmmonitorHelmEvent) {
	// Get the tracked operation
	if opInterface, exists := c.operations.Load(event.Pid); exists {
		op := opInterface.(*HelmOperation)

		fd := *(*uint32)(unsafe.Pointer(&event.Data[0][0]))
		count := *(*uint32)(unsafe.Pointer(&event.Data[0][4]))

		// Track output for error detection
		if fd == 2 { // stderr
			c.logger.Debug("Helm stderr output",
				zap.String("operation_id", op.ID),
				zap.Uint32("bytes", count),
			)
			// Could capture actual error text here if needed
		}
	}
}

// Helper functions

// parseHelmCommand parses a Helm command to extract details
func (c *Collector) parseHelmCommand(op *HelmOperation, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return
	}

	// First part after "helm" is usually the action
	if len(parts) > 0 {
		op.Action = parts[0]
	}

	// Extract release name and other args
	for i, part := range parts {
		switch part {
		case "install", "upgrade", "rollback", "uninstall", "delete":
			op.Action = part
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
				op.ReleaseName = parts[i+1]
			}
		case "-n", "--namespace":
			if i+1 < len(parts) {
				op.Namespace = parts[i+1]
				op.Arguments["namespace"] = parts[i+1]
			}
		case "--timeout":
			if i+1 < len(parts) {
				op.Arguments["timeout"] = parts[i+1]
			}
		case "--atomic":
			op.Arguments["atomic"] = "true"
		case "--force":
			op.Arguments["force"] = "true"
		case "--wait":
			op.Arguments["wait"] = "true"
		case "--no-hooks":
			op.Arguments["no-hooks"] = "true"
		}

		// Extract chart path
		if strings.HasPrefix(part, "./") || strings.HasPrefix(part, "/") {
			if strings.Contains(part, "chart") || strings.HasSuffix(part, ".tgz") {
				op.ChartPath = part
			}
		}
	}

	// Default namespace if not specified
	if op.Namespace == "" {
		op.Namespace = "default"
	}
}

// mapFileType maps numeric file type to string
func (c *Collector) mapFileType(fileType uint8) string {
	switch fileType {
	case 1:
		return "values"
	case 2:
		return "template"
	case 3:
		return "chart"
	default:
		return "other"
	}
}

// nullTerminatedString converts a null-terminated byte array to string
func nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// ipToString converts uint32 IP to string
func ipToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}
