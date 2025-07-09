package collectors

import (
	"context"
	"os"
	"runtime"
	"strings"
)

// Platform represents the current platform
type Platform struct {
	OS           string
	Architecture string
	IsLinux      bool
	IsDarwin     bool
	IsWindows    bool
	SupportseBPF bool
}

// DetectPlatform detects the current platform and its capabilities
func DetectPlatform() *Platform {
	p := &Platform{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		IsLinux:      runtime.GOOS == "linux",
		IsDarwin:     runtime.GOOS == "darwin",
		IsWindows:    runtime.GOOS == "windows",
	}

	// eBPF is only available on Linux with sufficient kernel version
	p.SupportseBPF = p.IsLinux && haseBPFSupport()

	return p
}

// haseBPFSupport checks if the Linux kernel supports eBPF
func haseBPFSupport() bool {
	// Check if we're in a container or have limited capabilities
	if isInContainer() {
		return false
	}

	// Check for eBPF filesystem
	if _, err := os.Stat("/sys/fs/bpf"); os.IsNotExist(err) {
		return false
	}

	// Check kernel version (rough check)
	if kernelVersion, err := getKernelVersion(); err == nil {
		// Require at least kernel 4.18 for stable eBPF support
		return kernelVersion >= "4.18"
	}

	return false
}

// isInContainer checks if we're running inside a container
func isInContainer() bool {
	// Check for container indicators
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check cgroup for container indicators
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "kubepods") ||
			strings.Contains(content, "containerd") {
			return true
		}
	}

	return false
}

// getKernelVersion gets the kernel version string
func getKernelVersion() (string, error) {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", err
	}

	version := string(data)
	if strings.Contains(version, "Linux version ") {
		parts := strings.Split(version, " ")
		if len(parts) >= 3 {
			return parts[2], nil
		}
	}

	return "", nil
}

// Capabilities represents what the platform can do
type Capabilities struct {
	CanAccessKernelTracing  bool
	CanAccessNetworkTracing bool
	CanAccessProcessTracing bool
	CanAccessFileSystem     bool
	HasContainerRuntime     bool
	HasKubernetes           bool
}

// DetectCapabilities detects what monitoring capabilities are available
func DetectCapabilities(ctx context.Context) *Capabilities {
	platform := DetectPlatform()

	caps := &Capabilities{
		CanAccessKernelTracing:  platform.SupportseBPF,
		CanAccessNetworkTracing: platform.SupportseBPF,
		CanAccessProcessTracing: platform.IsLinux || platform.IsDarwin,
		CanAccessFileSystem:     true,
		HasContainerRuntime:     hasContainerRuntime(),
		HasKubernetes:           hasKubernetes(),
	}

	return caps
}

// hasContainerRuntime checks if a container runtime is available
func hasContainerRuntime() bool {
	// Check for Docker
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}

	// Check for containerd
	if _, err := os.Stat("/run/containerd/containerd.sock"); err == nil {
		return true
	}

	// Check for Podman
	if _, err := os.Stat("/run/podman/podman.sock"); err == nil {
		return true
	}

	return false
}

// hasKubernetes checks if Kubernetes is available
func hasKubernetes() bool {
	// Check for in-cluster config
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return true
	}

	// Check for kubeconfig
	if home := os.Getenv("HOME"); home != "" {
		if _, err := os.Stat(home + "/.kube/config"); err == nil {
			return true
		}
	}

	// Check KUBECONFIG environment variable
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		if _, err := os.Stat(kubeconfig); err == nil {
			return true
		}
	}

	return false
}
