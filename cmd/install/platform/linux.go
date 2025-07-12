//go:build linux
// +build linux

package platform

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// detectPlatformSpecific detects Linux-specific information
func (d *detector) detectPlatformSpecific(info *Info) {
	// Detect distribution
	info.Distribution = d.detectLinuxDistro()
	info.Version = d.detectLinuxVersion()
	info.Kernel = d.detectKernelVersion()
	
	// Check for systemd
	info.HasSystemd = d.hasSystemd()
	
	// Detect package manager
	info.PackageManager = d.detectPackageManager()
}

// detectLinuxDistro detects the Linux distribution
func (d *detector) detectLinuxDistro() string {
	// Try /etc/os-release first
	if distro := d.parseOSRelease(); distro != "" {
		return distro
	}
	
	// Try lsb_release
	if distro := d.parseLSBRelease(); distro != "" {
		return distro
	}
	
	// Check for specific distro files
	distroFiles := map[string]string{
		"/etc/debian_version": "debian",
		"/etc/redhat-release": "redhat",
		"/etc/fedora-release": "fedora",
		"/etc/centos-release": "centos",
		"/etc/arch-release":   "arch",
		"/etc/alpine-release": "alpine",
		"/etc/gentoo-release": "gentoo",
	}
	
	for file, distro := range distroFiles {
		if _, err := os.Stat(file); err == nil {
			return distro
		}
	}
	
	return "unknown"
}

// detectLinuxVersion detects the distribution version
func (d *detector) detectLinuxVersion() string {
	// Parse /etc/os-release for VERSION_ID
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VERSION_ID=") {
			version := strings.TrimPrefix(line, "VERSION_ID=")
			return strings.Trim(version, `"`)
		}
	}
	
	return "unknown"
}

// detectKernelVersion detects the kernel version
func (d *detector) detectKernelVersion() string {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// parseOSRelease parses /etc/os-release
func (d *detector) parseOSRelease() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			return strings.Trim(id, `"`)
		}
	}
	
	return ""
}

// parseLSBRelease parses lsb_release output
func (d *detector) parseLSBRelease() string {
	cmd := exec.Command("lsb_release", "-si")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(output)))
}

// hasSystemd checks if systemd is available
func (d *detector) hasSystemd() bool {
	// Check if systemd is PID 1
	if target, err := os.Readlink("/proc/1/exe"); err == nil {
		if strings.Contains(target, "systemd") {
			return true
		}
	}
	
	// Check if systemctl exists
	if _, err := exec.LookPath("systemctl"); err == nil {
		return true
	}
	
	return false
}

// detectPackageManager detects the system package manager
func (d *detector) detectPackageManager() string {
	packageManagers := []struct {
		cmd  string
		name string
	}{
		{"apt-get", "apt"},
		{"yum", "yum"},
		{"dnf", "dnf"},
		{"zypper", "zypper"},
		{"pacman", "pacman"},
		{"apk", "apk"},
		{"emerge", "portage"},
		{"snap", "snap"},
		{"flatpak", "flatpak"},
	}
	
	for _, pm := range packageManagers {
		if _, err := exec.LookPath(pm.cmd); err == nil {
			return pm.name
		}
	}
	
	return "unknown"
}

// GetServiceManager returns the init system type
func GetServiceManager() string {
	// Check for systemd
	if _, err := exec.LookPath("systemctl"); err == nil {
		return "systemd"
	}
	
	// Check for openrc
	if _, err := exec.LookPath("rc-service"); err == nil {
		return "openrc"
	}
	
	// Check for upstart
	if _, err := exec.LookPath("initctl"); err == nil {
		return "upstart"
	}
	
	// Check for sysvinit
	if _, err := os.Stat("/etc/init.d"); err == nil {
		return "sysvinit"
	}
	
	return "unknown"
}

// InstallService installs a system service
func InstallService(name, execPath, workingDir string) error {
	serviceManager := GetServiceManager()
	
	switch serviceManager {
	case "systemd":
		return installSystemdService(name, execPath, workingDir)
	case "sysvinit":
		return installSysVInitService(name, execPath, workingDir)
	default:
		return fmt.Errorf("unsupported service manager: %s", serviceManager)
	}
}

// installSystemdService installs a systemd service
func installSystemdService(name, execPath, workingDir string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=Tapio %s
After=network.target

[Service]
Type=simple
User=tapio
ExecStart=%s
Restart=on-failure
RestartSec=10
WorkingDirectory=%s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, name, execPath, workingDir)
	
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", name)
	
	// Write service file
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}
	
	// Reload systemd
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}
	
	// Enable service
	if err := exec.Command("systemctl", "enable", name).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	
	return nil
}

// installSysVInitService installs a SysV init service
func installSysVInitService(name, execPath, workingDir string) error {
	// This is a simplified implementation
	// Real implementation would create proper init script
	return fmt.Errorf("SysV init service installation not implemented")
}