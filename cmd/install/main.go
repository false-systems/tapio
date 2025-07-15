package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yairfalse/tapio/cmd/install/factory"
	"github.com/yairfalse/tapio/cmd/install/installer"
	"github.com/yairfalse/tapio/cmd/install/platform"
	"github.com/yairfalse/tapio/cmd/install/progress"
	"github.com/yairfalse/tapio/cmd/install/validation"
)

var (
	// Version information
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// Config holds installation configuration
type Config struct {
	Strategy       installer.InstallStrategy
	Version        string
	InstallPath    string
	ConfigPath     string
	DataPath       string
	Force          bool
	DryRun         bool
	NoProgress     bool
	SkipValidation bool
	LogLevel       string
	Timeout        time.Duration
}

func main() {
	if err := Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func Execute() error {
	rootCmd := &cobra.Command{
		Use:     "tapio-install",
		Short:   "Tapio installation manager",
		Long:    `A sophisticated installer for Tapio with support for multiple deployment strategies`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
	}

	var cfg Config

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 30*time.Minute, "Installation timeout")
	rootCmd.PersistentFlags().BoolVar(&cfg.NoProgress, "no-progress", false, "Disable progress reporting")

	// Install command
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install Tapio",
		Long:  `Install Tapio using the specified strategy (binary, docker, kubernetes)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd.Context(), cfg)
		},
	}

	installCmd.Flags().StringVarP(&cfg.Strategy, "strategy", "s", "binary", "Installation strategy (binary, docker, kubernetes)")
	installCmd.Flags().StringVarP(&cfg.Version, "version", "v", "latest", "Version to install")
	installCmd.Flags().StringVar(&cfg.InstallPath, "install-path", defaultInstallPath(), "Installation path")
	installCmd.Flags().StringVar(&cfg.ConfigPath, "config-path", defaultConfigPath(), "Configuration path")
	installCmd.Flags().StringVar(&cfg.DataPath, "data-path", defaultDataPath(), "Data path")
	installCmd.Flags().BoolVarP(&cfg.Force, "force", "f", false, "Force installation")
	installCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Perform a dry run")
	installCmd.Flags().BoolVar(&cfg.SkipValidation, "skip-validation", false, "Skip post-install validation")

	// Uninstall command
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Tapio",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(cmd.Context(), cfg)
		},
	}

	uninstallCmd.Flags().StringVarP(&cfg.Strategy, "strategy", "s", "binary", "Installation strategy")
	uninstallCmd.Flags().BoolVar(&cfg.Force, "force", false, "Force uninstallation")
	uninstallCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Perform a dry run")

	// Upgrade command
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Tapio",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd.Context(), cfg)
		},
	}

	upgradeCmd.Flags().StringVarP(&cfg.Strategy, "strategy", "s", "binary", "Installation strategy")
	upgradeCmd.Flags().StringVarP(&cfg.Version, "version", "v", "latest", "Version to upgrade to")
	upgradeCmd.Flags().BoolVar(&cfg.Force, "force", false, "Force upgrade")
	upgradeCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Perform a dry run")

	// Validate command
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate Tapio installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.Context(), cfg)
		},
	}

	validateCmd.Flags().StringVarP(&cfg.Strategy, "strategy", "s", "binary", "Installation strategy")

	// Status command
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show installation status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), cfg)
		},
	}

	// Add commands
	rootCmd.AddCommand(installCmd, uninstallCmd, upgradeCmd, validateCmd, statusCmd)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, cancelling...")
		cancel()
	}()

	// Execute with context
	return rootCmd.ExecuteContext(ctx)
}

func runInstall(ctx context.Context, cfg Config) error {
	// Create progress reporter
	var progressReporter installer.ProgressReporter
	if !cfg.NoProgress {
		progressReporter = progress.NewSimpleTerminalReporter()
	} else {
		progressReporter = progress.NewSilentReporter()
	}

	progressReporter.Start("Initializing", 0)

	// Detect platform
	detector := platform.NewDetector()
	platformInfo := detector.Detect()

	progressReporter.Log("info", "Detected platform",
		"os", platformInfo.OS,
		"arch", platformInfo.Arch,
		"distro", platformInfo.Distribution)

	// Create installer factory
	f := factory.NewFactory(platformInfo)

	// Create installer
	inst, err := f.Create(cfg.Strategy)
	if err != nil {
		progressReporter.Error("Initialization", err)
		return fmt.Errorf("failed to create installer: %w", err)
	}

	progressReporter.Complete("Initializing")

	// Build install options
	opts := installer.InstallOptions{
		Version:        cfg.Version,
		InstallPath:    cfg.InstallPath,
		ConfigPath:     cfg.ConfigPath,
		DataPath:       cfg.DataPath,
		Force:          cfg.Force,
		SkipValidation: cfg.SkipValidation,
		DryRun:         cfg.DryRun,
		Progress:       progressReporter,
		DownloadOpts: installer.DownloadOptions{
			MaxRetries:     3,
			RetryDelay:     5 * time.Second,
			Timeout:        5 * time.Minute,
			CircuitBreaker: installer.NewCircuitBreaker(5, 1*time.Minute),
		},
	}

	// Apply timeout
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	// Run installation
	if err := inst.Install(ctx, opts); err != nil {
		progressReporter.Error("Installation", err)
		return fmt.Errorf("installation failed: %w", err)
	}

	progressReporter.Log("info", "Installation completed successfully")
	return nil
}

func runUninstall(ctx context.Context, cfg Config) error {
	// Create progress reporter
	var progressReporter installer.ProgressReporter
	if !cfg.NoProgress {
		progressReporter = progress.NewSimpleTerminalReporter()
	} else {
		progressReporter = progress.NewSilentReporter()
	}

	// Detect platform
	detector := platform.NewDetector()
	platformInfo := detector.Detect()

	// Create installer factory
	f := factory.NewFactory(platformInfo)

	// Create installer
	inst, err := f.Create(cfg.Strategy)
	if err != nil {
		return fmt.Errorf("failed to create installer: %w", err)
	}

	// Build uninstall options
	opts := installer.UninstallOptions{
		RemoveConfig: true,
		RemoveData:   false,
		Force:        cfg.Force,
		DryRun:       cfg.DryRun,
	}

	// Run uninstallation
	if err := inst.Uninstall(ctx, opts); err != nil {
		return fmt.Errorf("uninstallation failed: %w", err)
	}

	fmt.Println("Uninstallation completed successfully")
	return nil
}

func runUpgrade(ctx context.Context, cfg Config) error {
	// Create progress reporter
	var progressReporter installer.ProgressReporter
	if !cfg.NoProgress {
		progressReporter = progress.NewSimpleTerminalReporter()
	} else {
		progressReporter = progress.NewSilentReporter()
	}

	// Detect platform
	detector := platform.NewDetector()
	platformInfo := detector.Detect()

	// Create installer factory
	f := factory.NewFactory(platformInfo)

	// Create installer
	inst, err := f.Create(cfg.Strategy)
	if err != nil {
		return fmt.Errorf("failed to create installer: %w", err)
	}

	// Check capabilities
	caps := inst.GetCapabilities()
	if !caps.SupportsUpgrade {
		return fmt.Errorf("installer %s does not support upgrades", inst.Name())
	}

	// Build upgrade options
	opts := installer.UpgradeOptions{
		ToVersion:  cfg.Version,
		BackupPath: filepath.Join(cfg.DataPath, "backups"),
		SkipBackup: false,
		Force:      cfg.Force,
		DryRun:     cfg.DryRun,
	}

	// Run upgrade
	if err := inst.Upgrade(ctx, opts); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	fmt.Println("Upgrade completed successfully")
	return nil
}

func runValidate(ctx context.Context, cfg Config) error {
	// Detect platform
	detector := platform.NewDetector()
	platformInfo := detector.Detect()

	// Create installer factory
	f := factory.NewFactory(platformInfo)

	// Create installer
	inst, err := f.Create(cfg.Strategy)
	if err != nil {
		return fmt.Errorf("failed to create installer: %w", err)
	}

	// Run validation
	if err := inst.Validate(ctx); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Println("Validation passed")
	return nil
}

func runStatus(ctx context.Context, cfg Config) error {
	// Implementation for status command
	fmt.Println("Status command not yet implemented")
	return nil
}

// Default path helpers
func defaultInstallPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramFiles"), "Tapio")
	}
	return "/opt/tapio"
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "Tapio", "config")
	}
	return "/etc/tapio"
}

func defaultDataPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "Tapio", "data")
	}
	return "/var/lib/tapio"
}
