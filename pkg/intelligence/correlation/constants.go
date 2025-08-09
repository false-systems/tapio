package correlation

import "time"

// Timeouts and Intervals
const (
	// Processing timeouts
	DefaultProcessingTimeout = 5 * time.Second
	ConfigModificationWindow = 30 * time.Minute
	RestartCorrelationWindow = 10 * time.Minute
	PodStartupWindow         = 5 * time.Minute
	ServiceMetricsWindow     = 5 * time.Minute

	// Pattern and sequence timeouts
	DefaultPatternTimeout  = 2 * time.Minute
	LongPatternTimeout     = 5 * time.Minute
	ExtendedPatternTimeout = 10 * time.Minute
	MaxEventAge            = 24 * time.Hour
	DefaultSequenceWindow  = 5 * time.Second

	// Retry and delay constants
	DefaultRetryDelay    = 1 * time.Second
	MaxRetryAttempts     = 3
	ProcessingDelay      = 100 * time.Millisecond
	ConcurrencyTestDelay = 100 * time.Millisecond
)

// Buffer and Limit Constants
const (
	// Engine buffer sizes
	DefaultEventBufferSize  = 1000
	DefaultResultBufferSize = 1000
	TestEventBufferSize     = 100
	TestResultBufferSize    = 100

	// Query limits
	DefaultQueryLimit   = 100
	MaxQueryLimit       = 1000
	ServiceQueryLimit   = 100
	OwnershipQueryLimit = 100
	PodQueryLimit       = 100

	// Correlation tracking limits
	MaxActiveSequences     = 1000
	TestMaxActiveSequences = 2000
	MaxPatternsTracked     = 1000
	MaxTemporalItems       = 10000
	TestTemporalItems      = 100

	// Channel buffer sizes
	DefaultChannelBuffer   = 256
	TestChannelBuffer      = 100
	ConcurrencyChannelSize = 100

	// Performance thresholds
	MaxEventsPerCorrelation = 50
	HighLatencyThresholdMs  = 1000
	SlowProcessingThreshold = 100 * time.Millisecond
)

// Kubernetes Constants
const (
	// Namespace constants
	DefaultNamespace    = "default"
	KubeSystemNamespace = "kube-system"
	KubePublicNamespace = "kube-public"
	ProductionNamespace = "production"
	AllNamespaces       = ""

	// Resource Type Constants
	ResourceTypePod         = "Pod"
	ResourceTypeService     = "Service"
	ResourceTypeConfigMap   = "ConfigMap"
	ResourceTypeSecret      = "Secret"
	ResourceTypeDeployment  = "Deployment"
	ResourceTypeStatefulSet = "StatefulSet"
	ResourceTypeDaemonSet   = "DaemonSet"
	ResourceTypeNode        = "Node"
)

// Correlation Constants
const (
	// Confidence thresholds
	MinConfidenceThreshold = 0.5
	DefaultConfidence      = 0.75
	MaxConfidenceValue     = 1.0
	InitialConfidence      = 0.5

	// Test confidence values
	TestConfidence     = 0.8
	LowTestConfidence  = 0.6
	HighTestConfidence = 0.85

	// Degradation percentages for calculations
	FullDegradation      = 100 // 100% - complete failure
	PercentageMultiplier = 100 // For percentage calculations
)

// Sequence Pattern Constants
const (
	// Sequence configuration defaults
	DefaultMaxSequenceAge    = 15 * time.Minute
	DefaultMaxSequenceGap    = 3 * time.Minute
	DefaultMinSequenceLength = 3

	// Test sequence configuration
	TestMaxSequenceAge    = 5 * time.Minute
	TestMaxSequenceGap    = 1 * time.Minute
	TestMinSequenceLength = 3
	ShortTestTimeout      = 100 * time.Millisecond
	VeryShortTestTimeout  = 100 * time.Millisecond
)

// Event Type Patterns
var (
	// Pod lifecycle sequence conditions
	PodCreationConditions     = []string{ResourceTypePod, "created", "scheduled"}
	PodReadyConditions        = []string{ResourceTypePod, "ready", "running"}
	PodStartConditions        = []string{ResourceTypePod, "started"}
	PodFailConditions         = []string{ResourceTypePod, "error", "failed"}
	PodBackoffConditions      = []string{ResourceTypePod, "backoff"}
	PodRestartConditions      = []string{ResourceTypePod, "restarting"}
	PodTerminateConditions    = []string{ResourceTypePod, "terminating"}
	PodEvictedConditions      = []string{ResourceTypePod, "evicted"}
	PodResourceFailConditions = []string{ResourceTypePod, "failed", "insufficient resources"}

	// Service-related sequence conditions
	ServiceEndpointsConditions = []string{ResourceTypeService, "endpoints changed"}

	// Deployment-related conditions
	DeploymentConditions = []string{ResourceTypeDeployment}
	ServiceConditions    = []string{ResourceTypeService}
)

// Test Data Constants
const (
	// Test event identifiers
	TestEventID1    = "event-1"
	TestEventID2    = "event-2"
	TestEventID3    = "event-3"
	TestPodName     = "test-pod"
	TestServiceName = "test-service"
	TestDeployName  = "test-deployment"

	// Test metric values
	TestEventCount      = 100
	TestConnectionCount = "100"

	// Test timeouts
	TestProcessingTimeout = 30 * time.Second
	ShortTestDelay        = 10 * time.Second
	MediumTestDelay       = 20 * time.Second
)

// Message Templates and Format Strings
const (
	// Degradation message templates
	ServiceNotExistMsg     = "100% - service does not exist"
	NoPodsAvailableMsg     = "100% - no pods available"
	AllPodsFailedMsg       = "100% - all pods failed"
	PodNotExistMsg         = "100% - pod does not exist"
	PodCrashMsg            = "100% - pod crash"
	PodRestartMsg          = "100% - pod restart"
	PodStuckPendingMsg     = "100% - pod stuck pending"
	VolumeProvisionFailMsg = "100% - volume provisioning failed"

	// Format strings for dynamic messages
	ReducedCapacityFmt   = "%d%% - reduced capacity"
	NodeCoverageFmt      = "%d%% node coverage"
	CapacityFmt          = "%d%% capacity"
	DesiredReplicasFmt   = "%d%% of desired replicas"
	CapacityAvailableFmt = "%d%% capacity available"
	PatternConfidenceFmt = "Pattern confidence: %.2f%%"
)

// File and Directory Constants
const (
	TestDataDir = "testdata"
	EventsFile  = "events.json"
)
