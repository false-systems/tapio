package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yairfalse/tapio/pkg/events_correlation"
)

// StorageVolumeCorrelation detects storage and volume-related issues
func StorageVolumeCorrelation() *events_correlation.Rule {
	return &events_correlation.Rule{
		ID:          "storage-volume-correlation",
		Name:        "Storage & Volume Correlation",
		Description: "Detects PVC issues, disk pressure, storage quotas, and volume mount problems with intelligent root cause analysis",
		Category:    events_correlation.CategoryStorage,
		Tags:        []string{"storage", "volume", "pvc", "disk", "quota", "mount", "kubernetes"},
		
		RequiredSources: []events_correlation.EventSource{
			events_correlation.SourceKubernetes,
		},
		OptionalSources: []events_correlation.EventSource{
			events_correlation.SourceEBPF,
			events_correlation.SourceSystemd,
			events_correlation.SourceMetrics,
		},
		
		MinConfidence: 0.7,
		
		Evaluate: func(ctx *events_correlation.Context) *events_correlation.Result {
			return evaluateStorageIssues(ctx)
		},
	}
}

// PVCIssueDetection specifically detects PersistentVolumeClaim problems
func PVCIssueDetection() *events_correlation.Rule {
	return &events_correlation.Rule{
		ID:          "pvc-issue-detection",
		Name:        "PVC Issue Detection",
		Description: "Detects PersistentVolumeClaim binding failures, provisioning errors, and access issues",
		Category:    events_correlation.CategoryStorage,
		Tags:        []string{"pvc", "persistent-volume", "storage", "kubernetes"},
		
		RequiredSources: []events_correlation.EventSource{
			events_correlation.SourceKubernetes,
		},
		OptionalSources: []events_correlation.EventSource{
			events_correlation.SourceMetrics,
		},
		
		MinConfidence: 0.8,
		
		Evaluate: func(ctx *events_correlation.Context) *events_correlation.Result {
			return evaluatePVCIssues(ctx)
		},
	}
}

// DiskPressureDetection identifies disk pressure situations
func DiskPressureDetection() *events_correlation.Rule {
	return &events_correlation.Rule{
		ID:          "disk-pressure-detection",
		Name:        "Disk Pressure Detection",
		Description: "Detects disk pressure conditions that can cause pod evictions and application failures",
		Category:    events_correlation.CategoryStorage,
		Tags:        []string{"disk", "pressure", "eviction", "node", "kubernetes"},
		
		RequiredSources: []events_correlation.EventSource{
			events_correlation.SourceKubernetes,
		},
		OptionalSources: []events_correlation.EventSource{
			events_correlation.SourceEBPF,
			events_correlation.SourceSystemd,
			events_correlation.SourceMetrics,
		},
		
		MinConfidence: 0.75,
		
		Evaluate: func(ctx *events_correlation.Context) *events_correlation.Result {
			return evaluateDiskPressure(ctx)
		},
	}
}

// StorageQuotaViolation detects storage quota violations
func StorageQuotaViolation() *events_correlation.Rule {
	return &events_correlation.Rule{
		ID:          "storage-quota-violation",
		Name:        "Storage Quota Violation",
		Description: "Detects storage quota violations and capacity limits that prevent normal operations",
		Category:    events_correlation.CategoryStorage,
		Tags:        []string{"quota", "storage", "capacity", "limit", "kubernetes"},
		
		RequiredSources: []events_correlation.EventSource{
			events_correlation.SourceKubernetes,
		},
		OptionalSources: []events_correlation.EventSource{
			events_correlation.SourceMetrics,
		},
		
		MinConfidence: 0.8,
		
		Evaluate: func(ctx *events_correlation.Context) *events_correlation.Result {
			return evaluateStorageQuotaViolations(ctx)
		},
	}
}

func evaluateStorageIssues(ctx *events_correlation.Context) *events_correlation.Result {
	storageEvents := filterStorageEvents(ctx.GetEvents(events_correlation.Filter{}))
	if len(storageEvents) == 0 {
		return nil
	}

	// Check for various storage issue patterns
	pvcIssues := analyzePVCEvents(storageEvents)
	diskPressure := analyzeDiskPressureEvents(storageEvents)
	quotaViolations := analyzeQuotaEvents(storageEvents)
	mountIssues := analyzeMountEvents(storageEvents)

	if len(pvcIssues) == 0 && len(diskPressure) == 0 && len(quotaViolations) == 0 && len(mountIssues) == 0 {
		return nil
	}

	// Calculate confidence based on multiple factors
	confidence := calculateStorageConfidence(pvcIssues, diskPressure, quotaViolations, mountIssues)
	
	// Generate detailed recommendations
	recommendations := generateStorageRecommendations(pvcIssues, diskPressure, quotaViolations, mountIssues)
	
	// Determine root cause
	rootCause := determineStorageRootCause(pvcIssues, diskPressure, quotaViolations, mountIssues)
	
	return &events_correlation.Result{
		RuleID:      "storage-volume-correlation",
		Confidence:  confidence,
		Title:       generateStorageSummary(pvcIssues, diskPressure, quotaViolations, mountIssues),
		Description: rootCause,
		Impact:      assessStorageImpact(pvcIssues, diskPressure, quotaViolations, mountIssues),
		Evidence: events_correlation.Evidence{
			Events: storageEvents,
		},
		Recommendations: recommendations,
		Metadata: map[string]string{
			"pvc_issues":       fmt.Sprintf("%d", len(pvcIssues)),
			"disk_pressure":    fmt.Sprintf("%d", len(diskPressure)),
			"quota_violations": fmt.Sprintf("%d", len(quotaViolations)),
			"mount_issues":     fmt.Sprintf("%d", len(mountIssues)),
			"analysis_type":    "comprehensive_storage_analysis",
		},
	}
}

func evaluatePVCIssues(ctx *events_correlation.Context) *events_correlation.Result {
	pvcEvents := filterPVCEvents(ctx.GetEvents(events_correlation.Filter{}))
	if len(pvcEvents) == 0 {
		return nil
	}

	issues := analyzePVCEvents(pvcEvents)
	if len(issues) == 0 {
		return nil
	}

	confidence := calculatePVCConfidence(issues)
	
	return &events_correlation.Result{
		RuleID:      "pvc-issue-detection",
		Confidence:  confidence,
		Title:       fmt.Sprintf("Detected %d PVC issues affecting storage operations", len(issues)),
		Description: determinePVCRootCause(issues),
		Impact:      "PVC issues can prevent pod startup and cause application failures",
		Evidence: events_correlation.Evidence{
			Events: pvcEvents,
		},
		Recommendations: generatePVCRecommendations(issues),
		Metadata: map[string]string{
			"pvc_issues":    fmt.Sprintf("%d", len(issues)),
			"analysis_type": "pvc_specific_analysis",
		},
	}
}

func evaluateDiskPressure(ctx *events_correlation.Context) *events_correlation.Result {
	pressureEvents := filterDiskPressureEvents(ctx.GetEvents(events_correlation.Filter{}))
	if len(pressureEvents) == 0 {
		return nil
	}

	issues := analyzeDiskPressureEvents(pressureEvents)
	if len(issues) == 0 {
		return nil
	}

	confidence := calculateDiskPressureConfidence(issues)
	
	return &events_correlation.Result{
		RuleID:      "disk-pressure-detection",
		Confidence:  confidence,
		Title:       fmt.Sprintf("Detected disk pressure on %d nodes affecting pod scheduling", len(issues)),
		Description: "Node disk pressure causing pod evictions and scheduling failures",
		Impact:      "Disk pressure can cause pod evictions and prevent new pod scheduling",
		Evidence: events_correlation.Evidence{
			Events: pressureEvents,
		},
		Recommendations: generateDiskPressureRecommendations(issues),
		Metadata: map[string]string{
			"pressure_nodes": fmt.Sprintf("%d", len(issues)),
			"analysis_type":  "disk_pressure_analysis",
		},
	}
}

func evaluateStorageQuotaViolations(ctx *events_correlation.Context) *events_correlation.Result {
	quotaEvents := filterQuotaEvents(ctx.GetEvents(events_correlation.Filter{}))
	if len(quotaEvents) == 0 {
		return nil
	}

	violations := analyzeQuotaEvents(quotaEvents)
	if len(violations) == 0 {
		return nil
	}

	confidence := calculateQuotaConfidence(violations)
	
	return &events_correlation.Result{
		RuleID:      "storage-quota-violation",
		Confidence:  confidence,
		Title:       fmt.Sprintf("Detected %d storage quota violations preventing operations", len(violations)),
		Description: "Storage quota limits exceeded for namespace or cluster resources",
		Impact:      "Quota violations prevent new storage allocation and can block deployments",
		Evidence: events_correlation.Evidence{
			Events: quotaEvents,
		},
		Recommendations: generateQuotaRecommendations(violations),
		Metadata: map[string]string{
			"quota_violations": fmt.Sprintf("%d", len(violations)),
			"analysis_type":    "quota_violation_analysis",
		},
	}
}

// Event filtering functions
func filterStorageEvents(events []events_correlation.Event) []events_correlation.Event {
	var storageEvents []events_correlation.Event
	
	storageKeywords := []string{
		"persistentvolume", "pvc", "pv", "storage", "volume", "mount",
		"disk", "quota", "capacity", "provisioning", "binding",
		"diskpressure", "evicted", "insufficient",
	}
	
	for _, event := range events {
		if event.Source == events_correlation.SourceKubernetes {
			eventText := strings.ToLower(event.Type + " " + fmt.Sprintf("%v", event.Attributes))
			for _, keyword := range storageKeywords {
				if strings.Contains(eventText, keyword) {
					storageEvents = append(storageEvents, event)
					break
				}
			}
		}
	}
	
	return storageEvents
}

func filterPVCEvents(events []events_correlation.Event) []events_correlation.Event {
	var pvcEvents []events_correlation.Event
	
	for _, event := range events {
		if event.Source == events_correlation.SourceKubernetes {
			eventText := strings.ToLower(event.Type + " " + fmt.Sprintf("%v", event.Attributes))
			if strings.Contains(eventText, "persistentvolumeclaim") || 
			   strings.Contains(eventText, "pvc") ||
			   strings.Contains(eventText, "persistentvolume") ||
			   strings.Contains(eventText, "pv") {
				pvcEvents = append(pvcEvents, event)
			}
		}
	}
	
	return pvcEvents
}

func filterDiskPressureEvents(events []events_correlation.Event) []events_correlation.Event {
	var pressureEvents []events_correlation.Event
	
	for _, event := range events {
		if event.Source == events_correlation.SourceKubernetes {
			eventText := strings.ToLower(event.Type + " " + fmt.Sprintf("%v", event.Attributes))
			if strings.Contains(eventText, "diskpressure") ||
			   strings.Contains(eventText, "disk pressure") ||
			   strings.Contains(eventText, "evicted") ||
			   strings.Contains(eventText, "insufficient disk") {
				pressureEvents = append(pressureEvents, event)
			}
		}
	}
	
	return pressureEvents
}

func filterQuotaEvents(events []events_correlation.Event) []events_correlation.Event {
	var quotaEvents []events_correlation.Event
	
	for _, event := range events {
		if event.Source == events_correlation.SourceKubernetes {
			eventText := strings.ToLower(event.Type + " " + fmt.Sprintf("%v", event.Attributes))
			if strings.Contains(eventText, "quota") ||
			   strings.Contains(eventText, "exceeded") ||
			   strings.Contains(eventText, "limit") ||
			   strings.Contains(eventText, "capacity") {
				quotaEvents = append(quotaEvents, event)
			}
		}
	}
	
	return quotaEvents
}

// Analysis functions
func analyzePVCEvents(events []events_correlation.Event) []map[string]interface{} {
	var issues []map[string]interface{}
	
	bindingFailures := regexp.MustCompile(`(?i)(binding|bound|failed|provision|timeout)`)
	accessIssues := regexp.MustCompile(`(?i)(permission|access|denied|unauthorized)`)
	
	for _, event := range events {
		issue := map[string]interface{}{
			"timestamp": event.Timestamp,
			"entity":    event.Entity.Name,
			"namespace": event.Entity.Namespace,
			"type":      event.Type,
		}
		
		eventStr := fmt.Sprintf("%v", event.Attributes)
		if bindingFailures.MatchString(eventStr) {
			issue["issue_type"] = "binding_failure"
			issue["severity"] = "high"
			issues = append(issues, issue)
		} else if accessIssues.MatchString(eventStr) {
			issue["issue_type"] = "access_issue"
			issue["severity"] = "medium"
			issues = append(issues, issue)
		}
	}
	
	return issues
}

func analyzeDiskPressureEvents(events []events_correlation.Event) []map[string]interface{} {
	var issues []map[string]interface{}
	
	for _, event := range events {
		eventStr := strings.ToLower(fmt.Sprintf("%v", event.Attributes))
		if strings.Contains(eventStr, "diskpressure") ||
		   strings.Contains(eventStr, "evicted") {
			issue := map[string]interface{}{
				"timestamp": event.Timestamp,
				"node":      event.Entity.Node,
				"type":      event.Type,
				"issue_type": "disk_pressure",
				"severity":  "high",
			}
			issues = append(issues, issue)
		}
	}
	
	return issues
}

func analyzeQuotaEvents(events []events_correlation.Event) []map[string]interface{} {
	var violations []map[string]interface{}
	
	quotaPattern := regexp.MustCompile(`(?i)(quota|exceeded|limit|capacity)`)
	
	for _, event := range events {
		eventStr := fmt.Sprintf("%v", event.Attributes)
		if quotaPattern.MatchString(eventStr) {
			violation := map[string]interface{}{
				"timestamp": event.Timestamp,
				"namespace": event.Entity.Namespace,
				"type":      event.Type,
				"issue_type": "quota_violation",
				"severity":  "medium",
			}
			violations = append(violations, violation)
		}
	}
	
	return violations
}

func analyzeMountEvents(events []events_correlation.Event) []map[string]interface{} {
	var issues []map[string]interface{}
	
	mountPattern := regexp.MustCompile(`(?i)(mount|volume|attach|detach|failed)`)
	
	for _, event := range events {
		eventStr := fmt.Sprintf("%v", event.Attributes)
		if mountPattern.MatchString(eventStr) {
			issue := map[string]interface{}{
				"timestamp": event.Timestamp,
				"entity":    event.Entity.Name,
				"type":      event.Type,
				"issue_type": "mount_issue",
				"severity":  "medium",
			}
			issues = append(issues, issue)
		}
	}
	
	return issues
}

// Confidence calculation functions
func calculateStorageConfidence(pvc, disk, quota, mount []map[string]interface{}) float64 {
	baseConfidence := 0.0
	
	if len(pvc) > 0 {
		baseConfidence += 0.3
	}
	if len(disk) > 0 {
		baseConfidence += 0.4
	}
	if len(quota) > 0 {
		baseConfidence += 0.2
	}
	if len(mount) > 0 {
		baseConfidence += 0.1
	}
	
	// Boost confidence if multiple types detected
	typesDetected := 0
	if len(pvc) > 0 { typesDetected++ }
	if len(disk) > 0 { typesDetected++ }
	if len(quota) > 0 { typesDetected++ }
	if len(mount) > 0 { typesDetected++ }
	
	if typesDetected > 1 {
		baseConfidence += 0.1 * float64(typesDetected-1)
	}
	
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	
	return baseConfidence
}

func calculatePVCConfidence(issues []map[string]interface{}) float64 {
	if len(issues) == 0 {
		return 0.0
	}
	
	highSeverityCount := 0
	for _, issue := range issues {
		if severity, ok := issue["severity"].(string); ok && severity == "high" {
			highSeverityCount++
		}
	}
	
	baseConfidence := 0.7
	if highSeverityCount > 0 {
		baseConfidence += 0.2
	}
	
	return baseConfidence
}

func calculateDiskPressureConfidence(issues []map[string]interface{}) float64 {
	if len(issues) == 0 {
		return 0.0
	}
	
	return 0.9 // Disk pressure events are highly reliable indicators
}

func calculateQuotaConfidence(violations []map[string]interface{}) float64 {
	if len(violations) == 0 {
		return 0.0
	}
	
	return 0.8 // Quota violations are clear indicators
}

// Root cause determination
func determineStorageRootCause(pvc, disk, quota, mount []map[string]interface{}) string {
	if len(disk) > 0 {
		return "Node disk pressure causing storage and scheduling issues"
	}
	if len(quota) > 0 {
		return "Storage quota limits preventing resource allocation"
	}
	if len(pvc) > 0 {
		return "PersistentVolumeClaim binding or provisioning failures"
	}
	if len(mount) > 0 {
		return "Volume mounting issues affecting pod startup"
	}
	
	return "Multiple storage-related issues detected"
}

func determinePVCRootCause(issues []map[string]interface{}) string {
	bindingFailures := 0
	accessIssues := 0
	
	for _, issue := range issues {
		if issueType, ok := issue["issue_type"].(string); ok {
			switch issueType {
			case "binding_failure":
				bindingFailures++
			case "access_issue":
				accessIssues++
			}
		}
	}
	
	if bindingFailures > accessIssues {
		return "PVC binding failures - storage class or provisioner issues"
	} else if accessIssues > 0 {
		return "PVC access permission issues - RBAC or security context problems"
	}
	
	return "Multiple PVC issues detected"
}

// Impact assessment
func assessStorageImpact(pvc, disk, quota, mount []map[string]interface{}) string {
	impacts := []string{}
	
	if len(disk) > 0 {
		impacts = append(impacts, "Pod evictions and scheduling failures")
	}
	if len(pvc) > 0 {
		impacts = append(impacts, "Application startup failures")
	}
	if len(quota) > 0 {
		impacts = append(impacts, "Deployment blocking")
	}
	if len(mount) > 0 {
		impacts = append(impacts, "Volume access issues")
	}
	
	if len(impacts) == 0 {
		return "Storage performance degradation"
	}
	
	return strings.Join(impacts, ", ")
}

// Recommendation generation
func generateStorageRecommendations(pvc, disk, quota, mount []map[string]interface{}) []string {
	recommendations := []string{}
	
	if len(disk) > 0 {
		recommendations = append(recommendations,
			"Clean up unused files and logs to free disk space",
			"Add more storage capacity to affected nodes",
			"Configure log rotation and cleanup policies",
			"Monitor disk usage proactively")
	}
	
	if len(pvc) > 0 {
		recommendations = append(recommendations,
			"Check storage class availability and configuration",
			"Verify storage provisioner is healthy",
			"Review PVC requests vs available capacity",
			"Check RBAC permissions for storage operations")
	}
	
	if len(quota) > 0 {
		recommendations = append(recommendations,
			"Review and adjust storage quotas based on actual needs",
			"Clean up unused PVCs and storage resources",
			"Implement storage usage monitoring",
			"Consider tiered storage strategies")
	}
	
	if len(mount) > 0 {
		recommendations = append(recommendations,
			"Verify volume mount paths and permissions",
			"Check security contexts and pod security policies",
			"Review storage class and volume settings",
			"Ensure nodes have proper storage drivers")
	}
	
	return recommendations
}

func generatePVCRecommendations(issues []map[string]interface{}) []string {
	recommendations := []string{
		"Verify storage class exists and is properly configured",
		"Check storage provisioner health and availability",
		"Review PVC size requests vs storage capacity",
		"Ensure proper RBAC permissions for storage operations",
		"Monitor PVC binding timeouts and retry mechanisms",
	}
	
	return recommendations
}

func generateDiskPressureRecommendations(issues []map[string]interface{}) []string {
	recommendations := []string{
		"Increase disk capacity on affected nodes",
		"Clean up container images and unused files",
		"Configure log rotation and cleanup policies",
		"Implement disk usage monitoring and alerting",
		"Consider adding taints/tolerations for storage workloads",
	}
	
	return recommendations
}

func generateQuotaRecommendations(violations []map[string]interface{}) []string {
	recommendations := []string{
		"Review storage quota limits vs actual usage patterns",
		"Clean up unused PVCs and persistent volumes",
		"Implement storage lifecycle management",
		"Consider increasing quotas for high-usage namespaces",
		"Set up monitoring for quota utilization trends",
	}
	
	return recommendations
}

// Summary generation
func generateStorageSummary(pvc, disk, quota, mount []map[string]interface{}) string {
	var parts []string
	
	if len(disk) > 0 {
		parts = append(parts, fmt.Sprintf("%d disk pressure events", len(disk)))
	}
	if len(pvc) > 0 {
		parts = append(parts, fmt.Sprintf("%d PVC issues", len(pvc)))
	}
	if len(quota) > 0 {
		parts = append(parts, fmt.Sprintf("%d quota violations", len(quota)))
	}
	if len(mount) > 0 {
		parts = append(parts, fmt.Sprintf("%d mount issues", len(mount)))
	}
	
	if len(parts) == 0 {
		return "Storage correlation analysis completed"
	}
	
	return "Detected: " + strings.Join(parts, ", ")
}