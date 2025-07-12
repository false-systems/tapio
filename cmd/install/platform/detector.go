package platform

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Info contains platform information
type Info struct {
	OS           string
	Arch         string
	Distribution string
	Version      string
	Kernel       string
	IsContainer  bool
	IsWSL        bool
	HasSystemd   bool
	PackageManager string
}

// Detector detects platform information
type Detector interface {
	Detect() Info
	IsSupported(os, arch string) bool
	GetRequirements() Requirements
}

// Requirements describes platform requirements
type Requirements struct {
	MinKernelVersion string
	RequiredLibs     []string
	RequiredCommands []string
	MinDiskSpace     int64
	MinMemory        int64
}

// detector implements the Detector interface
type detector struct{}

// NewDetector creates a new platform detector
func NewDetector() Detector {
	return &detector{}
}

// Detect detects the current platform
func (d *detector) Detect() Info {
	info := Info{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	
	// Check if running in container
	info.IsContainer = d.isContainer()
	
	// Check if running in WSL
	info.IsWSL = d.isWSL()
	
	// Platform-specific detection
	d.detectPlatformSpecific(&info)
	
	return info
}

// IsSupported checks if the platform is supported
func (d *detector) IsSupported(os, arch string) bool {
	supportedPlatforms := map[string][]string{
		"linux":   {"amd64", "arm64", "arm"},
		"darwin":  {"amd64", "arm64"},
		"windows": {"amd64"},
	}
	
	arches, ok := supportedPlatforms[os]
	if !ok {
		return false
	}
	
	for _, a := range arches {
		if a == arch {
			return true
		}
	}
	
	return false
}

// GetRequirements returns platform requirements
func (d *detector) GetRequirements() Requirements {
	return Requirements{
		MinKernelVersion: "3.10",
		RequiredLibs:     []string{},
		RequiredCommands: []string{"tar", "gzip"},
		MinDiskSpace:     1 << 30, // 1GB
		MinMemory:        512 << 20, // 512MB
	}
}

// isContainer checks if running in a container
func (d *detector) isContainer() bool {
	// Check for .dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	
	// Check cgroup
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		if strings.Contains(string(data), "docker") || 
		   strings.Contains(string(data), "containerd") ||
		   strings.Contains(string(data), "kubepods") {
			return true
		}
	}
	
	// Check for Kubernetes environment variables
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	
	return false
}

// isWSL checks if running in WSL
func (d *detector) isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	
	// Check for WSL-specific files
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/WSLInterop"); err == nil {
		return true
	}
	
	// Check kernel version
	if data, err := os.ReadFile("/proc/version"); err == nil {
		if strings.Contains(strings.ToLower(string(data)), "microsoft") {
			return true
		}
	}
	
	return false
}

// PlatformError represents a platform-specific error
type PlatformError struct {
	Platform string
	Message  string
}

func (e *PlatformError) Error() string {
	return fmt.Sprintf("platform error (%s): %s", e.Platform, e.Message)
}

// UnsupportedPlatformError indicates an unsupported platform
type UnsupportedPlatformError struct {
	OS   string
	Arch string
}

func (e *UnsupportedPlatformError) Error() string {
	return fmt.Sprintf("unsupported platform: %s/%s", e.OS, e.Arch)
}