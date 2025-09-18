package storageio

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// DetectRuntimeEnvironment detects the runtime environment
func DetectRuntimeEnvironment() (*RuntimeEnvironment, error) {
	env := &RuntimeEnvironment{
		VolumePathPatterns: make(map[string]string),
	}

	// Check for Kubernetes
	if _, err := os.Stat("/var/lib/kubelet"); err == nil {
		env.IsKubernetes = true
		env.VolumePathPatterns["/var/lib/kubelet/pods"] = "kubernetes"
	}

	// Check for Docker
	if _, err := os.Stat("/var/lib/docker"); err == nil {
		env.IsDocker = true
		env.VolumePathPatterns["/var/lib/docker/volumes"] = "docker"
		env.VolumePathPatterns["/var/lib/docker/overlay2"] = "docker-overlay"
	}

	// Check for containerd
	if _, err := os.Stat("/var/lib/containerd"); err == nil {
		env.IsContainerd = true
		env.VolumePathPatterns["/var/lib/containerd"] = "containerd"
	}

	// Check for CRI-O
	if _, err := os.Stat("/var/lib/containers"); err == nil {
		env.VolumePathPatterns["/var/lib/containers/storage"] = "crio"
	}

	return env, nil
}

// DiscoverK8sMounts discovers Kubernetes-related mount points
func DiscoverK8sMounts() ([]*MountInfo, error) {
	mounts := make([]*MountInfo, 0)

	// Read /proc/mounts
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return mounts, fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		path := fields[1]
		fsType := fields[2]

		// Check if this is a K8s-related mount
		if isK8sMount(path) {
			mount := &MountInfo{
				Device: device,
				Path:   path,
				FSType: fsType,
			}

			// Try to extract PVC info from path
			if pvcName, namespace := extractPVCInfo(path); pvcName != "" {
				mount.PVCName = pvcName
				mount.Namespace = namespace
			}

			mounts = append(mounts, mount)
		}
	}

	return mounts, scanner.Err()
}

// isK8sMount checks if a mount path is Kubernetes-related
func isK8sMount(path string) bool {
	k8sPrefixes := []string{
		"/var/lib/kubelet/pods",
		"/var/lib/kubelet/plugins",
		"/var/lib/rancher/k3s/storage",
		"/var/snap/microk8s/common/var/lib/kubelet",
	}

	for _, prefix := range k8sPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// extractPVCInfo extracts PVC name and namespace from mount path
func extractPVCInfo(path string) (string, string) {
	// Pattern: /var/lib/kubelet/pods/<pod-uid>/volumes/kubernetes.io~<type>/<pvc-name>
	if strings.Contains(path, "/volumes/kubernetes.io~") {
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if part == "volumes" && i+2 < len(parts) {
				// The PVC name is typically the last part
				pvcName := parts[len(parts)-1]
				// Namespace might be encoded in the PVC name (namespace-pvcname)
				if idx := strings.LastIndex(pvcName, "-"); idx > 0 {
					namespace := pvcName[:idx]
					name := pvcName[idx+1:]
					return name, namespace
				}
				return pvcName, ""
			}
		}
	}
	return "", ""
}

// GetContainerInfoFromCgroup extracts container information from cgroup ID
func GetContainerInfoFromCgroup(cgroupID uint64) (*ContainerInfo, error) {
	// Try multiple cgroup paths
	cgroupPaths := []string{
		fmt.Sprintf("/sys/fs/cgroup/unified/%d", cgroupID),
		fmt.Sprintf("/sys/fs/cgroup/memory/%d", cgroupID),
		fmt.Sprintf("/sys/fs/cgroup/cpu/%d", cgroupID),
	}

	for _, cgroupPath := range cgroupPaths {
		if info, err := extractContainerInfoFromPath(cgroupPath); err == nil && info != nil {
			return info, nil
		}
	}

	// Fallback: search /proc/*/cgroup files
	return searchProcCgroups(cgroupID)
}

// extractContainerInfoFromPath extracts container info from cgroup path
func extractContainerInfoFromPath(cgroupPath string) (*ContainerInfo, error) {
	// Read the cgroup path to extract container ID
	if _, err := os.Stat(cgroupPath); err != nil {
		return nil, err
	}

	// Parse the path for container patterns
	// Common patterns:
	// /docker/<container-id>
	// /kubepods/pod<pod-uid>/<container-id>
	// /system.slice/docker-<container-id>.scope
	parts := strings.Split(cgroupPath, "/")

	for i, part := range parts {
		// Docker container pattern
		if len(part) == 64 && isHexString(part) {
			containerID := part
			podName, namespace := extractPodInfoFromCgroup(cgroupPath)
			return &ContainerInfo{
				ContainerID: containerID,
				PodName:     podName,
				Namespace:   namespace,
				CgroupPath:  cgroupPath,
			}, nil
		}

		// Kubernetes pod pattern
		if strings.HasPrefix(part, "pod") && len(part) > 3 {
			podUID := strings.TrimPrefix(part, "pod")
			if i+1 < len(parts) {
				containerID := parts[i+1]
				podName, namespace := lookupPodByUID(podUID)
				return &ContainerInfo{
					ContainerID: containerID,
					PodName:     podName,
					Namespace:   namespace,
					CgroupPath:  cgroupPath,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no container info found in path: %s", cgroupPath)
}

// searchProcCgroups searches /proc/*/cgroup for the given cgroup ID
func searchProcCgroups(cgroupID uint64) (*ContainerInfo, error) {
	// This is a simplified implementation
	// In production, you'd cache this information and use more efficient lookups
	procEntries, err := filepath.Glob("/proc/*/cgroup")
	if err != nil {
		return nil, err
	}

	for _, procPath := range procEntries[:100] { // Limit search to avoid performance issues
		file, err := os.Open(procPath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, fmt.Sprintf("%d", cgroupID)) {
				file.Close()
				return extractContainerInfoFromCgroupLine(line, procPath)
			}
		}
		file.Close()
	}

	return nil, fmt.Errorf("cgroup ID %d not found in /proc/*/cgroup", cgroupID)
}

// extractContainerInfoFromCgroupLine extracts container info from cgroup line
func extractContainerInfoFromCgroupLine(line, procPath string) (*ContainerInfo, error) {
	// Parse cgroup line format: hierarchy-ID:controller-list:cgroup-path
	parts := strings.Split(line, ":")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid cgroup line format")
	}

	cgroupPath := parts[2]
	containerID := extractContainerIDFromCgroupPath(cgroupPath)
	if containerID == "" {
		return nil, fmt.Errorf("no container ID found in cgroup path")
	}

	podName, namespace := extractPodInfoFromCgroup(cgroupPath)

	return &ContainerInfo{
		ContainerID: containerID,
		PodName:     podName,
		Namespace:   namespace,
		CgroupPath:  cgroupPath,
	}, nil
}

// extractContainerIDFromCgroupPath extracts container ID from cgroup path
func extractContainerIDFromCgroupPath(cgroupPath string) string {
	// Common patterns:
	// /docker/<container-id>
	// /kubepods/burstable/pod<pod-uid>/<container-id>
	// /system.slice/docker-<container-id>.scope

	parts := strings.Split(cgroupPath, "/")
	for _, part := range parts {
		// Docker container ID (64-char hex)
		if len(part) == 64 && isHexString(part) {
			return part
		}

		// Docker scope format
		if strings.HasPrefix(part, "docker-") && strings.HasSuffix(part, ".scope") {
			containerID := strings.TrimPrefix(part, "docker-")
			containerID = strings.TrimSuffix(containerID, ".scope")
			if len(containerID) == 64 && isHexString(containerID) {
				return containerID
			}
		}
	}

	return ""
}

// extractPodInfoFromCgroup extracts pod information from cgroup path
func extractPodInfoFromCgroup(cgroupPath string) (podName, namespace string) {
	// Look for pod UID in the path
	parts := strings.Split(cgroupPath, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, "pod") && len(part) > 3 {
			podUID := strings.TrimPrefix(part, "pod")
			return lookupPodByUID(podUID)
		}
	}
	return "", ""
}

// lookupPodByUID looks up pod name and namespace by UID
func lookupPodByUID(podUID string) (podName, namespace string) {
	// Check kubelet pod directory
	podDir := filepath.Join("/var/lib/kubelet/pods", podUID)
	if _, err := os.Stat(podDir); err != nil {
		return "", ""
	}

	// Try to read pod metadata
	// In a real implementation, you'd parse the pod spec or use the Kubernetes API
	// For now, we'll use a simplified approach

	// Look for volume mounts to infer namespace/name
	volumesDir := filepath.Join(podDir, "volumes")
	if entries, err := os.ReadDir(volumesDir); err == nil {
		for _, entry := range entries {
			if strings.Contains(entry.Name(), "kubernetes.io") {
				// Try to extract namespace/name from volume paths
				volumePath := filepath.Join(volumesDir, entry.Name())
				if subEntries, err := os.ReadDir(volumePath); err == nil {
					for _, subEntry := range subEntries {
						name := subEntry.Name()
						// Common pattern: namespace-podname or just podname
						if parts := strings.Split(name, "-"); len(parts) >= 2 {
							return parts[len(parts)-1], parts[0]
						}
						return name, "default"
					}
				}
			}
		}
	}

	return "", ""
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

// EnrichEventWithK8sInfo enriches a storage event with Kubernetes information
func (o *Observer) EnrichEventWithK8sInfo(event *StorageEvent) {
	if !o.config.EnableK8sIntegration {
		return
	}

	// Check cache first
	o.containerCacheMu.RLock()
	containerInfo, exists := o.containerCache[event.CgroupID]
	o.containerCacheMu.RUnlock()

	if !exists {
		// Lookup container info from cgroup
		if info, err := GetContainerInfoFromCgroup(event.CgroupID); err == nil {
			containerInfo = info

			// Cache the result
			o.containerCacheMu.Lock()
			o.containerCache[event.CgroupID] = containerInfo
			o.containerCacheMu.Unlock()
		}
	}

	// Enrich event with container/pod information
	if containerInfo != nil {
		// Add to custom fields
		eventPath := event.GetFullPath()

		// Check if this path corresponds to a known mount
		o.mountCacheMu.RLock()
		for mountPath, mountInfo := range o.mountCache {
			if strings.HasPrefix(eventPath, mountPath) {
				// Found matching mount - this gives us PVC information
				if mountInfo.PVCName != "" {
					// Update custom fields in the domain event (this would be done in convertCoreToDomainEvent)
					o.logger.Debug("Enriched storage event with K8s info",
						zap.String("pod", containerInfo.PodName),
						zap.String("namespace", containerInfo.Namespace),
						zap.String("pvc", mountInfo.PVCName),
						zap.String("path", eventPath))
				}
				break
			}
		}
		o.mountCacheMu.RUnlock()
	}
}
