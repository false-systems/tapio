package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/interfaces/logging"
)

// SecurityAuditor provides comprehensive security audit logging
type SecurityAuditor struct {
	config     AuditConfig
	logger     *logging.Logger
	events     chan *AuditEvent
	eventLog   []AuditEvent
	mutex      sync.RWMutex
	siemSender *SIEMSender
}

// AuditEvent represents a security audit event
type AuditEvent struct {
	ID             string                 `json:"id"`
	Timestamp      time.Time              `json:"timestamp"`
	EventType      string                 `json:"event_type"`
	Category       string                 `json:"category"`
	Severity       string                 `json:"severity"`
	Source         AuditSource            `json:"source"`
	Actor          AuditActor             `json:"actor"`
	Target         AuditTarget            `json:"target"`
	Action         string                 `json:"action"`
	Result         string                 `json:"result"`
	Context        map[string]interface{} `json:"context"`
	RiskScore      float64                `json:"risk_score"`
	Signature      string                 `json:"signature,omitempty"`
	ComplianceInfo ComplianceInfo         `json:"compliance_info"`
}

// AuditSource represents the source of an audit event
type AuditSource struct {
	Component string `json:"component"`
	Module    string `json:"module"`
	Version   string `json:"version"`
	Instance  string `json:"instance"`
	Node      string `json:"node,omitempty"`
}

// AuditActor represents the actor performing an action
type AuditActor struct {
	Type       string            `json:"type"` // user, system, api, service
	ID         string            `json:"id"`
	Username   string            `json:"username,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	IPAddress  string            `json:"ip_address,omitempty"`
	UserAgent  string            `json:"user_agent,omitempty"`
	Roles      []string          `json:"roles,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// AuditTarget represents the target of an action
type AuditTarget struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Resource   string            `json:"resource,omitempty"`
	Path       string            `json:"path,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// ComplianceInfo contains compliance-related information
type ComplianceInfo struct {
	Standards      []string          `json:"standards"`
	Controls       []string          `json:"controls"`
	Classification string            `json:"classification"`
	Retention      time.Duration     `json:"retention"`
	Tags           map[string]string `json:"tags"`
}

// NewSecurityAuditor creates a new security auditor
func NewSecurityAuditor(config AuditConfig, logger *logging.Logger) *SecurityAuditor {
	auditor := &SecurityAuditor{
		config:   config,
		logger:   logger.WithComponent("security-auditor"),
		events:   make(chan *AuditEvent, 1000),
		eventLog: make([]AuditEvent, 0, 10000),
	}

	if config.ExternalSIEM.Enabled {
		auditor.siemSender = NewSIEMSender(config.ExternalSIEM, logger)
	}

	return auditor
}

// Initialize sets up the security auditor
func (sa *SecurityAuditor) Initialize() error {
	if !sa.config.Enabled {
		sa.logger.Info("Security auditing is disabled")
		return nil
	}

	sa.logger.Info("Initializing security auditor",
		"log_authentication", sa.config.LogAuthentication,
		"log_authorization", sa.config.LogAuthorization,
		"encrypt_logs", sa.config.EncryptLogs,
	)

	// Start event processor
	go sa.processEvents()

	// Start log retention cleanup
	go sa.logRetentionCleanup()

	// Initialize SIEM sender if configured
	if sa.siemSender != nil {
		if err := sa.siemSender.Initialize(); err != nil {
			return fmt.Errorf("failed to initialize SIEM sender: %w", err)
		}
	}

	return nil
}

// LogRequest logs an HTTP request for audit purposes
func (sa *SecurityAuditor) LogRequest(r *http.Request) {
	if !sa.config.Enabled {
		return
	}

	event := &AuditEvent{
		ID:        sa.generateEventID(),
		Timestamp: time.Now(),
		EventType: "http_request",
		Category:  "access",
		Severity:  "info",
		Source: AuditSource{
			Component: "tapio",
			Module:    "http_server",
			Version:   "1.0.0",
		},
		Actor: AuditActor{
			Type:      "user",
			IPAddress: sa.getClientIP(r),
			UserAgent: r.UserAgent(),
		},
		Target: AuditTarget{
			Type:     "endpoint",
			Resource: r.URL.Path,
			Path:     r.URL.String(),
		},
		Action: r.Method,
		Result: "initiated",
		Context: map[string]interface{}{
			"method":       r.Method,
			"path":         r.URL.Path,
			"query":        r.URL.RawQuery,
			"content_type": r.Header.Get("Content-Type"),
			"user_agent":   r.UserAgent(),
		},
		RiskScore: sa.calculateRiskScore("http_request", r),
		ComplianceInfo: ComplianceInfo{
			Standards:      []string{"SOC2", "ISO27001"},
			Controls:       []string{"CC6.2", "A.12.4.1"},
			Classification: "public",
			Retention:      90 * 24 * time.Hour,
		},
	}

	// Add user context if available
	if userID := r.Context().Value("user_id"); userID != nil {
		event.Actor.ID = userID.(string)
	}
	if username := r.Context().Value("username"); username != nil {
		event.Actor.Username = username.(string)
	}
	if sessionID := r.Context().Value("session_id"); sessionID != nil {
		event.Actor.SessionID = sessionID.(string)
	}

	sa.logEvent(event)
}

// LogAuthentication logs authentication events
func (sa *SecurityAuditor) LogAuthentication(username, result, ipAddress string, context map[string]interface{}) {
	if !sa.config.Enabled || !sa.config.LogAuthentication {
		return
	}

	severity := "info"
	if result == "failed" {
		severity = "warning"
	}

	event := &AuditEvent{
		ID:        sa.generateEventID(),
		Timestamp: time.Now(),
		EventType: "authentication",
		Category:  "security",
		Severity:  severity,
		Source: AuditSource{
			Component: "tapio",
			Module:    "auth_manager",
		},
		Actor: AuditActor{
			Type:      "user",
			Username:  username,
			IPAddress: ipAddress,
		},
		Target: AuditTarget{
			Type:     "system",
			Resource: "authentication",
		},
		Action:    "authenticate",
		Result:    result,
		Context:   context,
		RiskScore: sa.calculateAuthRiskScore(result, ipAddress),
		ComplianceInfo: ComplianceInfo{
			Standards:      []string{"SOC2", "ISO27001"},
			Controls:       []string{"CC6.2", "A.9.4.2"},
			Classification: "confidential",
			Retention:      365 * 24 * time.Hour,
		},
	}

	sa.logEvent(event)
}

// LogAuthorization logs authorization events
func (sa *SecurityAuditor) LogAuthorization(userID, action, resource, result string, context map[string]interface{}) {
	if !sa.config.Enabled || !sa.config.LogAuthorization {
		return
	}

	severity := "info"
	if result == "denied" {
		severity = "warning"
	}

	event := &AuditEvent{
		ID:        sa.generateEventID(),
		Timestamp: time.Now(),
		EventType: "authorization",
		Category:  "security",
		Severity:  severity,
		Source: AuditSource{
			Component: "tapio",
			Module:    "auth_manager",
		},
		Actor: AuditActor{
			Type: "user",
			ID:   userID,
		},
		Target: AuditTarget{
			Type:     "resource",
			Resource: resource,
		},
		Action:    action,
		Result:    result,
		Context:   context,
		RiskScore: sa.calculateAuthzRiskScore(action, resource, result),
		ComplianceInfo: ComplianceInfo{
			Standards:      []string{"SOC2", "ISO27001"},
			Controls:       []string{"CC6.3", "A.9.4.1"},
			Classification: "confidential",
			Retention:      365 * 24 * time.Hour,
		},
	}

	sa.logEvent(event)
}

// LogDataAccess logs data access events
func (sa *SecurityAuditor) LogDataAccess(userID, operation, dataType string, recordCount int, context map[string]interface{}) {
	if !sa.config.Enabled || !sa.config.LogDataAccess {
		return
	}

	event := &AuditEvent{
		ID:        sa.generateEventID(),
		Timestamp: time.Now(),
		EventType: "data_access",
		Category:  "data",
		Severity:  "info",
		Source: AuditSource{
			Component: "tapio",
			Module:    "data_layer",
		},
		Actor: AuditActor{
			Type: "user",
			ID:   userID,
		},
		Target: AuditTarget{
			Type:     "data",
			Resource: dataType,
		},
		Action:    operation,
		Result:    "success",
		Context:   context,
		RiskScore: sa.calculateDataAccessRiskScore(operation, dataType, recordCount),
		ComplianceInfo: ComplianceInfo{
			Standards:      []string{"GDPR", "SOC2"},
			Controls:       []string{"Art. 30", "CC6.8"},
			Classification: "confidential",
			Retention:      2555 * 24 * time.Hour, // 7 years for GDPR
		},
	}

	if context == nil {
		event.Context = make(map[string]interface{})
	}
	event.Context["record_count"] = recordCount

	sa.logEvent(event)
}

// LogConfigChange logs configuration changes
func (sa *SecurityAuditor) LogConfigChange(userID, component, setting, oldValue, newValue string) {
	if !sa.config.Enabled || !sa.config.LogConfigChanges {
		return
	}

	event := &AuditEvent{
		ID:        sa.generateEventID(),
		Timestamp: time.Now(),
		EventType: "config_change",
		Category:  "configuration",
		Severity:  "warning",
		Source: AuditSource{
			Component: "tapio",
			Module:    component,
		},
		Actor: AuditActor{
			Type: "user",
			ID:   userID,
		},
		Target: AuditTarget{
			Type:     "configuration",
			Resource: setting,
		},
		Action: "modify",
		Result: "success",
		Context: map[string]interface{}{
			"setting":   setting,
			"old_value": oldValue,
			"new_value": newValue,
		},
		RiskScore: sa.calculateConfigRiskScore(setting),
		ComplianceInfo: ComplianceInfo{
			Standards:      []string{"SOC2", "ISO27001"},
			Controls:       []string{"CC8.1", "A.12.1.2"},
			Classification: "confidential",
			Retention:      365 * 24 * time.Hour,
		},
	}

	sa.logEvent(event)
}

// LogSecurityEvent logs general security events
func (sa *SecurityAuditor) LogSecurityEvent(eventType, ipAddress string, context ...interface{}) {
	if !sa.config.Enabled {
		return
	}

	severity := "warning"
	// Determine severity based on event type
	switch eventType {
	case "brute_force_attack", "sql_injection", "xss_attempt":
		severity = "critical"
	case "rate_limit_exceeded", "invalid_token":
		severity = "warning"
	default:
		severity = "info"
	}

	event := &AuditEvent{
		ID:        sa.generateEventID(),
		Timestamp: time.Now(),
		EventType: eventType,
		Category:  "security",
		Severity:  severity,
		Source: AuditSource{
			Component: "tapio",
			Module:    "security",
		},
		Actor: AuditActor{
			Type:      "unknown",
			IPAddress: ipAddress,
		},
		Target: AuditTarget{
			Type: "system",
		},
		Action:    eventType,
		Result:    "blocked",
		Context:   sa.parseContext(context...),
		RiskScore: sa.calculateSecurityEventRiskScore(eventType),
		ComplianceInfo: ComplianceInfo{
			Standards:      []string{"SOC2", "ISO27001"},
			Controls:       []string{"CC7.1", "A.12.6.1"},
			Classification: "restricted",
			Retention:      365 * 24 * time.Hour,
		},
	}

	sa.logEvent(event)
}

// logEvent processes and stores an audit event
func (sa *SecurityAuditor) logEvent(event *AuditEvent) {
	// Sign the event if required
	if sa.config.SignLogs {
		event.Signature = sa.signEvent(event)
	}

	// Send to event processing channel
	select {
	case sa.events <- event:
	default:
		sa.logger.Error("Audit event channel full, dropping event")
	}
}

// processEvents processes audit events in the background
func (sa *SecurityAuditor) processEvents() {
	for event := range sa.events {
		// Store in memory log
		sa.mutex.Lock()
		sa.eventLog = append(sa.eventLog, *event)
		// Limit memory usage
		if len(sa.eventLog) > 10000 {
			sa.eventLog = sa.eventLog[1000:]
		}
		sa.mutex.Unlock()

		// Log to structured logger
		sa.logger.WithFields(map[string]interface{}{
			"audit_event_id":  event.ID,
			"event_type":      event.EventType,
			"category":        event.Category,
			"severity":        event.Severity,
			"actor_id":        event.Actor.ID,
			"actor_ip":        event.Actor.IPAddress,
			"target_resource": event.Target.Resource,
			"action":          event.Action,
			"result":          event.Result,
			"risk_score":      event.RiskScore,
		}).Info("Security audit event")

		// Send to SIEM if configured
		if sa.siemSender != nil {
			if err := sa.siemSender.SendEvent(event); err != nil {
				sa.logger.Error("Failed to send event to SIEM", "error", err)
			}
		}
	}
}

// logRetentionCleanup cleans up old audit logs based on retention policy
func (sa *SecurityAuditor) logRetentionCleanup() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		retentionPeriod := time.Duration(sa.config.RetentionDays) * 24 * time.Hour
		cutoff := time.Now().Add(-retentionPeriod)

		sa.mutex.Lock()
		var filteredLog []AuditEvent
		for _, event := range sa.eventLog {
			if event.Timestamp.After(cutoff) {
				filteredLog = append(filteredLog, event)
			}
		}
		sa.eventLog = filteredLog
		sa.mutex.Unlock()

		sa.logger.Info("Audit log retention cleanup completed",
			"retention_days", sa.config.RetentionDays,
			"events_remaining", len(filteredLog),
		)
	}
}

// Helper methods

func (sa *SecurityAuditor) generateEventID() string {
	// Generate a unique event ID
	return fmt.Sprintf("audit-%d-%s", time.Now().UnixNano(),
		hex.EncodeToString(sha256.New().Sum(nil))[:8])
}

func (sa *SecurityAuditor) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Check X-Real-IP header
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	return r.RemoteAddr
}

func (sa *SecurityAuditor) parseContext(args ...interface{}) map[string]interface{} {
	context := make(map[string]interface{})
	for i := 0; i < len(args)-1; i += 2 {
		if key, ok := args[i].(string); ok && i+1 < len(args) {
			context[key] = args[i+1]
		}
	}
	return context
}

func (sa *SecurityAuditor) signEvent(event *AuditEvent) string {
	// Create a hash of the event for integrity verification
	data, _ := json.Marshal(event)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Risk scoring methods

func (sa *SecurityAuditor) calculateRiskScore(eventType string, r *http.Request) float64 {
	score := 0.1 // Base score

	// Increase score for sensitive endpoints
	path := r.URL.Path
	if strings.Contains(path, "/admin") || strings.Contains(path, "/config") {
		score += 0.3
	}

	// Increase score for non-GET methods
	if r.Method != "GET" {
		score += 0.2
	}

	return min(score, 1.0)
}

func (sa *SecurityAuditor) calculateAuthRiskScore(result, ipAddress string) float64 {
	score := 0.1

	if result == "failed" {
		score += 0.5
	}

	// Add more risk factors based on IP reputation, geolocation, etc.

	return min(score, 1.0)
}

func (sa *SecurityAuditor) calculateAuthzRiskScore(action, resource, result string) float64 {
	score := 0.1

	if result == "denied" {
		score += 0.3
	}

	// Increase score for sensitive actions
	if action == "delete" || action == "modify" {
		score += 0.2
	}

	return min(score, 1.0)
}

func (sa *SecurityAuditor) calculateDataAccessRiskScore(operation, dataType string, recordCount int) float64 {
	score := 0.1

	// Increase score for large data access
	if recordCount > 1000 {
		score += 0.3
	} else if recordCount > 100 {
		score += 0.1
	}

	// Increase score for sensitive operations
	if operation == "export" || operation == "bulk_read" {
		score += 0.2
	}

	return min(score, 1.0)
}

func (sa *SecurityAuditor) calculateConfigRiskScore(setting string) float64 {
	score := 0.3 // Config changes are inherently risky

	// Increase score for security-related settings
	if strings.Contains(setting, "auth") || strings.Contains(setting, "security") {
		score += 0.4
	}

	return min(score, 1.0)
}

func (sa *SecurityAuditor) calculateSecurityEventRiskScore(eventType string) float64 {
	switch eventType {
	case "brute_force_attack", "sql_injection", "xss_attempt":
		return 0.9
	case "rate_limit_exceeded", "invalid_token":
		return 0.5
	default:
		return 0.3
	}
}

// GetAuditEvents returns audit events matching criteria
func (sa *SecurityAuditor) GetAuditEvents(criteria AuditSearchCriteria) []AuditEvent {
	sa.mutex.RLock()
	defer sa.mutex.RUnlock()

	var results []AuditEvent
	for _, event := range sa.eventLog {
		if sa.matchesCriteria(event, criteria) {
			results = append(results, event)
		}
	}

	return results
}

// AuditSearchCriteria defines search criteria for audit events
type AuditSearchCriteria struct {
	EventType string
	Category  string
	Severity  string
	ActorID   string
	IPAddress string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
}

func (sa *SecurityAuditor) matchesCriteria(event AuditEvent, criteria AuditSearchCriteria) bool {
	if criteria.EventType != "" && event.EventType != criteria.EventType {
		return false
	}
	if criteria.Category != "" && event.Category != criteria.Category {
		return false
	}
	if criteria.Severity != "" && event.Severity != criteria.Severity {
		return false
	}
	if criteria.ActorID != "" && event.Actor.ID != criteria.ActorID {
		return false
	}
	if criteria.IPAddress != "" && event.Actor.IPAddress != criteria.IPAddress {
		return false
	}
	if !criteria.StartTime.IsZero() && event.Timestamp.Before(criteria.StartTime) {
		return false
	}
	if !criteria.EndTime.IsZero() && event.Timestamp.After(criteria.EndTime) {
		return false
	}

	return true
}

// SIEMSender sends audit events to external SIEM systems
type SIEMSender struct {
	config SIEMConfig
	logger *logging.Logger
}

// NewSIEMSender creates a new SIEM sender
func NewSIEMSender(config SIEMConfig, logger *logging.Logger) *SIEMSender {
	return &SIEMSender{
		config: config,
		logger: logger.WithComponent("siem-sender"),
	}
}

// Initialize sets up the SIEM sender
func (ss *SIEMSender) Initialize() error {
	ss.logger.Info("Initializing SIEM sender",
		"endpoint", ss.config.Endpoint,
		"format", ss.config.Format,
	)
	return nil
}

// SendEvent sends an audit event to the SIEM system
func (ss *SIEMSender) SendEvent(event *AuditEvent) error {
	// Implementation would send event to external SIEM
	// This is a placeholder
	ss.logger.Debug("Sending event to SIEM",
		"event_id", event.ID,
		"event_type", event.EventType,
	)
	return nil
}

// Helper function
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
