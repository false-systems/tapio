package patterns

import (
	"time"
)

// Interface method implementations for MemoryLeakDetector

// RequiredEventTypes returns the event types this detector needs
func (mld *MemoryLeakDetector) RequiredEventTypes() []string {
	return []string{
		"oom_kill",
		"memory_pressure",
		"container_restart",
		"pod_evicted",
		"memory_high",
		"memory_critical",
		"allocation_failure",
	}
}

// RequiredMetricTypes returns the metric types this detector needs
func (mld *MemoryLeakDetector) RequiredMetricTypes() []string {
	return []string{
		"container_memory_usage_bytes",
		"container_memory_working_set_bytes",
		"container_memory_rss",
		"container_memory_cache",
		"node_memory_MemAvailable_bytes",
		"node_memory_MemFree_bytes",
		"container_memory_failures_total",
	}
}

// TimeWindow returns the time window this detector operates on
func (mld *MemoryLeakDetector) TimeWindow() time.Duration {
	return mld.config.LookbackWindow
}

// Interface method implementations for NetworkFailureCascadeDetector

// RequiredEventTypes returns the event types this detector needs
func (nfcd *NetworkFailureCascadeDetector) RequiredEventTypes() []string {
	return []string{
		"connection_timeout",
		"connection_refused",
		"dns_resolution_failed",
		"network_unreachable",
		"packet_loss",
		"high_latency",
		"bandwidth_limit",
		"service_unavailable",
		"gateway_timeout",
		"circuit_breaker_open",
		"load_balancer_error",
		"proxy_error",
	}
}

// RequiredMetricTypes returns the metric types this detector needs
func (nfcd *NetworkFailureCascadeDetector) RequiredMetricTypes() []string {
	return []string{
		"network_latency",
		"http_request_duration",
		"network_throughput",
		"network_transmit_bytes",
		"network_receive_bytes",
		"network_errors",
		"http_errors",
		"network_packet_loss",
		"tcp_retransmissions",
		"dns_query_duration",
		"connection_pool_size",
	}
}

// TimeWindow returns the time window this detector operates on
func (nfcd *NetworkFailureCascadeDetector) TimeWindow() time.Duration {
	return nfcd.config.LookbackWindow
}

// Interface method implementations for StorageIOBottleneckDetector

// RequiredEventTypes returns the event types this detector needs
func (siobd *StorageIOBottleneckDetector) RequiredEventTypes() []string {
	return []string{
		"storage_io_error",
		"disk_full",
		"volume_mount_failure",
		"write_failure",
		"read_failure",
		"io_timeout",
		"volume_detached",
		"filesystem_readonly",
		"inode_exhaustion",
		"disk_pressure",
	}
}

// RequiredMetricTypes returns the metric types this detector needs
func (siobd *StorageIOBottleneckDetector) RequiredMetricTypes() []string {
	return []string{
		"node_disk_io_time_seconds_total",
		"node_disk_read_bytes_total",
		"node_disk_written_bytes_total",
		"node_disk_reads_completed_total",
		"node_disk_writes_completed_total",
		"node_filesystem_avail_bytes",
		"node_filesystem_size_bytes",
		"container_fs_reads_total",
		"container_fs_writes_total",
		"container_fs_reads_bytes_total",
		"container_fs_writes_bytes_total",
		"node_disk_io_now",
		"node_disk_io_time_weighted_seconds_total",
	}
}

// TimeWindow returns the time window this detector operates on
func (siobd *StorageIOBottleneckDetector) TimeWindow() time.Duration {
	return siobd.config.LookbackWindow
}

// Interface method implementations for ContainerRuntimeFailureDetector

// RequiredEventTypes returns the event types this detector needs
func (crfd *ContainerRuntimeFailureDetector) RequiredEventTypes() []string {
	return []string{
		"container_start_failed",
		"container_create_failed",
		"image_pull_failed",
		"image_pull_backoff",
		"container_runtime_error",
		"container_oom_killed",
		"container_died",
		"pod_sandbox_failed",
		"runtime_unhealthy",
		"cgroup_error",
		"volume_mount_error",
		"network_setup_failed",
	}
}

// RequiredMetricTypes returns the metric types this detector needs
func (crfd *ContainerRuntimeFailureDetector) RequiredMetricTypes() []string {
	return []string{
		"kubelet_runtime_operations_total",
		"kubelet_runtime_operations_duration_seconds",
		"kubelet_runtime_operations_errors_total",
		"kubelet_pod_start_duration_seconds",
		"kubelet_pod_worker_duration_seconds",
		"kubelet_cgroup_manager_duration_seconds",
		"container_start_time_seconds",
		"container_last_seen",
		"kubelet_container_log_filesystem_used_bytes",
		"kubelet_volume_stats_available_bytes",
		"kubelet_volume_stats_capacity_bytes",
	}
}

// TimeWindow returns the time window this detector operates on
func (crfd *ContainerRuntimeFailureDetector) TimeWindow() time.Duration {
	return crfd.config.LookbackWindow
}

// Interface method implementations for ServiceDependencyFailureDetector

// RequiredEventTypes returns the event types this detector needs
func (sdfd *ServiceDependencyFailureDetector) RequiredEventTypes() []string {
	return []string{
		"service_unavailable",
		"dependency_timeout",
		"circuit_breaker_open",
		"upstream_connection_error",
		"downstream_error",
		"service_degraded",
		"health_check_failed",
		"readiness_probe_failed",
		"liveness_probe_failed",
		"endpoint_not_ready",
		"service_discovery_failed",
	}
}

// RequiredMetricTypes returns the metric types this detector needs
func (sdfd *ServiceDependencyFailureDetector) RequiredMetricTypes() []string {
	return []string{
		"http_requests_total",
		"http_request_duration_seconds",
		"grpc_server_handled_total",
		"grpc_server_handling_seconds",
		"tcp_connections_total",
		"tcp_connection_errors_total",
		"service_response_time_seconds",
		"service_error_rate",
		"service_availability",
		"circuit_breaker_state",
		"dependency_health_score",
		"service_call_rate",
	}
}

// TimeWindow returns the time window this detector operates on
func (sdfd *ServiceDependencyFailureDetector) TimeWindow() time.Duration {
	return sdfd.config.LookbackWindow
}
