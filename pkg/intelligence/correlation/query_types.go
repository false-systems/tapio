package correlation

import (
	"time"
)

// QueryParams is the base interface for all query parameters
type QueryParams interface {
	// Validate ensures the parameters are valid
	Validate() error
	// ToMap converts the parameters to a map for the underlying query engine
	// This is only used internally by the GraphStore implementation
	ToMap() map[string]interface{}
}

// BaseQueryParams contains common fields for all query parameters
type BaseQueryParams struct {
	Namespace string
	Cluster   string
}

// ServiceQueryParams represents parameters for service-related queries
type ServiceQueryParams struct {
	BaseQueryParams
	ServiceName string
	TimeWindow  time.Duration
	StartTime   time.Time
}

// Validate ensures ServiceQueryParams are valid
func (p *ServiceQueryParams) Validate() error {
	if p.ServiceName == "" {
		return ErrMissingRequiredField("ServiceName")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts ServiceQueryParams to map for internal use
func (p *ServiceQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["serviceName"] = p.ServiceName
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// PodQueryParams represents parameters for pod-related queries
type PodQueryParams struct {
	BaseQueryParams
	PodName    string
	PodUID     string
	TimeWindow time.Duration
	StartTime  time.Time
}

// Validate ensures PodQueryParams are valid
func (p *PodQueryParams) Validate() error {
	if p.PodName == "" && p.PodUID == "" {
		return ErrMissingRequiredField("PodName or PodUID")
	}
	if p.Namespace == "" && p.PodUID == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts PodQueryParams to map for internal use
func (p *PodQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	if p.PodName != "" {
		m["podName"] = p.PodName
	}
	if p.PodUID != "" {
		m["podUID"] = p.PodUID
	}
	if p.Namespace != "" {
		m["namespace"] = p.Namespace
	}
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// ConfigQueryParams represents parameters for config-related queries
type ConfigQueryParams struct {
	BaseQueryParams
	ConfigName string
	ConfigType string // "ConfigMap" or "Secret"
	TimeWindow time.Duration
	StartTime  time.Time
}

// Validate ensures ConfigQueryParams are valid
func (p *ConfigQueryParams) Validate() error {
	if p.ConfigName == "" {
		return ErrMissingRequiredField("ConfigName")
	}
	if p.ConfigType == "" {
		return ErrMissingRequiredField("ConfigType")
	}
	if p.ConfigType != "ConfigMap" && p.ConfigType != "Secret" {
		return ErrInvalidFieldValue("ConfigType", "must be 'ConfigMap' or 'Secret'")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts ConfigQueryParams to map for internal use
func (p *ConfigQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["configName"] = p.ConfigName
	m["configType"] = p.ConfigType
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// DeploymentQueryParams represents parameters for deployment-related queries
type DeploymentQueryParams struct {
	BaseQueryParams
	DeploymentName string
	TimeWindow     time.Duration
	StartTime      time.Time
}

// Validate ensures DeploymentQueryParams are valid
func (p *DeploymentQueryParams) Validate() error {
	if p.DeploymentName == "" {
		return ErrMissingRequiredField("DeploymentName")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts DeploymentQueryParams to map for internal use
func (p *DeploymentQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["deploymentName"] = p.DeploymentName
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// ReplicaSetQueryParams represents parameters for replicaset-related queries
type ReplicaSetQueryParams struct {
	BaseQueryParams
	ReplicaSetName string
	TimeWindow     time.Duration
	StartTime      time.Time
}

// Validate ensures ReplicaSetQueryParams are valid
func (p *ReplicaSetQueryParams) Validate() error {
	if p.ReplicaSetName == "" {
		return ErrMissingRequiredField("ReplicaSetName")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts ReplicaSetQueryParams to map for internal use
func (p *ReplicaSetQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["rsName"] = p.ReplicaSetName
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// StatefulSetQueryParams represents parameters for statefulset-related queries
type StatefulSetQueryParams struct {
	BaseQueryParams
	StatefulSetName string
	TimeWindow      time.Duration
	StartTime       time.Time
}

// Validate ensures StatefulSetQueryParams are valid
func (p *StatefulSetQueryParams) Validate() error {
	if p.StatefulSetName == "" {
		return ErrMissingRequiredField("StatefulSetName")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts StatefulSetQueryParams to map for internal use
func (p *StatefulSetQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["stsName"] = p.StatefulSetName
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// DaemonSetQueryParams represents parameters for daemonset-related queries
type DaemonSetQueryParams struct {
	BaseQueryParams
	DaemonSetName string
	TimeWindow    time.Duration
	StartTime     time.Time
}

// Validate ensures DaemonSetQueryParams are valid
func (p *DaemonSetQueryParams) Validate() error {
	if p.DaemonSetName == "" {
		return ErrMissingRequiredField("DaemonSetName")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts DaemonSetQueryParams to map for internal use
func (p *DaemonSetQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["dsName"] = p.DaemonSetName
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// VolumeQueryParams represents parameters for volume-related queries
type VolumeQueryParams struct {
	BaseQueryParams
	PVCName    string
	PodName    string
	TimeWindow time.Duration
	StartTime  time.Time
}

// Validate ensures VolumeQueryParams are valid
func (p *VolumeQueryParams) Validate() error {
	if p.PVCName == "" && p.PodName == "" {
		return ErrMissingRequiredField("PVCName or PodName")
	}
	if p.Namespace == "" {
		return ErrMissingRequiredField("Namespace")
	}
	return nil
}

// ToMap converts VolumeQueryParams to map for internal use
func (p *VolumeQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	if p.PVCName != "" {
		m["pvcName"] = p.PVCName
	}
	if p.PodName != "" {
		m["podName"] = p.PodName
	}
	m["namespace"] = p.Namespace
	if p.Cluster != "" {
		m["cluster"] = p.Cluster
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// EntityQueryParams represents parameters for generic entity queries
type EntityQueryParams struct {
	EntityUID  string
	EntityType string
	TimeWindow time.Duration
	StartTime  time.Time
}

// Validate ensures EntityQueryParams are valid
func (p *EntityQueryParams) Validate() error {
	if p.EntityUID == "" {
		return ErrMissingRequiredField("EntityUID")
	}
	return nil
}

// ToMap converts EntityQueryParams to map for internal use
func (p *EntityQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["uid"] = p.EntityUID
	if p.EntityType != "" {
		m["entityType"] = p.EntityType
	}
	if !p.StartTime.IsZero() {
		m["startTime"] = p.StartTime.Unix()
	}
	if p.TimeWindow > 0 {
		m["timeWindow"] = int64(p.TimeWindow.Seconds())
	}
	return m
}

// TimeRangeQueryParams represents parameters for time-based queries
type TimeRangeQueryParams struct {
	StartTime time.Time
	EndTime   time.Time
}

// Validate ensures TimeRangeQueryParams are valid
func (p *TimeRangeQueryParams) Validate() error {
	if p.StartTime.IsZero() {
		return ErrMissingRequiredField("StartTime")
	}
	if !p.EndTime.IsZero() && p.EndTime.Before(p.StartTime) {
		return ErrInvalidFieldValue("EndTime", "must be after StartTime")
	}
	return nil
}

// ToMap converts TimeRangeQueryParams to map for internal use
func (p *TimeRangeQueryParams) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["startTime"] = p.StartTime.Unix()
	if !p.EndTime.IsZero() {
		m["endTime"] = p.EndTime.Unix()
	}
	return m
}
