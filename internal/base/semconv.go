package base

import (
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// NetworkAttributes converts NetworkEventData to OTEL semantic conventions
func NetworkAttributes(data *domain.NetworkEventData) []attribute.KeyValue {
	if data == nil {
		return nil
	}

	attrs := []attribute.KeyValue{}

	// Protocol
	if data.Protocol != "" {
		attrs = append(attrs, semconv.NetworkProtocolName(data.Protocol))
	}

	// Peer (destination) attributes
	if data.DstIP != "" {
		attrs = append(attrs, semconv.NetworkPeerAddress(data.DstIP))
	}
	if data.DstPort > 0 {
		attrs = append(attrs, semconv.NetworkPeerPort(int(data.DstPort)))
	}

	// Local (source) attributes
	if data.SrcIP != "" {
		attrs = append(attrs, semconv.NetworkLocalAddress(data.SrcIP))
	}
	if data.SrcPort > 0 {
		attrs = append(attrs, semconv.NetworkLocalPort(int(data.SrcPort)))
	}

	// HTTP attributes (L7 protocol)
	if data.HTTPMethod != "" {
		attrs = append(attrs, attribute.String("http.request.method", data.HTTPMethod))
	}
	if data.HTTPPath != "" {
		attrs = append(attrs, semconv.URLPath(data.HTTPPath))
	}
	if data.HTTPStatusCode > 0 {
		attrs = append(attrs, attribute.Int("http.response.status_code", data.HTTPStatusCode))
	}

	// Connection metadata
	if data.Duration > 0 {
		attrs = append(attrs, attribute.Int64("network.connection.duration_ns", data.Duration))
	}
	if data.BytesSent > 0 {
		attrs = append(attrs, attribute.Int64("network.io.bytes_sent", int64(data.BytesSent)))
	}
	if data.BytesReceived > 0 {
		attrs = append(attrs, attribute.Int64("network.io.bytes_received", int64(data.BytesReceived)))
	}

	return attrs
}

// ProcessAttributes converts ProcessEventData to OTEL semantic conventions
func ProcessAttributes(data *domain.ProcessEventData) []attribute.KeyValue {
	if data == nil {
		return nil
	}

	attrs := []attribute.KeyValue{
		semconv.ProcessPID(int(data.PID)),
	}

	if data.PPID > 0 {
		attrs = append(attrs, semconv.ProcessParentPID(int(data.PPID)))
	}
	if data.ProcessName != "" {
		attrs = append(attrs, semconv.ProcessExecutableName(data.ProcessName))
	}
	if data.CommandLine != "" {
		attrs = append(attrs, semconv.ProcessCommand(data.CommandLine))
	}

	return attrs
}

// K8sAttributes converts K8s context to OTEL semantic conventions
func K8sAttributes(clusterID, namespace, podName, deploymentName string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}

	if clusterID != "" {
		attrs = append(attrs, semconv.K8SClusterName(clusterID))
	}
	if namespace != "" {
		attrs = append(attrs, semconv.K8SNamespaceName(namespace))
	}
	if podName != "" {
		attrs = append(attrs, semconv.K8SPodName(podName))
	}
	if deploymentName != "" {
		attrs = append(attrs, semconv.K8SDeploymentName(deploymentName))
	}

	return attrs
}

// EventDomainAttribute returns event domain for grouping
// Maps event types to domains: network, kernel, container, kubernetes, process
func EventDomainAttribute(eventType string) attribute.KeyValue {
	domain := getEventDomain(eventType)
	return attribute.String("event.domain", domain)
}

// getEventDomain maps event type to domain
func getEventDomain(eventType string) string {
	// Network events
	networkPrefixes := []string{"tcp_", "udp_", "http_", "dns_", "connection_"}
	for _, prefix := range networkPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return "network"
		}
	}

	// Kernel events
	kernelPrefixes := []string{"oom_", "syscall_", "signal_"}
	for _, prefix := range kernelPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return "kernel"
		}
	}

	// Container events
	containerPrefixes := []string{"container_", "docker_"}
	for _, prefix := range containerPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return "container"
		}
	}

	// Kubernetes events
	k8sPrefixes := []string{"pod_", "deployment_", "service_", "node_"}
	for _, prefix := range k8sPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return "kubernetes"
		}
	}

	// Process events
	processPrefixes := []string{"process_", "exec_"}
	for _, prefix := range processPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return "process"
		}
	}

	return "unknown"
}

// ErrorTypeAttribute categorizes errors for metrics
func ErrorTypeAttribute(eventType string) attribute.KeyValue {
	errorType := categorizeError(eventType)
	return attribute.String("error.type", errorType)
}

// categorizeError maps event types to error categories
func categorizeError(eventType string) string {
	switch eventType {
	case "oom_kill":
		return "out_of_memory"
	case "connection_timeout", "connection_refused":
		return "network_error"
	case "pod_crash_loop", "container_crash":
		return "crash"
	case "deployment_failed":
		return "deployment_error"
	case "http_5xx":
		return "http_server_error"
	case "http_4xx":
		return "http_client_error"
	default:
		return "unknown"
	}
}

// IsErrorEvent determines if an event represents an error condition
func IsErrorEvent(event *domain.ObserverEvent) bool {
	// Known error event types
	errorEvents := map[string]bool{
		"oom_kill":           true,
		"connection_timeout": true,
		"connection_refused": true,
		"pod_crash_loop":     true,
		"container_crash":    true,
		"deployment_failed":  true,
	}

	if errorEvents[event.Type] {
		return true
	}

	// HTTP 5xx errors
	if event.NetworkData != nil && event.NetworkData.HTTPStatusCode >= 500 {
		return true
	}

	return false
}
