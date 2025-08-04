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

// DependencyCorrelator analyzes K8s dependency relationships to find root causes
// It handles: Service→Pod, Pod→ConfigMap/Secret, Pod→PVC relationships
type DependencyCorrelator struct {
	*BaseCorrelator
	neo4jDriver neo4j.DriverWithContext
	logger      *zap.Logger
}

// NewDependencyCorrelator creates a new dependency correlator
func NewDependencyCorrelator(neo4jDriver neo4j.DriverWithContext, logger *zap.Logger) (*DependencyCorrelator, error) {
	if neo4jDriver == nil {
		return nil, fmt.Errorf("neo4jDriver is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	capabilities := CorrelatorCapabilities{
		EventTypes: []string{
			"pod_failed",
			"service_unavailable",
			"config_changed",
			"volume_mount_failed",
			"endpoint_not_ready",
			"container_crash",
		},
		RequiredData: []string{"namespace", "cluster"},
		OptionalData: []string{"pod", "service", "configmap", "secret", "pvc"},
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

	base := NewBaseCorrelator("dependency-correlator", "1.0.0", capabilities)

	return &DependencyCorrelator{
		BaseCorrelator: base,
		neo4jDriver:    neo4jDriver,
		logger:         logger,
	}, nil
}

// Correlate processes an event and finds dependency-related root causes
func (d *DependencyCorrelator) Correlate(ctx context.Context, event *domain.UnifiedEvent) (*aggregator.CorrelatorOutput, error) {
	startTime := time.Now()

	// Validate event can be processed
	if err := d.ValidateEvent(event); err != nil {
		return nil, err
	}

	d.logger.Debug("Processing dependency correlation",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("namespace", d.getNamespace(event)),
		zap.String("entity", d.getEntityName(event)))

	// Route to appropriate correlation handler
	var findings []aggregator.Finding
	var err error

	switch event.Type {
	case "service_unavailable", "endpoint_not_ready":
		findings, err = d.correlateServiceIssues(ctx, event)
	case "pod_failed", "container_crash":
		findings, err = d.correlatePodIssues(ctx, event)
	case "config_changed":
		findings, err = d.correlateConfigImpact(ctx, event)
	case "volume_mount_failed":
		findings, err = d.correlateVolumeIssues(ctx, event)
	default:
		// Generic dependency analysis
		findings, err = d.correlateGenericDependencies(ctx, event)
	}

	if err != nil {
		d.logger.Error("Dependency correlation failed",
			zap.String("event_id", event.ID),
			zap.Error(err))
		return nil, fmt.Errorf("dependency correlation failed: %w", err)
	}

	// Calculate overall confidence
	confidence := d.calculateConfidence(findings, event)

	// Build context
	context_map := map[string]string{
		"namespace":        d.getNamespace(event),
		"cluster":          d.getCluster(event),
		"correlation_type": "dependency",
		"event_type":       string(event.Type),
	}

	if entity := d.getEntityName(event); entity != "" {
		context_map["entity"] = entity
	}

	return &aggregator.CorrelatorOutput{
		CorrelatorName:    d.Name(),
		CorrelatorVersion: d.Version(),
		Findings:          findings,
		Context:           context_map,
		Confidence:        confidence,
		ProcessingTime:    time.Since(startTime),
		Timestamp:         time.Now(),
	}, nil
}

// correlateServiceIssues analyzes service availability problems
func (d *DependencyCorrelator) correlateServiceIssues(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := d.getNamespace(event)
	serviceName := d.getEntityName(event)

	if serviceName == "" {
		return nil, fmt.Errorf("service name not found in event")
	}

	d.logger.Debug("Correlating service issues",
		zap.String("service", serviceName),
		zap.String("namespace", namespace))

	session := d.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for service and its pod dependencies
	query := `
		MATCH (s:Service {name: $serviceName, namespace: $namespace})
		OPTIONAL MATCH (s)-[:SELECTS]->(p:Pod)
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		OPTIONAL MATCH (p)-[:USES_SECRET]->(sec:Secret)
		OPTIONAL MATCH (p)-[:CLAIMS]->(pvc:PVC)
		RETURN s, 
		       collect(DISTINCT p) as pods,
		       collect(DISTINCT cm) as configmaps,
		       collect(DISTINCT sec) as secrets,
		       collect(DISTINCT pvc) as pvcs
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"serviceName": serviceName,
		"namespace":   namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query service dependencies: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Check if service exists
		serviceNode, found := record.Get("s")
		if !found || serviceNode == nil {
			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("service-not-found-%s", serviceName),
				Type:       "service_not_found",
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.95,
				Message:    fmt.Sprintf("Service %s not found in namespace %s", serviceName, namespace),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
				},
				Impact: aggregator.Impact{
					Scope:       "service",
					Resources:   []string{serviceName},
					UserImpact:  "Service completely unavailable",
					Degradation: "100% - service does not exist",
				},
				Timestamp: time.Now(),
			})
			continue
		}

		// Check pod availability
		podsInterface, _ := record.Get("pods")
		pods, ok := podsInterface.([]interface{})
		if !ok || len(pods) == 0 {
			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("service-no-pods-%s", serviceName),
				Type:       "service_no_endpoints",
				Severity:   aggregator.SeverityCritical,
				Confidence: 0.90,
				Message:    fmt.Sprintf("Service %s has no running pods", serviceName),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
					GraphPaths: []aggregator.GraphPath{{
						Nodes: []aggregator.GraphNode{{
							ID:   serviceName,
							Type: "Service",
							Labels: map[string]string{
								"name":      serviceName,
								"namespace": namespace,
							},
						}},
					}},
				},
				Impact: aggregator.Impact{
					Scope:       "service",
					Resources:   []string{serviceName},
					UserImpact:  "Service has no endpoints",
					Degradation: "100% - no pods available",
				},
				Timestamp: time.Now(),
			})
		} else {
			// Analyze pod health
			readyPods := 0
			failedPods := 0

			for _, podInterface := range pods {
				if pod, ok := podInterface.(neo4j.Node); ok {
					props := pod.Props
					if ready, exists := props["ready"]; exists {
						if ready == true {
							readyPods++
						} else {
							failedPods++
						}
					}
				}
			}

			if readyPods == 0 && failedPods > 0 {
				findings = append(findings, aggregator.Finding{
					ID:         fmt.Sprintf("service-pods-failed-%s", serviceName),
					Type:       "service_endpoints_failed",
					Severity:   aggregator.SeverityCritical,
					Confidence: 0.85,
					Message:    fmt.Sprintf("Service %s has %d failed pods, 0 ready", serviceName, failedPods),
					Evidence: aggregator.Evidence{
						Events: []domain.UnifiedEvent{*event},
					},
					Impact: aggregator.Impact{
						Scope:       "service",
						Resources:   []string{serviceName},
						UserImpact:  "Service unavailable due to pod failures",
						Degradation: "100% - all pods failed",
					},
					Timestamp: time.Now(),
				})
			} else if readyPods < len(pods)/2 {
				findings = append(findings, aggregator.Finding{
					ID:         fmt.Sprintf("service-pods-degraded-%s", serviceName),
					Type:       "service_endpoints_degraded",
					Severity:   aggregator.SeverityHigh,
					Confidence: 0.75,
					Message:    fmt.Sprintf("Service %s has only %d/%d pods ready", serviceName, readyPods, len(pods)),
					Evidence: aggregator.Evidence{
						Events: []domain.UnifiedEvent{*event},
					},
					Impact: aggregator.Impact{
						Scope:       "service",
						Resources:   []string{serviceName},
						UserImpact:  "Service degraded due to pod failures",
						Degradation: fmt.Sprintf("%d%% - reduced capacity", (readyPods*100)/len(pods)),
					},
					Timestamp: time.Now(),
				})
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating service dependency results: %w", err)
	}

	return findings, nil
}

// correlatePodIssues analyzes pod failure dependencies
func (d *DependencyCorrelator) correlatePodIssues(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := d.getNamespace(event)
	podName := d.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	d.logger.Debug("Correlating pod issues",
		zap.String("pod", podName),
		zap.String("namespace", namespace))

	session := d.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for pod and its dependencies
	query := `
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		OPTIONAL MATCH (p)-[:USES_SECRET]->(sec:Secret)
		OPTIONAL MATCH (p)-[:CLAIMS]->(pvc:PVC)
		OPTIONAL MATCH (svc:Service)-[:SELECTS]->(p)
		RETURN p, 
		       collect(DISTINCT cm) as configmaps,
		       collect(DISTINCT sec) as secrets,
		       collect(DISTINCT pvc) as pvcs,
		       collect(DISTINCT svc) as services
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"podName":   podName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query pod dependencies: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Get pod info
		podNode, found := record.Get("p")
		if !found || podNode == nil {
			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("pod-not-found-%s", podName),
				Type:       "pod_not_found",
				Severity:   aggregator.SeverityHigh,
				Confidence: 0.95,
				Message:    fmt.Sprintf("Pod %s not found in namespace %s", podName, namespace),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
				},
				Impact: aggregator.Impact{
					Scope:       "pod",
					Resources:   []string{podName},
					UserImpact:  "Pod unavailable",
					Degradation: "100% - pod does not exist",
				},
				Timestamp: time.Now(),
			})
			continue
		}

		// Check ConfigMap dependencies
		configmapsInterface, _ := record.Get("configmaps")
		if configmaps, ok := configmapsInterface.([]interface{}); ok && len(configmaps) > 0 {
			for _, cmInterface := range configmaps {
				if cm, ok := cmInterface.(neo4j.Node); ok {
					props := cm.Props
					if modified, exists := props["lastModified"]; exists {
						if modTime, ok := modified.(time.Time); ok {
							// Check if ConfigMap was modified recently before pod failure
							if event.Timestamp.Sub(modTime) < 30*time.Minute && event.Timestamp.After(modTime) {
								cmName := props["name"].(string)
								findings = append(findings, aggregator.Finding{
									ID:         fmt.Sprintf("pod-config-dependency-%s-%s", podName, cmName),
									Type:       "pod_config_dependency_failure",
									Severity:   aggregator.SeverityHigh,
									Confidence: 0.80,
									Message:    fmt.Sprintf("Pod %s failed after ConfigMap %s was modified", podName, cmName),
									Evidence: aggregator.Evidence{
										Events: []domain.UnifiedEvent{*event},
										GraphPaths: []aggregator.GraphPath{{
											Nodes: []aggregator.GraphNode{
												{
													ID:     podName,
													Type:   "Pod",
													Labels: map[string]string{"name": podName, "namespace": namespace},
												},
												{
													ID:     cmName,
													Type:   "ConfigMap",
													Labels: map[string]string{"name": cmName, "namespace": namespace},
												},
											},
											Edges: []aggregator.GraphEdge{{
												From:         podName,
												To:           cmName,
												Relationship: "MOUNTS",
												Properties:   map[string]string{"type": "configmap"},
											}},
										}},
									},
									Impact: aggregator.Impact{
										Scope:       "pod",
										Resources:   []string{podName, cmName},
										UserImpact:  "Pod failed due to configuration change",
										Degradation: "100% - pod crash",
									},
									Timestamp: time.Now(),
								})
							}
						}
					}
				}
			}
		}

		// Check Service impact
		servicesInterface, _ := record.Get("services")
		if services, ok := servicesInterface.([]interface{}); ok && len(services) > 0 {
			var serviceNames []string
			for _, svcInterface := range services {
				if svc, ok := svcInterface.(neo4j.Node); ok {
					if name, exists := svc.Props["name"]; exists {
						serviceNames = append(serviceNames, name.(string))
					}
				}
			}

			if len(serviceNames) > 0 {
				findings = append(findings, aggregator.Finding{
					ID:         fmt.Sprintf("pod-service-impact-%s", podName),
					Type:       "pod_failure_service_impact",
					Severity:   aggregator.SeverityMedium,
					Confidence: 0.70,
					Message:    fmt.Sprintf("Pod %s failure impacts services: %s", podName, strings.Join(serviceNames, ", ")),
					Evidence: aggregator.Evidence{
						Events: []domain.UnifiedEvent{*event},
					},
					Impact: aggregator.Impact{
						Scope:       "service",
						Resources:   append([]string{podName}, serviceNames...),
						UserImpact:  "Service availability reduced",
						Degradation: "Partial - one endpoint lost",
					},
					Timestamp: time.Now(),
				})
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pod dependency results: %w", err)
	}

	return findings, nil
}

// correlateConfigImpact analyzes configuration change impacts
func (d *DependencyCorrelator) correlateConfigImpact(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := d.getNamespace(event)
	configName := d.getEntityName(event)

	if configName == "" {
		return nil, fmt.Errorf("config name not found in event")
	}

	d.logger.Debug("Correlating config impact",
		zap.String("config", configName),
		zap.String("namespace", namespace))

	session := d.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for config and dependent pods
	query := `
		MATCH (cm:ConfigMap {name: $configName, namespace: $namespace})
		OPTIONAL MATCH (p:Pod)-[:MOUNTS]->(cm)
		OPTIONAL MATCH (svc:Service)-[:SELECTS]->(p)
		RETURN cm,
		       collect(DISTINCT p) as pods,
		       collect(DISTINCT svc) as services
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"configName": configName,
		"namespace":  namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query config dependencies: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Get dependent pods
		podsInterface, _ := record.Get("pods")
		if pods, ok := podsInterface.([]interface{}); ok && len(pods) > 0 {
			var podNames []string
			affectedPods := 0

			for _, podInterface := range pods {
				if pod, ok := podInterface.(neo4j.Node); ok {
					if name, exists := pod.Props["name"]; exists {
						podName := name.(string)
						podNames = append(podNames, podName)

						// Check if pod restarted after config change
						if lastRestart, exists := pod.Props["lastRestart"]; exists {
							if restartTime, ok := lastRestart.(time.Time); ok {
								if restartTime.After(event.Timestamp) && restartTime.Sub(event.Timestamp) < 10*time.Minute {
									affectedPods++
								}
							}
						}
					}
				}
			}

			if affectedPods > 0 {
				findings = append(findings, aggregator.Finding{
					ID:         fmt.Sprintf("config-change-impact-%s", configName),
					Type:       "config_change_pod_impact",
					Severity:   aggregator.SeverityHigh,
					Confidence: 0.85,
					Message:    fmt.Sprintf("ConfigMap %s change caused %d pods to restart: %s", configName, affectedPods, strings.Join(podNames[:affectedPods], ", ")),
					Evidence: aggregator.Evidence{
						Events: []domain.UnifiedEvent{*event},
					},
					Impact: aggregator.Impact{
						Scope:       "config",
						Resources:   append([]string{configName}, podNames...),
						UserImpact:  "Application restarts due to config change",
						Degradation: fmt.Sprintf("%d pods restarted", affectedPods),
					},
					Timestamp: time.Now(),
				})
			}

			// Check service impact
			servicesInterface, _ := record.Get("services")
			if services, ok := servicesInterface.([]interface{}); ok && len(services) > 0 {
				var serviceNames []string
				for _, svcInterface := range services {
					if svc, ok := svcInterface.(neo4j.Node); ok {
						if name, exists := svc.Props["name"]; exists {
							serviceNames = append(serviceNames, name.(string))
						}
					}
				}

				if len(serviceNames) > 0 && affectedPods > 0 {
					findings = append(findings, aggregator.Finding{
						ID:         fmt.Sprintf("config-change-service-impact-%s", configName),
						Type:       "config_change_service_impact",
						Severity:   aggregator.SeverityMedium,
						Confidence: 0.75,
						Message:    fmt.Sprintf("ConfigMap %s change impacted services: %s", configName, strings.Join(serviceNames, ", ")),
						Evidence: aggregator.Evidence{
							Events: []domain.UnifiedEvent{*event},
						},
						Impact: aggregator.Impact{
							Scope:       "service",
							Resources:   append([]string{configName}, serviceNames...),
							UserImpact:  "Service interruption during pod restarts",
							Degradation: "Temporary - rolling restart",
						},
						Timestamp: time.Now(),
					})
				}
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating config dependency results: %w", err)
	}

	return findings, nil
}

// correlateVolumeIssues analyzes volume mount failures
func (d *DependencyCorrelator) correlateVolumeIssues(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := d.getNamespace(event)
	podName := d.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	d.logger.Debug("Correlating volume issues",
		zap.String("pod", podName),
		zap.String("namespace", namespace))

	session := d.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for pod volume dependencies
	query := `
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (p)-[:CLAIMS]->(pvc:PVC)
		OPTIONAL MATCH (pvc)-[:USES]->(sc:StorageClass)
		RETURN p,
		       collect(DISTINCT pvc) as pvcs,
		       collect(DISTINCT sc) as storage_classes
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"podName":   podName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query volume dependencies: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Check PVC availability
		pvcsInterface, _ := record.Get("pvcs")
		if pvcs, ok := pvcsInterface.([]interface{}); ok && len(pvcs) > 0 {
			for _, pvcInterface := range pvcs {
				if pvc, ok := pvcInterface.(neo4j.Node); ok {
					props := pvc.Props
					pvcName := props["name"].(string)

					// Check PVC status
					if status, exists := props["status"]; exists && status == "Pending" {
						findings = append(findings, aggregator.Finding{
							ID:         fmt.Sprintf("pod-pvc-pending-%s-%s", podName, pvcName),
							Type:       "pod_volume_pending",
							Severity:   aggregator.SeverityCritical,
							Confidence: 0.90,
							Message:    fmt.Sprintf("Pod %s cannot start because PVC %s is pending", podName, pvcName),
							Evidence: aggregator.Evidence{
								Events: []domain.UnifiedEvent{*event},
								GraphPaths: []aggregator.GraphPath{{
									Nodes: []aggregator.GraphNode{
										{
											ID:     podName,
											Type:   "Pod",
											Labels: map[string]string{"name": podName, "namespace": namespace},
										},
										{
											ID:     pvcName,
											Type:   "PVC",
											Labels: map[string]string{"name": pvcName, "namespace": namespace, "status": "Pending"},
										},
									},
									Edges: []aggregator.GraphEdge{{
										From:         podName,
										To:           pvcName,
										Relationship: "CLAIMS",
										Properties:   map[string]string{"type": "volume"},
									}},
								}},
							},
							Impact: aggregator.Impact{
								Scope:       "pod",
								Resources:   []string{podName, pvcName},
								UserImpact:  "Pod cannot start due to volume issue",
								Degradation: "100% - pod stuck pending",
							},
							Timestamp: time.Now(),
						})
					}

					// Check storage class issues
					storageClassesInterface, _ := record.Get("storage_classes")
					if scs, ok := storageClassesInterface.([]interface{}); ok && len(scs) == 0 {
						findings = append(findings, aggregator.Finding{
							ID:         fmt.Sprintf("pod-pvc-no-storage-class-%s-%s", podName, pvcName),
							Type:       "pod_volume_no_storage_class",
							Severity:   aggregator.SeverityHigh,
							Confidence: 0.80,
							Message:    fmt.Sprintf("Pod %s PVC %s has no storage class configured", podName, pvcName),
							Evidence: aggregator.Evidence{
								Events: []domain.UnifiedEvent{*event},
							},
							Impact: aggregator.Impact{
								Scope:       "pod",
								Resources:   []string{podName, pvcName},
								UserImpact:  "Pod cannot get persistent storage",
								Degradation: "100% - volume provisioning failed",
							},
							Timestamp: time.Now(),
						})
					}
				}
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating volume dependency results: %w", err)
	}

	return findings, nil
}

// correlateGenericDependencies performs general dependency analysis
func (d *DependencyCorrelator) correlateGenericDependencies(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	// For now, return empty findings for unknown event types
	// This can be extended later for generic dependency patterns
	d.logger.Debug("Generic dependency correlation",
		zap.String("event_type", string(event.Type)),
		zap.String("event_id", event.ID))

	return []aggregator.Finding{}, nil
}

// calculateConfidence determines overall confidence based on findings
func (d *DependencyCorrelator) calculateConfidence(findings []aggregator.Finding, event *domain.UnifiedEvent) float64 {
	if len(findings) == 0 {
		return 0.0
	}

	// Calculate weighted average of finding confidences
	totalWeight := 0.0
	weightedSum := 0.0

	for _, finding := range findings {
		// Weight by severity
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

	// Boost if multiple findings agree
	if len(findings) > 1 {
		confidence += 0.1
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// Health checks if the correlator is healthy
func (d *DependencyCorrelator) Health(ctx context.Context) error {
	return d.neo4jDriver.VerifyConnectivity(ctx)
}

// SetGraphClient implements GraphCorrelator interface
func (d *DependencyCorrelator) SetGraphClient(client interface{}) {
	if driver, ok := client.(neo4j.DriverWithContext); ok {
		d.neo4jDriver = driver
	}
}

// PreloadGraph implements GraphCorrelator interface
func (d *DependencyCorrelator) PreloadGraph(ctx context.Context) error {
	// For now, no preloading needed
	// Could be used to cache common graph patterns
	return nil
}

// Helper functions
func (d *DependencyCorrelator) getNamespace(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.Namespace != "" {
		return event.K8sContext.Namespace
	}
	if event.Entity != nil && event.Entity.Namespace != "" {
		return event.Entity.Namespace
	}
	return "default"
}

func (d *DependencyCorrelator) getCluster(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.ClusterName != "" {
		return event.K8sContext.ClusterName
	}
	return "unknown"
}

func (d *DependencyCorrelator) getEntityName(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.Name != "" {
		return event.K8sContext.Name
	}
	if event.Entity != nil && event.Entity.Name != "" {
		return event.Entity.Name
	}
	return ""
}
