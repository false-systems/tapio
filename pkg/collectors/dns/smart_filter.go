package dns

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// NewSmartFilter creates a new intelligent event filter
func NewSmartFilter(config SmartFilterConfig, learningEngine *DNSLearningEngine, circuitBreaker *CircuitBreaker, logger *zap.Logger) *SmartFilter {
	return &SmartFilter{
		config:         config,
		learningEngine: learningEngine,
		circuitBreaker: circuitBreaker,
		recentEvents:   make(map[string]time.Time),
		priorityQueue:  make([]*FilteredEvent, 0),
		lastRateReset:  time.Now(),
		eventBuffer:    make([]*domain.CollectorEvent, 0),
		maxBufferSize:  10000, // Default max buffer size
	}
}

// FilterEvent processes and filters a DNS event based on intelligent criteria
func (sf *SmartFilter) FilterEvent(ctx context.Context, event *domain.CollectorEvent) (*FilteredEvent, bool, error) {
	if !sf.shouldProcessEvent() {
		return nil, false, nil
	}

	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Rate limiting check
	if !sf.checkRateLimit() {
		sf.droppedEvents++
		return nil, false, nil
	}

	// Circuit breaker check
	if sf.circuitBreaker != nil && !sf.circuitBreaker.AllowRequest() {
		return nil, false, fmt.Errorf("circuit breaker open")
	}

	// Extract DNS event data for filtering
	dnsEvent, err := sf.extractDNSEvent(event)
	if err != nil {
		if sf.circuitBreaker != nil {
			sf.circuitBreaker.RecordFailure(err)
		}
		return nil, false, err
	}

	// Check for duplicates
	if sf.isDuplicate(dnsEvent) {
		sf.droppedEvents++
		return nil, false, nil
	}

	// Calculate event importance
	importance, score, reason := sf.calculateImportance(ctx, dnsEvent)

	// Apply filtering based on mode
	shouldPass := sf.applyModeFiltering(dnsEvent, importance, score)
	if !shouldPass {
		sf.droppedEvents++
		return nil, false, nil
	}

	// Create filtered event
	filteredEvent := &FilteredEvent{
		Event:      event,
		Importance: importance,
		Score:      score,
		Reason:     reason,
		Timestamp:  time.Now(),
	}

	// Handle priority queue if enabled
	if sf.config.PriorityQueueEnabled {
		sf.addToPriorityQueue(filteredEvent)
	}

	sf.processedEvents++

	if sf.circuitBreaker != nil {
		sf.circuitBreaker.RecordSuccess()
	}

	return filteredEvent, true, nil
}

// GetNextEvent returns the next highest priority event from the queue
func (sf *SmartFilter) GetNextEvent() (*FilteredEvent, bool) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if len(sf.priorityQueue) == 0 {
		return nil, false
	}

	// Sort by score (highest first) if not already sorted
	sf.sortPriorityQueue()

	// Pop highest priority event
	event := sf.priorityQueue[0]
	sf.priorityQueue = sf.priorityQueue[1:]

	return event, true
}

// HandleBufferOverflow manages buffer overflow situations
func (sf *SmartFilter) HandleBufferOverflow(event *domain.CollectorEvent) bool {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if len(sf.eventBuffer) >= sf.maxBufferSize {
		switch sf.config.BufferOverflowStrategy {
		case "drop_oldest":
			// Remove oldest event
			sf.eventBuffer = sf.eventBuffer[1:]
		case "drop_lowest_priority":
			// Remove lowest priority event
			sf.dropLowestPriorityEvent()
		default:
			// Default: drop current event
			sf.droppedEvents++
			return false
		}
	}

	sf.eventBuffer = append(sf.eventBuffer, event)
	return true
}

// UpdateConfig updates the filter configuration dynamically
func (sf *SmartFilter) UpdateConfig(config SmartFilterConfig) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	oldMode := sf.config.Mode
	sf.config = config

	// If mode changed, update learning engine
	if oldMode != config.Mode && sf.learningEngine != nil {
		sf.learningEngine.UpdateMode(config.Mode)
	}
}

// SmartFilterStats represents filtering statistics
type SmartFilterStats struct {
	Mode                   string  `json:"mode"`
	ProcessedEvents        int64   `json:"processed_events"`
	DroppedEvents          int64   `json:"dropped_events"`
	FilteringEffectiveness float64 `json:"filtering_effectiveness"`
	BufferSize             int     `json:"buffer_size"`
	PriorityQueueSize      int     `json:"priority_queue_size"`
	RateLimit              int64   `json:"rate_limit"`
	MaxEventsPerSecond     int     `json:"max_events_per_second"`
}

// GetStats returns filtering statistics
func (sf *SmartFilter) GetStats() SmartFilterStats {
	sf.mu.RLock()
	defer sf.mu.RUnlock()

	totalEvents := sf.processedEvents + sf.droppedEvents
	effectiveness := 0.0
	if totalEvents > 0 {
		effectiveness = float64(sf.processedEvents) / float64(totalEvents)
	}

	return SmartFilterStats{
		Mode:                   sf.config.Mode.String(),
		ProcessedEvents:        sf.processedEvents,
		DroppedEvents:          sf.droppedEvents,
		FilteringEffectiveness: effectiveness,
		BufferSize:             len(sf.eventBuffer),
		PriorityQueueSize:      len(sf.priorityQueue),
		RateLimit:              sf.rateLimit,
		MaxEventsPerSecond:     sf.config.MaxEventsPerSecond,
	}
}

// Private methods

func (sf *SmartFilter) shouldProcessEvent() bool {
	// Basic sampling check
	if sf.config.SamplingRate < 1.0 {
		// Use deterministic sampling based on hash for consistency
		hash := fnv.New32a()
		hash.Write([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		hashValue := float64(hash.Sum32()) / float64(^uint32(0))

		if hashValue > sf.config.SamplingRate {
			return false
		}
	}

	return true
}

func (sf *SmartFilter) checkRateLimit() bool {
	now := time.Now()

	// Reset rate counter every second
	if now.Sub(sf.lastRateReset) >= time.Second {
		sf.rateLimit = 0
		sf.lastRateReset = now
	}

	if sf.rateLimit >= int64(sf.config.MaxEventsPerSecond) {
		return false
	}

	sf.rateLimit++
	return true
}

func (sf *SmartFilter) extractDNSEvent(event *domain.CollectorEvent) (*DNSEvent, error) {
	// This would parse the actual DNS event from the domain.CollectorEvent
	// For now, create a mock DNS event - in real implementation, this would
	// parse the event data properly

	eventType := DNSEventTypeQuery
	if strings.Contains(string(event.Type), "response") {
		eventType = DNSEventTypeResponse
	}

	// Extract metadata fields safely
	queryName := ""
	queryType := DNSQueryTypeA
	clientIP := ""
	serverIP := ""
	namespace := ""
	serviceName := ""
	containerID := ""

	if event.Metadata.Attributes != nil {
		if name, exists := event.Metadata.Attributes["query_name"]; exists {
			queryName = name
		}
		if qtype, exists := event.Metadata.Attributes["query_type"]; exists {
			queryType = DNSQueryType(qtype)
		}
		if cip, exists := event.Metadata.Attributes["client_ip"]; exists {
			clientIP = cip
		}
		if sip, exists := event.Metadata.Attributes["server_ip"]; exists {
			serverIP = sip
		}
		if ns, exists := event.Metadata.Attributes["namespace"]; exists {
			namespace = ns
		}
		if svc, exists := event.Metadata.Attributes["service_name"]; exists {
			serviceName = svc
		}
		if cid, exists := event.Metadata.Attributes["container_id"]; exists {
			containerID = cid
		}
	}

	return &DNSEvent{
		Timestamp:    event.Timestamp,
		EventType:    eventType,
		QueryName:    queryName,
		QueryType:    queryType,
		ClientIP:     clientIP,
		ServerIP:     serverIP,
		Success:      event.Severity != domain.EventSeverityHigh,
		Namespace:    namespace,
		ServiceName:  serviceName,
		ContainerID:  containerID,
		ResponseCode: DNSResponseNoError,
		Protocol:     DNSProtocolUDP,
	}, nil
}

func (sf *SmartFilter) isDuplicate(dnsEvent *DNSEvent) bool {
	// Create unique key for event deduplication
	key := fmt.Sprintf("%s:%s:%s:%s",
		dnsEvent.QueryName,
		dnsEvent.QueryType,
		dnsEvent.ClientIP,
		dnsEvent.EventType)

	hash := sha256.Sum256([]byte(key))
	hashKey := fmt.Sprintf("%x", hash[:8])

	now := time.Now()

	// Clean up old entries
	for k, timestamp := range sf.recentEvents {
		if now.Sub(timestamp) > sf.config.DuplicateTimeWindow {
			delete(sf.recentEvents, k)
		}
	}

	// Check if this is a duplicate
	if lastSeen, exists := sf.recentEvents[hashKey]; exists {
		if now.Sub(lastSeen) < sf.config.DuplicateTimeWindow {
			return true
		}
	}

	// Record this event
	sf.recentEvents[hashKey] = now
	return false
}

func (sf *SmartFilter) calculateImportance(ctx context.Context, dnsEvent *DNSEvent) (EventImportance, float64, string) {
	score := 0.0
	reasons := make([]string, 0)

	// Base importance based on event type
	switch dnsEvent.EventType {
	case DNSEventTypeError, DNSEventTypeTimeout:
		score += 0.8
		reasons = append(reasons, "error_event")
	case DNSEventTypeResponse:
		if !dnsEvent.Success {
			score += 0.6
			reasons = append(reasons, "failed_response")
		} else {
			score += 0.1
			reasons = append(reasons, "successful_response")
		}
	case DNSEventTypeQuery:
		score += 0.2
		reasons = append(reasons, "query_event")
	}

	// Check if domain is suspicious
	if sf.learningEngine != nil {
		suspiciousDomains := sf.learningEngine.GetSuspiciousDomains()
		for _, suspicious := range suspiciousDomains {
			if suspicious.DomainName == dnsEvent.QueryName {
				score += suspicious.ConfidenceScore
				reasons = append(reasons, fmt.Sprintf("suspicious_domain_%s", suspicious.Reason))
				break
			}
		}

		// Check for anomalies
		anomaly, _ := sf.learningEngine.ProcessEvent(ctx, dnsEvent)
		if anomaly != nil {
			switch anomaly.Severity {
			case domain.EventSeverityHigh:
				score += 0.9
			case domain.EventSeverityMedium:
				score += 0.6
			case domain.EventSeverityLow:
				score += 0.3
			}
			reasons = append(reasons, fmt.Sprintf("anomaly_%s", anomaly.AnomalyType))
		}
	}

	// Security-relevant patterns
	if sf.isSecurityRelevant(dnsEvent) {
		score += 0.7
		reasons = append(reasons, "security_relevant")
	}

	// Health check filtering
	if sf.config.HealthCheckFilter && sf.isHealthCheckQuery(dnsEvent) {
		score -= 0.5
		reasons = append(reasons, "health_check_filtered")
	}

	// Noise filtering
	if sf.config.NoiseFilterEnabled && sf.isNoise(dnsEvent) {
		score -= 0.3
		reasons = append(reasons, "noise_filtered")
	}

	// Adaptive sampling adjustment
	if sf.config.AdaptiveSampling {
		adaptiveAdjustment := sf.calculateAdaptiveSampling(dnsEvent)
		score += adaptiveAdjustment
		if adaptiveAdjustment != 0 {
			reasons = append(reasons, fmt.Sprintf("adaptive_adjustment_%.2f", adaptiveAdjustment))
		}
	}

	// Determine importance level
	importance := sf.scoreToImportance(score)

	reasonStr := strings.Join(reasons, ",")
	return importance, score, reasonStr
}

func (sf *SmartFilter) applyModeFiltering(dnsEvent *DNSEvent, importance EventImportance, score float64) bool {
	switch sf.config.Mode {
	case FilteringModePassthrough:
		return true

	case FilteringModeBaseline:
		// In baseline mode, capture most events for learning
		return score > -0.8 // Only filter very obvious noise

	case FilteringModeIntelligent:
		// In intelligent mode, filter based on importance
		switch importance {
		case ImportanceCritical:
			return true
		case ImportanceHigh:
			return true
		case ImportanceNormal:
			// Apply sampling for normal events
			return sf.shouldSampleNormalEvent(dnsEvent)
		case ImportanceLow:
			// Heavily sample low importance events
			return sf.shouldSampleLowEvent(dnsEvent)
		}

	case FilteringModeEmergency:
		// Only critical and high importance events
		return importance >= ImportanceHigh
	}

	return false
}

func (sf *SmartFilter) shouldSampleNormalEvent(dnsEvent *DNSEvent) bool {
	// Sample 30% of normal events
	hash := fnv.New32a()
	hash.Write([]byte(dnsEvent.QueryName + dnsEvent.ClientIP))
	return (hash.Sum32() % 100) < 30
}

func (sf *SmartFilter) shouldSampleLowEvent(dnsEvent *DNSEvent) bool {
	// Sample 5% of low importance events
	hash := fnv.New32a()
	hash.Write([]byte(dnsEvent.QueryName + dnsEvent.ClientIP))
	return (hash.Sum32() % 100) < 5
}

func (sf *SmartFilter) isSecurityRelevant(dnsEvent *DNSEvent) bool {
	domain := strings.ToLower(dnsEvent.QueryName)

	// Check for suspicious TLDs
	suspiciousTLDs := []string{".tk", ".ml", ".ga", ".cf", ".bit", ".onion"}
	for _, tld := range suspiciousTLDs {
		if strings.HasSuffix(domain, tld) {
			return true
		}
	}

	// Check for long subdomain (possible DGA)
	parts := strings.Split(domain, ".")
	if len(parts) > 1 && len(parts[0]) > 20 {
		return true
	}

	// Check for many numeric characters (possible DGA)
	numericCount := 0
	for _, r := range parts[0] {
		if r >= '0' && r <= '9' {
			numericCount++
		}
	}
	if len(parts[0]) > 8 && float64(numericCount)/float64(len(parts[0])) > 0.3 {
		return true
	}

	// Check for known security-relevant query types
	securityTypes := []DNSQueryType{DNSQueryTypeTXT, DNSQueryTypeSOA, DNSQueryTypeNS}
	for _, secType := range securityTypes {
		if dnsEvent.QueryType == secType {
			return true
		}
	}

	return false
}

func (sf *SmartFilter) isHealthCheckQuery(dnsEvent *DNSEvent) bool {
	domain := strings.ToLower(dnsEvent.QueryName)

	// Common health check patterns
	healthPatterns := []string{
		"health", "alive", "ready", "status", "ping", "heartbeat",
		"kubernetes.default", "cluster.local",
	}

	for _, pattern := range healthPatterns {
		if strings.Contains(domain, pattern) {
			return true
		}
	}

	// Check for very frequent queries (likely health checks)
	if sf.learningEngine != nil {
		baseline := sf.learningEngine.GetBaseline(dnsEvent.QueryName)
		if baseline != nil && baseline.QueryFrequency > 60 { // More than 60 queries per hour
			return true
		}
	}

	return false
}

func (sf *SmartFilter) isNoise(dnsEvent *DNSEvent) bool {
	domain := strings.ToLower(dnsEvent.QueryName)

	// Common noise patterns
	noisePatterns := []string{
		"google.com", "googleapis.com", "gstatic.com",
		"microsoft.com", "microsoftonline.com",
		"amazon.com", "amazonaws.com",
		"cloudflare.com", "cdn.",
		"telemetry", "analytics", "tracking",
	}

	for _, pattern := range noisePatterns {
		if strings.Contains(domain, pattern) {
			return true
		}
	}

	// Check for very common domains
	if sf.learningEngine != nil {
		baseline := sf.learningEngine.GetBaseline(dnsEvent.QueryName)
		if baseline != nil && baseline.QueryFrequency > 100 && baseline.SampleCount > 1000 {
			return true
		}
	}

	return false
}

func (sf *SmartFilter) calculateAdaptiveSampling(dnsEvent *DNSEvent) float64 {
	if sf.learningEngine == nil {
		return 0.0
	}

	baseline := sf.learningEngine.GetBaseline(dnsEvent.QueryName)
	if baseline == nil {
		// New domain - higher importance
		return 0.3
	}

	// Lower importance for very common domains
	if baseline.QueryFrequency > 50 && baseline.SampleCount > 500 {
		return -0.2
	}

	// Higher importance for infrequent domains
	if baseline.QueryFrequency < 1 {
		return 0.2
	}

	return 0.0
}

func (sf *SmartFilter) scoreToImportance(score float64) EventImportance {
	if score >= 0.8 {
		return ImportanceCritical
	} else if score >= 0.5 {
		return ImportanceHigh
	} else if score >= 0.0 {
		return ImportanceNormal
	} else {
		return ImportanceLow
	}
}

func (sf *SmartFilter) addToPriorityQueue(event *FilteredEvent) {
	sf.priorityQueue = append(sf.priorityQueue, event)

	// Keep queue size manageable
	if len(sf.priorityQueue) > 1000 {
		sf.sortPriorityQueue()
		sf.priorityQueue = sf.priorityQueue[:500] // Keep top 500
	}
}

func (sf *SmartFilter) sortPriorityQueue() {
	sort.Slice(sf.priorityQueue, func(i, j int) bool {
		return sf.priorityQueue[i].Score > sf.priorityQueue[j].Score
	})
}

func (sf *SmartFilter) dropLowestPriorityEvent() {
	if len(sf.eventBuffer) == 0 {
		return
	}

	// This is a simplified implementation - would need proper priority tracking
	// For now, just remove the oldest event
	sf.eventBuffer = sf.eventBuffer[1:]
	sf.droppedEvents++
}
