//go:build linux
// +build linux

package storageio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/storage-io/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// CO-RE eBPF implementation
type coreEBPF struct {
	collection *ebpf.Collection
	links      []link.Link
	reader     *ringbuf.Reader

	// Metrics
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	slowIOCounter   metric.Int64Counter
	ioErrors        metric.Int64Counter

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// startEBPF loads and attaches CO-RE eBPF programs
func (o *Observer) startEBPF() error {
	o.logger.Info("Loading CO-RE eBPF programs for storage I/O observer")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF spec
	spec, err := bpf.LoadStorage()
	if err != nil {
		return fmt.Errorf("loading BPF spec: %w", err)
	}

	// Verify BTF is available
	if spec.Types == nil {
		return fmt.Errorf("BTF information not available - CO-RE requires BTF-enabled kernel")
	}

	// Load collection with CO-RE options
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInfo,
			LogSize:  64 * 1024 * 1024, // 64MB for verifier logs
		},
	}

	coll, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()),
				zap.String("log", ve.Log))
			return fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return fmt.Errorf("loading BPF collection: %w", err)
	}

	o.ebpfState = &coreEBPF{
		collection:      coll,
		links:           make([]link.Link, 0),
		eventsProcessed: o.eventsProcessed,
		eventsDropped:   o.errorsTotal,
		slowIOCounter:   o.slowIOOperations,
		ioErrors:        o.errorsTotal,
		logger:          o.logger,
	}

	// Configure thresholds
	if err := o.configureEBPF(); err != nil {
		coll.Close()
		return fmt.Errorf("configuring eBPF: %w", err)
	}

	// Attach kprobes
	if err := o.attachCoreProbes(); err != nil {
		coll.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(coll.Maps["storage_events"])
	if err != nil {
		o.closeCoreEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := o.ebpfState.(*coreEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(1)
	go o.processCoreEvents(ctx)

	// Start metrics collector
	ebpfState.wg.Add(1)
	go o.collectCoreMetrics(ctx)

	o.logger.Info("CO-RE eBPF programs loaded successfully")
	return nil
}

// Configure eBPF maps with thresholds
func (o *Observer) configureEBPF() error {
	ebpfState := o.ebpfState.(*coreEBPF)

	// Set slow I/O threshold
	configMap := ebpfState.collection.Maps["config"]
	if configMap == nil {
		return fmt.Errorf("config map not found")
	}

	// CONFIG_SLOW_THRESHOLD_NS = 0
	slowThresholdNs := uint64(o.config.SlowIOThresholdMs) * 1_000_000
	key := uint32(0)
	if err := configMap.Update(key, slowThresholdNs, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("updating slow threshold: %w", err)
	}

	// CONFIG_RATE_LIMIT_NS = 1
	key = uint32(1)
	if err := configMap.Update(key, o.config.RateLimitNs, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("updating rate limit: %w", err)
	}

	return nil
}

// Attach CO-RE kprobes
func (o *Observer) attachCoreProbes() error {
	ebpfState := o.ebpfState.(*coreEBPF)

	// Define required probes
	requiredProbes := []struct {
		progName string
		funcName string
		isReturn bool
	}{
		{"trace_vfs_read", "vfs_read", false},
		{"trace_vfs_read_ret", "vfs_read", true},
		{"trace_vfs_write", "vfs_write", false},
		{"trace_vfs_write_ret", "vfs_write", true},
		{"trace_vfs_fsync", "vfs_fsync", false},
		{"trace_vfs_fsync_ret", "vfs_fsync", true},
	}

	// Attach required probes
	for _, probe := range requiredProbes {
		if err := o.attachProbe(ebpfState, probe.progName, probe.funcName, probe.isReturn, true); err != nil {
			return err
		}
	}

	// Define optional probes (best-effort attachment)
	optionalProbes := []struct {
		progName string
		funcName string
		isReturn bool
	}{
		{"trace_blk_mq_start_request", "blk_mq_start_request", false},
		{"trace_blk_account_io_done", "blk_account_io_done", false},
		{"trace_io_submit", "io_submit", false},
		{"trace_io_submit_ret", "io_submit", true},
		{"trace_io_getevents", "io_getevents", false},
		{"trace_io_getevents_ret", "io_getevents", true},
	}

	// Attach optional probes (don't fail if they're not available)
	for _, probe := range optionalProbes {
		_ = o.attachProbe(ebpfState, probe.progName, probe.funcName, probe.isReturn, false)
	}

	o.logger.Debug("Attached CO-RE kprobes",
		zap.Int("count", len(ebpfState.links)))

	return nil
}

// attachProbe is a helper to attach a single probe
func (o *Observer) attachProbe(ebpfState *coreEBPF, progName, funcName string, isReturn, required bool) error {
	prog := ebpfState.collection.Programs[progName]
	if prog == nil {
		if required {
			return fmt.Errorf("%s program not found", progName)
		}
		o.logger.Debug("Optional program not found", zap.String("program", progName))
		return nil
	}

	var l link.Link
	var err error
	if isReturn {
		l, err = link.Kretprobe(funcName, prog, nil)
	} else {
		l, err = link.Kprobe(funcName, prog, nil)
	}
	if err != nil {
		if required {
			probeType := "kprobe"
			if isReturn {
				probeType = "kretprobe"
			}
			return fmt.Errorf("attaching %s %s: %w", funcName, probeType, err)
		}
		o.logger.Warn("Failed to attach optional probe",
			zap.String("function", funcName),
			zap.Bool("isReturn", isReturn),
			zap.Error(err))
		return nil
	}

	ebpfState.links = append(ebpfState.links, l)
	return nil
}

// Process events from ring buffer
func (o *Observer) processCoreEvents(ctx context.Context) {
	defer o.ebpfState.(*coreEBPF).wg.Done()

	ebpfState := o.ebpfState.(*coreEBPF)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := ebpfState.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			o.logger.Warn("Error reading from ringbuf",
				zap.Error(err))
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(StorageEvent{})) {
			o.logger.Warn("Invalid event size",
				zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*StorageEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Convert to domain event
		domainEvent := o.convertCoreToDomainEvent(event)

		// Send to channel
		select {
		case o.EventChannelManager.GetChannel() <- domainEvent:
			o.BaseObserver.RecordEvent()
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("type", "storage"),
						attribute.String("operation", event.EventType.String())))
			}

			// Track slow I/O
			if event.IsSlow(float64(o.config.SlowIOThresholdMs)) {
				if o.slowIOOperations != nil {
					o.slowIOOperations.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("operation", event.EventType.String())))
				}
			}

			// Track errors
			if event.IsError() {
				if o.errorsTotal != nil {
					o.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("operation", event.EventType.String())))
				}
			}
		default:
			o.BaseObserver.RecordDrop()
			if ebpfState.eventsDropped != nil {
				ebpfState.eventsDropped.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("reason", "channel_full")))
			}
		}
	}
}

// Collect metrics from BPF maps
func (o *Observer) collectCoreMetrics(ctx context.Context) {
	defer o.ebpfState.(*coreEBPF).wg.Done()

	ebpfState := o.ebpfState.(*coreEBPF)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.readCoreStats()
		}
	}
}

// Read statistics from BPF maps
func (o *Observer) readCoreStats() {
	ebpfState := o.ebpfState.(*coreEBPF)

	statsMap := ebpfState.collection.Maps["stats"]
	if statsMap == nil {
		return
	}

	var stats IOStats
	key := uint32(0)

	// Read per-CPU stats and aggregate
	if err := statsMap.Lookup(key, &stats); err == nil {
		o.logger.Debug("Storage I/O stats",
			zap.Uint64("reads", stats.TotalReads),
			zap.Uint64("writes", stats.TotalWrites),
			zap.Uint64("fsyncs", stats.TotalFsyncs),
			zap.Uint64("slow_ios", stats.SlowIOs),
			zap.Uint64("errors", stats.Errors),
			zap.Uint64("dropped", stats.EventsDropped))
	}
}

// Convert BPF event to domain event
func (o *Observer) convertCoreToDomainEvent(event *StorageEvent) *domain.CollectorEvent {
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Enrich with Kubernetes information
	o.EnrichEventWithK8sInfo(event)

	// Determine severity
	severity := o.determineSeverity(event)

	// Build custom fields
	customFields := o.buildCustomFields(event)

	// Create domain event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("storage-%d-%d", event.PID, event.Timestamp),
		Timestamp: timestamp,
		Type:      domain.EventTypeStorageIO,
		Source:    o.name,
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Storage: &domain.StorageData{
				Operation: event.EventType.String(),
				Path:      event.GetFullPath(),
				Size:      int64(event.Size),
				LatencyMs: event.GetLatencyMs(),
				ErrorCode: int(event.ErrorCode),
				Inode:     event.Inode,
				Offset:    int64(event.Offset),
			},
			Process: &domain.ProcessData{
				PID:      int32(event.PID),
				TID:      int32(event.TID),
				UID:      int32(event.UID),
				GID:      int32(event.GID),
				Command:  event.GetComm(),
				CgroupID: event.CgroupID,
			},
			Custom: customFields,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "storage-io",
				"core":     "true",
				"version":  "1.0",
			},
		},
	}
}

// determineSeverity calculates event severity based on latency and errors
func (o *Observer) determineSeverity(event *StorageEvent) domain.EventSeverity {
	if event.IsError() {
		return domain.EventSeverityError
	}
	if event.IsSlow(float64(o.config.BlockingIOThresholdMs)) {
		return domain.EventSeverityWarning
	}
	if event.IsSlow(float64(o.config.SlowIOThresholdMs)) {
		return domain.EventSeverityNotice
	}
	return domain.EventSeverityInfo
}

// buildCustomFields creates custom field map for the event
func (o *Observer) buildCustomFields(event *StorageEvent) map[string]string {
	customFields := map[string]string{
		"flags":       fmt.Sprintf("0x%x", event.Flags),
		"file_size":   fmt.Sprintf("%d", event.FileSize),
		"latency_ns":  fmt.Sprintf("%d", event.LatencyNs),
		"is_slow":     fmt.Sprintf("%v", event.IsSlow(float64(o.config.SlowIOThresholdMs))),
		"is_blocking": fmt.Sprintf("%v", event.IsSlow(float64(o.config.BlockingIOThresholdMs))),
	}

	// Add block layer information if available
	if event.EventType == StorageEventBlockIO {
		customFields["device_major"] = fmt.Sprintf("%d", event.Major)
		customFields["device_minor"] = fmt.Sprintf("%d", event.Minor)
		customFields["sector"] = fmt.Sprintf("%d", event.Sector)
		customFields["queue_depth"] = fmt.Sprintf("%d", event.QueueDepth)
		customFields["bio_flags"] = fmt.Sprintf("0x%x", event.BioFlags)
	}

	// Add async I/O information if available
	if event.EventType == StorageEventAIOSubmit || event.EventType == StorageEventAIOComplete {
		customFields["aio_ctx_id"] = fmt.Sprintf("%d", event.AIOCtxID)
		customFields["aio_nr_events"] = fmt.Sprintf("%d", event.AIONrEvents)
		customFields["aio_flags"] = fmt.Sprintf("0x%x", event.AIOFlags)
	}

	// Add K8s enrichment
	o.addK8sFieldsToCustom(event, customFields)

	return customFields
}

// addK8sFieldsToCustom adds Kubernetes-related fields to custom fields
func (o *Observer) addK8sFieldsToCustom(event *StorageEvent, customFields map[string]string) {
	// Add container information
	o.containerCacheMu.RLock()
	if containerInfo, exists := o.containerCache[event.CgroupID]; exists && containerInfo != nil {
		if containerInfo.PodName != "" {
			customFields["k8s_pod"] = containerInfo.PodName
		}
		if containerInfo.Namespace != "" {
			customFields["k8s_namespace"] = containerInfo.Namespace
		}
		if containerInfo.ContainerID != "" {
			customFields["container_id"] = containerInfo.ContainerID
		}
	}
	o.containerCacheMu.RUnlock()

	// Add PVC information if available
	eventPath := event.GetFullPath()
	o.mountCacheMu.RLock()
	for mountPath, mountInfo := range o.mountCache {
		if strings.HasPrefix(eventPath, mountPath) && mountInfo.PVCName != "" {
			customFields["k8s_pvc"] = mountInfo.PVCName
			customFields["k8s_storage_class"] = mountInfo.StorageClass
			customFields["volume_type"] = mountInfo.VolumeType
			break
		}
	}
	o.mountCacheMu.RUnlock()
}

// Close CO-RE eBPF
func (o *Observer) stopEBPF() {
	if o.ebpfState == nil {
		return
	}

	ebpfState := o.ebpfState.(*coreEBPF)

	// Cancel context
	if ebpfState.cancel != nil {
		ebpfState.cancel()
	}

	// Close reader
	if ebpfState.reader != nil {
		ebpfState.reader.Close()
	}

	// Wait for goroutines
	ebpfState.wg.Wait()

	// Detach probes
	for _, l := range ebpfState.links {
		l.Close()
	}

	// Close collection
	if ebpfState.collection != nil {
		ebpfState.collection.Close()
	}

	o.logger.Info("CO-RE eBPF programs closed")
}

// closeCoreEBPF is a helper for cleanup during initialization
func (o *Observer) closeCoreEBPF() {
	if o.ebpfState != nil {
		o.stopEBPF()
	}
}

// readEBPFEvents is called by the observer lifecycle - delegates to processCoreEvents
func (o *Observer) readEBPFEvents() {
	// This is handled by processCoreEvents goroutine started in startEBPF
	// Keep this method for compatibility with observer interface
}
