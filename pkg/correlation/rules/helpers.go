package rules

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/types"
)

// isETCDPod checks if a pod is an etcd instance
func isETCDPod(pod types.PodInfo) bool {
	// Check by label
	if component, ok := pod.Labels["component"]; ok && component == "etcd" {
		return true
	}
	
	// Check by name pattern
	if strings.Contains(pod.Name, "etcd") && pod.Namespace == "kube-system" {
		return true
	}
	
	// Check by container name
	for _, container := range pod.Spec.Containers {
		if container.Name == "etcd" {
			return true
		}
	}
	
	return false
}

// isAPIServerPod checks if a pod is a kube-apiserver instance
func isAPIServerPod(pod types.PodInfo) bool {
	// Check by label
	if component, ok := pod.Labels["component"]; ok && component == "kube-apiserver" {
		return true
	}
	
	// Check by name pattern
	if strings.Contains(pod.Name, "kube-apiserver") && pod.Namespace == "kube-system" {
		return true
	}
	
	// Check by container name
	for _, container := range pod.Spec.Containers {
		if container.Name == "kube-apiserver" {
			return true
		}
	}
	
	return false
}

// isCoreDNSPod checks if a pod is a CoreDNS instance
func isCoreDNSPod(pod types.PodInfo) bool {
	// Check by label
	if appName, ok := pod.Labels["k8s-app"]; ok && appName == "kube-dns" {
		return true
	}
	
	// Check by name pattern
	if strings.Contains(pod.Name, "coredns") && pod.Namespace == "kube-system" {
		return true
	}
	
	return false
}

// getContainerMemoryUsage gets current memory usage for a container
func getContainerMemoryUsage(pod types.PodInfo, containerName string, data *correlation.AnalysisData) uint64 {
	// In a real implementation, this would fetch metrics from the metrics API
	// For now, we'll check if there's metric data in the analysis data
	
	// Check if we have metrics data
	if data.KubernetesData.Metrics != nil {
		key := pod.Namespace + "/" + pod.Name + "/" + containerName
		if metric, ok := data.KubernetesData.Metrics[key]; ok {
			if memBytes, ok := metric["memory_usage_bytes"].(float64); ok {
				return uint64(memBytes)
			}
		}
	}
	
	// Fallback: estimate from status if available
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			// This is a placeholder - real implementation would use metrics API
			return 0
		}
	}
	
	return 0
}

// getContainerMemoryLimit gets memory limit for a container
func getContainerMemoryLimit(pod types.PodInfo, containerName string) uint64 {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			if limits := container.Resources.Limits; limits != nil {
				if memory, ok := limits[v1.ResourceMemory]; ok {
					return uint64(memory.Value())
				}
			}
		}
	}
	return 0
}

// findPodByPID finds a pod that contains a process with the given PID
func findPodByPID(data *correlation.AnalysisData, pid uint32) *types.PodInfo {
	// In a real implementation, this would map PIDs to pods
	// This requires integration with container runtime or eBPF data
	
	// For now, check if eBPF data has container info
	if data.EBPFData != nil {
		for _, memStat := range data.EBPFData.MemoryStats {
			if memStat.PID == pid && memStat.ContainerID != "" {
				// Find pod by container ID
				for i, pod := range data.KubernetesData.Pods {
					for _, status := range pod.Status.ContainerStatuses {
						if strings.Contains(status.ContainerID, memStat.ContainerID) {
							return &data.KubernetesData.Pods[i]
						}
					}
				}
			}
		}
	}
	
	return nil
}

// isControllerManagerPod checks if a pod is a kube-controller-manager instance
func isControllerManagerPod(pod types.PodInfo) bool {
	// Check by label
	if component, ok := pod.Labels["component"]; ok && component == "kube-controller-manager" {
		return true
	}
	
	// Check by name pattern
	if strings.Contains(pod.Name, "kube-controller-manager") && pod.Namespace == "kube-system" {
		return true
	}
	
	return false
}

// isSchedulerPod checks if a pod is a kube-scheduler instance
func isSchedulerPod(pod types.PodInfo) bool {
	// Check by label
	if component, ok := pod.Labels["component"]; ok && component == "kube-scheduler" {
		return true
	}
	
	// Check by name pattern
	if strings.Contains(pod.Name, "kube-scheduler") && pod.Namespace == "kube-system" {
		return true
	}
	
	return false
}

// isAdmissionWebhook checks if a pod is an admission webhook
func isAdmissionWebhook(pod types.PodInfo) bool {
	// Check for common webhook labels
	if _, ok := pod.Labels["webhook"]; ok {
		return true
	}
	
	// Check for admission webhook annotations
	for key := range pod.Annotations {
		if strings.Contains(key, "admission") || strings.Contains(key, "webhook") {
			return true
		}
	}
	
	// Check service names
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == "webhook" || port.Name == "admission" {
				return true
			}
		}
	}
	
	return false
}

// getPodCertificateExpiry extracts certificate expiry information from a pod
func getPodCertificateExpiry(pod types.PodInfo, data *correlation.AnalysisData) (string, bool) {
	// Check if pod has certificate volume mounts
	for _, container := range pod.Spec.Containers {
		for _, mount := range container.VolumeMounts {
			if strings.Contains(mount.MountPath, "certs") || 
			   strings.Contains(mount.MountPath, "tls") ||
			   strings.Contains(mount.MountPath, "pki") {
				// In real implementation, would check certificate expiry
				// For now, check if we have this info in logs
				if logs, ok := data.KubernetesData.Logs[pod.Name]; ok {
					for _, line := range logs {
						if strings.Contains(strings.ToLower(line), "certificate") &&
						   (strings.Contains(strings.ToLower(line), "expir") ||
						    strings.Contains(strings.ToLower(line), "invalid")) {
							return line, true
						}
					}
				}
			}
		}
	}
	
	return "", false
}