package storageio

import (
	"bufio"
	"fmt"
	"os"
	"strings"
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
