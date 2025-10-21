package scheduler

import "time"

// SchedulingInfo contains scheduling metadata for a pod
// Populated from K8s Events API (FailedScheduling events)
type SchedulingInfo struct {
	PodUID        string    `json:"pod_uid"`
	PodName       string    `json:"pod_name"`
	Namespace     string    `json:"namespace"`
	FailureCount  int       `json:"failure_count"`
	FailureReason string    `json:"failure_reason"` // e.g., "0/3 nodes available: insufficient cpu"
	LastAttempt   time.Time `json:"last_attempt"`
	Scheduled     bool      `json:"scheduled"`
	NodeName      string    `json:"node_name"` // Assigned node (if scheduled)
}

// PluginMetrics contains per-plugin execution metrics
// Populated from Prometheus scheduler_plugin_execution_duration_seconds
type PluginMetrics struct {
	PluginName     string    `json:"plugin_name"`
	ExtensionPoint string    `json:"extension_point"` // "Filter", "Score", "Bind", etc.
	DurationMs     float64   `json:"duration_ms"`
	Result         string    `json:"result"` // Success, Error, Unschedulable
	Timestamp      time.Time `json:"timestamp"`
}

// PreemptionInfo tracks pod preemption events
// Populated from Prometheus scheduler_preemption_victims
type PreemptionInfo struct {
	PreemptorPodUID string    `json:"preemptor_pod_uid"`
	PreemptorPod    string    `json:"preemptor_pod"`
	VictimPodUID    string    `json:"victim_pod_uid"`
	VictimPod       string    `json:"victim_pod"`
	Namespace       string    `json:"namespace"`
	Reason          string    `json:"reason"`
	Timestamp       time.Time `json:"timestamp"`
}

// SchedulerMetrics contains global scheduler metrics
// Populated from Prometheus (scheduler_pending_pods, queue depth, etc.)
type SchedulerMetrics struct {
	PendingPods        int64     `json:"pending_pods"`
	SchedulingAttempts int64     `json:"scheduling_attempts_total"`
	SchedulingErrors   int64     `json:"scheduling_errors_total"`
	PreemptionAttempts int64     `json:"preemption_attempts_total"`
	PreemptionVictims  int64     `json:"preemption_victims_total"`
	LastUpdated        time.Time `json:"last_updated"`
}
