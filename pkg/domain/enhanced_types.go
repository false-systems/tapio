package domain

import (
	"time"
)

// CollectorMetadata provides metadata about the collector that generated an event
type CollectorMetadata struct {
	ID            string                 `json:"id"`
	Type          SourceType             `json:"type"`
	Version       string                 `json:"version"`
	Capabilities  []string               `json:"capabilities"`
	Health        HealthStatus           `json:"health"`
	Node          string                 `json:"node,omitempty"`
	StartTime     time.Time              `json:"start_time"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
	Config        map[string]interface{} `json:"config,omitempty"`
}

// ResourceRelationship defines a relationship between two resources
type ResourceRelationship struct {
	Source    ResourceRef            `json:"source"`
	Target    ResourceRef            `json:"target"`
	Type      string                 `json:"type"`               // "contains", "depends-on", "communicates-with", etc.
	Direction string                 `json:"direction"`          // "unidirectional", "bidirectional"
	Strength  float64                `json:"strength,omitempty"` // 0.0-1.0
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ResourceTopology represents the topology of resources and their relationships
type ResourceTopology struct {
	Resources     []ResourceRef            `json:"resources"`
	Relationships []ResourceRelationship   `json:"relationships"`
	Layers        map[string][]ResourceRef `json:"layers,omitempty"` // e.g., "infrastructure", "platform", "application"
	UpdatedAt     time.Time                `json:"updated_at"`
}

// BusinessContext provides business-level context for findings
type BusinessContext struct {
	Service          string   `json:"service"`
	Team             string   `json:"team,omitempty"`
	BusinessUnit     string   `json:"business_unit,omitempty"`
	CustomerImpact   string   `json:"customer_impact,omitempty"`
	RevenueImpact    float64  `json:"revenue_impact,omitempty"`
	SLAViolation     bool     `json:"sla_violation"`
	ComplianceImpact []string `json:"compliance_impact,omitempty"` // e.g., ["PCI", "HIPAA"]
	CostImpact       float64  `json:"cost_impact,omitempty"`
}

// FindingLifecycle represents the lifecycle state of a finding
type FindingLifecycle struct {
	State           FindingState  `json:"state"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	AcknowledgedAt  *time.Time    `json:"acknowledged_at,omitempty"`
	InvestigatingAt *time.Time    `json:"investigating_at,omitempty"`
	ResolvedAt      *time.Time    `json:"resolved_at,omitempty"`
	ClosedAt        *time.Time    `json:"closed_at,omitempty"`
	StateHistory    []StateChange `json:"state_history"`
	TTL             time.Duration `json:"ttl,omitempty"` // Time to live before auto-closing
}

// FindingState represents the state of a finding
type FindingState string

const (
	FindingStateNew           FindingState = "new"
	FindingStateAcknowledged  FindingState = "acknowledged"
	FindingStateInvestigating FindingState = "investigating"
	FindingStateResolved      FindingState = "resolved"
	FindingStateClosed        FindingState = "closed"
	FindingStateSuppressed    FindingState = "suppressed"
)

// StateChange represents a change in finding state
type StateChange struct {
	From      FindingState `json:"from"`
	To        FindingState `json:"to"`
	Timestamp time.Time    `json:"timestamp"`
	By        string       `json:"by"`
	Reason    string       `json:"reason,omitempty"`
}

// EnrichedFinding extends Finding with additional context and lifecycle management
type EnrichedFinding struct {
	Finding                            // Embedded base finding
	RiskScore       float64            `json:"risk_score"` // 0.0-100.0
	Remediation     []ActionItem       `json:"remediation"`
	BusinessCtx     BusinessContext    `json:"business_context"`
	Lifecycle       FindingLifecycle   `json:"lifecycle"`
	RootCause       *RootCauseAnalysis `json:"root_cause,omitempty"`
	RelatedFindings []string           `json:"related_findings,omitempty"` // IDs of related findings
	Evidence        []Evidence         `json:"evidence"`
	Impact          ImpactAnalysis     `json:"impact_analysis"`
	Tags            []string           `json:"tags,omitempty"`
	Annotations     map[string]string  `json:"annotations,omitempty"`
}

// RootCauseAnalysis provides root cause analysis for a finding
type RootCauseAnalysis struct {
	PrimaryRootCause    string       `json:"primary_root_cause"`
	ContributingFactors []string     `json:"contributing_factors"`
	CausalChain         []CausalStep `json:"causal_chain"`
	Confidence          float64      `json:"confidence"`
	Evidence            []string     `json:"evidence"` // Event IDs that support this analysis
}

// CausalStep represents a step in the causal chain
type CausalStep struct {
	Description string    `json:"description"`
	EventID     string    `json:"event_id,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Impact      string    `json:"impact"`
}

// ImpactAnalysis provides detailed impact assessment
type ImpactAnalysis struct {
	Severity           SeverityLevel `json:"severity"`
	Scope              ImpactScope   `json:"scope"`
	Duration           time.Duration `json:"duration,omitempty"`
	AffectedServices   []string      `json:"affected_services"`
	AffectedUsers      int           `json:"affected_users,omitempty"`
	PerformanceImpact  float64       `json:"performance_impact,omitempty"`  // Percentage degradation
	AvailabilityImpact float64       `json:"availability_impact,omitempty"` // Percentage downtime
	DataIntegrityRisk  bool          `json:"data_integrity_risk"`
	SecurityRisk       bool          `json:"security_risk"`
}

// ImpactScope represents the scope of impact
type ImpactScope string

const (
	ImpactScopeNode         ImpactScope = "node"
	ImpactScopePod          ImpactScope = "pod"
	ImpactScopeService      ImpactScope = "service"
	ImpactScopeNamespace    ImpactScope = "namespace"
	ImpactScopeCluster      ImpactScope = "cluster"
	ImpactScopeMultiCluster ImpactScope = "multi-cluster"
)

// Enhanced KernelData for eBPF events
type EnhancedKernelData struct {
	KernelData                       // Embedded base kernel data
	BPFProgram      string           `json:"bpf_program,omitempty"`
	BPFMapStats     map[string]int   `json:"bpf_map_stats,omitempty"`
	KprobeDetails   *KprobeDetails   `json:"kprobe_details,omitempty"`
	SecurityContext *SecurityContext `json:"security_context,omitempty"`
}

// KprobeDetails provides kprobe-specific information
type KprobeDetails struct {
	FunctionName string            `json:"function_name"`
	Offset       int               `json:"offset,omitempty"`
	Module       string            `json:"module,omitempty"`
	Arguments    map[string]string `json:"arguments,omitempty"`
}

// SecurityContext provides security-related context
type SecurityContext struct {
	SELinuxContext  string   `json:"selinux_context,omitempty"`
	AppArmorProfile string   `json:"apparmor_profile,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Seccomp         string   `json:"seccomp,omitempty"`
}

// Enhanced KubernetesData for K8s events
type EnhancedKubernetesData struct {
	KubernetesData                       // Embedded base kubernetes data
	ClusterName      string              `json:"cluster_name,omitempty"`
	CustomResource   *CustomResourceInfo `json:"custom_resource,omitempty"`
	AdmissionWebhook *WebhookInfo        `json:"admission_webhook,omitempty"`
	NetworkPolicy    *NetworkPolicyInfo  `json:"network_policy,omitempty"`
}

// CustomResourceInfo provides CRD-specific information
type CustomResourceInfo struct {
	Group   string                 `json:"group"`
	Version string                 `json:"version"`
	Kind    string                 `json:"kind"`
	Name    string                 `json:"name"`
	Spec    map[string]interface{} `json:"spec,omitempty"`
	Status  map[string]interface{} `json:"status,omitempty"`
}

// WebhookInfo provides admission webhook information
type WebhookInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`     // "validating", "mutating"
	Decision     string `json:"decision"` // "allow", "deny"
	Reason       string `json:"reason,omitempty"`
	PatchApplied string `json:"patch_applied,omitempty"`
}

// NetworkPolicyInfo provides network policy context
type NetworkPolicyInfo struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	PodSelector map[string]string `json:"pod_selector"`
	PolicyTypes []string          `json:"policy_types"` // "Ingress", "Egress"
	Rules       []PolicyRule      `json:"rules"`
}

// PolicyRule represents a network policy rule
type PolicyRule struct {
	Type     string   `json:"type"` // "ingress", "egress"
	From     []string `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	Ports    []int    `json:"ports,omitempty"`
	Protocol string   `json:"protocol,omitempty"`
}

// Enhanced ServiceEvent for systemd with dependency tracking
type EnhancedServiceEvent struct {
	ServiceEvent                        // Embedded base service event
	Dependencies    []ServiceDependency `json:"dependencies,omitempty"`
	ResourceUsage   *ResourceUsage      `json:"resource_usage,omitempty"`
	RestartCount    int                 `json:"restart_count,omitempty"`
	LastRestartTime *time.Time          `json:"last_restart_time,omitempty"`
}

// ServiceDependency represents a systemd service dependency
type ServiceDependency struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // "requires", "wants", "after", "before"
	State     string `json:"state"`
	Satisfied bool   `json:"satisfied"`
}

// ResourceUsage provides resource usage information
type ResourceUsage struct {
	CPUUsagePercent     float64 `json:"cpu_usage_percent"`
	MemoryUsageBytes    int64   `json:"memory_usage_bytes"`
	MemoryLimitBytes    int64   `json:"memory_limit_bytes"`
	OpenFileDescriptors int     `json:"open_file_descriptors"`
	ThreadCount         int     `json:"thread_count"`
}

// Enhanced NetworkData for CNI with network policy context
type EnhancedNetworkData struct {
	NetworkData                       // Embedded base network data
	NetworkPolicy  *NetworkPolicyInfo `json:"network_policy,omitempty"`
	IPTablesRules  []IPTablesRule     `json:"iptables_rules,omitempty"`
	ContainerID    string             `json:"container_id,omitempty"`
	InterfaceName  string             `json:"interface_name,omitempty"`
	VirtualNetwork string             `json:"virtual_network,omitempty"`
}

// IPTablesRule represents an iptables rule
type IPTablesRule struct {
	Table    string `json:"table"`
	Chain    string `json:"chain"`
	Rule     string `json:"rule"`
	Target   string `json:"target"`
	Protocol string `json:"protocol,omitempty"`
	Source   string `json:"source,omitempty"`
	Dest     string `json:"dest,omitempty"`
}
