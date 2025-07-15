package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

// AddToPath adds a directory to the system PATH
func AddToPath(path string) error {
	// TODO: Implement proper PATH modification for each OS
	return nil
}

// InstallService installs a system service
func InstallService(name, binaryPath, installPath string) error {
	switch runtime.GOOS {
	case "linux":
		// TODO: Implement systemd service installation
		return nil
	case "darwin":
		// TODO: Implement launchd service installation
		return nil
	case "windows":
		// TODO: Implement Windows service installation
		return nil
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// StartService starts a system service
func StartService(name string) error {
	switch runtime.GOOS {
	case "linux":
		cmd := exec.Command("systemctl", "start", name)
		return cmd.Run()
	case "darwin":
		cmd := exec.Command("launchctl", "start", name)
		return cmd.Run()
	case "windows":
		cmd := exec.Command("sc", "start", name)
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// UninstallService removes a system service
func UninstallService(name string) error {
	switch runtime.GOOS {
	case "linux":
		// Stop service first
		_ = exec.Command("systemctl", "stop", name).Run()
		// Then disable it
		return exec.Command("systemctl", "disable", name).Run()
	case "darwin":
		return exec.Command("launchctl", "unload", name).Run()
	case "windows":
		return exec.Command("sc", "delete", name).Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
