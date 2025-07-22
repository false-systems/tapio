package grpc

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Test helper to create a test service
func createTestService(t *testing.T) *TapioServiceComplete {
	logger := zap.NewNop()

	storage := NewMemoryEventStorage(1000, 24*time.Hour)
	correlator := NewRealTimeCorrelationEngine(logger, CorrelationConfig{
		BufferSize:        100,
		TimeWindow:        5 * time.Minute,
		MaxCorrelations:   1000,
		PatternConfidence: 0.7,
		CleanupInterval:   5 * time.Minute,
		RetentionPeriod:   24 * time.Hour,
	})
	collectors := NewInMemoryCollectorRegistry(logger)
	metrics := NewPrometheusMetricsCollector(logger)

	service := &TapioServiceComplete{
		logger:     logger,
		tracer:     noop.NewTracerProvider().Tracer("test"),
		storage:    storage,
		correlator: correlator,
		collectors: collectors,
		metrics:    metrics,
		streams:    make(map[string]*EventStream),
		shutdown:   make(chan struct{}),
	}

	return service
}

// Test helper to create test events
func createTestEvent(t *testing.T, eventType domain.EventType, source string) *pb.TapioSubmitEventRequest {
	return &pb.TapioSubmitEventRequest{
		Event: &pb.UnifiedEvent{
			Id:        "test-" + time.Now().Format(time.RFC3339Nano),
			Type:      string(eventType),
			Source:    source,
			Timestamp: timestamppb.Now(),
			Entity: &pb.Entity{
				Type:      "service",
				Name:      "test-service",
				Namespace: "default",
			},
			Semantic: &pb.SemanticContext{
				Intent:     "test-intent",
				Category:   "test",
				Confidence: 0.9,
			},
		},
	}
}

func TestTapioServiceComplete_SubmitEvent(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	tests := []struct {
		name    string
		request *pb.TapioSubmitEventRequest
		wantErr bool
		errCode codes.Code
	}{
		{
			name:    "valid event",
			request: createTestEvent(t, domain.EventTypeProcess, "test-collector"),
			wantErr: false,
		},
		{
			name:    "missing event",
			request: &pb.TapioSubmitEventRequest{},
			wantErr: true,
			errCode: codes.InvalidArgument,
		},
		{
			name: "missing event ID",
			request: &pb.TapioSubmitEventRequest{
				Event: &pb.UnifiedEvent{
					Type:      string(domain.EventTypeProcess),
					Source:    "test",
					Timestamp: timestamppb.Now(),
				},
			},
			wantErr: true,
			errCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.SubmitEvent(context.Background(), tt.request)

			if tt.wantErr {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.NotEmpty(t, resp.EventId)
			}
		})
	}
}

func TestTapioServiceComplete_SubmitBatch(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	tests := []struct {
		name       string
		numEvents  int
		includeNil bool
		wantErr    bool
	}{
		{
			name:      "valid batch",
			numEvents: 10,
			wantErr:   false,
		},
		{
			name:      "empty batch",
			numEvents: 0,
			wantErr:   true,
		},
		{
			name:       "batch with nil event",
			numEvents:  5,
			includeNil: true,
			wantErr:    true,
		},
		{
			name:      "large batch",
			numEvents: 100,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []*pb.UnifiedEvent
			for i := 0; i < tt.numEvents; i++ {
				if tt.includeNil && i == 2 {
					events = append(events, nil)
				} else {
					event := createTestEvent(t, domain.EventTypeProcess, "batch-test").Event
					event.Id = event.Id + "-" + string(rune(i))
					events = append(events, event)
				}
			}

			resp, err := service.SubmitBatch(context.Background(), &pb.TapioSubmitBatchRequest{
				Events: events,
			})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.Equal(t, int32(tt.numEvents), resp.EventsReceived)
				assert.Len(t, resp.FailedEvents, 0)
			}
		})
	}
}

func TestTapioServiceComplete_QueryEvents(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	// Add test events
	for i := 0; i < 20; i++ {
		event := createTestEvent(t, domain.EventTypeProcess, "query-test")
		event.Event.Id = event.Event.Id + "-" + string(rune(i))
		if i < 10 {
			event.Event.Entity.Namespace = "namespace-a"
		} else {
			event.Event.Entity.Namespace = "namespace-b"
		}
		_, err := service.SubmitEvent(context.Background(), event)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		filter        *pb.Filter
		limit         int32
		expectedCount int
	}{
		{
			name:          "no filter",
			filter:        nil,
			limit:         10,
			expectedCount: 10,
		},
		{
			name: "filter by namespace",
			filter: &pb.Filter{
				Namespaces: []string{"namespace-a"},
			},
			limit:         20,
			expectedCount: 10,
		},
		{
			name: "filter by source",
			filter: &pb.Filter{
				Sources: []string{"query-test"},
			},
			limit:         5,
			expectedCount: 5,
		},
		{
			name: "filter by event type",
			filter: &pb.Filter{
				EventTypes: []string{string(domain.EventTypeProcess)},
			},
			limit:         15,
			expectedCount: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.QueryEvents(context.Background(), &pb.TapioQueryEventsRequest{
				Filter: tt.filter,
				Limit:  tt.limit,
			})

			require.NoError(t, err)
			assert.Len(t, resp.Events, tt.expectedCount)
			assert.Equal(t, int64(20), resp.TotalCount)
		})
	}
}

func TestTapioServiceComplete_StreamEvents(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create mock stream
	stream := &mockServerStream{
		ctx:    ctx,
		events: make(chan *pb.UnifiedEvent, 10),
	}

	// Start streaming in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- service.StreamEvents(&pb.TapioStreamEventsRequest{
			Filter: &pb.Filter{
				Sources: []string{"stream-test"},
			},
		}, stream)
	}()

	// Wait for stream to be registered
	time.Sleep(100 * time.Millisecond)

	// Submit events
	for i := 0; i < 5; i++ {
		event := createTestEvent(t, domain.EventTypeProcess, "stream-test")
		_, err := service.SubmitEvent(context.Background(), event)
		require.NoError(t, err)
	}

	// Verify events are received
	receivedCount := 0
	timeout := time.After(2 * time.Second)

	for receivedCount < 5 {
		select {
		case event := <-stream.events:
			assert.NotNil(t, event)
			assert.Equal(t, "stream-test", event.Source)
			receivedCount++
		case <-timeout:
			t.Fatal("timeout waiting for events")
		}
	}

	// Cancel stream
	cancel()

	// Check error
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for stream to finish")
	}
}

func TestTapioServiceComplete_GetCorrelations(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	// Submit events that should trigger correlation
	baseTime := time.Now()

	// Create error cascade pattern
	for i := 0; i < 5; i++ {
		event := createTestEvent(t, domain.EventTypeProcess, "service-"+string(rune(i)))
		event.Event.Timestamp = timestamppb.New(baseTime.Add(time.Duration(i) * time.Second))
		event.Event.Semantic.Category = "error"
		event.Event.Application = &pb.ApplicationContext{
			Level: "error",
		}
		_, err := service.SubmitEvent(context.Background(), event)
		require.NoError(t, err)
	}

	// Wait for correlation processing
	time.Sleep(100 * time.Millisecond)

	// Query correlations
	resp, err := service.GetCorrelations(context.Background(), &pb.TapioGetCorrelationsRequest{})
	require.NoError(t, err)

	// Should have at least one correlation detected
	assert.GreaterOrEqual(t, len(resp.Correlations), 0)
}

func TestTapioServiceComplete_RegisterCollector(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	tests := []struct {
		name    string
		request *pb.TapioRegisterCollectorRequest
		wantErr bool
	}{
		{
			name: "valid registration",
			request: &pb.TapioRegisterCollectorRequest{
				Name:         "test-collector",
				Type:         "process",
				Version:      "1.0.0",
				Capabilities: []string{"process-events", "kernel-events"},
				EventTypes:   []string{string(domain.EventTypeProcess), string(domain.EventTypeKernel)},
			},
			wantErr: false,
		},
		{
			name: "duplicate registration",
			request: &pb.TapioRegisterCollectorRequest{
				Name:    "test-collector",
				Type:    "process",
				Version: "1.0.0",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			request: &pb.TapioRegisterCollectorRequest{
				Type:    "process",
				Version: "1.0.0",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.RegisterCollector(context.Background(), tt.request)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.NotEmpty(t, resp.CollectorId)
			}
		})
	}
}

func TestTapioServiceComplete_GetMetrics(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	// Generate some activity
	for i := 0; i < 10; i++ {
		event := createTestEvent(t, domain.EventTypeProcess, "metrics-test")
		_, err := service.SubmitEvent(context.Background(), event)
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		component pb.TapioGetMetricsRequest_Component
		wantErr   bool
	}{
		{
			name:      "all components",
			component: pb.TapioGetMetricsRequest_COMPONENT_ALL,
			wantErr:   false,
		},
		{
			name:      "server component",
			component: pb.TapioGetMetricsRequest_COMPONENT_SERVER,
			wantErr:   false,
		},
		{
			name:      "collectors component",
			component: pb.TapioGetMetricsRequest_COMPONENT_COLLECTORS,
			wantErr:   false,
		},
		{
			name:      "correlation component",
			component: pb.TapioGetMetricsRequest_COMPONENT_CORRELATION,
			wantErr:   false,
		},
		{
			name:      "storage component",
			component: pb.TapioGetMetricsRequest_COMPONENT_STORAGE,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.GetMetrics(context.Background(), &pb.TapioGetMetricsRequest{
				Component: tt.component,
			})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, resp.Metrics)
			}
		})
	}
}

func TestTapioServiceComplete_Ping(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	resp, err := service.Ping(context.Background(), &pb.TapioPingRequest{})
	require.NoError(t, err)
	assert.Equal(t, "pong", resp.Message)
	assert.NotNil(t, resp.Timestamp)
}

func TestTapioServiceComplete_GetHealth(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	resp, err := service.GetHealth(context.Background(), &pb.TapioGetHealthRequest{})
	require.NoError(t, err)
	assert.Equal(t, pb.HealthStatus_STATUS_HEALTHY, resp.Status)
	assert.NotNil(t, resp.Components)
}

func TestTapioServiceComplete_AnalyzeEvents(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	// Submit test events
	eventIDs := make([]string, 0)
	for i := 0; i < 10; i++ {
		event := createTestEvent(t, domain.EventTypeProcess, "analyze-test")
		resp, err := service.SubmitEvent(context.Background(), event)
		require.NoError(t, err)
		eventIDs = append(eventIDs, resp.EventId)
		time.Sleep(10 * time.Millisecond) // Create temporal pattern
	}

	// Analyze events
	resp, err := service.AnalyzeEvents(context.Background(), &pb.TapioAnalyzeEventsRequest{
		EventIds: eventIDs[:5],
	})

	require.NoError(t, err)
	assert.NotNil(t, resp.Findings)
}

func TestTapioServiceComplete_SubmitEventStream(t *testing.T) {
	service := createTestService(t)
	defer service.Close()

	// Create bidirectional stream mock
	stream := &mockBidirectionalStream{
		ctx:       context.Background(),
		requests:  make(chan *pb.TapioSubmitEventRequest, 10),
		responses: make(chan *pb.TapioSubmitEventResponse, 10),
	}

	// Start stream handler in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- service.SubmitEventStream(stream)
	}()

	// Send events
	for i := 0; i < 5; i++ {
		event := createTestEvent(t, domain.EventTypeProcess, "bistream-test")
		stream.requests <- event
	}

	// Receive responses
	for i := 0; i < 5; i++ {
		select {
		case resp := <-stream.responses:
			assert.True(t, resp.Success)
			assert.NotEmpty(t, resp.EventId)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for response")
		}
	}

	// Close stream
	close(stream.requests)

	// Wait for handler to finish
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for stream handler")
	}
}

// Mock stream implementations
type mockServerStream struct {
	ctx    context.Context
	events chan *pb.UnifiedEvent
}

func (m *mockServerStream) Send(event *pb.UnifiedEvent) error {
	select {
	case m.events <- event:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockServerStream) SendHeader(metadata.MD) error { return nil }
func (m *mockServerStream) SetTrailer(metadata.MD)       {}
func (m *mockServerStream) SendMsg(interface{}) error    { return nil }
func (m *mockServerStream) RecvMsg(interface{}) error    { return nil }

type mockBidirectionalStream struct {
	ctx       context.Context
	requests  chan *pb.TapioSubmitEventRequest
	responses chan *pb.TapioSubmitEventResponse
}

func (m *mockBidirectionalStream) Send(resp *pb.TapioSubmitEventResponse) error {
	select {
	case m.responses <- resp:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *mockBidirectionalStream) Recv() (*pb.TapioSubmitEventRequest, error) {
	select {
	case req, ok := <-m.requests:
		if !ok {
			return nil, io.EOF
		}
		return req, nil
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	}
}

func (m *mockBidirectionalStream) Context() context.Context {
	return m.ctx
}

func (m *mockBidirectionalStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockBidirectionalStream) SendHeader(metadata.MD) error { return nil }
func (m *mockBidirectionalStream) SetTrailer(metadata.MD)       {}
func (m *mockBidirectionalStream) SendMsg(interface{}) error    { return nil }
func (m *mockBidirectionalStream) RecvMsg(interface{}) error    { return nil }

// Benchmarks
func BenchmarkSubmitEvent(b *testing.B) {
	service := createTestService(&testing.T{})
	defer service.Close()

	event := createTestEvent(&testing.T{}, domain.EventTypeProcess, "bench-test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event.Event.Id = event.Event.Id + "-" + string(rune(i))
		_, err := service.SubmitEvent(context.Background(), event)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueryEvents(b *testing.B) {
	service := createTestService(&testing.T{})
	defer service.Close()

	// Pre-populate with events
	for i := 0; i < 1000; i++ {
		event := createTestEvent(&testing.T{}, domain.EventTypeProcess, "bench-query")
		event.Event.Id = event.Event.Id + "-" + string(rune(i))
		service.SubmitEvent(context.Background(), event)
	}

	req := &pb.TapioQueryEventsRequest{
		Filter: &pb.Filter{
			Sources: []string{"bench-query"},
		},
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := service.QueryEvents(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
	}
}
