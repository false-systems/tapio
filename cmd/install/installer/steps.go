package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/yairfalse/tapio/cmd/install/platform"
	"github.com/yairfalse/tapio/cmd/install/validation"
)

// installBinaryStep installs the binary to the target location
type installBinaryStep struct {
	platform platform.Info
}

func (s *installBinaryStep) Name() string { return "install-binary" }

func (s *installBinaryStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	if data.Options.Progress != nil {
		data.Options.Progress.Start("Installing binary", 0)
		defer data.Options.Progress.Complete("Installing binary")
	}

	// Create install directory
	installPath := data.Options.InstallPath
	if err := os.MkdirAll(installPath, 0755); err != nil {
		return data, fmt.Errorf("failed to create install directory: %w", err)
	}

	// Backup existing binary if it exists
	targetPath := filepath.Join(installPath, filepath.Base(data.BinaryPath))
	if _, err := os.Stat(targetPath); err == nil {
		backupPath := targetPath + ".backup"
		if err := os.Rename(targetPath, backupPath); err != nil {
			return data, fmt.Errorf("failed to backup existing binary: %w", err)
		}
		data.BackupPath = backupPath
	}

	// Copy binary with atomic operation
	if err := s.atomicCopy(data.BinaryPath, targetPath); err != nil {
		// Restore backup if copy failed
		if data.BackupPath != "" {
			os.Rename(data.BackupPath, targetPath)
		}
		return data, fmt.Errorf("failed to install binary: %w", err)
	}

	// Set permissions
	if runtime.GOOS != "windows" {
		if err := os.Chmod(targetPath, 0755); err != nil {
			return data, fmt.Errorf("failed to set binary permissions: %w", err)
		}
	}

	// Update PATH on Windows
	if runtime.GOOS == "windows" {
		if err := platform.AddToPath(installPath); err != nil {
			data.Options.Progress.Log("warn", "Failed to add to PATH", "error", err)
		}
	}

	// Remove backup after successful installation
	if data.BackupPath != "" {
		os.Remove(data.BackupPath)
	}

	return data, nil
}

func (s *installBinaryStep) atomicCopy(src, dst string) error {
	// Copy to temp file first
	tmpDst := dst + ".tmp"

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(tmpDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		os.Remove(tmpDst)
		return err
	}

	if err := dstFile.Close(); err != nil {
		os.Remove(tmpDst)
		return err
	}

	// Atomic rename
	return os.Rename(tmpDst, dst)
}

func (s *installBinaryStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	targetPath := filepath.Join(data.Options.InstallPath, filepath.Base(data.BinaryPath))

	// Remove installed binary
	os.Remove(targetPath)

	// Restore backup if exists
	if data.BackupPath != "" {
		return os.Rename(data.BackupPath, targetPath)
	}

	return nil
}

func (s *installBinaryStep) Validate(ctx context.Context, data *binaryInstallData) error {
	targetPath := filepath.Join(data.Options.InstallPath, filepath.Base(data.BinaryPath))

	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("binary not found: %w", err)
	}

	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		return fmt.Errorf("binary is not executable")
	}

	return nil
}

// createConfigStep creates default configuration
type createConfigStep struct{}

func (s *createConfigStep) Name() string { return "create-config" }

func (s *createConfigStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	if data.Options.Progress != nil {
		data.Options.Progress.Start("Creating configuration", 0)
		defer data.Options.Progress.Complete("Creating configuration")
	}

	// Create config directory
	configPath := data.Options.ConfigPath
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return data, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create default config file if it doesn't exist
	configFile := filepath.Join(configPath, "tapio.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := `# Tapio Configuration
version: 1

# Server settings
server:
  address: "0.0.0.0:8080"
  tls:
    enabled: false
    cert: ""
    key: ""

# Data collection settings
collection:
  interval: 60s
  buffer_size: 1000

# Storage settings
storage:
  type: "local"
  path: "%s"

# Logging settings
logging:
  level: "info"
  format: "json"
  output: "stdout"
`
		configContent := fmt.Sprintf(defaultConfig, data.Options.DataPath)

		if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
			return data, fmt.Errorf("failed to create config file: %w", err)
		}
	}

	// Create data directory
	if err := os.MkdirAll(data.Options.DataPath, 0755); err != nil {
		return data, fmt.Errorf("failed to create data directory: %w", err)
	}

	return data, nil
}

func (s *createConfigStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	// Only remove config if we created it
	configFile := filepath.Join(data.Options.ConfigPath, "tapio.yaml")

	// Check if file was created recently (within last minute)
	if info, err := os.Stat(configFile); err == nil {
		if time.Since(info.ModTime()) < time.Minute {
			os.Remove(configFile)
		}
	}

	return nil
}

func (s *createConfigStep) Validate(ctx context.Context, data *binaryInstallData) error {
	configFile := filepath.Join(data.Options.ConfigPath, "tapio.yaml")

	if _, err := os.Stat(configFile); err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	return nil
}

// setupServiceStep sets up the system service
type setupServiceStep struct {
	platform platform.Info
}

func (s *setupServiceStep) Name() string { return "setup-service" }

func (s *setupServiceStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	if data.Options.Progress != nil {
		data.Options.Progress.Start("Setting up service", 0)
		defer data.Options.Progress.Complete("Setting up service")
	}

	// Skip service setup in containers
	if s.platform.IsContainer {
		data.Options.Progress.Log("info", "Skipping service setup in container environment")
		return data, nil
	}

	binaryPath := filepath.Join(data.Options.InstallPath, "tapio")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	// Install service based on platform
	if err := platform.InstallService("tapio", binaryPath, data.Options.InstallPath); err != nil {
		return data, fmt.Errorf("failed to install service: %w", err)
	}

	// Start service if not in dry run mode
	if !data.Options.DryRun {
		switch runtime.GOOS {
		case "windows":
			if err := platform.StartService("tapio"); err != nil {
				data.Options.Progress.Log("warn", "Failed to start service", "error", err)
			}
		case "darwin":
			// Service is started automatically by launchd
		case "linux":
			if s.platform.HasSystemd {
				if err := exec.Command("systemctl", "start", "tapio").Run(); err != nil {
					data.Options.Progress.Log("warn", "Failed to start service", "error", err)
				}
			}
		}
	}

	return data, nil
}

func (s *setupServiceStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	// Remove service
	switch runtime.GOOS {
	case "windows":
		return platform.UninstallService("tapio")
	case "darwin":
		return platform.UninstallService("tapio")
	case "linux":
		if s.platform.HasSystemd {
			exec.Command("systemctl", "stop", "tapio").Run()
			exec.Command("systemctl", "disable", "tapio").Run()
			os.Remove("/etc/systemd/system/tapio.service")
			exec.Command("systemctl", "daemon-reload").Run()
		}
	}

	return nil
}

func (s *setupServiceStep) Validate(ctx context.Context, data *binaryInstallData) error {
	// Skip validation in containers
	if s.platform.IsContainer {
		return nil
	}

	// Validate service is installed
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("sc.exe", "query", "tapio")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("service not found")
		}
	case "darwin":
		// Check launchd plist exists
		plistPath := "/Library/LaunchDaemons/com.tapio.tapio.plist"
		if os.Getuid() != 0 {
			home, _ := os.UserHomeDir()
			plistPath = filepath.Join(home, "Library/LaunchAgents/com.tapio.tapio.plist")
		}
		if _, err := os.Stat(plistPath); err != nil {
			return fmt.Errorf("launch daemon not found")
		}
	case "linux":
		if s.platform.HasSystemd {
			if _, err := os.Stat("/etc/systemd/system/tapio.service"); err != nil {
				return fmt.Errorf("systemd service not found")
			}
		}
	}

	return nil
}

// validateStep performs final validation
type validateStep struct{}

func (s *validateStep) Name() string { return "validate" }

func (s *validateStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	if data.Options.SkipValidation {
		return data, nil
	}

	if data.Options.Progress != nil {
		data.Options.Progress.Start("Validating installation", 0)
		defer data.Options.Progress.Complete("Validating installation")
	}

	validator := validation.NewValidator()

	// Validate binary
	binaryPath := filepath.Join(data.Options.InstallPath, "tapio")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	if err := validator.ValidateBinary(ctx, binaryPath, data.Checksum); err != nil {
		return data, err
	}

	// Test binary execution
	cmd := exec.Command(binaryPath, "version")
	if output, err := cmd.CombinedOutput(); err != nil {
		return data, fmt.Errorf("binary execution test failed: %w\nOutput: %s", err, string(output))
	}

	// Validate connectivity if service is running
	endpoints := []string{"http://localhost:8080/health"}
	if err := validator.ValidateConnectivity(ctx, endpoints); err != nil {
		data.Options.Progress.Log("warn", "Service connectivity check failed", "error", err)
	}

	return data, nil
}

func (s *validateStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	// Nothing to rollback for validation
	return nil
}

func (s *validateStep) Validate(ctx context.Context, data *binaryInstallData) error {
	// Validation step validates itself by running
	return nil
}
