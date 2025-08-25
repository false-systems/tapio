//go:build linux
// +build linux

package network

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// detectSecurityConcern analyzes network events for security threats
func (ic *IntelligenceCollector) detectSecurityConcern(event *IntelligenceEvent) *SecurityConcern {
	// Check for suspicious user agents
	if concern := ic.detectSuspiciousUserAgent(event); concern != nil {
		return concern
	}

	// Check for suspicious endpoints
	if concern := ic.detectSuspiciousEndpoint(event); concern != nil {
		return concern
	}

	// Check for port scanning patterns
	if concern := ic.detectPortScanning(event); concern != nil {
		return concern
	}

	// Check for SQL injection attempts
	if concern := ic.detectSQLInjection(event); concern != nil {
		return concern
	}

	// Check for brute force attempts
	if concern := ic.detectBruteForce(event); concern != nil {
		return concern
	}

	return nil
}

// detectSuspiciousUserAgent checks for known malicious user agents
func (ic *IntelligenceCollector) detectSuspiciousUserAgent(event *IntelligenceEvent) *SecurityConcern {
	if event.AnalysisContext == nil {
		return nil
	}

	userAgent, exists := event.AnalysisContext["user_agent"]
	if !exists || userAgent == "" {
		return nil
	}

	userAgentLower := strings.ToLower(userAgent)

	// Check against configured suspicious user agents
	for _, suspicious := range ic.intelConfig.SuspiciousUserAgents {
		if strings.Contains(userAgentLower, strings.ToLower(suspicious)) {
			ic.logger.Debug("Suspicious user agent detected",
				zap.String("user_agent", userAgent),
				zap.String("pattern", suspicious))

			return &SecurityConcern{
				SourceService:  event.SourceService,
				DestService:    event.DestService,
				SourceIP:       event.SourceIP,
				DestIP:         event.DestIP,
				ConcernType:    SecurityConcernSuspiciousUA,
				Description:    fmt.Sprintf("Suspicious user agent '%s' detected", suspicious),
				Severity:       ic.calculateSeverity(suspicious),
				Confidence:     0.95,
				Evidence:       []string{fmt.Sprintf("User-Agent: %s", userAgent)},
				RiskLevel:      SecurityRiskHigh,
				Timestamp:      event.Timestamp,
				AttackerUA:     userAgent,
				TargetEndpoint: event.AnalysisContext["endpoint"],
				RequestCount:   1,
				Recommendation: fmt.Sprintf("Block or rate-limit requests from user agent containing '%s'", suspicious),
				Blocked:        false,
			}
		}
	}

	return nil
}

// detectSuspiciousEndpoint checks for attempts to access sensitive endpoints
func (ic *IntelligenceCollector) detectSuspiciousEndpoint(event *IntelligenceEvent) *SecurityConcern {
	if event.AnalysisContext == nil {
		return nil
	}

	endpoint, exists := event.AnalysisContext["endpoint"]
	if !exists || endpoint == "" {
		return nil
	}

	endpointLower := strings.ToLower(endpoint)

	// Check against configured suspicious endpoints
	for _, suspicious := range ic.intelConfig.SuspiciousEndpoints {
		if strings.Contains(endpointLower, strings.ToLower(suspicious)) {
			ic.logger.Debug("Suspicious endpoint access detected",
				zap.String("endpoint", endpoint),
				zap.String("pattern", suspicious))

			severity := SecuritySeverityHigh
			if strings.Contains(suspicious, ".env") || strings.Contains(suspicious, ".git") {
				severity = SecuritySeverityCritical
			}

			return &SecurityConcern{
				SourceService:  event.SourceService,
				DestService:    event.DestService,
				SourceIP:       event.SourceIP,
				DestIP:         event.DestIP,
				ConcernType:    SecurityConcernUnauthorized,
				Description:    fmt.Sprintf("Attempt to access sensitive endpoint '%s'", endpoint),
				Severity:       severity,
				Confidence:     0.90,
				Evidence:       []string{fmt.Sprintf("Endpoint: %s", endpoint), fmt.Sprintf("Method: %s", event.AnalysisContext["method"])},
				RiskLevel:      SecurityRiskHigh,
				Timestamp:      event.Timestamp,
				TargetEndpoint: endpoint,
				RequestCount:   1,
				Recommendation: fmt.Sprintf("Block access to '%s' and investigate source", suspicious),
				Blocked:        false,
			}
		}
	}

	return nil
}

// detectPortScanning checks for port scanning patterns
func (ic *IntelligenceCollector) detectPortScanning(event *IntelligenceEvent) *SecurityConcern {
	// Track connection attempts per source IP
	sourceKey := fmt.Sprintf("scan_%s", event.SourceIP)

	// Simple port scan detection: multiple ports from same source in short time
	if event.DestPort > 0 {
		ic.mutex.Lock()
		defer ic.mutex.Unlock()

		// Get or create tracking for this source
		tracker, exists := ic.errorCascadeTracker[sourceKey]
		if !exists {
			tracker = &ErrorCascade{
				WindowStart: time.Now(),
				ErrorCount:  0,
				Services:    make(map[string]int32),
				StatusCodes: make(map[int32]int32),
			}
			ic.errorCascadeTracker[sourceKey] = tracker
		}

		// Track unique ports accessed
		portKey := fmt.Sprintf("%d", event.DestPort)
		tracker.Services[portKey]++
		tracker.ErrorCount++

		// If more than 10 different ports in 30 seconds, likely a scan
		if len(tracker.Services) > 10 && time.Since(tracker.WindowStart) < 30*time.Second {
			ic.logger.Warn("Port scanning detected",
				zap.String("source_ip", event.SourceIP),
				zap.Int("ports_scanned", len(tracker.Services)))

			ports := make([]string, 0, len(tracker.Services))
			for port := range tracker.Services {
				ports = append(ports, port)
			}

			return &SecurityConcern{
				SourceService:  event.SourceService,
				DestService:    event.DestService,
				SourceIP:       event.SourceIP,
				DestIP:         event.DestIP,
				ConcernType:    SecurityConcernPortScan,
				Description:    fmt.Sprintf("Port scanning detected from %s", event.SourceIP),
				Severity:       SecuritySeverityHigh,
				Confidence:     0.85,
				Evidence:       append([]string{fmt.Sprintf("Ports scanned: %d", len(tracker.Services))}, ports...),
				RiskLevel:      SecurityRiskHigh,
				Timestamp:      event.Timestamp,
				RequestCount:   tracker.ErrorCount,
				Recommendation: "Block source IP and investigate for reconnaissance activity",
				Blocked:        false,
			}
		}
	}

	return nil
}

// detectSQLInjection checks for SQL injection patterns
func (ic *IntelligenceCollector) detectSQLInjection(event *IntelligenceEvent) *SecurityConcern {
	if event.AnalysisContext == nil {
		return nil
	}

	// Check URL parameters and body for SQL injection patterns
	params, exists := event.AnalysisContext["params"]
	if !exists {
		params, exists = event.AnalysisContext["body"]
		if !exists {
			return nil
		}
	}

	paramsLower := strings.ToLower(params)

	// Common SQL injection patterns
	sqlPatterns := []string{
		"union select",
		"or 1=1",
		"' or '",
		"drop table",
		"exec(",
		"execute(",
		"script>",
		"javascript:",
		"<iframe",
		"../",
		"..\\",
	}

	for _, pattern := range sqlPatterns {
		if strings.Contains(paramsLower, pattern) {
			ic.logger.Error("SQL injection attempt detected",
				zap.String("source_ip", event.SourceIP),
				zap.String("pattern", pattern))

			concernType := SecurityConcernSQLInjection
			if strings.Contains(pattern, "script") || strings.Contains(pattern, "javascript") {
				concernType = SecurityConcernXSSAttempt
			} else if strings.Contains(pattern, "../") {
				concernType = SecurityConcernPathTraversal
			}

			return &SecurityConcern{
				SourceService:  event.SourceService,
				DestService:    event.DestService,
				SourceIP:       event.SourceIP,
				DestIP:         event.DestIP,
				ConcernType:    concernType,
				Description:    fmt.Sprintf("Injection attempt detected with pattern '%s'", pattern),
				Severity:       SecuritySeverityCritical,
				Confidence:     0.98,
				Evidence:       []string{fmt.Sprintf("Malicious pattern: %s", pattern), fmt.Sprintf("Full params: %s", params)},
				RiskLevel:      SecurityRiskCritical,
				Timestamp:      event.Timestamp,
				TargetEndpoint: event.AnalysisContext["endpoint"],
				RequestCount:   1,
				Recommendation: "Immediately block source IP and review application security",
				Blocked:        true, // Auto-block critical threats
			}
		}
	}

	return nil
}

// detectBruteForce checks for brute force login attempts
func (ic *IntelligenceCollector) detectBruteForce(event *IntelligenceEvent) *SecurityConcern {
	// Check if this is a login endpoint
	endpoint := event.AnalysisContext["endpoint"]
	if !strings.Contains(strings.ToLower(endpoint), "login") &&
		!strings.Contains(strings.ToLower(endpoint), "auth") &&
		!strings.Contains(strings.ToLower(endpoint), "signin") {
		return nil
	}

	// Track failed login attempts per source
	sourceKey := fmt.Sprintf("brute_%s_%s", event.SourceIP, endpoint)

	ic.mutex.Lock()
	defer ic.mutex.Unlock()

	tracker, exists := ic.errorCascadeTracker[sourceKey]
	if !exists {
		tracker = &ErrorCascade{
			WindowStart: time.Now(),
			ErrorCount:  0,
			Services:    make(map[string]int32),
			StatusCodes: make(map[int32]int32),
		}
		ic.errorCascadeTracker[sourceKey] = tracker
	}

	// Track failed attempts (401, 403 status codes)
	if event.ErrorPattern != nil && (event.ErrorPattern.StatusCode == 401 || event.ErrorPattern.StatusCode == 403) {
		tracker.StatusCodes[int32(event.ErrorPattern.StatusCode)]++
		tracker.ErrorCount++

		// If more than 5 failed attempts in 1 minute, likely brute force
		if tracker.ErrorCount > 5 && time.Since(tracker.WindowStart) < time.Minute {
			ic.logger.Error("Brute force attack detected",
				zap.String("source_ip", event.SourceIP),
				zap.String("endpoint", endpoint),
				zap.Int32("attempts", tracker.ErrorCount))

			return &SecurityConcern{
				SourceService: event.SourceService,
				DestService:   event.DestService,
				SourceIP:      event.SourceIP,
				DestIP:        event.DestIP,
				ConcernType:   SecurityConcernBruteForce,
				Description:   fmt.Sprintf("Brute force attack on %s", endpoint),
				Severity:      SecuritySeverityCritical,
				Confidence:    0.95,
				Evidence: []string{
					fmt.Sprintf("Failed attempts: %d", tracker.ErrorCount),
					fmt.Sprintf("Time window: %v", time.Since(tracker.WindowStart)),
					fmt.Sprintf("Endpoint: %s", endpoint),
				},
				RiskLevel:      SecurityRiskCritical,
				Timestamp:      event.Timestamp,
				TargetEndpoint: endpoint,
				RequestCount:   tracker.ErrorCount,
				Recommendation: "Implement rate limiting and consider IP blocking",
				Blocked:        tracker.ErrorCount > 10, // Auto-block after 10 attempts
			}
		}
	}

	return nil
}

// calculateSeverity determines severity based on threat type
func (ic *IntelligenceCollector) calculateSeverity(pattern string) string {
	patternLower := strings.ToLower(pattern)

	// Critical severity patterns
	if strings.Contains(patternLower, "sqlmap") ||
		strings.Contains(patternLower, "masscan") ||
		strings.Contains(patternLower, "metasploit") {
		return SecuritySeverityCritical
	}

	// High severity patterns
	if strings.Contains(patternLower, "nmap") ||
		strings.Contains(patternLower, "nikto") {
		return SecuritySeverityHigh
	}

	return SecuritySeverityMedium
}

// analyzeNetworkEventForSecurity performs security analysis on network events
func (ic *IntelligenceCollector) analyzeNetworkEventForSecurity(event *IntelligenceEvent) {
	// Skip if security analysis is disabled
	if !ic.intelConfig.SecurityAnalysisEnabled {
		return
	}

	// Skip known good services
	for _, goodService := range ic.intelConfig.KnownGoodServices {
		if event.SourceService == goodService || event.DestService == goodService {
			return
		}
	}

	// Detect security concerns
	if concern := ic.detectSecurityConcern(event); concern != nil {
		// Create security concern event
		securityEvent := &IntelligenceEvent{
			EventID:         fmt.Sprintf("sec-%s-%d", concern.ConcernType, time.Now().UnixNano()),
			Timestamp:       time.Now(),
			Type:            IntelEventSecurityConcern,
			Protocol:        event.Protocol,
			SourceIP:        event.SourceIP,
			DestIP:          event.DestIP,
			SourcePort:      event.SourcePort,
			DestPort:        event.DestPort,
			ProcessID:       event.ProcessID,
			CgroupID:        event.CgroupID,
			PodUID:          event.PodUID,
			SourceService:   event.SourceService,
			DestService:     event.DestService,
			AnalysisContext: event.AnalysisContext,
			SecurityConcern: concern,
		}

		// Send to intelligence events channel
		select {
		case ic.intelligenceEvents <- securityEvent:
			ic.intelStats.SecurityConcerns++
		default:
			ic.logger.Warn("Intelligence events channel full, dropping security event")
		}
	}
}
