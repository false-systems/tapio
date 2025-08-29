//go:build linux
// +build linux

package resourcestarvation

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors/resource-starvation/bpf"
)

type LinuxCollector struct {
	*Collector

	// eBPF resources
	bpfObjs *bpf.StarvationmonitorObjects
	links   []link.Link
	reader  *ringbuf.Reader

	// Control channels
	stopCh chan struct{}
	doneCh chan struct{}
	wg     sync.WaitGroup
}

func NewLinuxCollector(config *Config, logger *zap.Logger) (*LinuxCollector, error) {
	baseCollector, err := NewCollector(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create base collector: %w", err)
	}

	if !bpf.IsSupported() {
		return nil, fmt.Errorf("eBPF not supported on this system")
	}

	return &LinuxCollector{
		Collector: baseCollector,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}, nil
}

func (c *LinuxCollector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "linux_collector.start")
	defer span.End()

	c.logger.Info("Starting Linux resource starvation collector with eBPF")

	// Load eBPF program
	if err := c.loadeBPFProgram(); err != nil {
		return fmt.Errorf("failed to load eBPF program: %w", err)
	}

	// Attach to kernel tracepoints
	if err := c.attachTracepoints(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to attach tracepoints: %w", err)
	}

	// Setup ring buffer reader
	if err := c.setupRingBuffer(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to setup ring buffer: %w", err)
	}

	// Start event processing goroutine
	c.wg.Add(1)
	go c.processEvents()

	c.logger.Info("Linux resource starvation collector started successfully")
	return nil
}

func (c *LinuxCollector) Stop() error {
	c.logger.Info("Stopping Linux resource starvation collector")

	// Signal stop
	close(c.stopCh)

	// Wait for goroutines to finish
	c.wg.Wait()

	// Cleanup resources
	c.cleanup()

	c.logger.Info("Linux resource starvation collector stopped")
	return nil
}

func (c *LinuxCollector) loadeBPFProgram() error {
	c.logger.Debug("Loading eBPF program")

	objs, err := bpf.LoadStarvationmonitor()
	if err != nil {
		return fmt.Errorf("failed to load starvation monitor: %w", err)
	}

	c.bpfObjs = objs
	c.logger.Debug("eBPF program loaded successfully")
	return nil
}

func (c *LinuxCollector) attachTracepoints() error {
	c.logger.Debug("Attaching to kernel tracepoints")

	// Attach to sched_stat_wait (scheduling delays)
	schedWaitLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "sched",
		Name:    "sched_stat_wait",
		Program: c.bpfObjs.TraceSchedWait,
	})
	if err != nil {
		return fmt.Errorf("failed to attach sched_stat_wait: %w", err)
	}
	c.links = append(c.links, schedWaitLink)

	// Attach to sched_stat_runtime (runtime tracking)
	runtimeLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "sched",
		Name:    "sched_stat_runtime",
		Program: c.bpfObjs.TraceThrottle,
	})
	if err != nil {
		return fmt.Errorf("failed to attach sched_stat_runtime: %w", err)
	}
	c.links = append(c.links, runtimeLink)

	// Attach to sched_migrate_task (CPU migrations)
	migrateLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "sched",
		Name:    "sched_migrate_task",
		Program: c.bpfObjs.TraceMigrate,
	})
	if err != nil {
		return fmt.Errorf("failed to attach sched_migrate_task: %w", err)
	}
	c.links = append(c.links, migrateLink)

	// Attach to sched_switch (context switches)
	switchLink, err := link.Tracepoint(link.TracepointOptions{
		Group:   "sched",
		Name:    "sched_switch",
		Program: c.bpfObjs.TraceSchedSwitch,
	})
	if err != nil {
		return fmt.Errorf("failed to attach sched_switch: %w", err)
	}
	c.links = append(c.links, switchLink)

	c.logger.Debug("All tracepoints attached successfully",
		zap.Int("attached_count", len(c.links)))
	return nil
}

func (c *LinuxCollector) setupRingBuffer() error {
	c.logger.Debug("Setting up ring buffer reader")

	reader, err := ringbuf.NewReader(c.bpfObjs.Events)
	if err != nil {
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	c.reader = reader
	c.logger.Debug("Ring buffer reader setup successfully")
	return nil
}

func (c *LinuxCollector) processEvents() {
	defer c.wg.Done()
	defer close(c.doneCh)

	c.logger.Debug("Starting event processing loop")

	eventCount := uint64(0)
	errorCount := uint64(0)

	for {
		select {
		case <-c.stopCh:
			c.logger.Debug("Event processing stopped",
				zap.Uint64("total_events", eventCount),
				zap.Uint64("total_errors", errorCount))
			return

		default:
			// Read event from ring buffer with timeout
			record, err := c.reader.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					c.logger.Debug("Ring buffer closed")
					return
				}
				errorCount++
				c.logger.Warn("Failed to read from ring buffer", zap.Error(err))

				// Record error metric
				if c.starvationEvents != nil {
					c.starvationEvents.Add(context.Background(), 1,
						metric.WithAttributes(attribute.String("status", "error")))
				}

				// Backoff on errors
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Parse the event
			if err := c.handleEvent(record.RawSample); err != nil {
				errorCount++
				c.logger.Warn("Failed to handle event", zap.Error(err))
				continue
			}

			eventCount++
		}
	}
}

func (c *LinuxCollector) handleEvent(rawData []byte) error {
	// Validate data size
	expectedSize := int(unsafe.Sizeof(StarvationEvent{}))
	if len(rawData) < expectedSize {
		return fmt.Errorf("event data too small: got %d, expected %d", len(rawData), expectedSize)
	}

	// Parse the raw event data
	event := (*StarvationEvent)(unsafe.Pointer(&rawData[0]))

	// Validate event fields
	if event.EventType == 0 || event.EventType > 10 {
		return fmt.Errorf("invalid event type: %d", event.EventType)
	}

	// Convert process names from C strings
	victimComm := make([]byte, 16)
	culpritComm := make([]byte, 16)
	copy(victimComm, event.VictimComm[:])
	copy(culpritComm, event.CulpritComm[:])

	// Create a proper Go event copy
	goEvent := &StarvationEvent{
		Timestamp:       event.Timestamp,
		EventType:       event.EventType,
		CPUCore:         event.CPUCore,
		VictimPID:       event.VictimPID,
		VictimTGID:      event.VictimTGID,
		WaitTimeNS:      event.WaitTimeNS,
		RunTimeNS:       event.RunTimeNS,
		CulpritPID:      event.CulpritPID,
		CulpritTGID:     event.CulpritTGID,
		CulpritRuntime:  event.CulpritRuntime,
		ThrottledNS:     event.ThrottledNS,
		NrPeriods:       event.NrPeriods,
		NrThrottled:     event.NrThrottled,
		VictimCgroupID:  event.VictimCgroupID,
		CulpritCgroupID: event.CulpritCgroupID,
		VictimPrio:      event.VictimPrio,
		CulpritPrio:     event.CulpritPrio,
		VictimPolicy:    event.VictimPolicy,
	}
	copy(goEvent.VictimComm[:], victimComm)
	copy(goEvent.CulpritComm[:], culpritComm)

	// Process the event
	ctx := context.Background()
	if err := c.ProcessEvent(ctx, goEvent); err != nil {
		return fmt.Errorf("failed to process event: %w", err)
	}

	// Log significant events
	if goEvent.WaitTimeNS > 500_000_000 { // > 500ms
		c.logger.Debug("Significant starvation event detected",
			zap.String("event_type", EventType(goEvent.EventType).String()),
			zap.Uint32("victim_pid", goEvent.VictimPID),
			zap.Float64("wait_time_ms", float64(goEvent.WaitTimeNS)/1_000_000),
			zap.String("victim_comm", string(victimComm)),
		)
	}

	return nil
}

func (c *LinuxCollector) cleanup() {
	c.logger.Debug("Cleaning up eBPF resources")

	// Close ring buffer reader
	if c.reader != nil {
		c.reader.Close()
		c.reader = nil
	}

	// Detach all links
	for i, l := range c.links {
		if err := l.Close(); err != nil {
			c.logger.Warn("Failed to close link", zap.Int("index", i), zap.Error(err))
		}
	}
	c.links = nil

	// Close eBPF objects
	if c.bpfObjs != nil {
		c.bpfObjs.Close()
		c.bpfObjs = nil
	}

	c.logger.Debug("eBPF resources cleaned up")
}

type CollectorStats struct {
	Platform      string `json:"platform"`
	EBPFEnabled   bool   `json:"ebpf_enabled"`
	LinksAttached int    `json:"links_attached"`
}

func (c *LinuxCollector) GetStats() CollectorStats {
	return CollectorStats{
		Platform:      "linux",
		EBPFEnabled:   c.bpfObjs != nil,
		LinksAttached: len(c.links),
	}
}
