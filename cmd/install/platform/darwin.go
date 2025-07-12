//go:build darwin
// +build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// detectPlatformSpecific detects macOS-specific information
func (d *detector) detectPlatformSpecific(info *Info) {
	info.Distribution = "macos"
	info.Version = d.detectMacOSVersion()
	info.Kernel = d.detectKernelVersion()
	info.HasSystemd = false // macOS uses launchd
	info.PackageManager = d.detectPackageManager()
}

// detectMacOSVersion detects the macOS version
func (d *detector) detectMacOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// detectKernelVersion detects the Darwin kernel version
func (d *detector) detectKernelVersion() string {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// detectPackageManager detects the package manager
func (d *detector) detectPackageManager() string {
	// Check for Homebrew
	if _, err := exec.LookPath("brew"); err == nil {
		return "homebrew"
	}

	// Check for MacPorts
	if _, err := exec.LookPath("port"); err == nil {
		return "macports"
	}

	return "none"
}

// GetServiceManager returns the init system type
func GetServiceManager() string {
	return "launchd"
}

// InstallService installs a launchd service
func InstallService(name, execPath, workingDir string) error {
	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.tapio.%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
        <key>Crashed</key>
        <true/>
    </dict>
    <key>StandardOutPath</key>
    <string>/usr/local/var/log/tapio-%s.log</string>
    <key>StandardErrorPath</key>
    <string>/usr/local/var/log/tapio-%s.error.log</string>
</dict>
</plist>`, name, execPath, workingDir, name, name)

	// Determine plist location
	var plistPath string
	if os.Getuid() == 0 {
		// System-wide daemon
		plistPath = fmt.Sprintf("/Library/LaunchDaemons/com.tapio.%s.plist", name)
	} else {
		// User agent
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		plistPath = filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.tapio.%s.plist", name))

		// Create LaunchAgents directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
			return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
		}
	}

	// Write plist file
	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	// Load the service
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to load service: %w", err)
	}

	return nil
}

// UninstallService uninstalls a launchd service
func UninstallService(name string) error {
	var plistPath string
	if os.Getuid() == 0 {
		plistPath = fmt.Sprintf("/Library/LaunchDaemons/com.tapio.%s.plist", name)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		plistPath = filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.tapio.%s.plist", name))
	}

	// Unload the service
	exec.Command("launchctl", "unload", plistPath).Run()

	// Remove the plist file
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	return nil
}

// GetApplicationSupportDir returns the Application Support directory
func GetApplicationSupportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Tapio"), nil
}

// GetLogsDir returns the logs directory
func GetLogsDir() (string, error) {
	if os.Getuid() == 0 {
		return "/var/log/tapio", nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "Tapio"), nil
}

// CheckCodeSigning checks if the binary is properly code signed
func CheckCodeSigning(binaryPath string) error {
	cmd := exec.Command("codesign", "-v", binaryPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("binary is not properly code signed: %w", err)
	}
	return nil
}

// CheckNotarization checks if the binary is notarized
func CheckNotarization(binaryPath string) error {
	cmd := exec.Command("spctl", "-a", "-v", binaryPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary is not notarized: %s", string(output))
	}
	return nil
}
