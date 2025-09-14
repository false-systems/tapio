package lifecycle

import (
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// ZERO interface{} - ZERO map[string]interface{} - ALL TYPED

// LifecycleTransition represents a state change that affects system stability
type LifecycleTransition struct {
	Type      TransitionType    `json:"type"`
	State     StateChange       `json:"state"`
	Resources AffectedResources `json:"resources"`
	Cascade   []CascadeEffect   `json:"cascade,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// TransitionType - What kind of lifecycle change
type TransitionType string

const (
	// Critical transitions that break availability
	TransitionScaleToZero TransitionType = "scale_to_zero"
	TransitionDeletion    TransitionType = "deletion"
	TransitionNotReady    TransitionType = "not_ready"
	TransitionEviction    TransitionType = "eviction"
	TransitionOOMKill     TransitionType = "oom_kill"
	TransitionCrashLoop   TransitionType = "crash_loop"

	// Transitions that degrade capacity
	TransitionScaleDown    TransitionType = "scale_down"
	TransitionResourceCut  TransitionType = "resource_cut"
	TransitionNodePressure TransitionType = "node_pressure"

	// Transitions that trigger cascades
	TransitionRollout      TransitionType = "rollout"
	TransitionConfigChange TransitionType = "config_change"
	TransitionImageUpdate  TransitionType = "image_update"

	// Security-critical transitions
	TransitionPrivileged TransitionType = "privileged"
	TransitionRootAccess TransitionType = "root_access"
)

// StateChange - From what to what
type StateChange struct {
	Resource  ResourceIdentifier `json:"resource"`
	FromState string             `json:"from_state"`
	ToState   string             `json:"to_state"`

	// Key facts about the change
	ReplicasBefore int `json:"replicas_before,omitempty"`
	ReplicasAfter  int `json:"replicas_after,omitempty"`

	// Only if resources were cut
	CPUBefore    string `json:"cpu_before,omitempty"`
	CPUAfter     string `json:"cpu_after,omitempty"`
	MemoryBefore string `json:"memory_before,omitempty"`
	MemoryAfter  string `json:"memory_after,omitempty"`
}

// ResourceIdentifier - What changed
type ResourceIdentifier struct {
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Namespace  string    `json:"namespace"`
	UID        types.UID `json:"uid"`
	APIVersion string    `json:"api_version"`
}

// AffectedResources - What else is impacted
type AffectedResources struct {
	DirectCount int                  `json:"direct_count"`
	Pods        []ResourceIdentifier `json:"pods,omitempty"`
	Services    []ResourceIdentifier `json:"services,omitempty"`
	Endpoints   []ResourceIdentifier `json:"endpoints,omitempty"`
	ConfigMaps  []ResourceIdentifier `json:"configmaps,omitempty"`
	Children    []ResourceIdentifier `json:"children,omitempty"`
}

// CascadeEffect - What WILL happen due to K8s mechanics
type CascadeEffect struct {
	Effect       string        `json:"effect"`                   // "pods_terminating", "endpoints_removed"
	Count        int           `json:"count"`                    // How many
	TimeToEffect time.Duration `json:"time_to_effect,omitempty"` // How long until it happens
}

// PodLifecycleState - Simplified pod states we care about
type PodLifecycleState string

const (
	PodPending     PodLifecycleState = "pending"
	PodRunning     PodLifecycleState = "running"
	PodTerminating PodLifecycleState = "terminating"
	PodFailed      PodLifecycleState = "failed"
	PodEvicted     PodLifecycleState = "evicted"
	PodOOMKilled   PodLifecycleState = "oom_killed"
)

// DeploymentLifecycleState - Deployment states that matter
type DeploymentLifecycleState string

const (
	DeploymentScaling       DeploymentLifecycleState = "scaling"
	DeploymentRollingUpdate DeploymentLifecycleState = "rolling_update"
	DeploymentRollback      DeploymentLifecycleState = "rollback"
	DeploymentPaused        DeploymentLifecycleState = "paused"
	DeploymentFailed        DeploymentLifecycleState = "failed"
)

// NodeLifecycleState - Node states that affect scheduling
type NodeLifecycleState string

const (
	NodeReady    NodeLifecycleState = "ready"
	NodeNotReady NodeLifecycleState = "not_ready"
	NodePressure NodeLifecycleState = "pressure"
	NodeDraining NodeLifecycleState = "draining"
	NodeCordoned NodeLifecycleState = "cordoned"
)

// TrackedResource - Resource we're monitoring for lifecycle changes
type TrackedResource struct {
	Identifier ResourceIdentifier
	LastState  string
	LastSeen   time.Time

	// For tracking patterns
	RestartCount  int
	OOMCount      int
	EvictionCount int

	// For rollout tracking
	UpdateStrategy string
	MaxUnavailable int
	MaxSurge       int
}

// TransitionPattern - Patterns that predict problems
type TransitionPattern struct {
	Pattern     string        `json:"pattern"`
	Occurrences int           `json:"occurrences"`
	Window      time.Duration `json:"window"`
	Prediction  string        `json:"prediction"` // "crash_loop_imminent"
}
