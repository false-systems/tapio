package cri

import (
	"context"
	"encoding/json"
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
	"google.golang.org/grpc/credentials/insecure"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Collector implements simple CRI monitoring
type Collector struct {
	name    string
	logger  *zap.Logger
	tracer  trace.Tracer
	events  chan domain.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	config  *Config
	mu      sync.RWMutex

	// CRI components
	socket string
	client cri.RuntimeServiceClient
	conn   *grpc.ClientConn

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
		events:          make(chan domain.RawEvent, cfg.BufferSize),
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
		zap.Duration("poll_interval", cfg.PollInterval),
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

	// Connect to CRI runtime
	conn, err := grpc.Dial(fmt.Sprintf("unix://%s", c.socket),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		span.SetAttributes(attribute.String("error", "cri_connection_failed"))
		return fmt.Errorf("failed to connect to CRI socket %s: %w", c.socket, err)
	}
	c.conn = conn
	c.client = cri.NewRuntimeServiceClient(conn)

	// Start monitoring
	go c.monitor()

	c.healthy = true
	c.logger.Info("CRI collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping CRI collector")

	if c.cancel != nil {
		c.cancel()
	}

	if c.conn != nil {
		c.conn.Close()
	}

	close(c.events)
	c.healthy = false

	c.logger.Info("CRI collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan domain.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// monitor polls CRI for container events
func (c *Collector) monitor() {
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.pollContainers()
		}
	}
}

// pollContainers polls CRI for container status
func (c *Collector) pollContainers() {
	start := time.Now()
	ctx, span := c.tracer.Start(c.ctx, "cri.poll_containers")
	defer span.End()

	// List all containers
	resp, err := c.client.ListContainers(ctx, &cri.ListContainersRequest{})
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "list_containers_failed"),
			))
		}
		c.logger.Error("Failed to list containers", zap.Error(err))
		return
	}

	// Process each container
	for _, container := range resp.Containers {
		c.processContainer(ctx, container)
	}

	// Record processing time
	if c.processingTime != nil {
		duration := time.Since(start).Milliseconds()
		c.processingTime.Record(ctx, float64(duration), metric.WithAttributes(
			attribute.String("operation", "poll_containers"),
		))
	}

	// Update buffer usage
	if c.bufferUsage != nil {
		usage := int64(len(c.events))
		c.bufferUsage.Record(ctx, usage, metric.WithAttributes(
			attribute.String("collector", c.name),
		))
	}
}

// processContainer processes a single container
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

	// Create event from container status
	eventData := ContainerEventData{
		ContainerID: container.Id,
		Name:        status.Metadata.Name,
		State:       status.State.String(),
		Image:       container.Image.Image,
		Labels:      container.Labels,
		CreatedAt:   time.Unix(0, status.CreatedAt),
	}

	// Marshal event data to bytes
	data, err := json.Marshal(eventData)
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "marshal_failed"),
			))
		}
		return
	}

	event := domain.RawEvent{
		Timestamp: time.Now(),
		Source:    c.name,
		Type:      getContainerEventType(status.State),
		Data:      data,
	}

	// Send event
	select {
	case c.events <- event:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", event.Type),
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
func getContainerEventType(state cri.ContainerState) string {
	switch state {
	case cri.ContainerState_CONTAINER_CREATED:
		return "container_created"
	case cri.ContainerState_CONTAINER_RUNNING:
		return "container_running"
	case cri.ContainerState_CONTAINER_EXITED:
		return "container_exited"
	default:
		return "container_unknown"
	}
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
