package patterns

import "time"

// DetectionMetadata represents strongly-typed metadata for detections
type DetectionMetadata struct {
	Service           string  `json:"service,omitempty"`
	TotalPods         int     `json:"total_pods,omitempty"`
	RecentRestarts    int     `json:"recent_restarts,omitempty"`
	AffectedPods      int     `json:"affected_pods,omitempty"`
	Restarts          int     `json:"restarts,omitempty"`
	Node              string  `json:"node,omitempty"`
	PressureEvents    int     `json:"pressure_events,omitempty"`
	Evictions         int     `json:"evictions,omitempty"`
	RestartCount      int     `json:"restart_count,omitempty"`
	RunningPods       int     `json:"running_pods,omitempty"`
	HealthRatio       float64 `json:"health_ratio,omitempty"`
	PodIssues         int     `json:"pod_issues,omitempty"`
	Deployment        string  `json:"deployment,omitempty"`
	DesiredReplicas   int     `json:"desired_replicas,omitempty"`
	UpdatedReplicas   int     `json:"updated_replicas,omitempty"`
	AvailableReplicas int     `json:"available_replicas,omitempty"`
	FailedPods        int     `json:"failed_pods,omitempty"`
	ProgressRatio     float64 `json:"progress_ratio,omitempty"`
}

// QueryParams represents strongly-typed query parameters
type QueryParams struct {
	PodUID        string `json:"podUID,omitempty"`
	Timestamp     int64  `json:"timestamp,omitempty"`
	StartTime     int64  `json:"startTime,omitempty"`
	CMUID         string `json:"cmUID,omitempty"`
	ServiceUID    string `json:"serviceUID,omitempty"`
	DeploymentUID string `json:"deploymentUID,omitempty"`
}

// NewQueryParams creates query parameters with type safety
func NewQueryParams() *QueryParams {
	return &QueryParams{}
}

// WithPodUID sets the pod UID
func (p *QueryParams) WithPodUID(uid string) *QueryParams {
	p.PodUID = uid
	return p
}

// WithTimestamp sets the timestamp
func (p *QueryParams) WithTimestamp(t time.Time) *QueryParams {
	p.Timestamp = t.Unix()
	return p
}

// WithStartTime sets the start time
func (p *QueryParams) WithStartTime(t time.Time) *QueryParams {
	p.StartTime = t.Unix()
	return p
}

// WithCMUID sets the ConfigMap UID
func (p *QueryParams) WithCMUID(uid string) *QueryParams {
	p.CMUID = uid
	return p
}

// WithServiceUID sets the service UID
func (p *QueryParams) WithServiceUID(uid string) *QueryParams {
	p.ServiceUID = uid
	return p
}

// WithDeploymentUID sets the deployment UID
func (p *QueryParams) WithDeploymentUID(uid string) *QueryParams {
	p.DeploymentUID = uid
	return p
}

// ToMap converts params to map for Neo4j driver (temporary bridge)
func (p *QueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	if p.PodUID != "" {
		m["podUID"] = p.PodUID
	}
	if p.Timestamp != 0 {
		m["timestamp"] = p.Timestamp
	}
	if p.StartTime != 0 {
		m["startTime"] = p.StartTime
	}
	if p.CMUID != "" {
		m["cmUID"] = p.CMUID
	}
	if p.ServiceUID != "" {
		m["serviceUID"] = p.ServiceUID
	}
	if p.DeploymentUID != "" {
		m["deploymentUID"] = p.DeploymentUID
	}
	return m
}
