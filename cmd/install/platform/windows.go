//go:build windows
// +build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	
	"unsafe"
	
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// detectPlatformSpecific detects Windows-specific information
func (d *detector) detectPlatformSpecific(info *Info) {
	info.Distribution = "windows"
	info.Version = d.detectWindowsVersion()
	info.Kernel = d.detectWindowsBuild()
	info.HasSystemd = false
	info.PackageManager = d.detectPackageManager()
}

// detectWindowsVersion detects the Windows version
func (d *detector) detectWindowsVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "unknown"
	}
	defer k.Close()
	
	productName, _, err := k.GetStringValue("ProductName")
	if err != nil {
		return "unknown"
	}
	
	return productName
}

// detectWindowsBuild detects the Windows build number
func (d *detector) detectWindowsBuild() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "unknown"
	}
	defer k.Close()
	
	build, _, err := k.GetStringValue("CurrentBuild")
	if err != nil {
		return "unknown"
	}
	
	ubr, _, err := k.GetIntegerValue("UBR")
	if err == nil && ubr > 0 {
		return fmt.Sprintf("%s.%d", build, ubr)
	}
	
	return build
}

// detectPackageManager detects the package manager
func (d *detector) detectPackageManager() string {
	// Check for Chocolatey
	if _, err := exec.LookPath("choco"); err == nil {
		return "chocolatey"
	}
	
	// Check for Scoop
	if os.Getenv("SCOOP") != "" {
		return "scoop"
	}
	
	// Check for winget
	if _, err := exec.LookPath("winget"); err == nil {
		return "winget"
	}
	
	return "none"
}

// GetServiceManager returns the service manager type
func GetServiceManager() string {
	return "windows-service"
}

// InstallService installs a Windows service
func InstallService(name, execPath, workingDir string) error {
	// Create service using sc.exe
	displayName := fmt.Sprintf("Tapio %s", name)
	description := fmt.Sprintf("Tapio %s service", name)
	
	// Create the service
	cmd := exec.Command("sc.exe", "create", name,
		fmt.Sprintf("binPath=%s", execPath),
		fmt.Sprintf("DisplayName=%s", displayName),
		"start=auto")
	
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create service: %w\nOutput: %s", err, string(output))
	}
	
	// Set service description
	cmd = exec.Command("sc.exe", "description", name, description)
	cmd.Run() // Ignore errors as this is optional
	
	// Configure service recovery
	cmd = exec.Command("sc.exe", "failure", name,
		"reset=86400",
		"actions=restart/60000/restart/60000/restart/60000")
	cmd.Run() // Ignore errors as this is optional
	
	return nil
}

// UninstallService uninstalls a Windows service
func UninstallService(name string) error {
	// Stop the service first
	exec.Command("sc.exe", "stop", name).Run()
	
	// Delete the service
	cmd := exec.Command("sc.exe", "delete", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete service: %w\nOutput: %s", err, string(output))
	}
	
	return nil
}

// StartService starts a Windows service
func StartService(name string) error {
	cmd := exec.Command("sc.exe", "start", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start service: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// StopService stops a Windows service
func StopService(name string) error {
	cmd := exec.Command("sc.exe", "stop", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop service: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// GetProgramDataDir returns the ProgramData directory
func GetProgramDataDir() (string, error) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = "C:\\ProgramData"
	}
	return filepath.Join(programData, "Tapio"), nil
}

// GetAppDataDir returns the AppData directory
func GetAppDataDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("APPDATA environment variable not set")
	}
	return filepath.Join(appData, "Tapio"), nil
}

// IsAdmin checks if running with administrator privileges
func IsAdmin() bool {
	var sid *windows.SID
	
	// Well-known SID for the Administrators group
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	
	// Check if the current process token contains the SID
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	
	return member
}

// ElevateIfNeeded attempts to restart the process with elevated privileges
func ElevateIfNeeded() error {
	if IsAdmin() {
		return nil
	}
	
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	
	// Prepare the command line
	args := strings.Join(os.Args[1:], " ")
	
	// Use ShellExecute to run with elevation
	verb := "runas"
	cwd, _ := os.Getwd()
	
	err = windows.ShellExecute(0, 
		syscall.StringToUTF16Ptr(verb),
		syscall.StringToUTF16Ptr(exe),
		syscall.StringToUTF16Ptr(args),
		syscall.StringToUTF16Ptr(cwd),
		windows.SW_NORMAL)
	
	if err != nil {
		return fmt.Errorf("failed to elevate privileges: %w", err)
	}
	
	// Exit the current process
	os.Exit(0)
	return nil
}

// AddToPath adds a directory to the system PATH
func AddToPath(dir string) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()
	
	// Get current PATH
	path, _, err := k.GetStringValue("Path")
	if err != nil {
		return fmt.Errorf("failed to read PATH: %w", err)
	}
	
	// Check if already in PATH
	paths := strings.Split(path, ";")
	for _, p := range paths {
		if strings.EqualFold(p, dir) {
			return nil // Already in PATH
		}
	}
	
	// Add to PATH
	newPath := path + ";" + dir
	if err := k.SetStringValue("Path", newPath); err != nil {
		return fmt.Errorf("failed to update PATH: %w", err)
	}
	
	// Notify system of environment change
	windows.SendMessage(windows.HWND_BROADCAST, windows.WM_SETTINGCHANGE, 0, 
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Environment"))))
	
	return nil
}