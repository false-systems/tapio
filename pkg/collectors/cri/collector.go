package cri

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Collector implements streaming CRI monitoring
type Collector struct {
	name    string
	logger  *zap.Logger
	tracer  trace.Tracer
	events  chan *domain.CollectorEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	stopped bool // Track if already stopped
	config  *Config
	mu      sync.RWMutex

	// CRI components
	socket string
	client cri.RuntimeServiceClient
	conn   *grpc.ClientConn

	// Streaming components
	streamClient  cri.RuntimeService_GetContainerEventsClient
	containerInfo map[string]*ContainerInfo // Rich metadata cache
	infoMu        sync.RWMutex
	reconnectChan chan struct{}

	// Essential OTEL Metrics (5 core metrics)
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge
}

// NewCollector creates a new simple CRI collector
func NewCollector(name string, cfg *Config) (*Collector, error) {
	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Detect CRI socket if not provided
	socket := cfg.SocketPath
	if socket == "" {
		socket = detectCRISocket()
		if socket == "" {
			return nil, fmt.Errorf("no CRI socket found")
		}
	}

	// Initialize minimal OTEL components
	tracer := otel.Tracer("cri-collector")
	meter := otel.Meter("cri-collector")

	// Only essential metrics - using CORRECT names
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
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

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	c := &Collector{
		name:            name,
		logger:          logger.Named(name),
		tracer:          tracer,
		config:          cfg,
		socket:          socket,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		containerInfo:   make(map[string]*ContainerInfo),
		reconnectChan:   make(chan struct{}, 1),
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
	}

	c.logger.Info("CRI collector created",
		zap.String("name", name),
		zap.String("socket", socket),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.String("mode", "streaming"),
	)

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the CRI monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Connect to CRI runtime with context deadline instead of deprecated WithTimeout
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	conn, err := grpc.DialContext(dialCtx, fmt.Sprintf("unix://%s", c.socket),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		span.SetAttributes(attribute.String("error", "cri_connection_failed"))
		span.RecordError(err)
		if c.cancel != nil {
			c.cancel()
		}
		return fmt.Errorf("failed to connect to CRI socket %s: %w", c.socket, err)
	}
	c.conn = conn
	c.client = cri.NewRuntimeServiceClient(conn)

	// Load initial container state
	if err := c.loadInitialState(ctx); err != nil {
		c.logger.Warn("Failed to load initial container state", zap.Error(err))
	}

	// Start streaming event monitor
	go c.streamMonitor()

	// Start metadata enricher
	go c.metadataEnricher()

	// Start health checker
	go c.healthChecker()

	// Use mutex for thread-safe health status update
	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()
	c.logger.Info("CRI collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping CRI collector")

	c.mu.Lock()
	defer c.mu.Unlock()

	// Only stop once
	if c.stopped {
		return nil
	}
	c.stopped = true

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	// Only close channel if not nil
	if c.events != nil {
		close(c.events)
	}

	c.healthy = false

	c.logger.Info("CRI collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// streamMonitor handles streaming events from CRI
func (c *Collector) streamMonitor() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Start streaming
		if err := c.startEventStream(); err != nil {
			c.logger.Error("Event stream error", zap.Error(err))

			// Update health status
			c.updateHealthStatus(false)

			// Trigger reconnection
			select {
			case c.reconnectChan <- struct{}{}:
			default:
			}

			// Backoff before retry
			time.Sleep(5 * time.Second)
		}
	}
}

// startEventStream starts receiving events from CRI streaming API
func (c *Collector) startEventStream() error {
	ctx, span := c.tracer.Start(c.ctx, "cri.streaming.event_stream")
	defer span.End()

	// Create event request
	request := &cri.GetEventsRequest{}

	stream, err := c.client.GetContainerEvents(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to get event stream: %w", err)
	}

	c.streamClient = stream
	c.updateHealthStatus(true)
	c.logger.Info("Started CRI event stream")

	// Process events from stream
	for {
		event, err := stream.Recv()
		if err != nil {
			c.logger.Error("Stream receive error", zap.Error(err))
			return err
		}

		// Process the container event
		if err := c.processStreamEvent(ctx, event); err != nil {
			c.logger.Warn("Failed to process stream event",
				zap.Error(err),
				zap.String("container_id", event.ContainerId),
			)

			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "process_stream_event"),
				))
			}
		}
	}
}

// loadInitialState loads the current state of all containers
func (c *Collector) loadInitialState(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri.load_initial_state")
	defer span.End()

	// List all containers to get initial state
	resp, err := c.client.ListContainers(ctx, &cri.ListContainersRequest{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	c.logger.Info("Loading initial container state",
		zap.Int("container_count", len(resp.Containers)),
	)

	// Load detailed info for each container
	for _, container := range resp.Containers {
		statusResp, err := c.client.ContainerStatus(ctx, &cri.ContainerStatusRequest{
			ContainerId: container.Id,
			Verbose:     true,
		})
		if err != nil {
			c.logger.Warn("Failed to get container status",
				zap.String("container_id", container.Id),
				zap.Error(err),
			)
			continue
		}

		// Cache the container info
		c.cacheContainerStatus(container, statusResp.Status)
	}

	return nil
}

// processStreamEvent processes a single event from the stream
func (c *Collector) processStreamEvent(ctx context.Context, event *cri.ContainerEventResponse) error {
	start := time.Now()
	defer func() {
		if c.processingTime != nil {
			duration := time.Since(start).Milliseconds()
			c.processingTime.Record(ctx, float64(duration), metric.WithAttributes(
				attribute.String("operation", "process_stream_event"),
				attribute.String("event_type", event.ContainerEventType.String()),
			))
		}
	}()

	// Update container info cache
	info := c.updateContainerInfo(event)

	// Create domain event
	containerIDShort := event.ContainerId
	if len(containerIDShort) > 12 {
		containerIDShort = containerIDShort[:12]
	}
	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("%s-%s-%d", c.name, containerIDShort, time.Now().UnixNano()),
		Timestamp: time.Unix(0, event.CreatedAt),
		Source:    c.name,
		Type:      mapEventType(event.ContainerEventType),
		Severity:  domain.EventSeverityInfo,

		EventData: domain.EventDataContainer{
			Container: &domain.ContainerData{
				ContainerID: event.ContainerId,
				ImageID:     info.ImageID,
				ImageName:   info.Image,
				Runtime:     info.Runtime,
				State:       info.State,
				Action:      mapEventAction(event.ContainerEventType),
				Labels:      info.Labels,
			},
		},

		K8sContext: &domain.K8sContext{
			Name:      info.PodName,
			Namespace: info.PodNamespace,
			UID:       info.PodUID,
			Labels:    info.Labels,
		},

		CorrelationHints: &domain.CorrelationHints{
			ContainerID: event.ContainerId,
			PodUID:      info.PodUID,
		},
	}

	// Send event
	select {
	case c.events <- domainEvent:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(domainEvent.Type)),
				attribute.String("container_state", info.State),
			))
		}
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Channel full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "channel_full"),
			))
		}
	}

	return nil
}

// ContainerInfo holds enriched container metadata
type ContainerInfo struct {
	ContainerID  string
	Name         string
	Image        string
	ImageID      string
	Labels       map[string]string
	Annotations  map[string]string
	PodName      string
	PodUID       string
	PodNamespace string
	Runtime      string
	State        string
	CreatedAt    time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
	ExitCode     int32
	Reason       string
	Message      string
	RestartCount int32
	LastUpdated  time.Time
}

// updateContainerInfo updates the container metadata cache
func (c *Collector) updateContainerInfo(event *cri.ContainerEventResponse) *ContainerInfo {
	c.infoMu.Lock()
	defer c.infoMu.Unlock()

	info, exists := c.containerInfo[event.ContainerId]
	if !exists {
		info = &ContainerInfo{
			ContainerID: event.ContainerId,
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
		}
		c.containerInfo[event.ContainerId] = info
	}

	// Update state based on event type
	info.State = event.ContainerEventType.String()
	info.LastUpdated = time.Now()

	// Extract K8s metadata from pod sandbox status if available
	if event.PodSandboxStatus != nil {
		if meta := event.PodSandboxStatus.GetMetadata(); meta != nil {
			info.PodUID = meta.GetUid()
			info.PodName = meta.GetName()
			info.PodNamespace = meta.GetNamespace()
		}

		// Update labels from pod sandbox
		if event.PodSandboxStatus.Labels != nil {
			for k, v := range event.PodSandboxStatus.Labels {
				info.Labels[k] = v
			}
		}

		// Update annotations
		if event.PodSandboxStatus.Annotations != nil {
			for k, v := range event.PodSandboxStatus.Annotations {
				info.Annotations[k] = v
			}
		}
	}

	return info
}

// cacheContainerStatus caches detailed container status
func (c *Collector) cacheContainerStatus(container *cri.Container, status *cri.ContainerStatus) {
	c.infoMu.Lock()
	defer c.infoMu.Unlock()

	containerInfo := &ContainerInfo{
		ContainerID:  container.Id,
		Name:         extractFromLabels(container.Labels, "io.kubernetes.container.name"),
		Image:        container.Image.Image,
		ImageID:      container.ImageRef,
		Labels:       container.Labels,
		Annotations:  container.Annotations,
		PodName:      extractFromLabels(container.Labels, "io.kubernetes.pod.name"),
		PodUID:       extractFromLabels(container.Labels, "io.kubernetes.pod.uid"),
		PodNamespace: extractFromLabels(container.Labels, "io.kubernetes.pod.namespace"),
		State:        status.State.String(),
		CreatedAt:    time.Unix(0, status.CreatedAt),
		LastUpdated:  time.Now(),
	}

	// Update exit info if container has exited
	if status.State == cri.ContainerState_CONTAINER_EXITED {
		containerInfo.ExitCode = status.ExitCode
		containerInfo.FinishedAt = time.Unix(0, status.FinishedAt)
		containerInfo.Reason = status.Reason
		containerInfo.Message = status.Message
	}

	c.containerInfo[container.Id] = containerInfo
}

// metadataEnricher periodically enriches container metadata
func (c *Collector) metadataEnricher() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.enrichMetadata()
		}
	}
}

// enrichMetadata enriches cached container metadata
func (c *Collector) enrichMetadata() {
	c.infoMu.RLock()
	containerIDs := make([]string, 0, len(c.containerInfo))
	for id := range c.containerInfo {
		containerIDs = append(containerIDs, id)
	}
	c.infoMu.RUnlock()

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	for _, id := range containerIDs {
		// Get fresh status with verbose info
		statusResp, err := c.client.ContainerStatus(ctx, &cri.ContainerStatusRequest{
			ContainerId: id,
			Verbose:     true,
		})
		if err != nil {
			continue // Container might have been removed
		}

		if statusResp.Status != nil {
			// Update cached info
			c.infoMu.Lock()
			if info, exists := c.containerInfo[id]; exists {
				info.State = statusResp.Status.State.String()
				info.LastUpdated = time.Now()

				// Update exit code if container has exited
				if statusResp.Status.State == cri.ContainerState_CONTAINER_EXITED {
					info.ExitCode = statusResp.Status.ExitCode
					info.FinishedAt = time.Unix(0, statusResp.Status.FinishedAt)
					info.Reason = statusResp.Status.Reason
					info.Message = statusResp.Status.Message
				}
			}
			c.infoMu.Unlock()
		}
	}
}

// healthChecker monitors connection health
func (c *Collector) healthChecker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.checkHealth()
		case <-c.reconnectChan:
			// Reconnection requested
			if err := c.reconnect(); err != nil {
				c.logger.Error("Failed to reconnect", zap.Error(err))
				c.updateHealthStatus(false)
			}
		}
	}
}

// checkHealth checks the health of the CRI connection
func (c *Collector) checkHealth() {
	if c.conn != nil {
		state := c.conn.GetState()
		healthy := state == connectivity.Ready || state == connectivity.Idle
		c.updateHealthStatus(healthy)

		if !healthy {
			c.logger.Warn("CRI connection unhealthy",
				zap.String("state", state.String()),
			)

			// Trigger reconnection
			select {
			case c.reconnectChan <- struct{}{}:
			default:
			}
		}
	} else {
		c.updateHealthStatus(false)
		// Trigger reconnection if no connection
		select {
		case c.reconnectChan <- struct{}{}:
		default:
		}
	}
}

// reconnect attempts to reconnect to CRI
func (c *Collector) reconnect() error {
	c.logger.Info("Attempting to reconnect to CRI")

	// Close existing connection
	if c.conn != nil {
		c.conn.Close()
	}

	// Establish new connection with retry
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	conn, err := grpc.DialContext(dialCtx, fmt.Sprintf("unix://%s", c.socket),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("reconnection failed: %w", err)
	}

	c.conn = conn
	c.client = cri.NewRuntimeServiceClient(conn)

	// Reload container state
	if err := c.loadInitialState(ctx); err != nil {
		c.logger.Warn("Failed to reload container state after reconnect", zap.Error(err))
	}

	c.logger.Info("Successfully reconnected to CRI")
	return nil
}

// mapEventType maps CRI event types to domain event types
func mapEventType(eventType cri.ContainerEventType) domain.CollectorEventType {
	switch eventType {
	case cri.ContainerEventType_CONTAINER_CREATED_EVENT:
		return domain.EventTypeContainerCreate
	case cri.ContainerEventType_CONTAINER_STARTED_EVENT:
		return domain.EventTypeContainerStart
	case cri.ContainerEventType_CONTAINER_STOPPED_EVENT,
		cri.ContainerEventType_CONTAINER_DELETED_EVENT:
		return domain.EventTypeContainerStop
	default:
		return domain.EventTypeContainerStop
	}
}

// mapEventAction maps CRI event types to action strings
func mapEventAction(eventType cri.ContainerEventType) string {
	switch eventType {
	case cri.ContainerEventType_CONTAINER_CREATED_EVENT:
		return "create"
	case cri.ContainerEventType_CONTAINER_STARTED_EVENT:
		return "start"
	case cri.ContainerEventType_CONTAINER_STOPPED_EVENT:
		return "stop"
	case cri.ContainerEventType_CONTAINER_DELETED_EVENT:
		return "delete"
	default:
		return "unknown"
	}
}

// extractFromLabels safely extracts a value from labels
func extractFromLabels(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}

// processContainer processes a single container - DEPRECATED (for backward compatibility)
func (c *Collector) processContainer(ctx context.Context, container *cri.Container) {
	// Get container status
	statusResp, err := c.client.ContainerStatus(ctx, &cri.ContainerStatusRequest{
		ContainerId: container.Id,
		Verbose:     false,
	})
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "container_status_failed"),
			))
		}
		return
	}

	status := statusResp.Status
	if status == nil {
		return
	}

	// Extract Kubernetes context from labels if available
	k8sContext := &domain.K8sContext{}
	if podName, ok := container.Labels["io.kubernetes.pod.name"]; ok {
		k8sContext.Name = podName
	}
	if podNamespace, ok := container.Labels["io.kubernetes.pod.namespace"]; ok {
		k8sContext.Namespace = podNamespace
	}
	if podUID, ok := container.Labels["io.kubernetes.pod.uid"]; ok {
		k8sContext.UID = podUID
	}

	// Create proper CollectorEvent with structured data
	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("%s-%s-%d", c.name, container.Id[:12], time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      getContainerEventType(status.State),
		Severity:  domain.EventSeverityInfo,

		EventData: domain.EventDataContainer{
			Container: &domain.ContainerData{
				ContainerID: container.Id,
				ImageID:     container.ImageRef,
				ImageName:   container.Image.Image,
				Runtime:     "cri", // Generic CRI runtime
				State:       status.State.String(),
				Action:      getContainerAction(status.State),
				Labels:      container.Labels,
				// Note: CRI API doesn't expose PID in container info
			},
		},

		K8sContext: k8sContext,

		CorrelationHints: &domain.CorrelationHints{
			ContainerID: container.Id,
			// Note: CRI API doesn't expose PID in container info
		},
	}

	// Send event
	select {
	case c.events <- event:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
				attribute.String("container_state", status.State.String()),
			))
		}
	default:
		// Channel full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "channel_full"),
			))
		}
	}
}

// getContainerEventType returns event type based on container state
func getContainerEventType(state cri.ContainerState) domain.CollectorEventType {
	switch state {
	case cri.ContainerState_CONTAINER_CREATED:
		return domain.EventTypeContainerCreate
	case cri.ContainerState_CONTAINER_RUNNING:
		return domain.EventTypeContainerStart
	case cri.ContainerState_CONTAINER_EXITED:
		return domain.EventTypeContainerStop
	default:
		return domain.EventTypeContainerStop // Default to stop for unknown states
	}
}

// getContainerAction returns the action based on container state
func getContainerAction(state cri.ContainerState) string {
	switch state {
	case cri.ContainerState_CONTAINER_CREATED:
		return "create"
	case cri.ContainerState_CONTAINER_RUNNING:
		return "start"
	case cri.ContainerState_CONTAINER_EXITED:
		return "stop"
	default:
		return "unknown"
	}
}

// updateHealthStatus safely updates the health status
func (c *Collector) updateHealthStatus(healthy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthy = healthy
}

// detectCRISocket detects the CRI socket path
func detectCRISocket() string {
	sockets := []string{
		"/run/containerd/containerd.sock",
		"/run/crio/crio.sock",
		"/var/run/dockershim.sock",
		"/var/run/cri-dockerd.sock",
	}

	for _, socket := range sockets {
		if _, err := os.Stat(socket); err == nil {
			return socket
		}
	}

	return ""
}
