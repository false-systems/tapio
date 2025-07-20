package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// eventProcessor implements core.EventProcessor
type eventProcessor struct{}

func newEventProcessor() core.EventProcessor {
	return &eventProcessor{}
}

// ProcessEvent converts a raw systemd event to a UnifiedEvent with rich semantic context
func (p *eventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (*domain.UnifiedEvent, error) {
	// Generate cryptographically secure event ID like K8s collector
	eventID := p.generateSecureEventID()

	// Determine severity with semantic intelligence
	severity := p.determineSeverity(raw)

	// Create the UnifiedEvent with rich semantic context
	unifiedEvent := &domain.UnifiedEvent{
		// Core event identification
		ID:        eventID,
		Timestamp: raw.Timestamp,
		Source:    string(domain.SourceSystemd),
		Type:      domain.EventTypeSystem,

		// Rich semantic correlation context - CRITICAL
		Semantic: p.createSemanticContext(raw),
		Entity:   p.createEntityContext(raw),

		// System-specific data (NOT generic Data!)
		System: p.createSystemContext(raw),

		// Complete impact context with all fields
		Impact: p.createImpactContext(raw, severity),
	}

	return unifiedEvent, nil
}

// generateSecureEventID generates cryptographically secure event ID like K8s
func (p *eventProcessor) generateSecureEventID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("systemd-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("systemd-%s", hex.EncodeToString(bytes))
}

// createSemanticContext creates rich semantic context for systemd events
func (p *eventProcessor) createSemanticContext(raw core.RawEvent) *domain.SemanticContext {
	intent := p.determineSemanticIntent(raw)
	category := p.determineSemanticCategory(raw)
	tags := p.generateSemanticTags(raw)
	narrative := p.generateNarrative(raw)
	confidence := p.calculateSemanticConfidence(raw)

	return &domain.SemanticContext{
		Intent:     intent,
		Category:   category,
		Tags:       tags,
		Narrative:  narrative,
		Confidence: confidence,
	}
}

// createEntityContext creates entity context for the systemd service
func (p *eventProcessor) createEntityContext(raw core.RawEvent) *domain.EntityContext {
	return &domain.EntityContext{
		Type:      "SystemdUnit",
		Name:      raw.UnitName,
		Namespace: raw.UnitType, // Use unit type as namespace (service, socket, timer)
		UID:       fmt.Sprintf("%s.%s", raw.UnitName, raw.UnitType),
		Labels: map[string]string{
			"unit_type": raw.UnitType,
			"state":     raw.NewState,
			"sub_state": raw.SubState,
		},
		Attributes: map[string]string{
			"old_state":  raw.OldState,
			"new_state":  raw.NewState,
			"sub_state":  raw.SubState,
			"result":     raw.Result,
			"event_type": string(raw.Type),
			"hostname":   p.getHostname(),
		},
	}
}

// createSystemContext creates system-specific context
func (p *eventProcessor) createSystemContext(raw core.RawEvent) *domain.SystemData {
	// Extract process info
	var processInfo *domain.ProcessInfo
	if raw.MainPID > 0 {
		processInfo = &domain.ProcessInfo{
			PID:      int(raw.MainPID),
			ExitCode: int(raw.ExitCode),
			Signal:   int(raw.ExitStatus),
		}
	}

	// Map systemd properties to system metrics
	metrics := make(map[string]float64)
	if props, ok := raw.Properties["CPUUsageNSec"]; ok {
		if cpuNano, ok := props.(uint64); ok {
			metrics["cpu_usage_seconds"] = float64(cpuNano) / 1e9
		}
	}
	if props, ok := raw.Properties["MemoryCurrent"]; ok {
		if memBytes, ok := props.(uint64); ok {
			metrics["memory_current_bytes"] = float64(memBytes)
		}
	}
	if props, ok := raw.Properties["TasksCurrent"]; ok {
		if tasks, ok := props.(uint64); ok {
			metrics["tasks_current"] = float64(tasks)
		}
	}

	// Convert properties to attributes
	attributes := make(map[string]string)
	for k, v := range raw.Properties {
		attributes[k] = fmt.Sprintf("%v", v)
	}

	return &domain.SystemData{
		Component:   raw.UnitName,
		Operation:   p.mapEventTypeToOperation(raw.Type),
		Status:      raw.NewState,
		Message:     p.generateSystemMessage(raw),
		Process:     processInfo,
		Metrics:     metrics,
		Attributes:  attributes,
		ErrorCode:   fmt.Sprintf("%d", raw.ExitCode),
		ErrorDetail: raw.Result,
	}
}

// createImpactContext creates complete impact context with all fields
func (p *eventProcessor) createImpactContext(raw core.RawEvent, severity string) *domain.ImpactContext {
	businessImpact := p.calculateBusinessImpact(raw, severity)
	affectedServices := p.determineAffectedServices(raw)
	customerFacing := p.isCustomerFacing(raw)
	sloImpact := p.hasSLOImpact(raw, severity)
	revenueImpacting := p.isRevenueImpacting(raw)
	affectedUsers := p.estimateAffectedUsers(raw)

	return &domain.ImpactContext{
		Severity:         severity,
		BusinessImpact:   businessImpact,
		AffectedServices: affectedServices,
		CustomerFacing:   customerFacing,
		SLOImpact:        sloImpact,
		RevenueImpacting: revenueImpacting,
		AffectedUsers:    int(affectedUsers),
	}
}

// determineSemanticIntent determines the semantic intent of systemd events
func (p *eventProcessor) determineSemanticIntent(raw core.RawEvent) string {
	switch raw.Type {
	case core.EventTypeStart:
		if raw.Result == "success" {
			return "service-started"
		}
		return "service-start-failed"

	case core.EventTypeStop:
		if raw.OldState == core.StateFailed {
			return "failed-service-stopped"
		}
		return "service-stopped"

	case core.EventTypeRestart:
		return "service-restarted"

	case core.EventTypeReload:
		return "service-reloaded"

	case core.EventTypeFailure:
		switch raw.Result {
		case "exit-code":
			return "service-crashed"
		case "signal":
			return "service-killed"
		case "timeout":
			return "service-timeout"
		case "watchdog":
			return "service-watchdog-failure"
		default:
			return "service-failed"
		}

	case core.EventTypeStateChange:
		if raw.OldState == core.StateActive && raw.NewState == core.StateInactive {
			return "service-deactivated"
		}
		if raw.OldState == core.StateInactive && raw.NewState == core.StateActive {
			return "service-activated"
		}
		if raw.NewState == core.StateFailed {
			return "service-entered-failed-state"
		}
		return fmt.Sprintf("service-state-%s-to-%s", raw.OldState, raw.NewState)

	default:
		return fmt.Sprintf("systemd-%s", strings.ToLower(string(raw.Type)))
	}
}

// determineSemanticCategory determines the semantic category
func (p *eventProcessor) determineSemanticCategory(raw core.RawEvent) string {
	// Failures are reliability issues
	if raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed {
		return "reliability"
	}

	// Restarts might indicate stability issues
	if raw.Type == core.EventTypeRestart {
		return "stability"
	}

	// Start/stop are lifecycle events
	if raw.Type == core.EventTypeStart || raw.Type == core.EventTypeStop {
		return "lifecycle"
	}

	// Reload is operational
	if raw.Type == core.EventTypeReload {
		return "operations"
	}

	// Critical services are availability-related
	if p.isCriticalService(raw.UnitName) && raw.Type == core.EventTypeStateChange {
		return "availability"
	}

	return "operations"
}

// generateSemanticTags generates semantic tags for correlation
func (p *eventProcessor) generateSemanticTags(raw core.RawEvent) []string {
	tags := []string{"systemd", raw.UnitType}

	// Add state tags
	if raw.NewState != "" {
		tags = append(tags, fmt.Sprintf("state-%s", raw.NewState))
	}

	// Add failure tags
	if raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed {
		tags = append(tags, "failure", "service-failure")
		if raw.Result != "" && raw.Result != "success" {
			tags = append(tags, fmt.Sprintf("failure-%s", raw.Result))
		}
	}

	// Add critical service tags
	if p.isCriticalService(raw.UnitName) {
		tags = append(tags, "critical-service", "infrastructure")
	}

	// Add specific service category tags
	switch {
	case strings.Contains(raw.UnitName, "docker") || strings.Contains(raw.UnitName, "containerd"):
		tags = append(tags, "container-runtime")
	case strings.Contains(raw.UnitName, "kubelet"):
		tags = append(tags, "kubernetes", "k8s-node")
	case strings.Contains(raw.UnitName, "network"):
		tags = append(tags, "networking")
	case strings.Contains(raw.UnitName, "ssh"):
		tags = append(tags, "remote-access", "security")
	case strings.Contains(raw.UnitName, "systemd-"):
		tags = append(tags, "core-system")
	}

	// Add unit type specific tags
	switch raw.UnitType {
	case "timer":
		tags = append(tags, "scheduled-task")
	case "socket":
		tags = append(tags, "network-socket")
	case "mount":
		tags = append(tags, "filesystem")
	}

	return tags
}

// generateNarrative creates human-readable description
func (p *eventProcessor) generateNarrative(raw core.RawEvent) string {
	switch raw.Type {
	case core.EventTypeStart:
		if raw.Result == "success" {
			return fmt.Sprintf("Systemd service %s started successfully", raw.UnitName)
		}
		return fmt.Sprintf("Systemd service %s failed to start: %s", raw.UnitName, raw.Result)

	case core.EventTypeStop:
		return fmt.Sprintf("Systemd service %s stopped", raw.UnitName)

	case core.EventTypeRestart:
		return fmt.Sprintf("Systemd service %s was restarted", raw.UnitName)

	case core.EventTypeFailure:
		if raw.ExitCode != 0 {
			return fmt.Sprintf("Systemd service %s failed with exit code %d", raw.UnitName, raw.ExitCode)
		}
		if raw.ExitStatus != 0 {
			return fmt.Sprintf("Systemd service %s terminated by signal %d", raw.UnitName, raw.ExitStatus)
		}
		return fmt.Sprintf("Systemd service %s failed: %s", raw.UnitName, raw.Result)

	case core.EventTypeStateChange:
		return fmt.Sprintf("Systemd service %s changed state from %s to %s", raw.UnitName, raw.OldState, raw.NewState)

	default:
		return fmt.Sprintf("Systemd %s event for %s", raw.Type, raw.UnitName)
	}
}

// calculateSemanticConfidence calculates confidence in semantic classification
func (p *eventProcessor) calculateSemanticConfidence(raw core.RawEvent) float64 {
	// Base confidence for systemd events is high (direct observation)
	confidence := 0.9

	// Well-known services have higher confidence
	if p.isCriticalService(raw.UnitName) {
		confidence += 0.05
	}

	// Clear failure reasons increase confidence
	if raw.Type == core.EventTypeFailure && raw.Result != "" {
		confidence += 0.05
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// determineSeverity determines event severity based on service and state
func (p *eventProcessor) determineSeverity(raw core.RawEvent) string {
	// Failures of critical services are critical
	if (raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed) && p.isCriticalService(raw.UnitName) {
		return "critical"
	}

	// Any other failure is high severity
	if raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed {
		return "high"
	}

	// Restarts are warnings
	if raw.Type == core.EventTypeRestart {
		return "warning"
	}

	// State changes for critical services
	if raw.Type == core.EventTypeStateChange && p.isCriticalService(raw.UnitName) {
		if raw.OldState == core.StateActive && raw.NewState == core.StateInactive {
			return "warning"
		}
	}

	// Exit codes indicate problems
	if raw.ExitCode != 0 {
		return "warning"
	}

	// Everything else is info
	return "info"
}

// calculateBusinessImpact calculates business impact score
func (p *eventProcessor) calculateBusinessImpact(raw core.RawEvent, severity string) float64 {
	base := 0.1

	// Adjust based on severity
	switch severity {
	case "critical":
		base = 0.9
	case "high":
		base = 0.7
	case "warning":
		base = 0.4
	case "info":
		base = 0.1
	}

	// Critical services have higher impact
	if p.isCriticalService(raw.UnitName) {
		base += 0.2
	}

	// Container runtime failures affect many workloads
	if strings.Contains(raw.UnitName, "docker") || strings.Contains(raw.UnitName, "containerd") {
		base += 0.2
	}

	// Network services affect connectivity
	if strings.Contains(raw.UnitName, "network") {
		base += 0.1
	}

	if base > 1.0 {
		base = 1.0
	}

	return base
}

// determineAffectedServices determines which services might be affected
func (p *eventProcessor) determineAffectedServices(raw core.RawEvent) []string {
	services := []string{}

	// Add the service itself
	services = append(services, raw.UnitName)

	// Container runtime affects all containers
	if strings.Contains(raw.UnitName, "docker") || strings.Contains(raw.UnitName, "containerd") {
		services = append(services, "container-workloads", "kubernetes-pods")
	}

	// Kubelet affects all pods on the node
	if strings.Contains(raw.UnitName, "kubelet") {
		services = append(services, "kubernetes-node", "node-pods")
	}

	// Network services affect connectivity
	if strings.Contains(raw.UnitName, "network") {
		services = append(services, "network-connectivity", "service-discovery")
	}

	// SSH affects remote access
	if strings.Contains(raw.UnitName, "ssh") {
		services = append(services, "remote-access", "administrative-access")
	}

	// Systemd core services affect the entire system
	if strings.HasPrefix(raw.UnitName, "systemd-") {
		services = append(services, "system-core", "service-management")
	}

	return services
}

// isCustomerFacing determines if the service affects customers
func (p *eventProcessor) isCustomerFacing(raw core.RawEvent) bool {
	// Application services are often customer-facing
	customerFacingPatterns := []string{
		"nginx",
		"apache",
		"httpd",
		"api",
		"web",
		"app",
		"frontend",
		"backend",
		"database",
		"redis",
		"mysql",
		"postgres",
		"mongo",
	}

	unitLower := strings.ToLower(raw.UnitName)
	for _, pattern := range customerFacingPatterns {
		if strings.Contains(unitLower, pattern) {
			return true
		}
	}

	// Infrastructure services are not customer-facing
	return false
}

// hasSLOImpact determines if the event impacts SLOs
func (p *eventProcessor) hasSLOImpact(raw core.RawEvent, severity string) bool {
	// Critical and high severity events impact SLOs
	if severity == "critical" || severity == "high" {
		return true
	}

	// Customer-facing service failures impact SLOs
	if (raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed) && p.isCustomerFacing(raw) {
		return true
	}

	// Critical service issues impact SLOs
	if p.isCriticalService(raw.UnitName) && raw.Type == core.EventTypeFailure {
		return true
	}

	return false
}

// isRevenueImpacting determines if the event impacts revenue
func (p *eventProcessor) isRevenueImpacting(raw core.RawEvent) bool {
	// Check for revenue-critical services
	revenuePatterns := []string{
		"payment",
		"billing",
		"checkout",
		"transaction",
		"stripe",
		"paypal",
		"commerce",
		"shop",
	}

	unitLower := strings.ToLower(raw.UnitName)
	for _, pattern := range revenuePatterns {
		if strings.Contains(unitLower, pattern) {
			return true
		}
	}

	// Database failures can impact revenue
	if raw.Type == core.EventTypeFailure {
		if strings.Contains(unitLower, "mysql") || strings.Contains(unitLower, "postgres") || strings.Contains(unitLower, "mongo") {
			return true
		}
	}

	return false
}

// estimateAffectedUsers estimates number of affected users
func (p *eventProcessor) estimateAffectedUsers(raw core.RawEvent) int64 {
	// No users affected for successful operations
	if raw.Type == core.EventTypeStart && raw.Result == "success" {
		return 0
	}

	// Infrastructure services don't directly affect users
	if !p.isCustomerFacing(raw) {
		return 0
	}

	// Customer-facing service failures affect many users
	if raw.Type == core.EventTypeFailure || raw.NewState == core.StateFailed {
		if p.isCustomerFacing(raw) {
			return 1000 // Estimate based on service impact
		}
	}

	// Service restarts might cause brief interruptions
	if raw.Type == core.EventTypeRestart && p.isCustomerFacing(raw) {
		return 100
	}

	return 0
}

// Helper functions

func (p *eventProcessor) mapEventTypeToOperation(eventType core.EventType) string {
	switch eventType {
	case core.EventTypeStart:
		return "start"
	case core.EventTypeStop:
		return "stop"
	case core.EventTypeRestart:
		return "restart"
	case core.EventTypeReload:
		return "reload"
	case core.EventTypeFailure:
		return "failure"
	case core.EventTypeStateChange:
		return "state_change"
	default:
		return string(eventType)
	}
}

func (p *eventProcessor) generateSystemMessage(raw core.RawEvent) string {
	switch raw.Type {
	case core.EventTypeFailure:
		if raw.ExitCode != 0 {
			return fmt.Sprintf("Service failed with exit code %d", raw.ExitCode)
		}
		if raw.ExitStatus != 0 {
			return fmt.Sprintf("Service terminated by signal %d", raw.ExitStatus)
		}
		return fmt.Sprintf("Service failed: %s", raw.Result)

	case core.EventTypeStateChange:
		return fmt.Sprintf("State changed from %s to %s", raw.OldState, raw.NewState)

	default:
		return fmt.Sprintf("%s operation completed", raw.Type)
	}
}

// isCriticalService checks if a service is considered critical
func (p *eventProcessor) isCriticalService(serviceName string) bool {
	criticalServices := []string{
		"sshd",
		"systemd-networkd",
		"systemd-resolved",
		"dbus",
		"systemd-journald",
		"kubelet",
		"docker",
		"containerd",
		"crio",
	}

	for _, critical := range criticalServices {
		if strings.Contains(serviceName, critical) {
			return true
		}
	}

	return false
}

// getHostname gets the system hostname
func (p *eventProcessor) getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return hostname
}