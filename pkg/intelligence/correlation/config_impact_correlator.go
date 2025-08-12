package correlation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// AffectedPodData represents a pod affected by config changes
type AffectedPodData struct {
	Pod      GraphNode
	Name     string
	Restart  time.Time
	Ready    bool
	Phase    string
	Services []string
}

// ConfigChangeInfo represents a configuration change info for impact analysis
type ConfigChangeInfo struct {
	Type     string // ConfigMap or Secret
	Name     string
	Modified time.Time
}

// ConfigImpactCorrelator analyzes the impact of configuration changes on pods and services
// It tracks: ConfigMap/Secret changes → Pod restarts → Service disruptions
type ConfigImpactCorrelator struct {
	*BaseCorrelator
	graphStore  GraphStore
	logger      *zap.Logger
	queryConfig QueryConfig
}

// NewConfigImpactCorrelator creates a new config impact correlator
func NewConfigImpactCorrelator(graphStore GraphStore, logger *zap.Logger) (*ConfigImpactCorrelator, error) {
	if graphStore == nil {
		return nil, fmt.Errorf("graphStore is required")
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
					return graphStore.HealthCheck(ctx)
				},
			},
		},
		MaxEventAge:  MaxEventAge,
		BatchSupport: false,
	}

	base := NewBaseCorrelator("config-impact-correlator", DefaultCorrelatorVersion, capabilities)

	return &ConfigImpactCorrelator{
		BaseCorrelator: base,
		graphStore:     graphStore,
		logger:         logger,
		queryConfig:    DefaultQueryConfig(),
	}, nil
}

// Correlate analyzes config change impacts
func (c *ConfigImpactCorrelator) Correlate(ctx context.Context, event *domain.UnifiedEvent) (*CorrelatorOutput, error) {
	startTime := time.Now()

	// Validate event can be processed
	if err := c.ValidateEvent(event); err != nil {
		return nil, err
	}

	c.logCorrelationStart(event)

	// Route to appropriate analysis
	findings, err := c.routeEventAnalysis(ctx, event)
	if err != nil {
		c.logCorrelationError(event, err)
		return nil, fmt.Errorf("config impact correlation failed: %w", err)
	}

	// Build and return correlator output
	return c.buildCorrelatorOutput(findings, event, time.Since(startTime)), nil
}

// logCorrelationStart logs the start of correlation processing
func (c *ConfigImpactCorrelator) logCorrelationStart(event *domain.UnifiedEvent) {
	c.logger.Debug("Processing config impact correlation",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("namespace", c.getNamespace(event)),
		zap.String("entity", c.getEntityName(event)))
}

// logCorrelationError logs correlation processing errors
func (c *ConfigImpactCorrelator) logCorrelationError(event *domain.UnifiedEvent, err error) {
	c.logger.Error("Config impact correlation failed",
		zap.String("event_id", event.ID),
		zap.Error(err))
}

// routeEventAnalysis routes events to appropriate analysis methods
func (c *ConfigImpactCorrelator) routeEventAnalysis(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	switch event.Type {
	case "config_changed", "secret_changed":
		return c.analyzeConfigChange(ctx, event)
	case "pod_restart", "pod_crash", "container_restart":
		return c.analyzePodRestartCause(ctx, event)
	case "deployment_rollout":
		return c.analyzeDeploymentConfig(ctx, event)
	default:
		return []Finding{}, nil
	}
}

// buildCorrelatorOutput builds the final correlator output
func (c *ConfigImpactCorrelator) buildCorrelatorOutput(findings []Finding, event *domain.UnifiedEvent, processingTime time.Duration) *CorrelatorOutput {
	confidence := c.calculateConfidence(findings)
	contextMap := c.buildContextMap(event)

	return &CorrelatorOutput{
		CorrelatorName:    c.Name(),
		CorrelatorVersion: c.Version(),
		Findings:          findings,
		Context:           contextMap,
		Confidence:        confidence,
		ProcessingTime:    processingTime,
		Timestamp:         time.Now(),
	}
}

// buildContextMap builds the correlation context map
func (c *ConfigImpactCorrelator) buildContextMap(event *domain.UnifiedEvent) map[string]string {
	contextMap := map[string]string{
		"namespace":        c.getNamespace(event),
		"cluster":          c.getCluster(event),
		"correlation_type": "config_impact",
		"event_type":       string(event.Type),
	}

	if entity := c.getEntityName(event); entity != "" {
		contextMap["entity"] = entity
	}

	return contextMap
}

// analyzeConfigChange checks what pods/services are affected by a config change
func (c *ConfigImpactCorrelator) analyzeConfigChange(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	namespace := c.getNamespace(event)
	configName := c.getEntityName(event)
	configType := c.determineConfigType(event)

	if configName == "" {
		return nil, fmt.Errorf("%s name not found in event", configType)
	}

	c.logConfigAnalysis(configName, configType, namespace)

	// Query for affected pods
	affectedPods, err := c.queryConfigImpactPods(ctx, namespace, configName, configType)
	if err != nil {
		return nil, err
	}

	// Generate findings from affected pods
	findings := c.generateConfigChangeFindings(configName, configType, affectedPods, event)

	// Add immediate impact finding if pods are affected
	if len(findings) > 0 {
		immediateFinding := c.createImmediateImpactFinding(configName, configType, len(findings), event)
		findings = append([]Finding{immediateFinding}, findings...)
	}

	return findings, nil
}

// determineConfigType determines if the event is for ConfigMap or Secret
func (c *ConfigImpactCorrelator) determineConfigType(event *domain.UnifiedEvent) string {
	if event.Type == "secret_changed" {
		return "Secret"
	}
	return "ConfigMap"
}

// logConfigAnalysis logs config change analysis details
func (c *ConfigImpactCorrelator) logConfigAnalysis(configName, configType, namespace string) {
	c.logger.Debug("Analyzing config change impact",
		zap.String("config", configName),
		zap.String("type", configType),
		zap.String("namespace", namespace))
}

// queryConfigImpactPods queries for pods affected by config changes
func (c *ConfigImpactCorrelator) queryConfigImpactPods(ctx context.Context, namespace, configName, configType string) ([]AffectedPodData, error) {
	limit := c.queryConfig.GetLimit("config")
	query := fmt.Sprintf(`
		MATCH (cfg:%s {name: $configName, namespace: $namespace})
		OPTIONAL MATCH (p:Pod)-[:MOUNTS|USES_SECRET]->(cfg)
		WHERE p.namespace = $namespace
		OPTIONAL MATCH (svc:Service)-[:SELECTS]->(p)
		WITH cfg, p, svc, 
		     p.lastRestart as podRestart,
		     p.ready as podReady,
		     p.phase as podPhase
		LIMIT %d
		RETURN cfg,
		       collect(DISTINCT {
		           pod: p,
		           restart: podRestart,
		           ready: podReady,
		           phase: podPhase,
		           services: collect(DISTINCT svc.name)[0..%d]
		       })[0..%d] as affectedPods
	`, configType, limit*2, limit, limit)

	params := &ConfigQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		ConfigName: configName,
		ConfigType: configType,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := c.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query config impact: %w", err)
	}
	defer result.Close(ctx)

	return c.extractAffectedPodsFromResult(ctx, result)
}

// extractAffectedPodsFromResult extracts affected pods from query result
func (c *ConfigImpactCorrelator) extractAffectedPodsFromResult(ctx context.Context, result ResultIterator) ([]AffectedPodData, error) {
	var allAffectedPods []AffectedPodData

	for result.Next(ctx) {
		record := result.Record()
		if affectedPodsValue, found := record.Get("affectedPods"); found {
			affectedPods := c.parseAffectedPodsFromValue(affectedPodsValue)
			allAffectedPods = append(allAffectedPods, affectedPods...)
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating config impact results: %w", err)
	}

	return allAffectedPods, nil
}

// generateConfigChangeFindings generates findings from affected pods
func (c *ConfigImpactCorrelator) generateConfigChangeFindings(configName, configType string, affectedPods []AffectedPodData, event *domain.UnifiedEvent) []Finding {
	var findings []Finding
	for _, pods := range [][]AffectedPodData{affectedPods} {
		if len(pods) > 0 {
			findings = append(findings, c.analyzeAffectedPods(configName, configType, pods, event)...)
		}
	}
	return findings
}

// createImmediateImpactFinding creates immediate impact finding for config changes
func (c *ConfigImpactCorrelator) createImmediateImpactFinding(configName, configType string, findingsCount int, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("config-change-impact-%s", configName),
		Type:       "config_change_detected",
		Severity:   SeverityMedium,
		Confidence: HighTestConfidence,
		Message:    fmt.Sprintf("%s %s was changed, analyzing impact on %d findings", configType, configName, findingsCount),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "config",
			Resources:   []string{configName},
			UserImpact:  "Configuration change may cause pod restarts",
			Degradation: "Temporary during rollout",
		},
		Timestamp: time.Now(),
	}
}

// analyzePodRestartCause checks if a pod restart was caused by config changes
func (c *ConfigImpactCorrelator) analyzePodRestartCause(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	namespace := c.getNamespace(event)
	podName := c.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	c.logPodRestartAnalysis(podName, namespace)

	// Query for recent config changes
	changes, err := c.queryRecentConfigChanges(ctx, namespace, podName)
	if err != nil {
		return nil, err
	}

	// Generate findings from config changes
	return c.generateRestartCauseFindings(podName, changes, event), nil
}

// logPodRestartAnalysis logs pod restart cause analysis
func (c *ConfigImpactCorrelator) logPodRestartAnalysis(podName, namespace string) {
	c.logger.Debug("Analyzing pod restart cause",
		zap.String("pod", podName),
		zap.String("namespace", namespace))
}

// queryRecentConfigChanges queries for recent config changes that might have caused restart
func (c *ConfigImpactCorrelator) queryRecentConfigChanges(ctx context.Context, namespace, podName string) ([]ConfigChangeInfo, error) {
	limit := c.queryConfig.GetLimit("config")
	query := fmt.Sprintf(`
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		WHERE cm.lastModified > datetime() - duration({minutes: 30})
		OPTIONAL MATCH (p)-[:USES_SECRET]->(s:Secret)
		WHERE s.lastModified > datetime() - duration({minutes: 30})
		RETURN p,
		       (collect(DISTINCT {type: 'ConfigMap', name: cm.name, modified: cm.lastModified})[0..%d] +
		        collect(DISTINCT {type: 'Secret', name: s.name, modified: s.lastModified})[0..%d]) as recentChanges
		LIMIT 1
	`, limit, limit)

	params := &PodQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		PodName: podName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := c.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query pod restart cause: %w", err)
	}
	defer result.Close(ctx)

	return c.extractConfigChangesFromResult(ctx, result)
}

// extractConfigChangesFromResult extracts config changes from query result
func (c *ConfigImpactCorrelator) extractConfigChangesFromResult(ctx context.Context, result ResultIterator) ([]ConfigChangeInfo, error) {
	var allChanges []ConfigChangeInfo

	for result.Next(ctx) {
		record := result.Record()
		if changesValue, found := record.Get("recentChanges"); found {
			changes := c.parseConfigChangesFromValue(changesValue)
			allChanges = append(allChanges, changes...)
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pod restart cause results: %w", err)
	}

	return allChanges, nil
}

// generateRestartCauseFindings generates findings from config changes
func (c *ConfigImpactCorrelator) generateRestartCauseFindings(podName string, changes []ConfigChangeInfo, event *domain.UnifiedEvent) []Finding {
	var findings []Finding
	if len(changes) > 0 {
		findings = append(findings, c.createRestartCauseFindings(podName, changes, event)...)
	}
	return findings
}

// analyzeDeploymentConfig checks deployment configuration issues
func (c *ConfigImpactCorrelator) analyzeDeploymentConfig(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	namespace := c.getNamespace(event)
	deploymentName := c.getEntityName(event)

	if deploymentName == "" {
		return nil, fmt.Errorf("deployment name not found in event")
	}

	// Query deployment config dependencies
	deploymentData, err := c.queryDeploymentConfigData(ctx, namespace, deploymentName)
	if err != nil {
		return nil, err
	}

	// Process query results
	findings, err := c.processDeploymentConfigResults(ctx, deploymentData, deploymentName, event)
	if err != nil {
		return nil, err
	}

	return findings, nil
}

// DeploymentConfigData holds deployment configuration analysis data
type DeploymentConfigData struct {
	Result ResultIterator
}

// queryDeploymentConfigData queries deployment configuration dependencies
func (c *ConfigImpactCorrelator) queryDeploymentConfigData(ctx context.Context, namespace, deploymentName string) (*DeploymentConfigData, error) {
	limit := c.queryConfig.GetLimit("config")
	query := fmt.Sprintf(`
		MATCH (d:Deployment {name: $deploymentName, namespace: $namespace})
		OPTIONAL MATCH (d)-[:OWNS]->(rs:ReplicaSet)-[:OWNS]->(p:Pod)
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		OPTIONAL MATCH (p)-[:USES_SECRET]->(s:Secret)
		WITH d, 
		     count(DISTINCT p) as podCount,
		     count(DISTINCT CASE WHEN p.ready = true THEN p END) as readyPods,
		     collect(DISTINCT cm.name)[0..%d] as configMaps,
		     collect(DISTINCT s.name)[0..%d] as secrets
		RETURN d, podCount, readyPods, configMaps, secrets
		LIMIT 1
	`, limit, limit)

	params := &DeploymentQueryParams{
		BaseQueryParams: BaseQueryParams{
			Namespace: namespace,
		},
		DeploymentName: deploymentName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := c.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment config: %w", err)
	}

	return &DeploymentConfigData{Result: result}, nil
}

// processDeploymentConfigResults processes deployment query results and creates findings
func (c *ConfigImpactCorrelator) processDeploymentConfigResults(ctx context.Context, data *DeploymentConfigData, deploymentName string, event *domain.UnifiedEvent) ([]Finding, error) {
	defer data.Result.Close(ctx)
	var findings []Finding

	for data.Result.Next(ctx) {
		record := data.Result.Record()
		finding := c.createDeploymentFinding(record, deploymentName, event)
		if finding != nil {
			findings = append(findings, *finding)
		}
	}

	if err := data.Result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating deployment config results: %w", err)
	}

	return findings, nil
}

// createDeploymentFinding creates a deployment rollout finding if needed
func (c *ConfigImpactCorrelator) createDeploymentFinding(record *GraphRecord, deploymentName string, event *domain.UnifiedEvent) *Finding {
	podCountValue, _ := record.Get("podCount")
	readyPodsValue, _ := record.Get("readyPods")

	podCount, ok := podCountValue.(int64)
	if !ok {
		c.logger.Warn("Failed to extract pod count as int64",
			zap.String("deployment", deploymentName),
			zap.Any("value", podCountValue))
		podCount = 0
	}
	readyPods, ok := readyPodsValue.(int64)
	if !ok {
		c.logger.Warn("Failed to extract ready pods as int64",
			zap.String("deployment", deploymentName),
			zap.Any("value", readyPodsValue))
		readyPods = 0
	}

	if podCount == 0 || readyPods >= podCount {
		return nil
	}

	configMapsValue, _ := record.Get("configMaps")
	secretsValue, _ := record.Get("secrets")

	configMaps := c.parseStringSliceFromValue(configMapsValue)
	secrets := c.parseStringSliceFromValue(secretsValue)

	finding := Finding{
		ID:         fmt.Sprintf("deployment-config-rollout-%s", deploymentName),
		Type:       "deployment_config_rollout",
		Severity:   SeverityMedium,
		Confidence: MediumConfidence,
		Message:    fmt.Sprintf("Deployment %s has %d/%d ready pods during rollout", deploymentName, readyPods, podCount),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
			Attributes: map[string]string{
				"configMaps": strings.Join(configMaps, ","),
				"secrets":    strings.Join(secrets, ","),
			},
		},
		Impact: Impact{
			Scope:       "deployment",
			Resources:   append([]string{deploymentName}, append(configMaps, secrets...)...),
			UserImpact:  "Service may be degraded during rollout",
			Degradation: fmt.Sprintf("%d%% capacity", (readyPods*100)/podCount),
		},
		Timestamp: time.Now(),
	}

	return &finding
}

// analyzeAffectedPods creates findings for pods affected by config changes
func (c *ConfigImpactCorrelator) analyzeAffectedPods(configName, configType string, affectedPods []AffectedPodData, event *domain.UnifiedEvent) []Finding {
	restartedPods, notReadyPods, affectedServices := c.categorizeAffectedPods(affectedPods, event)

	var findings []Finding
	if len(restartedPods) > 0 {
		findings = append(findings, c.createRestartedPodsFindings(configName, configType, restartedPods, event))
	}
	if len(notReadyPods) > 0 {
		findings = append(findings, c.createNotReadyPodsFindings(configName, configType, notReadyPods, event))
	}
	if len(affectedServices) > 0 {
		findings = append(findings, c.createAffectedServicesFindings(configName, configType, affectedServices, event))
	}

	return findings
}

// categorizeAffectedPods categorizes pods into restarted, not ready, and affected services
func (c *ConfigImpactCorrelator) categorizeAffectedPods(affectedPods []AffectedPodData, event *domain.UnifiedEvent) ([]string, []string, []string) {
	restartedPods := []string{}
	notReadyPods := []string{}
	affectedServices := map[string]bool{}

	for _, pod := range affectedPods {
		// Check if pod restarted after config change
		if pod.Restart.After(event.Timestamp.Add(-10 * time.Minute)) {
			restartedPods = append(restartedPods, pod.Name)
		}

		// Check if pod is not ready
		if !pod.Ready {
			notReadyPods = append(notReadyPods, pod.Name)
		}

		// Track affected services
		for _, svcName := range pod.Services {
			affectedServices[svcName] = true
		}
	}

	// Convert services map to slice
	svcList := []string{}
	for svc := range affectedServices {
		svcList = append(svcList, svc)
	}

	return restartedPods, notReadyPods, svcList
}

// createRestartedPodsFindings creates findings for restarted pods
func (c *ConfigImpactCorrelator) createRestartedPodsFindings(configName, configType string, restartedPods []string, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("config-caused-restarts-%s", configName),
		Type:       "config_change_pod_restarts",
		Severity:   SeverityHigh,
		Confidence: MediumHighConfidence,
		Message:    fmt.Sprintf("%s %s change caused %d pod restarts: %s", configType, configName, len(restartedPods), strings.Join(restartedPods, ", ")),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "pods",
			Resources:   append([]string{configName}, restartedPods...),
			UserImpact:  "Service disruption during pod restarts",
			Degradation: fmt.Sprintf("%d pods restarted", len(restartedPods)),
		},
		Timestamp: time.Now(),
	}
}

// createNotReadyPodsFindings creates findings for not ready pods
func (c *ConfigImpactCorrelator) createNotReadyPodsFindings(configName, configType string, notReadyPods []string, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("config-pods-not-ready-%s", configName),
		Type:       "config_change_pods_not_ready",
		Severity:   SeverityMedium,
		Confidence: MediumLowConfidence,
		Message:    fmt.Sprintf("%d pods not ready after %s %s change", len(notReadyPods), configType, configName),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "pods",
			Resources:   append([]string{configName}, notReadyPods...),
			UserImpact:  "Pods still initializing with new configuration",
			Degradation: fmt.Sprintf("%d pods not ready", len(notReadyPods)),
		},
		Timestamp: time.Now(),
	}
}

// createAffectedServicesFindings creates findings for affected services
func (c *ConfigImpactCorrelator) createAffectedServicesFindings(configName, configType string, affectedServices []string, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("config-service-impact-%s", configName),
		Type:       "config_change_service_impact",
		Severity:   SeverityMedium,
		Confidence: LowConfidence,
		Message:    fmt.Sprintf("Services affected by %s %s change: %s", configType, configName, strings.Join(affectedServices, ", ")),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "services",
			Resources:   append([]string{configName}, affectedServices...),
			UserImpact:  "Service endpoints changing due to pod restarts",
			Degradation: "Temporary - rolling update",
		},
		Timestamp: time.Now(),
	}
}

// createRestartCauseFindings creates findings for pod restarts caused by config changes
func (c *ConfigImpactCorrelator) createRestartCauseFindings(podName string, changes []ConfigChangeInfo, event *domain.UnifiedEvent) []Finding {
	var findings []Finding
	configChanges := []string{}

	for _, change := range changes {
		configChanges = append(configChanges, fmt.Sprintf("%s/%s", change.Type, change.Name))
	}

	if len(configChanges) > 0 {
		findings = append(findings, Finding{
			ID:         fmt.Sprintf("pod-restart-config-cause-%s", podName),
			Type:       "pod_restart_config_cause",
			Severity:   SeverityHigh,
			Confidence: HighConfidence,
			Message:    fmt.Sprintf("Pod %s restart likely caused by config changes: %s", podName, strings.Join(configChanges, ", ")),
			Evidence: Evidence{
				Events: []domain.UnifiedEvent{*event},
				GraphPaths: []EvidenceGraphPath{{
					Nodes: []EvidenceGraphNode{
						{
							ID:     podName,
							Type:   "Pod",
							Labels: map[string]string{"name": podName},
						},
					},
				}},
			},
			Impact: Impact{
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
func (c *ConfigImpactCorrelator) calculateConfidence(findings []Finding) float64 {
	if len(findings) == 0 {
		return 0
	}

	totalWeight := 0.0
	weightedSum := 0.0

	for _, finding := range findings {
		var weight float64
		switch finding.Severity {
		case SeverityCritical:
			weight = 1.0
		case SeverityHigh:
			weight = HighWeight
		case SeverityMedium:
			weight = MediumWeight
		case SeverityLow:
			weight = LowWeight
		default:
			weight = VeryLowWeight
		}

		weightedSum += finding.Confidence * weight
		totalWeight += weight
	}

	confidence := weightedSum / totalWeight

	// Boost if multiple findings correlate
	if len(findings) > 1 {
		confidence += BoostMultiplier
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// Health checks if the correlator is healthy
func (c *ConfigImpactCorrelator) Health(ctx context.Context) error {
	return c.graphStore.HealthCheck(ctx)
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

// parseStringSliceFromValue safely parses a string slice from a query result value
func (c *ConfigImpactCorrelator) parseStringSliceFromValue(value interface{}) []string {
	var result []string
	if slice, ok := value.([]string); ok {
		return slice
	}
	// Fallback for interface{} slices from database
	if slice, ok := value.([]interface{}); ok {
		for _, item := range slice {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	}
	return result
}

// parseAffectedPodsFromValue safely parses affected pods from a query result value
func (c *ConfigImpactCorrelator) parseAffectedPodsFromValue(value interface{}) []AffectedPodData {
	slice, ok := value.([]interface{})
	if !ok {
		return []AffectedPodData{}
	}

	var result []AffectedPodData
	for _, item := range slice {
		podData := c.parseAffectedPodItem(item)
		if podData != nil {
			result = append(result, *podData)
		}
	}

	return result
}

// parseAffectedPodItem parses a single affected pod item from query result
func (c *ConfigImpactCorrelator) parseAffectedPodItem(item interface{}) *AffectedPodData {
	podMap, ok := item.(map[string]interface{})
	if !ok {
		return nil
	}

	podData := &AffectedPodData{}
	c.parsePodIdentity(podMap, podData)
	c.parsePodStatus(podMap, podData)
	c.parsePodServices(podMap, podData)

	return podData
}

// parsePodIdentity parses pod identity information
func (c *ConfigImpactCorrelator) parsePodIdentity(podMap map[string]interface{}, podData *AffectedPodData) {
	// Parse pod node
	if podNode, ok := podMap["pod"].(map[string]interface{}); ok {
		if props, ok := podNode["properties"].(map[string]interface{}); ok {
			podData.Pod.Properties = parseNodeProperties(props)
			podData.Name = podData.Pod.Properties.Name
		}
	} else if name, ok := podMap["name"].(string); ok {
		// Fallback to direct name access
		podData.Name = name
	}
}

// parsePodStatus parses pod status information
func (c *ConfigImpactCorrelator) parsePodStatus(podMap map[string]interface{}, podData *AffectedPodData) {
	// Parse restart time
	if restartTime, ok := podMap["restart"].(time.Time); ok {
		podData.Restart = restartTime
	}

	// Parse ready status
	if ready, ok := podMap["ready"].(bool); ok {
		podData.Ready = ready
	}

	// Parse phase
	if phase, ok := podMap["phase"].(string); ok {
		podData.Phase = phase
	}
}

// parsePodServices parses pod services information
func (c *ConfigImpactCorrelator) parsePodServices(podMap map[string]interface{}, podData *AffectedPodData) {
	// Parse services
	if services, ok := podMap["services"].([]interface{}); ok {
		for _, svc := range services {
			if svcName, ok := svc.(string); ok {
				podData.Services = append(podData.Services, svcName)
			}
		}
	}
}

// parseConfigChangesFromValue safely parses config changes from a query result value
func (c *ConfigImpactCorrelator) parseConfigChangesFromValue(value interface{}) []ConfigChangeInfo {
	var result []ConfigChangeInfo

	slice, ok := value.([]interface{})
	if !ok {
		return result
	}

	for _, item := range slice {
		changeMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		change := ConfigChangeInfo{}

		if changeType, ok := changeMap["type"].(string); ok {
			change.Type = changeType
		}
		if name, ok := changeMap["name"].(string); ok {
			change.Name = name
		}
		if modified, ok := changeMap["modified"].(time.Time); ok {
			change.Modified = modified
		}

		result = append(result, change)
	}

	return result
}
