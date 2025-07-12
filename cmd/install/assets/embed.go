package assets

import (
	_ "embed"
)

// Embedded installation scripts

//go:embed scripts/install.sh
var InstallScript string

// GetInstallScript returns the installation script for the given platform
func GetInstallScript(platform string) (string, bool) {
	switch platform {
	case "linux", "darwin":
		return InstallScript, true
	default:
		return "", false
	}
}