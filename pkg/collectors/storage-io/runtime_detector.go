//go:build linux

package storageio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeEnvironment contains detected runtime configuration
type RuntimeEnvironment struct {
	// Kubernetes paths
	KubeletRootDir    string // Usually /var/lib/kubelet but can be different
	KubeletPodsDir    string // Usually <kubelet-root>/pods
	KubeletPluginsDir string // Usually <kubelet-root>/plugins
	KubeletVolumesDir string // Usually <kubelet-root>/plugins/kubernetes.io/

	// Container runtime paths
	ContainerRuntime  string // docker, containerd, cri-o, or unknown
	ContainerRootDir  string // Runtime-specific root directory
	ContainerStateDir string // Where container state is stored
	ContainerLogDir   string // Container log directory

	// Kubernetes service paths
	EtcdDataDir   string // etcd data directory if present
	KubeConfigDir string // Usually /etc/kubernetes but configurable

	// Detected features
	IsKubernetes       bool     // Whether we detected a Kubernetes environment
	HasEBPFSupport     bool     // Whether eBPF is supported
	DetectedCSIDrivers []string // List of detected CSI drivers

	// Volume path patterns (discovered, not hardcoded)
	VolumePathPatterns map[string]string // volume_type -> path_pattern
}

// DetectRuntimeEnvironment probes the system to detect actual paths and configuration
func DetectRuntimeEnvironment() (*RuntimeEnvironment, error) {
	env := &RuntimeEnvironment{
		VolumePathPatterns: make(map[string]string),
	}

	// Detect Kubernetes environment
	env.detectKubernetes()

	// Detect container runtime
	env.detectContainerRuntime()

	// Detect CSI drivers
	env.detectCSIDrivers()

	// Check eBPF support
	env.HasEBPFSupport = checkEBPFSupport()

	return env, env.validate()
}

// detectKubernetes finds Kubernetes paths dynamically
func (env *RuntimeEnvironment) detectKubernetes() {
	// Check for kubelet by looking for its common locations
	possibleKubeletDirs := []string{
		"/var/lib/kubelet",  // Standard location
		"/var/data/kubelet", // Some k3s installations
		"/opt/kubelet",      // Custom installations
		"/var/snap/microk8s/common/var/lib/kubelet", // MicroK8s
	}

	// Also check environment variable
	if kubeletDir := os.Getenv("KUBELET_ROOT_DIR"); kubeletDir != "" {
		possibleKubeletDirs = append([]string{kubeletDir}, possibleKubeletDirs...)
	}

	// Find the actual kubelet directory
	for _, dir := range possibleKubeletDirs {
		if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
			// Verify it's actually a kubelet dir by checking for expected subdirs
			if env.isValidKubeletDir(dir) {
				env.KubeletRootDir = dir
				env.KubeletPodsDir = filepath.Join(dir, "pods")
				env.KubeletPluginsDir = filepath.Join(dir, "plugins")
				env.KubeletVolumesDir = filepath.Join(dir, "plugins/kubernetes.io")
				env.IsKubernetes = true
				break
			}
		}
	}

	// Detect Kubernetes config directory
	possibleConfigDirs := []string{
		"/etc/kubernetes",
		"/etc/rancher/k3s",
		"/var/snap/microk8s/current/args",
	}

	for _, dir := range possibleConfigDirs {
		if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
			env.KubeConfigDir = dir
			break
		}
	}

	// Detect etcd if present
	possibleEtcdDirs := []string{
		"/var/lib/etcd",
		"/var/etcd/data",
		"/opt/etcd-data",
	}

	for _, dir := range possibleEtcdDirs {
		if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
			env.EtcdDataDir = dir
			break
		}
	}
}

// isValidKubeletDir verifies a directory contains expected kubelet structure
func (env *RuntimeEnvironment) isValidKubeletDir(dir string) bool {
	// Check for key subdirectories that indicate this is a kubelet directory
	expectedSubdirs := []string{"pods", "plugins", "pod-resources"}
	foundCount := 0

	for _, subdir := range expectedSubdirs {
		path := filepath.Join(dir, subdir)
		if stat, err := os.Stat(path); err == nil && stat.IsDir() {
			foundCount++
		}
	}

	// If we find at least 2 of the expected subdirs, it's likely kubelet
	return foundCount >= 2
}

// detectContainerRuntime identifies which container runtime is in use
func (env *RuntimeEnvironment) detectContainerRuntime() {
	// Check for Docker
	if env.checkDocker() {
		env.ContainerRuntime = "docker"
		env.ContainerRootDir = "/var/lib/docker"
		env.ContainerStateDir = "/var/lib/docker/containers"
		env.ContainerLogDir = "/var/lib/docker/containers"
		return
	}

	// Check for containerd
	if env.checkContainerd() {
		env.ContainerRuntime = "containerd"
		env.ContainerRootDir = "/var/lib/containerd"
		env.ContainerStateDir = "/run/containerd"
		// Containerd can have different log locations
		if stat, err := os.Stat("/var/log/pods"); err == nil && stat.IsDir() {
			env.ContainerLogDir = "/var/log/pods"
		} else {
			env.ContainerLogDir = "/var/log/containers"
		}
		return
	}

	// Check for CRI-O
	if env.checkCRIO() {
		env.ContainerRuntime = "cri-o"
		env.ContainerRootDir = "/var/lib/containers"
		env.ContainerStateDir = "/run/containers"
		env.ContainerLogDir = "/var/log/pods"
		return
	}

	env.ContainerRuntime = "unknown"
}

// checkDocker checks if Docker is the container runtime
func (env *RuntimeEnvironment) checkDocker() bool {
	// Check for Docker socket
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}

	// Check for Docker directory structure
	if stat, err := os.Stat("/var/lib/docker"); err == nil && stat.IsDir() {
		// Verify it has expected subdirectories
		if _, err := os.Stat("/var/lib/docker/containers"); err == nil {
			return true
		}
	}

	return false
}

// checkContainerd checks if containerd is the container runtime
func (env *RuntimeEnvironment) checkContainerd() bool {
	// Check for containerd socket
	sockets := []string{
		"/run/containerd/containerd.sock",
		"/var/run/containerd/containerd.sock",
	}

	for _, socket := range sockets {
		if _, err := os.Stat(socket); err == nil {
			return true
		}
	}

	// Check for containerd directory
	if stat, err := os.Stat("/var/lib/containerd"); err == nil && stat.IsDir() {
		return true
	}

	return false
}

// checkCRIO checks if CRI-O is the container runtime
func (env *RuntimeEnvironment) checkCRIO() bool {
	// Check for CRI-O socket
	if _, err := os.Stat("/var/run/crio/crio.sock"); err == nil {
		return true
	}

	// Check for CRI-O directory structure
	if stat, err := os.Stat("/var/lib/containers/storage"); err == nil && stat.IsDir() {
		return true
	}

	return false
}

// detectCSIDrivers discovers installed CSI drivers
func (env *RuntimeEnvironment) detectCSIDrivers() {
	if env.KubeletPluginsDir == "" {
		return
	}

	// Look for CSI driver directories
	csiDir := filepath.Join(env.KubeletPluginsDir, "kubernetes.io/csi")
	entries, err := os.ReadDir(csiDir)
	if err != nil {
		// Try alternative location
		csiDir = filepath.Join(env.KubeletRootDir, "csi-plugins")
		entries, err = os.ReadDir(csiDir)
		if err != nil {
			return
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			env.DetectedCSIDrivers = append(env.DetectedCSIDrivers, entry.Name())
		}
	}

	// Also check for CSI drivers in plugin registration
	registrationDir := filepath.Join(env.KubeletPluginsDir, "registry")
	if entries, err := os.ReadDir(registrationDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasSuffix(name, "-reg.sock") {
				driverName := strings.TrimSuffix(name, "-reg.sock")
				if !contains(env.DetectedCSIDrivers, driverName) {
					env.DetectedCSIDrivers = append(env.DetectedCSIDrivers, driverName)
				}
			}
		}
	}
}

// checkEBPFSupport verifies if eBPF is available on the system
func checkEBPFSupport() bool {
	// Check for BPF filesystem
	if _, err := os.Stat("/sys/fs/bpf"); err != nil {
		return false
	}

	// Check for BTF support (required for CO-RE)
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err != nil {
		return false
	}

	return true
}

// validate checks if the detected environment is usable
func (env *RuntimeEnvironment) validate() error {
	if !env.IsKubernetes && env.KubeletRootDir == "" {
		// Not in Kubernetes, but we can still monitor general filesystem
		// This is okay for development/testing
		return nil
	}

	if env.IsKubernetes && env.KubeletRootDir == "" {
		return fmt.Errorf("detected Kubernetes environment but couldn't find kubelet directory")
	}

	if env.ContainerRuntime == "unknown" && env.IsKubernetes {
		// Warning but not fatal - we can still monitor filesystem
		fmt.Printf("Warning: Could not detect container runtime\n")
	}

	return nil
}

// GetMonitoredPaths returns the list of paths to monitor based on detected environment
func (env *RuntimeEnvironment) GetMonitoredPaths() []string {
	var paths []string

	// Add Kubernetes paths if detected
	if env.IsKubernetes && env.KubeletRootDir != "" {
		paths = append(paths,
			env.KubeletPodsDir,
			env.KubeletPluginsDir,
		)
	}

	// Add container runtime paths if detected
	if env.ContainerStateDir != "" {
		paths = append(paths, env.ContainerStateDir)
	}
	if env.ContainerLogDir != "" {
		paths = append(paths, env.ContainerLogDir)
	}

	// Add etcd path if detected
	if env.EtcdDataDir != "" {
		paths = append(paths, env.EtcdDataDir)
	}

	// Add Kubernetes config if detected
	if env.KubeConfigDir != "" {
		paths = append(paths, env.KubeConfigDir)
	}

	return paths
}

// BuildVolumePathPattern creates a pattern for matching volume paths
func (env *RuntimeEnvironment) BuildVolumePathPattern(volumeType string) string {
	if env.KubeletPodsDir == "" {
		return ""
	}

	// Build pattern based on detected paths, not hardcoded
	// Format: <kubelet-pods-dir>/<pod-uid>/volumes/kubernetes.io~<type>/<name>
	return fmt.Sprintf("%s/*/volumes/kubernetes.io~%s/*", env.KubeletPodsDir, volumeType)
}

// IsKubernetesPath checks if a path belongs to Kubernetes without hardcoded paths
func (env *RuntimeEnvironment) IsKubernetesPath(path string) bool {
	if !env.IsKubernetes {
		return false
	}

	// Check against detected paths
	if env.KubeletRootDir != "" && strings.HasPrefix(path, env.KubeletRootDir) {
		return true
	}

	if env.KubeConfigDir != "" && strings.HasPrefix(path, env.KubeConfigDir) {
		return true
	}

	if env.EtcdDataDir != "" && strings.HasPrefix(path, env.EtcdDataDir) {
		return true
	}

	return false
}

// IsContainerPath checks if a path belongs to container runtime
func (env *RuntimeEnvironment) IsContainerPath(path string) bool {
	if env.ContainerRootDir != "" && strings.HasPrefix(path, env.ContainerRootDir) {
		return true
	}

	if env.ContainerStateDir != "" && strings.HasPrefix(path, env.ContainerStateDir) {
		return true
	}

	if env.ContainerLogDir != "" && strings.HasPrefix(path, env.ContainerLogDir) {
		return true
	}

	return false
}

// contains checks if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
