//go:build linux
// +build linux

package systemd

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 systemdMonitor ./bpf_src/systemd_monitor.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfState struct {
	objs   *bpf.SystemdMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "systemd.ebpf.start")
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := bpf.SystemdMonitorObjects{}
	if err := bpf.LoadSystemdMonitorObjects(&objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &ebpfState{objs: &objs}

	// Attach tracepoints for systemd monitoring
	execLink, err := link.Tracepoint("syscalls", "sys_enter_execve", objs.TraceExec, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching execve tracepoint: %w", err)
	}

	exitLink, err := link.Tracepoint("syscalls", "sys_enter_exit", objs.TraceExit, nil)
	if err != nil {
		execLink.Close()
		objs.Close()
		return fmt.Errorf("attaching exit tracepoint: %w", err)
	}

	c.ebpfState.(*ebpfState).reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		execLink.Close()
		exitLink.Close()
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	c.ebpfState.(*ebpfState).links = []link.Link{execLink, exitLink}

	c.logger.Info("Systemd eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(c.ebpfState.(*ebpfState).links)),
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

	c.logger.Info("Systemd eBPF monitoring stopped", zap.String("collector", c.name))
}

// processEvents processes events from the ring buffer - simple version
func (c *Collector) processEvents() {
	ctx, span := c.tracer.Start(context.Background(), "systemd.collector.process_events")
	defer span.End()

	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	if state.reader == nil {
		return
	}

	c.logger.Info("Starting systemd event processing loop")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Event processing stopped due to context cancellation")
			return
		default:
		}

		record, err := state.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			c.logger.Debug("Failed to read from ring buffer", zap.Error(err))
			continue
		}

		// Parse event safely
		if len(record.RawSample) < int(unsafe.Sizeof(SystemdEvent{})) {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
			continue
		}

		var event SystemdEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
			c.logger.Debug("Failed to parse systemd event", zap.Error(err))
			continue
		}

		// Convert to RawEvent - NO BUSINESS LOGIC
		rawEvent := domain.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Source:    "systemd",
			Data:      record.RawSample, // Raw eBPF event data
		}

		// Track processed events
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1)
		}

		select {
		case c.events <- rawEvent:
			// Event sent successfully
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
		}
	}
}
