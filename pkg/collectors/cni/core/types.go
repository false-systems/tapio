package core

import (
	"fmt"
	"time"
)

// CNIError represents a CNI collector error
type CNIError struct {
	Type      ErrorType
	Message   string
	Cause     error
	Timestamp time.Time
	Context   map[string]interface{}
}

// ErrorType categorizes CNI collector errors
type ErrorType string

const (
	ErrorTypeCNIExecution  ErrorType = "cni_execution"
	ErrorTypeConfiguration ErrorType = "configuration"
	ErrorTypeKubernetes    ErrorType = "kubernetes"
	ErrorTypeMonitoring    ErrorType = "monitoring"
	ErrorTypeCorrelation   ErrorType = "correlation"
	ErrorTypeIPAM          ErrorType = "ipam"
	ErrorTypeNetworkPolicy ErrorType = "network_policy"
	ErrorTypeUnsupported   ErrorType = "unsupported"
)

func (e CNIError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s error: %s (caused by: %v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s error: %s", e.Type, e.Message)
}

func (e CNIError) Unwrap() error {
	return e.Cause
}

// NewCNIError creates a new CNI error
func NewCNIError(errType ErrorType, message string, cause error) CNIError {
	return CNIError{
		Type:      errType,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
	}
}

// CNIPlugin represents supported CNI plugins
type CNIPlugin string

const (
	CNIPluginCilium  CNIPlugin = "cilium"
	CNIPluginCalico  CNIPlugin = "calico"
	CNIPluginFlannel CNIPlugin = "flannel"
	CNIPluginWeave   CNIPlugin = "weave"
	CNIPluginAWSVPC  CNIPlugin = "aws-vpc-cni"
	CNIPluginAzure   CNIPlugin = "azure-cni"
	CNIPluginGCP     CNIPlugin = "gcp-cni"
	CNIPluginBridge  CNIPlugin = "bridge"
	CNIPluginHost    CNIPlugin = "host-device"
	CNIPluginMacvlan CNIPlugin = "macvlan"
	CNIPluginIPVLAN  CNIPlugin = "ipvlan"
	CNIPluginUnknown CNIPlugin = "unknown"
)

// DetectCNIPlugin attempts to identify the CNI plugin from various sources
func DetectCNIPlugin(pluginName, command, config string) CNIPlugin {
	switch {
	case contains(pluginName, "cilium") || contains(command, "cilium"):
		return CNIPluginCilium
	case contains(pluginName, "calico") || contains(command, "calico"):
		return CNIPluginCalico
	case contains(pluginName, "flannel") || contains(command, "flannel"):
		return CNIPluginFlannel
	case contains(pluginName, "weave") || contains(command, "weave"):
		return CNIPluginWeave
	case contains(pluginName, "aws") || contains(command, "aws-vpc-cni"):
		return CNIPluginAWSVPC
	case contains(pluginName, "azure") || contains(command, "azure"):
		return CNIPluginAzure
	case contains(pluginName, "gcp") || contains(command, "gcp"):
		return CNIPluginGCP
	case contains(pluginName, "bridge") || contains(command, "bridge"):
		return CNIPluginBridge
	case contains(pluginName, "host") || contains(command, "host-device"):
		return CNIPluginHost
	case contains(pluginName, "macvlan") || contains(command, "macvlan"):
		return CNIPluginMacvlan
	case contains(pluginName, "ipvlan") || contains(command, "ipvlan"):
		return CNIPluginIPVLAN
	default:
		return CNIPluginUnknown
	}
}

// CNIEventType represents the semantic type of CNI event for correlation
type CNIEventType string

const (
	CNIEventTypePluginExecution   CNIEventType = "plugin_execution"
	CNIEventTypeIPAllocation      CNIEventType = "ip_allocation"
	CNIEventTypeIPDeallocation    CNIEventType = "ip_deallocation"
	CNIEventTypeInterfaceSetup    CNIEventType = "interface_setup"
	CNIEventTypeInterfaceTeardown CNIEventType = "interface_teardown"
	CNIEventTypeRouteAdd          CNIEventType = "route_add"
	CNIEventTypeRouteDelete       CNIEventType = "route_delete"
	CNIEventTypePolicyApply       CNIEventType = "policy_apply"
	CNIEventTypePolicyRemove      CNIEventType = "policy_remove"
	CNIEventTypeConfigChange      CNIEventType = "config_change"
	CNIEventTypeHealthCheck       CNIEventType = "health_check"
	CNIEventTypeError             CNIEventType = "error"
)

// MapOperationToEventType maps CNI operations to semantic event types
func MapOperationToEventType(operation CNIOperation, success bool, hasIP bool) CNIEventType {
	if !success {
		return CNIEventTypeError
	}

	switch operation {
	case CNIOperationAdd:
		if hasIP {
			return CNIEventTypeIPAllocation
		}
		return CNIEventTypeInterfaceSetup
	case CNIOperationDel:
		if hasIP {
			return CNIEventTypeIPDeallocation
		}
		return CNIEventTypeInterfaceTeardown
	case CNIOperationCheck:
		return CNIEventTypeHealthCheck
	default:
		return CNIEventTypePluginExecution
	}
}

// CNISeverity represents the severity of CNI events
type CNISeverity string

const (
	CNISeverityInfo     CNISeverity = "info"
	CNISeverityWarning  CNISeverity = "warning"
	CNISeverityError    CNISeverity = "error"
	CNISeverityCritical CNISeverity = "critical"
)

// DetermineCNISeverity determines event severity based on context
func DetermineCNISeverity(operation CNIOperation, success bool, duration time.Duration, plugin CNIPlugin) CNISeverity {
	// Failed operations
	if !success {
		if operation == CNIOperationAdd {
			// Failed ADD operations are critical as they prevent pod startup
			return CNISeverityCritical
		}
		if operation == CNIOperationDel {
			// Failed DEL operations are errors but not critical
			return CNISeverityError
		}
		return CNISeverityError
	}

	// Slow operations indicate potential issues
	slowThreshold := 10 * time.Second
	if duration > slowThreshold {
		return CNISeverityWarning
	}

	// Check operations that fail are warnings
	if operation == CNIOperationCheck {
		return CNISeverityWarning
	}

	// Normal successful operations
	return CNISeverityInfo
}

// CNIMetrics represents CNI-specific metrics
type CNIMetrics struct {
	PluginExecutions    map[string]uint64        `json:"plugin_executions"`
	ExecutionLatency    map[string]time.Duration `json:"execution_latency"`
	SuccessRates        map[string]float64       `json:"success_rates"`
	IPPoolUtilization   map[string]float64       `json:"ip_pool_utilization"`
	InterfaceCount      uint64                   `json:"interface_count"`
	ActiveConnections   uint64                   `json:"active_connections"`
	PolicyRulesActive   uint64                   `json:"policy_rules_active"`
	ConfigurationErrors uint64                   `json:"configuration_errors"`
}

// MonitorType represents different CNI monitoring approaches
type MonitorType string

const (
	MonitorTypeLog     MonitorType = "log"
	MonitorTypeProcess MonitorType = "process"
	MonitorTypeEvent   MonitorType = "event"
	MonitorTypeFile    MonitorType = "file"
	MonitorTypeAPI     MonitorType = "api"
)

// CorrelationContext provides context for correlating CNI events with other sources
type CorrelationContext struct {
	PodCreationTime  *time.Time `json:"pod_creation_time,omitempty"`
	ServiceEndpoints []string   `json:"service_endpoints,omitempty"`
	NetworkPolicies  []string   `json:"network_policies,omitempty"`
	RelatedEvents    []string   `json:"related_events,omitempty"`
	TraceID          string     `json:"trace_id,omitempty"`
	SpanID           string     `json:"span_id,omitempty"`
	CorrelationScore float64    `json:"correlation_score,omitempty"`
}

// Helper function for string matching
func contains(str, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(str) < len(substr) {
		return false
	}
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
