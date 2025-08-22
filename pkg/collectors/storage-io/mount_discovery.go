package storageio

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// discoverK8sMountPointsImpl discovers Kubernetes-relevant mount points
func discoverK8sMountPointsImpl(monitoredPaths []string) ([]*MountInfo, error) {
	// Read /proc/mounts to get current mount points
	mounts, err := readProcMounts()
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/mounts: %w", err)
	}

	var k8sMounts []*MountInfo

	// Filter mounts that match our monitored paths
	for _, mount := range mounts {
		if isK8sRelevantMount(mount, monitoredPaths) {
			k8sMount := enrichMountWithK8sInfo(mount)
			k8sMounts = append(k8sMounts, k8sMount)
		}
	}

	// Also discover kubelet pod directories directly
	podMounts, err := discoverKubeletPodMounts()
	if err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: failed to discover kubelet pod mounts: %v\n", err)
	} else {
		k8sMounts = append(k8sMounts, podMounts...)
	}

	return k8sMounts, nil
}

// ProcMount represents a mount entry from /proc/mounts
type ProcMount struct {
	Device     string
	MountPoint string
	FSType     string
	Options    string
}

// readProcMounts reads and parses /proc/mounts
func readProcMounts() ([]*ProcMount, error) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	defer file.Close()

	var mounts []*ProcMount
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		mount := &ProcMount{
			Device:     fields[0],
			MountPoint: fields[1],
			FSType:     fields[2],
			Options:    fields[3],
		}

		mounts = append(mounts, mount)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading /proc/mounts: %w", err)
	}

	return mounts, nil
}

// isK8sRelevantMount checks if a mount is relevant for Kubernetes monitoring
func isK8sRelevantMount(mount *ProcMount, monitoredPaths []string) bool {
	// Check if mount point matches any of our monitored paths
	for _, path := range monitoredPaths {
		if strings.HasPrefix(mount.MountPoint, path) {
			return true
		}
	}

	// Check for Kubernetes-specific mount patterns
	k8sPatterns := []string{
		"/var/lib/kubelet/",
		"/var/lib/docker/",
		"/var/lib/containerd/",
		"/var/log/containers/",
		"/var/log/pods/",
		"/etc/kubernetes/",
		"/var/lib/etcd/",
	}

	for _, pattern := range k8sPatterns {
		if strings.HasPrefix(mount.MountPoint, pattern) {
			return true
		}
	}

	return false
}

// enrichMountWithK8sInfo enriches mount information with Kubernetes context
func enrichMountWithK8sInfo(mount *ProcMount) *MountInfo {
	mountInfo := &MountInfo{
		Path:     mount.MountPoint,
		Device:   mount.Device,
		Type:     mount.FSType,
		Options:  mount.Options,
		ReadOnly: strings.Contains(mount.Options, "ro"),
	}

	// Determine K8s volume type and extract metadata
	mountInfo.K8sVolumeType, mountInfo.PodUID, mountInfo.VolumeName = parseK8sVolumeInfo(mount.MountPoint)

	return mountInfo
}

// parseK8sVolumeInfo extracts Kubernetes volume information from mount path
func parseK8sVolumeInfo(mountPath string) (volumeType, podUID, volumeName string) {
	// Pattern for kubelet pod volumes: /var/lib/kubelet/pods/{pod-uid}/volumes/{volume-type}/{volume-name}
	podVolumePattern := regexp.MustCompile(`/var/lib/kubelet/pods/([^/]+)/volumes/([^/]+)/(.+)`)
	if matches := podVolumePattern.FindStringSubmatch(mountPath); len(matches) == 4 {
		podUID = matches[1]
		volumeType = mapK8sVolumeType(matches[2])
		volumeName = matches[3]
		return
	}

	// Pattern for kubelet plugin volumes: /var/lib/kubelet/plugins/{plugin-name}
	pluginPattern := regexp.MustCompile(`/var/lib/kubelet/plugins/([^/]+)`)
	if matches := pluginPattern.FindStringSubmatch(mountPath); len(matches) == 2 {
		volumeType = string(K8sVolumePVC) // Assume PVC for plugin mounts
		volumeName = matches[1]
		return
	}

	// Pattern for container mounts
	if strings.Contains(mountPath, "/var/lib/docker/containers/") {
		volumeType = "container"
		return
	}

	if strings.Contains(mountPath, "/var/lib/containerd/") {
		volumeType = "container"
		return
	}

	// Pattern for config/secret paths
	if strings.Contains(mountPath, "/etc/kubernetes/") {
		volumeType = string(K8sVolumeConfigMap)
		return
	}

	// Default to hostpath
	volumeType = string(K8sVolumeHostPath)
	return
}

// mapK8sVolumeType maps kubelet volume directory names to our volume types
func mapK8sVolumeType(kubeletVolumeType string) string {
	switch kubeletVolumeType {
	case "kubernetes.io~configmap":
		return string(K8sVolumeConfigMap)
	case "kubernetes.io~secret":
		return string(K8sVolumeSecret)
	case "kubernetes.io~empty-dir":
		return string(K8sVolumeEmptyDir)
	case "kubernetes.io~projected":
		return string(K8sVolumeProjected)
	case "kubernetes.io~downward-api":
		return string(K8sVolumeDownwardAPI)
	case "kubernetes.io~host-path":
		return string(K8sVolumeHostPath)
	default:
		// Check for CSI or other PVC patterns
		if strings.Contains(kubeletVolumeType, "csi") || strings.Contains(kubeletVolumeType, "pvc") {
			return string(K8sVolumePVC)
		}
		return string(K8sVolumeHostPath)
	}
}

// discoverKubeletPodMounts discovers pod-specific mount points by scanning kubelet directories
func discoverKubeletPodMounts() ([]*MountInfo, error) {
	kubeletPodsPath := "/var/lib/kubelet/pods"

	// Check if kubelet pods directory exists
	if _, err := os.Stat(kubeletPodsPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubelet pods directory not found: %s", kubeletPodsPath)
	}

	var mounts []*MountInfo

	// Walk through pod directories
	err := filepath.Walk(kubeletPodsPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors for individual paths
		}

		// Only process volume directories
		if !info.IsDir() {
			return nil
		}

		// Check if this looks like a volume mount point
		if isVolumeMount(path) {
			mountInfo := createMountInfoFromPath(path)
			if mountInfo != nil {
				mounts = append(mounts, mountInfo)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk kubelet pods directory: %w", err)
	}

	return mounts, nil
}

// isVolumeMount checks if a path represents a volume mount point
func isVolumeMount(path string) bool {
	// Look for volume-like patterns
	volumePatterns := []string{
		"/volumes/",
		"/volume-subpaths/",
	}

	for _, pattern := range volumePatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}

	return false
}

// createMountInfoFromPath creates MountInfo from a filesystem path
func createMountInfoFromPath(path string) *MountInfo {
	// Check if the path is actually mounted by looking for mount markers
	if !isActualMount(path) {
		return nil
	}

	volumeType, podUID, volumeName := parseK8sVolumeInfo(path)

	return &MountInfo{
		Path:          path,
		Device:        "unknown", // We'll update this if we can determine it
		Type:          "unknown", // We'll update this if we can determine it
		K8sVolumeType: volumeType,
		PodUID:        podUID,
		VolumeName:    volumeName,
		ReadOnly:      isReadOnlyMount(path),
	}
}

// isActualMount checks if a path is actually a mount point
func isActualMount(path string) bool {
	// Simple heuristic: check if it's a directory that exists
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		// Additional check: see if it has the expected structure
		return hasExpectedVolumeStructure(path)
	}
	return false
}

// hasExpectedVolumeStructure checks if a volume path has the expected Kubernetes structure
func hasExpectedVolumeStructure(path string) bool {
	// For now, just check if it's under a recognized pattern
	return strings.Contains(path, "/var/lib/kubelet/pods/")
}

// isReadOnlyMount checks if a mount appears to be read-only
func isReadOnlyMount(path string) bool {
	// Simple heuristic: check if we can create a test file
	testFile := filepath.Join(path, ".write-test")
	if file, err := os.Create(testFile); err == nil {
		file.Close()
		os.Remove(testFile)
		return false // Writable
	}
	return true // Assume read-only if we can't write
}
