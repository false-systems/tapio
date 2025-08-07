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

// ConfigImpactCorrelator analyzes the impact of configuration changes on pods and services
// It tracks: ConfigMap/Secret changes → Pod restarts → Service disruptions
type ConfigImpactCorrelator struct {
	*BaseCorrelator
	neo4jDriver neo4j.DriverWithContext
	logger      *zap.Logger
}

// NewConfigImpactCorrelator creates a new config impact correlator
func NewConfigImpactCorrelator(neo4jDriver neo4j.DriverWithContext, logger *zap.Logger) (*ConfigImpactCorrelator, error) {
	if neo4jDriver == nil {
		return nil, fmt.Errorf("neo4jDriver is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	capabilities := CorrelatorCapabilities{
		EventTypes: []string{
			"config_changed",
			"secret_changed",
			"pod_restart",
			"pod_crash",
			"container_restart",
			"deployment_rollout",
		},
		RequiredData: []string{"namespace", "cluster"},
		OptionalData: []string{"configmap", "secret", "pod", "deployment"},
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

	base := NewBaseCorrelator("config-impact-correlator", "1.0.0", capabilities)

	return &ConfigImpactCorrelator{
		BaseCorrelator: base,
		neo4jDriver:    neo4jDriver,
		logger:         logger,
	}, nil
}

// Correlate analyzes config change impacts
func (c *ConfigImpactCorrelator) Correlate(ctx context.Context, event *domain.UnifiedEvent) (*aggregator.CorrelatorOutput, error) {
	startTime := time.Now()

	// Validate event can be processed
	if err := c.ValidateEvent(event); err != nil {
		return nil, err
	}

	c.logger.Debug("Processing config impact correlation",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("namespace", c.getNamespace(event)),
		zap.String("entity", c.getEntityName(event)))

	// Route to appropriate analysis
	var findings []aggregator.Finding
	var err error

	switch event.Type {
	case "config_changed", "secret_changed":
		findings, err = c.analyzeConfigChange(ctx, event)
	case "pod_restart", "pod_crash", "container_restart":
		findings, err = c.analyzePodRestartCause(ctx, event)
	case "deployment_rollout":
		findings, err = c.analyzeDeploymentConfig(ctx, event)
	default:
		findings = []aggregator.Finding{}
	}

	if err != nil {
		c.logger.Error("Config impact correlation failed",
			zap.String("event_id", event.ID),
			zap.Error(err))
		return nil, fmt.Errorf("config impact correlation failed: %w", err)
	}

	// Calculate overall confidence
	confidence := c.calculateConfidence(findings)

	// Build context
	context_map := map[string]string{
		"namespace":        c.getNamespace(event),
		"cluster":          c.getCluster(event),
		"correlation_type": "config_impact",
		"event_type":       string(event.Type),
	}

	if entity := c.getEntityName(event); entity != "" {
		context_map["entity"] = entity
	}

	return &aggregator.CorrelatorOutput{
		CorrelatorName:    c.Name(),
		CorrelatorVersion: c.Version(),
		Findings:          findings,
		Context:           context_map,
		Confidence:        confidence,
		ProcessingTime:    time.Since(startTime),
		Timestamp:         time.Now(),
	}, nil
}

// analyzeConfigChange checks what pods/services are affected by a config change
func (c *ConfigImpactCorrelator) analyzeConfigChange(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := c.getNamespace(event)
	configName := c.getEntityName(event)
	configType := "ConfigMap"
	if event.Type == "secret_changed" {
		configType = "Secret"
	}

	if configName == "" {
		return nil, fmt.Errorf("%s name not found in event", configType)
	}

	c.logger.Debug("Analyzing config change impact",
		zap.String("config", configName),
		zap.String("type", configType),
		zap.String("namespace", namespace))

	session := c.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for pods using this config and their recent status
	query := fmt.Sprintf(`
		MATCH (cfg:%s {name: $configName, namespace: $namespace})
		OPTIONAL MATCH (p:Pod)-[:MOUNTS|USES_SECRET]->(cfg)
		WHERE p.namespace = $namespace
		OPTIONAL MATCH (svc:Service)-[:SELECTS]->(p)
		WITH cfg, p, svc, 
		     p.lastRestart as podRestart,
		     p.ready as podReady,
		     p.phase as podPhase
		RETURN cfg,
		       collect(DISTINCT {
		           pod: p,
		           restart: podRestart,
		           ready: podReady,
		           phase: podPhase,
		           services: collect(DISTINCT svc.name)
		       }) as affectedPods
	`, configType)

	result, err := session.Run(ctx, query, map[string]interface{}{
		"configName": configName,
		"namespace":  namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query config impact: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		// Analyze affected pods
		if affectedPodsValue, found := record.Get("affectedPods"); found {
			if affectedPods, ok := affectedPodsValue.([]interface{}); ok {
				findings = append(findings, c.analyzeAffectedPods(configName, configType, affectedPods, event)...)
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating config impact results: %w", err)
	}

	// Add immediate impact finding if pods are affected
	if len(findings) > 0 {
		findings = append([]aggregator.Finding{{
			ID:         fmt.Sprintf("config-change-impact-%s", configName),
			Type:       "config_change_detected",
			Severity:   aggregator.SeverityMedium,
			Confidence: 0.95,
			Message:    fmt.Sprintf("%s %s was changed, analyzing impact on %d findings", configType, configName, len(findings)),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: aggregator.Impact{
				Scope:       "config",
				Resources:   []string{configName},
				UserImpact:  "Configuration change may cause pod restarts",
				Degradation: "Temporary during rollout",
			},
			Timestamp: time.Now(),
		}}, findings...)
	}

	return findings, nil
}

// analyzePodRestartCause checks if a pod restart was caused by config changes
func (c *ConfigImpactCorrelator) analyzePodRestartCause(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := c.getNamespace(event)
	podName := c.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	c.logger.Debug("Analyzing pod restart cause",
		zap.String("pod", podName),
		zap.String("namespace", namespace))

	session := c.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for recent config changes that might have caused the restart
	query := `
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		WHERE cm.lastModified > datetime() - duration({minutes: 30})
		OPTIONAL MATCH (p)-[:USES_SECRET]->(s:Secret)
		WHERE s.lastModified > datetime() - duration({minutes: 30})
		RETURN p,
		       collect(DISTINCT {type: 'ConfigMap', name: cm.name, modified: cm.lastModified}) +
		       collect(DISTINCT {type: 'Secret', name: s.name, modified: s.lastModified}) as recentChanges
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"podName":   podName,
		"namespace": namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query pod restart cause: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		if changesValue, found := record.Get("recentChanges"); found {
			if changes, ok := changesValue.([]interface{}); ok && len(changes) > 0 {
				findings = append(findings, c.createRestartCauseFindings(podName, changes, event)...)
			}
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pod restart cause results: %w", err)
	}

	return findings, nil
}

// analyzeDeploymentConfig checks deployment configuration issues
func (c *ConfigImpactCorrelator) analyzeDeploymentConfig(ctx context.Context, event *domain.UnifiedEvent) ([]aggregator.Finding, error) {
	namespace := c.getNamespace(event)
	deploymentName := c.getEntityName(event)

	if deploymentName == "" {
		return nil, fmt.Errorf("deployment name not found in event")
	}

	session := c.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	// Query for deployment's config dependencies
	query := `
		MATCH (d:Deployment {name: $deploymentName, namespace: $namespace})
		OPTIONAL MATCH (d)-[:OWNS]->(rs:ReplicaSet)-[:OWNS]->(p:Pod)
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		OPTIONAL MATCH (p)-[:USES_SECRET]->(s:Secret)
		WITH d, 
		     count(DISTINCT p) as podCount,
		     count(DISTINCT CASE WHEN p.ready = true THEN p END) as readyPods,
		     collect(DISTINCT cm.name) as configMaps,
		     collect(DISTINCT s.name) as secrets
		RETURN d, podCount, readyPods, configMaps, secrets
	`

	result, err := session.Run(ctx, query, map[string]interface{}{
		"deploymentName": deploymentName,
		"namespace":      namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment config: %w", err)
	}

	var findings []aggregator.Finding

	for result.Next(ctx) {
		record := result.Record()

		podCountValue, _ := record.Get("podCount")
		readyPodsValue, _ := record.Get("readyPods")

		podCount, _ := podCountValue.(int64)
		readyPods, _ := readyPodsValue.(int64)

		if podCount > 0 && readyPods < podCount {
			configMapsValue, _ := record.Get("configMaps")
			secretsValue, _ := record.Get("secrets")

			configMaps := c.interfaceSliceToStringSlice(configMapsValue)
			secrets := c.interfaceSliceToStringSlice(secretsValue)

			findings = append(findings, aggregator.Finding{
				ID:         fmt.Sprintf("deployment-config-rollout-%s", deploymentName),
				Type:       "deployment_config_rollout",
				Severity:   aggregator.SeverityMedium,
				Confidence: 0.80,
				Message:    fmt.Sprintf("Deployment %s has %d/%d ready pods during rollout", deploymentName, readyPods, podCount),
				Evidence: aggregator.Evidence{
					Events: []domain.UnifiedEvent{*event},
					Attributes: map[string]interface{}{
						"configMaps": configMaps,
						"secrets":    secrets,
					},
				},
				Impact: aggregator.Impact{
					Scope:       "deployment",
					Resources:   append([]string{deploymentName}, append(configMaps, secrets...)...),
					UserImpact:  "Service may be degraded during rollout",
					Degradation: fmt.Sprintf("%d%% capacity", (readyPods*100)/podCount),
				},
				Timestamp: time.Now(),
			})
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating deployment config results: %w", err)
	}

	return findings, nil
}

// analyzeAffectedPods creates findings for pods affected by config changes
func (c *ConfigImpactCorrelator) analyzeAffectedPods(configName, configType string, affectedPods []interface{}, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	restartedPods := []string{}
	notReadyPods := []string{}
	affectedServices := map[string]bool{}

	for _, podData := range affectedPods {
		if podMap, ok := podData.(map[string]interface{}); ok {
			if podNode, ok := podMap["pod"].(neo4j.Node); ok {
				podName := podNode.Props["name"].(string)

				// Check if pod restarted after config change
				if restartTime, ok := podMap["restart"].(time.Time); ok {
					if restartTime.After(event.Timestamp.Add(-10 * time.Minute)) {
						restartedPods = append(restartedPods, podName)
					}
				}

				// Check if pod is not ready
				if ready, ok := podMap["ready"].(bool); ok && !ready {
					notReadyPods = append(notReadyPods, podName)
				}

				// Track affected services
				if services, ok := podMap["services"].([]interface{}); ok {
					for _, svc := range services {
						if svcName, ok := svc.(string); ok {
							affectedServices[svcName] = true
						}
					}
				}
			}
		}
	}

	// Create findings based on impact
	if len(restartedPods) > 0 {
		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("config-caused-restarts-%s", configName),
			Type:       "config_change_pod_restarts",
			Severity:   aggregator.SeverityHigh,
			Confidence: 0.85,
			Message:    fmt.Sprintf("%s %s change caused %d pod restarts: %s", configType, configName, len(restartedPods), strings.Join(restartedPods, ", ")),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: aggregator.Impact{
				Scope:       "pods",
				Resources:   append([]string{configName}, restartedPods...),
				UserImpact:  "Service disruption during pod restarts",
				Degradation: fmt.Sprintf("%d pods restarted", len(restartedPods)),
			},
			Timestamp: time.Now(),
		})
	}

	if len(notReadyPods) > 0 {
		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("config-pods-not-ready-%s", configName),
			Type:       "config_change_pods_not_ready",
			Severity:   aggregator.SeverityMedium,
			Confidence: 0.75,
			Message:    fmt.Sprintf("%d pods not ready after %s %s change", len(notReadyPods), configType, configName),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: aggregator.Impact{
				Scope:       "pods",
				Resources:   append([]string{configName}, notReadyPods...),
				UserImpact:  "Pods still initializing with new configuration",
				Degradation: fmt.Sprintf("%d pods not ready", len(notReadyPods)),
			},
			Timestamp: time.Now(),
		})
	}

	if len(affectedServices) > 0 {
		svcList := []string{}
		for svc := range affectedServices {
			svcList = append(svcList, svc)
		}

		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("config-service-impact-%s", configName),
			Type:       "config_change_service_impact",
			Severity:   aggregator.SeverityMedium,
			Confidence: 0.70,
			Message:    fmt.Sprintf("Services affected by %s %s change: %s", configType, configName, strings.Join(svcList, ", ")),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: aggregator.Impact{
				Scope:       "services",
				Resources:   append([]string{configName}, svcList...),
				UserImpact:  "Service endpoints changing due to pod restarts",
				Degradation: "Temporary - rolling update",
			},
			Timestamp: time.Now(),
		})
	}

	return findings
}

// createRestartCauseFindings creates findings for pod restarts caused by config changes
func (c *ConfigImpactCorrelator) createRestartCauseFindings(podName string, changes []interface{}, event *domain.UnifiedEvent) []aggregator.Finding {
	var findings []aggregator.Finding
	configChanges := []string{}

	for _, change := range changes {
		if changeMap, ok := change.(map[string]interface{}); ok {
			configType := changeMap["type"].(string)
			configName := changeMap["name"].(string)
			configChanges = append(configChanges, fmt.Sprintf("%s/%s", configType, configName))
		}
	}

	if len(configChanges) > 0 {
		findings = append(findings, aggregator.Finding{
			ID:         fmt.Sprintf("pod-restart-config-cause-%s", podName),
			Type:       "pod_restart_config_cause",
			Severity:   aggregator.SeverityHigh,
			Confidence: 0.90,
			Message:    fmt.Sprintf("Pod %s restart likely caused by config changes: %s", podName, strings.Join(configChanges, ", ")),
			Evidence: aggregator.Evidence{
				Events: []domain.UnifiedEvent{*event},
				GraphPaths: []aggregator.GraphPath{{
					Nodes: []aggregator.GraphNode{
						{
							ID:     podName,
							Type:   "Pod",
							Labels: map[string]string{"name": podName},
						},
					},
				}},
			},
			Impact: aggregator.Impact{
				Scope:       "pod",
				Resources:   append([]string{podName}, configChanges...),
				UserImpact:  "Pod restarted due to configuration change",
				Degradation: "100% - pod restart",
			},
			Timestamp: time.Now(),
		})
	}

	return findings
}

// calculateConfidence calculates overall confidence for findings
func (c *ConfigImpactCorrelator) calculateConfidence(findings []aggregator.Finding) float64 {
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

	// Boost if multiple findings correlate
	if len(findings) > 1 {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// Health checks if the correlator is healthy
func (c *ConfigImpactCorrelator) Health(ctx context.Context) error {
	return c.neo4jDriver.VerifyConnectivity(ctx)
}

// SetGraphClient implements GraphCorrelator interface
func (c *ConfigImpactCorrelator) SetGraphClient(client interface{}) {
	if driver, ok := client.(neo4j.DriverWithContext); ok {
		c.neo4jDriver = driver
	}
}

// PreloadGraph implements GraphCorrelator interface
func (c *ConfigImpactCorrelator) PreloadGraph(ctx context.Context) error {
	// No preloading needed for now
	return nil
}

// Helper functions
func (c *ConfigImpactCorrelator) getNamespace(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.Namespace != "" {
		return event.K8sContext.Namespace
	}
	if event.Entity != nil && event.Entity.Namespace != "" {
		return event.Entity.Namespace
	}
	return "default"
}

func (c *ConfigImpactCorrelator) getCluster(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.ClusterName != "" {
		return event.K8sContext.ClusterName
	}
	return "unknown"
}

func (c *ConfigImpactCorrelator) getEntityName(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil && event.K8sContext.Name != "" {
		return event.K8sContext.Name
	}
	if event.Entity != nil && event.Entity.Name != "" {
		return event.Entity.Name
	}
	return ""
}

func (c *ConfigImpactCorrelator) interfaceSliceToStringSlice(input interface{}) []string {
	result := []string{}
	if slice, ok := input.([]interface{}); ok {
		for _, item := range slice {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	}
	return result
}
