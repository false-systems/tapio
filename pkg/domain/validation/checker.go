package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Validator implements the installer.Validator interface
type Validator struct {
	timeout time.Duration
}

// NewValidator creates a new validator
func NewValidator() common.Validator {
	return &Validator{
		timeout: 30 * time.Second,
	}
}

// ValidateInstallation checks if installation is valid
func (v *Validator) ValidateInstallation(ctx context.Context, installPath string) error {
	checks := []struct {
		name string
		fn   func() error
	}{
		{"directory structure", func() error { return v.checkDirectoryStructure(installPath) }},
		{"binary presence", func() error { return v.checkBinaryPresence(installPath) }},
		{"permissions", func() error { return v.checkPathPermissions(installPath) }},
		{"dependencies", func() error { return v.checkDependencies() }},
	}

	for _, check := range checks {
		if err := check.fn(); err != nil {
			return fmt.Errorf("%s check failed: %w", check.name, err)
		}
	}

	return nil
}

// ValidateBinary checks binary integrity
func (v *Validator) ValidateBinary(ctx context.Context, binaryPath string, expectedChecksum string) error {
	// Check file exists
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary not found: %w", err)
	}

	// Check file is not empty
	if info.Size() == 0 {
		return fmt.Errorf("binary file is empty")
	}

	// Check executable permissions on Unix
	if runtime.GOOS != "windows" {
		if info.Mode()&0111 == 0 {
			return fmt.Errorf("binary is not executable")
		}
	}

	// Verify checksum if provided
	if expectedChecksum != "" {
		file, err := os.Open(binaryPath)
		if err != nil {
			return fmt.Errorf("failed to open binary: %w", err)
		}
		defer file.Close()

		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return fmt.Errorf("failed to calculate checksum: %w", err)
		}

		calculated := hex.EncodeToString(hash.Sum(nil))
		if calculated != expectedChecksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, calculated)
		}
	}

	// Test binary execution
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary execution failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ValidateConnectivity checks network connectivity
func (v *Validator) ValidateConnectivity(ctx context.Context, endpoints []string) error {
	checker := NewConnectivityChecker()
	return checker.CheckEndpoints(ctx, endpoints)
}

// ValidatePermissions checks file permissions
func (v *Validator) ValidatePermissions(ctx context.Context, paths []string) error {
	for _, path := range paths {
		if err := v.checkPathPermissions(path); err != nil {
			return fmt.Errorf("permission check failed for %s: %w", path, err)
		}
	}
	return nil
}

// checkDirectoryStructure verifies the installation directory structure
func (v *Validator) checkDirectoryStructure(installPath string) error {
	requiredDirs := []string{
		installPath,
		filepath.Join(installPath, "bin"),
		filepath.Join(installPath, "config"),
		filepath.Join(installPath, "data"),
	}

	for _, dir := range requiredDirs {
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("required directory %s does not exist", dir)
			}
			return fmt.Errorf("failed to check directory %s: %w", dir, err)
		}

		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
	}

	return nil
}

// checkBinaryPresence verifies the binary exists
func (v *Validator) checkBinaryPresence(installPath string) error {
	binaryName := "tapio"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	binaryPath := filepath.Join(installPath, "bin", binaryName)

	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary not found at %s: %w", binaryPath, err)
	}

	if info.IsDir() {
		return fmt.Errorf("expected file but found directory at %s", binaryPath)
	}

	return nil
}

// checkPermissions verifies file and directory permissions
func (v *Validator) checkPathPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Check read permissions
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("no read permission: %w", err)
	}
	file.Close()

	// Check write permissions for directories
	if info.IsDir() {
		testFile := filepath.Join(path, ".permission_test")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			return fmt.Errorf("no write permission: %w", err)
		}
		os.Remove(testFile)
	}

	return nil
}

// checkDependencies verifies system dependencies
func (v *Validator) checkDependencies() error {
	// Platform-specific dependency checks
	switch runtime.GOOS {
	case "linux":
		return v.checkLinuxDependencies()
	case "darwin":
		return v.checkDarwinDependencies()
	case "windows":
		return v.checkWindowsDependencies()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// checkLinuxDependencies checks Linux-specific dependencies
func (v *Validator) checkLinuxDependencies() error {
	// Check for required libraries
	requiredLibs := []string{
		"libc.so.6",
		"libpthread.so.0",
	}

	for _, lib := range requiredLibs {
		cmd := exec.Command("ldconfig", "-p")
		output, err := cmd.Output()
		if err == nil {
			// Check if library is in ldconfig output
			if !containsLibrary(string(output), lib) {
				return fmt.Errorf("required library %s not found", lib)
			}
		}
	}

	return nil
}

// checkDarwinDependencies checks macOS-specific dependencies
func (v *Validator) checkDarwinDependencies() error {
	// macOS typically has all required dependencies
	return nil
}

// checkWindowsDependencies checks Windows-specific dependencies
func (v *Validator) checkWindowsDependencies() error {
	// Check for Visual C++ Redistributables
	// This is a simplified check - real implementation would be more thorough
	return nil
}

// containsLibrary checks if a library is present in ldconfig output
func containsLibrary(output, library string) bool {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, library) {
			return true
		}
	}
	return false
}

// ValidationError represents a validation error with details
type ValidationError struct {
	Component string
	Check     string
	Message   string
	Details   map[string]interface{}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %s: %s - %s", e.Component, e.Check, e.Message)
}

// ValidationReport contains validation results
type ValidationReport struct {
	Passed     bool
	Timestamp  time.Time
	Duration   time.Duration
	Checks     []CheckResult
	SystemInfo map[string]interface{}
}

// CheckResult represents a single validation check result
type CheckResult struct {
	Name     string
	Passed   bool
	Duration time.Duration
	Error    error
	Details  map[string]interface{}
}

// RunFullValidation performs comprehensive validation
func RunFullValidation(ctx context.Context, installPath string) (*ValidationReport, error) {
	startTime := time.Now()
	report := &ValidationReport{
		Timestamp: startTime,
		Checks:    []CheckResult{},
		SystemInfo: map[string]interface{}{
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
			"version": runtime.Version(),
		},
	}

	validator := NewValidator()

	// Define all validation checks
	checks := []struct {
		name string
		fn   func(context.Context) error
	}{
		{
			name: "Installation Structure",
			fn: func(ctx context.Context) error {
				return validator.ValidateInstallation(ctx, installPath)
			},
		},
		{
			name: "Binary Integrity",
			fn: func(ctx context.Context) error {
				binaryPath := filepath.Join(installPath, "bin", "tapio")
				if runtime.GOOS == "windows" {
					binaryPath += ".exe"
				}
				return validator.ValidateBinary(ctx, binaryPath, "")
			},
		},
		{
			name: "Permissions",
			fn: func(ctx context.Context) error {
				paths := []string{
					installPath,
					filepath.Join(installPath, "config"),
					filepath.Join(installPath, "data"),
				}
				return validator.ValidatePermissions(ctx, paths)
			},
		},
		{
			name: "Service Health",
			fn: func(ctx context.Context) error {
				checker := NewHealthChecker()
				return checker.CheckService(ctx, "tapio")
			},
		},
		{
			name: "Network Connectivity",
			fn: func(ctx context.Context) error {
				endpoints := []string{
					"http://localhost:8080/health",
					"http://localhost:8080/metrics",
				}
				return validator.ValidateConnectivity(ctx, endpoints)
			},
		},
	}

	// Run all checks
	allPassed := true
	for _, check := range checks {
		checkStart := time.Now()

		// Run check with timeout
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := check.fn(checkCtx)
		cancel()

		result := CheckResult{
			Name:     check.name,
			Passed:   err == nil,
			Duration: time.Since(checkStart),
			Error:    err,
			Details:  make(map[string]interface{}),
		}

		if !result.Passed {
			allPassed = false
		}

		report.Checks = append(report.Checks, result)
	}

	report.Passed = allPassed
	report.Duration = time.Since(startTime)

	return report, nil
}
