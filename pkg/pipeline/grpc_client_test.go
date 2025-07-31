package pipeline

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yairfalse/tapio/pkg/collectors"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// Real test server implementation
type testServer struct {
	pb.UnimplementedRawEventServiceServer
	mu              sync.Mutex
	receivedBatches []*pb.RawEventBatch
}

func (s *testServer) SendBatch(ctx context.Context, batch *pb.RawEventBatch) (*pb.BatchAck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.receivedBatches = append(s.receivedBatches, batch)

	return &pb.BatchAck{
		Success:        true,
		ProcessedCount: int32(len(batch.Events)),
	}, nil
}

func (s *testServer) getReceivedBatches() []*pb.RawEventBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*pb.RawEventBatch{}, s.receivedBatches...)
}

func TestGRPCClientRealServer(t *testing.T) {
	// Start a real gRPC server
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	testSrv := &testServer{}
	pb.RegisterRawEventServiceServer(grpcServer, testSrv)

	go grpcServer.Serve(listener)
	defer grpcServer.Stop()

	// Create client config
	config := &ClientConfig{
		Endpoint:      listener.Addr().String(),
		BatchSize:     5,
		FlushInterval: "100ms",
		Timeout:       "5s",
	}

	// Create real client
	client, err := NewGRPCClient(config)
	require.NoError(t, err)
	defer client.Close()

	// Send some events
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		event := collectors.RawEvent{
			Type:      "test",
			Timestamp: time.Now(),
			Data:      []byte("test data"),
			Metadata: map[string]string{
				"index": fmt.Sprintf("%d", i),
			},
		}
		err := client.Send(ctx, event)
		assert.NoError(t, err)

		// Small delay after batch size is reached to ensure flush happens
		if i == 4 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Wait for final flush
	time.Sleep(200 * time.Millisecond)

	// Check received batches
	batches := testSrv.getReceivedBatches()
	assert.Len(t, batches, 2) // 10 events with batch size 5 = 2 batches

	// Verify first batch
	assert.Equal(t, 5, len(batches[0].Events))
	assert.Equal(t, "test", batches[0].Events[0].Type)
	assert.Equal(t, "0", batches[0].Events[0].Metadata["index"])

	// Verify second batch
	assert.Equal(t, 5, len(batches[1].Events))
	assert.Equal(t, "5", batches[1].Events[0].Metadata["index"])
}

func TestGRPCClientBatching(t *testing.T) {
	// Start server
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	testSrv := &testServer{}
	pb.RegisterRawEventServiceServer(grpcServer, testSrv)

	go grpcServer.Serve(listener)
	defer grpcServer.Stop()

	// Create client with larger batch
	config := &ClientConfig{
		Endpoint:      listener.Addr().String(),
		BatchSize:     20,
		FlushInterval: "1s", // Long interval to test manual flush
		Timeout:       "5s",
	}

	client, err := NewGRPCClient(config)
	require.NoError(t, err)

	// Send events less than batch size
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		event := collectors.RawEvent{
			Type:      "test",
			Timestamp: time.Now(),
			Data:      []byte("data"),
		}
		client.Send(ctx, event)
	}

	// No flush yet
	time.Sleep(100 * time.Millisecond)
	assert.Len(t, testSrv.getReceivedBatches(), 0)

	// Close should flush
	client.Close()

	// Check final flush
	batches := testSrv.getReceivedBatches()
	assert.Len(t, batches, 1)
	assert.Equal(t, 15, len(batches[0].Events))
}

func TestGRPCClientAutoFlush(t *testing.T) {
	// Start server
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	testSrv := &testServer{}
	pb.RegisterRawEventServiceServer(grpcServer, testSrv)

	go grpcServer.Serve(listener)
	defer grpcServer.Stop()

	// Create client with short flush interval
	config := &ClientConfig{
		Endpoint:      listener.Addr().String(),
		BatchSize:     100, // Large batch to ensure timer triggers
		FlushInterval: "50ms",
		Timeout:       "5s",
	}

	client, err := NewGRPCClient(config)
	require.NoError(t, err)
	defer client.Close()

	// Send just one event
	event := collectors.RawEvent{
		Type:      "test",
		Timestamp: time.Now(),
		Data:      []byte("single event"),
	}
	client.Send(context.Background(), event)

	// Wait for auto flush
	time.Sleep(100 * time.Millisecond)

	// Should have flushed
	batches := testSrv.getReceivedBatches()
	assert.Len(t, batches, 1)
	assert.Len(t, batches[0].Events, 1)
	assert.Equal(t, "single event", string(batches[0].Events[0].Data))
}

// Error handling test server
type errorServer struct {
	pb.UnimplementedRawEventServiceServer
}

func (s *errorServer) SendBatch(ctx context.Context, batch *pb.RawEventBatch) (*pb.BatchAck, error) {
	return nil, status.Error(codes.Unavailable, "service unavailable")
}

func TestGRPCClientErrorHandling(t *testing.T) {
	// Start error server
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	pb.RegisterRawEventServiceServer(grpcServer, &errorServer{})

	go grpcServer.Serve(listener)
	defer grpcServer.Stop()

	// Create client
	config := &ClientConfig{
		Endpoint:      listener.Addr().String(),
		BatchSize:     1,
		FlushInterval: "50ms",
		Timeout:       "1s",
	}

	client, err := NewGRPCClient(config)
	require.NoError(t, err)
	defer client.Close()

	// Send event - should not fail (errors are logged, not returned)
	event := collectors.RawEvent{
		Type:      "test",
		Timestamp: time.Now(),
		Data:      []byte("test"),
	}
	err = client.Send(context.Background(), event)
	assert.NoError(t, err) // Send doesn't return errors

	// Wait for flush attempt
	time.Sleep(100 * time.Millisecond)

	// Events are lost on error (as designed - could add retry logic)
}
