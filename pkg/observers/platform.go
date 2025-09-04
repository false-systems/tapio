package observers

import (
	"runtime"
)

// Platform represents the current platform
type Platform struct {
	OS           string
	Architecture string
	HasEBPF      bool
	HasJournald  bool
	HasSystemd   bool
}

// GetCurrentPlatform returns platform information
func GetCurrentPlatform() Platform {
	return Platform{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		HasEBPF:      hasEBPFSupport(),
		HasJournald:  hasJournaldSupport(),
		HasSystemd:   hasSystemdSupport(),
	}
}

// hasEBPFSupport checks if eBPF is available on the current platform
func hasEBPFSupport() bool {
	return runtime.GOOS == "linux"
}

// hasJournaldSupport checks if journald is available on the current platform
func hasJournaldSupport() bool {
	return runtime.GOOS == "linux"
}

// hasSystemdSupport checks if systemd is available on the current platform
func hasSystemdSupport() bool {
	return runtime.GOOS == "linux"
}

// GetSupportedObservers returns the list of observers supported on the current platform
func GetSupportedObservers() []string {
	platform := GetCurrentPlatform()
	supported := []string{}

	// Basic observers that work on all platforms
	supported = append(supported, "simple", "basic")

	// Platform-specific observers
	if platform.HasEBPF {
		supported = append(supported, "kernel", "memory", "network", "circuit")
	}

	if platform.HasJournald {
		supported = append(supported, "journald", "systemd")
	}

	// Mock observers for development (always available)
	supported = append(supported, "mock", "stub")

	return supported
}

// IsObserverSupported checks if an observer is supported on the current platform
func IsObserverSupported(observerType string) bool {
	supported := GetSupportedObservers()
	for _, s := range supported {
		if s == observerType {
			return true
		}
	}
	return false
}

// GetPlatformMessage returns a descriptive message about platform support
func GetPlatformMessage(observerType string) string {
	platform := GetCurrentPlatform()

	switch observerType {
	case "kernel":
		if platform.HasEBPF {
			return "Kernel observer is supported on this Linux system"
		}
		return "Kernel observer is only supported on Linux (stub implementation active)"
	case "journald":
		if platform.HasJournald {
			return "Journald observer is supported on this Linux system"
		}
		return "Journald observer is only supported on Linux (stub implementation active)"
	case "systemd":
		if platform.HasSystemd {
			return "Systemd observer is supported on this Linux system"
		}
		return "Systemd observer is only supported on Linux (stub implementation active)"
	case "circuit":
		if platform.HasEBPF {
			return "Circuit observer is supported on this Linux system"
		}
		return "Circuit observer is only supported on Linux (stub implementation active)"
	default:
		return "Observer is supported on all platforms"
	}
}
