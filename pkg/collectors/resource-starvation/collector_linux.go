//go:build linux
// +build linux

package resourcestarvation

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
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

// Default threshold values
const (
	DEFAULT_STARVATION_THRESHOLD_NS = 100000000 // 100ms
	DEFAULT_THROTTLE_THRESHOLD_NS   = 10000000  // 10ms
	DEFAULT_MIGRATION_THRESHOLD     = 10
)

type LinuxCollector struct {
	*Collector

	// eBPF resources
	bpfObjs *bpf.StarvationmonitorObjects
	links   []link.Link
	reader  *ringbuf.Reader

	// New: Maps for config, filters, dropped
	configMap      *ebpf.Map
	filterMap      *ebpf.Map
	droppedMap     *ebpf.Map
	stackTracesMap *ebpf.Map // For resolving stack IDs

	// Control channels
	stopCh chan struct{}
	doneCh chan struct{}
	wg     sync.WaitGroup

	// New: Metrics
	waitTimeHistogram metric.Float64Histogram
	droppedEvents     metric.Int64Counter

	// Error recovery
	errorCount       int
	lastError        error
	lastErrorTime    time.Time
	circuitBreakerOn bool
	recoveryAttempts int
	mu               sync.RWMutex
}

func NewLinuxCollector(config *Config, logger *zap.Logger) (*LinuxCollector, error) {
	baseCollector, err := NewCollector(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create base collector: %w", err)
	}

	if !bpf.IsSupported() {
		return nil, fmt.Errorf("eBPF not supported on this system")
	}

	// Assume meter from baseCollector or global
	meter := baseCollector.meter // Adjust as needed

	waitHist, err := meter.Float64Histogram("starvation.wait_time_ms", metric.WithDescription("Wait time distribution"))
	if err != nil {
		return nil, err
	}

	droppedCtr, err := meter.Int64Counter("starvation.dropped_events", metric.WithDescription("Dropped ring buffer events"))
	if err != nil {
		return nil, err
	}

	return &LinuxCollector{
		Collector:         baseCollector,
		stopCh:            make(chan struct{}),
		doneCh:            make(chan struct{}),
		waitTimeHistogram: waitHist,
		droppedEvents:     droppedCtr,
	}, nil
}

func (c *LinuxCollector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "linux_collector.start")
	defer span.End()

	// Validate configuration before starting
	if err := c.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	c.logger.Info("Starting Linux resource starvation collector with eBPF",
		zap.String("config", c.config.String()))

	if err := c.loadeBPFProgram(); err != nil {
		return fmt.Errorf("failed to load eBPF program: %w", err)
	}

	// New: Pin and access additional maps
	c.configMap = c.bpfObjs.ConfigMap
	c.filterMap = c.bpfObjs.FilterMap
	c.droppedMap = c.bpfObjs.DroppedEvents
	c.stackTracesMap = c.bpfObjs.StackTraces

	if err := c.attachTracepoints(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to attach tracepoints: %w", err)
	}

	if err := c.setupRingBuffer(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to setup ring buffer: %w", err)
	}

	// Initialize thresholds from configuration
	// Convert milliseconds to nanoseconds for kernel
	starvationNS := uint64(c.config.StarvationThresholdMS) * 1_000_000
	severeNS := uint64(c.config.SevereThresholdMS) * 1_000_000
	criticalNS := uint64(c.config.CriticalThresholdMS) * 1_000_000

	if err := c.updateThreshold(0, starvationNS); err != nil {
		c.logger.Warn("Failed to set starvation threshold", zap.Error(err))
	}
	if err := c.updateThreshold(1, DEFAULT_THROTTLE_THRESHOLD_NS); err != nil {
		c.logger.Warn("Failed to set throttle threshold", zap.Error(err))
	}
	if err := c.updateThreshold(2, severeNS); err != nil {
		c.logger.Warn("Failed to set severe threshold", zap.Error(err))
	}
	if err := c.updateThreshold(3, DEFAULT_MIGRATION_THRESHOLD); err != nil {
		c.logger.Warn("Failed to set migration threshold", zap.Error(err))
	}
	// Also store critical threshold for later use
	if err := c.updateThreshold(4, criticalNS); err != nil {
		c.logger.Warn("Failed to set critical threshold", zap.Error(err))
	}

	// Configure PID filtering if specified
	if c.config.FilterPIDs != nil && len(c.config.FilterPIDs) > 0 {
		// Add marker to enable filtering
		if err := c.addFilter(0); err != nil {
			c.logger.Warn("Failed to enable PID filtering", zap.Error(err))
		}
		// Add each PID to track
		for _, pid := range c.config.FilterPIDs {
			if err := c.addFilter(pid); err != nil {
				c.logger.Warn("Failed to add PID filter", zap.Uint32("pid", pid), zap.Error(err))
			}
		}
		c.logger.Info("PID filtering enabled", zap.Int("pids", len(c.config.FilterPIDs)))
	}

	c.wg.Add(1)
	go c.processEvents(ctx) // Propagate ctx

	c.logger.Info("Linux resource starvation collector started successfully",
		zap.Bool("safe_mode", c.config.IsSafeMode()))
	return nil
}

// New: Method to update thresholds
func (c *LinuxCollector) updateThreshold(key uint32, val uint64) error {
	return c.configMap.Update(key, val, ebpf.UpdateAny)
}

// New: Method to add filter
func (c *LinuxCollector) addFilter(pid uint32) error {
	var val uint8 = 1
	return c.filterMap.Update(pid, val, ebpf.UpdateAny)
}

func (c *LinuxCollector) Stop() error {
	c.logger.Info("Stopping Linux resource starvation collector")

	close(c.stopCh)
	c.wg.Wait()

	// New: Log dropped events
	var key uint32 = 0
	var count uint64
	if err := c.droppedMap.Lookup(key, &count); err == nil {
		c.logger.Info("Total dropped events", zap.Uint64("count", count))
	}

	c.cleanup()
	c.logger.Info("Linux resource starvation collector stopped")
	return nil
}

func (c *LinuxCollector) loadeBPFProgram() error {
	c.logger.Debug("Loading eBPF program")

	spec, err := bpf.LoadStarvationmonitor()
	if err != nil {
		return fmt.Errorf("failed to load starvation monitor spec: %w", err)
	}

	objs := &bpf.StarvationmonitorObjects{}
	if err := spec.LoadAndAssign(objs, nil); err != nil {
		return fmt.Errorf("failed to load starvation monitor objects: %w", err)

	}

	c.bpfObjs = objs
	c.logger.Debug("eBPF program loaded successfully")
	return nil
}

func (c *LinuxCollector) attachTracepoints() error {
	c.logger.Debug("Attaching eBPF tracepoints")

	// Attach sched_stat_wait tracepoint
	tpWait, err := link.Tracepoint("sched", "sched_stat_wait", c.bpfObjs.TraceSchedWait, nil)
	if err != nil {
		return fmt.Errorf("failed to attach sched_stat_wait: %w", err)
	}
	c.links = append(c.links, tpWait)

	// Attach sched_stat_runtime tracepoint
	tpRuntime, err := link.Tracepoint("sched", "sched_stat_runtime", c.bpfObjs.TraceThrottle, nil)
	if err != nil {
		return fmt.Errorf("failed to attach sched_stat_runtime: %w", err)
	}
	c.links = append(c.links, tpRuntime)

	// Attach sched_migrate_task tracepoint
	tpMigrate, err := link.Tracepoint("sched", "sched_migrate_task", c.bpfObjs.TraceMigrate, nil)
	if err != nil {
		return fmt.Errorf("failed to attach sched_migrate_task: %w", err)
	}
	c.links = append(c.links, tpMigrate)

	// Attach sched_switch tracepoint
	tpSwitch, err := link.Tracepoint("sched", "sched_switch", c.bpfObjs.TraceSchedSwitch, nil)
	if err != nil {
		return fmt.Errorf("failed to attach sched_switch: %w", err)
	}
	c.links = append(c.links, tpSwitch)

	c.logger.Debug("eBPF tracepoints attached successfully", zap.Int("count", len(c.links)))
	return nil
}

func (c *LinuxCollector) setupRingBuffer() error {
	c.logger.Debug("Setting up ring buffer")

	reader, err := ringbuf.NewReader(c.bpfObjs.Events)
	if err != nil {
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	c.reader = reader
	c.logger.Debug("Ring buffer set up successfully")
	return nil
}

func (c *LinuxCollector) processEvents(ctx context.Context) {
	defer c.wg.Done()
	defer close(c.doneCh)

	c.logger.Debug("Starting event processing loop")

	eventCount := uint64(0)
	errorCount := uint64(0)
	backoff := 100 * time.Millisecond
	maxBackoff := 1 * time.Second

	for {
		select {
		case <-c.stopCh:
			c.logger.Debug("Event processing stopped",
				zap.Uint64("total_events", eventCount),
				zap.Uint64("total_errors", errorCount))
			return
		default:
			// Check circuit breaker
			if c.isCircuitBreakerOpen() {
				if !c.tryRecovery(ctx) {
					time.Sleep(c.config.ErrorBackoffDuration)
					continue
				}
			}

			// Read events one by one (batch read not available in older versions)
			record, err := c.reader.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					return
				}
				errorCount++
				c.handleError(err)
				c.logger.Warn("Failed to read batch from ring buffer", zap.Error(err))
				c.starvationEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "error")))

				// Exponential backoff
				time.Sleep(backoff)
				backoff = min(maxBackoff, backoff*2)
				continue
			}
			backoff = 100 * time.Millisecond // Reset on success
			c.resetErrorCount()              // Reset error count on successful read

			if err := c.handleEvent(ctx, record.RawSample); err != nil {
				errorCount++
				c.logger.Warn("Failed to handle event", zap.Error(err))
			} else {
				eventCount++
			}

			// New: Check dropped events periodically
			if eventCount%100 == 0 {
				var key uint32 = 0
				var count uint64
				if err := c.droppedMap.Lookup(key, &count); err == nil && count > 0 {
					c.droppedEvents.Add(ctx, int64(count))
					c.logger.Warn("Detected dropped events", zap.Uint64("count", count))
				}
			}
		}
	}
}

func (c *LinuxCollector) handleEvent(ctx context.Context, rawData []byte) error {
	expectedSize := int(unsafe.Sizeof(StarvationEvent{}))
	if len(rawData) < expectedSize {
		return fmt.Errorf("event data too small: got %d, expected %d", len(rawData), expectedSize)
	}

	// New: Safe parsing with binary.Read
	var event StarvationEvent
	buf := bytes.NewReader(rawData)
	if err := binary.Read(buf, binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// New: Tight validation
	if event.EventType < 1 || event.EventType > 5 {
		return fmt.Errorf("invalid event type: %d", event.EventType)
	}

	// New: Trim strings
	victimComm := strings.TrimRight(string(event.VictimComm[:]), "\x00")
	culpritComm := strings.TrimRight(string(event.CulpritComm[:]), "\x00")

	// Resolve cgroup paths (simple; improve with lib)
	victimCgroupPath := resolveCgroupPath(event.VictimCgroupID)
	_ = resolveCgroupPath(event.CulpritCgroupID) // culpritCgroupPath - unused for now

	// New: Resolve stack trace if stack_id >=0
	var stackTrace string
	if event.StackID >= 0 {
		// Resolve using stackTracesMap (implement resolution logic)
		// For example, iterate symbols; placeholder
		stackTrace = fmt.Sprintf("Stack trace ID: %d (resolution not implemented)", event.StackID)
	}

	// Process event (assume base handles it)
	if err := c.ProcessEvent(ctx, &event); err != nil {
		return fmt.Errorf("failed to process event: %w", err)
	}

	// New: Record histogram
	waitMs := float64(event.WaitTimeNS) / 1e6
	c.waitTimeHistogram.Record(ctx, waitMs,
		metric.WithAttributes(
			attribute.String("event_type", EventType(event.EventType).String()),
			attribute.String("victim_cgroup", victimCgroupPath),
		))

	// Log significant events
	if event.WaitTimeNS > 500_000_000 {
		c.logger.Debug("Significant starvation event detected",
			zap.String("event_type", EventType(event.EventType).String()),
			zap.Uint32("victim_pid", event.VictimPID),
			zap.Float64("wait_time_ms", waitMs),
			zap.String("victim_comm", victimComm),
			zap.String("victim_cgroup", victimCgroupPath),
			zap.String("stack_trace", stackTrace),
		)
	}

	// Update metrics
	c.starvationEvents.Add(ctx, 1, metric.WithAttributes(
		attribute.String("event_type", EventType(event.EventType).String()),
		attribute.String("status", "success"),
	))

	// Emit formatted event if debug enabled
	if c.config.DebugMode {
		c.logger.Info("Starvation event",
			zap.String("type", EventType(event.EventType).String()),
			zap.Uint32("victim_pid", event.VictimPID),
			zap.String("victim_comm", victimComm),
			zap.Uint32("culprit_pid", event.CulpritPID),
			zap.String("culprit_comm", culpritComm),
			zap.Float64("wait_time_ms", waitMs),
			zap.Uint32("cpu", event.CPUCore),
		)
	}

	return nil
}

// New: Simple cgroup resolver (read from /sys/fs/cgroup)
func resolveCgroupPath(id uint64) string {
	if id == 0 {
		return "/"
	}
	// Scan /sys/fs/cgroup for matching id (simplified; use lib for prod)
	// This is a placeholder implementation
	// In production, you'd want to use a proper cgroup library
	// or maintain a cache of cgroup ID to path mappings
	path := fmt.Sprintf("/sys/fs/cgroup/unified/%d", id)
	if data, err := ioutil.ReadFile(filepath.Join(path, "cgroup.controllers")); err == nil {
		// Parse the cgroup path from the file
		return strings.TrimSpace(string(data))
	}
	return fmt.Sprintf("cgroup:%d", id)
}

func (c *LinuxCollector) cleanup() {
	c.logger.Debug("Cleaning up eBPF resources")

	// Close ring buffer reader
	if c.reader != nil {
		c.reader.Close()
		c.reader = nil
	}

	// Detach links
	for _, l := range c.links {
		l.Close()
	}
	c.links = nil

	// Close eBPF objects
	if c.bpfObjs != nil {
		c.bpfObjs.Close()
		c.bpfObjs = nil
	}

	c.logger.Debug("eBPF resources cleaned up")
}

// Error recovery methods
func (c *LinuxCollector) handleError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.errorCount++
	c.lastError = err
	c.lastErrorTime = time.Now()

	if c.config.CircuitBreakerEnabled && c.errorCount >= c.config.CircuitBreakerThreshold {
		c.circuitBreakerOn = true
		c.logger.Error("Circuit breaker activated",
			zap.Int("error_count", c.errorCount),
			zap.Error(err))
		c.SetHealthy(false)
	}
}

func (c *LinuxCollector) resetErrorCount() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.errorCount > 0 {
		c.logger.Info("Errors cleared after successful operation",
			zap.Int("previous_error_count", c.errorCount))
	}
	c.errorCount = 0
	c.lastError = nil
	c.circuitBreakerOn = false
	c.recoveryAttempts = 0
	c.SetHealthy(true)
}

func (c *LinuxCollector) isCircuitBreakerOpen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.circuitBreakerOn
}

func (c *LinuxCollector) tryRecovery(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.recoveryAttempts++

	// Check if enough time has passed since last error
	if time.Since(c.lastErrorTime) < c.config.ErrorBackoffDuration {
		return false
	}

	c.logger.Info("Attempting recovery",
		zap.Int("attempt", c.recoveryAttempts))

	// Try a simple health check - check if we can read from maps
	var key uint32 = 0
	var count uint64
	if err := c.droppedMap.Lookup(key, &count); err != nil {
		c.logger.Warn("Recovery health check failed", zap.Error(err))
		return false
	}

	// If we got here, the eBPF subsystem seems responsive
	c.logger.Info("Recovery successful, resetting circuit breaker")
	c.errorCount = 0
	c.circuitBreakerOn = false
	c.recoveryAttempts = 0
	c.SetHealthy(true)
	return true
}

// Reload attempts to reload the eBPF program after failures
func (c *LinuxCollector) Reload(ctx context.Context) error {
	c.logger.Info("Attempting to reload eBPF program")

	// Clean up existing resources
	c.cleanup()

	// Give kernel time to clean up
	time.Sleep(100 * time.Millisecond)

	// Try to reload
	if err := c.loadeBPFProgram(); err != nil {
		return fmt.Errorf("failed to reload eBPF program: %w", err)
	}

	if err := c.attachTracepoints(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to reattach tracepoints: %w", err)
	}

	if err := c.setupRingBuffer(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to setup ring buffer on reload: %w", err)
	}

	c.resetErrorCount()
	c.logger.Info("eBPF program reloaded successfully")
	return nil
}

type CollectorStats struct {
	Platform      string `json:"platform"`
	EBPFEnabled   bool   `json:"ebpf_enabled"`
	LinksAttached int    `json:"links_attached"`
	DroppedEvents uint64 `json:"dropped_events"`
}

func (c *LinuxCollector) GetStats() CollectorStats {
	stats := CollectorStats{
		Platform:      "linux",
		EBPFEnabled:   c.bpfObjs != nil,
		LinksAttached: len(c.links),
	}

	var key uint32 = 0
	c.droppedMap.Lookup(key, &stats.DroppedEvents)
	return stats
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
