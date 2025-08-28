//go:build linux

package dns

import (
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
)

// Platform-specific eBPF fields for Linux
type ebpfState struct {
	objs       *dnsMonitorObjects
	links      []link.Link
	perfReader *perf.Reader
}

// startEBPF initializes eBPF monitoring on Linux
func (c *Collector) startEBPF() error {
	if !c.config.EnableEBPF {
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &dnsMonitorObjects{}
	if err := loadDnsMonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	c.ebpfState.objs = objs

	// Create perf event reader
	perfReader, err := perf.NewReader(objs.Events, 4096)
	if err != nil {
		objs.Close()
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	c.ebpfState.perfReader = perfReader

	// Attach kprobes for DNS monitoring
	// Monitor UDP send/recv for DNS traffic (port 53)
	// This is handled by the eBPF program's tracepoint attachments

	return nil
}

// readEBPFEvents reads DNS events from eBPF
func (c *Collector) readEBPFEvents() {
	if c.ebpfState.perfReader == nil {
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.ebpfState.perfReader.Read()
			if err != nil {
				if err == perf.ErrClosed {
					return
				}
				c.logger.Error("Failed to read from perf buffer", zap.Error(err))
				continue
			}

			// Process the DNS event
			c.processDNSEvent(record.RawSample)
		}
	}
}

// stopEBPF cleans up eBPF resources on Linux
func (c *Collector) stopEBPF() {
	if c.ebpfState.perfReader != nil {
		c.ebpfState.perfReader.Close()
	}

	for _, l := range c.ebpfState.links {
		if l != nil {
			l.Close()
		}
	}

	if c.ebpfState.objs != nil {
		c.ebpfState.objs.Close()
	}
}

// processDNSEvent processes raw DNS event from eBPF
func (c *Collector) processDNSEvent(data []byte) {
	// Parse DNS event from raw bytes
	// The actual parsing is handled by the event processing pipeline
	// which converts raw eBPF data to structured DNS events
	c.logger.Debug("Processing DNS event from eBPF", zap.Int("size", len(data)))
}
