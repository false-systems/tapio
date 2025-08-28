package dns

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NewDNSLearningEngine creates a new learning engine for DNS pattern recognition
func NewDNSLearningEngine(config DNSLearningConfig, logger *zap.Logger) *DNSLearningEngine {
	return &DNSLearningEngine{
		config:            config,
		baselines:         make(map[string]*DNSBaseline),
		serviceBaselines:  make(map[string]*DNSBaseline),
		suspiciousDomains: make(map[string]*SuspiciousDomain),
		recentAnomalies:   make([]*DNSAnomaly, 0),
		mode:              FilteringModeBaseline, // Start in learning mode
		startTime:         time.Now(),
		lastPatternUpdate: time.Now(),
		learningActive:    config.Enabled,
		logger:            logger,                          // CRITICAL FIX: Store logger for use
		queryWindows:      make(map[string]*SlidingWindow), // CRITICAL FIX: Initialize sliding windows
	}
}

// ProcessEvent analyzes a DNS event and updates learning patterns
func (e *DNSLearningEngine) ProcessEvent(ctx context.Context, dnsEvent *DNSEvent) (*DNSAnomaly, error) {
	if !e.learningActive {
		return nil, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	domainKey := e.getDomainKey(dnsEvent.QueryName)
	serviceKey := e.getServiceKey(dnsEvent.Namespace, dnsEvent.ServiceName)

	// CRITICAL FIX: Record query in sliding window for frequency analysis
	if _, exists := e.queryWindows[domainKey]; !exists {
		e.queryWindows[domainKey] = NewSlidingWindow(100, time.Hour) // 100 entries, 1 hour window
	}
	e.queryWindows[domainKey].AddEntry(time.Now(), 1)

	// Update or create baseline for domain
	if baseline := e.baselines[domainKey]; baseline != nil {
		e.updateBaseline(baseline, dnsEvent)
	} else {
		e.baselines[domainKey] = e.createBaseline(dnsEvent)
		e.logger.Debug("Created new baseline for domain",
			zap.String("domain", dnsEvent.QueryName),
			zap.String("namespace", dnsEvent.Namespace),
			zap.String("service", dnsEvent.ServiceName))
	}

	// Update or create baseline for service if available
	if serviceKey != "" {
		if baseline := e.serviceBaselines[serviceKey]; baseline != nil {
			e.updateBaseline(baseline, dnsEvent)
		} else {
			e.serviceBaselines[serviceKey] = e.createBaseline(dnsEvent)
		}
	}

	// Check for anomalies only after baseline period
	if e.isBaselinePeriodComplete() && e.mode == FilteringModeIntelligent {
		if anomaly, err := e.detectAnomaly(dnsEvent, domainKey, serviceKey); err != nil {
			e.logger.Error("Error detecting anomaly",
				zap.Error(err),
				zap.String("domain", dnsEvent.QueryName))
			return nil, err
		} else if anomaly != nil {
			// CRITICAL FIX: Add anomaly logging
			e.logger.Warn("DNS anomaly detected",
				zap.String("anomaly_type", anomaly.AnomalyType),
				zap.String("domain", anomaly.DomainName),
				zap.String("description", anomaly.Description),
				zap.Float64("deviation", anomaly.BaselineDeviation),
				zap.String("severity", string(anomaly.Severity)))
			return anomaly, nil
		}
	}

	// Check for suspicious domains
	if suspicious := e.checkSuspiciousDomain(dnsEvent); suspicious != nil {
		// CRITICAL FIX: Add suspicious domain logging
		e.logger.Warn("Suspicious domain activity detected",
			zap.String("domain", dnsEvent.QueryName),
			zap.String("reason", suspicious.Reason),
			zap.Float64("confidence", suspicious.ConfidenceScore),
			zap.String("severity", string(suspicious.Severity)),
			zap.Uint32("pid", dnsEvent.PID))

		return &DNSAnomaly{
			ID:                e.generateAnomalyID(dnsEvent),
			Timestamp:         time.Now(),
			AnomalyType:       "suspicious_domain",
			Severity:          suspicious.Severity,
			DomainName:        dnsEvent.QueryName,
			Namespace:         dnsEvent.Namespace,
			ServiceName:       dnsEvent.ServiceName,
			Description:       fmt.Sprintf("Suspicious domain detected: %s", suspicious.Reason),
			BaselineDeviation: suspicious.ConfidenceScore,
			ResolvedIPs:       []string{dnsEvent.ResolvedIP},
			PID:               dnsEvent.PID,
			ContainerID:       dnsEvent.ContainerID,
		}, nil
	}

	e.domainsSeen++
	return nil, nil
}

// UpdateMode switches the learning engine mode
func (e *DNSLearningEngine) UpdateMode(mode FilteringMode) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.mode != mode {
		e.mode = mode
		if mode == FilteringModeIntelligent && e.isBaselinePeriodComplete() {
			e.optimizeBaselines()
		}
	}
}

// GetBaseline returns the learned baseline for a domain
func (e *DNSLearningEngine) GetBaseline(domainName string) *DNSBaseline {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if baseline := e.baselines[e.getDomainKey(domainName)]; baseline != nil {
		// Return a copy to avoid race conditions
		return e.copyBaseline(baseline)
	}
	return nil
}

// GetBaselines returns all learned baselines (for persistence)
func (e *DNSLearningEngine) GetBaselines() map[string]*DNSBaseline {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Return copies to avoid race conditions
	baselines := make(map[string]*DNSBaseline, len(e.baselines))
	for key, baseline := range e.baselines {
		baselines[key] = e.copyBaseline(baseline)
	}
	return baselines
}

// LoadBaselines loads baselines from persistence (call during initialization)
func (e *DNSLearningEngine) LoadBaselines(baselines map[string]*DNSBaseline) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Replace current baselines with loaded ones
	e.baselines = baselines

	e.logger.Info("Loaded baselines from persistence",
		zap.Int("baseline_count", len(baselines)))
}

// GetAnomalies returns recent anomalies
func (e *DNSLearningEngine) GetAnomalies(limit int) []*DNSAnomaly {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > len(e.recentAnomalies) {
		limit = len(e.recentAnomalies)
	}

	// Return most recent anomalies
	anomalies := make([]*DNSAnomaly, limit)
	start := len(e.recentAnomalies) - limit
	copy(anomalies, e.recentAnomalies[start:])
	return anomalies
}

// GetSuspiciousDomains returns currently flagged suspicious domains
func (e *DNSLearningEngine) GetSuspiciousDomains() []*SuspiciousDomain {
	e.mu.Lock() // CRITICAL FIX: Use Lock, not RLock for deletion
	defer e.mu.Unlock()

	domains := make([]*SuspiciousDomain, 0, len(e.suspiciousDomains))
	now := time.Now()

	// Clean up expired domains and return active ones
	for domain, suspicious := range e.suspiciousDomains {
		if now.After(suspicious.TTL) {
			delete(e.suspiciousDomains, domain)
			e.logger.Debug("Expired suspicious domain removed",
				zap.String("domain", domain),
				zap.Duration("age", now.Sub(suspicious.FirstSeen)))
		} else {
			domains = append(domains, suspicious)
		}
	}

	// Sort by confidence score (highest first)
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].ConfidenceScore > domains[j].ConfidenceScore
	})

	return domains
}

// LearningEngineStats represents learning engine statistics
type LearningEngineStats struct {
	Mode                  string        `json:"mode"`
	LearningActive        bool          `json:"learning_active"`
	BaselinesCount        int           `json:"baselines_count"`
	ServiceBaselinesCount int           `json:"service_baselines_count"`
	SuspiciousDomains     int           `json:"suspicious_domains"`
	AnomaliesDetected     int64         `json:"anomalies_detected"`
	DomainsSeen           int64         `json:"domains_seen"`
	LearningDuration      time.Duration `json:"learning_duration"`
	BaselineComplete      bool          `json:"baseline_complete"`
	LastPatternUpdate     time.Time     `json:"last_pattern_update"`
}

// GetLearningStats returns learning engine statistics
func (e *DNSLearningEngine) GetLearningStats() LearningEngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return LearningEngineStats{
		Mode:                  e.mode.String(),
		LearningActive:        e.learningActive,
		BaselinesCount:        len(e.baselines),
		ServiceBaselinesCount: len(e.serviceBaselines),
		SuspiciousDomains:     len(e.suspiciousDomains),
		AnomaliesDetected:     e.anomalyCount,
		DomainsSeen:           e.domainsSeen,
		LearningDuration:      time.Since(e.startTime),
		BaselineComplete:      e.isBaselinePeriodComplete(),
		LastPatternUpdate:     e.lastPatternUpdate,
	}
}

// Private methods

func (e *DNSLearningEngine) getDomainKey(domain string) string {
	// Normalize domain name to lowercase for consistent storage
	return strings.ToLower(strings.TrimSpace(domain))
}

func (e *DNSLearningEngine) getServiceKey(namespace, serviceName string) string {
	if namespace == "" || serviceName == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", namespace, serviceName)
}

func (e *DNSLearningEngine) createBaseline(event *DNSEvent) *DNSBaseline {
	baseline := &DNSBaseline{
		DomainName:      event.QueryName,
		Namespace:       event.Namespace,
		ServiceName:     event.ServiceName,
		QueryTypes:      make(map[DNSQueryType]int64),
		AvgResponseTime: time.Duration(event.LatencyMs) * time.Millisecond,
		QueryFrequency:  1.0, // Will be calculated over time
		TypicalServers:  make(map[string]int64),
		ResponseCodes:   make(map[DNSResponseCode]int64),
		FirstSeen:       time.Now(),
		LastSeen:        time.Now(),
		SampleCount:     1,
		UpdatedAt:       time.Now(),
	}

	// Initialize with first event data
	baseline.QueryTypes[event.QueryType] = 1
	if event.ServerIP != "" {
		baseline.TypicalServers[event.ServerIP] = 1
	}
	baseline.ResponseCodes[event.ResponseCode] = 1

	return baseline
}

func (e *DNSLearningEngine) updateBaseline(baseline *DNSBaseline, event *DNSEvent) {
	baseline.mu.Lock()
	defer baseline.mu.Unlock()

	baseline.SampleCount++
	baseline.LastSeen = time.Now()
	baseline.UpdatedAt = time.Now()

	// Update query type frequencies
	baseline.QueryTypes[event.QueryType]++

	// CRITICAL FIX: Update response time statistics using proper Welford's online algorithm
	latency := time.Duration(event.LatencyMs) * time.Millisecond

	// Welford's algorithm for online mean and variance calculation
	oldMean := baseline.AvgResponseTime
	delta := latency - oldMean
	baseline.AvgResponseTime += delta / time.Duration(baseline.SampleCount)

	// Update variance sum (M2)
	delta2 := latency - baseline.AvgResponseTime
	baseline.varianceSum += float64(delta) * float64(delta2)

	// Calculate standard deviation using Welford's method
	if baseline.SampleCount > 1 {
		variance := baseline.varianceSum / float64(baseline.SampleCount-1) // Sample variance
		baseline.StdDevResponseTime = time.Duration(math.Sqrt(variance))
	}

	// Update server frequencies
	if event.ServerIP != "" {
		baseline.TypicalServers[event.ServerIP]++
	}

	// Update response codes
	baseline.ResponseCodes[event.ResponseCode]++

	// Update query frequency (queries per hour)
	hoursSinceFirst := time.Since(baseline.FirstSeen).Hours()
	if hoursSinceFirst > 0 {
		baseline.QueryFrequency = float64(baseline.SampleCount) / hoursSinceFirst
	}
}

func (e *DNSLearningEngine) detectAnomaly(event *DNSEvent, domainKey, serviceKey string) (*DNSAnomaly, error) {
	baseline := e.baselines[domainKey]
	if baseline == nil {
		// New domain - could be anomaly
		return e.checkNewDomainAnomaly(event)
	}

	baseline.mu.RLock()
	defer baseline.mu.RUnlock()

	var anomalies []*DNSAnomaly

	// Check response time anomaly
	if anomaly := e.checkLatencyAnomaly(event, baseline); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	// Check frequency anomaly
	if anomaly := e.checkFrequencyAnomaly(event, baseline); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	// Check unusual query type
	if anomaly := e.checkQueryTypeAnomaly(event, baseline); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	// Check unusual server
	if anomaly := e.checkServerAnomaly(event, baseline); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	// Return the most severe anomaly
	if len(anomalies) > 0 {
		// Sort by severity and return highest
		sort.Slice(anomalies, func(i, j int) bool {
			return anomalies[i].Severity > anomalies[j].Severity
		})

		e.recordAnomaly(anomalies[0])
		return anomalies[0], nil
	}

	return nil, nil
}

func (e *DNSLearningEngine) checkLatencyAnomaly(event *DNSEvent, baseline *DNSBaseline) *DNSAnomaly {
	if baseline.SampleCount < 10 {
		return nil // Need enough samples
	}

	latency := time.Duration(event.LatencyMs) * time.Millisecond
	if baseline.StdDevResponseTime == 0 {
		return nil // No variation yet
	}

	// Calculate Z-score
	zScore := float64(latency-baseline.AvgResponseTime) / float64(baseline.StdDevResponseTime)

	if math.Abs(zScore) > e.config.AnomalyThreshold {
		severity := domain.EventSeverityMedium
		if math.Abs(zScore) > e.config.AnomalyThreshold*2 {
			severity = domain.EventSeverityHigh
		}

		return &DNSAnomaly{
			ID:          e.generateAnomalyID(event),
			Timestamp:   time.Now(),
			AnomalyType: "latency_anomaly",
			Severity:    severity,
			DomainName:  event.QueryName,
			Namespace:   event.Namespace,
			ServiceName: event.ServiceName,
			Description: fmt.Sprintf("Unusual response time: %s (expected: %s ±%s)",
				latency, baseline.AvgResponseTime, baseline.StdDevResponseTime),
			Metrics: DNSAnomalyMetrics{
				ActualValue:    float64(latency),
				ExpectedValue:  float64(baseline.AvgResponseTime),
				DeviationScore: zScore,
				Confidence:     math.Min(1.0, math.Abs(zScore)/10.0),
			},
			BaselineDeviation: zScore,
			ResolvedIPs:       []string{event.ResolvedIP},
			PID:               event.PID,
			ContainerID:       event.ContainerID,
		}
	}

	return nil
}

func (e *DNSLearningEngine) checkFrequencyAnomaly(event *DNSEvent, baseline *DNSBaseline) *DNSAnomaly {
	// This would need a time-series analysis - simplified for now
	if baseline.SampleCount < 100 {
		return nil
	}

	// Check if query frequency has suddenly increased
	currentWindow := time.Hour
	recentQueries := e.countRecentQueries(event.QueryName, currentWindow)
	expectedQueries := baseline.QueryFrequency * currentWindow.Hours()

	if float64(recentQueries) > expectedQueries*2 {
		return &DNSAnomaly{
			ID:          e.generateAnomalyID(event),
			Timestamp:   time.Now(),
			AnomalyType: "frequency_spike",
			Severity:    domain.EventSeverityMedium,
			DomainName:  event.QueryName,
			Namespace:   event.Namespace,
			ServiceName: event.ServiceName,
			Description: fmt.Sprintf("Query frequency spike: %d queries in last hour (expected: %.1f)",
				recentQueries, expectedQueries),
			Metrics: DNSAnomalyMetrics{
				ActualValue:   float64(recentQueries),
				ExpectedValue: expectedQueries,
				Confidence:    0.8,
			},
			ResolvedIPs: []string{event.ResolvedIP},
			PID:         event.PID,
			ContainerID: event.ContainerID,
		}
	}

	return nil
}

func (e *DNSLearningEngine) checkQueryTypeAnomaly(event *DNSEvent, baseline *DNSBaseline) *DNSAnomaly {
	// Check if this query type is unusual for this domain
	totalQueries := int64(0)
	for _, count := range baseline.QueryTypes {
		totalQueries += count
	}

	queryTypeCount := baseline.QueryTypes[event.QueryType]
	frequency := float64(queryTypeCount) / float64(totalQueries)

	// If this query type is less than 5% of historical queries, it might be anomalous
	if frequency < 0.05 && totalQueries > 50 {
		return &DNSAnomaly{
			ID:          e.generateAnomalyID(event),
			Timestamp:   time.Now(),
			AnomalyType: "unusual_query_type",
			Severity:    domain.EventSeverityLow,
			DomainName:  event.QueryName,
			Namespace:   event.Namespace,
			ServiceName: event.ServiceName,
			Description: fmt.Sprintf("Unusual query type %s for domain (%.1f%% of historical queries)",
				event.QueryType, frequency*100),
			Metrics: DNSAnomalyMetrics{
				ActualValue:   1.0,
				ExpectedValue: frequency,
				Confidence:    0.6,
			},
			ResolvedIPs: []string{event.ResolvedIP},
			PID:         event.PID,
			ContainerID: event.ContainerID,
		}
	}

	return nil
}

func (e *DNSLearningEngine) checkServerAnomaly(event *DNSEvent, baseline *DNSBaseline) *DNSAnomaly {
	if event.ServerIP == "" {
		return nil
	}

	// Check if this server is unusual
	if _, exists := baseline.TypicalServers[event.ServerIP]; !exists && len(baseline.TypicalServers) > 0 {
		return &DNSAnomaly{
			ID:          e.generateAnomalyID(event),
			Timestamp:   time.Now(),
			AnomalyType: "unusual_dns_server",
			Severity:    domain.EventSeverityLow,
			DomainName:  event.QueryName,
			Namespace:   event.Namespace,
			ServiceName: event.ServiceName,
			Description: fmt.Sprintf("Query to unusual DNS server: %s", event.ServerIP),
			Metrics: DNSAnomalyMetrics{
				ActualValue:   1.0,
				ExpectedValue: 0.0,
				Confidence:    0.7,
			},
			ResolvedIPs: []string{event.ResolvedIP},
			PID:         event.PID,
			ContainerID: event.ContainerID,
		}
	}

	return nil
}

func (e *DNSLearningEngine) checkNewDomainAnomaly(event *DNSEvent) (*DNSAnomaly, error) {
	// Check for domain generation algorithm (DGA) patterns
	if e.isDGADomain(event.QueryName) {
		suspicious := &SuspiciousDomain{
			DomainName:       event.QueryName,
			Reason:           "dga_pattern",
			FirstSeen:        time.Now(),
			LastSeen:         time.Now(),
			ConfidenceScore:  0.8,
			Severity:         domain.EventSeverityHigh,
			QueryCount:       1,
			AffectedServices: []string{fmt.Sprintf("%s/%s", event.Namespace, event.ServiceName)},
			ResolvedIPs:      []string{event.ResolvedIP},
			TTL:              time.Now().Add(e.config.SuspiciousDomainTTL),
		}

		e.suspiciousDomains[e.getDomainKey(event.QueryName)] = suspicious

		return &DNSAnomaly{
			ID:                e.generateAnomalyID(event),
			Timestamp:         time.Now(),
			AnomalyType:       "new_suspicious_domain",
			Severity:          domain.EventSeverityHigh,
			DomainName:        event.QueryName,
			Namespace:         event.Namespace,
			ServiceName:       event.ServiceName,
			Description:       "Domain matches DGA pattern",
			BaselineDeviation: 0.8,
			ResolvedIPs:       []string{event.ResolvedIP},
			PID:               event.PID,
			ContainerID:       event.ContainerID,
		}, nil
	}

	return nil, nil
}

func (e *DNSLearningEngine) checkSuspiciousDomain(event *DNSEvent) *SuspiciousDomain {
	domainKey := e.getDomainKey(event.QueryName)
	if suspicious, exists := e.suspiciousDomains[domainKey]; exists {
		// Update existing suspicious domain
		suspicious.LastSeen = time.Now()
		suspicious.QueryCount++
		return suspicious
	}
	return nil
}

func (e *DNSLearningEngine) isDGADomain(domain string) bool {
	// Simple DGA detection heuristics
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false
	}

	subdomain := parts[0]

	// Check for high entropy (randomness)
	entropy := e.calculateEntropy(subdomain)
	if entropy > 3.5 && len(subdomain) > 8 {
		return true
	}

	// Check for numeric patterns common in DGA
	numericCount := 0
	for _, r := range subdomain {
		if r >= '0' && r <= '9' {
			numericCount++
		}
	}
	numericRatio := float64(numericCount) / float64(len(subdomain))
	if numericRatio > 0.3 && len(subdomain) > 10 {
		return true
	}

	// Check for consonant/vowel patterns typical in DGA
	consonantRatio := e.calculateConsonantRatio(subdomain)
	if consonantRatio > 0.8 && len(subdomain) > 12 {
		return true
	}

	return false
}

func (e *DNSLearningEngine) calculateEntropy(s string) float64 {
	freq := make(map[rune]int)
	for _, r := range s {
		freq[r]++
	}

	entropy := 0.0
	length := float64(len(s))
	for _, count := range freq {
		p := float64(count) / length
		entropy -= p * math.Log2(p)
	}

	return entropy
}

func (e *DNSLearningEngine) calculateConsonantRatio(s string) float64 {
	vowels := "aeiou"
	consonantCount := 0

	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' && !strings.ContainsRune(vowels, r) {
			consonantCount++
		}
	}

	alphabeticCount := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			alphabeticCount++
		}
	}

	if alphabeticCount == 0 {
		return 0
	}

	return float64(consonantCount) / float64(alphabeticCount)
}

func (e *DNSLearningEngine) countRecentQueries(domain string, window time.Duration) int {
	// CRITICAL FIX: Implement proper frequency tracking
	domainKey := e.getDomainKey(domain)

	// Get or create sliding window for this domain
	if _, exists := e.queryWindows[domainKey]; !exists {
		e.queryWindows[domainKey] = NewSlidingWindow(100, window) // 100 entries, specified window
	}

	slidingWindow := e.queryWindows[domainKey]
	return slidingWindow.CountInWindow(time.Now())
}

func (e *DNSLearningEngine) generateAnomalyID(event *DNSEvent) string {
	data := fmt.Sprintf("%s-%s-%d-%d", event.QueryName, event.QueryType, event.PID, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

func (e *DNSLearningEngine) recordAnomaly(anomaly *DNSAnomaly) {
	e.recentAnomalies = append(e.recentAnomalies, anomaly)
	e.anomalyCount++

	// Keep only recent anomalies (last 1000)
	if len(e.recentAnomalies) > 1000 {
		e.recentAnomalies = e.recentAnomalies[500:] // Keep last 500
	}
}

func (e *DNSLearningEngine) isBaselinePeriodComplete() bool {
	return time.Since(e.startTime) >= e.config.BaselinePeriod
}

func (e *DNSLearningEngine) optimizeBaselines() {
	// Remove baselines with insufficient data
	minSamples := int64(10)
	for key, baseline := range e.baselines {
		if baseline.SampleCount < minSamples {
			delete(e.baselines, key)
		}
	}

	// Limit memory usage by keeping only top domains
	if len(e.baselines) > e.config.MaxDomainsToTrack {
		e.pruneBaselines()
	}
}

func (e *DNSLearningEngine) pruneBaselines() {
	// Convert to slice for sorting
	type baselineEntry struct {
		key      string
		baseline *DNSBaseline
	}

	entries := make([]baselineEntry, 0, len(e.baselines))
	for key, baseline := range e.baselines {
		entries = append(entries, baselineEntry{key, baseline})
	}

	// Sort by sample count (keep most active domains)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].baseline.SampleCount > entries[j].baseline.SampleCount
	})

	// Keep only top domains
	newBaselines := make(map[string]*DNSBaseline)
	for i := 0; i < e.config.MaxDomainsToTrack && i < len(entries); i++ {
		newBaselines[entries[i].key] = entries[i].baseline
	}

	e.baselines = newBaselines
}

func (e *DNSLearningEngine) copyBaseline(baseline *DNSBaseline) *DNSBaseline {
	baseline.mu.RLock()
	defer baseline.mu.RUnlock()

	queryTypes := make(map[DNSQueryType]int64)
	for k, v := range baseline.QueryTypes {
		queryTypes[k] = v
	}

	typicalServers := make(map[string]int64)
	for k, v := range baseline.TypicalServers {
		typicalServers[k] = v
	}

	responseCodes := make(map[DNSResponseCode]int64)
	for k, v := range baseline.ResponseCodes {
		responseCodes[k] = v
	}

	return &DNSBaseline{
		DomainName:         baseline.DomainName,
		Namespace:          baseline.Namespace,
		ServiceName:        baseline.ServiceName,
		QueryTypes:         queryTypes,
		AvgResponseTime:    baseline.AvgResponseTime,
		StdDevResponseTime: baseline.StdDevResponseTime,
		QueryFrequency:     baseline.QueryFrequency,
		TypicalServers:     typicalServers,
		ResponseCodes:      responseCodes,
		FirstSeen:          baseline.FirstSeen,
		LastSeen:           baseline.LastSeen,
		SampleCount:        baseline.SampleCount,
		UpdatedAt:          baseline.UpdatedAt,
	}
}
