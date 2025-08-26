package storageio

import (
	"regexp"
	"strings"
)

// PVCInfo contains extracted PVC information from a path
type PVCInfo struct {
	PVCName      string `json:"pvc_name"`
	Namespace    string `json:"namespace"`
	PodUID       string `json:"pod_uid"`
	VolumeName   string `json:"volume_name"`
	StorageClass string `json:"storage_class,omitempty"`
	VolumeType   string `json:"volume_type"`
}

var (
	// Kubernetes volume path patterns
	// Example: /var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~csi/pvc-123456/mount
	pvcPathRegex = regexp.MustCompile(`/var/lib/kubelet/pods/([^/]+)/volumes/kubernetes\.io~([^/]+)/([^/]+)`)

	// ConfigMap/Secret patterns
	// Example: /var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~configmap/my-config
	configMapRegex = regexp.MustCompile(`/var/lib/kubelet/pods/([^/]+)/volumes/kubernetes\.io~configmap/([^/]+)`)
	secretRegex    = regexp.MustCompile(`/var/lib/kubelet/pods/([^/]+)/volumes/kubernetes\.io~secret/([^/]+)`)

	// EmptyDir pattern
	// Example: /var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~empty-dir/cache-volume
	emptyDirRegex = regexp.MustCompile(`/var/lib/kubelet/pods/([^/]+)/volumes/kubernetes\.io~empty-dir/([^/]+)`)

	// HostPath pattern
	// Example: /var/lib/kubelet/pods/550e8400-e29b-41d4-a716-446655440000/volumes/kubernetes.io~host-path/host-logs
	hostPathRegex = regexp.MustCompile(`/var/lib/kubelet/pods/([^/]+)/volumes/kubernetes\.io~host-path/([^/]+)`)

	// CSI volume pattern (common for cloud providers)
	// Example: pvc-123e4567-e89b-12d3-a456-426614174000
	pvcUUIDPattern = regexp.MustCompile(`^pvc-[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
)

// ExtractPVCInfo extracts PVC and volume information from a file path
func ExtractPVCInfo(path string) *PVCInfo {
	// Check for ConfigMap volumes first (more specific pattern)
	if matches := configMapRegex.FindStringSubmatch(path); len(matches) > 2 {
		return &PVCInfo{
			PodUID:     matches[1],
			VolumeType: "configmap",
			VolumeName: matches[2],
		}
	}

	// Check for Secret volumes
	if matches := secretRegex.FindStringSubmatch(path); len(matches) > 2 {
		return &PVCInfo{
			PodUID:     matches[1],
			VolumeType: "secret",
			VolumeName: matches[2],
		}
	}

	// Check for EmptyDir volumes
	if matches := emptyDirRegex.FindStringSubmatch(path); len(matches) > 2 {
		return &PVCInfo{
			PodUID:     matches[1],
			VolumeType: "emptydir",
			VolumeName: matches[2],
		}
	}

	// Check for HostPath volumes
	if matches := hostPathRegex.FindStringSubmatch(path); len(matches) > 2 {
		return &PVCInfo{
			PodUID:     matches[1],
			VolumeType: "hostpath",
			VolumeName: matches[2],
		}
	}

	// Check for PVC volumes (general pattern after specific ones)
	if matches := pvcPathRegex.FindStringSubmatch(path); len(matches) > 3 {
		volumeType := matches[2] // e.g., "csi", "nfs", "rbd", "cephfs"
		volumeName := matches[3] // e.g., "pvc-123456" or actual PVC name

		info := &PVCInfo{
			PodUID:     matches[1],
			VolumeType: "pvc",
			VolumeName: volumeName,
		}

		// Extract PVC name from volume name
		// CSI volumes often use pvc-<uid> format
		if pvcUUIDPattern.MatchString(volumeName) {
			// This is a CSI volume with UUID, try to extract actual PVC name
			// In production, we'd query K8s API or maintain a cache
			info.PVCName = extractPVCNameFromUID(volumeName)
		} else {
			// Direct PVC name (for simpler volume plugins)
			info.PVCName = volumeName
		}

		// Determine storage class from volume type hint
		info.StorageClass = inferStorageClass(volumeType)

		return info
	}

	// Check if this is a container rootfs or log path
	if strings.Contains(path, "/var/lib/docker/containers/") ||
		strings.Contains(path, "/var/lib/containerd/") {
		return &PVCInfo{
			VolumeType: "container_rootfs",
		}
	}

	// Check for etcd data
	if strings.HasPrefix(path, "/var/lib/etcd/") {
		return &PVCInfo{
			VolumeType: "etcd_data",
			VolumeName: "etcd-data",
		}
	}

	return nil
}

// extractPVCNameFromUID attempts to extract a meaningful PVC name from a PVC UID
// In production, this would query K8s API or use a cache populated by watching PVC objects
func extractPVCNameFromUID(pvcUID string) string {
	// For demo purposes, we'll return a formatted version
	// In production, maintain a map: pvcUID -> {name, namespace, storageClass}

	// Common patterns for PVC names based on their usage:
	// - Database PVCs often contain "db", "postgres", "mysql", "mongo"
	// - Cache PVCs often contain "cache", "redis", "memcached"
	// - Log PVCs often contain "log", "audit", "events"

	// This would be populated from K8s API watch
	// Example implementation:
	pvcNameCache := map[string]string{
		"pvc-123e4567-e89b-12d3-a456-426614174000": "postgres-data",
		"pvc-234e5678-f89c-23d4-b567-537625285111": "redis-cache",
		"pvc-345e6789-089d-34e5-c678-648736396222": "elasticsearch-data",
		"pvc-456e7890-189e-45f6-d789-759847407333": "prometheus-storage",
	}

	if name, exists := pvcNameCache[pvcUID]; exists {
		return name
	}

	// Fallback to UID if no mapping found
	return pvcUID
}

// inferStorageClass attempts to determine storage class from volume type hints
func inferStorageClass(volumeType string) string {
	switch volumeType {
	case "csi":
		return "standard-csi"
	case "nfs":
		return "nfs-storage"
	case "rbd":
		return "ceph-block"
	case "cephfs":
		return "ceph-filesystem"
	case "ebs":
		return "gp3" // AWS EBS gp3
	case "azuredisk":
		return "managed-premium" // Azure Premium SSD
	case "gcepd", "gce-pd":
		return "pd-ssd" // GCP Persistent Disk SSD
	default:
		return ""
	}
}

// EnrichEventWithPVCInfo adds PVC information to a storage event
func EnrichEventWithPVCInfo(event *StorageIOEvent, path string) {
	pvcInfo := ExtractPVCInfo(path)
	if pvcInfo == nil {
		return
	}

	// Enrich the event with PVC information
	event.K8sVolumeType = pvcInfo.VolumeType
	event.PodUID = pvcInfo.PodUID

	// Additional enrichment would happen here
	// For example, looking up pod name, namespace from UID
}

// GetVolumeDisplayName returns a human-readable name for the volume
func GetVolumeDisplayName(pvcInfo *PVCInfo) string {
	if pvcInfo == nil {
		return "unknown"
	}

	switch pvcInfo.VolumeType {
	case "pvc":
		if pvcInfo.PVCName != "" {
			if pvcInfo.Namespace != "" {
				return pvcInfo.Namespace + "/" + pvcInfo.PVCName
			}
			return pvcInfo.PVCName
		}
		return pvcInfo.VolumeName
	case "configmap":
		if pvcInfo.Namespace != "" {
			return "configmap:" + pvcInfo.Namespace + "/" + pvcInfo.VolumeName
		}
		return "configmap:" + pvcInfo.VolumeName
	case "secret":
		if pvcInfo.Namespace != "" {
			return "secret:" + pvcInfo.Namespace + "/" + pvcInfo.VolumeName
		}
		return "secret:" + pvcInfo.VolumeName
	case "emptydir":
		return "emptydir:" + pvcInfo.VolumeName
	case "hostpath":
		return "hostpath:" + pvcInfo.VolumeName
	default:
		return pvcInfo.VolumeType
	}
}

// IsKubernetesPVCPath checks if a path is a Kubernetes PVC path
func IsKubernetesPVCPath(path string) bool {
	// Check if it's a PVC-related path (not ConfigMap/Secret/EmptyDir/HostPath)
	if configMapRegex.MatchString(path) ||
		secretRegex.MatchString(path) ||
		emptyDirRegex.MatchString(path) ||
		hostPathRegex.MatchString(path) {
		return false
	}

	// Check if it matches the PVC pattern
	if matches := pvcPathRegex.FindStringSubmatch(path); len(matches) > 2 {
		volumeType := matches[2]
		// PVC volume types include: csi, nfs, rbd, cephfs, ebs, azuredisk, gcepd, etc.
		return volumeType == "csi" || volumeType == "nfs" || volumeType == "rbd" ||
			volumeType == "cephfs" || volumeType == "ebs" || volumeType == "azuredisk" ||
			volumeType == "gcepd" || volumeType == "gce-pd"
	}

	return false
}

// IsKubernetesVolumePath checks if a path is any Kubernetes volume path
func IsKubernetesVolumePath(path string) bool {
	if !strings.Contains(path, "/var/lib/kubelet/pods/") {
		return false
	}

	return strings.Contains(path, "/volumes/kubernetes.io~")
}
