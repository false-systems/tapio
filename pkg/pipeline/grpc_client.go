package pipeline

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yairfalse/tapio/pkg/collectors"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// GRPCClient implements the pipeline client using gRPC
type GRPCClient struct {
	conn   *grpc.ClientConn
	client pb.RawEventServiceClient
	config *ClientConfig

	// Batching
	batch   []collectors.RawEvent
	batchMu sync.Mutex
	flushCh chan struct{}

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewGRPCClient creates a new gRPC pipeline client
func NewGRPCClient(config *ClientConfig) (*GRPCClient, error) {
	// Parse timeout
	timeout, err := time.ParseDuration(config.Timeout)
	if err != nil {
		timeout = 30 * time.Second
	}

	// Parse flush interval
	flushInterval, err := time.ParseDuration(config.FlushInterval)
	if err != nil {
		flushInterval = 5 * time.Second
	}

	// Create gRPC connection with keepalive
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, config.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             timeout,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to pipeline: %w", err)
	}

	// Create client
	client := pb.NewRawEventServiceClient(conn)

	ctx, cancel = context.WithCancel(context.Background())

	c := &GRPCClient{
		conn:    conn,
		client:  client,
		config:  config,
		batch:   make([]collectors.RawEvent, 0, config.BatchSize),
		flushCh: make(chan struct{}, 1),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start batch processor
	c.wg.Add(1)
	go c.batchProcessor(flushInterval)

	return c, nil
}

// Send adds an event to the batch
func (c *GRPCClient) Send(ctx context.Context, event collectors.RawEvent) error {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()

	c.batch = append(c.batch, event)

	// Flush if batch is full
	if len(c.batch) >= c.config.BatchSize {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}

	return nil
}

// Close gracefully shuts down the client
func (c *GRPCClient) Close() error {
	// Final flush before canceling context
	c.flush()

	// Cancel context
	c.cancel()

	// Wait for batch processor
	c.wg.Wait()

	// Close connection
	return c.conn.Close()
}

// batchProcessor handles periodic flushing
func (c *GRPCClient) batchProcessor(interval time.Duration) {
	defer c.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.flush()
		case <-c.flushCh:
			c.flush()
		}
	}
}

// flush sends the current batch
func (c *GRPCClient) flush() {
	c.batchMu.Lock()
	if len(c.batch) == 0 {
		c.batchMu.Unlock()
		return
	}

	// Take ownership of batch
	events := c.batch
	c.batch = make([]collectors.RawEvent, 0, c.config.BatchSize)
	c.batchMu.Unlock()

	// Convert to proto events
	protoEvents := make([]*pb.RawEvent, 0, len(events))
	for _, event := range events {
		protoEvent := &pb.RawEvent{
			Type:      event.Type,
			Timestamp: timestamppb.New(event.Timestamp),
			Data:      event.Data,
			Metadata:  event.Metadata,
		}
		protoEvents = append(protoEvents, protoEvent)
	}

	// Send batch - use fresh context to avoid cancellation issues
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get node ID (hostname)
	hostname, _ := os.Hostname()

	req := &pb.RawEventBatch{
		Events:      protoEvents,
		CollectorId: "tapio-unified",
		NodeId:      hostname,
	}

	_, err := c.client.SendBatch(ctx, req)
	if err != nil {
		// Log error but don't fail - events are lost
		// In production, add retry logic here
		fmt.Printf("Failed to send batch: %v\n", err)
	}
}
