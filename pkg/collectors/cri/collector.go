package cri

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Collector - the lean, mean, CRI machine
type Collector struct {
	name   string
	socket string // CRI socket path
	client cri.RuntimeServiceClient
	conn   *grpc.ClientConn
	logger *zap.Logger

	// Performance critical components
	ringBuffer *RingBuffer // Lock-free ring buffer
	eventPool  *EventPool  // Reuse Event objects
	batch      []*Event    // Current batch
	batchMu    sync.Mutex  // Batch lock
	metrics    *Metrics    // Atomic counters

	// Channels
	events chan collectors.RawEvent
	stopCh chan struct{}

	// State tracking for efficient change detection
	lastSeen    map[string]*cri.ContainerStatus
	lastSeenMu  sync.RWMutex
	lastPoll    time.Time
	pollTicker  *time.Ticker
	batchTicker *time.Ticker

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// OTEL metric instruments
	eventsProcessed   metric.Int64Counter
	eventsDropped     metric.Int64Counter
	oomKillsDetected  metric.Int64Counter
	processingLatency metric.Float64Histogram
	bufferUsage       metric.Int64ObservableGauge
	activeContainers  metric.Int64UpDownCounter
	criErrors         metric.Int64Counter
	batchSize         metric.Int64Histogram
	checksPerformed   metric.Int64Counter

	// Status
	isRunning atomic.Bool
}

// NewCollector creates a new CRI collector - mega lean constructor
func NewCollector(name string, config Config) (*Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	logger = logger.Named(CollectorName)

	// Detect CRI socket
	socket := config.SocketPath
	if socket == "" {
		socket = detectCRISocket()
		if socket == "" {
			return nil, fmt.Errorf("no CRI socket found")
		}
	}

	// Initialize OTEL instrumentation with semantic conventions
	tracer := otel.Tracer("tapio.collectors.cri",
		trace.WithInstrumentationVersion("1.0.0"),
		trace.WithSchemaURL(semconv.SchemaURL),
	)

	meter := otel.Meter("tapio.collectors.cri",
		metric.WithInstrumentationVersion("1.0.0"),
		metric.WithSchemaURL(semconv.SchemaURL),
	)

	collector := &Collector{
		name:       name,
		socket:     socket,
		logger:     logger,
		ringBuffer: NewRingBuffer(),
		eventPool:  NewEventPool(),
		batch:      make([]*Event, 0, EventBatchSize),
		metrics:    &Metrics{},
		events:     make(chan collectors.RawEvent, config.EventBufferSize),
		stopCh:     make(chan struct{}),
		lastSeen:   make(map[string]*cri.ContainerStatus, 1000), // Pre-allocate
		tracer:     tracer,
		meter:      meter,
	}

	// Initialize OTEL metric instruments
	if err := collector.initializeMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return collector, nil
}

// detectCRISocket detects available CRI socket - fast detection
func detectCRISocket() string {
	// Common CRI socket paths in order of preference
	sockets := []string{
		"/run/containerd/containerd.sock",     // containerd
		"/var/run/crio/crio.sock",             // CRI-O
		"/run/k3s/containerd/containerd.sock", // k3s
		"/run/k0s/containerd.sock",            // k0s
	}

	for _, socket := range sockets {
		if _, err := os.Stat(socket); err == nil {
			return socket
		}
	}
	return ""
}

// Start begins CRI monitoring - optimized startup
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri.Start",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
			attribute.String("cri.socket", c.socket),
			attribute.String("cri.runtime", c.detectRuntime()),
			attribute.Int("buffer.size", RingBufferSize),
			attribute.Int("batch.size", EventBatchSize),
		),
	)
	defer span.End()

	if c.isRunning.Load() {
		err := fmt.Errorf("collector already running")
		span.RecordError(err)
		span.SetStatus(codes.Error, "already running")
		return err
	}

	c.logger.Info("Starting CRI collector",
		zap.String("socket", c.socket),
		zap.Int("buffer_size", RingBufferSize),
	)

	span.AddEvent("connecting_to_cri_socket")

	// Connect to CRI socket with optimized settings
	conn, err := grpc.DialContext(ctx, "unix://"+c.socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(4*1024*1024), // 4MB max
		),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to connect to CRI")
		if c.criErrors != nil {
			c.criErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("operation", "connect"),
					attribute.String("error", err.Error()),
				),
			)
		}
		return fmt.Errorf("failed to connect to CRI socket %s: %w", c.socket, err)
	}
	c.conn = conn
	c.client = cri.NewRuntimeServiceClient(conn)

	span.AddEvent("cri_connection_established",
		trace.WithAttributes(
			attribute.String("socket", c.socket),
		),
	)

	// Test connection
	versionCtx, versionSpan := c.tracer.Start(ctx, "cri.version_check")
	version, err := c.client.Version(versionCtx, &cri.VersionRequest{})
	if err != nil {
		versionSpan.RecordError(err)
		versionSpan.SetStatus(codes.Error, "version check failed")
		versionSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "CRI version check failed")
		c.conn.Close()
		if c.criErrors != nil {
			c.criErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("operation", "version_check"),
				),
			)
		}
		return fmt.Errorf("CRI version check failed: %w", err)
	}
	versionSpan.SetAttributes(
		attribute.String("runtime.name", version.RuntimeName),
		attribute.String("runtime.version", version.RuntimeVersion),
		attribute.String("runtime.api_version", version.RuntimeApiVersion),
	)
	versionSpan.SetStatus(codes.Ok, "version check successful")
	versionSpan.End()

	span.AddEvent("version_check_successful",
		trace.WithAttributes(
			attribute.String("runtime.name", version.RuntimeName),
			attribute.String("runtime.version", version.RuntimeVersion),
		),
	)

	// Initialize state with current containers
	span.AddEvent("initializing_container_state")
	if err := c.initializeState(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to initialize state")
		c.conn.Close()
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Record initial container count
	c.lastSeenMu.RLock()
	initialCount := len(c.lastSeen)
	c.lastSeenMu.RUnlock()

	if c.activeContainers != nil {
		c.activeContainers.Add(ctx, int64(initialCount),
			metric.WithAttributes(
				attribute.String("operation", "initial_load"),
			),
		)
	}

	span.AddEvent("container_state_initialized",
		trace.WithAttributes(
			attribute.Int("initial_container_count", initialCount),
		),
	)

	// Start background goroutines
	c.pollTicker = time.NewTicker(100 * time.Millisecond) // 10 Hz polling
	c.batchTicker = time.NewTicker(FlushInterval)

	c.isRunning.Store(true)

	span.AddEvent("starting_background_goroutines")

	// Start event streaming
	go c.streamEvents(ctx)

	// Start batch processing
	go c.processBatches(ctx)

	span.SetAttributes(
		attribute.Bool("collector.running", true),
		attribute.Int("initial_containers", initialCount),
	)

	span.SetStatus(codes.Ok, "collector started successfully")
	c.logger.Info("CRI collector started successfully")
	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	if !c.isRunning.Load() {
		return nil
	}

	c.logger.Info("Stopping CRI collector")

	// Signal stop
	close(c.stopCh)
	c.isRunning.Store(false)

	// Stop tickers
	if c.pollTicker != nil {
		c.pollTicker.Stop()
	}
	if c.batchTicker != nil {
		c.batchTicker.Stop()
	}

	// Close gRPC connection
	if c.conn != nil {
		c.conn.Close()
	}

	// Close events channel
	close(c.events)

	c.logger.Info("CRI collector stopped")
	return nil
}

// initializeState initializes the container state map
func (c *Collector) initializeState(ctx context.Context) error {
	resp, err := c.client.ListContainers(ctx, &cri.ListContainersRequest{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	c.lastSeenMu.Lock()
	defer c.lastSeenMu.Unlock()

	for _, container := range resp.Containers {
		statusResp, err := c.client.ContainerStatus(ctx,
			&cri.ContainerStatusRequest{ContainerId: container.Id})
		if err != nil {
			c.logger.Warn("Failed to get container status",
				zap.String("container_id", container.Id),
				zap.Error(err))
			continue
		}
		c.lastSeen[container.Id] = statusResp.Status
	}

	c.lastPoll = time.Now()
	c.logger.Info("Initialized state",
		zap.Int("containers", len(c.lastSeen)))
	return nil
}

// streamEvents monitors container changes - the core event loop
func (c *Collector) streamEvents(ctx context.Context) {
	defer c.logger.Info("Event streaming stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-c.pollTicker.C:
			c.checkContainerChanges(ctx)
		}
	}
}

// checkContainerChanges detects state changes efficiently
func (c *Collector) checkContainerChanges(ctx context.Context) {
	start := time.Now()

	// List current containers
	resp, err := c.client.ListContainers(ctx, &cri.ListContainersRequest{})
	if err != nil {
		c.metrics.CRIErrors.Add(1)
		c.logger.Error("Failed to list containers", zap.Error(err))
		return
	}

	currentContainers := make(map[string]*cri.Container)
	for _, container := range resp.Containers {
		currentContainers[container.Id] = container
	}

	c.lastSeenMu.Lock()

	// Check for new containers and state changes
	for _, container := range resp.Containers {
		lastStatus, exists := c.lastSeen[container.Id]

		// Get current status
		statusResp, err := c.client.ContainerStatus(ctx,
			&cri.ContainerStatusRequest{ContainerId: container.Id})
		if err != nil {
			c.logger.Warn("Failed to get container status",
				zap.String("container_id", container.Id),
				zap.Error(err))
			continue
		}

		currentStatus := statusResp.Status

		// Detect state changes
		if !exists {
			// New container - created event
			c.createEvent(currentStatus, EventCreated)
		} else if c.hasStateChanged(lastStatus, currentStatus) {
			// State changed - determine event type
			eventType := c.determineEventType(lastStatus, currentStatus)
			c.createEvent(currentStatus, eventType)
		}

		c.lastSeen[container.Id] = currentStatus
	}

	// Check for removed containers
	for containerID := range c.lastSeen {
		if _, exists := currentContainers[containerID]; !exists {
			delete(c.lastSeen, containerID)
		}
	}

	c.lastSeenMu.Unlock()

	// Update metrics
	processingTime := time.Since(start)
	c.metrics.ProcessingTimeNs.Store(uint64(processingTime.Nanoseconds()))
}

// hasStateChanged detects if container state changed
func (c *Collector) hasStateChanged(old, new *cri.ContainerStatus) bool {
	if old == nil || new == nil {
		return true
	}

	// Check critical state changes
	return old.State != new.State ||
		old.ExitCode != new.ExitCode ||
		old.FinishedAt != new.FinishedAt ||
		old.StartedAt != new.StartedAt
}

// determineEventType determines event type from state change
func (c *Collector) determineEventType(old, new *cri.ContainerStatus) EventType {
	// State transition matrix for event determination
	if old.State == cri.ContainerState_CONTAINER_CREATED &&
		new.State == cri.ContainerState_CONTAINER_RUNNING {
		return EventStarted
	}

	if new.State == cri.ContainerState_CONTAINER_EXITED {
		// Check for OOM kill - CRITICAL detection
		if new.ExitCode == 137 || strings.Contains(strings.ToLower(new.Reason), "oomkilled") {
			return EventOOM
		}
		if new.ExitCode != 0 {
			return EventDied
		}
		return EventStopped
	}

	return EventDied // Default for unexpected transitions
}

// createEvent creates and queues an event - optimized for speed
func (c *Collector) createEvent(status *cri.ContainerStatus, eventType EventType) {
	// Get event from pool - zero allocation
	event := c.eventPool.Get()

	// Fill critical data efficiently
	event.SetContainerID(status.Id)
	event.Type = eventType
	event.ExitCode = status.ExitCode
	event.Timestamp = time.Now().UnixNano()

	// Extract timing
	if status.StartedAt > 0 {
		event.StartedAt = status.StartedAt
	}
	if status.FinishedAt > 0 {
		event.FinishedAt = status.FinishedAt
	}

	// Extract Kubernetes metadata
	if status.Labels != nil {
		if podUID, ok := status.Labels["io.kubernetes.pod.uid"]; ok {
			event.SetPodUID(podUID)
		}
		if podName, ok := status.Labels["io.kubernetes.pod.name"]; ok {
			event.PodName = podName
		}
		if namespace, ok := status.Labels["io.kubernetes.pod.namespace"]; ok {
			event.Namespace = namespace
		}
	}

	// CRITICAL: Detect OOM kill
	if eventType == EventOOM {
		event.OOMKilled = 1
		event.Signal = 9 // SIGKILL
		c.metrics.OOMKillsDetected.Add(1)

		// Extract memory usage from annotations if available
		if status.Annotations != nil {
			if memUsage, ok := status.Annotations["memory.usage"]; ok {
				event.MemoryUsage = parseBytes(memUsage)
			}
			if memLimit, ok := status.Annotations["memory.limit"]; ok {
				event.MemoryLimit = parseBytes(memLimit)
			}
		}

		event.Reason = "OOMKilled"
		event.Message = fmt.Sprintf("Container killed due to OOM: exit code %d", status.ExitCode)
	}

	// Add to ring buffer (lock-free)
	if !c.ringBuffer.Write(event) {
		// Buffer full - drop event and return to pool
		c.eventPool.Put(event)
		c.metrics.EventsDropped.Add(1)
		return
	}

	c.metrics.EventsProcessed.Add(1)
}

// processBatches processes events in batches for efficiency
func (c *Collector) processBatches(ctx context.Context) {
	defer c.logger.Info("Batch processing stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-c.batchTicker.C:
			c.processBatch()
		}
	}
}

// processBatch processes current batch of events
func (c *Collector) processBatch() {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()

	// Drain ring buffer into batch
	for len(c.batch) < EventBatchSize {
		event := c.ringBuffer.Read()
		if event == nil {
			break // No more events
		}
		c.batch = append(c.batch, event)
	}

	if len(c.batch) == 0 {
		return // No events to process
	}

	// Process batch
	for _, event := range c.batch {
		// Convert to RawEvent for pipeline processing
		rawEvent := event.ToRawEvent()

		// Send to pipeline
		select {
		case c.events <- rawEvent:
			// Event sent successfully
		default:
			// Channel full - drop event
			c.metrics.EventsDropped.Add(1)
		}

		// Return event to pool
		c.eventPool.Put(event)
	}

	c.metrics.BatchesSent.Add(1)

	// Clear batch for reuse
	c.batch = c.batch[:0]
}

// Events returns the events channel for consumption
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns collector health status (required by collectors.Collector interface)
func (c *Collector) IsHealthy() bool {
	if !c.isRunning.Load() {
		return false
	}

	// Test CRI connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := c.client.Version(ctx, &cri.VersionRequest{}); err != nil {
		return false
	}

	return true
}

// Health returns detailed collector health status
func (c *Collector) Health() *domain.HealthStatus {
	if !c.isRunning.Load() {
		return domain.NewUnhealthyStatus("collector stopped", nil)
	}

	// Test CRI connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status := domain.NewHealthyStatus("CRI collector healthy")
	status.Component = c.name

	if _, err := c.client.Version(ctx, &cri.VersionRequest{}); err != nil {
		status = domain.NewUnhealthyStatus(fmt.Sprintf("CRI connection error: %v", err), err)
	}

	// Add detailed health information
	if status.Details == nil {
		status.Details = &domain.HealthDetails{}
	}
	status.Details.Labels = map[string]string{
		"socket":       c.socket,
		"buffer_usage": fmt.Sprintf("%.2f", c.ringBuffer.Usage()),
	}

	// Add metrics
	stats := c.metrics.GetStats()
	status.EventsEmitted = stats.EventsProcessed
	status.ErrorCount = stats.ErrorCount

	// Add container count
	c.lastSeenMu.RLock()
	status.Details.Labels["tracked_containers"] = fmt.Sprintf("%d", len(c.lastSeen))
	c.lastSeenMu.RUnlock()

	return status
}

// Statistics returns collector statistics
func (c *Collector) Statistics() *domain.CollectorStats {
	stats := c.metrics.GetStats()

	// Add runtime info to custom metrics
	if stats.CustomMetrics == nil {
		stats.CustomMetrics = make(map[string]string)
	}
	stats.CustomMetrics["socket"] = c.socket
	stats.CustomMetrics["running"] = fmt.Sprintf("%t", c.isRunning.Load())
	stats.CustomMetrics["buffer_usage"] = fmt.Sprintf("%.2f", c.ringBuffer.Usage())

	// Container count
	c.lastSeenMu.RLock()
	stats.CustomMetrics["tracked_containers"] = fmt.Sprintf("%d", len(c.lastSeen))
	c.lastSeenMu.RUnlock()

	return stats
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Type returns collector type
func (c *Collector) Type() string {
	return CollectorName
}

// initializeMetrics initializes all OTEL metric instruments
func (c *Collector) initializeMetrics() error {
	var err error

	// Counter for processed events
	c.eventsProcessed, err = c.meter.Int64Counter("cri.events.processed",
		metric.WithDescription("Total number of CRI events processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create events_processed counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Counter for dropped events
	c.eventsDropped, err = c.meter.Int64Counter("cri.events.dropped",
		metric.WithDescription("Number of CRI events dropped due to buffer overflow"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create events_dropped counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Counter for OOM kills detected
	c.oomKillsDetected, err = c.meter.Int64Counter("cri.oom_kills",
		metric.WithDescription("Number of OOM kills detected by CRI collector"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create oom_kills counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Histogram for processing latency
	c.processingLatency, err = c.meter.Float64Histogram("cri.processing.latency",
		metric.WithDescription("Latency of container state check operations"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		c.logger.Warn("Failed to create processing_latency histogram", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// UpDownCounter for active containers
	c.activeContainers, err = c.meter.Int64UpDownCounter("cri.containers.active",
		metric.WithDescription("Number of containers currently being monitored"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create active_containers counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Counter for CRI errors
	c.criErrors, err = c.meter.Int64Counter("cri.errors",
		metric.WithDescription("Number of CRI client errors encountered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create cri_errors counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Histogram for batch sizes
	c.batchSize, err = c.meter.Int64Histogram("cri.batch.size",
		metric.WithDescription("Size of event batches sent to pipeline"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create batch_size histogram", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Counter for container state checks
	c.checksPerformed, err = c.meter.Int64Counter("cri.checks.performed",
		metric.WithDescription("Number of container state checks performed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.logger.Warn("Failed to create checks_performed counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Observable gauge for buffer usage
	c.bufferUsage, err = c.meter.Int64ObservableGauge("cri.buffer.usage",
		metric.WithDescription("Ring buffer usage percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		c.logger.Warn("Failed to create buffer_usage gauge", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	// Register callback for buffer usage gauge
	if c.bufferUsage != nil {
		_, err = c.meter.RegisterCallback(c.recordBufferUsage, c.bufferUsage)
		if err != nil {
			c.logger.Warn("Failed to register buffer usage callback", zap.Error(err))
			// Continue without callback - graceful degradation
		}
	}

	return nil
}

// recordBufferUsage callback for async gauge metrics
func (c *Collector) recordBufferUsage(ctx context.Context, observer metric.Observer) error {
	if c.bufferUsage != nil {
		usage := int64(c.ringBuffer.Usage())
		observer.ObserveInt64(c.bufferUsage, usage,
			metric.WithAttributes(
				attribute.String("collector", c.name),
				attribute.String("socket", c.socket),
			),
		)
	}
	return nil
}

// detectRuntime detects the container runtime type from socket path
func (c *Collector) detectRuntime() string {
	socket := strings.ToLower(c.socket)
	if strings.Contains(socket, "containerd") {
		return "containerd"
	} else if strings.Contains(socket, "crio") {
		return "cri-o"
	} else if strings.Contains(socket, "dockershim") {
		return "docker"
	}
	return "unknown"
}

// ensure Collector implements collectors.Collector interface
var _ collectors.Collector = (*Collector)(nil)
