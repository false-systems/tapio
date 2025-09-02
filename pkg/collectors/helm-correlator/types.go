package helmcorrelator

import (
	"encoding/json"
	"time"
)

// HelmOperation represents a complete Helm operation from start to finish
type HelmOperation struct {
	// Identity
	ID  string
	PID uint32
	UID uint32
	GID uint32

	// Command details
	Command     string // Full command line
	Binary      string // "helm" or "kubectl"
	Action      string // "install", "upgrade", "rollback", etc.
	ReleaseName string
	Namespace   string
	ChartPath   string
	Arguments   map[string]string // Parsed flags

	// Timing
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
	ExitCode  int32
	Signal    int32 // If killed by signal

	// File tracking
	FilesRead   []FileAccess
	ValuesFiles []string

	// API tracking
	APICalls []APICall

	// K8s correlation
	ReleaseVersion int    // Version number from secret
	FromStatus     string // Previous release status
	ToStatus       string // New release status

	// Failure analysis
	Failed      bool
	FailureType string
	RootCause   *RootCause
}

// FileAccess represents a file read by Helm
type FileAccess struct {
	Timestamp time.Time
	Path      string
	Size      uint32
	FileType  string // "values", "template", "chart", "other"
}

// APICall represents a Kubernetes API call made by Helm
type APICall struct {
	Timestamp  time.Time
	Method     string // GET, POST, PUT, DELETE, PATCH
	Path       string // /api/v1/namespaces/default/secrets
	StatusCode int32
	Duration   time.Duration
	Error      string
}

// HelmValues represents Helm chart values as raw JSON
// to avoid map[string]interface{} violations
type HelmValues struct {
	Raw json.RawMessage `json:"-"`
}

// HelmRelease represents the decoded Helm release from K8s secret
type HelmRelease struct {
	Name       string
	Namespace  string
	Version    int
	Status     string // deployed, failed, pending-upgrade, pending-install, pending-rollback
	Chart      string
	AppVersion string
	Values     *HelmValues // The values used
	Manifest   string      // Rendered YAML
	Notes      string
	Info       *ReleaseInfo
	Hooks      []HelmHook
	CreatedAt  time.Time
}

// ReleaseInfo contains metadata about the release
type ReleaseInfo struct {
	FirstDeployed time.Time
	LastDeployed  time.Time
	Deleted       time.Time
	Description   string // "Install complete" or error message
	Status        string
	Notes         string // Release notes
}

// HelmHook represents a Helm hook (pre/post install/upgrade/rollback/delete)
type HelmHook struct {
	Name     string
	Kind     string // Job, Pod, etc.
	Phase    string // pre-install, post-upgrade, etc.
	Weight   int
	Manifest string
	Events   string // Hook events (e.g., "pre-upgrade,post-upgrade")
}

// RootCause is the final diagnosis of what went wrong
type RootCause struct {
	// Identity
	OperationID string
	ReleaseName string
	Namespace   string

	// What happened
	Pattern     string  // Which pattern matched
	Confidence  float32 // How sure are we (0-1)
	Operation   string  // install, upgrade, rollback
	FromVersion int
	ToVersion   int
	Status      string // Current release status

	// The story
	Summary    string   // One-liner: "Pre-upgrade hook failed due to ECR rate limit"
	Details    string   // Full explanation
	Evidence   []string // Specific evidence points with timestamps
	EventChain []string // Sequence of events

	// Actionable info
	Impact     string // "Backend worker stuck on v1 while API on v2"
	Resolution string // Step-by-step fix

	// Technical details
	HookFailure *HookFailureDetails
	PodFailure  *PodFailureDetails
	APIError    *APIErrorDetails

	// Timeline
	FailureTime time.Time
	Duration    time.Duration
}

// HookFailureDetails provides details about hook failures
type HookFailureDetails struct {
	HookName string
	JobName  string
	PodName  string
	Phase    string // pre-upgrade, post-install, etc.
	ExitCode int32
	Logs     string // Last lines of logs
	Error    string
}

// PodFailureDetails provides details about pod failures
type PodFailureDetails struct {
	PodName       string
	Phase         string // Pending, Running, Failed
	Reason        string // ImagePullBackOff, CrashLoopBackOff, etc.
	Message       string
	ContainerName string
	ExitCode      int32
	RestartCount  int32
}

// APIErrorDetails provides details about API errors
type APIErrorDetails struct {
	Method     string
	Path       string
	StatusCode int32
	Error      string
	Reason     string // From K8s API
}

// FailurePattern represents a known Helm failure pattern
type FailurePattern struct {
	Name        string
	Description string
	Detector    func(*HelmOperation, *CorrelationContext) *RootCause
}

// CorrelationContext holds data for correlation
type CorrelationContext struct {
	Operation       *HelmOperation
	Release         *HelmRelease
	PreviousRelease *HelmRelease
	K8sEvents       []K8sEvent
	Jobs            []JobStatus
	Pods            []PodStatus
	TimeWindow      TimeWindow
}

// K8sEvent represents a Kubernetes event
type K8sEvent struct {
	Timestamp time.Time
	Type      string // Normal, Warning
	Reason    string
	Object    string // pod/xxx, job/yyy
	Message   string
	FirstSeen time.Time
	LastSeen  time.Time
	Count     int32
}

// JobStatus represents the status of a K8s Job (usually hooks)
type JobStatus struct {
	Name         string
	Namespace    string
	CreatedAt    time.Time
	CompletedAt  time.Time
	Failed       bool
	BackoffLimit int32
	Completions  int32
	Succeeded    int32
	Pods         []string
}

// PodStatus represents the status of a K8s Pod
type PodStatus struct {
	Name              string
	Namespace         string
	Phase             string
	Reason            string
	Message           string
	ContainerStatuses []ContainerStatus
	CreatedAt         time.Time
}

// ContainerStatus represents container status within a pod
type ContainerStatus struct {
	Name         string
	Ready        bool
	RestartCount int32
	State        string // waiting, running, terminated
	Reason       string // ImagePullBackOff, CrashLoopBackOff, etc.
	Message      string
	ExitCode     int32
}

// TimeWindow represents a time range for correlation
type TimeWindow struct {
	Start time.Time
	End   time.Time
}

// FailureInfo contains detailed failure information from a release
type FailureInfo struct {
	HasError     bool
	Status       string
	Description  string
	Notes        string
	HookFailures []string
	Timestamp    time.Time
}

// Config for the helm-correlator collector
type Config struct {
	Name       string
	BufferSize int

	// Feature flags
	EnableEBPF        bool
	EnableK8sWatching bool

	// Correlation settings
	CorrelationWindow   time.Duration // Time window for correlation (default: 5m)
	StuckReleaseTimeout time.Duration // When to consider release stuck (default: 10m)
	HookTimeout         time.Duration // Max time for hooks (default: 5m)

	// Kubernetes settings
	Namespaces []string // Empty = all namespaces
	KubeConfig string   // Path to kubeconfig (empty = in-cluster)

	// eBPF settings
	TrackKubectl bool // Also track kubectl commands
	TrackFiles   bool // Track file access
	TrackAPI     bool // Track API calls
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:                "helm-correlator",
		BufferSize:          1000,
		EnableEBPF:          false, // Disabled by default for testing
		EnableK8sWatching:   false, // Disabled by default for testing
		CorrelationWindow:   5 * time.Minute,
		StuckReleaseTimeout: 10 * time.Minute,
		HookTimeout:         5 * time.Minute,
		TrackKubectl:        true,
		TrackFiles:          true,
		TrackAPI:            true,
	}
}
