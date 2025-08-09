package correlation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
	"go.uber.org/zap"
)

// ReplicaSetData represents structured data for a ReplicaSet and its pods
type ReplicaSetData struct {
	ReplicaSet    GraphNode
	Replicas      int64
	ReadyReplicas int64
	Pods          []PodData
}

// PodData represents structured data for a Pod
type PodData struct {
	Pod   GraphNode
	Name  string
	Ready bool
	Phase string
}

// DaemonSetPodData represents a DaemonSet pod and its node
type DaemonSetPodData struct {
	Pod      GraphNode
	NodeName string
}

// OwnershipCorrelator analyzes K8s ownership chains to find root causes
// It tracks: Deployment→ReplicaSet→Pod, StatefulSet→Pod, DaemonSet→Pod chains
type OwnershipCorrelator struct {
	*BaseCorrelator
	graphStore  GraphStore
	logger      *zap.Logger
	queryConfig QueryConfig
}

// NewOwnershipCorrelator creates a new ownership correlator
func NewOwnershipCorrelator(graphStore GraphStore, logger *zap.Logger) (*OwnershipCorrelator, error) {
	if graphStore == nil {
		return nil, fmt.Errorf("graphStore is required")
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
					return graphStore.HealthCheck(ctx)
				},
			},
		},
		MaxEventAge:  24 * time.Hour,
		BatchSupport: false,
	}

	base := NewBaseCorrelator("ownership-correlator", DefaultCorrelatorVersion, capabilities)

	return &OwnershipCorrelator{
		BaseCorrelator: base,
		graphStore:     graphStore,
		logger:         logger,
		queryConfig:    DefaultQueryConfig(),
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

	// Query for deployment ownership chain
	limit := o.queryConfig.GetLimit("ownership")
	query := fmt.Sprintf(`
		MATCH (d:Deployment {name: $deploymentName, namespace: $namespace})
		OPTIONAL MATCH (d)-[:OWNS]->(rs:ReplicaSet)
		OPTIONAL MATCH (rs)-[:OWNS]->(p:Pod)
		WITH d, rs, p,
		     d.replicas as desiredReplicas,
		     rs.replicas as rsReplicas,
		     rs.readyReplicas as rsReady,
		     p.ready as podReady,
		     p.phase as podPhase
		LIMIT %d
		RETURN d,
		       collect(DISTINCT {
		           replicaSet: rs,
		           replicas: rsReplicas,
		           ready: rsReady,
		           pods: collect(DISTINCT {
		               pod: p,
		               ready: podReady,
		               phase: podPhase
		           })[0..%d]
		       })[0..%d] as replicaSets`, limit*2, limit, limit)

	params := &DeploymentQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		DeploymentName: deploymentName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := o.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment chain: %w", err)
	}
	defer result.Close(ctx)

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Get deployment info
		deploymentNode, err := record.GetNode("d")
		if err == nil {
			desiredReplicas := int64(0)
			if replicasStr, ok := deploymentNode.Properties.Metadata["replicas"]; ok {
				// Parse replicas from metadata
				if replicas, err := strconv.ParseInt(replicasStr, 10, 64); err == nil {
					desiredReplicas = replicas
				}
			}

			// Analyze replica sets
			if replicaSetsValue, found := record.Get("replicaSets"); found {
				// Parse replica sets into typed structure
				replicaSets := o.parseReplicaSetsFromValue(replicaSetsValue)
				if len(replicaSets) > 0 {
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

	// Query for ReplicaSet ownership
	limit := o.queryConfig.GetLimit("ownership")
	query := fmt.Sprintf(`
		MATCH (rs:ReplicaSet {name: $rsName, namespace: $namespace})
		OPTIONAL MATCH (d:Deployment)-[:OWNS]->(rs)
		OPTIONAL MATCH (rs)-[:OWNS]->(p:Pod)
		WITH rs, d, 
		     rs.replicas as desiredReplicas,
		     rs.readyReplicas as readyReplicas,
		     collect(DISTINCT p)[0..%d] as pods
		RETURN rs, d, desiredReplicas, readyReplicas, pods
		LIMIT 1
	`, limit)

	params := &ReplicaSetQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		ReplicaSetName: rsName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := o.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query replicaset ownership: %w", err)
	}
	defer result.Close(ctx)

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
			if deploymentNode, err := record.GetNode("d"); err == nil && deploymentNode != nil {
				deploymentName = deploymentNode.Properties.Name
			}

			// Analyze pods
			if podsValue, found := record.Get("pods"); found {
				pods := o.parsePodsFromValue(podsValue)
				if len(pods) > 0 {
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

	// Query for pod ownership chain
	query := `
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (rs:ReplicaSet)-[:OWNS]->(p)
		OPTIONAL MATCH (d:Deployment)-[:OWNS]->(rs)
		OPTIONAL MATCH (sts:StatefulSet)-[:OWNS]->(p)
		OPTIONAL MATCH (ds:DaemonSet)-[:OWNS]->(p)
		RETURN p, rs, d, sts, ds
		LIMIT 1
	`

	params := &PodQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		PodName: podName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := o.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query pod ownership: %w", err)
	}
	defer result.Close(ctx)

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Build ownership chain finding
		var ownerChain []string
		var ownerType string
		var ownerId string

		// Check Deployment→ReplicaSet ownership
		if deploymentNode, err := record.GetNode("d"); err == nil && deploymentNode != nil {
			deploymentName := deploymentNode.Properties.Name
			ownerChain = append(ownerChain, fmt.Sprintf("Deployment/%s", deploymentName))
			ownerType = "Deployment"
			ownerId = deploymentName
		}

		if rsNode, err := record.GetNode("rs"); err == nil && rsNode != nil {
			rsName := rsNode.Properties.Name
			if rsName != "" {
				ownerChain = append(ownerChain, fmt.Sprintf("ReplicaSet/%s", rsName))
				if ownerType == "" {
					ownerType = "ReplicaSet"
					ownerId = rsName
				}
			}
		}

		// Check StatefulSet ownership
		if stsNode, err := record.GetNode("sts"); err == nil && stsNode != nil {
			stsName := stsNode.Properties.Name
			ownerChain = []string{fmt.Sprintf("StatefulSet/%s", stsName)}
			ownerType = "StatefulSet"
			ownerId = stsName
		}

		// Check DaemonSet ownership
		if dsNode, err := record.GetNode("ds"); err == nil && dsNode != nil {
			dsName := dsNode.Properties.Name
			if dsName != "" {
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
				Confidence: HighConfidence,
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

	// Query for StatefulSet pods
	limit := o.queryConfig.GetLimit("ownership")
	query := fmt.Sprintf(`
		MATCH (sts:StatefulSet {name: $stsName, namespace: $namespace})
		OPTIONAL MATCH (sts)-[:OWNS]->(p:Pod)
		WITH sts,
		     sts.replicas as desiredReplicas,
		     sts.readyReplicas as readyReplicas,
		     collect(p)[0..%d] as pods
		RETURN sts, desiredReplicas, readyReplicas, pods
		LIMIT 1
	`, limit)

	params := &StatefulSetQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		StatefulSetName: stsName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := o.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query statefulset chain: %w", err)
	}
	defer result.Close(ctx)

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		desiredValue, _ := record.Get("desiredReplicas")
		readyValue, _ := record.Get("readyReplicas")

		desired, _ := desiredValue.(int64)
		ready, _ := readyValue.(int64)

		if desired > 0 && ready < desired {
			if podsValue, found := record.Get("pods"); found {
				pods := o.parsePodsFromValue(podsValue)
				if len(pods) > 0 {
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

	// Query for DaemonSet pods across nodes
	limit := o.queryConfig.GetLimit("ownership")
	query := fmt.Sprintf(`
		MATCH (ds:DaemonSet {name: $dsName, namespace: $namespace})
		OPTIONAL MATCH (ds)-[:OWNS]->(p:Pod)
		OPTIONAL MATCH (p)-[:RUNS_ON]->(n:Node)
		WITH ds,
		     count(DISTINCT n) as nodeCount,
		     count(DISTINCT p) as podCount,
		     collect(DISTINCT {pod: p, node: n.name})[0..%d] as podNodes
		RETURN ds, nodeCount, podCount, podNodes
		LIMIT 1
	`, limit)

	params := &DaemonSetQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		DaemonSetName: dsName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := o.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query daemonset issues: %w", err)
	}
	defer result.Close(ctx)

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
				Confidence: CriticalConfidence,
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

func (o *OwnershipCorrelator) analyzeReplicaSets(deploymentName string, desiredReplicas int64, replicaSets []ReplicaSetData, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	var totalPods int64
	var readyPods int64
	var failedPods []string

	for _, rsData := range replicaSets {
		rsName := rsData.ReplicaSet.Properties.Name
		totalPods += int64(len(rsData.Pods))

		for _, pod := range rsData.Pods {
			if pod.Ready {
				readyPods++
			} else {
				failedPods = append(failedPods, pod.Name)
			}
		}

		// Create finding if ReplicaSet has issues
		if rsData.ReadyReplicas < rsData.Replicas {
			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("replicaset-degraded-%s", rsName),
				Type:       "replicaset_not_ready",
				Severity:   aggregator.SeverityHigh,
				Confidence: MediumHighConfidence,
				Message:    fmt.Sprintf("ReplicaSet %s has %d/%d ready pods", rsName, rsData.ReadyReplicas, rsData.Replicas),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
				},
				Impact: aggregator.Impact{
					Scope:       "replicaset",
					Resources:   []string{deploymentName, rsName},
					UserImpact:  "Deployment not at full capacity",
					Degradation: fmt.Sprintf("%d%% capacity", (rsData.ReadyReplicas*100)/rsData.Replicas),
				},
				Timestamp: time.Now(),
			})
		}
	}

	// Create deployment-level finding
	if totalPods < desiredReplicas {
		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("deployment-underscaled-%s", deploymentName),
			Type:       "deployment_insufficient_pods",
			Severity:   aggregator.SeverityCritical,
			Confidence: HighConfidence,
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

func (o *OwnershipCorrelator) createReplicaSetFindings(rsName, deploymentName string, desired, ready int64, pods []PodData, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	notReadyPods := []string{}

	for _, pod := range pods {
		if !pod.Ready {
			notReadyPods = append(notReadyPods, pod.Name)
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
		Confidence: MediumHighConfidence,
		Message:    message,
		Evidence: aggregator.Evidence{
			Events: []domain.UnifiedEvent{*event},
			Attributes: map[string]string{
				"not_ready_pods": strings.Join(notReadyPods, ","),
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

func (o *OwnershipCorrelator) analyzeStatefulSetPods(stsName string, desired, ready int64, pods []PodData, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	var brokenOrdinal int64 = -1

	// StatefulSets have ordered pod names: name-0, name-1, etc.
	// Find the first broken pod in the sequence
	for i := int64(0); i < desired; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		found := false

		for _, pod := range pods {
			if pod.Name == podName {
				found = true
				if !pod.Ready {
					brokenOrdinal = i
					break
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
			Confidence: HighConfidence,
			Message:    fmt.Sprintf("StatefulSet %s pod sequence broken at ordinal %d", stsName, brokenOrdinal),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
				Attributes: map[string]string{
					"broken_ordinal": fmt.Sprintf("%d", brokenOrdinal),
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
		return 0
	}

	totalWeight := 0.0
	weightedSum := 0.0

	for _, finding := range findings {
		var weight float64
		switch finding.Severity {
		case aggregator.SeverityCritical:
			weight = 1.0
		case aggregator.SeverityHigh:
			weight = HighWeight
		case aggregator.SeverityMedium:
			weight = MediumWeight
		case aggregator.SeverityLow:
			weight = LowWeight
		default:
			weight = VeryLowWeight
		}

		weightedSum += finding.Confidence * weight
		totalWeight += weight
	}

	confidence := weightedSum / totalWeight

	// Boost if multiple findings in ownership chain
	if len(findings) > 1 {
		confidence += BoostMultiplier
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// Health checks if the correlator is healthy
func (o *OwnershipCorrelator) Health(ctx context.Context) error {
	return o.graphStore.HealthCheck(ctx)
}

// SetGraphClient implements GraphCorrelator interface
func (o *OwnershipCorrelator) SetGraphClient(client interface{}) {
	// This method is no longer needed as we use GraphStore interface
	// The graphStore is injected via constructor
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

// parseReplicaSetsFromValue safely parses replica sets from a query result value
func (o *OwnershipCorrelator) parseReplicaSetsFromValue(value interface{}) []ReplicaSetData {
	var result []ReplicaSetData

	// Value should be a slice
	slice, ok := value.([]interface{})
	if !ok {
		o.logger.Warn("Failed to parse replica sets: not a slice")
		return result
	}

	for _, item := range slice {
		rsMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		rsData := ReplicaSetData{}

		// Parse replicaSet node
		if rsNode, ok := rsMap["replicaSet"].(map[string]interface{}); ok {
			if props, ok := rsNode["properties"].(map[string]interface{}); ok {
				rsData.ReplicaSet.Properties = parseNodeProperties(props)
			}
		}

		// Parse replicas and readyReplicas
		if replicas, ok := rsMap["replicas"].(int64); ok {
			rsData.Replicas = replicas
		}
		if ready, ok := rsMap["ready"].(int64); ok {
			rsData.ReadyReplicas = ready
		}

		// Parse pods
		if pods, ok := rsMap["pods"].([]interface{}); ok {
			rsData.Pods = o.parsePodsFromValue(pods)
		}

		result = append(result, rsData)
	}

	return result
}

// parsePodsFromValue safely parses pods from a query result value
func (o *OwnershipCorrelator) parsePodsFromValue(value interface{}) []PodData {
	var result []PodData

	slice, ok := value.([]interface{})
	if !ok {
		return result
	}

	for _, item := range slice {
		podMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		podData := PodData{}

		// Parse pod node
		if podNode, ok := podMap["pod"].(map[string]interface{}); ok {
			if props, ok := podNode["properties"].(map[string]interface{}); ok {
				podData.Pod.Properties = parseNodeProperties(props)
				podData.Name = podData.Pod.Properties.Name
			}
		}

		// Parse ready and phase
		if ready, ok := podMap["ready"].(bool); ok {
			podData.Ready = ready
		}
		if phase, ok := podMap["phase"].(string); ok {
			podData.Phase = phase
		}

		result = append(result, podData)
	}

	return result
}
