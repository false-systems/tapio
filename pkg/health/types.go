package health

import "time"

type Status string

const (
	StatusHealthy   Status = "Healthy"
	StatusWarning   Status = "Warning"
	StatusCritical  Status = "Critical"
	StatusUnknown   Status = "Unknown"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Report struct {
	Timestamp     time.Time
	OverallStatus Status
	TotalPods     int
	HealthyPods   int
	Namespaces    []NamespaceHealth
	Pods          []PodHealth
	Issues        []Issue
}

type NamespaceHealth struct {
	Name        string
	Status      Status
	TotalPods   int
	HealthyPods int
}

type PodHealth struct {
	Name         string
	Namespace    string
	Status       string
	RestartCount int32
	Age          time.Duration
	Ready        bool
	Issues       []string
}

type Issue struct {
	Severity Severity
	Message  string
	Resource string
}