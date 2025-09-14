package behavior

// RelationType defines the types of relationships in our K8s behavior graph
// EXTRACTED FROM PROVEN CORRELATION PATTERNS
type RelationType string

const (
	// Resource relationships
	RelationMounts    RelationType = "MOUNTS"     // Pod mounts ConfigMap/Secret
	RelationSelects   RelationType = "SELECTS"    // Service selects Pods
	RelationOwns      RelationType = "OWNS"       // Deployment owns ReplicaSet owns Pod
	RelationRunsOn    RelationType = "RUNS_ON"    // Pod runs on Node
	RelationDependsOn RelationType = "DEPENDS_ON" // Service depends on another Service
	RelationExposes   RelationType = "EXPOSES"    // Service exposes Deployment
	RelationRoutesTo  RelationType = "ROUTES_TO"  // Ingress routes to Service

	// Event relationships
	RelationCaused    RelationType = "CAUSED"    // Event caused another Event
	RelationAffected  RelationType = "AFFECTED"  // Event affected a Resource
	RelationPreceded  RelationType = "PRECEDED"  // Event preceded another Event
	RelationTriggered RelationType = "TRIGGERED" // Event triggered an action

	// Pattern relationships
	RelationMatches   RelationType = "MATCHES"   // Event matches Pattern
	RelationPredicted RelationType = "PREDICTED" // Pattern predicted Event
	RelationValidated RelationType = "VALIDATED" // Feedback validated Prediction
)

// KnownSequences represents proven K8s behavioral sequences
// EXTRACTED FROM ACTUAL PRODUCTION PATTERNS
var KnownSequences = map[string][]string{
	"deployment_rollout": {
		"DeploymentUpdated",
		"ReplicaSetCreated",
		"PodCreated",
		"PodScheduled",
		"ContainerStarted",
		"PodReady",
		"EndpointUpdated",
	},
	"pod_crash_loop": {
		"PodStarted",
		"ContainerError",
		"PodFailed",
		"BackoffRestart",
		"PodRestarting",
		"CrashLoopBackOff",
	},
	"service_disruption": {
		"EndpointsChanged",
		"PodTerminating",
		"ServiceDegraded",
		"ConnectionRefused",
		"ServiceDown",
	},
	"config_cascade": {
		"ConfigMapUpdated",
		"PodRestartRequired",
		"PodTerminating",
		"PodCreated",
		"ServiceDisrupted",
	},
	"oom_cascade": {
		"MemoryPressure",
		"OOMKilled",
		"PodEvicted",
		"NodePressure",
		"CascadeEviction",
	},
	"dns_failure": {
		"CoreDNSError",
		"ResolutionFailed",
		"ServiceUnreachable",
		"ApplicationError",
	},
}
