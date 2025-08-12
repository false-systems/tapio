package patterns

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// GraphClient interface for graph queries
type GraphClient interface {
	ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error)
}

// Detector identifies patterns in event streams
type Detector struct {
	client   GraphClient
	logger   *zap.Logger
	patterns []Pattern
}

// Pattern represents a detectable failure pattern
type Pattern interface {
	Name() string
	Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error)
}

// Detection represents a detected pattern
type Detection struct {
	PatternName string               `json:"pattern_name"`
	Confidence  float64              `json:"confidence"`
	Severity    domain.EventSeverity `json:"severity"`
	Message     string               `json:"message"`
	Evidence    []string             `json:"evidence"`
	Metadata    *DetectionMetadata   `json:"metadata"`
	DetectedAt  time.Time            `json:"detected_at"`
}

// NewDetector creates a new pattern detector
func NewDetector(client GraphClient, logger *zap.Logger) *Detector {
	return &Detector{
		client: client,
		logger: logger,
		patterns: []Pattern{
			NewOOMKillPattern(),
			NewConfigMapChangePattern(),
			NewNodePressurePattern(),
			NewCrashLoopPattern(),
			NewServiceDisruptionPattern(),
			NewRollingUpdateFailurePattern(),
		},
	}
}

// DetectPatterns runs all pattern detectors on an event
func (d *Detector) DetectPatterns(ctx context.Context, event *domain.UnifiedEvent) ([]*Detection, error) {
	var detections []*Detection

	for _, pattern := range d.patterns {
		detection, err := pattern.Detect(ctx, event, d.client)
		if err != nil {
			d.logger.Error("Pattern detection failed",
				zap.String("pattern", pattern.Name()),
				zap.Error(err))
			continue
		}

		if detection != nil && detection.Confidence > 0.7 {
			detections = append(detections, detection)

			// Log high-confidence detections
			d.logger.Info("Pattern detected",
				zap.String("pattern", detection.PatternName),
				zap.Float64("confidence", detection.Confidence),
				zap.String("message", detection.Message))
		}
	}

	return detections, nil
}

// OOMKillPattern detects OOM kill cascades
type OOMKillPattern struct{}

func NewOOMKillPattern() *OOMKillPattern {
	return &OOMKillPattern{}
}

func (p *OOMKillPattern) Name() string {
	return "oom_kill_cascade"
}

func (p *OOMKillPattern) Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error) {
	// Check if this is an OOMKilled event
	if event.Type != "pod_oom_killed" && event.Message != "OOMKilled" {
		return nil, nil
	}

	// Query for related impacts
	query := `
		MATCH (p:Pod {uid: $podUID})
		MATCH (p)<-[:SELECTS]-(s:Service)
		MATCH (s)-[:SELECTS]->(otherPods:Pod)
		WHERE otherPods.uid <> p.uid
		
		// Check recent restarts of other pods
		OPTIONAL MATCH (e:Event {type: 'pod_restarted'})-[:AFFECTS]->(otherPods)
		WHERE e.timestamp > $timestamp - 300
		
		RETURN s.name as service, 
		       count(DISTINCT otherPods) as totalPods,
		       count(DISTINCT e) as recentRestarts
	`

	params := map[string]interface{}{
		"podUID":    event.Entity.UID,
		"timestamp": event.Timestamp.Unix(),
	}

	results, err := client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	result := results[0]
	totalPods := getInt(result, "totalPods")
	recentRestarts := getInt(result, "recentRestarts")
	serviceName := getString(result, "service")

	// Calculate pattern confidence
	confidence := 0.8
	severity := domain.EventSeverityWarning

	if recentRestarts > 0 {
		confidence = 0.95
		severity = domain.EventSeverityCritical
	}

	return &Detection{
		PatternName: p.Name(),
		Confidence:  confidence,
		Severity:    severity,
		Message:     "OOM Kill detected with potential service disruption",
		Evidence: []string{
			"Pod killed due to memory limits",
			fmt.Sprintf("Service has %d total pods", totalPods),
			fmt.Sprintf("Recent restarts: %d", recentRestarts),
		},
		Metadata: &DetectionMetadata{
			Service:        serviceName,
			TotalPods:      totalPods,
			RecentRestarts: recentRestarts,
		},
		DetectedAt: time.Now(),
	}, nil
}

// ConfigMapChangePattern detects ConfigMap changes causing restarts
type ConfigMapChangePattern struct{}

func NewConfigMapChangePattern() *ConfigMapChangePattern {
	return &ConfigMapChangePattern{}
}

func (p *ConfigMapChangePattern) Name() string {
	return "configmap_change_cascade"
}

func (p *ConfigMapChangePattern) Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error) {
	// Check if this is a ConfigMap update event
	if event.Entity == nil || event.Entity.Type != "configmap" || event.Type != "modified" {
		return nil, nil
	}

	// Find pods mounting this ConfigMap
	query := `
		MATCH (cm:ConfigMap {uid: $cmUID})
		MATCH (p:Pod)-[:MOUNTS]->(cm)
		
		// Check for recent pod restarts
		OPTIONAL MATCH (e:Event {type: 'pod_restarted'})-[:AFFECTS]->(p)
		WHERE e.timestamp > $timestamp
		
		// Find deployments
		OPTIONAL MATCH (p)-[:OWNED_BY*1..2]->(d:Deployment)
		
		RETURN count(DISTINCT p) as affectedPods,
		       count(DISTINCT e) as restarts,
		       collect(DISTINCT d.name) as deployments
	`

	params := map[string]interface{}{
		"cmUID":     event.Entity.UID,
		"timestamp": event.Timestamp.Unix(),
	}

	results, err := client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	result := results[0]
	affectedPods := getInt(result, "affectedPods")
	restarts := getInt(result, "restarts")

	if affectedPods == 0 {
		return nil, nil
	}

	confidence := 0.7
	if restarts > 0 {
		confidence = 0.9
	}

	return &Detection{
		PatternName: p.Name(),
		Confidence:  confidence,
		Severity:    domain.EventSeverityWarning,
		Message:     "ConfigMap change detected affecting multiple pods",
		Evidence: []string{
			"ConfigMap updated",
			fmt.Sprintf("Affected pods: %d", affectedPods),
			fmt.Sprintf("Triggered restarts: %d", restarts),
		},
		Metadata: &DetectionMetadata{
			AffectedPods: affectedPods,
			Restarts:     restarts,
		},
		DetectedAt: time.Now(),
	}, nil
}

// NodePressurePattern detects node pressure causing pod evictions
type NodePressurePattern struct{}

func NewNodePressurePattern() *NodePressurePattern {
	return &NodePressurePattern{}
}

func (p *NodePressurePattern) Name() string {
	return "node_pressure_eviction"
}

func (p *NodePressurePattern) Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error) {
	// Check if this is a pod eviction or node pressure event
	if event.Type != "pod_evicted" && event.Type != "node_pressure" {
		return nil, nil
	}

	// For pod eviction, check if it's due to node pressure
	if event.Type == "pod_evicted" && event.Entity != nil && event.Entity.Type == "pod" {
		// Query for node conditions and other evictions
		query := `
			MATCH (p:Pod {uid: $podUID})-[:RUNS_ON]->(n:Node)
			MATCH (e:Event {type: 'node_pressure'})-[:AFFECTS]->(n)
			WHERE e.timestamp > $startTime
			
			// Find other pods evicted from same node
			OPTIONAL MATCH (otherPod:Pod)-[:RUNS_ON]->(n)
			OPTIONAL MATCH (evictEvent:Event {type: 'pod_evicted'})-[:AFFECTS]->(otherPod)
			WHERE evictEvent.timestamp > $startTime
			
			RETURN n.name as nodeName,
			       count(DISTINCT e) as pressureEvents,
			       count(DISTINCT evictEvent) as evictions,
			       e.message as pressureReason
			LIMIT 1
		`

		params := map[string]interface{}{
			"podUID":    event.Entity.UID,
			"startTime": event.Timestamp.Add(-30 * time.Minute).Unix(),
		}

		results, err := client.ExecuteQuery(ctx, query, params)
		if err != nil {
			return nil, err
		}

		if len(results) > 0 {
			result := results[0]
			pressureEvents := getInt(result, "pressureEvents")
			evictions := getInt(result, "evictions")
			nodeName := getString(result, "nodeName")

			if pressureEvents > 0 || evictions > 2 {
				return &Detection{
					PatternName: p.Name(),
					Confidence:  0.9,
					Severity:    domain.EventSeverityCritical,
					Message:     "Node pressure causing pod evictions",
					Evidence: []string{
						fmt.Sprintf("Node %s under pressure", nodeName),
						fmt.Sprintf("Pressure events: %d", pressureEvents),
						fmt.Sprintf("Total evictions: %d", evictions),
					},
					Metadata: &DetectionMetadata{
						Node:           nodeName,
						PressureEvents: pressureEvents,
						Evictions:      evictions,
					},
					DetectedAt: time.Now(),
				}, nil
			}
		}
	}

	return nil, nil
}

// CrashLoopPattern detects crash loop backoff
type CrashLoopPattern struct{}

func NewCrashLoopPattern() *CrashLoopPattern {
	return &CrashLoopPattern{}
}

func (p *CrashLoopPattern) Name() string {
	return "crash_loop_backoff"
}

func (p *CrashLoopPattern) Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error) {
	// Check if pod is restarting frequently
	if event.Entity == nil || event.Entity.Type != "pod" || event.Type != "pod_restarted" {
		return nil, nil
	}

	// Count recent restarts
	query := `
		MATCH (p:Pod {uid: $podUID})
		MATCH (e:Event {type: 'pod_restarted'})-[:AFFECTS]->(p)
		WHERE e.timestamp > $startTime
		RETURN count(e) as restartCount,
		       min(e.timestamp) as firstRestart,
		       max(e.timestamp) as lastRestart
	`

	params := map[string]interface{}{
		"podUID":    event.Entity.UID,
		"startTime": event.Timestamp.Add(-10 * time.Minute).Unix(),
	}

	results, err := client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	result := results[0]
	restartCount := getInt(result, "restartCount")

	if restartCount < 3 {
		return nil, nil
	}

	return &Detection{
		PatternName: p.Name(),
		Confidence:  0.95,
		Severity:    domain.EventSeverityCritical,
		Message:     "Pod is in crash loop backoff",
		Evidence: []string{
			fmt.Sprintf("Restart count in 10 minutes: %d", restartCount),
			"Pod continuously failing to start",
		},
		Metadata: &DetectionMetadata{
			RestartCount: restartCount,
		},
		DetectedAt: time.Now(),
	}, nil
}

// ServiceDisruptionPattern detects service disruption
type ServiceDisruptionPattern struct{}

func NewServiceDisruptionPattern() *ServiceDisruptionPattern {
	return &ServiceDisruptionPattern{}
}

func (p *ServiceDisruptionPattern) Name() string {
	return "service_disruption"
}

func (p *ServiceDisruptionPattern) Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error) {
	// Check multiple indicators of service disruption
	if event.Entity == nil || event.Entity.Type != "service" {
		return nil, nil
	}

	// Query for service health indicators
	query := `
		MATCH (s:Service {uid: $serviceUID})
		
		// Find pods selected by service
		MATCH (s)-[:SELECTS]->(p:Pod)
		
		// Check pod states
		OPTIONAL MATCH (podEvent:Event)-[:AFFECTS]->(p)
		WHERE podEvent.timestamp > $startTime
		AND podEvent.type IN ['pod_restarted', 'pod_failed', 'pod_evicted']
		
		// Check endpoints
		OPTIONAL MATCH (e:Endpoints {name: s.name, namespace: s.namespace})
		
		RETURN s.name as serviceName,
		       count(DISTINCT p) as totalPods,
		       count(DISTINCT CASE WHEN p.phase = 'Running' THEN p END) as runningPods,
		       count(DISTINCT podEvent) as podIssues,
		       e.addresses as endpoints
	`

	params := map[string]interface{}{
		"serviceUID": event.Entity.UID,
		"startTime":  event.Timestamp.Add(-10 * time.Minute).Unix(),
	}

	results, err := client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	result := results[0]
	totalPods := getInt(result, "totalPods")
	runningPods := getInt(result, "runningPods")
	podIssues := getInt(result, "podIssues")
	serviceName := getString(result, "serviceName")

	// Calculate service health
	if totalPods == 0 {
		// No pods backing the service
		return &Detection{
			PatternName: p.Name(),
			Confidence:  1.0,
			Severity:    domain.EventSeverityCritical,
			Message:     "Service has no backing pods",
			Evidence: []string{
				fmt.Sprintf("Service %s has no pods", serviceName),
				"Endpoints likely empty",
				"Service is completely down",
			},
			Metadata: &DetectionMetadata{
				Service:     serviceName,
				TotalPods:   0,
				RunningPods: 0,
			},
			DetectedAt: time.Now(),
		}, nil
	}

	// Check for partial disruption
	healthRatio := float64(runningPods) / float64(totalPods)
	if healthRatio < 0.5 || podIssues > totalPods/2 {
		confidence := 0.8
		severity := domain.EventSeverityWarning

		if healthRatio == 0 {
			confidence = 0.95
			severity = domain.EventSeverityCritical
		}

		return &Detection{
			PatternName: p.Name(),
			Confidence:  confidence,
			Severity:    severity,
			Message:     "Service experiencing disruption",
			Evidence: []string{
				fmt.Sprintf("Only %d/%d pods running", runningPods, totalPods),
				fmt.Sprintf("Recent pod issues: %d", podIssues),
				fmt.Sprintf("Service health: %.0f%%", healthRatio*100),
			},
			Metadata: &DetectionMetadata{
				Service:     serviceName,
				TotalPods:   totalPods,
				RunningPods: runningPods,
				HealthRatio: healthRatio,
				PodIssues:   podIssues,
			},
			DetectedAt: time.Now(),
		}, nil
	}

	return nil, nil
}

// RollingUpdateFailurePattern detects failed rolling updates
type RollingUpdateFailurePattern struct{}

func NewRollingUpdateFailurePattern() *RollingUpdateFailurePattern {
	return &RollingUpdateFailurePattern{}
}

func (p *RollingUpdateFailurePattern) Name() string {
	return "rolling_update_failure"
}

func (p *RollingUpdateFailurePattern) Detect(ctx context.Context, event *domain.UnifiedEvent, client GraphClient) (*Detection, error) {
	// Check if this is a deployment or replicaset event
	if event.Entity == nil || (event.Entity.Type != "deployment" && event.Entity.Type != "replicaset") {
		return nil, nil
	}

	// Query for rolling update status
	query := `
		MATCH (d:Deployment {uid: $deploymentUID})
		
		// Find current and previous ReplicaSets
		MATCH (d)-[:OWNS]->(rs:ReplicaSet)
		
		// Find pods in each ReplicaSet
		MATCH (rs)-[:OWNS]->(p:Pod)
		
		// Check for failed pods in new ReplicaSet
		OPTIONAL MATCH (failEvent:Event)-[:AFFECTS]->(p)
		WHERE failEvent.timestamp > $startTime
		AND failEvent.type IN ['pod_failed', 'pod_crashloop', 'image_pull_failed']
		
		// Get deployment progress
		WITH d, rs, 
		     count(DISTINCT p) as podCount,
		     count(DISTINCT CASE WHEN p.phase = 'Running' AND p.ready = true THEN p END) as readyPods,
		     count(DISTINCT failEvent) as failedPods,
		     rs.revision as revision
		ORDER BY revision DESC
		
		RETURN d.name as deploymentName,
		       d.replicas as desiredReplicas,
		       collect({
		         revision: revision,
		         podCount: podCount,
		         readyPods: readyPods,
		         failedPods: failedPods
		       }) as replicaSets,
		       d.updatedReplicas as updatedReplicas,
		       d.availableReplicas as availableReplicas
		LIMIT 1
	`

	params := map[string]interface{}{
		"deploymentUID": event.Entity.UID,
		"startTime":     event.Timestamp.Add(-30 * time.Minute).Unix(),
	}

	results, err := client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	result := results[0]
	deploymentName := getString(result, "deploymentName")
	desiredReplicas := getInt(result, "desiredReplicas")
	updatedReplicas := getInt(result, "updatedReplicas")
	availableReplicas := getInt(result, "availableReplicas")

	// Check if update is stuck
	if updatedReplicas < desiredReplicas && availableReplicas < desiredReplicas {
		// Analyze ReplicaSet data
		replicaSets, ok := result["replicaSets"].([]interface{})
		if ok && len(replicaSets) > 1 {
			// Get latest RS (first in ordered list)
			latestRS, ok := replicaSets[0].(map[string]interface{})
			if ok {
				failedPods := getInt(latestRS, "failedPods")
				readyPods := getInt(latestRS, "readyPods")

				if failedPods > 0 || (readyPods == 0 && updatedReplicas > 0) {
					return &Detection{
						PatternName: p.Name(),
						Confidence:  0.9,
						Severity:    domain.EventSeverityCritical,
						Message:     "Rolling update is failing",
						Evidence: []string{
							fmt.Sprintf("Deployment %s stuck during update", deploymentName),
							fmt.Sprintf("Desired: %d, Updated: %d, Available: %d",
								desiredReplicas, updatedReplicas, availableReplicas),
							fmt.Sprintf("Failed pods in new ReplicaSet: %d", failedPods),
							"New pods failing to become ready",
						},
						Metadata: &DetectionMetadata{
							Deployment:        deploymentName,
							DesiredReplicas:   desiredReplicas,
							UpdatedReplicas:   updatedReplicas,
							AvailableReplicas: availableReplicas,
							FailedPods:        failedPods,
						},
						DetectedAt: time.Now(),
					}, nil
				}
			}
		}
	}

	// Check for slow progress
	if updatedReplicas > 0 && updatedReplicas < desiredReplicas {
		progressRatio := float64(updatedReplicas) / float64(desiredReplicas)
		if progressRatio < 0.5 {
			return &Detection{
				PatternName: p.Name(),
				Confidence:  0.7,
				Severity:    domain.EventSeverityWarning,
				Message:     "Rolling update progressing slowly",
				Evidence: []string{
					fmt.Sprintf("Update progress: %.0f%%", progressRatio*100),
					fmt.Sprintf("Updated: %d/%d replicas", updatedReplicas, desiredReplicas),
					"Update may be stuck or throttled",
				},
				Metadata: &DetectionMetadata{
					Deployment:      deploymentName,
					ProgressRatio:   progressRatio,
					UpdatedReplicas: updatedReplicas,
					DesiredReplicas: desiredReplicas,
				},
				DetectedAt: time.Now(),
			}, nil
		}
	}

	return nil, nil
}

// Helper functions
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(int64); ok {
		return int(v)
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}
