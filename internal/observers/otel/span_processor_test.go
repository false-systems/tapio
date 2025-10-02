package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// testSpanSetup encapsulates span creation infrastructure
type testSpanSetup struct {
	recorder *tracetest.SpanRecorder
	tracer   oteltrace.Tracer
	ctx      context.Context
}

// newTestSpanSetup creates reusable span creation infrastructure
func newTestSpanSetup(serviceName string) *testSpanSetup {
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	)

	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSpanProcessor(recorder),
	)

	return &testSpanSetup{
		recorder: recorder,
		tracer:   tp.Tracer("test"),
		ctx:      context.Background(),
	}
}

// createSpan creates a single span with the given parameters
func (s *testSpanSetup) createSpan(name string, kind oteltrace.SpanKind, attrs []attribute.KeyValue) {
	_, span := s.tracer.Start(s.ctx, name, oteltrace.WithSpanKind(kind), oteltrace.WithAttributes(attrs...))
	span.End()
}

// createSpanWithStatus creates a span with a specific status
func (s *testSpanSetup) createSpanWithStatus(name string, kind oteltrace.SpanKind, statusCode codes.Code, statusMsg string) {
	_, span := s.tracer.Start(s.ctx, name, oteltrace.WithSpanKind(kind))
	span.SetStatus(statusCode, statusMsg)
	span.End()
}

// getSpans returns all recorded spans
func (s *testSpanSetup) getSpans() []trace.ReadOnlySpan {
	return s.recorder.Ended()
}

// Helper to create a single test span (backward compatible)
func createTestSpan(t *testing.T, name string, kind oteltrace.SpanKind, attrs []attribute.KeyValue) trace.ReadOnlySpan {
	t.Helper()

	setup := newTestSpanSetup("test-service")
	setup.createSpan(name, kind, attrs)

	spans := setup.getSpans()
	require.Len(t, spans, 1)
	return spans[0]
}

func TestTransformSpan_BasicFields(t *testing.T) {
	span := createTestSpan(t, "test-span", oteltrace.SpanKindClient, nil)

	data := TransformSpan(span)

	require.NotNil(t, data)
	assert.Equal(t, "test-span", data.Name)
	assert.Equal(t, "client", data.Kind)
	assert.Equal(t, "test-service", data.ServiceName)
	assert.Equal(t, "UNSET", data.StatusCode)
	assert.Equal(t, "", data.StatusMessage)
	assert.Greater(t, data.DurationNanos, int64(0))
}

func TestTransformSpan_NilSpan(t *testing.T) {
	data := TransformSpan(nil)
	assert.Nil(t, data)
}

func TestTransformSpan_HTTPAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("http.method", "GET"),
		attribute.String("http.target", "/api/users"),
		attribute.Int64("http.status_code", 200),
	}

	span := createTestSpan(t, "http-request", oteltrace.SpanKindServer, attrs)
	data := TransformSpan(span)

	require.NotNil(t, data)
	assert.Equal(t, "GET", data.HTTPMethod)
	assert.Equal(t, "/api/users", data.HTTPURL)
	assert.Equal(t, 200, data.HTTPStatusCode)
}

func TestTransformSpan_GRPCAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("rpc.method", "GetUser"),
		attribute.String("rpc.service", "user.UserService"),
	}

	span := createTestSpan(t, "grpc-call", oteltrace.SpanKindClient, attrs)
	data := TransformSpan(span)

	require.NotNil(t, data)
	assert.Equal(t, "GetUser", data.RPCMethod)
	assert.Equal(t, "user.UserService", data.RPCService)
}

func TestTransformSpan_DBAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "SELECT * FROM users"),
	}

	span := createTestSpan(t, "db-query", oteltrace.SpanKindClient, attrs)
	data := TransformSpan(span)

	require.NotNil(t, data)
	assert.Equal(t, "postgresql", data.DBSystem)
	assert.Equal(t, "SELECT * FROM users", data.DBStatement)
}

func TestTransformSpan_K8sAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("k8s.pod.name", "app-pod-123"),
		attribute.String("k8s.namespace.name", "production"),
		attribute.String("k8s.deployment.name", "app-deployment"),
		attribute.String("k8s.container.name", "main-container"),
	}

	span := createTestSpan(t, "k8s-span", oteltrace.SpanKindServer, attrs)
	data := TransformSpan(span)

	require.NotNil(t, data)
	assert.Equal(t, "app-pod-123", data.K8sPodName)
	assert.Equal(t, "production", data.K8sNamespace)
	assert.Equal(t, "app-deployment", data.K8sDeployment)
	assert.Equal(t, "main-container", data.ContainerName)
}

func TestTransformSpan_CustomAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("http.method", "POST"),
		attribute.String("custom.field", "custom-value"),
		attribute.String("app.version", "1.2.3"),
	}

	span := createTestSpan(t, "custom-span", oteltrace.SpanKindServer, attrs)
	data := TransformSpan(span)

	require.NotNil(t, data)
	assert.Equal(t, "POST", data.HTTPMethod)
	require.NotNil(t, data.Attributes)
	assert.Equal(t, "custom-value", data.Attributes["custom.field"])
	assert.Equal(t, "1.2.3", data.Attributes["app.version"])
	assert.NotContains(t, data.Attributes, "http.method")
}

func TestTransformSpan_ErrorStatus(t *testing.T) {
	setup := newTestSpanSetup("test-service")
	setup.createSpanWithStatus("error-span", oteltrace.SpanKindServer, codes.Error, "Internal error")

	spans := setup.getSpans()
	require.Len(t, spans, 1)

	data := TransformSpan(spans[0])

	require.NotNil(t, data)
	assert.Equal(t, "ERROR", data.StatusCode)
	assert.Equal(t, "Internal error", data.StatusMessage)
}

func TestExtractServiceDependency_ClientSpan(t *testing.T) {
	spanData := &domain.OTELSpanData{
		Kind:        "client",
		ServiceName: "frontend",
		RPCService:  "backend.UserService",
	}

	from, to, found := ExtractServiceDependency(spanData)

	assert.True(t, found)
	assert.Equal(t, "frontend", from)
	assert.Equal(t, "backend.UserService", to)
}

func TestExtractServiceDependency_ProducerSpan(t *testing.T) {
	spanData := &domain.OTELSpanData{
		Kind:        "producer",
		ServiceName: "order-service",
		RPCService:  "queue.OrderQueue",
	}

	from, to, found := ExtractServiceDependency(spanData)

	assert.True(t, found)
	assert.Equal(t, "order-service", from)
	assert.Equal(t, "queue.OrderQueue", to)
}

func TestExtractServiceDependency_ServerSpan(t *testing.T) {
	spanData := &domain.OTELSpanData{
		Kind:        "server",
		ServiceName: "backend",
		RPCService:  "backend.UserService",
	}

	from, to, found := ExtractServiceDependency(spanData)

	assert.False(t, found)
	assert.Equal(t, "", from)
	assert.Equal(t, "", to)
}

func TestExtractServiceDependency_SameService(t *testing.T) {
	spanData := &domain.OTELSpanData{
		Kind:        "client",
		ServiceName: "backend",
		RPCService:  "backend",
	}

	_, _, found := ExtractServiceDependency(spanData)

	assert.False(t, found)
}

func TestExtractServiceDependency_MissingFields(t *testing.T) {
	tests := []struct {
		name     string
		spanData *domain.OTELSpanData
	}{
		{
			name:     "nil span",
			spanData: nil,
		},
		{
			name: "missing service name",
			spanData: &domain.OTELSpanData{
				Kind:       "client",
				RPCService: "backend",
			},
		},
		{
			name: "missing rpc service",
			spanData: &domain.OTELSpanData{
				Kind:        "client",
				ServiceName: "frontend",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, found := ExtractServiceDependency(tt.spanData)
			assert.False(t, found)
			assert.Equal(t, "", from)
			assert.Equal(t, "", to)
		})
	}
}

func TestProcessSpanBatch_Empty(t *testing.T) {
	spans, deps := ProcessSpanBatch(nil)
	assert.Nil(t, spans)
	assert.Nil(t, deps)

	spans, deps = ProcessSpanBatch([]trace.ReadOnlySpan{})
	assert.Nil(t, spans)
	assert.Nil(t, deps)
}

func TestProcessSpanBatch_MultipleSpans(t *testing.T) {
	setup := newTestSpanSetup("test-service")

	setup.createSpan("span1", oteltrace.SpanKindClient, []attribute.KeyValue{
		attribute.String("rpc.service", "backend.UserService"),
	})
	setup.createSpan("span2", oteltrace.SpanKindServer, nil)

	readonlySpans := setup.getSpans()
	require.Len(t, readonlySpans, 2)

	spans, deps := ProcessSpanBatch(readonlySpans)

	require.Len(t, spans, 2)
	assert.Equal(t, "span1", spans[0].Name)
	assert.Equal(t, "span2", spans[1].Name)

	require.NotNil(t, deps)
	require.Contains(t, deps, "test-service")
	assert.Equal(t, 1, deps["test-service"]["backend.UserService"])
}

func TestProcessSpanBatch_DependencyCounting(t *testing.T) {
	setup := newTestSpanSetup("test-service")

	setup.createSpan("span1", oteltrace.SpanKindClient, []attribute.KeyValue{
		attribute.String("rpc.service", "backend"),
	})
	setup.createSpan("span2", oteltrace.SpanKindClient, []attribute.KeyValue{
		attribute.String("rpc.service", "backend"),
	})
	setup.createSpan("span3", oteltrace.SpanKindClient, []attribute.KeyValue{
		attribute.String("rpc.service", "cache"),
	})

	readonlySpans := setup.getSpans()
	require.Len(t, readonlySpans, 3)

	_, deps := ProcessSpanBatch(readonlySpans)

	require.NotNil(t, deps)
	require.Contains(t, deps, "test-service")
	assert.Equal(t, 2, deps["test-service"]["backend"])
	assert.Equal(t, 1, deps["test-service"]["cache"])
}

func TestMapStatusCode(t *testing.T) {
	tests := []struct {
		name     string
		code     codes.Code
		expected string
	}{
		{"ok", codes.Ok, "OK"},
		{"error", codes.Error, "ERROR"},
		{"unset", codes.Unset, "UNSET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapStatusCode(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractServiceName(t *testing.T) {
	span := createTestSpan(t, "test", oteltrace.SpanKindClient, nil)
	serviceName := extractServiceName(span)
	assert.Equal(t, "test-service", serviceName)
}

func TestExtractServiceName_Missing(t *testing.T) {
	// Create resource without service name
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		attribute.String("other.attribute", "value"),
	)

	// Create span recorder to capture readonly spans
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSpanProcessor(recorder),
	)

	// Create span
	tracer := tp.Tracer("test")
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test")
	span.End()

	// Get the readonly span
	spans := recorder.Ended()
	require.Len(t, spans, 1)

	serviceName := extractServiceName(spans[0])
	assert.Equal(t, "unknown-service", serviceName)
}
