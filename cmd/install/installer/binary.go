package installer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	
	"tapio/cmd/install/platform"
	"tapio/cmd/install/validation"
)

// BinaryInstaller installs Tapio as a binary
type BinaryInstaller struct {
	platform      platform.Info
	client        *http.Client
	commandHistory CommandHistory
	pipeline      Pipeline[*binaryInstallData]
}

// binaryInstallData holds data passed through the pipeline
type binaryInstallData struct {
	Options      InstallOptions
	DownloadPath string
	ExtractPath  string
	BinaryPath   string
	BackupPath   string
	Checksum     string
}

// NewBinaryInstaller creates a new binary installer
func NewBinaryInstaller(p platform.Info) Installer {
	return &BinaryInstaller{
		platform: p,
		client: &http.Client{
			Timeout: 30 * time.Minute,
		},
		commandHistory: NewCommandHistory(),
	}
}

// Name returns the installer name
func (b *BinaryInstaller) Name() string {
	return "binary"
}

// Install performs the installation
func (b *BinaryInstaller) Install(ctx context.Context, opts InstallOptions) error {
	// Build installation pipeline
	b.pipeline = NewPipeline[*binaryInstallData]().
		WithRollback(true).
		WithMetrics(NewMetricsCollector())
	
	// Add installation steps
	b.pipeline.
		AddStep(&downloadStep{installer: b}).
		AddStep(&verifyChecksumStep{}).
		AddStep(&extractStep{}).
		AddStep(&installBinaryStep{platform: b.platform}).
		AddStep(&createConfigStep{}).
		AddStep(&setupServiceStep{platform: b.platform}).
		AddStep(&validateStep{})
	
	// Execute pipeline
	data := &binaryInstallData{
		Options: opts,
	}
	
	if opts.DryRun {
		return b.dryRun(ctx, data)
	}
	
	_, err := b.pipeline.Execute(ctx, data)
	return err
}

// Uninstall removes the installation
func (b *BinaryInstaller) Uninstall(ctx context.Context, opts UninstallOptions) error {
	if opts.DryRun {
		return b.dryRunUninstall(ctx, opts)
	}
	
	// Stop and remove service
	if err := b.removeService(ctx); err != nil {
		return fmt.Errorf("failed to remove service: %w", err)
	}
	
	// Remove binary
	binaryPath := b.getBinaryPath()
	if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove binary: %w", err)
	}
	
	// Remove config if requested
	if opts.RemoveConfig {
		configPath := b.getConfigPath()
		if err := os.RemoveAll(configPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove config: %w", err)
		}
	}
	
	// Remove data if requested
	if opts.RemoveData {
		dataPath := b.getDataPath()
		if err := os.RemoveAll(dataPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove data: %w", err)
		}
	}
	
	return nil
}

// Upgrade performs an upgrade
func (b *BinaryInstaller) Upgrade(ctx context.Context, opts UpgradeOptions) error {
	if opts.DryRun {
		return b.dryRunUpgrade(ctx, opts)
	}
	
	// Backup current installation
	if !opts.SkipBackup {
		if err := b.backup(ctx, opts.BackupPath); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}
	
	// Stop service
	if err := b.stopService(ctx); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	
	// Perform installation with new version
	installOpts := InstallOptions{
		Version:     opts.ToVersion,
		InstallPath: b.getInstallPath(),
		ConfigPath:  b.getConfigPath(),
		DataPath:    b.getDataPath(),
		Force:       opts.Force,
		DryRun:      false,
	}
	
	if err := b.Install(ctx, installOpts); err != nil {
		// Restore from backup
		if !opts.SkipBackup {
			b.restore(ctx, opts.BackupPath)
		}
		return err
	}
	
	// Start service
	if err := b.startService(ctx); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	
	return nil
}

// Validate checks the installation
func (b *BinaryInstaller) Validate(ctx context.Context) error {
	validator := validation.NewValidator()
	
	// Validate binary exists and is executable
	binaryPath := b.getBinaryPath()
	if err := validator.ValidateBinary(ctx, binaryPath, ""); err != nil {
		return err
	}
	
	// Validate permissions
	paths := []string{
		b.getInstallPath(),
		b.getConfigPath(),
		b.getDataPath(),
	}
	
	if err := validator.ValidatePermissions(ctx, paths); err != nil {
		return err
	}
	
	// Validate service
	if err := b.validateService(ctx); err != nil {
		return err
	}
	
	return nil
}

// GetCapabilities returns supported features
func (b *BinaryInstaller) GetCapabilities() Capabilities {
	return Capabilities{
		SupportsUpgrade:    true,
		SupportsRollback:   true,
		SupportsValidation: true,
		RequiresRoot:       b.platform.OS != "windows",
		PlatformSpecific:   true,
	}
}

// Pipeline steps

// downloadStep downloads the binary
type downloadStep struct {
	installer *BinaryInstaller
}

func (s *downloadStep) Name() string { return "download" }

func (s *downloadStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	// Build download URL
	url := s.buildDownloadURL(data.Options.Version)
	data.Options.DownloadOpts.URL = url
	
	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "tapio-*.tar.gz")
	if err != nil {
		return data, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()
	
	data.DownloadPath = tmpFile.Name()
	
	// Download with progress
	downloader := NewDownloader(s.installer.client)
	
	if data.Options.Progress != nil {
		data.Options.Progress.Start("Downloading", -1)
		err = downloader.DownloadWithProgress(ctx, data.Options.DownloadOpts, tmpFile,
			func(current, total int64) {
				data.Options.Progress.Update(current)
			})
		data.Options.Progress.Complete("Downloading")
	} else {
		err = downloader.Download(ctx, data.Options.DownloadOpts, tmpFile)
	}
	
	if err != nil {
		return data, fmt.Errorf("download failed: %w", err)
	}
	
	// Get checksum
	data.Checksum = data.Options.DownloadOpts.Checksum
	if data.Checksum == "" {
		// Download checksum file
		checksumURL := url + ".sha256"
		data.Checksum, err = s.downloadChecksum(ctx, checksumURL)
		if err != nil {
			return data, fmt.Errorf("failed to download checksum: %w", err)
		}
	}
	
	return data, nil
}

func (s *downloadStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	if data.DownloadPath != "" {
		os.Remove(data.DownloadPath)
	}
	return nil
}

func (s *downloadStep) Validate(ctx context.Context, data *binaryInstallData) error {
	if _, err := os.Stat(data.DownloadPath); err != nil {
		return fmt.Errorf("download file not found: %w", err)
	}
	return nil
}

func (s *downloadStep) buildDownloadURL(version string) string {
	// Example URL structure
	baseURL := "https://github.com/tapio/tapio/releases/download"
	if version == "latest" {
		version = "v1.0.0" // Would fetch latest from API
	}
	
	filename := fmt.Sprintf("tapio_%s_%s_%s.tar.gz",
		strings.TrimPrefix(version, "v"),
		s.installer.platform.OS,
		s.installer.platform.Arch)
	
	return fmt.Sprintf("%s/%s/%s", baseURL, version, filename)
}

func (s *downloadStep) downloadChecksum(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	
	resp, err := s.installer.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download failed: %s", resp.Status)
	}
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	
	// Parse checksum (format: "checksum  filename")
	parts := strings.Fields(string(data))
	if len(parts) > 0 {
		return parts[0], nil
	}
	
	return "", fmt.Errorf("invalid checksum format")
}

// verifyChecksumStep verifies the download checksum
type verifyChecksumStep struct{}

func (s *verifyChecksumStep) Name() string { return "verify-checksum" }

func (s *verifyChecksumStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	if data.Options.Progress != nil {
		data.Options.Progress.Start("Verifying checksum", 0)
		defer data.Options.Progress.Complete("Verifying checksum")
	}
	
	file, err := os.Open(data.DownloadPath)
	if err != nil {
		return data, err
	}
	defer file.Close()
	
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return data, err
	}
	
	calculated := hex.EncodeToString(hash.Sum(nil))
	if calculated != data.Checksum {
		return data, fmt.Errorf("checksum mismatch: expected %s, got %s", data.Checksum, calculated)
	}
	
	return data, nil
}

func (s *verifyChecksumStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	return nil
}

func (s *verifyChecksumStep) Validate(ctx context.Context, data *binaryInstallData) error {
	return nil
}

// extractStep extracts the archive
type extractStep struct{}

func (s *extractStep) Name() string { return "extract" }

func (s *extractStep) Execute(ctx context.Context, data *binaryInstallData) (*binaryInstallData, error) {
	if data.Options.Progress != nil {
		data.Options.Progress.Start("Extracting", 0)
		defer data.Options.Progress.Complete("Extracting")
	}
	
	// Create temp directory for extraction
	tmpDir, err := os.MkdirTemp("", "tapio-extract-*")
	if err != nil {
		return data, err
	}
	
	data.ExtractPath = tmpDir
	
	// Open archive
	file, err := os.Open(data.DownloadPath)
	if err != nil {
		return data, err
	}
	defer file.Close()
	
	// Extract tar.gz
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return data, err
	}
	defer gzReader.Close()
	
	tarReader := tar.NewReader(gzReader)
	
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return data, err
		}
		
		target := filepath.Join(tmpDir, header.Name)
		
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return data, err
			}
		case tar.TypeReg:
			if err := s.extractFile(tarReader, target, os.FileMode(header.Mode)); err != nil {
				return data, err
			}
		}
	}
	
	// Find binary
	binaryName := "tapio"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	
	data.BinaryPath = filepath.Join(tmpDir, binaryName)
	if _, err := os.Stat(data.BinaryPath); err != nil {
		return data, fmt.Errorf("binary not found in archive: %w", err)
	}
	
	return data, nil
}

func (s *extractStep) extractFile(src io.Reader, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	
	file, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	
	_, err = io.Copy(file, src)
	return err
}

func (s *extractStep) Rollback(ctx context.Context, data *binaryInstallData) error {
	if data.ExtractPath != "" {
		os.RemoveAll(data.ExtractPath)
	}
	return nil
}

func (s *extractStep) Validate(ctx context.Context, data *binaryInstallData) error {
	if _, err := os.Stat(data.BinaryPath); err != nil {
		return fmt.Errorf("extracted binary not found: %w", err)
	}
	return nil
}

// Helper methods
func (b *BinaryInstaller) getBinaryPath() string {
	return filepath.Join(b.getInstallPath(), "tapio")
}

func (b *BinaryInstaller) getInstallPath() string {
	if b.platform.OS == "windows" {
		return filepath.Join(os.Getenv("ProgramFiles"), "Tapio")
	}
	return "/opt/tapio"
}

func (b *BinaryInstaller) getConfigPath() string {
	if b.platform.OS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "Tapio", "config")
	}
	return "/etc/tapio"
}

func (b *BinaryInstaller) getDataPath() string {
	if b.platform.OS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "Tapio", "data")
	}
	return "/var/lib/tapio"
}

// Service management helpers
func (b *BinaryInstaller) removeService(ctx context.Context) error {
	// Implementation would call platform-specific service removal
	return nil
}

func (b *BinaryInstaller) stopService(ctx context.Context) error {
	// Implementation would call platform-specific service stop
	return nil
}

func (b *BinaryInstaller) startService(ctx context.Context) error {
	// Implementation would call platform-specific service start
	return nil
}

func (b *BinaryInstaller) validateService(ctx context.Context) error {
	// Implementation would validate service is running
	return nil
}

func (b *BinaryInstaller) backup(ctx context.Context, backupPath string) error {
	// Implementation would create backup
	return nil
}

func (b *BinaryInstaller) restore(ctx context.Context, backupPath string) error {
	// Implementation would restore from backup
	return nil
}

// Dry run methods
func (b *BinaryInstaller) dryRun(ctx context.Context, data *binaryInstallData) error {
	fmt.Println("[DRY RUN] Would perform installation:")
	fmt.Printf("  Version: %s\n", data.Options.Version)
	fmt.Printf("  Install Path: %s\n", data.Options.InstallPath)
	fmt.Printf("  Config Path: %s\n", data.Options.ConfigPath)
	fmt.Printf("  Data Path: %s\n", data.Options.DataPath)
	return nil
}

func (b *BinaryInstaller) dryRunUninstall(ctx context.Context, opts UninstallOptions) error {
	fmt.Println("[DRY RUN] Would perform uninstallation:")
	fmt.Printf("  Remove Config: %v\n", opts.RemoveConfig)
	fmt.Printf("  Remove Data: %v\n", opts.RemoveData)
	return nil
}

func (b *BinaryInstaller) dryRunUpgrade(ctx context.Context, opts UpgradeOptions) error {
	fmt.Println("[DRY RUN] Would perform upgrade:")
	fmt.Printf("  From Version: %s\n", opts.FromVersion)
	fmt.Printf("  To Version: %s\n", opts.ToVersion)
	fmt.Printf("  Backup Path: %s\n", opts.BackupPath)
	return nil
}