package capabilities

import (
	"runtime"
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
	// This function will be implemented by build_linux.go
	// It will use the plugins package directly without causing circular imports
	return nil
}

// registerDarwinCapabilities registers macOS-specific capabilities
func registerDarwinCapabilities() error {
	// This function will be implemented by build_darwin.go
	return nil
}

// registerWindowsCapabilities registers Windows-specific capabilities
func registerWindowsCapabilities() error {
	// This function will be implemented by build_windows.go
	return nil
}

// registerGenericCapabilities registers minimal capabilities for unknown platforms
func registerGenericCapabilities() error {
	// This function will be implemented by build_generic.go
	return nil
}
