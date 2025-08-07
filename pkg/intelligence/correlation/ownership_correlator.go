package correlation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"go.uber.org/zap"
)

// OwnershipCorrelator analyzes K8s ownership chains to find root causes
// It tracks: Deployment→ReplicaSet→Pod, StatefulSet→Pod, DaemonSet→Pod chains
type OwnershipCorrelator struct {
	*BaseCorrelator
	neo4jDriver neo4j.DriverWithContext
	logger      *zap.Logger
}

// NewOwnershipCorrelator creates a new ownership correlator
func NewOwnershipCorrelator(neo4jDriver neo4j.DriverWithContext, logger *zap.Logger) (*OwnershipCorrelator, error) {
	if neo4jDriver == nil {
		return nil, fmt.Errorf("neo4jDriver is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	capabilities := CorrelatorCapabilities{
		EventTypes: []string{
			"deployment_failed",
			"replicaset_failed",
			"pod_failed",
			"pod_deleted",
			"statefulset_failed",
			"daemonset_failed",
			"scaling_failed",
			"rollout_stuck",
		},
		RequiredData: []string{"namespace", "cluster"},
		OptionalData: []string{"deployment", "replicaset", "statefulset", "daemonset", "pod"},
		Dependencies: []Dependency{
			{
				Name:     "neo4j",
				Type:     "database",
				Required: true,
				HealthCheck: func(ctx context.Context) error {
					return neo4jDriver.VerifyConnectivity(ctx)
				},
			},
		},
		MaxEventAge:  24 * time.Hour,
		BatchSupport: false,
	}

	base := NewBaseCorrelator("ownership-correlator", "1.0.0", capabilities)

	return &OwnershipCorrelator{
		BaseCorrelator: base,
		neo4jDriver:    neo4jDriver,
		logger:         logger,
	}, nil
}

// Correlate analyzes ownership chain issues
func (o *OwnershipCorrelator) Correlate(ctx context.Context, event *domain.UnifiedEvent) (*aggregator.CorrelatorOutput, error) {
	startTime := time.Now()

	// Validate event can be processed
	if err := o.ValidateEvent(event); err != nil {
		return nil, err
	}

	o.logger.Debug("Processing ownership correlation",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("namespace", o.getNamespace(event)),
		zap.String("entity", o.getEntityName(event)))

	// Route to appropriate analysis
	var findings []aggregator.Finding
	var err error

	switch event.Type {
	case "deployment_failed", "rollout_stuck":
		findings, err = o.analyzeDeploymentChain(ctx, event)
	case "replicaset_failed", "scaling_failed":
		findings, err = o.analyzeReplicaSetIssues(ctx, event)
	case "pod_failed", "pod_deleted":
		findings, err = o.analyzePodOwnership(ctx, event)
	case "statefulset_failed":
		findings, err = o.analyzeStatefulSetChain(ctx, event)
	case "daemonset_failed":
		findings, err = o.analyzeDaemonSetIssues(ctx, event)
	default:
		findings = []aggregator.Finding{}
	}

	if err != nil {
		o.logger.Error("Ownership correlation failed",
			zap.String("event_id", event.ID),
			zap.Error(err))
		return nil, fmt.Errorf("ownership correlation failed: %w", err)
	}

	// Calculate overall confidence
	confidence := o.calculateConfidence(findings)

	// Build context
	context_map := map[string]string{
		"namespace":        o.getNamespace(event),
		"cluster":          o.getCluster(event),
		"correlation_type": "ownership",
		"event_type":       string(event.Type),
	}

	if entity := o.getEntityName(event); entity != "" {
		context_map["entity"] = entity
	}

	return &aggregator.CorrelatorOutput{
		CorrelatorName:    o.Name(),
		CorrelatorVersion: o.Version(),
		Findings:          findings,
		Context:           context_map,
		Confidence:        confidence,
		ProcessingTime:    time.Since(startTime),
		Timestamp:         time.Now(),
	}, nil
}

// analyzeDeploymentChain analyzes Deployment→ReplicaSet→Pod ownership chain
func (o *OwnershipCorrelator) analyzeDeploymentChain(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := o.getNamespace(event)
	deploymentName := o.getEntityName(event)

	if deploymentName == "" {
		return nil, fmt.Errorf("deployment name not found in event")
	}

	o.logger.Debug("Analyzing deployment ownership chain",
		zap.String("deployment", deploymentName),
		zap.String("namespace", namespace))

	session := o.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for deployment ownership chain
	query := `
		MATCH (d:Deployment {name: $deploymentName, namespace: $namespace})
		OPTIONAL MATCH (d)-[:OWNS]->(rs:ReplicaSet)
		OPTIONAL MATCH (rs)-[:OWNS]->(p:Pod)
		WITH d, rs, p,
		     d.replicas as desiredReplicas,
		     rs.replicas as rsReplicas,
		     rs.readyReplicas as rsReady,
		     p.ready as podReady,
		     p.phase as podPhase
		RETURN d,
		       collect(DISTINCT {
		           replicaSet: rs,
		           replicas: rsReplicas,
		           ready: rsReady,
		           pods: collect(DISTINCT {
		               pod: p,
		               ready: podReady,
		               phase: podPhase
		           })
		       }) as replicaSets
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"deploymentName": deploymentName,
		"namespace":      namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment chain: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Get deployment info
		deploymentValue, _ := record.Get("d")
		if deploymentNode, ok := deploymentValue.(neo4j.Node); ok {
			desiredReplicas, _ := deploymentNode.Props["replicas"].(int64)

			// Analyze replica sets
			if replicaSetsValue, found := record.Get("replicaSets"); found {
				if replicaSets, ok := replicaSetsValue.([]interface{}); ok {
					findings = append(findings, o.analyzeReplicaSets(deploymentName, desiredReplicas, replicaSets, event)...)
				}
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating deployment chain results: %w", err)
	}

	return findings, nil
}

// analyzeReplicaSetIssues analyzes ReplicaSet→Pod ownership issues
func (o *OwnershipCorrelator) analyzeReplicaSetIssues(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := o.getNamespace(event)
	rsName := o.getEntityName(event)

	if rsName == "" {
		return nil, fmt.Errorf("replicaset name not found in event")
	}

	session := o.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for ReplicaSet ownership
	query := `
		MATCH (rs:ReplicaSet {name: $rsName, namespace: $namespace})
		OPTIONAL MATCH (d:Deployment)-[:OWNS]->(rs)
		OPTIONAL MATCH (rs)-[:OWNS]->(p:Pod)
		WITH rs, d, 
		     rs.replicas as desiredReplicas,
		     rs.readyReplicas as readyReplicas,
		     collect(DISTINCT p) as pods
		RETURN rs, d, desiredReplicas, readyReplicas, pods
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"rsName":    rsName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query replicaset ownership: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		desiredValue, _ := record.Get("desiredReplicas")
		readyValue, _ := record.Get("readyReplicas")

		desired, _ := desiredValue.(int64)
		ready, _ := readyValue.(int64)

		if desired > 0 && ready < desired {
			// Get deployment name if exists
			var deploymentName string
			if deploymentValue, found := record.Get("d"); found {
				if deploymentNode, ok := deploymentValue.(neo4j.Node); ok {
					deploymentName, _ = deploymentNode.Props["name"].(string)
				}
			}

			// Analyze pods
			if podsValue, found := record.Get("pods"); found {
				if pods, ok := podsValue.([]interface{}); ok {
					findings = append(findings, o.createReplicaSetFindings(rsName, deploymentName, desired, ready, pods, event)...)
				}
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating replicaset results: %w", err)
	}

	return findings, nil
}

// analyzePodOwnership traces pod ownership up the chain
func (o *OwnershipCorrelator) analyzePodOwnership(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := o.getNamespace(event)
	podName := o.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	session := o.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for pod ownership chain
	query := `
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (rs:ReplicaSet)-[:OWNS]->(p)
		OPTIONAL MATCH (d:Deployment)-[:OWNS]->(rs)
		OPTIONAL MATCH (sts:StatefulSet)-[:OWNS]->(p)
		OPTIONAL MATCH (ds:DaemonSet)-[:OWNS]->(p)
		RETURN p, rs, d, sts, ds
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"podName":   podName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query pod ownership: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Build ownership chain finding
		var ownerChain []string
		var ownerType string
		var ownerId string

		// Check Deployment→ReplicaSet ownership
		if deploymentValue, found := record.Get("d"); found && deploymentValue != nil {
			if deploymentNode, ok := deploymentValue.(neo4j.Node); ok {
				deploymentName, _ := deploymentNode.Props["name"].(string)
				ownerChain = append(ownerChain, fmt.Sprintf("Deployment/%s", deploymentName))
				ownerType = "Deployment"
				ownerId = deploymentName
			}
		}

		if rsValue, found := record.Get("rs"); found && rsValue != nil {
			if rsNode, ok := rsValue.(neo4j.Node); ok {
				rsName, _ := rsNode.Props["name"].(string)
				ownerChain = append(ownerChain, fmt.Sprintf("ReplicaSet/%s", rsName))
				if ownerType == "" {
					ownerType = "ReplicaSet"
					ownerId = rsName
				}
			}
		}

		// Check StatefulSet ownership
		if stsValue, found := record.Get("sts"); found && stsValue != nil {
			if stsNode, ok := stsValue.(neo4j.Node); ok {
				stsName, _ := stsNode.Props["name"].(string)
				ownerChain = []string{fmt.Sprintf("StatefulSet/%s", stsName)}
				ownerType = "StatefulSet"
				ownerId = stsName
			}
		}

		// Check DaemonSet ownership
		if dsValue, found := record.Get("ds"); found && dsValue != nil {
			if dsNode, ok := dsValue.(neo4j.Node); ok {
				dsName, _ := dsNode.Props["name"].(string)
				ownerChain = []string{fmt.Sprintf("DaemonSet/%s", dsName)}
				ownerType = "DaemonSet"
				ownerId = dsName
			}
		}

		if len(ownerChain) > 0 {
			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("pod-ownership-%s", podName),
				Type:       "pod_ownership_chain",
				Severity:   aggregator.SeverityMedium,
				Confidence: 0.90,
				Message:    fmt.Sprintf("Pod %s failure traced to %s", podName, strings.Join(ownerChain, " → ")),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
					GraphPaths: []aggregator.GraphPath{{
						Nodes: o.buildOwnershipNodes(podName, ownerChain, namespace),
						Edges: o.buildOwnershipEdges(podName, ownerChain),
					}},
				},
				Impact: aggregator.Impact{
					Scope:       "ownership",
					Resources:   append(ownerChain, podName),
					UserImpact:  fmt.Sprintf("Pod controlled by %s %s", ownerType, ownerId),
					Degradation: "Pod failure affects controller",
				},
				Timestamp: time.Now(),
			})
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pod ownership results: %w", err)
	}

	return findings, nil
}

// analyzeStatefulSetChain analyzes StatefulSet→Pod ownership
func (o *OwnershipCorrelator) analyzeStatefulSetChain(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := o.getNamespace(event)
	stsName := o.getEntityName(event)

	if stsName == "" {
		return nil, fmt.Errorf("statefulset name not found in event")
	}

	session := o.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for StatefulSet pods
	query := `
		MATCH (sts:StatefulSet {name: $stsName, namespace: $namespace})
		OPTIONAL MATCH (sts)-[:OWNS]->(p:Pod)
		WITH sts,
		     sts.replicas as desiredReplicas,
		     sts.readyReplicas as readyReplicas,
		     collect(p) as pods
		RETURN sts, desiredReplicas, readyReplicas, pods
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"stsName":   stsName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query statefulset chain: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		desiredValue, _ := record.Get("desiredReplicas")
		readyValue, _ := record.Get("readyReplicas")

		desired, _ := desiredValue.(int64)
		ready, _ := readyValue.(int64)

		if desired > 0 && ready < desired {
			if podsValue, found := record.Get("pods"); found {
				if pods, ok := podsValue.([]interface{}); ok {
					// Check pod ordering issues (StatefulSets care about order)
					findings = append(findings, o.analyzeStatefulSetPods(stsName, desired, ready, pods, event)...)
				}
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating statefulset results: %w", err)
	}

	return findings, nil
}

// analyzeDaemonSetIssues analyzes DaemonSet→Pod issues
func (o *OwnershipCorrelator) analyzeDaemonSetIssues(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := o.getNamespace(event)
	dsName := o.getEntityName(event)

	if dsName == "" {
		return nil, fmt.Errorf("daemonset name not found in event")
	}

	session := o.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for DaemonSet pods across nodes
	query := `
		MATCH (ds:DaemonSet {name: $dsName, namespace: $namespace})
		OPTIONAL MATCH (ds)-[:OWNS]->(p:Pod)
		OPTIONAL MATCH (p)-[:RUNS_ON]->(n:Node)
		WITH ds,
		     count(DISTINCT n) as nodeCount,
		     count(DISTINCT p) as podCount,
		     collect(DISTINCT {pod: p, node: n.name}) as podNodes
		RETURN ds, nodeCount, podCount, podNodes
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"dsName":    dsName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query daemonset issues: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		nodeCountValue, _ := record.Get("nodeCount")
		podCountValue, _ := record.Get("podCount")

		nodeCount, _ := nodeCountValue.(int64)
		podCount, _ := podCountValue.(int64)

		// DaemonSet should have one pod per node
		if nodeCount > podCount {
			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("daemonset-missing-pods-%s", dsName),
				Type:       "daemonset_incomplete_coverage",
				Severity:   aggregator.SeverityHigh,
				Confidence: 0.95,
				Message:    fmt.Sprintf("DaemonSet %s has %d pods but %d nodes (missing %d)", dsName, podCount, nodeCount, nodeCount-podCount),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
				},
				Impact: aggregator.Impact{
					Scope:       "daemonset",
					Resources:   []string{dsName},
					UserImpact:  "Some nodes not running required daemon",
					Degradation: fmt.Sprintf("%d%% node coverage", (podCount*100)/nodeCount),
				},
				Timestamp: time.Now(),
			})
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating daemonset results: %w", err)
	}

	return findings, nil
}

// Helper methods

func (o *OwnershipCorrelator) analyzeReplicaSets(deploymentName string, desiredReplicas int64, replicaSets []interface{}, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	var totalPods int64
	var readyPods int64
	var failedPods []string

	for _, rsData := range replicaSets {
		if rsMap, ok := rsData.(map[string]interface{}); ok {
			if rsNode, ok := rsMap["replicaSet"].(neo4j.Node); ok && rsNode.Props != nil {
				rsName, _ := rsNode.Props["name"].(string)

				// Check if this is the active ReplicaSet
				if pods, ok := rsMap["pods"].([]interface{}); ok {
					for _, podData := range pods {
						if podMap, ok := podData.(map[string]interface{}); ok {
							if podNode, ok := podMap["pod"].(neo4j.Node); ok && podNode.Props != nil {
								totalPods++
								podName, _ := podNode.Props["name"].(string)

								if ready, ok := podMap["ready"].(bool); ok && ready {
									readyPods++
								} else {
									failedPods = append(failedPods, podName)
								}
							}
						}
					}
				}

				// Create finding if ReplicaSet has issues
				if rsReplicas, ok := rsMap["replicas"].(int64); ok {
					if rsReady, ok := rsMap["ready"].(int64); ok && rsReady < rsReplicas {
						findings = append(findings, aggregator.Finding{
							ID:         fmt.Sprintf("replicaset-degraded-%s", rsName),
							Type:       "replicaset_not_ready",
							Severity:   aggregator.SeverityHigh,
							Confidence: 0.85,
							Message:    fmt.Sprintf("ReplicaSet %s has %d/%d ready pods", rsName, rsReady, rsReplicas),
							Evidence: aggregator.Evidence{
								Events: []domain.UnifiedEvent{*event},
							},
							Impact: aggregator.Impact{
								Scope:       "replicaset",
								Resources:   []string{deploymentName, rsName},
								UserImpact:  "Deployment not at full capacity",
								Degradation: fmt.Sprintf("%d%% capacity", (rsReady*100)/rsReplicas),
							},
							Timestamp: time.Now(),
						})
					}
				}
			}
		}
	}

	// Create deployment-level finding
	if totalPods < desiredReplicas {
		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("deployment-underscaled-%s", deploymentName),
			Type:       "deployment_insufficient_pods",
			Severity:   aggregator.SeverityCritical,
			Confidence: 0.90,
			Message:    fmt.Sprintf("Deployment %s has %d pods but needs %d", deploymentName, totalPods, desiredReplicas),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: aggregator.Impact{
				Scope:       "deployment",
				Resources:   append([]string{deploymentName}, failedPods...),
				UserImpact:  "Service running below desired capacity",
				Degradation: fmt.Sprintf("%d%% of desired replicas", (totalPods*100)/desiredReplicas),
			},
			Timestamp: time.Now(),
		})
	}

	return findings
}

func (o *OwnershipCorrelator) createReplicaSetFindings(rsName, deploymentName string, desired, ready int64, pods []interface{}, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	notReadyPods := []string{}

	for _, podInterface := range pods {
		if pod, ok := podInterface.(neo4j.Node); ok {
			podName, _ := pod.Props["name"].(string)
			if podReady, exists := pod.Props["ready"]; exists {
				if !podReady.(bool) {
					notReadyPods = append(notReadyPods, podName)
				}
			}
		}
	}

	message := fmt.Sprintf("ReplicaSet %s has %d/%d ready pods", rsName, ready, desired)
	if deploymentName != "" {
		message = fmt.Sprintf("ReplicaSet %s (owned by Deployment %s) has %d/%d ready pods", rsName, deploymentName, ready, desired)
	}

	findings = append(findings, aggregator.Finding{
		ID:         fmt.Sprintf("replicaset-pods-not-ready-%s", rsName),
		Type:       "replicaset_degraded",
		Severity:   aggregator.SeverityHigh,
		Confidence: 0.85,
		Message:    message,
		Evidence: aggregator.Evidence{
			Events: []domain.UnifiedEvent{*event},
			Attributes: map[string]interface{}{
				"not_ready_pods": notReadyPods,
			},
		},
		Impact: aggregator.Impact{
			Scope:       "replicaset",
			Resources:   append([]string{rsName}, notReadyPods...),
			UserImpact:  "Service capacity reduced",
			Degradation: fmt.Sprintf("%d%% capacity available", (ready*100)/desired),
		},
		Timestamp: time.Now(),
	})

	return findings
}

func (o *OwnershipCorrelator) analyzeStatefulSetPods(stsName string, desired, ready int64, pods []interface{}, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	var brokenOrdinal int64 = -1

	// StatefulSets have ordered pod names: name-0, name-1, etc.
	// Find the first broken pod in the sequence
	for i := int64(0); i < desired; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		found := false

		for _, podInterface := range pods {
			if pod, ok := podInterface.(neo4j.Node); ok {
				if name, exists := pod.Props["name"]; exists && name == podName {
					found = true
					if podReady, exists := pod.Props["ready"]; exists && !podReady.(bool) {
						brokenOrdinal = i
						break
					}
				}
			}
		}

		if !found || brokenOrdinal >= 0 {
			brokenOrdinal = i
			break
		}
	}

	if brokenOrdinal >= 0 {
		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("statefulset-broken-ordinal-%s", stsName),
			Type:       "statefulset_pod_sequence_broken",
			Severity:   aggregator.SeverityCritical,
			Confidence: 0.90,
			Message:    fmt.Sprintf("StatefulSet %s pod sequence broken at ordinal %d", stsName, brokenOrdinal),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
				Attributes: map[string]interface{}{
					"broken_ordinal": brokenOrdinal,
					"pod_name":       fmt.Sprintf("%s-%d", stsName, brokenOrdinal),
				},
			},
			Impact: aggregator.Impact{
				Scope:       "statefulset",
				Resources:   []string{stsName, fmt.Sprintf("%s-%d", stsName, brokenOrdinal)},
				UserImpact:  "StatefulSet cannot progress past broken pod",
				Degradation: fmt.Sprintf("Stuck at %d/%d pods", brokenOrdinal, desired),
			},
			Timestamp: time.Now(),
		})
	}

	return findings
}

func (o *OwnershipCorrelator) buildOwnershipNodes(podName string, ownerChain []string, namespace string) []aggregator.GraphNode {
	nodes := []aggregator.GraphNode{}

	// Add pod node
	nodes = append(nodes, aggregator.GraphNode{
		ID:   podName,
		Type: "Pod",
		Labels: map[string]string{
			"name":      podName,
			"namespace": namespace,
		},
	})

	// Add owner nodes
	for _, owner := range ownerChain {
		parts := strings.Split(owner, "/")
		if len(parts) == 2 {
			nodes = append(nodes, aggregator.GraphNode{
				ID:   parts[1],
				Type: parts[0],
				Labels: map[string]string{
					"name":      parts[1],
					"namespace": namespace,
				},
			})
		}
	}

	return nodes
}

func (o *OwnershipCorrelator) buildOwnershipEdges(podName string, ownerChain []string) []aggregator.GraphEdge {
	edges := []aggregator.GraphEdge{}

	// Build edges in reverse order (owner → owned)
	if len(ownerChain) >= 2 {
		// Deployment → ReplicaSet
		deploymentParts := strings.Split(ownerChain[0], "/")
		rsParts := strings.Split(ownerChain[1], "/")

		if len(deploymentParts) == 2 && len(rsParts) == 2 {
			edges = append(edges, aggregator.GraphEdge{
				From:         deploymentParts[1],
				To:           rsParts[1],
				Relationship: "OWNS",
				Properties:   map[string]string{"type": "deployment_replicaset"},
			})

			// ReplicaSet → Pod
			edges = append(edges, aggregator.GraphEdge{
				From:         rsParts[1],
				To:           podName,
				Relationship: "OWNS",
				Properties:   map[string]string{"type": "replicaset_pod"},
			})
		}
	} else if len(ownerChain) == 1 {
		// Direct ownership (StatefulSet/DaemonSet → Pod)
		ownerParts := strings.Split(ownerChain[0], "/")
		if len(ownerParts) == 2 {
			edges = append(edges, aggregator.GraphEdge{
				From:         ownerParts[1],
				To:           podName,
				Relationship: "OWNS",
				Properties:   map[string]string{"type": strings.ToLower(ownerParts[0]) + "_pod"},
			})
		}
	}

	return edges
}

// calculateConfidence calculates overall confidence for findings
func (o *OwnershipCorrelator) calculateConfidence(findings []aggregator.Finding) float64 {
	if len(findings) == 0 {
		return 0.0
	}

	totalWeight := 0.0
	weightedSum := 0.0

	for _, finding := range findings {
		var weight float64
		switch finding.Severity {
		case aggregator.SeverityCritical:
			weight = 1.0
		case aggregator.SeverityHigh:
			weight = 0.8
		case aggregator.SeverityMedium:
			weight = 0.6
		case aggregator.SeverityLow:
			weight = 0.4
		default:
			weight = 0.2
		}

		weightedSum += finding.Confidence * weight
		totalWeight += weight
	}

	confidence := weightedSum / totalWeight

	// Boost if multiple findings in ownership chain
	if len(findings) > 1 {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// Health checks if the correlator is healthy
func (o *OwnershipCorrelator) Health(ctx context.Context) error {
	return o.neo4jDriver.VerifyConnectivity(ctx)
}

// SetGraphClient implements GraphCorrelator interface
func (o *OwnershipCorrelator) SetGraphClient(client interface{}) {
	if driver, ok := client.(neo4j.DriverWithContext); ok {
		o.neo4jDriver = driver
	}
}

// PreloadGraph implements GraphCorrelator interface
func (o *OwnershipCorrelator) PreloadGraph(ctx context.Context) error {
	// No preloading needed for now
	return nil
}

// Helper functions
func (o *OwnershipCorrelator) getNamespace(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.Namespace != "" {
		return event.K8sContext.Namespace
	}
	if event.Entity != nil && event.Entity.Namespace != "" {
		return event.Entity.Namespace
	}
	return "default"
}

func (o *OwnershipCorrelator) getCluster(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.ClusterName != "" {
		return event.K8sContext.ClusterName
	}
	return "unknown"
}

func (o *OwnershipCorrelator) getEntityName(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.Name != "" {
		return event.K8sContext.Name
	}
	if event.Entity != nil && event.Entity.Name != "" {
		return event.Entity.Name
	}
	return ""
}
