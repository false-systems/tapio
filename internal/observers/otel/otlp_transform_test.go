package otel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

func TestTransformSpansToOTLP(t *testing.T) {
	t.Run("Empty spans return nil", func(t *testing.T) {
		result, err := TransformSpansToOTLP(nil)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("Transforms single span", func(t *testing.T) {
		spans := []*domain.OTELSpanData{
			{
				TraceID:      "1234567890abcdef1234567890abcdef",
				SpanID:       "1234567890abcdef",
				ParentSpanID: "abcdef1234567890",
				ServiceName:  "test-service",
				Name:         "GET /api/users",
				Kind:         "SERVER",
				StartTime:    time.Now(),
				EndTime:      time.Now().Add(100 * time.Millisecond),
				StatusCode:   "OK",
			},
		}

		result, err := TransformSpansToOTLP(spans)
		require.NoError(t, err)
		require.Len(t, result, 1)

		otlpSpan := result[0]
		assert.Equal(t, "GET /api/users", otlpSpan.Name())
		assert.Equal(t, trace.SpanKindServer, otlpSpan.SpanKind())
	})

	t.Run("Skips nil spans", func(t *testing.T) {
		spans := []*domain.OTELSpanData{
			nil,
			{
				TraceID:     "1234567890abcdef1234567890abcdef",
				SpanID:      "1234567890abcdef",
				ServiceName: "test-service",
				Name:        "test",
			},
			nil,
		}

		result, err := TransformSpansToOTLP(spans)
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})
}

func TestCreateOTLPSpan(t *testing.T) {
	t.Run("Creates span with all fields", func(t *testing.T) {
		now := time.Now()
		span := &domain.OTELSpanData{
			TraceID:        "1234567890abcdef1234567890abcdef",
			SpanID:         "1234567890abcdef",
			ParentSpanID:   "abcdef1234567890",
			ServiceName:    "api-service",
			Name:           "POST /api/orders",
			Kind:           "SERVER",
			StartTime:      now,
			EndTime:        now.Add(200 * time.Millisecond),
			StatusCode:     "OK",
			StatusMessage:  "Request completed",
			HTTPMethod:     "POST",
			HTTPURL:        "/api/orders",
			HTTPStatusCode: 201,
			Attributes: map[string]string{
				"user_id":    "12345",
				"request_id": "req-abc",
			},
		}

		otlpSpan := createOTLPSpan(span)

		assert.Equal(t, "POST /api/orders", otlpSpan.Name())
		assert.Equal(t, trace.SpanKindServer, otlpSpan.SpanKind())
		assert.Equal(t, now, otlpSpan.StartTime())
		assert.Equal(t, now.Add(200*time.Millisecond), otlpSpan.EndTime())
		assert.Equal(t, codes.Ok, otlpSpan.Status().Code)
		assert.Equal(t, "Request completed", otlpSpan.Status().Description)
	})

	t.Run("Creates span with HTTP attributes", func(t *testing.T) {
		span := &domain.OTELSpanData{
			TraceID:        "1234567890abcdef1234567890abcdef",
			SpanID:         "1234567890abcdef",
			ServiceName:    "web",
			Name:           "GET /health",
			HTTPMethod:     "GET",
			HTTPURL:        "/health",
			HTTPStatusCode: 200,
		}

		otlpSpan := createOTLPSpan(span)
		attrs := otlpSpan.Attributes()

		// Check for HTTP attributes
		hasHTTPMethod := false
		hasHTTPTarget := false
		hasHTTPStatus := false

		for _, attr := range attrs {
			if attr.Key == semconv.HTTPMethodKey {
				hasHTTPMethod = true
				assert.Equal(t, "GET", attr.Value.AsString())
			}
			if attr.Key == semconv.HTTPURLKey {
				hasHTTPTarget = true
				assert.Equal(t, "/health", attr.Value.AsString())
			}
			if attr.Key == semconv.HTTPStatusCodeKey {
				hasHTTPStatus = true
				assert.Equal(t, int64(200), attr.Value.AsInt64())
			}
		}

		assert.True(t, hasHTTPMethod)
		assert.True(t, hasHTTPTarget)
		assert.True(t, hasHTTPStatus)
	})

	t.Run("Creates span with gRPC attributes", func(t *testing.T) {
		span := &domain.OTELSpanData{
			TraceID:     "1234567890abcdef1234567890abcdef",
			SpanID:      "1234567890abcdef",
			ServiceName: "grpc-service",
			Name:        "GetUser",
			RPCMethod:   "GetUser",
			RPCService:  "UserService",
		}

		otlpSpan := createOTLPSpan(span)
		attrs := otlpSpan.Attributes()

		hasRPCMethod := false
		hasRPCService := false

		for _, attr := range attrs {
			if attr.Key == semconv.RPCMethodKey {
				hasRPCMethod = true
				assert.Equal(t, "GetUser", attr.Value.AsString())
			}
			if attr.Key == semconv.RPCServiceKey {
				hasRPCService = true
				assert.Equal(t, "UserService", attr.Value.AsString())
			}
		}

		assert.True(t, hasRPCMethod)
		assert.True(t, hasRPCService)
	})

	t.Run("Creates span with K8s resource attributes", func(t *testing.T) {
		span := &domain.OTELSpanData{
			TraceID:       "1234567890abcdef1234567890abcdef",
			SpanID:        "1234567890abcdef",
			ServiceName:   "k8s-service",
			Name:          "process",
			K8sPodName:    "pod-123",
			K8sNamespace:  "production",
			K8sDeployment: "api-deployment",
			ContainerName: "api",
		}

		otlpSpan := createOTLPSpan(span)
		resource := otlpSpan.Resource()
		attrs := resource.Attributes()

		hasK8sPod := false
		hasK8sNamespace := false

		for _, attr := range attrs {
			if attr.Key == semconv.K8SPodNameKey {
				hasK8sPod = true
				assert.Equal(t, "pod-123", attr.Value.AsString())
			}
			if attr.Key == semconv.K8SNamespaceNameKey {
				hasK8sNamespace = true
				assert.Equal(t, "production", attr.Value.AsString())
			}
		}

		assert.True(t, hasK8sPod)
		assert.True(t, hasK8sNamespace)
	})
}

func TestMapSpanKind(t *testing.T) {
	tests := []struct {
		kind     string
		expected trace.SpanKind
	}{
		{"CLIENT", trace.SpanKindClient},
		{"SERVER", trace.SpanKindServer},
		{"PRODUCER", trace.SpanKindProducer},
		{"CONSUMER", trace.SpanKindConsumer},
		{"INTERNAL", trace.SpanKindInternal},
		{"UNKNOWN", trace.SpanKindUnspecified},
		{"", trace.SpanKindUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := mapSpanKind(tt.kind)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapStatus(t *testing.T) {
	tests := []struct {
		status   string
		expected codes.Code
	}{
		{"OK", codes.Ok},
		{"ERROR", codes.Error},
		{"UNSET", codes.Unset},
		{"", codes.Unset},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := mapStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildAttributes(t *testing.T) {
	span := &domain.OTELSpanData{
		ServiceName:    "test-service",
		HTTPMethod:     "POST",
		HTTPURL:        "/api/v1/users",
		HTTPStatusCode: 201,
		DBSystem:       "postgresql",
		DBStatement:    "INSERT INTO users...",
		Attributes: map[string]string{
			"custom1": "value1",
			"custom2": "value2",
		},
	}

	attrs := buildAttributes(span)

	// Should have service name + HTTP (3) + DB (2) + custom (2) = 8 attributes
	assert.GreaterOrEqual(t, len(attrs), 8)

	// Verify custom attributes are included
	hasCustom1 := false
	hasCustom2 := false

	for _, attr := range attrs {
		if attr.Key == "custom1" {
			hasCustom1 = true
			assert.Equal(t, "value1", attr.Value.AsString())
		}
		if attr.Key == "custom2" {
			hasCustom2 = true
			assert.Equal(t, "value2", attr.Value.AsString())
		}
	}

	assert.True(t, hasCustom1)
	assert.True(t, hasCustom2)
}

func TestBuildResource(t *testing.T) {
	span := &domain.OTELSpanData{
		ServiceName:   "k8s-app",
		K8sPodName:    "app-pod-123",
		K8sNamespace:  "default",
		K8sDeployment: "app-deployment",
		ContainerName: "app",
	}

	resource := buildResource(span)
	assert.NotNil(t, resource)

	attrs := resource.Attributes()
	assert.Greater(t, len(attrs), 0)

	// Verify K8s attributes
	hasService := false
	hasPod := false

	for _, attr := range attrs {
		if attr.Key == semconv.ServiceNameKey {
			hasService = true
			assert.Equal(t, "k8s-app", attr.Value.AsString())
		}
		if attr.Key == semconv.K8SPodNameKey {
			hasPod = true
			assert.Equal(t, "app-pod-123", attr.Value.AsString())
		}
	}

	assert.True(t, hasService)
	assert.True(t, hasPod)
}
