package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yairfalse/tapio/pkg/events"
)

// Client provides a resilient gRPC client for streaming events to the server
type Client struct {
	config ClientConfig

	// Connection management
	conn   *grpc.ClientConn
	client CollectorServiceClient
	connMu sync.RWMutex

	// Stream management
	stream       CollectorService_StreamEventsClient
	streamMu     sync.RWMutex
	streamCtx    context.Context
	streamCancel context.CancelFunc

	// Reconnection logic
	reconnectCh chan struct{}
	backoff     *ExponentialBackoff

	// Event batching
	batcher *EventBatcher

	// Flow control
	flowState *ClientFlowState

	// Metrics and monitoring
	metrics *ClientMetrics

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State tracking
	connected atomic.Bool
	streaming atomic.Bool
	stopped   atomic.Bool

	// Configuration
	collectorID   string
	collectorType string
	nodeInfo      *NodeInfo
}

// ClientConfig contains client configuration options
type ClientConfig struct {
	// Server connection
	ServerEndpoints []string
	TLSEnabled      bool
	TLSInsecure     bool
	CertFile        string
	KeyFile         string
	CAFile          string

	// Collector identification
	CollectorID   string
	CollectorType string
	Version       string

	// Performance settings
	MaxBatchSize       uint32
	BatchTimeout       time.Duration
	MaxEventsPerSecond uint32
	BufferSize         int

	// Connection settings
	ConnectTimeout   time.Duration
	KeepAliveTime    time.Duration
	KeepAliveTimeout time.Duration
	MaxRetries       int

	// Reconnection settings
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	Jitter            bool

	// Health checking
	HeartbeatInterval  time.Duration
	HealthCheckTimeout time.Duration

	// Quality settings
	EnableAcks        bool
	AckTimeout        time.Duration
	RequireValidation bool
}

// DefaultClientConfig returns a production-ready client configuration
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		TLSEnabled:         true,
		TLSInsecure:        false,
		MaxBatchSize:       1000,
		BatchTimeout:       time.Second,
		MaxEventsPerSecond: 10000,
		BufferSize:         10000,
		ConnectTimeout:     10 * time.Second,
		KeepAliveTime:      30 * time.Second,
		KeepAliveTimeout:   5 * time.Second,
		MaxRetries:         5,
		InitialBackoff:     time.Second,
		MaxBackoff:         60 * time.Second,
		BackoffMultiplier:  2.0,
		Jitter:             true,
		HeartbeatInterval:  30 * time.Second,
		HealthCheckTimeout: 5 * time.Second,
		EnableAcks:         true,
		AckTimeout:         5 * time.Second,
		RequireValidation:  true,
	}
}

// ClientFlowState tracks client-side flow control state
type ClientFlowState struct {
	maxEventsPerSecond uint32
	maxBatchSize       uint32
	compressionType    CompressionType

	// Backpressure detection
	bufferUtilization float32
	memoryPressure    MemoryPressure

	// Rate limiting
	rateLimiter *RateLimiter

	mu sync.RWMutex
}

// ExponentialBackoff implements exponential backoff with jitter
type ExponentialBackoff struct {
	initialBackoff time.Duration
	maxBackoff     time.Duration
	multiplier     float64
	jitter         bool
	attempt        uint32
}

// NewClient creates a new gRPC client
func NewClient(config ClientConfig, nodeInfo *NodeInfo) *Client {
	if config.CollectorID == "" {
		config.CollectorID = fmt.Sprintf("%s-%d", config.CollectorType, time.Now().Unix())
	}

	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		config:        config,
		collectorID:   config.CollectorID,
		collectorType: config.CollectorType,
		nodeInfo:      nodeInfo,
		ctx:           ctx,
		cancel:        cancel,
		reconnectCh:   make(chan struct{}, 1),
		backoff:       NewExponentialBackoff(config),
		metrics:       NewClientMetrics(),
	}

	// Initialize flow state
	client.flowState = &ClientFlowState{
		maxEventsPerSecond: config.MaxEventsPerSecond,
		maxBatchSize:       config.MaxBatchSize,
		compressionType:    CompressionType_COMPRESSION_LZ4,
		rateLimiter:        NewRateLimiter(float64(config.MaxEventsPerSecond), int64(config.MaxEventsPerSecond*2)),
	}

	// Initialize batcher
	client.batcher = NewEventBatcher(config, client.sendBatch)

	return client
}

// NewExponentialBackoff creates a new exponential backoff
func NewExponentialBackoff(config ClientConfig) *ExponentialBackoff {
	return &ExponentialBackoff{
		initialBackoff: config.InitialBackoff,
		maxBackoff:     config.MaxBackoff,
		multiplier:     config.BackoffMultiplier,
		jitter:         config.Jitter,
	}
}

// NextBackoff calculates the next backoff duration
func (eb *ExponentialBackoff) NextBackoff() time.Duration {
	attempt := atomic.LoadUint32(&eb.attempt)

	// Calculate exponential backoff
	backoff := eb.initialBackoff
	for i := uint32(0); i < attempt; i++ {
		backoff = time.Duration(float64(backoff) * eb.multiplier)
		if backoff > eb.maxBackoff {
			backoff = eb.maxBackoff
			break
		}
	}

	// Add jitter if enabled
	if eb.jitter {
		jitterAmount := time.Duration(float64(backoff) * 0.1) // 10% jitter
		backoff += time.Duration(rand.Int63n(int64(jitterAmount)))
	}

	atomic.AddUint32(&eb.attempt, 1)
	return backoff
}

// Reset resets the backoff attempt counter
func (eb *ExponentialBackoff) Reset() {
	atomic.StoreUint32(&eb.attempt, 0)
}

// Start starts the client and establishes connections
func (c *Client) Start(ctx context.Context) error {
	if c.stopped.Load() {
		return fmt.Errorf("client has been stopped")
	}

	// Start background tasks
	c.wg.Add(4)
	go c.connectionManager()
	go c.heartbeatManager()
	go c.flowControlManager()
	go c.metricsUpdater()

	// Start batcher
	if err := c.batcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start event batcher: %w", err)
	}

	// Trigger initial connection
	select {
	case c.reconnectCh <- struct{}{}:
	default:
	}

	c.metrics.ClientStarted()

	return nil
}

// Stop gracefully stops the client
func (c *Client) Stop() error {
	if !c.stopped.CompareAndSwap(false, true) {
		return fmt.Errorf("client already stopped")
	}

	// Stop batcher first to prevent new events
	c.batcher.Stop()

	// Cancel context
	c.cancel()

	// Close connections
	c.closeStream()
	c.closeConnection()

	// Wait for background tasks
	c.wg.Wait()

	c.metrics.ClientStopped()

	return nil
}

// SendEvent sends a single event
func (c *Client) SendEvent(ctx context.Context, event *events.UnifiedEvent) error {
	if c.stopped.Load() {
		return fmt.Errorf("client is stopped")
	}

	// Validate event if required
	if c.config.RequireValidation {
		if err := event.Validate(); err != nil {
			c.metrics.EventValidationFailed()
			return fmt.Errorf("event validation failed: %w", err)
		}
	}

	// Add to batcher
	return c.batcher.AddEvent(event)
}

// SendEventBatch sends a batch of events
func (c *Client) SendEventBatch(ctx context.Context, events []*events.UnifiedEvent) error {
	if c.stopped.Load() {
		return fmt.Errorf("client is stopped")
	}

	if len(events) == 0 {
		return fmt.Errorf("empty event batch")
	}

	// Validate events if required
	if c.config.RequireValidation {
		for _, event := range events {
			if err := event.Validate(); err != nil {
				c.metrics.EventValidationFailed()
				return fmt.Errorf("event validation failed: %w", err)
			}
		}
	}

	// Create batch
	batch := &EventBatch{
		BatchId:       fmt.Sprintf("batch_%d", time.Now().UnixNano()),
		CreatedAt:     timestamppb.Now(),
		CollectorId:   c.collectorID,
		CollectorType: c.collectorType,
		NodeId:        c.nodeInfo.NodeId,
		Events:        events,
		Compression:   c.flowState.compressionType,
	}

	return c.sendBatch(ctx, batch)
}

// sendBatch sends a batch to the server
func (c *Client) sendBatch(ctx context.Context, batch *EventBatch) error {
	if !c.streaming.Load() {
		return c.sendBatchNonStreaming(ctx, batch)
	}

	return c.sendBatchStreaming(ctx, batch)
}

// sendBatchStreaming sends a batch via streaming
func (c *Client) sendBatchStreaming(ctx context.Context, batch *EventBatch) error {
	c.streamMu.RLock()
	stream := c.stream
	c.streamMu.RUnlock()

	if stream == nil {
		return fmt.Errorf("no active stream")
	}

	// Check rate limits
	if !c.flowState.rateLimiter.AllowN(len(batch.Events)) {
		c.metrics.EventsThrottled(uint64(len(batch.Events)))
		return fmt.Errorf("rate limited")
	}

	request := &StreamRequest{
		Sequence:    c.metrics.NextSequence(),
		CollectorId: c.collectorID,
		Payload: &StreamRequest_Events{
			Events: batch,
		},
	}

	// Send request with timeout
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- stream.Send(request)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			c.metrics.EventSendFailed(uint64(len(batch.Events)))
			c.triggerReconnect()
			return fmt.Errorf("failed to send batch: %w", err)
		}
		c.metrics.EventsSent(uint64(len(batch.Events)))
		return nil

	case <-sendCtx.Done():
		c.metrics.EventSendTimeout()
		return fmt.Errorf("send timeout")

	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

// sendBatchNonStreaming sends a batch via non-streaming RPC
func (c *Client) sendBatchNonStreaming(ctx context.Context, batch *EventBatch) error {
	c.connMu.RLock()
	client := c.client
	c.connMu.RUnlock()

	if client == nil {
		return fmt.Errorf("no connection available")
	}

	request := &EventBatchRequest{
		Batch:      batch,
		RequireAck: c.config.EnableAcks,
		Timeout:    durationpb.New(c.config.AckTimeout),
	}

	response, err := client.SendEventBatch(ctx, request)
	if err != nil {
		c.metrics.EventSendFailed(uint64(len(batch.Events)))
		return fmt.Errorf("failed to send batch: %w", err)
	}

	if !response.Success {
		c.metrics.EventSendFailed(uint64(len(batch.Events)))
		return fmt.Errorf("server rejected batch: %s", response.Message)
	}

	// Process flow control if provided
	if response.FlowControl != nil {
		c.updateFlowControl(response.FlowControl)
	}

	c.metrics.EventsSent(uint64(len(batch.Events)))
	return nil
}

// Connection management methods

// connectionManager manages connection lifecycle
func (c *Client) connectionManager() {
	defer c.wg.Done()

	for {
		select {
		case <-c.reconnectCh:
			if err := c.connect(); err != nil {
				c.metrics.ConnectionFailed()

				// Schedule reconnection with backoff
				backoff := c.backoff.NextBackoff()
				timer := time.NewTimer(backoff)

				select {
				case <-timer.C:
					select {
					case c.reconnectCh <- struct{}{}:
					default:
					}
				case <-c.ctx.Done():
					timer.Stop()
					return
				}
			} else {
				c.backoff.Reset()
				c.metrics.ConnectionEstablished()
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// connect establishes connection to server
func (c *Client) connect() error {
	// Close existing connection
	c.closeConnection()

	// Try each endpoint
	var lastErr error
	for _, endpoint := range c.config.ServerEndpoints {
		if err := c.connectToEndpoint(endpoint); err != nil {
			lastErr = err
			continue
		}

		// Connection successful
		c.connected.Store(true)

		// Register collector
		if err := c.registerCollector(); err != nil {
			c.closeConnection()
			return fmt.Errorf("failed to register collector: %w", err)
		}

		// Start streaming
		if err := c.startStreaming(); err != nil {
			c.closeConnection()
			return fmt.Errorf("failed to start streaming: %w", err)
		}

		return nil
	}

	return fmt.Errorf("failed to connect to any endpoint: %w", lastErr)
}

// connectToEndpoint connects to a specific endpoint
func (c *Client) connectToEndpoint(endpoint string) error {
	ctx, cancel := context.WithTimeout(c.ctx, c.config.ConnectTimeout)
	defer cancel()

	// Setup TLS
	var creds credentials.TransportCredentials
	if c.config.TLSEnabled {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: c.config.TLSInsecure,
		}

		if c.config.CertFile != "" && c.config.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(c.config.CertFile, c.config.KeyFile)
			if err != nil {
				return fmt.Errorf("failed to load client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		creds = credentials.NewTLS(tlsConfig)
	} else {
		creds = insecure.NewCredentials()
	}

	// Create connection
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                c.config.KeepAliveTime,
			Timeout:             c.config.KeepAliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(4*1024*1024), // 4MB
			grpc.MaxCallRecvMsgSize(4*1024*1024), // 4MB
		),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", endpoint, err)
	}

	// Wait for connection to be ready
	if !conn.WaitForStateChange(ctx, connectivity.Connecting) {
		conn.Close()
		return fmt.Errorf("connection timeout")
	}

	if conn.GetState() != connectivity.Ready {
		conn.Close()
		return fmt.Errorf("connection not ready")
	}

	c.connMu.Lock()
	c.conn = conn
	c.client = NewCollectorServiceClient(conn)
	c.connMu.Unlock()

	return nil
}

// registerCollector registers the collector with the server
func (c *Client) registerCollector() error {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	registration := &CollectorRegistration{
		CollectorId:   c.collectorID,
		CollectorType: c.collectorType,
		Version:       c.config.Version,
		Node:          c.nodeInfo,
		Capabilities: &CollectorCapabilities{
			MaxEventsPerSecond:   c.config.MaxEventsPerSecond,
			SupportedCompression: []CompressionType{CompressionType_COMPRESSION_LZ4, CompressionType_COMPRESSION_GZIP},
			SupportedEventTypes:  []string{"network", "memory", "cpu", "io", "system"},
			SupportsStreaming:    true,
			SupportsBatching:     true,
			MaxBatchSize:         c.config.MaxBatchSize,
			BufferSize:           uint64(c.config.BufferSize),
		},
		ConfigRequest: &ConfigurationRequest{
			PreferredRate:        c.config.MaxEventsPerSecond,
			PreferredBatchSize:   c.config.MaxBatchSize,
			PreferredCompression: CompressionType_COMPRESSION_LZ4,
		},
	}

	response, err := c.client.RegisterCollector(ctx, registration)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Update configuration from server
	if response.FlowControl != nil {
		c.updateFlowControl(response.FlowControl)
	}

	c.metrics.CollectorRegistered()

	return nil
}

// startStreaming starts the bidirectional stream
func (c *Client) startStreaming() error {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	// Create stream context
	c.streamCtx, c.streamCancel = context.WithCancel(c.ctx)

	// Start stream
	stream, err := c.client.StreamEvents(c.streamCtx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	c.stream = stream
	c.streaming.Store(true)

	// Start response handler
	c.wg.Add(1)
	go c.handleStreamResponses()

	return nil
}

// handleStreamResponses handles responses from the stream
func (c *Client) handleStreamResponses() {
	defer c.wg.Done()
	defer c.streaming.Store(false)

	c.streamMu.RLock()
	stream := c.stream
	c.streamMu.RUnlock()

	for {
		select {
		case <-c.streamCtx.Done():
			return
		case <-c.ctx.Done():
			return
		default:
		}

		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				c.triggerReconnect()
				return
			}
			c.metrics.StreamError()
			c.triggerReconnect()
			return
		}

		c.metrics.ResponseReceived()
		c.handleResponse(response)
	}
}

// handleResponse processes a server response
func (c *Client) handleResponse(response *StreamResponse) {
	switch payload := response.Payload.(type) {
	case *StreamResponse_EventAck:
		c.metrics.EventAckReceived()
		c.batcher.ProcessAck(payload.EventAck)

	case *StreamResponse_FlowControl:
		c.updateFlowControl(payload.FlowControl)

	case *StreamResponse_ServerStatus:
		c.metrics.UpdateServerStatus(payload.ServerStatus)

	case *StreamResponse_Error:
		c.metrics.GenericError()
		c.handleServerError(payload.Error)
	}
}

// updateFlowControl updates flow control settings
func (c *Client) updateFlowControl(directive *FlowControlDirective) {
	c.flowState.mu.Lock()
	defer c.flowState.mu.Unlock()

	if directive.MaxEventsPerSecond > 0 {
		c.flowState.maxEventsPerSecond = directive.MaxEventsPerSecond
		c.flowState.rateLimiter.SetRate(float64(directive.MaxEventsPerSecond))
	}

	if directive.MaxBatchSize > 0 {
		c.flowState.maxBatchSize = directive.MaxBatchSize
		c.batcher.UpdateMaxBatchSize(directive.MaxBatchSize)
	}

	if directive.CompressionType != CompressionType_COMPRESSION_NONE {
		c.flowState.compressionType = directive.CompressionType
	}

	c.metrics.FlowControlUpdated()
}

// handleServerError handles server error responses
func (c *Client) handleServerError(errorResp *ErrorResponse) {
	switch errorResp.Code {
	case ErrorCode_ERROR_RATE_LIMITED:
		c.metrics.RateLimited()
		// Reduce sending rate temporarily
		c.flowState.rateLimiter.SetRate(float64(c.flowState.maxEventsPerSecond) * 0.5)

	case ErrorCode_ERROR_SERVICE_UNAVAILABLE:
		c.triggerReconnect()

	default:
		c.metrics.GenericError()
	}
}

// triggerReconnect triggers a reconnection attempt
func (c *Client) triggerReconnect() {
	c.closeStream()

	select {
	case c.reconnectCh <- struct{}{}:
	default:
	}
}

// closeStream closes the current stream
func (c *Client) closeStream() {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	if c.streamCancel != nil {
		c.streamCancel()
	}

	c.stream = nil
	c.streaming.Store(false)
}

// closeConnection closes the current connection
func (c *Client) closeConnection() {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.client = nil
	}

	c.connected.Store(false)
}

// Background management methods

// heartbeatManager sends periodic heartbeats
func (c *Client) heartbeatManager() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				c.metrics.HeartbeatFailed()
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// sendHeartbeat sends a heartbeat to the server
func (c *Client) sendHeartbeat() error {
	c.connMu.RLock()
	client := c.client
	c.connMu.RUnlock()

	if client == nil {
		return fmt.Errorf("no connection available")
	}

	ctx, cancel := context.WithTimeout(c.ctx, c.config.HealthCheckTimeout)
	defer cancel()

	request := &HeartbeatRequest{
		CollectorId: c.collectorID,
		Timestamp:   timestamppb.Now(),
		Status:      c.getCollectorStatus(),
	}

	// Create heartbeat stream
	stream, err := client.Heartbeat(ctx)
	if err != nil {
		return fmt.Errorf("failed to create heartbeat stream: %w", err)
	}
	defer stream.CloseSend()

	// Send heartbeat request
	if err := stream.Send(request); err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	// Receive response
	response, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive heartbeat response: %w", err)
	}

	c.metrics.HeartbeatSent()

	// Process server status
	if response.ServerStatus != nil {
		c.metrics.UpdateServerStatus(response.ServerStatus)
	}

	return nil
}

// flowControlManager monitors and reports flow control state
func (c *Client) flowControlManager() {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.updateBufferUtilization()
			c.sendFlowControlMessage()

		case <-c.ctx.Done():
			return
		}
	}
}

// updateBufferUtilization calculates current buffer utilization
func (c *Client) updateBufferUtilization() {
	utilization := c.batcher.GetUtilization()

	c.flowState.mu.Lock()
	c.flowState.bufferUtilization = utilization

	// Determine memory pressure based on utilization
	if utilization > 0.9 {
		c.flowState.memoryPressure = MemoryPressure_MEMORY_PRESSURE_CRITICAL
	} else if utilization > 0.8 {
		c.flowState.memoryPressure = MemoryPressure_MEMORY_PRESSURE_HIGH
	} else if utilization > 0.6 {
		c.flowState.memoryPressure = MemoryPressure_MEMORY_PRESSURE_MEDIUM
	} else if utilization > 0.4 {
		c.flowState.memoryPressure = MemoryPressure_MEMORY_PRESSURE_LOW
	} else {
		c.flowState.memoryPressure = MemoryPressure_MEMORY_PRESSURE_NONE
	}
	c.flowState.mu.Unlock()
}

// sendFlowControlMessage sends flow control information to server
func (c *Client) sendFlowControlMessage() {
	if !c.streaming.Load() {
		return
	}

	c.streamMu.RLock()
	stream := c.stream
	c.streamMu.RUnlock()

	if stream == nil {
		return
	}

	c.flowState.mu.RLock()
	msg := &FlowControlMessage{
		RequestedRate:     c.flowState.maxEventsPerSecond,
		BufferUtilization: c.flowState.bufferUtilization,
		MemoryPressure:    c.flowState.memoryPressure,
		Reason:            "periodic_update",
	}
	c.flowState.mu.RUnlock()

	request := &StreamRequest{
		Sequence:    c.metrics.NextSequence(),
		CollectorId: c.collectorID,
		Payload: &StreamRequest_FlowControl{
			FlowControl: msg,
		},
	}

	if err := stream.Send(request); err != nil {
		c.metrics.FlowControlSendFailed()
	}
}

// metricsUpdater periodically updates metrics
func (c *Client) metricsUpdater() {
	defer c.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.metrics.UpdateRates()

		case <-c.ctx.Done():
			return
		}
	}
}

// getCollectorStatus returns current collector status
func (c *Client) getCollectorStatus() *CollectorStatus {
	stats := c.metrics.GetStats()

	state := CollectorState_COLLECTOR_RUNNING
	if !c.connected.Load() {
		state = CollectorState_COLLECTOR_ERROR
	}

	return &CollectorStatus{
		State: state,
		Resources: &ResourceUtilization{
			CpuUsage:    0.1,              // Placeholder - would get from runtime
			MemoryBytes: 1024 * 1024 * 10, // 10MB placeholder
			Network: &NetworkUtilization{
				BytesSentPerSec: uint64(stats.BytesPerSecond),
				Connections:     1,
			},
		},
		Stats: &CollectionStats{
			TotalEvents:     stats.EventsSent,
			EventsPerSecond: float32(stats.EventsPerSecond),
			BatchesSent:     stats.BatchesSent,
			AvgBatchSize:    float32(stats.AvgBatchSize),
		},
		Uptime: durationpb.New(stats.Uptime),
	}
}

// GetStats returns client statistics
func (c *Client) GetStats() ClientStats {
	return c.metrics.GetStats()
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// IsStreaming returns whether the client is streaming
func (c *Client) IsStreaming() bool {
	return c.streaming.Load()
}
