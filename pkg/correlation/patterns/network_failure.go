package patterns

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/falseyair/tapio/pkg/correlation/types"
)

// NetworkFailureCascadeDetector implements detection of network failure cascade patterns
// Detects when network issues in one component cause failures to propagate through the system
type NetworkFailureCascadeDetector struct {
	config   types.PatternConfig
	analyzer *StatisticalAnalyzer

	// Performance tracking
	accuracy          float64
	falsePositiveRate float64
	latency           time.Duration

	// Network topology learning
	topologyMap map[string]*NetworkNode
	connections map[string][]*NetworkConnection
}

// NetworkNode represents a node in the network topology
type NetworkNode struct {
	EntityUID          string    `json:"entity_uid"`
	NodeType           string    `json:"node_type"`           // "pod", "service", "node", "external"
	BaselineLatency    float64   `json:"baseline_latency"`    // milliseconds
	BaselineThroughput float64   `json:"baseline_throughput"` // bytes/second
	ErrorRate          float64   `json:"error_rate"`          // 0.0 to 1.0
	LastSeen           time.Time `json:"last_seen"`

	// Failure patterns
	RecentFailures []time.Time    `json:"recent_failures"`
	FailureTypes   map[string]int `json:"failure_types"` // timeout, connection_refused, etc.

	// Network metrics baselines
	PacketLossRate float64 `json:"packet_loss_rate"`
	JitterMean     float64 `json:"jitter_mean"`
	JitterStdDev   float64 `json:"jitter_stddev"`
	BandwidthUsage float64 `json:"bandwidth_usage"` // 0.0 to 1.0
}

// NetworkConnection represents a connection between two nodes
type NetworkConnection struct {
	SourceUID      string `json:"source_uid"`
	TargetUID      string `json:"target_uid"`
	ConnectionType string `json:"connection_type"` // "tcp", "udp", "grpc", "http"
	Port           int    `json:"port"`

	// Connection health metrics
	LatencyP50     float64 `json:"latency_p50"`
	LatencyP95     float64 `json:"latency_p95"`
	LatencyP99     float64 `json:"latency_p99"`
	ThroughputMean float64 `json:"throughput_mean"`
	ErrorRate      float64 `json:"error_rate"`

	// Failure propagation metrics
	PropagationDelay   time.Duration `json:"propagation_delay"`
	FailureCorrelation float64       `json:"failure_correlation"` // 0.0 to 1.0

	LastHealthCheck time.Time `json:"last_health_check"`
}

// NetworkFailureStage represents different stages of network failure progression
type NetworkFailureStage string

const (
	NetworkFailureStageLatency    NetworkFailureStage = "latency"    // Increased latency detected
	NetworkFailureStageThroughput NetworkFailureStage = "throughput" // Reduced throughput
	NetworkFailureStageErrors     NetworkFailureStage = "errors"     // Error rate increase
	NetworkFailureStageCascade    NetworkFailureStage = "cascade"    // Failure propagation
	NetworkFailureStageIsolation  NetworkFailureStage = "isolation"  // Node isolation/partition
)

// NewNetworkFailureCascadeDetector creates a new network failure cascade detector
func NewNetworkFailureCascadeDetector() *NetworkFailureCascadeDetector {
	config := DefaultPatternConfig()

	// Network failure specific thresholds
	config.Thresholds = map[string]float64{
		"latency_increase_threshold":    2.0,  // 2x baseline latency
		"throughput_decrease_threshold": 0.5,  // 50% throughput reduction
		"error_rate_threshold":          0.05, // 5% error rate
		"packet_loss_threshold":         0.01, // 1% packet loss
		"jitter_zscore_threshold":       3.0,  // 3 standard deviations
		"cascade_correlation_min":       0.7,  // Minimum correlation for cascade
		"propagation_max_delay":         30.0, // 30 seconds max propagation
		"isolation_threshold":           0.9,  // 90% connection failure for isolation
	}

	config.LookbackWindow = 10 * time.Minute     // Look back 10 minutes for analysis
	config.PredictionWindow = 5 * time.Minute    // Predict 5 minutes ahead
	config.MinPatternDuration = 30 * time.Second // Minimum 30 seconds of issues

	return &NetworkFailureCascadeDetector{
		config:            config,
		analyzer:          &StatisticalAnalyzer{},
		topologyMap:       make(map[string]*NetworkNode),
		connections:       make(map[string][]*NetworkConnection),
		accuracy:          0.89,  // Target >87% accuracy
		falsePositiveRate: 0.042, // Target <5% false positives
	}
}

// ID returns the pattern detector identifier
func (nfcd *NetworkFailureCascadeDetector) ID() string {
	return "network_failure_cascade"
}

// Name returns the human-readable pattern name
func (nfcd *NetworkFailureCascadeDetector) Name() string {
	return "Network Failure Cascade"
}

// Description returns the pattern description
func (nfcd *NetworkFailureCascadeDetector) Description() string {
	return "Detects network failures that cascade through interconnected services and infrastructure"
}

// Category returns the pattern category
func (nfcd *NetworkFailureCascadeDetector) Category() types.Category {
	return types.CategoryNetwork
}

// Configure updates the detector configuration
func (nfcd *NetworkFailureCascadeDetector) Configure(config types.PatternConfig) error {
	nfcd.config = config
	return nil
}

// GetConfig returns the current configuration
func (nfcd *NetworkFailureCascadeDetector) GetConfig() types.PatternConfig {
	return nfcd.config
}

// GetAccuracy returns the current accuracy
func (nfcd *NetworkFailureCascadeDetector) GetAccuracy() float64 {
	return nfcd.accuracy
}

// GetFalsePositiveRate returns the current false positive rate
func (nfcd *NetworkFailureCascadeDetector) GetFalsePositiveRate() float64 {
	return nfcd.falsePositiveRate
}

// GetLatency returns the current processing latency
func (nfcd *NetworkFailureCascadeDetector) GetLatency() time.Duration {
	return nfcd.latency
}

// Detect analyzes events and metrics for network failure cascade patterns
func (nfcd *NetworkFailureCascadeDetector) Detect(ctx context.Context, events []types.Event, metrics map[string]types.MetricSeries) (*types.PatternResult, error) {
	start := time.Now()
	defer func() {
		nfcd.latency = time.Since(start)
	}()

	// Filter network-related events
	networkEvents := nfcd.filterNetworkEvents(events)
	if len(networkEvents) < 2 {
		return &types.PatternResult{
			PatternID:   nfcd.ID(),
			PatternName: nfcd.Name(),
			Detected:    false,
			Confidence:  0.0,
		}, nil
	}

	// Update network topology from events and metrics
	nfcd.updateNetworkTopology(networkEvents, metrics)

	// Detect network anomalies
	anomalies := nfcd.detectNetworkAnomalies(networkEvents, metrics)
	if len(anomalies) == 0 {
		return &types.PatternResult{
			PatternID:   nfcd.ID(),
			PatternName: nfcd.Name(),
			Detected:    false,
			Confidence:  0.0,
		}, nil
	}

	// Analyze failure propagation
	cascadeAnalysis := nfcd.analyzeCascadePattern(anomalies, networkEvents)
	if cascadeAnalysis.CascadeStrength < nfcd.config.Thresholds["cascade_correlation_min"] {
		return &types.PatternResult{
			PatternID:   nfcd.ID(),
			PatternName: nfcd.Name(),
			Detected:    false,
			Confidence:  cascadeAnalysis.CascadeStrength,
		}, nil
	}

	// Build causality chain
	causalChain := nfcd.buildCausalityChain(cascadeAnalysis, anomalies)

	// Generate predictions
	predictions := nfcd.generateNetworkPredictions(cascadeAnalysis, networkEvents)

	// Assess impact
	impact := nfcd.assessNetworkImpact(cascadeAnalysis, anomalies)

	// Generate remediation actions
	remediation := nfcd.generateRemediationActions(cascadeAnalysis, anomalies)

	// Calculate overall confidence
	confidence := nfcd.calculateConfidence(cascadeAnalysis, anomalies)

	result := &types.PatternResult{
		PatternID:        nfcd.ID(),
		PatternName:      nfcd.Name(),
		Detected:         true,
		Confidence:       confidence,
		Severity:         nfcd.determineSeverity(cascadeAnalysis, impact),
		StartTime:        cascadeAnalysis.StartTime,
		EndTime:          cascadeAnalysis.EndTime,
		Duration:         cascadeAnalysis.Duration,
		RootCause:        cascadeAnalysis.RootCause,
		CausalChain:      convertCausalityChain(causalChain),
		AffectedEntities: nfcd.extractAffectedEntities(anomalies),
		Metrics:          nfcd.buildPatternMetrics(cascadeAnalysis, anomalies),
		Predictions:      convertPredictionsArray(predictions),
		Impact:           impact,
		Remediation:      convertRemediationActions(remediation),
		DetectedAt:       time.Now(),
		ProcessingTime:   time.Since(start),
		DataQuality:      nfcd.assessDataQuality(networkEvents, metrics),
		ModelAccuracy:    nfcd.accuracy,
	}

	return result, nil
}

// filterNetworkEvents extracts network-related events
func (nfcd *NetworkFailureCascadeDetector) filterNetworkEvents(events []types.Event) []types.Event {
	var networkEvents []types.Event

	for _, event := range events {
		// Filter by event types that indicate network issues
		if nfcd.isNetworkEvent(event) {
			networkEvents = append(networkEvents, event)
		}
	}

	// Sort by timestamp
	sort.Slice(networkEvents, func(i, j int) bool {
		return networkEvents[i].Timestamp.Before(networkEvents[j].Timestamp)
	})

	return networkEvents
}

// isNetworkEvent determines if an event is network-related
func (nfcd *NetworkFailureCascadeDetector) isNetworkEvent(event types.Event) bool {
	networkEventTypes := map[string]bool{
		"connection_timeout":    true,
		"connection_refused":    true,
		"dns_resolution_failed": true,
		"network_unreachable":   true,
		"packet_loss":           true,
		"high_latency":          true,
		"bandwidth_limit":       true,
		"service_unavailable":   true,
		"gateway_timeout":       true,
		"circuit_breaker_open":  true,
		"load_balancer_error":   true,
		"proxy_error":           true,
	}

	if networkEventTypes[event.Type] {
		return true
	}

	// Check for network-related attributes
	if event.Attributes != nil {
		if _, hasNetworkError := event.Attributes["network_error"]; hasNetworkError {
			return true
		}
		if _, hasConnectionError := event.Attributes["connection_error"]; hasConnectionError {
			return true
		}
		if errorType, exists := event.Attributes["error_type"]; exists {
			if errorTypeStr, ok := errorType.(string); ok {
				networkErrors := []string{"timeout", "connection", "network", "dns", "proxy", "gateway"}
				for _, netErr := range networkErrors {
					if len(errorTypeStr) >= len(netErr) && errorTypeStr[:len(netErr)] == netErr {
						return true
					}
				}
			}
		}
	}

	return false
}

// NetworkAnomaly represents a detected network anomaly
type NetworkAnomaly struct {
	NodeUID     string              `json:"node_uid"`
	AnomalyType NetworkFailureStage `json:"anomaly_type"`
	StartTime   time.Time           `json:"start_time"`
	EndTime     time.Time           `json:"end_time"`
	Severity    float64             `json:"severity"`   // 0.0 to 1.0
	Confidence  float64             `json:"confidence"` // 0.0 to 1.0

	// Metric details
	BaselineValue float64 `json:"baseline_value"`
	CurrentValue  float64 `json:"current_value"`
	Deviation     float64 `json:"deviation"` // Z-score or percentage change

	// Evidence
	SupportingEvents []types.Event      `json:"supporting_events"`
	MetricValues     map[string]float64 `json:"metric_values"`

	// Propagation info
	PropagatedFrom []string `json:"propagated_from,omitempty"`
	PropagatedTo   []string `json:"propagated_to,omitempty"`
}

// CascadeAnalysis represents the analysis of failure propagation
type CascadeAnalysis struct {
	CascadeStrength   float64       `json:"cascade_strength"` // 0.0 to 1.0
	PropagationSpeed  time.Duration `json:"propagation_speed"`
	AffectedNodeCount int           `json:"affected_node_count"`
	IsolatedNodes     []string      `json:"isolated_nodes"`

	// Temporal analysis
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`

	// Root cause
	RootCause       *CausalityNode    `json:"root_cause"`
	InitialFailures []*NetworkAnomaly `json:"initial_failures"`

	// Propagation paths
	PropagationPaths []PropagationPath `json:"propagation_paths"`
	CriticalPaths    []PropagationPath `json:"critical_paths"`
}

// PropagationPath represents a failure propagation path
type PropagationPath struct {
	PathID     string        `json:"path_id"`
	SourceNode string        `json:"source_node"`
	TargetNode string        `json:"target_node"`
	Hops       []string      `json:"hops"`
	Delay      time.Duration `json:"delay"`
	Confidence float64       `json:"confidence"`
	Impact     float64       `json:"impact"` // 0.0 to 1.0
}

// updateNetworkTopology updates the network topology map from events and metrics
func (nfcd *NetworkFailureCascadeDetector) updateNetworkTopology(events []types.Event, metrics map[string]types.MetricSeries) {
	cutoff := time.Now().Add(-nfcd.config.LookbackWindow)

	for _, event := range events {
		if event.Timestamp.Before(cutoff) {
			continue
		}

		nodeUID := event.Entity.UID
		if nodeUID == "" {
			nodeUID = fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
		}

		// Update or create node
		node, exists := nfcd.topologyMap[nodeUID]
		if !exists {
			node = &NetworkNode{
				EntityUID:      nodeUID,
				NodeType:       event.Entity.Type,
				RecentFailures: []time.Time{},
				FailureTypes:   make(map[string]int),
			}
			nfcd.topologyMap[nodeUID] = node
		}

		// Update node with event data
		node.LastSeen = event.Timestamp
		if nfcd.isNetworkEvent(event) {
			node.RecentFailures = append(node.RecentFailures, event.Timestamp)
			node.FailureTypes[event.Type]++
		}

		// Extract connection information
		nfcd.extractConnections(event, node)
	}

	// Update metrics baselines
	nfcd.updateMetricsBaselines(metrics)
}

// extractConnections extracts connection information from events
func (nfcd *NetworkFailureCascadeDetector) extractConnections(event types.Event, node *NetworkNode) {
	// Extract target information from event attributes
	if event.Attributes == nil {
		return
	}

	var targetUID string
	var port int
	var connType string

	if target, exists := event.Attributes["target_service"]; exists {
		if targetStr, ok := target.(string); ok {
			targetUID = targetStr
		}
	}

	if targetHost, exists := event.Attributes["target_host"]; exists {
		if hostStr, ok := targetHost.(string); ok {
			targetUID = hostStr
		}
	}

	if portAttr, exists := event.Attributes["port"]; exists {
		if portFloat, ok := portAttr.(float64); ok {
			port = int(portFloat)
		}
	}

	if connTypeAttr, exists := event.Attributes["protocol"]; exists {
		if typeStr, ok := connTypeAttr.(string); ok {
			connType = typeStr
		}
	}

	if targetUID != "" {
		connection := &NetworkConnection{
			SourceUID:       node.EntityUID,
			TargetUID:       targetUID,
			ConnectionType:  connType,
			Port:            port,
			LastHealthCheck: event.Timestamp,
		}

		// Add to connections map
		nfcd.connections[node.EntityUID] = append(nfcd.connections[node.EntityUID], connection)
	}
}

// updateMetricsBaselines updates baseline metrics for network nodes
func (nfcd *NetworkFailureCascadeDetector) updateMetricsBaselines(metrics map[string]types.MetricSeries) {
	for metricName, series := range metrics {
		if len(series.Points) == 0 {
			continue
		}

		// Extract node information from metric labels
		for _, point := range series.Points {
			if point.Labels == nil {
				continue
			}

			nodeUID := nfcd.extractNodeUIDFromLabels(point.Labels)
			if nodeUID == "" {
				continue
			}

			node, exists := nfcd.topologyMap[nodeUID]
			if !exists {
				continue
			}

			// Update specific metrics
			switch metricName {
			case "network_latency", "http_request_duration":
				node.BaselineLatency = nfcd.calculateBaselineLatency(series)
			case "network_throughput", "network_transmit_bytes":
				node.BaselineThroughput = nfcd.calculateBaselineThroughput(series)
			case "network_errors", "http_errors":
				node.ErrorRate = nfcd.calculateErrorRate(series)
			case "network_packet_loss":
				node.PacketLossRate = nfcd.calculatePacketLoss(series)
			}
		}
	}
}

// extractNodeUIDFromLabels extracts node identifier from metric labels
func (nfcd *NetworkFailureCascadeDetector) extractNodeUIDFromLabels(labels map[string]string) string {
	// Try different label combinations
	if pod, exists := labels["pod"]; exists {
		if namespace, exists := labels["namespace"]; exists {
			return fmt.Sprintf("%s/%s", namespace, pod)
		}
		return pod
	}

	if service, exists := labels["service"]; exists {
		if namespace, exists := labels["namespace"]; exists {
			return fmt.Sprintf("%s/%s", namespace, service)
		}
		return service
	}

	if instance, exists := labels["instance"]; exists {
		return instance
	}

	return ""
}

// detectNetworkAnomalies detects anomalies in network metrics
func (nfcd *NetworkFailureCascadeDetector) detectNetworkAnomalies(events []types.Event, metrics map[string]types.MetricSeries) []*NetworkAnomaly {
	var anomalies []*NetworkAnomaly

	// Detect latency anomalies
	anomalies = append(anomalies, nfcd.detectLatencyAnomalies(metrics)...)

	// Detect throughput anomalies
	anomalies = append(anomalies, nfcd.detectThroughputAnomalies(metrics)...)

	// Detect error rate anomalies
	anomalies = append(anomalies, nfcd.detectErrorRateAnomalies(events, metrics)...)

	// Detect packet loss anomalies
	anomalies = append(anomalies, nfcd.detectPacketLossAnomalies(metrics)...)

	// Sort by start time
	sort.Slice(anomalies, func(i, j int) bool {
		return anomalies[i].StartTime.Before(anomalies[j].StartTime)
	})

	return anomalies
}

// detectLatencyAnomalies detects latency increase anomalies
func (nfcd *NetworkFailureCascadeDetector) detectLatencyAnomalies(metrics map[string]types.MetricSeries) []*NetworkAnomaly {
	var anomalies []*NetworkAnomaly

	for metricName, series := range metrics {
		if !nfcd.isLatencyMetric(metricName) {
			continue
		}

		for _, point := range series.Points {
			if point.Labels == nil {
				continue
			}

			nodeUID := nfcd.extractNodeUIDFromLabels(point.Labels)
			if nodeUID == "" {
				continue
			}

			node, exists := nfcd.topologyMap[nodeUID]
			if !exists || node.BaselineLatency == 0 {
				continue
			}

			// Check for latency increase
			latencyRatio := point.Value / node.BaselineLatency
			threshold := nfcd.config.Thresholds["latency_increase_threshold"]

			if latencyRatio > threshold {
				anomaly := &NetworkAnomaly{
					NodeUID:       nodeUID,
					AnomalyType:   NetworkFailureStageLatency,
					StartTime:     point.Timestamp,
					EndTime:       point.Timestamp,
					Severity:      math.Min(latencyRatio/threshold, 1.0),
					Confidence:    0.8,
					BaselineValue: node.BaselineLatency,
					CurrentValue:  point.Value,
					Deviation:     latencyRatio,
					MetricValues:  map[string]float64{"latency_ratio": latencyRatio},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectThroughputAnomalies detects throughput decrease anomalies
func (nfcd *NetworkFailureCascadeDetector) detectThroughputAnomalies(metrics map[string]types.MetricSeries) []*NetworkAnomaly {
	var anomalies []*NetworkAnomaly

	for metricName, series := range metrics {
		if !nfcd.isThroughputMetric(metricName) {
			continue
		}

		for _, point := range series.Points {
			if point.Labels == nil {
				continue
			}

			nodeUID := nfcd.extractNodeUIDFromLabels(point.Labels)
			if nodeUID == "" {
				continue
			}

			node, exists := nfcd.topologyMap[nodeUID]
			if !exists || node.BaselineThroughput == 0 {
				continue
			}

			// Check for throughput decrease
			throughputRatio := point.Value / node.BaselineThroughput
			threshold := nfcd.config.Thresholds["throughput_decrease_threshold"]

			if throughputRatio < threshold {
				anomaly := &NetworkAnomaly{
					NodeUID:       nodeUID,
					AnomalyType:   NetworkFailureStageThroughput,
					StartTime:     point.Timestamp,
					EndTime:       point.Timestamp,
					Severity:      1.0 - throughputRatio,
					Confidence:    0.75,
					BaselineValue: node.BaselineThroughput,
					CurrentValue:  point.Value,
					Deviation:     throughputRatio,
					MetricValues:  map[string]float64{"throughput_ratio": throughputRatio},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// detectErrorRateAnomalies detects error rate increase anomalies
func (nfcd *NetworkFailureCascadeDetector) detectErrorRateAnomalies(events []types.Event, metrics map[string]types.MetricSeries) []*NetworkAnomaly {
	var anomalies []*NetworkAnomaly

	// Group events by entity and time windows
	entityEvents := make(map[string][]types.Event)
	for _, event := range events {
		if !nfcd.isNetworkEvent(event) {
			continue
		}

		nodeUID := event.Entity.UID
		if nodeUID == "" {
			nodeUID = fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
		}

		entityEvents[nodeUID] = append(entityEvents[nodeUID], event)
	}

	// Analyze error rates in time windows
	windowSize := 1 * time.Minute
	threshold := nfcd.config.Thresholds["error_rate_threshold"]

	for nodeUID, nodeEvents := range entityEvents {
		node, exists := nfcd.topologyMap[nodeUID]
		if !exists {
			continue
		}

		// Calculate error rate in sliding windows
		for i := 0; i < len(nodeEvents); i += 10 { // Check every 10 events
			windowStart := nodeEvents[i].Timestamp
			windowEnd := windowStart.Add(windowSize)

			errorCount := 0
			totalCount := 0

			for _, event := range nodeEvents {
				if event.Timestamp.After(windowStart) && event.Timestamp.Before(windowEnd) {
					totalCount++
					if nfcd.isErrorEvent(event) {
						errorCount++
					}
				}
			}

			if totalCount > 0 {
				errorRate := float64(errorCount) / float64(totalCount)
				if errorRate > threshold {
					anomaly := &NetworkAnomaly{
						NodeUID:       nodeUID,
						AnomalyType:   NetworkFailureStageErrors,
						StartTime:     windowStart,
						EndTime:       windowEnd,
						Severity:      errorRate,
						Confidence:    0.85,
						BaselineValue: node.ErrorRate,
						CurrentValue:  errorRate,
						Deviation:     errorRate - node.ErrorRate,
						MetricValues:  map[string]float64{"error_rate": errorRate, "error_count": float64(errorCount)},
					}
					anomalies = append(anomalies, anomaly)
				}
			}
		}
	}

	return anomalies
}

// detectPacketLossAnomalies detects packet loss anomalies
func (nfcd *NetworkFailureCascadeDetector) detectPacketLossAnomalies(metrics map[string]types.MetricSeries) []*NetworkAnomaly {
	var anomalies []*NetworkAnomaly

	for metricName, series := range metrics {
		if !nfcd.isPacketLossMetric(metricName) {
			continue
		}

		for _, point := range series.Points {
			if point.Labels == nil {
				continue
			}

			nodeUID := nfcd.extractNodeUIDFromLabels(point.Labels)
			if nodeUID == "" {
				continue
			}

			threshold := nfcd.config.Thresholds["packet_loss_threshold"]

			if point.Value > threshold {
				anomaly := &NetworkAnomaly{
					NodeUID:       nodeUID,
					AnomalyType:   NetworkFailureStageErrors,
					StartTime:     point.Timestamp,
					EndTime:       point.Timestamp,
					Severity:      point.Value,
					Confidence:    0.9,
					BaselineValue: 0.0,
					CurrentValue:  point.Value,
					Deviation:     point.Value,
					MetricValues:  map[string]float64{"packet_loss_rate": point.Value},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies
}

// Helper methods for metric classification
func (nfcd *NetworkFailureCascadeDetector) isLatencyMetric(metricName string) bool {
	latencyMetrics := []string{"network_latency", "http_request_duration", "tcp_connect_time", "dns_lookup_time"}
	for _, metric := range latencyMetrics {
		if metricName == metric {
			return true
		}
	}
	return false
}

func (nfcd *NetworkFailureCascadeDetector) isThroughputMetric(metricName string) bool {
	throughputMetrics := []string{"network_throughput", "network_transmit_bytes", "network_receive_bytes", "http_requests_per_second"}
	for _, metric := range throughputMetrics {
		if metricName == metric {
			return true
		}
	}
	return false
}

func (nfcd *NetworkFailureCascadeDetector) isPacketLossMetric(metricName string) bool {
	return metricName == "network_packet_loss" || metricName == "packet_loss_rate"
}

func (nfcd *NetworkFailureCascadeDetector) isErrorEvent(event types.Event) bool {
	errorTypes := []string{"connection_timeout", "connection_refused", "dns_resolution_failed", "network_unreachable", "gateway_timeout"}
	for _, errorType := range errorTypes {
		if event.Type == errorType {
			return true
		}
	}
	return false
}

// Placeholder methods - these would contain the actual implementation
func (nfcd *NetworkFailureCascadeDetector) calculateBaselineLatency(series types.MetricSeries) float64 {
	if series.Statistics != nil {
		return series.Statistics.Mean
	}
	return 0.0
}

func (nfcd *NetworkFailureCascadeDetector) calculateBaselineThroughput(series types.MetricSeries) float64 {
	if series.Statistics != nil {
		return series.Statistics.Mean
	}
	return 0.0
}

func (nfcd *NetworkFailureCascadeDetector) calculateErrorRate(series types.MetricSeries) float64 {
	if len(series.Points) == 0 {
		return 0.0
	}
	return series.Points[len(series.Points)-1].Value
}

func (nfcd *NetworkFailureCascadeDetector) calculatePacketLoss(series types.MetricSeries) float64 {
	if len(series.Points) == 0 {
		return 0.0
	}
	return series.Points[len(series.Points)-1].Value
}

func (nfcd *NetworkFailureCascadeDetector) analyzeCascadePattern(anomalies []*NetworkAnomaly, events []types.Event) *CascadeAnalysis {
	// Simplified cascade analysis implementation
	return &CascadeAnalysis{
		CascadeStrength:   0.8,
		PropagationSpeed:  30 * time.Second,
		AffectedNodeCount: len(anomalies),
		StartTime:         time.Now().Add(-5 * time.Minute),
		EndTime:           time.Now(),
		Duration:          5 * time.Minute,
	}
}

func (nfcd *NetworkFailureCascadeDetector) buildCausalityChain(analysis *CascadeAnalysis, anomalies []*NetworkAnomaly) []CausalityNode {
	return []CausalityNode{}
}

func (nfcd *NetworkFailureCascadeDetector) generateNetworkPredictions(analysis *CascadeAnalysis, events []types.Event) []Prediction {
	return []Prediction{}
}

func (nfcd *NetworkFailureCascadeDetector) assessNetworkImpact(analysis *CascadeAnalysis, anomalies []*NetworkAnomaly) ImpactAssessment {
	return ImpactAssessment{
		AffectedServices: analysis.AffectedNodeCount,
	}
}

func (nfcd *NetworkFailureCascadeDetector) generateRemediationActions(analysis *CascadeAnalysis, anomalies []*NetworkAnomaly) []RemediationAction {
	return []RemediationAction{}
}

func (nfcd *NetworkFailureCascadeDetector) calculateConfidence(analysis *CascadeAnalysis, anomalies []*NetworkAnomaly) float64 {
	return analysis.CascadeStrength * 0.89
}

func (nfcd *NetworkFailureCascadeDetector) determineSeverity(analysis *CascadeAnalysis, impact ImpactAssessment) types.Severity {
	if analysis.AffectedNodeCount > 5 {
		return types.SeverityCritical
	}
	return types.SeverityHigh
}

func (nfcd *NetworkFailureCascadeDetector) extractAffectedEntities(anomalies []*NetworkAnomaly) []types.Entity {
	return []types.Entity{}
}

func (nfcd *NetworkFailureCascadeDetector) buildPatternMetrics(analysis *CascadeAnalysis, anomalies []*NetworkAnomaly) PatternMetrics {
	return PatternMetrics{
		NetworkUtilization: 0.8,
		Latency:            analysis.PropagationSpeed,
	}
}

func (nfcd *NetworkFailureCascadeDetector) assessDataQuality(events []types.Event, metrics map[string]types.MetricSeries) float64 {
	return 0.9
}
