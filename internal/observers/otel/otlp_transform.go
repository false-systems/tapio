package otel

import (
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// TransformSpansToOTLP converts domain spans to OTLP SDK format
func TransformSpansToOTLP(spans []*domain.OTELSpanData) ([]tracesdk.ReadOnlySpan, error) {
	if len(spans) == 0 {
		return nil, nil
	}

	result := make([]tracesdk.ReadOnlySpan, 0, len(spans))

	for _, span := range spans {
		if span == nil {
			continue
		}

		// Create OTLP span
		otlpSpan := createOTLPSpan(span)
		result = append(result, otlpSpan)
	}

	return result, nil
}

// createOTLPSpan creates an OTLP SDK span from domain data
func createOTLPSpan(span *domain.OTELSpanData) tracesdk.ReadOnlySpan {
	// Parse trace and span IDs
	traceID, _ := trace.TraceIDFromHex(span.TraceID)
	spanID, _ := trace.SpanIDFromHex(span.SpanID)
	parentSpanID, _ := trace.SpanIDFromHex(span.ParentSpanID)

	// Build attributes from domain data
	attrs := buildAttributes(span)

	// Build resource with K8s info
	res := buildResource(span)

	// Determine span kind
	kind := mapSpanKind(span.Kind)

	// Map status
	status := mapStatus(span.StatusCode)

	// Create span snapshot
	snapshot := tracetest.SpanStub{
		Name: span.Name,
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: trace.FlagsSampled,
		}),
		Parent: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  parentSpanID,
		}),
		SpanKind:   kind,
		StartTime:  span.StartTime,
		EndTime:    span.EndTime,
		Attributes: attrs,
		Status: tracesdk.Status{
			Code:        status,
			Description: span.StatusMessage,
		},
		Resource: res,
		InstrumentationLibrary: instrumentation.Library{
			Name:    "tapio-otel-observer",
			Version: "1.0.0",
		},
	}

	return snapshot.Snapshot()
}

// buildAttributes creates OTLP attributes from domain span
func buildAttributes(span *domain.OTELSpanData) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(span.ServiceName),
	}

	// Add HTTP attributes
	if span.HTTPMethod != "" {
		attrs = append(attrs, semconv.HTTPMethod(span.HTTPMethod))
	}
	if span.HTTPURL != "" {
		attrs = append(attrs, semconv.HTTPURL(span.HTTPURL))
	}
	if span.HTTPStatusCode > 0 {
		attrs = append(attrs, semconv.HTTPStatusCode(int(span.HTTPStatusCode)))
	}

	// Add gRPC attributes
	if span.RPCMethod != "" {
		attrs = append(attrs, semconv.RPCMethod(span.RPCMethod))
	}
	if span.RPCService != "" {
		attrs = append(attrs, semconv.RPCService(span.RPCService))
	}

	// Add database attributes
	if span.DBSystem != "" {
		attrs = append(attrs, attribute.String("db.system", span.DBSystem))
	}
	if span.DBStatement != "" {
		attrs = append(attrs, semconv.DBStatement(span.DBStatement))
	}

	// Add custom attributes
	for k, v := range span.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return attrs
}

// buildResource creates OTLP resource with K8s attributes
func buildResource(span *domain.OTELSpanData) *resource.Resource {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(span.ServiceName),
	}

	// Add K8s resource attributes
	if span.K8sPodName != "" {
		attrs = append(attrs, semconv.K8SPodName(span.K8sPodName))
	}
	if span.K8sNamespace != "" {
		attrs = append(attrs, semconv.K8SNamespaceName(span.K8sNamespace))
	}
	if span.K8sDeployment != "" {
		attrs = append(attrs, semconv.K8SDeploymentName(span.K8sDeployment))
	}
	if span.ContainerName != "" {
		attrs = append(attrs, semconv.K8SContainerName(span.ContainerName))
	}

	return resource.NewWithAttributes(
		semconv.SchemaURL,
		attrs...,
	)
}

// mapSpanKind converts domain span kind to OTLP
func mapSpanKind(kind string) trace.SpanKind {
	switch kind {
	case "CLIENT":
		return trace.SpanKindClient
	case "SERVER":
		return trace.SpanKindServer
	case "PRODUCER":
		return trace.SpanKindProducer
	case "CONSUMER":
		return trace.SpanKindConsumer
	case "INTERNAL":
		return trace.SpanKindInternal
	default:
		return trace.SpanKindUnspecified
	}
}

// mapStatus converts domain status to OTLP status code
func mapStatus(statusCode string) codes.Code {
	switch statusCode {
	case "OK":
		return codes.Ok
	case "ERROR":
		return codes.Error
	default:
		return codes.Unset
	}
}
