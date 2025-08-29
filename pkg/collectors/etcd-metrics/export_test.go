package etcdmetrics

// Export internal functions for testing
var (
	PerformHealthCheck = (*Collector).performHealthCheck
	CheckClusterStatus = (*Collector).checkClusterStatus
	HandleError        = (*Collector).handleError
	SendEvent          = (*Collector).sendEvent
	MonitorLoop        = (*Collector).monitorLoop
)
