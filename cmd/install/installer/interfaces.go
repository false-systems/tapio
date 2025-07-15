package installer

import (
	"context"
	"io"
	"time"
)

// InstallStrategy represents different installation methods
type InstallStrategy string

const (
	StrategyBinary     InstallStrategy = "binary"
	StrategyDocker     InstallStrategy = "docker"
	StrategyKubernetes InstallStrategy = "kubernetes"
)

// Step represents a single installation step
type Step[T any] interface {
	// Name returns the step name for logging
	Name() string

	// Execute performs the step operation
	Execute(ctx context.Context, data T) (T, error)

	// Rollback reverses the step operation
	Rollback(ctx context.Context, data T) error

	// Validate checks if the step completed successfully
	Validate(ctx context.Context, data T) error
}

// Pipeline orchestrates a series of installation steps
type Pipeline[T any] interface {
	// AddStep adds a step to the pipeline
	AddStep(step Step[T]) Pipeline[T]

	// Execute runs all steps in order
	Execute(ctx context.Context, initial T) (T, error)

	// WithRollback enables automatic rollback on failure
	WithRollback(enabled bool) Pipeline[T]

	// WithMetrics enables metrics collection
	WithMetrics(collector MetricsCollector) Pipeline[T]
}

// Installer is the main interface for installation strategies
type Installer interface {
	// Name returns the installer name
	Name() string

	// Install performs the installation
	Install(ctx context.Context, opts InstallOptions) error

	// Uninstall removes the installation
	Uninstall(ctx context.Context, opts UninstallOptions) error

	// Upgrade performs an upgrade
	Upgrade(ctx context.Context, opts UpgradeOptions) error

	// Validate checks the installation
	Validate(ctx context.Context) error

	// GetCapabilities returns supported features
	GetCapabilities() Capabilities
}

// InstallOptions contains installation configuration
type InstallOptions struct {
	Version        string
	InstallPath    string
	ConfigPath     string
	DataPath       string
	Force          bool
	SkipValidation bool
	DryRun         bool
	Progress       ProgressReporter
	DownloadOpts   DownloadOptions
}

// UninstallOptions contains uninstallation configuration
type UninstallOptions struct {
	RemoveConfig bool
	RemoveData   bool
	Force        bool
	DryRun       bool
}

// UpgradeOptions contains upgrade configuration
type UpgradeOptions struct {
	FromVersion string
	ToVersion   string
	BackupPath  string
	SkipBackup  bool
	Force       bool
	DryRun      bool
}

// DownloadOptions configures download behavior
type DownloadOptions struct {
	URL            string
	Checksum       string
	ChecksumType   string
	MaxRetries     int
	RetryDelay     time.Duration
	Timeout        time.Duration
	ProxyURL       string
	CircuitBreaker CircuitBreaker
}

// Capabilities describes what an installer supports
type Capabilities struct {
	SupportsUpgrade    bool
	SupportsRollback   bool
	SupportsValidation bool
	RequiresRoot       bool
	PlatformSpecific   bool
}

// ProgressReporter reports installation progress
type ProgressReporter interface {
	// Start begins a new phase
	Start(phase string, total int64)

	// Update reports progress
	Update(current int64)

	// Complete marks phase as complete
	Complete(phase string)

	// Error reports an error
	Error(phase string, err error)

	// Log writes a log message
	Log(level string, message string, fields ...interface{})
}

// MetricsCollector collects installation metrics
type MetricsCollector interface {
	// RecordDuration records step duration
	RecordDuration(step string, duration time.Duration)

	// RecordError records an error
	RecordError(step string, err error)

	// RecordSuccess records a successful step
	RecordSuccess(step string)

	// GetReport returns metrics report
	GetReport() MetricsReport
}

// MetricsReport contains collected metrics
type MetricsReport struct {
	TotalDuration   time.Duration
	StepDurations   map[string]time.Duration
	Errors          map[string][]error
	SuccessfulSteps []string
	FailedSteps     []string
}

// CircuitBreaker provides circuit breaker functionality
type CircuitBreaker interface {
	// Execute runs the function with circuit breaker protection
	Execute(fn func() error) error

	// IsOpen returns if the circuit is open
	IsOpen() bool

	// Reset resets the circuit breaker
	Reset()
}


// Downloader handles file downloads with progress
type Downloader interface {
	// Download downloads a file
	Download(ctx context.Context, opts DownloadOptions, dst io.Writer) error

	// DownloadWithProgress downloads with progress reporting
	DownloadWithProgress(ctx context.Context, opts DownloadOptions, dst io.Writer, progress func(current, total int64)) error
}


// Command represents a reversible operation
type Command interface {
	// Execute performs the command
	Execute(ctx context.Context) error

	// Undo reverses the command
	Undo(ctx context.Context) error

	// CanUndo returns if the command can be undone
	CanUndo() bool
}

// CommandHistory tracks executed commands for rollback
type CommandHistory interface {
	// Push adds a command to history
	Push(cmd Command)

	// Pop removes and returns the last command
	Pop() (Command, bool)

	// Clear removes all commands
	Clear()

	// Rollback undoes all commands in reverse order
	Rollback(ctx context.Context) error
}
