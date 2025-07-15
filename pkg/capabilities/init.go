package capabilities

import (
	"runtime"

	"github.com/yairfalse/tapio/pkg/capabilities/plugins"
)

// init automatically registers platform-appropriate capabilities
func init() {
	// This will be called when the package is imported
	// Register capabilities based on build tags and platform
	if err := RegisterDefaultCapabilities(); err != nil {
		// Log error but don't fail the program
		// The capability registry will report unavailable capabilities
		return
	}
}

// RegisterDefaultCapabilities registers the default set of capabilities for the current platform
func RegisterDefaultCapabilities() error {
	platform := runtime.GOOS

	// Register platform-appropriate plugins
	switch platform {
	case "linux":
		return registerLinuxCapabilities()
	case "darwin":
		return registerDarwinCapabilities()
	case "windows":
		return registerWindowsCapabilities()
	default:
		return registerGenericCapabilities()
	}
}

// registerLinuxCapabilities registers Linux-specific capabilities
func registerLinuxCapabilities() error {
	// eBPF capabilities (will check availability at runtime)
	if err := Register(plugins.NewEBPFMemoryPlugin(nil)); err != nil {
		return err
	}

	// Native capabilities as fallback
	if err := Register(plugins.NewNativeMemoryPlugin()); err != nil {
		return err
	}

	// Network capabilities (not yet implemented)
	if err := Register(plugins.NewNotAvailablePlugin("ebpf-network", "eBPF network monitoring not yet implemented")); err != nil {
		return err
	}

	// System capabilities (journald)
	if err := Register(plugins.NewNotAvailablePlugin("journald", "journald integration not yet implemented")); err != nil {
		return err
	}

	return nil
}

// registerDarwinCapabilities registers macOS-specific capabilities
func registerDarwinCapabilities() error {
	// Native memory monitoring
	if err := Register(plugins.NewNativeMemoryPlugin()); err != nil {
		return err
	}

	// Other capabilities not available
	if err := Register(plugins.NewNotAvailablePlugin("ebpf-memory", "eBPF only available on Linux")); err != nil {
		return err
	}

	if err := Register(plugins.NewNotAvailablePlugin("native-network", "macOS network monitoring not yet implemented")); err != nil {
		return err
	}

	if err := Register(plugins.NewNotAvailablePlugin("native-system", "macOS system monitoring not yet implemented")); err != nil {
		return err
	}

	return nil
}

// registerWindowsCapabilities registers Windows-specific capabilities
func registerWindowsCapabilities() error {
	// Native memory monitoring
	if err := Register(plugins.NewNativeMemoryPlugin()); err != nil {
		return err
	}

	// Other capabilities not available
	if err := Register(plugins.NewNotAvailablePlugin("ebpf-memory", "eBPF only available on Linux")); err != nil {
		return err
	}

	if err := Register(plugins.NewNotAvailablePlugin("native-network", "Windows network monitoring not yet implemented")); err != nil {
		return err
	}

	if err := Register(plugins.NewNotAvailablePlugin("native-system", "Windows system monitoring not yet implemented")); err != nil {
		return err
	}

	return nil
}

// registerGenericCapabilities registers minimal capabilities for unknown platforms
func registerGenericCapabilities() error {
	// Basic memory monitoring
	if err := Register(plugins.NewNativeMemoryPlugin()); err != nil {
		return err
	}

	// Everything else unavailable
	platform := runtime.GOOS
	if err := Register(plugins.NewNotAvailablePlugin("ebpf-memory", "eBPF only available on Linux")); err != nil {
		return err
	}

	if err := Register(plugins.NewNotAvailablePlugin("native-network", platform+" network monitoring not implemented")); err != nil {
		return err
	}

	if err := Register(plugins.NewNotAvailablePlugin("native-system", platform+" system monitoring not implemented")); err != nil {
		return err
	}

	return nil
}
