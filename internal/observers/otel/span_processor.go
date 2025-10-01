package otel

import (
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
)

// TransformSpan converts OTEL SDK span to domain.OTELSpanData with full attribute extraction
func TransformSpan(span trace.ReadOnlySpan) *domain.OTELSpanData {
	if span == nil {
		return nil
	}

	spanCtx := span.SpanContext()

	data := &domain.OTELSpanData{
		// Identity
		TraceID:      spanCtx.TraceID().String(),
		SpanID:       spanCtx.SpanID().String(),
		ParentSpanID: span.Parent().SpanID().String(),

		// Basic info
		Name:          span.Name(),
		Kind:          span.SpanKind().String(),
		StartTime:     span.StartTime(),
		EndTime:       span.EndTime(),
		DurationNanos: span.EndTime().Sub(span.StartTime()).Nanoseconds(),

		// Status
		StatusCode:    mapStatusCode(span.Status().Code),
		StatusMessage: span.Status().Description,

		// Service info
		ServiceName: extractServiceName(span),
	}

	// Extract attributes by category
	attrs := attributesToMap(span.Attributes())
	extractHTTPAttributes(data, attrs)
	extractGRPCAttributes(data, attrs)
	extractDBAttributes(data, attrs)
	extractK8sAttributes(data, attrs)
	extractCustomAttributes(data, attrs)

	// Extract span events
	if events := extractSpanEvents(span.Events()); events != nil {
		data.Events = events
	}

	return data
}

// mapStatusCode converts OTEL status code to string
func mapStatusCode(code codes.Code) string {
	switch code {
	case codes.Ok:
		return "OK"
	case codes.Error:
		return "ERROR"
	default:
		return "UNSET"
	}
}

// extractServiceName gets service name from resource attributes
func extractServiceName(span trace.ReadOnlySpan) string {
	resource := span.Resource()
	for _, attr := range resource.Attributes() {
		if attr.Key == "service.name" {
			return attr.Value.AsString()
		}
	}
	return "unknown-service"
}

// attributesToMap converts OTEL attributes to map for easier lookup
func attributesToMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		m[string(attr.Key)] = attr.Value
	}
	return m
}

// extractHTTPAttributes extracts HTTP-specific span attributes
func extractHTTPAttributes(data *domain.OTELSpanData, attrs map[string]attribute.Value) {
	if val, ok := attrs["http.method"]; ok {
		data.HTTPMethod = val.AsString()
	}
	if val, ok := attrs["http.target"]; ok {
		data.HTTPURL = val.AsString()
	}
	if val, ok := attrs["http.status_code"]; ok {
		data.HTTPStatusCode = int(val.AsInt64())
	}
}

// extractGRPCAttributes extracts gRPC-specific span attributes
func extractGRPCAttributes(data *domain.OTELSpanData, attrs map[string]attribute.Value) {
	if val, ok := attrs["rpc.method"]; ok {
		data.RPCMethod = val.AsString()
	}
	if val, ok := attrs["rpc.service"]; ok {
		data.RPCService = val.AsString()
	}
}

// extractDBAttributes extracts database-specific span attributes
func extractDBAttributes(data *domain.OTELSpanData, attrs map[string]attribute.Value) {
	if val, ok := attrs["db.system"]; ok {
		data.DBSystem = val.AsString()
	}
	if val, ok := attrs["db.statement"]; ok {
		data.DBStatement = val.AsString()
	}
}

// extractK8sAttributes extracts Kubernetes resource attributes
func extractK8sAttributes(data *domain.OTELSpanData, attrs map[string]attribute.Value) {
	if val, ok := attrs["k8s.pod.name"]; ok {
		data.K8sPodName = val.AsString()
	}
	if val, ok := attrs["k8s.namespace.name"]; ok {
		data.K8sNamespace = val.AsString()
	}
	if val, ok := attrs["k8s.deployment.name"]; ok {
		data.K8sDeployment = val.AsString()
	}
	if val, ok := attrs["k8s.container.name"]; ok {
		data.ContainerName = val.AsString()
	}
}

// extractCustomAttributes extracts remaining custom attributes
func extractCustomAttributes(data *domain.OTELSpanData, attrs map[string]attribute.Value) {
	// Well-known semantic conventions already extracted
	// Store remaining custom attributes
	customAttrs := make(map[string]string)

	// Skip already-extracted semantic conventions
	skipKeys := map[string]bool{
		"http.method":         true,
		"http.target":         true,
		"http.status_code":    true,
		"rpc.method":          true,
		"rpc.service":         true,
		"db.system":           true,
		"db.statement":        true,
		"k8s.pod.name":        true,
		"k8s.namespace.name":  true,
		"k8s.deployment.name": true,
		"k8s.container.name":  true,
	}

	for key, val := range attrs {
		if !skipKeys[key] {
			customAttrs[key] = val.AsString()
		}
	}

	if len(customAttrs) > 0 {
		data.Attributes = customAttrs
	}
}

// extractSpanEvents converts OTEL span events to domain format
func extractSpanEvents(events []trace.Event) []domain.OTELSpanEvent {
	if len(events) == 0 {
		return nil
	}

	domainEvents := make([]domain.OTELSpanEvent, 0, len(events))
	for _, evt := range events {
		domainEvt := domain.OTELSpanEvent{
			Timestamp: evt.Time,
			Name:      evt.Name,
		}

		// Extract event attributes
		if len(evt.Attributes) > 0 {
			attrs := make(map[string]string, len(evt.Attributes))
			for _, attr := range evt.Attributes {
				attrs[string(attr.Key)] = attr.Value.AsString()
			}
			domainEvt.Attributes = attrs
		}

		domainEvents = append(domainEvents, domainEvt)
	}

	return domainEvents
}

// ExtractServiceDependency extracts service-to-service relationship from span
// Returns (fromService, toService, found)
func ExtractServiceDependency(span *domain.OTELSpanData) (string, string, bool) {
	if span == nil {
		return "", "", false
	}

	// For client spans, service calls another service
	// Note: SpanKind().String() returns lowercase "client", "producer", etc.
	if span.Kind == "client" || span.Kind == "producer" {
		fromService := span.ServiceName
		toService := ""

		// Try to get target service from RPC service attribute
		if span.RPCService != "" {
			toService = span.RPCService
		}

		if fromService != "" && toService != "" && fromService != toService {
			return fromService, toService, true
		}
	}

	return "", "", false
}

// ProcessSpanBatch transforms multiple spans and extracts dependencies
func ProcessSpanBatch(spans []trace.ReadOnlySpan) ([]*domain.OTELSpanData, map[string]map[string]int) {
	if len(spans) == 0 {
		return nil, nil
	}

	domainSpans := make([]*domain.OTELSpanData, 0, len(spans))
	dependencies := make(map[string]map[string]int)

	for _, span := range spans {
		domainSpan := TransformSpan(span)
		if domainSpan != nil {
			domainSpans = append(domainSpans, domainSpan)

			// Extract service dependency
			if from, to, found := ExtractServiceDependency(domainSpan); found {
				if dependencies[from] == nil {
					dependencies[from] = make(map[string]int)
				}
				dependencies[from][to]++
			}
		}
	}

	return domainSpans, dependencies
}
