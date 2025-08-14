package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// EventParser parses RawEvent to ObservationEvent
type EventParser struct {
	logger *zap.Logger

	// OTEL instrumentation - REQUIRED fields
	tracer              trace.Tracer
	eventsProcessed     metric.Int64Counter
	errorsTotal         metric.Int64Counter
	processingTime      metric.Float64Histogram
	parseSuccessCounter metric.Int64Counter
	parseErrorCounter   metric.Int64Counter
}

// NewEventParser creates a new parser with OTEL instrumentation
func NewEventParser(logger *zap.Logger) (*EventParser, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer("domain.event_parser")
	meter := otel.Meter("domain.event_parser")

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		"event_parser_events_processed_total",
		metric.WithDescription("Total events processed by the event parser"),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		"event_parser_errors_total",
		metric.WithDescription("Total errors in event parser"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		"event_parser_processing_duration_ms",
		metric.WithDescription("Processing duration for event parser in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	parseSuccessCounter, err := meter.Int64Counter(
		"event_parser_parse_success_total",
		metric.WithDescription("Total successful parse operations"),
	)
	if err != nil {
		logger.Warn("Failed to create parse success counter", zap.Error(err))
	}

	parseErrorCounter, err := meter.Int64Counter(
		"event_parser_parse_error_total",
		metric.WithDescription("Total parse error operations"),
	)
	if err != nil {
		logger.Warn("Failed to create parse error counter", zap.Error(err))
	}

	return &EventParser{
		logger:              logger,
		tracer:              tracer,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
		processingTime:      processingTime,
		parseSuccessCounter: parseSuccessCounter,
		parseErrorCounter:   parseErrorCounter,
	}, nil
}

// ParseEvent converts a RawEvent to ObservationEvent
func (p *EventParser) ParseEvent(ctx context.Context, raw collectors.RawEvent) (*ObservationEvent, error) {
	// Always start spans for operations
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_event")
	defer span.End()

	start := time.Now()
	defer func() {
		// Record processing time
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if p.processingTime != nil {
			p.processingTime.Record(ctx, duration, metric.WithAttributes(
				attribute.String("event_type", raw.Type),
			))
		}
	}()

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("raw_event.type", raw.Type),
		attribute.String("raw_event.trace_id", raw.TraceID),
		attribute.String("raw_event.span_id", raw.SpanID),
		attribute.Int("raw_event.data_size", len(raw.Data)),
	)

	// Create base observation event
	obsEvent := &ObservationEvent{
		ID:        p.generateEventID(),
		Timestamp: raw.Timestamp,
		Source:    raw.Type,
		Type:      p.mapEventType(raw.Type),
		Data:      make(map[string]string),
	}

	// Parse based on source type
	if err := p.parseByType(ctx, raw, obsEvent); err != nil {
		// Record error metrics
		if p.errorsTotal != nil {
			p.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "parse_failed"),
				attribute.String("event_type", raw.Type),
			))
		}
		if p.parseErrorCounter != nil {
			p.parseErrorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", raw.Type),
			))
		}

		// Record error in span
		span.SetAttributes(attribute.String("error", err.Error()))
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("failed to parse %s event: %w", raw.Type, err)
	}

	// Copy metadata to Data map (NO interface{})
	for k, v := range raw.Metadata {
		obsEvent.Data[k] = v
	}

	// Validate the result
	if err := obsEvent.Validate(); err != nil {
		// Record validation error metrics
		if p.errorsTotal != nil {
			p.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "validation_failed"),
				attribute.String("event_type", raw.Type),
			))
		}

		span.SetAttributes(attribute.String("validation_error", err.Error()))
		span.SetStatus(codes.Error, "validation failed")
		return nil, fmt.Errorf("validation failed for parsed event: %w", err)
	}

	// Record success metrics
	if p.eventsProcessed != nil {
		p.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", raw.Type),
			attribute.String("status", "success"),
		))
	}
	if p.parseSuccessCounter != nil {
		p.parseSuccessCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", raw.Type),
		))
	}

	span.SetAttributes(
		attribute.String("parsed_event.id", obsEvent.ID),
		attribute.String("parsed_event.type", obsEvent.Type),
		attribute.Bool("parsed_event.has_correlation_key", obsEvent.HasCorrelationKey()),
	)

	return obsEvent, nil
}

// generateEventID generates a unique event ID
func (p *EventParser) generateEventID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	return "evt_" + hex.EncodeToString(bytes)
}

// mapEventType maps collector type to standardized event type
func (p *EventParser) mapEventType(collectorType string) string {
	switch strings.ToLower(collectorType) {
	case "kernel", "ebpf":
		return "syscall"
	case "kubeapi", "kubernetes":
		return "k8s_event"
	case "dns":
		return "dns_query"
	case "systemd":
		return "service_event"
	case "cni":
		return "network_event"
	case "etcd":
		return "storage_event"
	default:
		return "unknown"
	}
}

// parseByType routes parsing to specific handler based on source type
func (p *EventParser) parseByType(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	switch strings.ToLower(raw.Type) {
	case "kernel", "ebpf":
		return p.parseKernelEvent(ctx, raw, obsEvent)
	case "kubeapi", "kubernetes":
		return p.parseKubeAPIEvent(ctx, raw, obsEvent)
	case "dns":
		return p.parseDNSEvent(ctx, raw, obsEvent)
	case "systemd":
		return p.parseSystemdEvent(ctx, raw, obsEvent)
	case "cni":
		return p.parseCNIEvent(ctx, raw, obsEvent)
	case "etcd":
		return p.parseEtcdEvent(ctx, raw, obsEvent)
	default:
		return p.parseGenericEvent(ctx, raw, obsEvent)
	}
}

// parseKernelEvent parses eBPF/kernel events
func (p *EventParser) parseKernelEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_kernel_event")
	defer span.End()

	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		// Try raw string parsing as fallback
		obsEvent.Action = stringPtr("kernel_raw")
		obsEvent.Data["raw_data"] = string(raw.Data)
		return nil
	}

	// Extract PID (required for kernel events)
	if pidVal, ok := data["pid"]; ok {
		if pid, err := p.extractInt32(pidVal); err == nil {
			obsEvent.PID = &pid
		}
	}

	// Extract container ID if available
	if containerID, ok := data["container_id"].(string); ok && containerID != "" {
		obsEvent.ContainerID = &containerID
	}

	// Extract syscall action
	if syscall, ok := data["syscall"].(string); ok {
		obsEvent.Action = &syscall
	} else {
		action := "kernel_event"
		obsEvent.Action = &action
	}

	// Extract target (filename, network address, etc.)
	if filename, ok := data["filename"].(string); ok && filename != "" {
		obsEvent.Target = &filename
	} else if addr, ok := data["address"].(string); ok && addr != "" {
		obsEvent.Target = &addr
	}

	// Extract result/return value
	if retVal, ok := data["return_value"]; ok {
		retStr := fmt.Sprintf("%v", retVal)
		obsEvent.Result = &retStr
	}

	// Store all fields as strings in Data map
	for k, v := range data {
		obsEvent.Data[k] = fmt.Sprintf("%v", v)
	}

	span.SetAttributes(
		attribute.String("kernel.syscall", p.getStringValue(obsEvent.Action)),
		attribute.Int("kernel.pid", int(p.getInt32Value(obsEvent.PID))),
	)

	return nil
}

// parseKubeAPIEvent parses Kubernetes API events
func (p *EventParser) parseKubeAPIEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_kubeapi_event")
	defer span.End()

	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		// Fallback to raw parsing
		obsEvent.Action = stringPtr("k8s_raw")
		obsEvent.Data["raw_data"] = string(raw.Data)
		return nil
	}

	// Extract namespace (high priority for k8s events)
	if ns, ok := data["namespace"].(string); ok && ns != "" {
		obsEvent.Namespace = &ns
	}

	// Extract pod name
	if podName, ok := data["pod_name"].(string); ok && podName != "" {
		obsEvent.PodName = &podName
	} else if name, ok := data["name"].(string); ok && name != "" {
		// Check if this is a pod event
		if kind, exists := data["kind"].(string); exists && strings.ToLower(kind) == "pod" {
			obsEvent.PodName = &name
		}
	}

	// Extract service name
	if serviceName, ok := data["service_name"].(string); ok && serviceName != "" {
		obsEvent.ServiceName = &serviceName
	}

	// Extract node name
	if nodeName, ok := data["node_name"].(string); ok && nodeName != "" {
		obsEvent.NodeName = &nodeName
	}

	// Extract action (event type/reason)
	if reason, ok := data["reason"].(string); ok && reason != "" {
		obsEvent.Action = &reason
	} else if eventType, ok := data["type"].(string); ok && eventType != "" {
		obsEvent.Action = &eventType
	} else {
		action := "k8s_event"
		obsEvent.Action = &action
	}

	// Extract target object
	if object, ok := data["object"].(string); ok && object != "" {
		obsEvent.Target = &object
	} else if kind, ok := data["kind"].(string); ok {
		if name, nameOk := data["name"].(string); nameOk {
			target := fmt.Sprintf("%s/%s", kind, name)
			obsEvent.Target = &target
		}
	}

	// Extract result/status
	if status, ok := data["status"].(string); ok && status != "" {
		obsEvent.Result = &status
	}

	// Store all fields as strings
	for k, v := range data {
		obsEvent.Data[k] = fmt.Sprintf("%v", v)
	}

	span.SetAttributes(
		attribute.String("k8s.namespace", p.getStringValue(obsEvent.Namespace)),
		attribute.String("k8s.pod_name", p.getStringValue(obsEvent.PodName)),
		attribute.String("k8s.action", p.getStringValue(obsEvent.Action)),
	)

	return nil
}

// parseDNSEvent parses DNS query events
func (p *EventParser) parseDNSEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_dns_event")
	defer span.End()

	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		// Fallback parsing
		obsEvent.Action = stringPtr("dns_query")
		obsEvent.Data["raw_data"] = string(raw.Data)
		return nil
	}

	// Extract PID if available
	if pidVal, ok := data["pid"]; ok {
		if pid, err := p.extractInt32(pidVal); err == nil {
			obsEvent.PID = &pid
		}
	}

	// Extract container context
	if containerID, ok := data["container_id"].(string); ok && containerID != "" {
		obsEvent.ContainerID = &containerID
	}

	if podName, ok := data["pod_name"].(string); ok && podName != "" {
		obsEvent.PodName = &podName
	}

	if namespace, ok := data["namespace"].(string); ok && namespace != "" {
		obsEvent.Namespace = &namespace
	}

	// DNS specific fields
	action := "dns_query"
	obsEvent.Action = &action

	// Extract target domain
	if domain, ok := data["domain"].(string); ok && domain != "" {
		obsEvent.Target = &domain
	} else if query, ok := data["query"].(string); ok && query != "" {
		obsEvent.Target = &query
	}

	// Extract result (response code, resolved IP, etc.)
	if response, ok := data["response"].(string); ok && response != "" {
		obsEvent.Result = &response
	} else if rcode, ok := data["rcode"]; ok {
		result := fmt.Sprintf("rcode_%v", rcode)
		obsEvent.Result = &result
	}

	// Store all fields
	for k, v := range data {
		obsEvent.Data[k] = fmt.Sprintf("%v", v)
	}

	span.SetAttributes(
		attribute.String("dns.domain", p.getStringValue(obsEvent.Target)),
		attribute.String("dns.result", p.getStringValue(obsEvent.Result)),
	)

	return nil
}

// parseSystemdEvent parses systemd service events
func (p *EventParser) parseSystemdEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_systemd_event")
	defer span.End()

	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		// Try parsing as plain log line
		obsEvent.Action = stringPtr("service_log")
		obsEvent.Data["message"] = string(raw.Data)
		return nil
	}

	// Extract service name
	if serviceName, ok := data["service_name"].(string); ok && serviceName != "" {
		obsEvent.ServiceName = &serviceName
	} else if unit, ok := data["unit"].(string); ok && unit != "" {
		obsEvent.ServiceName = &unit
	}

	// Extract PID if available
	if pidVal, ok := data["pid"]; ok {
		if pid, err := p.extractInt32(pidVal); err == nil {
			obsEvent.PID = &pid
		}
	}

	// Extract action (state change, log level, etc.)
	if action, ok := data["action"].(string); ok && action != "" {
		obsEvent.Action = &action
	} else if level, ok := data["level"].(string); ok && level != "" {
		obsEvent.Action = &level
	} else {
		action := "service_event"
		obsEvent.Action = &action
	}

	// Extract target (service state, file path, etc.)
	if state, ok := data["state"].(string); ok && state != "" {
		obsEvent.Target = &state
	}

	// Extract result
	if result, ok := data["result"].(string); ok && result != "" {
		obsEvent.Result = &result
	} else if exitCode, ok := data["exit_code"]; ok {
		result := fmt.Sprintf("exit_%v", exitCode)
		obsEvent.Result = &result
	}

	// Store all fields
	for k, v := range data {
		obsEvent.Data[k] = fmt.Sprintf("%v", v)
	}

	span.SetAttributes(
		attribute.String("systemd.service", p.getStringValue(obsEvent.ServiceName)),
		attribute.String("systemd.action", p.getStringValue(obsEvent.Action)),
	)

	return nil
}

// parseCNIEvent parses CNI network events
func (p *EventParser) parseCNIEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_cni_event")
	defer span.End()

	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		obsEvent.Action = stringPtr("network_event")
		obsEvent.Data["raw_data"] = string(raw.Data)
		return nil
	}

	// Extract pod/namespace context
	if podName, ok := data["pod_name"].(string); ok && podName != "" {
		obsEvent.PodName = &podName
	}

	if namespace, ok := data["namespace"].(string); ok && namespace != "" {
		obsEvent.Namespace = &namespace
	}

	if containerID, ok := data["container_id"].(string); ok && containerID != "" {
		obsEvent.ContainerID = &containerID
	}

	// Extract action
	if action, ok := data["action"].(string); ok && action != "" {
		obsEvent.Action = &action
	} else {
		action := "network_event"
		obsEvent.Action = &action
	}

	// Extract target (interface, IP, etc.)
	if iface, ok := data["interface"].(string); ok && iface != "" {
		obsEvent.Target = &iface
	} else if ip, ok := data["ip"].(string); ok && ip != "" {
		obsEvent.Target = &ip
	}

	// Extract result
	if result, ok := data["result"].(string); ok && result != "" {
		obsEvent.Result = &result
	}

	// Store all fields
	for k, v := range data {
		obsEvent.Data[k] = fmt.Sprintf("%v", v)
	}

	span.SetAttributes(
		attribute.String("cni.pod_name", p.getStringValue(obsEvent.PodName)),
		attribute.String("cni.action", p.getStringValue(obsEvent.Action)),
	)

	return nil
}

// parseEtcdEvent parses etcd storage events
func (p *EventParser) parseEtcdEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_etcd_event")
	defer span.End()

	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		obsEvent.Action = stringPtr("storage_event")
		obsEvent.Data["raw_data"] = string(raw.Data)
		return nil
	}

	// Extract action (operation type)
	if operation, ok := data["operation"].(string); ok && operation != "" {
		obsEvent.Action = &operation
	} else {
		action := "storage_event"
		obsEvent.Action = &action
	}

	// Extract target (key path)
	if key, ok := data["key"].(string); ok && key != "" {
		obsEvent.Target = &key

		// Try to extract namespace and pod name from kubernetes resource keys
		if strings.Contains(key, "/registry/") {
			parts := strings.Split(key, "/")
			for i, part := range parts {
				if part == "namespaces" && i+1 < len(parts) {
					namespace := parts[i+1]
					obsEvent.Namespace = &namespace
					break
				} else if (part == "pods" || part == "services" || part == "deployments") && i+2 < len(parts) {
					// Extract namespace and resource name from /registry/pods/namespace/name pattern
					namespace := parts[i+1]
					resourceName := parts[i+2]
					obsEvent.Namespace = &namespace

					if part == "pods" {
						obsEvent.PodName = &resourceName
					} else if part == "services" {
						obsEvent.ServiceName = &resourceName
					}
					break
				}
			}
		}
	}

	// Extract result
	if result, ok := data["result"].(string); ok && result != "" {
		obsEvent.Result = &result
	}

	// Store all fields
	for k, v := range data {
		obsEvent.Data[k] = fmt.Sprintf("%v", v)
	}

	span.SetAttributes(
		attribute.String("etcd.operation", p.getStringValue(obsEvent.Action)),
		attribute.String("etcd.key", p.getStringValue(obsEvent.Target)),
	)

	return nil
}

// parseGenericEvent handles unknown event types
func (p *EventParser) parseGenericEvent(ctx context.Context, raw collectors.RawEvent, obsEvent *ObservationEvent) error {
	ctx, span := p.tracer.Start(ctx, "event_parser.parse_generic_event")
	defer span.End()

	// Try JSON parsing first
	var data map[string]interface{}
	if err := json.Unmarshal(raw.Data, &data); err == nil {
		// Extract common fields if they exist
		if pidVal, ok := data["pid"]; ok {
			if pid, err := p.extractInt32(pidVal); err == nil {
				obsEvent.PID = &pid
			}
		}

		if action, ok := data["action"].(string); ok {
			obsEvent.Action = &action
		}

		// Store all fields
		for k, v := range data {
			obsEvent.Data[k] = fmt.Sprintf("%v", v)
		}
	} else {
		// Raw text fallback
		action := "raw_event"
		obsEvent.Action = &action
		obsEvent.Data["raw_data"] = string(raw.Data)
	}

	span.SetAttributes(
		attribute.String("generic.type", raw.Type),
		attribute.Int("generic.data_size", len(raw.Data)),
	)

	return nil
}

// Helper functions

// extractInt32 safely converts various numeric types to int32
func (p *EventParser) extractInt32(val interface{}) (int32, error) {
	switch v := val.(type) {
	case int32:
		return v, nil
	case int:
		return int32(v), nil
	case int64:
		return int32(v), nil
	case float64:
		return int32(v), nil
	case string:
		if i, err := strconv.ParseInt(v, 10, 32); err == nil {
			return int32(i), nil
		}
		return 0, fmt.Errorf("cannot parse string %q as int32", v)
	default:
		return 0, fmt.Errorf("cannot convert %T to int32", v)
	}
}

// Helper functions for safe string extraction
func (p *EventParser) getStringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func (p *EventParser) getInt32Value(ptr *int32) int32 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

// stringPtr is a helper to create string pointers
func stringPtr(s string) *string {
	return &s
}
