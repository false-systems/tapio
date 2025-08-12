package correlation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// DependencyCorrelator analyzes K8s dependency relationships to find root causes
// It handles: Service→Pod, Pod→ConfigMap/Secret, Pod→PVC relationships
type DependencyCorrelator struct {
	*BaseCorrelator
	graphStore  GraphStore
	logger      *zap.Logger
	queryConfig QueryConfig

	// OTEL instrumentation - REQUIRED fields
	tracer             trace.Tracer
	eventsProcessedCtr metric.Int64Counter
	errorsTotalCtr     metric.Int64Counter
	processingTimeHist metric.Float64Histogram
	findingsFoundCtr   metric.Int64Counter
	queryDurationHist  metric.Float64Histogram
}

// NewDependencyCorrelator creates a new dependency correlator
func NewDependencyCorrelator(graphStore GraphStore, logger *zap.Logger) (*DependencyCorrelator, error) {
	if graphStore == nil {
		return nil, fmt.Errorf("graphStore is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer("dependency-correlator")
	meter := otel.Meter("dependency-correlator")

	// Create metrics with descriptive names and descriptions
	eventsProcessedCtr, err := meter.Int64Counter(
		"dependency_events_processed_total",
		metric.WithDescription("Total events processed by dependency correlator"),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotalCtr, err := meter.Int64Counter(
		"dependency_errors_total",
		metric.WithDescription("Total errors in dependency correlator"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTimeHist, err := meter.Float64Histogram(
		"dependency_processing_duration_ms",
		metric.WithDescription("Processing duration for dependency correlator in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	findingsFoundCtr, err := meter.Int64Counter(
		"dependency_findings_found_total",
		metric.WithDescription("Total findings found by dependency correlator"),
	)
	if err != nil {
		logger.Warn("Failed to create findings counter", zap.Error(err))
	}

	queryDurationHist, err := meter.Float64Histogram(
		"dependency_query_duration_ms",
		metric.WithDescription("Graph query duration for dependency correlator in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create query duration histogram", zap.Error(err))
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
					return graphStore.HealthCheck(ctx)
				},
			},
		},
		MaxEventAge:  24 * time.Hour,
		BatchSupport: false,
	}

	base := NewBaseCorrelator("dependency-correlator", "1.0.0", capabilities)

	return &DependencyCorrelator{
		BaseCorrelator:     base,
		graphStore:         graphStore,
		logger:             logger,
		queryConfig:        DefaultQueryConfig(),
		tracer:             tracer,
		eventsProcessedCtr: eventsProcessedCtr,
		errorsTotalCtr:     errorsTotalCtr,
		processingTimeHist: processingTimeHist,
		findingsFoundCtr:   findingsFoundCtr,
		queryDurationHist:  queryDurationHist,
	}, nil
}

// Correlate processes an event and finds dependency-related root causes
func (d *DependencyCorrelator) Correlate(ctx context.Context, event *domain.UnifiedEvent) (*CorrelatorOutput, error) {
	// Always start spans for operations
	ctx, span := d.tracer.Start(ctx, "correlation.dependency.analyze")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record processing time
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if d.processingTimeHist != nil {
			d.processingTimeHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
			))
		}
	}()

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("component", "dependency-correlator"),
		attribute.String("operation", "correlate"),
		attribute.String("event.type", string(event.Type)),
		attribute.String("event.id", event.ID),
		attribute.String("namespace", d.getNamespace(event)),
		attribute.String("entity", d.getEntityName(event)),
	)

	// Validate event can be processed
	if err := d.ValidateEvent(event); err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "validation_failed"),
		)
		// Record error metrics
		if d.errorsTotalCtr != nil {
			d.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "validation_failed"),
				attribute.String("event_type", string(event.Type)),
			))
		}
		return nil, err
	}

	// Record event processed
	if d.eventsProcessedCtr != nil {
		d.eventsProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", string(event.Type)),
			attribute.String("status", "processing"),
		))
	}

	d.logCorrelationStart(event)

	// Route to appropriate correlation handler
	findings, err := d.routeEventToHandler(ctx, event)
	if err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "handler_failed"),
		)
		// Record error metrics
		if d.errorsTotalCtr != nil {
			d.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "handler_failed"),
				attribute.String("event_type", string(event.Type)),
			))
		}
		d.logger.Error("Dependency correlation failed",
			zap.String("event_id", event.ID),
			zap.Error(err))
		return nil, fmt.Errorf("dependency correlation failed: %w", err)
	}

	// Record findings count
	span.SetAttributes(attribute.Int("findings.count", len(findings)))
	if d.findingsFoundCtr != nil && len(findings) > 0 {
		d.findingsFoundCtr.Add(ctx, int64(len(findings)), metric.WithAttributes(
			attribute.String("event_type", string(event.Type)),
		))
	}

	// Calculate overall confidence and build output
	confidence := d.calculateConfidence(findings, event)
	contextMap := d.buildContextMap(event)

	// Set final attributes
	span.SetAttributes(attribute.Float64("confidence", confidence))

	return &CorrelatorOutput{
		CorrelatorName:    d.Name(),
		CorrelatorVersion: d.Version(),
		Findings:          findings,
		Context:           contextMap,
		Confidence:        confidence,
		ProcessingTime:    time.Since(startTime),
		Timestamp:         time.Now(),
	}, nil
}

// logCorrelationStart logs the start of correlation processing
func (d *DependencyCorrelator) logCorrelationStart(event *domain.UnifiedEvent) {
	d.logger.Debug("Processing dependency correlation",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("namespace", d.getNamespace(event)),
		zap.String("entity", d.getEntityName(event)))
}

// routeEventToHandler routes events to appropriate correlation handlers
func (d *DependencyCorrelator) routeEventToHandler(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	switch event.Type {
	case "service_unavailable", "endpoint_not_ready":
		return d.correlateServiceIssues(ctx, event)
	case "pod_failed", "container_crash":
		return d.correlatePodIssues(ctx, event)
	case "config_changed":
		return d.correlateConfigImpact(ctx, event)
	case "volume_mount_failed":
		return d.correlateVolumeIssues(ctx, event)
	default:
		return d.correlateGenericDependencies(ctx, event)
	}
}

// buildContextMap builds the context map for correlation output
func (d *DependencyCorrelator) buildContextMap(event *domain.UnifiedEvent) map[string]string {
	contextMap := map[string]string{
		"namespace":        d.getNamespace(event),
		"cluster":          d.getCluster(event),
		"correlation_type": "dependency",
		"event_type":       string(event.Type),
	}

	if entity := d.getEntityName(event); entity != "" {
		contextMap["entity"] = entity
	}

	return contextMap
}

// correlateServiceIssues analyzes service availability problems
func (d *DependencyCorrelator) correlateServiceIssues(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	// Create span for service correlation
	ctx, span := d.tracer.Start(ctx, "correlation.dependency.service_issues")
	defer span.End()

	namespace := d.getNamespace(event)
	serviceName := d.getEntityName(event)

	// Set span attributes
	span.SetAttributes(
		attribute.String("correlation.type", "service_issues"),
		attribute.String("service", serviceName),
		attribute.String("namespace", namespace),
	)

	if serviceName == "" {
		err := fmt.Errorf("service name not found in event")
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "missing_service_name"),
		)
		return nil, err
	}

	d.logger.Debug("Correlating service issues",
		zap.String("service", serviceName),
		zap.String("namespace", namespace))

	// Query service dependencies
	result, err := d.queryServiceDependencies(ctx, namespace, serviceName)
	if err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "query_failed"),
		)
		return nil, err
	}
	defer result.Close(ctx)

	var findings []Finding
	for result.Next(ctx) {
		record := result.Record()

		// Check service existence
		if serviceFinding := d.checkServiceExistence(record, serviceName, namespace, event); serviceFinding != nil {
			findings = append(findings, *serviceFinding)
			continue
		}

		// Check pod availability and health
		if podFindings := d.analyzePodAvailability(record, serviceName, namespace, event); len(podFindings) > 0 {
			findings = append(findings, podFindings...)
		}
	}

	if err = result.Err(); err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "iteration_failed"),
		)
		return nil, fmt.Errorf("error iterating service dependency results: %w", err)
	}

	// Record findings count in span
	span.SetAttributes(attribute.Int("findings.count", len(findings)))

	return findings, nil
}

// correlatePodIssues analyzes pod failure dependencies
func (d *DependencyCorrelator) correlatePodIssues(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	namespace := d.getNamespace(event)
	podName := d.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	d.logger.Debug("Correlating pod issues",
		zap.String("pod", podName),
		zap.String("namespace", namespace))

	// Query pod dependencies
	result, err := d.queryPodDependencies(ctx, namespace, podName)
	if err != nil {
		return nil, err
	}
	defer result.Close(ctx)

	var findings []Finding
	for result.Next(ctx) {
		record := result.Record()

		// Check pod existence
		if podFinding := d.checkPodExistence(record, podName, namespace, event); podFinding != nil {
			findings = append(findings, *podFinding)
			continue
		}

		// Check ConfigMap dependencies
		if configFindings := d.analyzeConfigMapDependencies(record, podName, namespace, event); len(configFindings) > 0 {
			findings = append(findings, configFindings...)
		}

		// Check service impact
		if serviceFindings := d.analyzeServiceImpact(record, podName, event); len(serviceFindings) > 0 {
			findings = append(findings, serviceFindings...)
		}
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pod dependency results: %w", err)
	}

	return findings, nil
}

// checkPodExistence checks if a pod exists and returns appropriate finding
func (d *DependencyCorrelator) checkPodExistence(record *GraphRecord, podName, namespace string, event *domain.UnifiedEvent) *Finding {
	podNode, err := record.GetNode("p")
	if err != nil || podNode == nil {
		return &Finding{
			ID:         fmt.Sprintf("pod-not-found-%s", podName),
			Type:       "pod_not_found",
			Severity:   SeverityHigh,
			Confidence: 0.95,
			Message:    fmt.Sprintf("Pod %s not found in namespace %s", podName, namespace),
			Evidence: Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: Impact{
				Scope:       "pod",
				Resources:   []string{podName},
				UserImpact:  "Pod unavailable",
				Degradation: PodNotExistMsg,
			},
			Timestamp: time.Now(),
		}
	}
	return nil
}

// analyzeConfigMapDependencies analyzes ConfigMap dependencies using typed API
func (d *DependencyCorrelator) analyzeConfigMapDependencies(record *GraphRecord, podName, namespace string, event *domain.UnifiedEvent) []Finding {
	configmaps, err := record.GetNodes("configmaps")
	if err != nil || len(configmaps) == 0 {
		return nil
	}

	var findings []Finding
	for _, cm := range configmaps {
		if finding := d.checkConfigMapModificationTiming(cm, podName, namespace, event); finding != nil {
			findings = append(findings, *finding)
		}
	}

	return findings
}

// checkConfigMapModificationTiming checks if ConfigMap was modified recently before pod failure
func (d *DependencyCorrelator) checkConfigMapModificationTiming(cm GraphNode, podName, namespace string, event *domain.UnifiedEvent) *Finding {
	cmName := cm.Properties.Name
	if cmName == "" {
		return nil
	}

	// Check if ConfigMap was modified recently
	_, valid := d.extractAndValidateModificationTime(cm, event)
	if !valid {
		return nil
	}

	return d.createConfigMapTimingFinding(podName, cmName, namespace, event)
}

// extractAndValidateModificationTime extracts and validates ConfigMap modification time
func (d *DependencyCorrelator) extractAndValidateModificationTime(cm GraphNode, event *domain.UnifiedEvent) (time.Time, bool) {
	lastModifiedStr, exists := cm.Properties.Metadata["lastModified"]
	if !exists {
		return time.Time{}, false
	}

	modTime, err := time.Parse(time.RFC3339, lastModifiedStr)
	if err != nil {
		return time.Time{}, false
	}

	// Check timing - ConfigMap modified recently before pod failure
	if event.Timestamp.Sub(modTime) >= 30*time.Minute || !event.Timestamp.After(modTime) {
		return time.Time{}, false
	}

	return modTime, true
}

// createConfigMapTimingFinding creates a finding for ConfigMap timing correlation
func (d *DependencyCorrelator) createConfigMapTimingFinding(podName, cmName, namespace string, event *domain.UnifiedEvent) *Finding {
	return &Finding{
		ID:         fmt.Sprintf("pod-config-dependency-%s-%s", podName, cmName),
		Type:       "pod_config_dependency_failure",
		Severity:   SeverityHigh,
		Confidence: 0.80,
		Message:    fmt.Sprintf("Pod %s failed after ConfigMap %s was modified", podName, cmName),
		Evidence:   d.buildConfigMapEvidence(podName, cmName, namespace, event),
		Impact: Impact{
			Scope:       "pod",
			Resources:   []string{podName, cmName},
			UserImpact:  "Pod failed due to configuration change",
			Degradation: "100% - pod crash",
		},
		Timestamp: time.Now(),
	}
}

// buildConfigMapEvidence builds evidence for ConfigMap correlation
func (d *DependencyCorrelator) buildConfigMapEvidence(podName, cmName, namespace string, event *domain.UnifiedEvent) Evidence {
	return Evidence{
		Events: []domain.UnifiedEvent{*event},
		GraphPaths: []EvidenceGraphPath{{
			Nodes: []EvidenceGraphNode{
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
			Edges: []GraphEdge{{
				From:         podName,
				To:           cmName,
				Relationship: "MOUNTS",
			}},
		}},
	}
}

// correlateConfigImpact analyzes configuration change impacts
func (d *DependencyCorrelator) correlateConfigImpact(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	namespace := d.getNamespace(event)
	configName := d.getEntityName(event)

	if configName == "" {
		return nil, fmt.Errorf("config name not found in event")
	}

	d.logger.Debug("Correlating config impact",
		zap.String("config", configName),
		zap.String("namespace", namespace))

	result, err := d.queryConfigDependencies(ctx, namespace, configName)
	if err != nil {
		return nil, err
	}
	defer result.Close(ctx)

	var findings []Finding
	for result.Next(ctx) {
		record := result.Record()
		podFindings := d.analyzeConfigImpactOnPods(record, configName, event)
		findings = append(findings, podFindings...)
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating config dependency results: %w", err)
	}

	return findings, nil
}

// queryConfigDependencies queries for config and its dependent resources
func (d *DependencyCorrelator) queryConfigDependencies(ctx context.Context, namespace, configName string) (ResultIterator, error) {
	limit := d.queryConfig.GetLimit("config")
	query := fmt.Sprintf(`
		MATCH (cm:ConfigMap {name: $configName, namespace: $namespace})
		OPTIONAL MATCH (p:Pod)-[:MOUNTS]->(cm)
		OPTIONAL MATCH (svc:Service)-[:SELECTS]->(p)
		RETURN cm,
		       collect(DISTINCT p)[0..%d] as pods,
		       collect(DISTINCT svc)[0..%d] as services
		LIMIT 1
	`, limit, limit)

	configType := d.inferConfigType(configName)
	params := &ConfigQueryParams{
		BaseQueryParams: BaseQueryParams{Namespace: namespace},
		ConfigName:      configName,
		ConfigType:      configType,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := d.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query config dependencies: %w", err)
	}

	return result, nil
}

// inferConfigType determines if config is a ConfigMap or Secret
func (d *DependencyCorrelator) inferConfigType(configName string) string {
	if strings.Contains(configName, "secret") {
		return "Secret"
	}
	return "ConfigMap"
}

// analyzeConfigImpactOnPods analyzes how config changes affect pods
func (d *DependencyCorrelator) analyzeConfigImpactOnPods(record *GraphRecord, configName string, event *domain.UnifiedEvent) []Finding {
	var findings []Finding

	pods, err := record.GetNodes("pods")
	if err != nil || len(pods) == 0 {
		return findings
	}

	podImpact := d.calculatePodImpact(pods, event)
	if podImpact.affectedCount > 0 {
		findings = append(findings, d.createPodImpactFinding(configName, podImpact, event))
	}

	serviceFindings := d.analyzeServiceImpactFromConfig(record, configName, podImpact.affectedCount, event)
	findings = append(findings, serviceFindings...)

	return findings
}

// podImpactInfo holds information about pods affected by config changes
type podImpactInfo struct {
	affectedCount int
	podNames      []string
}

// calculatePodImpact determines which pods were affected by config change
func (d *DependencyCorrelator) calculatePodImpact(pods []GraphNode, event *domain.UnifiedEvent) podImpactInfo {
	var podNames []string
	affectedPods := 0

	for _, pod := range pods {
		podName := pod.Properties.Name
		if podName != "" {
			podNames = append(podNames, podName)
		}

		if d.wasPodRestartedAfterEvent(pod, event) {
			affectedPods++
		}
	}

	return podImpactInfo{
		affectedCount: affectedPods,
		podNames:      podNames,
	}
}

// wasPodRestartedAfterEvent checks if pod restarted after the event
func (d *DependencyCorrelator) wasPodRestartedAfterEvent(pod GraphNode, event *domain.UnifiedEvent) bool {
	lastRestartStr, exists := pod.Properties.Metadata["lastRestart"]
	if !exists {
		return false
	}

	restartTime, err := time.Parse(time.RFC3339, lastRestartStr)
	if err != nil {
		return false
	}

	return restartTime.After(event.Timestamp) && restartTime.Sub(event.Timestamp) < 10*time.Minute
}

// createPodImpactFinding creates a finding for pods affected by config change
func (d *DependencyCorrelator) createPodImpactFinding(configName string, impact podImpactInfo, event *domain.UnifiedEvent) Finding {
	affectedPodNames := impact.podNames
	if len(affectedPodNames) > impact.affectedCount {
		affectedPodNames = affectedPodNames[:impact.affectedCount]
	}

	return Finding{
		ID:         fmt.Sprintf("config-change-impact-%s", configName),
		Type:       "config_change_pod_impact",
		Severity:   SeverityHigh,
		Confidence: 0.85,
		Message:    fmt.Sprintf("ConfigMap %s change caused %d pods to restart: %s", configName, impact.affectedCount, strings.Join(affectedPodNames, ", ")),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "config",
			Resources:   append([]string{configName}, impact.podNames...),
			UserImpact:  "Application restarts due to config change",
			Degradation: fmt.Sprintf("%d pods restarted", impact.affectedCount),
		},
		Timestamp: time.Now(),
	}
}

// analyzeServiceImpactFromConfig analyzes service impact from config changes
func (d *DependencyCorrelator) analyzeServiceImpactFromConfig(record *GraphRecord, configName string, affectedPods int, event *domain.UnifiedEvent) []Finding {
	if affectedPods == 0 {
		return nil
	}

	serviceNames := d.extractServiceNames(record, configName)
	if len(serviceNames) == 0 {
		return nil
	}

	return []Finding{{
		ID:         fmt.Sprintf("config-change-service-impact-%s", configName),
		Type:       "config_change_service_impact",
		Severity:   SeverityMedium,
		Confidence: 0.75,
		Message:    fmt.Sprintf("ConfigMap %s change impacted services: %s", configName, strings.Join(serviceNames, ", ")),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "service",
			Resources:   append([]string{configName}, serviceNames...),
			UserImpact:  "Service interruption during pod restarts",
			Degradation: "Temporary - rolling restart",
		},
		Timestamp: time.Now(),
	}}
}

// extractServiceNames extracts service names from record
func (d *DependencyCorrelator) extractServiceNames(record *GraphRecord, configName string) []string {
	servicesInterface, _ := record.Get("services")
	services, ok := servicesInterface.([]interface{})
	if !ok || len(services) == 0 {
		return nil
	}

	var serviceNames []string
	for _, svcInterface := range services {
		svcName := d.extractSingleServiceName(svcInterface, configName)
		if svcName != "" {
			serviceNames = append(serviceNames, svcName)
		}
	}
	return serviceNames
}

// extractSingleServiceName extracts a service name from interface
func (d *DependencyCorrelator) extractSingleServiceName(svcInterface interface{}, configName string) string {
	svc, ok := svcInterface.(map[string]interface{})
	if !ok {
		return ""
	}

	if props, ok := svc["properties"].(map[string]interface{}); ok {
		if name, ok := props["name"].(string); ok {
			return name
		}
		d.logger.Warn("Failed to extract service name from properties",
			zap.String("config", configName),
			zap.Any("props", props))
	}

	if name, ok := svc["name"].(string); ok {
		return name
	}

	return ""
}

// correlateVolumeIssues analyzes volume mount failures
func (d *DependencyCorrelator) correlateVolumeIssues(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	namespace := d.getNamespace(event)
	podName := d.getEntityName(event)

	if podName == "" {
		return nil, fmt.Errorf("pod name not found in event")
	}

	d.logger.Debug("Correlating volume issues",
		zap.String("pod", podName),
		zap.String("namespace", namespace))

	result, err := d.queryVolumeDependencies(ctx, namespace, podName)
	if err != nil {
		return nil, err
	}
	defer result.Close(ctx)

	var findings []Finding
	for result.Next(ctx) {
		record := result.Record()
		pvcFindings := d.analyzePVCIssues(record, namespace, podName, event)
		findings = append(findings, pvcFindings...)
	}

	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating volume dependency results: %w", err)
	}

	return findings, nil
}

// queryVolumeDependencies queries for pod volume dependencies
func (d *DependencyCorrelator) queryVolumeDependencies(ctx context.Context, namespace, podName string) (ResultIterator, error) {
	limit := d.queryConfig.GetLimit("pod")
	query := fmt.Sprintf(`
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (p)-[:CLAIMS]->(pvc:PVC)
		OPTIONAL MATCH (pvc)-[:USES]->(sc:StorageClass)
		RETURN p,
		       collect(DISTINCT pvc)[0..%d] as pvcs,
		       collect(DISTINCT sc)[0..%d] as storage_classes
		LIMIT 1
	`, limit, limit)

	params := &PodQueryParams{
		BaseQueryParams: BaseQueryParams{Namespace: namespace},
		PodName:         podName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := d.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query volume dependencies: %w", err)
	}

	return result, nil
}

// analyzePVCIssues analyzes PVC-related issues for a pod
func (d *DependencyCorrelator) analyzePVCIssues(record *GraphRecord, namespace, podName string, event *domain.UnifiedEvent) []Finding {
	pvcs, err := record.GetNodes("pvcs")
	if err != nil || len(pvcs) == 0 {
		return nil
	}

	var findings []Finding
	for _, pvc := range pvcs {
		pvcName := pvc.Properties.Name

		if pendingFinding := d.checkPendingPVC(pvc, namespace, podName, pvcName, event); pendingFinding != nil {
			findings = append(findings, *pendingFinding)
		}

		if storageClassFinding := d.checkStorageClass(record, namespace, podName, pvcName, event); storageClassFinding != nil {
			findings = append(findings, *storageClassFinding)
		}
	}

	return findings
}

// checkPendingPVC checks if PVC is in pending state
func (d *DependencyCorrelator) checkPendingPVC(pvc GraphNode, namespace, podName, pvcName string, event *domain.UnifiedEvent) *Finding {
	if pvc.Properties.Phase != "Pending" {
		return nil
	}

	return &Finding{
		ID:         fmt.Sprintf("pod-pvc-pending-%s-%s", podName, pvcName),
		Type:       "pod_volume_pending",
		Severity:   SeverityCritical,
		Confidence: 0.90,
		Message:    fmt.Sprintf("Pod %s cannot start because PVC %s is pending", podName, pvcName),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
			GraphPaths: []EvidenceGraphPath{{
				Nodes: []EvidenceGraphNode{
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
				Edges: []GraphEdge{{
					From:         podName,
					To:           pvcName,
					Relationship: "CLAIMS",
				}},
			}},
		},
		Impact: Impact{
			Scope:       "pod",
			Resources:   []string{podName, pvcName},
			UserImpact:  "Pod cannot start due to volume issue",
			Degradation: "100% - pod stuck pending",
		},
		Timestamp: time.Now(),
	}
}

// checkStorageClass checks if storage class is configured
func (d *DependencyCorrelator) checkStorageClass(record *GraphRecord, namespace, podName, pvcName string, event *domain.UnifiedEvent) *Finding {
	storageClassesInterface, _ := record.Get("storage_classes")
	scs, ok := storageClassesInterface.([]interface{})
	if !ok || len(scs) > 0 {
		return nil
	}

	return &Finding{
		ID:         fmt.Sprintf("pod-pvc-no-storage-class-%s-%s", podName, pvcName),
		Type:       "pod_volume_no_storage_class",
		Severity:   SeverityHigh,
		Confidence: 0.80,
		Message:    fmt.Sprintf("Pod %s PVC %s has no storage class configured", podName, pvcName),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "pod",
			Resources:   []string{podName, pvcName},
			UserImpact:  "Pod cannot get persistent storage",
			Degradation: "100% - volume provisioning failed",
		},
		Timestamp: time.Now(),
	}
}

// correlateGenericDependencies performs general dependency analysis
func (d *DependencyCorrelator) correlateGenericDependencies(ctx context.Context, event *domain.UnifiedEvent) ([]Finding, error) {
	// For now, return empty findings for unknown event types
	// This can be extended later for generic dependency patterns
	d.logger.Debug("Generic dependency correlation",
		zap.String("event_type", string(event.Type)),
		zap.String("event_id", event.ID))

	return []Finding{}, nil
}

// calculateConfidence determines overall confidence based on findings
func (d *DependencyCorrelator) calculateConfidence(findings []Finding, event *domain.UnifiedEvent) float64 {
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
		case SeverityCritical:
			weight = 1.0
		case SeverityHigh:
			weight = 0.8
		case SeverityMedium:
			weight = 0.6
		case SeverityLow:
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
	return d.graphStore.HealthCheck(ctx)
}

// SetGraphClient implements GraphCorrelator interface
func (d *DependencyCorrelator) SetGraphClient(client interface{}) {
	// This method is no longer needed as we use GraphStore interface
	// The graphStore is injected via constructor
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

// queryPodDependencies executes the pod dependency query
func (d *DependencyCorrelator) queryPodDependencies(ctx context.Context, namespace, podName string) (ResultIterator, error) {
	limit := d.queryConfig.GetLimit("pod")
	query := fmt.Sprintf(`
		MATCH (p:Pod {name: $podName, namespace: $namespace})
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		OPTIONAL MATCH (p)-[:USES_SECRET]->(sec:Secret)
		OPTIONAL MATCH (p)-[:CLAIMS]->(pvc:PVC)
		OPTIONAL MATCH (svc:Service)-[:SELECTS]->(p)
		RETURN p, 
		       collect(DISTINCT cm)[0..%d] as configmaps,
		       collect(DISTINCT sec)[0..%d] as secrets,
		       collect(DISTINCT pvc)[0..%d] as pvcs,
		       collect(DISTINCT svc)[0..%d] as services
		LIMIT 1
	`, limit, limit, limit, limit)

	params := &PodQueryParams{
		BaseQueryParams: BaseQueryParams{Namespace: namespace},
		PodName:         podName,
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	result, err := d.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query pod dependencies: %w", err)
	}

	return result, nil
}

// analyzeServiceImpact analyzes service impact from pod failures
func (d *DependencyCorrelator) analyzeServiceImpact(record *GraphRecord, podName string, event *domain.UnifiedEvent) []Finding {
	services, err := record.GetNodes("services")
	if err != nil || len(services) == 0 {
		return nil
	}

	var serviceNames []string
	for _, service := range services {
		if service.Properties.Name != "" {
			serviceNames = append(serviceNames, service.Properties.Name)
		}
	}

	if len(serviceNames) == 0 {
		return nil
	}

	return []Finding{{
		ID:         fmt.Sprintf("pod-service-impact-%s", podName),
		Type:       "pod_failure_service_impact",
		Severity:   SeverityMedium,
		Confidence: 0.70,
		Message:    fmt.Sprintf("Pod %s failure impacts services: %s", podName, strings.Join(serviceNames, ", ")),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "service",
			Resources:   append([]string{podName}, serviceNames...),
			UserImpact:  "Service availability reduced",
			Degradation: "Partial - one endpoint lost",
		},
		Timestamp: time.Now(),
	}}
}

// queryServiceDependencies executes the service dependency query
func (d *DependencyCorrelator) queryServiceDependencies(ctx context.Context, namespace, serviceName string) (ResultIterator, error) {
	// Create span for query operation
	ctx, span := d.tracer.Start(ctx, "correlation.dependency.query_service")
	defer span.End()

	queryStart := time.Now()
	defer func() {
		// Record query duration
		duration := time.Since(queryStart).Seconds() * 1000 // Convert to milliseconds
		if d.queryDurationHist != nil {
			d.queryDurationHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("query_type", "service_dependencies"),
			))
		}
	}()

	// Set span attributes
	span.SetAttributes(
		attribute.String("query.type", "service_dependencies"),
		attribute.String("service", serviceName),
		attribute.String("namespace", namespace),
	)

	limit := d.queryConfig.GetLimit("service")
	query := fmt.Sprintf(`
		MATCH (s:Service {name: $serviceName, namespace: $namespace})
		OPTIONAL MATCH (s)-[:SELECTS]->(p:Pod)
		OPTIONAL MATCH (p)-[:MOUNTS]->(cm:ConfigMap)
		OPTIONAL MATCH (p)-[:USES_SECRET]->(sec:Secret)
		OPTIONAL MATCH (p)-[:CLAIMS]->(pvc:PVC)
		RETURN s, 
		       collect(DISTINCT p)[0..%d] as pods,
		       collect(DISTINCT cm)[0..%d] as configmaps,
		       collect(DISTINCT sec)[0..%d] as secrets,
		       collect(DISTINCT pvc)[0..%d] as pvcs
		LIMIT %d
	`, limit, limit, limit, limit, limit)

	params := &ServiceQueryParams{
		BaseQueryParams: BaseQueryParams{Namespace: namespace},
		ServiceName:     serviceName,
	}
	if err := params.Validate(); err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "validation_failed"),
		)
		return nil, err
	}

	result, err := d.graphStore.ExecuteQuery(ctx, query, params)
	if err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "query_failed"),
		)
		// Record error metrics
		if d.errorsTotalCtr != nil {
			d.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "query_failed"),
				attribute.String("query_type", "service_dependencies"),
			))
		}
		return nil, fmt.Errorf("failed to query service dependencies: %w", err)
	}

	return result, nil
}

// checkServiceExistence checks if a service exists and returns appropriate finding
func (d *DependencyCorrelator) checkServiceExistence(record *GraphRecord, serviceName, namespace string, event *domain.UnifiedEvent) *Finding {
	serviceNode, err := record.GetNode("s")
	if err != nil || serviceNode == nil {
		return &Finding{
			ID:         fmt.Sprintf("service-not-found-%s", serviceName),
			Type:       "service_not_found",
			Severity:   SeverityCritical,
			Confidence: 0.95,
			Message:    fmt.Sprintf("Service %s not found in namespace %s", serviceName, namespace),
			Evidence: Evidence{
				Events: []domain.UnifiedEvent{*event},
			},
			Impact: Impact{
				Scope:       "service",
				Resources:   []string{serviceName},
				UserImpact:  "Service completely unavailable",
				Degradation: ServiceNotExistMsg,
			},
			Timestamp: time.Now(),
		}
	}
	return nil
}

// analyzePodAvailability analyzes pod availability for a service
func (d *DependencyCorrelator) analyzePodAvailability(record *GraphRecord, serviceName, namespace string, event *domain.UnifiedEvent) []Finding {
	pods, err := record.GetNodes("pods")
	if err != nil || len(pods) == 0 {
		return []Finding{d.createNoPodsAvailableFinding(serviceName, namespace, event)}
	}

	// Analyze pod health metrics
	readyPods, failedPods := d.calculatePodHealth(pods)

	// Generate findings based on pod health
	return d.createPodHealthFindings(serviceName, readyPods, failedPods, len(pods), event)
}

// createNoPodsAvailableFinding creates finding when no pods are available
func (d *DependencyCorrelator) createNoPodsAvailableFinding(serviceName, namespace string, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("service-no-pods-%s", serviceName),
		Type:       "service_no_endpoints",
		Severity:   SeverityCritical,
		Confidence: 0.90,
		Message:    fmt.Sprintf("Service %s has no running pods", serviceName),
		Evidence:   d.createServiceEvidence(serviceName, namespace, event),
		Impact: Impact{
			Scope:       "service",
			Resources:   []string{serviceName},
			UserImpact:  "Service has no endpoints",
			Degradation: NoPodsAvailableMsg,
		},
		Timestamp: time.Now(),
	}
}

// createServiceEvidence creates evidence for service-related findings
func (d *DependencyCorrelator) createServiceEvidence(serviceName, namespace string, event *domain.UnifiedEvent) Evidence {
	return Evidence{
		Events: []domain.UnifiedEvent{*event},
		GraphPaths: []EvidenceGraphPath{{
			Nodes: []EvidenceGraphNode{{
				ID:   serviceName,
				Type: "Service",
				Labels: map[string]string{
					"name":      serviceName,
					"namespace": namespace,
				},
			}},
		}},
	}
}

// calculatePodHealth calculates ready and failed pod counts
func (d *DependencyCorrelator) calculatePodHealth(pods []GraphNode) (readyPods, failedPods int) {
	for _, pod := range pods {
		if pod.Properties.Ready {
			readyPods++
		} else {
			failedPods++
		}
	}
	return readyPods, failedPods
}

// createPodHealthFindings creates findings based on pod health status
func (d *DependencyCorrelator) createPodHealthFindings(serviceName string, readyPods, failedPods, totalPods int, event *domain.UnifiedEvent) []Finding {
	var findings []Finding

	if readyPods == 0 && failedPods > 0 {
		findings = append(findings, d.createAllPodsFailedFinding(serviceName, failedPods, event))
	} else if readyPods < totalPods/2 {
		findings = append(findings, d.createDegradedServiceFinding(serviceName, readyPods, totalPods, event))
	}

	return findings
}

// createAllPodsFailedFinding creates finding when all pods have failed
func (d *DependencyCorrelator) createAllPodsFailedFinding(serviceName string, failedPods int, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("service-pods-failed-%s", serviceName),
		Type:       "service_endpoints_failed",
		Severity:   SeverityCritical,
		Confidence: 0.85,
		Message:    fmt.Sprintf("Service %s has %d failed pods, 0 ready", serviceName, failedPods),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "service",
			Resources:   []string{serviceName},
			UserImpact:  "Service unavailable due to pod failures",
			Degradation: AllPodsFailedMsg,
		},
		Timestamp: time.Now(),
	}
}

// createDegradedServiceFinding creates finding when service is degraded
func (d *DependencyCorrelator) createDegradedServiceFinding(serviceName string, readyPods, totalPods int, event *domain.UnifiedEvent) Finding {
	return Finding{
		ID:         fmt.Sprintf("service-pods-degraded-%s", serviceName),
		Type:       "service_endpoints_degraded",
		Severity:   SeverityHigh,
		Confidence: 0.75,
		Message:    fmt.Sprintf("Service %s has only %d/%d pods ready", serviceName, readyPods, totalPods),
		Evidence: Evidence{
			Events: []domain.UnifiedEvent{*event},
		},
		Impact: Impact{
			Scope:       "service",
			Resources:   []string{serviceName},
			UserImpact:  "Service degraded due to pod failures",
			Degradation: fmt.Sprintf(ReducedCapacityFmt, (readyPods*100)/totalPods),
		},
		Timestamp: time.Now(),
	}
}
