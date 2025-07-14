package build

import (
	"runtime"
	"strings"
)

// BuildTags represents the build tags for conditional compilation
type BuildTags struct {
	Platform string
	Features []string
}

// GetDefaultTags returns the default build tags for the current platform
func GetDefaultTags() BuildTags {
	tags := BuildTags{
		Platform: runtime.GOOS,
		Features: []string{},
	}

	switch runtime.GOOS {
	case "linux":
		tags.Features = append(tags.Features, "linux", "unix")
		// Add eBPF support check
		if hasEBPFSupport() {
			tags.Features = append(tags.Features, "ebpf")
		}
		// Add journald support
		if hasJournaldSupport() {
			tags.Features = append(tags.Features, "journald", "systemd")
		}
	case "darwin":
		tags.Features = append(tags.Features, "darwin", "unix")
	case "windows":
		tags.Features = append(tags.Features, "windows")
	default:
		tags.Features = append(tags.Features, "unknown")
	}

	return tags
}

// String returns the build tags as a string
func (bt BuildTags) String() string {
	return strings.Join(bt.Features, ",")
}

// HasFeature checks if a feature is supported
func (bt BuildTags) HasFeature(feature string) bool {
	for _, f := range bt.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// hasEBPFSupport checks for eBPF support
func hasEBPFSupport() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	
	// TODO: Add actual eBPF availability check
	// For now, assume Linux has eBPF support
	return true
}

// hasJournaldSupport checks for journald support
func hasJournaldSupport() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	
	// TODO: Add actual journald availability check
	// For now, assume Linux has journald support
	return true
}

// GenerateBuildConstraints generates build constraints for files
func GenerateBuildConstraints(requireLinux bool, requireEBPF bool) string {
	var constraints []string
	
	if requireLinux {
		constraints = append(constraints, "linux")
	}
	
	if requireEBPF {
		constraints = append(constraints, "ebpf")
	}
	
	if len(constraints) == 0 {
		return ""
	}
	
	return "//go:build " + strings.Join(constraints, " && ")
}

// GenerateStubConstraints generates build constraints for stub files
func GenerateStubConstraints(requireLinux bool, requireEBPF bool) string {
	var constraints []string
	
	if requireLinux {
		constraints = append(constraints, "!linux")
	}
	
	if requireEBPF {
		constraints = append(constraints, "!ebpf")
	}
	
	if len(constraints) == 0 {
		return ""
	}
	
	return "//go:build " + strings.Join(constraints, " || ")
}