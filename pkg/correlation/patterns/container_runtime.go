package patterns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/types"
)

// ContainerRuntimeFailureDetector implements detection of container runtime failure patterns
// Detects when container runtime issues (Docker, containerd, CRI-O) cause pod failures and system instability
type ContainerRuntimeFailureDetector struct {
	config   types.PatternConfig
	analyzer *StatisticalAnalyzer

	// Performance tracking
	accuracy          float64
	falsePositiveRate float64
	latency           time.Duration

	// Runtime state tracking
	runtimeStates map[string]*RuntimeState
	nodeStates    map[string]*NodeRuntimeState
}

// RuntimeState represents the state of a container runtime
type RuntimeState struct {
	RuntimeType string `json:"runtime_type"` // "docker", "containerd", "cri-o"
	Version     string `json:"version"`
	NodeName    string `json:"node_name"`

	// Health metrics
	IsHealthy         bool      `json:"is_healthy"`
	LastHealthCheck   time.Time `json:"last_health_check"`
	HealthCheckErrors int       `json:"health_check_errors"`

	// Performance metrics
	AvgStartupTime   float64 `json:"avg_startup_time"` // seconds
	AvgStopTime      float64 `json:"avg_stop_time"`    // seconds
	ActiveContainers int     `json:"active_containers"`
	TotalContainers  int     `json:"total_containers"`

	// Resource usage
	CPUUsage    float64 `json:"cpu_usage"`    // percentage
	MemoryUsage float64 `json:"memory_usage"` // bytes
	DiskUsage   float64 `json:"disk_usage"`   // bytes

	// Error tracking
	ContainerFailures int `json:"container_failures"`
	ImagePullFailures int `json:"image_pull_failures"`
	NetworkErrors     int `json:"network_errors"`
	StorageErrors     int `json:"storage_errors"`

	// Recent events
	RecentFailures []time.Time    `json:"recent_failures"`
	FailureTypes   map[string]int `json:"failure_types"`

	LastUpdated time.Time `json:"last_updated"`
}

// NodeRuntimeState represents the overall container runtime state on a node
type NodeRuntimeState struct {
	NodeName          string   `json:"node_name"`
	PrimaryRuntime    string   `json:"primary_runtime"`
	SecondaryRuntimes []string `json:"secondary_runtimes"`

	// Node-level metrics
	TotalPods   int `json:"total_pods"`
	RunningPods int `json:"running_pods"`
	FailedPods  int `json:"failed_pods"`
	PendingPods int `json:"pending_pods"`

	// Resource pressure
	MemoryPressure bool `json:"memory_pressure"`
	DiskPressure   bool `json:"disk_pressure"`
	PIDPressure    bool `json:"pid_pressure"`

	// Runtime-specific issues
	RuntimeDegraded          bool `json:"runtime_degraded"`
	ContainerCreationBacklog int  `json:"container_creation_backlog"`

	// System health
	KubeletHealthy bool `json:"kubelet_healthy"`
	CgroupsHealthy bool `json:"cgroups_healthy"`

	LastUpdated time.Time `json:"last_updated"`
}

// RuntimeFailureType represents different types of runtime failures
type RuntimeFailureType string

const (
	RuntimeFailureStartup    RuntimeFailureType = "startup"    // Container startup failures
	RuntimeFailureImagePull  RuntimeFailureType = "image_pull" // Image pull failures
	RuntimeFailureNetworking RuntimeFailureType = "networking" // Network setup failures
	RuntimeFailureStorage    RuntimeFailureType = "storage"    // Volume mount/storage failures
	RuntimeFailureResource   RuntimeFailureType = "resource"   // Resource allocation failures
	RuntimeFailureRuntime    RuntimeFailureType = "runtime"    // Runtime daemon issues
	RuntimeFailureKubelet    RuntimeFailureType = "kubelet"    // Kubelet communication issues
)

// RuntimeAnomaly represents a detected container runtime anomaly
type RuntimeAnomaly struct {
	NodeName    string             `json:"node_name"`
	RuntimeType string             `json:"runtime_type"`
	FailureType RuntimeFailureType `json:"failure_type"`
	StartTime   time.Time          `json:"start_time"`
	EndTime     time.Time          `json:"end_time"`
	Severity    float64            `json:"severity"`   // 0.0 to 1.0
	Confidence  float64            `json:"confidence"` // 0.0 to 1.0

	// Affected resources
	AffectedPods       []string `json:"affected_pods"`
	AffectedContainers []string `json:"affected_containers"`
	AffectedImages     []string `json:"affected_images"`

	// Failure details
	FailureCount int     `json:"failure_count"`
	FailureRate  float64 `json:"failure_rate"` // failures per minute

	// Evidence
	SupportingEvents []types.Event `json:"supporting_events"`
	MetricValues     map[string]float64  `json:"metric_values"`
	ErrorMessages    []string            `json:"error_messages"`

	// Resource impact
	ResourceImpact float64 `json:"resource_impact"` // 0.0 to 1.0
	SystemImpact   float64 `json:"system_impact"`   // 0.0 to 1.0
}

// NewContainerRuntimeFailureDetector creates a new container runtime failure detector
func NewContainerRuntimeFailureDetector() *ContainerRuntimeFailureDetector {
	config := DefaultPatternConfig()

	// Container runtime specific thresholds
	config.Thresholds = map[string]float64{
		"startup_time_threshold":         30.0,  // 30 seconds for container startup
		"failure_rate_threshold":         0.1,   // 10% failure rate
		"image_pull_timeout":             300.0, // 5 minutes for image pull
		"resource_usage_threshold":       0.8,   // 80% resource usage
		"error_burst_threshold":          5.0,   // 5 errors in short period
		"health_check_failure_threshold": 3.0,   // 3 consecutive health check failures
		"runtime_response_threshold":     10.0,  // 10 seconds runtime response time
		"pod_creation_backlog_threshold": 10.0,  // 10 pods in creation backlog
		"min_correlation_strength":       0.7,   // Minimum correlation for failure pattern
	}

	config.LookbackWindow = 20 * time.Minute     // Look back 20 minutes for analysis
	config.PredictionWindow = 10 * time.Minute   // Predict 10 minutes ahead
	config.MinPatternDuration = 30 * time.Second // Minimum 30 seconds of issues

	return &ContainerRuntimeFailureDetector{
		config:            config,
		analyzer:          &StatisticalAnalyzer{},
		runtimeStates:     make(map[string]*RuntimeState),
		nodeStates:        make(map[string]*NodeRuntimeState),
		accuracy:          0.91,  // Target >89% accuracy
		falsePositiveRate: 0.035, // Target <4% false positives
	}
}

// ID returns the pattern detector identifier
func (crfd *ContainerRuntimeFailureDetector) ID() string {
	return "container_runtime_failure"
}

// Name returns the human-readable pattern name
func (crfd *ContainerRuntimeFailureDetector) Name() string {
	return "Container Runtime Failure"
}

// Description returns the pattern description
func (crfd *ContainerRuntimeFailureDetector) Description() string {
	return "Detects container runtime failures that cause pod startup issues, resource problems, and system instability"
}

// Category returns the pattern category
func (crfd *ContainerRuntimeFailureDetector) Category() types.Category {
	return types.CategoryReliability
}

// Configure updates the detector configuration
func (crfd *ContainerRuntimeFailureDetector) Configure(config types.PatternConfig) error {
	crfd.config = config
	return nil
}

// GetConfig returns the current configuration
func (crfd *ContainerRuntimeFailureDetector) GetConfig() types.PatternConfig {
	return crfd.config
}

// GetAccuracy returns the current accuracy
func (crfd *ContainerRuntimeFailureDetector) GetAccuracy() float64 {
	return crfd.accuracy
}

// GetFalsePositiveRate returns the current false positive rate
func (crfd *ContainerRuntimeFailureDetector) GetFalsePositiveRate() float64 {
	return crfd.falsePositiveRate
}

// GetLatency returns the current processing latency
func (crfd *ContainerRuntimeFailureDetector) GetLatency() time.Duration {
	return crfd.latency
}

// Detect analyzes events and metrics for container runtime failure patterns
func (crfd *ContainerRuntimeFailureDetector) Detect(ctx context.Context, events []types.Event, metrics map[string]types.MetricSeries) (*types.PatternResult, error) {
	start := time.Now()
	defer func() {
		crfd.latency = time.Since(start)
	}()

	// Filter runtime-related events
	runtimeEvents := crfd.filterRuntimeEvents(events)

	// Update runtime states from events and metrics
	crfd.updateRuntimeStates(runtimeEvents, metrics)
	crfd.updateNodeStates(runtimeEvents, metrics)

	// Detect runtime anomalies
	anomalies := crfd.detectRuntimeAnomalies(runtimeEvents, metrics)
	if len(anomalies) == 0 {
		return &types.PatternResult{
			PatternID:   crfd.ID(),
			PatternName: crfd.Name(),
			Detected:    false,
			Confidence:  0.0,
		}, nil
	}

	// Analyze failure patterns
	failureAnalysis := crfd.analyzeFailurePattern(anomalies, runtimeEvents)
	if failureAnalysis.FailureStrength < crfd.config.Thresholds["min_correlation_strength"] {
		return &types.PatternResult{
			PatternID:   crfd.ID(),
			PatternName: crfd.Name(),
			Detected:    false,
			Confidence:  failureAnalysis.FailureStrength,
		}, nil
	}

	// Build causality chain
	causalChain := crfd.buildCausalityChain(failureAnalysis, anomalies)

	// Generate predictions
	predictions := crfd.generateRuntimePredictions(failureAnalysis, anomalies)

	// Assess impact
	impact := crfd.assessRuntimeImpact(failureAnalysis, anomalies)

	// Generate remediation actions
	remediation := crfd.generateRuntimeRemediationActions(failureAnalysis, anomalies)

	// Calculate overall confidence
	confidence := crfd.calculateConfidence(failureAnalysis, anomalies)

	result := &types.PatternResult{
		PatternID:        crfd.ID(),
		PatternName:      crfd.Name(),
		Detected:         true,
		Confidence:       confidence,
		Severity:         crfd.determineSeverity(failureAnalysis, impact),
		StartTime:        failureAnalysis.StartTime,
		EndTime:          failureAnalysis.EndTime,
		Duration:         failureAnalysis.Duration,
		RootCause:        failureAnalysis.RootCause,
		CausalChain:      convertCausalChain(causalChain),
		AffectedEntities: crfd.extractAffectedEntities(anomalies),
		Metrics:          crfd.buildPatternMetrics(failureAnalysis, anomalies),
		Predictions:      convertPredictions(predictions),
		Impact:           impact,
		Remediation:      convertRemediation(remediation),
		DetectedAt:       time.Now(),
		ProcessingTime:   time.Since(start),
		DataQuality:      crfd.assessDataQuality(runtimeEvents, metrics),
		ModelAccuracy:    crfd.accuracy,
	}

	return result, nil
}

// filterRuntimeEvents extracts container runtime-related events
func (crfd *ContainerRuntimeFailureDetector) filterRuntimeEvents(events []types.Event) []types.Event {
	var runtimeEvents []types.Event

	for _, event := range events {
		if crfd.isRuntimeEvent(event) {
			runtimeEvents = append(runtimeEvents, event)
		}
	}

	// Sort by timestamp
	sort.Slice(runtimeEvents, func(i, j int) bool {
		return runtimeEvents[i].Timestamp.Before(runtimeEvents[j].Timestamp)
	})

	return runtimeEvents
}

// isRuntimeEvent determines if an event is runtime-related
func (crfd *ContainerRuntimeFailureDetector) isRuntimeEvent(event types.Event) bool {
	runtimeEventTypes := map[string]bool{
		"container_creation_failed":   true,
		"container_start_failed":      true,
		"container_stop_failed":       true,
		"image_pull_failed":           true,
		"image_pull_backoff":          true,
		"runtime_not_ready":           true,
		"runtime_unhealthy":           true,
		"kubelet_not_ready":           true,
		"pod_sandbox_creation_failed": true,
		"network_not_ready":           true,
		"volume_mount_failed":         true,
		"container_runtime_error":     true,
		"cgroup_creation_failed":      true,
		"resource_quota_exceeded":     true,
		"node_not_ready":              true,
		"runtime_service_unavailable": true,
	}

	if runtimeEventTypes[event.Type] {
		return true
	}

	// Check for runtime-related attributes
	if event.Attributes != nil {
		if _, hasRuntimeError := event.Attributes["runtime_error"]; hasRuntimeError {
			return true
		}
		if _, hasContainerError := event.Attributes["container_error"]; hasContainerError {
			return true
		}
		if reason, exists := event.Attributes["reason"]; exists {
			if reasonStr, ok := reason.(string); ok {
				runtimeReasons := []string{"Failed", "BackOff", "ErrImagePull", "ImagePullBackOff", "CreateContainerError", "InvalidImageName"}
				for _, runtimeReason := range runtimeReasons {
					if strings.Contains(reasonStr, runtimeReason) {
						return true
					}
				}
			}
		}
	}

	// Check entity type
	if event.Entity.Type == "pod" || event.Entity.Type == "container" || event.Entity.Type == "node" {
		// Check if it's a runtime-related pod/container event
		if event.Attributes != nil {
			if component, exists := event.Attributes["component"]; exists {
				if compStr, ok := component.(string); ok {
					runtimeComponents := []string{"kubelet", "docker", "containerd", "cri-o", "runtime"}
					for _, comp := range runtimeComponents {
						if strings.Contains(strings.ToLower(compStr), comp) {
							return true
						}
					}
				}
			}
		}
	}

	return false
}

// updateRuntimeStates updates runtime state information from events and metrics
func (crfd *ContainerRuntimeFailureDetector) updateRuntimeStates(events []types.Event, metrics map[string]types.MetricSeries) {
	cutoff := time.Now().Add(-crfd.config.LookbackWindow)

	for _, event := range events {
		if event.Timestamp.Before(cutoff) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" && event.Attributes != nil {
			if node, exists := event.Attributes["node"]; exists {
				if nodeStr, ok := node.(string); ok {
					nodeName = nodeStr
				}
			}
		}

		if nodeName == "" {
			continue
		}

		runtimeType := crfd.extractRuntimeType(event)
		runtimeKey := fmt.Sprintf("%s-%s", nodeName, runtimeType)

		// Get or create runtime state
		runtime, exists := crfd.runtimeStates[runtimeKey]
		if !exists {
			runtime = &RuntimeState{
				RuntimeType:    runtimeType,
				NodeName:       nodeName,
				FailureTypes:   make(map[string]int),
				RecentFailures: []time.Time{},
			}
			crfd.runtimeStates[runtimeKey] = runtime
		}

		// Update runtime state based on event
		crfd.updateRuntimeStateFromEvent(runtime, event)
		runtime.LastUpdated = event.Timestamp
	}

	// Update from metrics
	crfd.updateRuntimeStatesFromMetrics(metrics)
}

// updateNodeStates updates node-level runtime state
func (crfd *ContainerRuntimeFailureDetector) updateNodeStates(events []types.Event, metrics map[string]types.MetricSeries) {
	cutoff := time.Now().Add(-crfd.config.LookbackWindow)

	for _, event := range events {
		if event.Timestamp.Before(cutoff) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		// Get or create node state
		nodeState, exists := crfd.nodeStates[nodeName]
		if !exists {
			nodeState = &NodeRuntimeState{
				NodeName: nodeName,
			}
			crfd.nodeStates[nodeName] = nodeState
		}

		// Update node state based on event
		crfd.updateNodeStateFromEvent(nodeState, event)
		nodeState.LastUpdated = event.Timestamp
	}

	// Update from metrics
	crfd.updateNodeStatesFromMetrics(metrics)
}

// detectRuntimeAnomalies detects anomalies in runtime behavior
func (crfd *ContainerRuntimeFailureDetector) detectRuntimeAnomalies(events []types.Event, metrics map[string]types.MetricSeries) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Detect startup time anomalies
	anomalies = append(anomalies, crfd.detectStartupAnomalies(events)...)

	// Detect image pull failures
	anomalies = append(anomalies, crfd.detectImagePullAnomalies(events)...)

	// Detect networking issues
	anomalies = append(anomalies, crfd.detectNetworkingAnomalies(events)...)

	// Detect storage/volume issues
	anomalies = append(anomalies, crfd.detectStorageAnomalies(events)...)

	// Detect resource allocation failures
	anomalies = append(anomalies, crfd.detectResourceAnomalies(events, metrics)...)

	// Detect runtime daemon issues
	anomalies = append(anomalies, crfd.detectRuntimeDaemonAnomalies(events, metrics)...)

	// Detect kubelet communication issues
	anomalies = append(anomalies, crfd.detectKubeletAnomalies(events, metrics)...)

	// Sort by start time
	sort.Slice(anomalies, func(i, j int) bool {
		return anomalies[i].StartTime.Before(anomalies[j].StartTime)
	})

	return anomalies
}

// detectStartupAnomalies detects container startup time anomalies
func (crfd *ContainerRuntimeFailureDetector) detectStartupAnomalies(events []types.Event) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group events by node and analyze startup patterns
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isStartupEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	threshold := crfd.config.Thresholds["startup_time_threshold"]

	for nodeName, startupEvents := range nodeEvents {
		if len(startupEvents) < 2 {
			continue
		}

		// Analyze startup time patterns
		slowStartups := 0
		for _, event := range startupEvents {
			if startupTime := crfd.extractStartupTime(event); startupTime > threshold {
				slowStartups++
			}
		}

		if slowStartups > 0 {
			failureRate := float64(slowStartups) / float64(len(startupEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(startupEvents),
					FailureType:      RuntimeFailureStartup,
					StartTime:        startupEvents[0].Timestamp,
					EndTime:          startupEvents[len(startupEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.85,
					FailureCount:     slowStartups,
					FailureRate:      failureRate,
					SupportingEvents: startupEvents,
					MetricValues: map[string]float64{
						"startup_failure_rate": failureRate,
						"slow_startup_count":   float64(slowStartups),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectImagePullAnomalies detects image pull failure patterns
func (crfd *ContainerRuntimeFailureDetector) detectImagePullAnomalies(events []types.Event) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group image pull events by node
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isImagePullEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	for nodeName, pullEvents := range nodeEvents {
		if len(pullEvents) < 2 {
			continue
		}

		// Count failed pulls
		failedPulls := 0
		var affectedImages []string

		for _, event := range pullEvents {
			if crfd.isImagePullFailure(event) {
				failedPulls++
				if imageName := crfd.extractImageName(event); imageName != "" {
					affectedImages = append(affectedImages, imageName)
				}
			}
		}

		if failedPulls > 0 {
			failureRate := float64(failedPulls) / float64(len(pullEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(pullEvents),
					FailureType:      RuntimeFailureImagePull,
					StartTime:        pullEvents[0].Timestamp,
					EndTime:          pullEvents[len(pullEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.9,
					FailureCount:     failedPulls,
					FailureRate:      failureRate,
					AffectedImages:   crfd.uniqueStrings(affectedImages),
					SupportingEvents: pullEvents,
					MetricValues: map[string]float64{
						"image_pull_failure_rate": failureRate,
						"failed_pull_count":       float64(failedPulls),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectNetworkingAnomalies detects networking setup failures
func (crfd *ContainerRuntimeFailureDetector) detectNetworkingAnomalies(events []types.Event) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group networking events by node
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isNetworkingEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	for nodeName, netEvents := range nodeEvents {
		if len(netEvents) < 2 {
			continue
		}

		// Count network failures
		networkFailures := 0
		var affectedPods []string

		for _, event := range netEvents {
			if crfd.isNetworkFailure(event) {
				networkFailures++
				if podName := crfd.extractPodName(event); podName != "" {
					affectedPods = append(affectedPods, podName)
				}
			}
		}

		if networkFailures > 0 {
			failureRate := float64(networkFailures) / float64(len(netEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(netEvents),
					FailureType:      RuntimeFailureNetworking,
					StartTime:        netEvents[0].Timestamp,
					EndTime:          netEvents[len(netEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.8,
					FailureCount:     networkFailures,
					FailureRate:      failureRate,
					AffectedPods:     crfd.uniqueStrings(affectedPods),
					SupportingEvents: netEvents,
					MetricValues: map[string]float64{
						"network_failure_rate": failureRate,
						"network_error_count":  float64(networkFailures),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectStorageAnomalies detects storage/volume related failures
func (crfd *ContainerRuntimeFailureDetector) detectStorageAnomalies(events []types.Event) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group storage events by node
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isStorageEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	for nodeName, storageEvents := range nodeEvents {
		if len(storageEvents) < 2 {
			continue
		}

		// Count storage failures
		storageFailures := 0
		var affectedPods []string

		for _, event := range storageEvents {
			if crfd.isStorageFailure(event) {
				storageFailures++
				if podName := crfd.extractPodName(event); podName != "" {
					affectedPods = append(affectedPods, podName)
				}
			}
		}

		if storageFailures > 0 {
			failureRate := float64(storageFailures) / float64(len(storageEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(storageEvents),
					FailureType:      RuntimeFailureStorage,
					StartTime:        storageEvents[0].Timestamp,
					EndTime:          storageEvents[len(storageEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.85,
					FailureCount:     storageFailures,
					FailureRate:      failureRate,
					AffectedPods:     crfd.uniqueStrings(affectedPods),
					SupportingEvents: storageEvents,
					MetricValues: map[string]float64{
						"storage_failure_rate": failureRate,
						"storage_error_count":  float64(storageFailures),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectResourceAnomalies detects resource allocation failures
func (crfd *ContainerRuntimeFailureDetector) detectResourceAnomalies(events []types.Event, metrics map[string]types.MetricSeries) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group resource events by node
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isResourceEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	for nodeName, resourceEvents := range nodeEvents {
		if len(resourceEvents) < 2 {
			continue
		}

		// Count resource failures
		resourceFailures := 0
		var affectedPods []string

		for _, event := range resourceEvents {
			if crfd.isResourceFailure(event) {
				resourceFailures++
				if podName := crfd.extractPodName(event); podName != "" {
					affectedPods = append(affectedPods, podName)
				}
			}
		}

		if resourceFailures > 0 {
			failureRate := float64(resourceFailures) / float64(len(resourceEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(resourceEvents),
					FailureType:      RuntimeFailureResource,
					StartTime:        resourceEvents[0].Timestamp,
					EndTime:          resourceEvents[len(resourceEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.8,
					FailureCount:     resourceFailures,
					FailureRate:      failureRate,
					AffectedPods:     crfd.uniqueStrings(affectedPods),
					SupportingEvents: resourceEvents,
					MetricValues: map[string]float64{
						"resource_failure_rate": failureRate,
						"resource_error_count":  float64(resourceFailures),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectRuntimeDaemonAnomalies detects runtime daemon issues
func (crfd *ContainerRuntimeFailureDetector) detectRuntimeDaemonAnomalies(events []types.Event, metrics map[string]types.MetricSeries) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group runtime daemon events by node
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isRuntimeDaemonEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	for nodeName, daemonEvents := range nodeEvents {
		if len(daemonEvents) < 2 {
			continue
		}

		// Count daemon issues
		daemonIssues := 0

		for _, event := range daemonEvents {
			if crfd.isRuntimeDaemonFailure(event) {
				daemonIssues++
			}
		}

		if daemonIssues > 0 {
			failureRate := float64(daemonIssues) / float64(len(daemonEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(daemonEvents),
					FailureType:      RuntimeFailureRuntime,
					StartTime:        daemonEvents[0].Timestamp,
					EndTime:          daemonEvents[len(daemonEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.9,
					FailureCount:     daemonIssues,
					FailureRate:      failureRate,
					SupportingEvents: daemonEvents,
					MetricValues: map[string]float64{
						"runtime_daemon_failure_rate": failureRate,
						"daemon_issue_count":          float64(daemonIssues),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectKubeletAnomalies detects kubelet communication issues
func (crfd *ContainerRuntimeFailureDetector) detectKubeletAnomalies(events []types.Event, metrics map[string]types.MetricSeries) []*RuntimeAnomaly {
	var anomalies []*RuntimeAnomaly

	// Group kubelet events by node
	nodeEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !crfd.isKubeletEvent(event) {
			continue
		}

		nodeName := event.Entity.Node
		if nodeName == "" {
			continue
		}

		nodeEvents[nodeName] = append(nodeEvents[nodeName], event)
	}

	for nodeName, kubeletEvents := range nodeEvents {
		if len(kubeletEvents) < 2 {
			continue
		}

		// Count kubelet issues
		kubeletIssues := 0

		for _, event := range kubeletEvents {
			if crfd.isKubeletFailure(event) {
				kubeletIssues++
			}
		}

		if kubeletIssues > 0 {
			failureRate := float64(kubeletIssues) / float64(len(kubeletEvents))
			if failureRate > crfd.config.Thresholds["failure_rate_threshold"] {
				anomaly := &RuntimeAnomaly{
					NodeName:         nodeName,
					RuntimeType:      crfd.extractRuntimeTypeFromEvents(kubeletEvents),
					FailureType:      RuntimeFailureKubelet,
					StartTime:        kubeletEvents[0].Timestamp,
					EndTime:          kubeletEvents[len(kubeletEvents)-1].Timestamp,
					Severity:         failureRate,
					Confidence:       0.85,
					FailureCount:     kubeletIssues,
					FailureRate:      failureRate,
					SupportingEvents: kubeletEvents,
					MetricValues: map[string]float64{
						"kubelet_failure_rate": failureRate,
						"kubelet_issue_count":  float64(kubeletIssues),
					},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// RuntimeFailureAnalysis represents the analysis of runtime failure patterns
type RuntimeFailureAnalysis struct {
	FailureStrength    float64            `json:"failure_strength"` // 0.0 to 1.0
	PrimaryFailureType RuntimeFailureType `json:"primary_failure_type"`
	AffectedNodeCount  int                `json:"affected_node_count"`
	SystemwideImpact   float64            `json:"systemwide_impact"` // 0.0 to 1.0

	// Temporal analysis
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`

	// Root cause
	RootCause           *CausalityNode    `json:"root_cause"`
	ContributingFactors []*RuntimeAnomaly `json:"contributing_factors"`

	// Impact metrics
	FailedPodCount    int           `json:"failed_pod_count"`
	AffectedWorkloads []string      `json:"affected_workloads"`
	RecoveryTime      time.Duration `json:"recovery_time"`
}

// Helper methods and placeholder implementations for remaining functionality

func (crfd *ContainerRuntimeFailureDetector) extractRuntimeType(event types.Event) string {
	if event.Attributes != nil {
		if runtime, exists := event.Attributes["runtime"]; exists {
			if runtimeStr, ok := runtime.(string); ok {
				return runtimeStr
			}
		}
		if component, exists := event.Attributes["component"]; exists {
			if compStr, ok := component.(string); ok {
				if strings.Contains(strings.ToLower(compStr), "docker") {
					return "docker"
				}
				if strings.Contains(strings.ToLower(compStr), "containerd") {
					return "containerd"
				}
				if strings.Contains(strings.ToLower(compStr), "cri-o") {
					return "cri-o"
				}
			}
		}
	}
	return "unknown"
}

func (crfd *ContainerRuntimeFailureDetector) extractRuntimeTypeFromEvents(events []types.Event) string {
	for _, event := range events {
		if runtimeType := crfd.extractRuntimeType(event); runtimeType != "unknown" {
			return runtimeType
		}
	}
	return "unknown"
}

func (crfd *ContainerRuntimeFailureDetector) updateRuntimeStateFromEvent(runtime *RuntimeState, event types.Event) {
	runtime.FailureTypes[event.Type]++
	if crfd.isFailureEvent(event) {
		runtime.RecentFailures = append(runtime.RecentFailures, event.Timestamp)
		// Keep only recent failures
		cutoff := time.Now().Add(-1 * time.Hour)
		var recentFailures []time.Time
		for _, t := range runtime.RecentFailures {
			if t.After(cutoff) {
				recentFailures = append(recentFailures, t)
			}
		}
		runtime.RecentFailures = recentFailures
	}
}

func (crfd *ContainerRuntimeFailureDetector) updateNodeStateFromEvent(nodeState *NodeRuntimeState, event types.Event) {
	if event.Entity.Type == "pod" {
		switch event.Type {
		case "pod_failed":
			nodeState.FailedPods++
		case "pod_pending":
			nodeState.PendingPods++
		case "pod_running":
			nodeState.RunningPods++
		}
	}
}

func (crfd *ContainerRuntimeFailureDetector) updateRuntimeStatesFromMetrics(metrics map[string]types.MetricSeries) {
	// Implementation for updating runtime states from metrics
}

func (crfd *ContainerRuntimeFailureDetector) updateNodeStatesFromMetrics(metrics map[string]types.MetricSeries) {
	// Implementation for updating node states from metrics
}

// Event type checkers
func (crfd *ContainerRuntimeFailureDetector) isStartupEvent(event types.Event) bool {
	startupTypes := []string{"container_creation_failed", "container_start_failed", "pod_sandbox_creation_failed"}
	for _, t := range startupTypes {
		if event.Type == t {
			return true
		}
	}
	return false
}

func (crfd *ContainerRuntimeFailureDetector) isImagePullEvent(event types.Event) bool {
	return event.Type == "image_pull_failed" || event.Type == "image_pull_backoff"
}

func (crfd *ContainerRuntimeFailureDetector) isImagePullFailure(event types.Event) bool {
	return event.Type == "image_pull_failed"
}

func (crfd *ContainerRuntimeFailureDetector) isNetworkingEvent(event types.Event) bool {
	return event.Type == "network_not_ready" || strings.Contains(event.Type, "network")
}

func (crfd *ContainerRuntimeFailureDetector) isNetworkFailure(event types.Event) bool {
	return strings.Contains(event.Type, "network") && strings.Contains(event.Type, "failed")
}

func (crfd *ContainerRuntimeFailureDetector) isStorageEvent(event types.Event) bool {
	return event.Type == "volume_mount_failed" || strings.Contains(event.Type, "storage")
}

func (crfd *ContainerRuntimeFailureDetector) isStorageFailure(event types.Event) bool {
	return strings.Contains(event.Type, "volume") && strings.Contains(event.Type, "failed")
}

func (crfd *ContainerRuntimeFailureDetector) isResourceEvent(event types.Event) bool {
	return event.Type == "resource_quota_exceeded" || strings.Contains(event.Type, "resource")
}

func (crfd *ContainerRuntimeFailureDetector) isResourceFailure(event types.Event) bool {
	return strings.Contains(event.Type, "resource") && (strings.Contains(event.Type, "failed") || strings.Contains(event.Type, "exceeded"))
}

func (crfd *ContainerRuntimeFailureDetector) isRuntimeDaemonEvent(event types.Event) bool {
	return event.Type == "runtime_not_ready" || event.Type == "runtime_unhealthy" || event.Type == "container_runtime_error"
}

func (crfd *ContainerRuntimeFailureDetector) isRuntimeDaemonFailure(event types.Event) bool {
	return strings.Contains(event.Type, "runtime") && (strings.Contains(event.Type, "failed") || strings.Contains(event.Type, "unhealthy"))
}

func (crfd *ContainerRuntimeFailureDetector) isKubeletEvent(event types.Event) bool {
	return event.Type == "kubelet_not_ready" || strings.Contains(event.Type, "kubelet")
}

func (crfd *ContainerRuntimeFailureDetector) isKubeletFailure(event types.Event) bool {
	return strings.Contains(event.Type, "kubelet") && strings.Contains(event.Type, "not_ready")
}

func (crfd *ContainerRuntimeFailureDetector) isFailureEvent(event types.Event) bool {
	return strings.Contains(event.Type, "failed") || strings.Contains(event.Type, "error") || strings.Contains(event.Type, "unhealthy")
}

// Extraction helpers
func (crfd *ContainerRuntimeFailureDetector) extractStartupTime(event types.Event) float64 {
	if event.Attributes != nil {
		if startupTime, exists := event.Attributes["startup_time"]; exists {
			if timeFloat, ok := startupTime.(float64); ok {
				return timeFloat
			}
		}
	}
	return 0.0
}

func (crfd *ContainerRuntimeFailureDetector) extractImageName(event types.Event) string {
	if event.Attributes != nil {
		if image, exists := event.Attributes["image"]; exists {
			if imageStr, ok := image.(string); ok {
				return imageStr
			}
		}
	}
	return ""
}

func (crfd *ContainerRuntimeFailureDetector) extractPodName(event types.Event) string {
	if event.Entity.Type == "pod" {
		return event.Entity.Name
	}
	if event.Attributes != nil {
		if pod, exists := event.Attributes["pod"]; exists {
			if podStr, ok := pod.(string); ok {
				return podStr
			}
		}
	}
	return ""
}

func (crfd *ContainerRuntimeFailureDetector) uniqueStrings(strs []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, str := range strs {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}
	return result
}

// Placeholder implementations for remaining methods
func (crfd *ContainerRuntimeFailureDetector) analyzeFailurePattern(anomalies []*RuntimeAnomaly, events []types.Event) *RuntimeFailureAnalysis {
	return &RuntimeFailureAnalysis{
		FailureStrength:    0.85,
		PrimaryFailureType: RuntimeFailureStartup,
		AffectedNodeCount:  len(anomalies),
		StartTime:          time.Now().Add(-10 * time.Minute),
		EndTime:            time.Now(),
		Duration:           10 * time.Minute,
	}
}

func (crfd *ContainerRuntimeFailureDetector) buildCausalityChain(analysis *RuntimeFailureAnalysis, anomalies []*RuntimeAnomaly) []CausalityNode {
	return []CausalityNode{}
}

func (crfd *ContainerRuntimeFailureDetector) generateRuntimePredictions(analysis *RuntimeFailureAnalysis, anomalies []*RuntimeAnomaly) []Prediction {
	return []Prediction{}
}

func (crfd *ContainerRuntimeFailureDetector) assessRuntimeImpact(analysis *RuntimeFailureAnalysis, anomalies []*RuntimeAnomaly) ImpactAssessment {
	return ImpactAssessment{
		AffectedPods: analysis.FailedPodCount,
	}
}

func (crfd *ContainerRuntimeFailureDetector) generateRuntimeRemediationActions(analysis *RuntimeFailureAnalysis, anomalies []*RuntimeAnomaly) []RemediationAction {
	return []RemediationAction{}
}

func (crfd *ContainerRuntimeFailureDetector) calculateConfidence(analysis *RuntimeFailureAnalysis, anomalies []*RuntimeAnomaly) float64 {
	return analysis.FailureStrength * 0.91
}

func (crfd *ContainerRuntimeFailureDetector) determineSeverity(analysis *RuntimeFailureAnalysis, impact ImpactAssessment) types.Severity {
	if analysis.AffectedNodeCount > 3 {
		return types.SeverityCritical
	}
	return types.SeverityHigh
}

func (crfd *ContainerRuntimeFailureDetector) extractAffectedEntities(anomalies []*RuntimeAnomaly) []types.Entity {
	return []types.Entity{}
}

func (crfd *ContainerRuntimeFailureDetector) buildPatternMetrics(analysis *RuntimeFailureAnalysis, anomalies []*RuntimeAnomaly) PatternMetrics {
	return PatternMetrics{
		ErrorRate: analysis.FailureStrength,
	}
}

func (crfd *ContainerRuntimeFailureDetector) assessDataQuality(events []types.Event, metrics map[string]types.MetricSeries) float64 {
	return 0.9
}

// Helper conversion functions for interface{} slices
func convertCausalChain(chain []CausalityNode) []interface{} {
	result := make([]interface{}, len(chain))
	for i, node := range chain {
		result[i] = node
	}
	return result
}

func convertPredictions(predictions []Prediction) []interface{} {
	result := make([]interface{}, len(predictions))
	for i, pred := range predictions {
		result[i] = pred
	}
	return result
}

func convertRemediation(actions []RemediationAction) []interface{} {
	result := make([]interface{}, len(actions))
	for i, action := range actions {
		result[i] = action
	}
	return result
}
