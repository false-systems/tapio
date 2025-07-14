package journald

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yairfalse/tapio/pkg/collectors/types"
)

// ContainerEventParser specializes in parsing container runtime failures
// Detects docker/containerd crashes, failed pulls, startup failures, etc.
type ContainerEventParser struct {
	// Container runtime patterns
	dockerFailurePattern     *regexp.Regexp
	containerdFailurePattern *regexp.Regexp
	crioFailurePattern       *regexp.Regexp
	
	// Specific failure patterns
	pullFailurePattern       *regexp.Regexp
	startFailurePattern      *regexp.Regexp
	execFailurePattern       *regexp.Regexp
	mountFailurePattern      *regexp.Regexp
	networkFailurePattern    *regexp.Regexp
	
	// Container ID extraction
	containerIDPattern       *regexp.Regexp
	imagePattern            *regexp.Regexp
	
	// Error detail extraction
	errorDetailPattern      *regexp.Regexp
	exitCodePattern         *regexp.Regexp
}

// NewContainerEventParser creates a parser for container runtime events
func NewContainerEventParser() *ContainerEventParser {
	return &ContainerEventParser{
		// Runtime failure patterns
		dockerFailurePattern: regexp.MustCompile(`(?i)docker.*?(error|failed|cannot|unable|panic|fatal)`),
		containerdFailurePattern: regexp.MustCompile(`(?i)containerd.*?(error|failed|cannot|unable|panic|fatal)`),
		crioFailurePattern: regexp.MustCompile(`(?i)crio.*?(error|failed|cannot|unable|panic|fatal)`),
		
		// Specific failures
		pullFailurePattern: regexp.MustCompile(`(?i)(pull.*?failed|failed.*?pull|error pulling|cannot pull|image.*?not found)`),
		startFailurePattern: regexp.MustCompile(`(?i)(failed to start|cannot start|error starting|container.*?failed.*?start)`),
		execFailurePattern: regexp.MustCompile(`(?i)(exec.*?failed|failed.*?exec|cannot exec|error exec)`),
		mountFailurePattern: regexp.MustCompile(`(?i)(mount.*?failed|failed.*?mount|cannot mount|error mounting)`),
		networkFailurePattern: regexp.MustCompile(`(?i)(network.*?failed|failed.*?network|cannot.*?network|error.*?network)`),
		
		// ID extraction
		containerIDPattern: regexp.MustCompile(`(?i)(?:container_id|containerid|container)[:=\s]+([a-f0-9]{64}|[a-f0-9]{12})`),
		imagePattern: regexp.MustCompile(`(?i)(?:image|img)[:=\s]+([^\s]+)`),
		
		// Error details
		errorDetailPattern: regexp.MustCompile(`(?i)(?:error|err)[:=\s]+(.+?)(?:\s+\w+[:=]|$)`),
		exitCodePattern: regexp.MustCompile(`(?i)(?:exit code|exitcode|exit status)[:=\s]+(\d+)`),
	}
}

// Parse checks if the entry is a container failure and extracts details
func (p *ContainerEventParser) Parse(entry *JournalEntry) *types.Event {
	// Quick check if this is container-related
	if !p.isContainerRelated(entry) {
		return nil
	}
	
	// Identify the failure type
	failureType := p.identifyFailureType(entry)
	if failureType == "" {
		return nil // Not a failure we care about
	}
	
	// Extract container details
	details := p.extractContainerDetails(entry)
	
	// Create the event
	return p.createContainerEvent(entry, failureType, details)
}

// isContainerRelated checks if the entry is from container runtime
func (p *ContainerEventParser) isContainerRelated(entry *JournalEntry) bool {
	// Check systemd unit
	unit := entry.SystemdUnit
	if strings.Contains(unit, "docker") || 
	   strings.Contains(unit, "containerd") || 
	   strings.Contains(unit, "crio") {
		return true
	}
	
	// Check syslog identifier
	ident := entry.SyslogIdentifier
	if ident == "dockerd" || ident == "containerd" || ident == "crio" {
		return true
	}
	
	// Check message content
	message := entry.Message
	containerKeywords := []string{
		"docker", "container", "containerd", "cri-o", "image", "registry",
		"OCI", "runc", "runtime",
	}
	
	messageLower := strings.ToLower(message)
	for _, keyword := range containerKeywords {
		if strings.Contains(messageLower, keyword) {
			return true
		}
	}
	
	return false
}

// identifyFailureType determines the type of container failure
func (p *ContainerEventParser) identifyFailureType(entry *JournalEntry) string {
	message := entry.Message
	
	// Check for specific failure types
	if p.pullFailurePattern.MatchString(message) {
		return "pull_failure"
	}
	if p.startFailurePattern.MatchString(message) {
		return "start_failure"
	}
	if p.execFailurePattern.MatchString(message) {
		return "exec_failure"
	}
	if p.mountFailurePattern.MatchString(message) {
		return "mount_failure"
	}
	if p.networkFailurePattern.MatchString(message) {
		return "network_failure"
	}
	
	// Check for runtime failures
	if p.dockerFailurePattern.MatchString(message) {
		return "docker_runtime_failure"
	}
	if p.containerdFailurePattern.MatchString(message) {
		return "containerd_runtime_failure"
	}
	if p.crioFailurePattern.MatchString(message) {
		return "crio_runtime_failure"
	}
	
	// Generic container failure
	if strings.Contains(strings.ToLower(message), "failed") ||
	   strings.Contains(strings.ToLower(message), "error") {
		return "container_failure"
	}
	
	return ""
}

// extractContainerDetails extracts container-specific information
func (p *ContainerEventParser) extractContainerDetails(entry *JournalEntry) map[string]interface{} {
	details := make(map[string]interface{})
	message := entry.Message
	
	// Extract container ID
	if matches := p.containerIDPattern.FindStringSubmatch(message); matches != nil {
		details["container_id"] = matches[1]
	}
	
	// Extract image
	if matches := p.imagePattern.FindStringSubmatch(message); matches != nil {
		details["image"] = matches[1]
	}
	
	// Extract error details
	if matches := p.errorDetailPattern.FindStringSubmatch(message); matches != nil {
		details["error_detail"] = strings.TrimSpace(matches[1])
	}
	
	// Extract exit code
	if matches := p.exitCodePattern.FindStringSubmatch(message); matches != nil {
		details["exit_code"] = matches[1]
	}
	
	// Try to identify the runtime
	runtime := "unknown"
	switch {
	case strings.Contains(entry.SystemdUnit, "docker") || entry.SyslogIdentifier == "dockerd":
		runtime = "docker"
	case strings.Contains(entry.SystemdUnit, "containerd") || entry.SyslogIdentifier == "containerd":
		runtime = "containerd"
	case strings.Contains(entry.SystemdUnit, "crio") || entry.SyslogIdentifier == "crio":
		runtime = "cri-o"
	}
	details["runtime"] = runtime
	
	// Extract additional context from message
	p.extractAdditionalContext(message, details)
	
	return details
}

// extractAdditionalContext extracts more context from the message
func (p *ContainerEventParser) extractAdditionalContext(message string, details map[string]interface{}) {
	// Look for registry information
	if strings.Contains(message, "registry") {
		registryPattern := regexp.MustCompile(`(?i)registry[:=\s]+([^\s]+)`)
		if matches := registryPattern.FindStringSubmatch(message); matches != nil {
			details["registry"] = matches[1]
		}
	}
	
	// Look for namespace/pod info (common in k8s environments)
	if strings.Contains(message, "namespace") {
		nsPattern := regexp.MustCompile(`(?i)namespace[:=\s]+([^\s]+)`)
		if matches := nsPattern.FindStringSubmatch(message); matches != nil {
			details["namespace"] = matches[1]
		}
	}
	
	if strings.Contains(message, "pod") {
		podPattern := regexp.MustCompile(`(?i)pod[:=\s]+([^\s]+)`)
		if matches := podPattern.FindStringSubmatch(message); matches != nil {
			details["pod"] = matches[1]
		}
	}
	
	// Check for specific error conditions
	errorConditions := map[string]string{
		"no such image":           "image_not_found",
		"no space left":           "disk_full",
		"permission denied":       "permission_error",
		"connection refused":      "connection_error",
		"timeout":                 "timeout_error",
		"authentication required": "auth_error",
		"pull rate limit":         "rate_limit",
		"manifest unknown":        "manifest_error",
		"failed to allocate":      "resource_error",
	}
	
	messageLower := strings.ToLower(message)
	for pattern, condition := range errorConditions {
		if strings.Contains(messageLower, pattern) {
			details["error_condition"] = condition
			break
		}
	}
}

// createContainerEvent creates a structured event for container failure
func (p *ContainerEventParser) createContainerEvent(entry *JournalEntry, failureType string, details map[string]interface{}) *types.Event {
	severity := p.determineSeverity(failureType, details)
	
	event := &types.Event{
		Type:     types.EventTypeContainerFailure,
		Category: types.CategoryReliability,
		Severity: severity,
		Data: map[string]interface{}{
			"failure_type": failureType,
			"message":      entry.Message,
			"unit":         entry.SystemdUnit,
			"service":      entry.SyslogIdentifier,
			"pid":          entry.PID,
		},
		Attributes: map[string]interface{}{
			"parsed_by":       "container_event_parser",
			"container_event": true,
		},
		Labels: p.buildLabels(entry, failureType, details),
		Context: p.buildContext(entry, details),
		Metadata: types.EventMetadata{
			Importance:  p.calculateImportance(failureType, severity),
			Reliability: 0.95,
		},
		Actionable: p.createActionableItem(failureType, details),
	}
	
	// Merge extracted details into event data
	for k, v := range details {
		event.Data[k] = v
	}
	
	return event
}

// determineSeverity determines event severity based on failure type
func (p *ContainerEventParser) determineSeverity(failureType string, details map[string]interface{}) types.Severity {
	// Critical failures
	criticalTypes := []string{
		"docker_runtime_failure",
		"containerd_runtime_failure",
		"crio_runtime_failure",
	}
	for _, ct := range criticalTypes {
		if failureType == ct {
			return types.SeverityCritical
		}
	}
	
	// Error-level failures
	errorTypes := []string{
		"start_failure",
		"mount_failure",
		"pull_failure",
	}
	for _, et := range errorTypes {
		if failureType == et {
			return types.SeverityError
		}
	}
	
	// Check error conditions
	if condition, ok := details["error_condition"].(string); ok {
		switch condition {
		case "disk_full", "resource_error":
			return types.SeverityCritical
		case "image_not_found", "permission_error", "auth_error":
			return types.SeverityError
		case "rate_limit", "timeout_error":
			return types.SeverityWarning
		}
	}
	
	return types.SeverityWarning
}

// calculateImportance calculates event importance
func (p *ContainerEventParser) calculateImportance(failureType string, severity types.Severity) float64 {
	base := 0.7 // Container failures are generally important
	
	// Adjust based on failure type
	switch failureType {
	case "docker_runtime_failure", "containerd_runtime_failure":
		base = 1.0 // Runtime failures affect all containers
	case "start_failure":
		base = 0.9 // Service unavailable
	case "pull_failure":
		base = 0.8 // Deployment blocked
	}
	
	// Adjust based on severity
	if severity == types.SeverityCritical {
		base = max(base, 0.9)
	}
	
	return base
}

// buildLabels creates labels for the event
func (p *ContainerEventParser) buildLabels(entry *JournalEntry, failureType string, details map[string]interface{}) map[string]string {
	labels := map[string]string{
		"hostname":     entry.Hostname,
		"failure_type": failureType,
	}
	
	if runtime, ok := details["runtime"].(string); ok {
		labels["runtime"] = runtime
	}
	if containerID, ok := details["container_id"].(string); ok {
		labels["container_id"] = containerID
	}
	if image, ok := details["image"].(string); ok {
		labels["image"] = image
	}
	
	return labels
}

// buildContext builds event context
func (p *ContainerEventParser) buildContext(entry *JournalEntry, details map[string]interface{}) *types.EventContext {
	ctx := &types.EventContext{
		Node:        entry.Hostname,
		PID:         uint32(entry.PID),
		ProcessName: entry.SyslogIdentifier,
	}
	
	if containerID, ok := details["container_id"].(string); ok {
		ctx.ContainerID = containerID
	}
	if pod, ok := details["pod"].(string); ok {
		ctx.PodName = pod
	}
	if namespace, ok := details["namespace"].(string); ok {
		ctx.Namespace = namespace
	}
	
	return ctx
}

// createActionableItem creates remediation suggestions
func (p *ContainerEventParser) createActionableItem(failureType string, details map[string]interface{}) *collectors.ActionableItem {
	action := &collectors.ActionableItem{
		Type:    "container_remediation",
		Urgency: "high",
	}
	
	switch failureType {
	case "pull_failure":
		action.Description = "Container image pull failed"
		action.Commands = []string{
			"docker images",
			"docker pull " + getImageName(details),
			"kubectl describe pod -A | grep -B5 -A5 'Failed'",
		}
		
	case "start_failure":
		action.Description = "Container failed to start"
		if containerID, ok := details["container_id"].(string); ok {
			action.Commands = []string{
				fmt.Sprintf("docker logs %s", containerID[:12]),
				fmt.Sprintf("docker inspect %s", containerID[:12]),
			}
		}
		
	case "mount_failure":
		action.Description = "Container mount operation failed"
		action.Commands = []string{
			"df -h",
			"mount | grep -E '(docker|overlay|containers)'",
			"dmesg | tail -50",
		}
		
	case "docker_runtime_failure", "containerd_runtime_failure":
		action.Description = "Container runtime failure detected"
		action.Urgency = "immediate"
		action.Commands = []string{
			"systemctl status docker",
			"systemctl status containerd",
			"journalctl -u docker -n 100",
			"docker ps -a",
		}
		
	default:
		action.Description = fmt.Sprintf("Container %s detected", failureType)
		action.Commands = []string{
			"docker ps -a | head -20",
			"kubectl get pods -A | grep -v Running",
		}
	}
	
	// Add condition-specific suggestions
	if condition, ok := details["error_condition"].(string); ok {
		switch condition {
		case "disk_full":
			action.Commands = append(action.Commands,
				"df -h /var/lib/docker",
				"docker system prune -a",
			)
		case "rate_limit":
			action.Description += " - Docker Hub rate limit reached"
			action.Commands = append(action.Commands,
				"docker logout",
				"docker login",
			)
		}
	}
	
	return action
}

func getImageName(details map[string]interface{}) string {
	if image, ok := details["image"].(string); ok {
		return image
	}
	return "<image>"
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}