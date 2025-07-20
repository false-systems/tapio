package ebpf

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// FilterEngine handles filtering and sampling of eBPF events
type FilterEngine struct {
	// Configuration
	config         *FilterConfig
	rawFilters     []RawEventFilter
	semanticFilter SemanticEventFilter
	sampler        *AdaptiveSampler

	// State
	mu               sync.RWMutex
	eventCounts      map[EventType]uint64
	lastReset        time.Time
	interestingCount uint64
	totalCount       uint64

	// Performance
	filterCache map[string]bool
	cacheMu     sync.RWMutex
}

// FilterConfig contains filtering configuration
type FilterConfig struct {
	// Raw event filtering
	EnableRawFiltering bool        `json:"enable_raw_filtering"`
	EventTypeWhitelist []EventType `json:"event_type_whitelist,omitempty"`
	EventTypeBlacklist []EventType `json:"event_type_blacklist,omitempty"`
	ProcessWhitelist   []string    `json:"process_whitelist,omitempty"`
	ProcessBlacklist   []string    `json:"process_blacklist,omitempty"`
	ContainerWhitelist []string    `json:"container_whitelist,omitempty"`
	NamespaceWhitelist []string    `json:"namespace_whitelist,omitempty"`

	// Semantic filtering
	EnableSemanticFilter bool     `json:"enable_semantic_filter"`
	MinImportanceScore   float64  `json:"min_importance_score"`
	SemanticTypes        []string `json:"semantic_types,omitempty"`
	RequireTraceContext  bool     `json:"require_trace_context"`

	// Sampling
	EnableSampling      bool    `json:"enable_sampling"`
	DefaultSampleRate   float64 `json:"default_sample_rate"`
	HighValueSampleRate float64 `json:"high_value_sample_rate"`
	LowValueSampleRate  float64 `json:"low_value_sample_rate"`
	AdaptiveSampling    bool    `json:"adaptive_sampling"`
	MaxEventsPerSecond  int     `json:"max_events_per_second"`

	// Rate limiting
	EnableRateLimit       bool `json:"enable_rate_limit"`
	GlobalRateLimit       int  `json:"global_rate_limit"`
	PerProcessRateLimit   int  `json:"per_process_rate_limit"`
	PerContainerRateLimit int  `json:"per_container_rate_limit"`

	// Cache
	CacheSize int           `json:"cache_size"`
	CacheTTL  time.Duration `json:"cache_ttl"`
}

// RawEventFilter interface for filtering raw events
type RawEventFilter interface {
	ShouldProcess(event *RawEvent) bool
	Name() string
}

// SemanticEventFilter interface for filtering enriched events
type SemanticEventFilter interface {
	ShouldSendToSemantic(event *EnrichedEvent) bool
	UpdateImportanceScore(event *EnrichedEvent) float64
}

// AdaptiveSampler implements adaptive sampling based on event volume and importance
type AdaptiveSampler struct {
	mu                 sync.RWMutex
	targetRate         float64
	currentRate        float64
	lastAdjustment     time.Time
	eventsInWindow     uint64
	windowStart        time.Time
	windowDuration     time.Duration
	highValueThreshold float64
	lowValueThreshold  float64
	rateLimiters       map[string]*RateLimiter
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	tokens     float64
	capacity   float64
	rate       float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewFilterEngine creates a new filter engine
func NewFilterEngine(config *FilterConfig) *FilterEngine {
	if config == nil {
		config = DefaultFilterConfig()
	}

	fe := &FilterEngine{
		config:      config,
		eventCounts: make(map[EventType]uint64),
		lastReset:   time.Now(),
		filterCache: make(map[string]bool),
	}

	// Initialize raw filters
	fe.rawFilters = []RawEventFilter{
		NewEventTypeFilter(config.EventTypeWhitelist, config.EventTypeBlacklist),
		NewProcessFilter(config.ProcessWhitelist, config.ProcessBlacklist),
		NewContainerFilter(config.ContainerWhitelist),
		NewNamespaceFilter(config.NamespaceWhitelist),
	}

	// Initialize semantic filter
	fe.semanticFilter = NewSemanticFilter(config)

	// Initialize sampler
	if config.EnableSampling {
		fe.sampler = NewAdaptiveSampler(&SamplerConfig{
			DefaultRate:        config.DefaultSampleRate,
			HighValueRate:      config.HighValueSampleRate,
			LowValueRate:       config.LowValueSampleRate,
			MaxEventsPerSecond: config.MaxEventsPerSecond,
			WindowDuration:     time.Minute,
		})
	}

	return fe
}

// DefaultFilterConfig returns default filtering configuration
func DefaultFilterConfig() *FilterConfig {
	return &FilterConfig{
		EnableRawFiltering:    true,
		EnableSemanticFilter:  true,
		EnableSampling:        true,
		DefaultSampleRate:     0.1,  // 10% by default
		HighValueSampleRate:   1.0,  // 100% for high-value events
		LowValueSampleRate:    0.01, // 1% for low-value events
		AdaptiveSampling:      true,
		MaxEventsPerSecond:    10000, // Rate limit
		MinImportanceScore:    0.3,   // Only process events with importance > 0.3
		EnableRateLimit:       true,
		GlobalRateLimit:       50000, // Global rate limit
		PerProcessRateLimit:   1000,  // Per-process rate limit
		PerContainerRateLimit: 5000,  // Per-container rate limit
		CacheSize:             10000,
		CacheTTL:              5 * time.Minute,
	}
}

// ProcessRawEvent filters and samples raw eBPF events
func (fe *FilterEngine) ProcessRawEvent(event *RawEvent) bool {
	if !fe.config.EnableRawFiltering {
		return true
	}

	// Check cache first
	cacheKey := fe.getCacheKey(event)
	if cached, ok := fe.getCachedResult(cacheKey); ok {
		return cached
	}

	// Apply raw filters
	for _, filter := range fe.rawFilters {
		if !filter.ShouldProcess(event) {
			fe.setCachedResult(cacheKey, false)
			return false
		}
	}

	// Update statistics
	fe.updateEventCounts(event.Type)

	result := true
	fe.setCachedResult(cacheKey, result)
	return result
}

// ProcessEnrichedEvent determines if enriched event should be sent to semantic layer
func (fe *FilterEngine) ProcessEnrichedEvent(event *EnrichedEvent) FilterDecision {
	decision := FilterDecision{
		SendRaw:      true, // Always preserve raw events for Hubble-style access
		SendSemantic: false,
		SampleRate:   1.0,
	}

	// Apply semantic filtering
	if fe.config.EnableSemanticFilter && fe.semanticFilter != nil {
		decision.SendSemantic = fe.semanticFilter.ShouldSendToSemantic(event)

		// Update importance score
		event.Importance = fe.semanticFilter.UpdateImportanceScore(event)
		decision.ImportanceScore = event.Importance
	}

	// Apply sampling
	if fe.config.EnableSampling && fe.sampler != nil {
		decision.SampleRate = fe.sampler.GetSampleRate(event)
		decision.ShouldSample = fe.sampler.ShouldSample(event, decision.SampleRate)

		// High-importance events bypass sampling
		if event.Importance > 0.8 {
			decision.ShouldSample = true
		}
	}

	// Apply rate limiting
	if fe.config.EnableRateLimit {
		decision.RateLimited = !fe.checkRateLimit(event)
		if decision.RateLimited {
			decision.SendSemantic = false
		}
	}

	fe.updateFilterStats(decision)
	return decision
}

// FilterDecision represents the result of filtering an event
type FilterDecision struct {
	SendRaw         bool     `json:"send_raw"`
	SendSemantic    bool     `json:"send_semantic"`
	ShouldSample    bool     `json:"should_sample"`
	SampleRate      float64  `json:"sample_rate"`
	ImportanceScore float64  `json:"importance_score"`
	RateLimited     bool     `json:"rate_limited"`
	FilterReasons   []string `json:"filter_reasons,omitempty"`
}

// EventTypeFilter filters based on event types
type EventTypeFilter struct {
	whitelist map[EventType]bool
	blacklist map[EventType]bool
}

func NewEventTypeFilter(whitelist, blacklist []EventType) *EventTypeFilter {
	f := &EventTypeFilter{
		whitelist: make(map[EventType]bool),
		blacklist: make(map[EventType]bool),
	}

	for _, t := range whitelist {
		f.whitelist[t] = true
	}
	for _, t := range blacklist {
		f.blacklist[t] = true
	}

	return f
}

func (f *EventTypeFilter) ShouldProcess(event *RawEvent) bool {
	// If blacklisted, reject
	if f.blacklist[event.Type] {
		return false
	}

	// If whitelist exists and event not in it, reject
	if len(f.whitelist) > 0 && !f.whitelist[event.Type] {
		return false
	}

	return true
}

func (f *EventTypeFilter) Name() string {
	return "event_type_filter"
}

// ProcessFilter filters based on process names
type ProcessFilter struct {
	whitelist map[string]bool
	blacklist map[string]bool
}

func NewProcessFilter(whitelist, blacklist []string) *ProcessFilter {
	f := &ProcessFilter{
		whitelist: make(map[string]bool),
		blacklist: make(map[string]bool),
	}

	for _, p := range whitelist {
		f.whitelist[p] = true
	}
	for _, p := range blacklist {
		f.blacklist[p] = true
	}

	return f
}

func (f *ProcessFilter) ShouldProcess(event *RawEvent) bool {
	// If blacklisted, reject
	if f.blacklist[event.Comm] {
		return false
	}

	// If whitelist exists and process not in it, reject
	if len(f.whitelist) > 0 && !f.whitelist[event.Comm] {
		return false
	}

	return true
}

func (f *ProcessFilter) Name() string {
	return "process_filter"
}

// ContainerFilter filters based on container information
type ContainerFilter struct {
	whitelist map[string]bool
}

func NewContainerFilter(whitelist []string) *ContainerFilter {
	f := &ContainerFilter{
		whitelist: make(map[string]bool),
	}

	for _, c := range whitelist {
		f.whitelist[c] = true
	}

	return f
}

func (f *ContainerFilter) ShouldProcess(event *RawEvent) bool {
	// If no whitelist, allow all
	if len(f.whitelist) == 0 {
		return true
	}

	// This would need container enrichment to work properly
	// For now, allow all raw events through
	return true
}

func (f *ContainerFilter) Name() string {
	return "container_filter"
}

// NamespaceFilter filters based on Kubernetes namespace
type NamespaceFilter struct {
	whitelist map[string]bool
}

func NewNamespaceFilter(whitelist []string) *NamespaceFilter {
	f := &NamespaceFilter{
		whitelist: make(map[string]bool),
	}

	for _, ns := range whitelist {
		f.whitelist[ns] = true
	}

	return f
}

func (f *NamespaceFilter) ShouldProcess(event *RawEvent) bool {
	// If no whitelist, allow all
	if len(f.whitelist) == 0 {
		return true
	}

	// This would need K8s enrichment to work properly
	// For now, allow all raw events through
	return true
}

func (f *NamespaceFilter) Name() string {
	return "namespace_filter"
}

// SemanticFilter implements semantic event filtering
type SemanticFilter struct {
	config             *FilterConfig
	importanceScorer   *ImportanceScorer
	traceContextFilter bool
}

func NewSemanticFilter(config *FilterConfig) *SemanticFilter {
	return &SemanticFilter{
		config:             config,
		importanceScorer:   NewImportanceScorer(),
		traceContextFilter: config.RequireTraceContext,
	}
}

func (f *SemanticFilter) ShouldSendToSemantic(event *EnrichedEvent) bool {
	// Check minimum importance score
	if event.Importance < f.config.MinImportanceScore {
		return false
	}

	// Check if trace context is required
	if f.traceContextFilter && event.TraceID == "" {
		return false
	}

	// Check semantic types if specified
	if len(f.config.SemanticTypes) > 0 {
		found := false
		for _, st := range f.config.SemanticTypes {
			if event.SemanticType == st {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (f *SemanticFilter) UpdateImportanceScore(event *EnrichedEvent) float64 {
	return f.importanceScorer.CalculateImportance(event)
}

// ImportanceScorer calculates importance scores for events
type ImportanceScorer struct {
	weights map[string]float64
}

func NewImportanceScorer() *ImportanceScorer {
	return &ImportanceScorer{
		weights: map[string]float64{
			"security_event":    1.0,
			"network_anomaly":   0.9,
			"process_spawn":     0.7,
			"file_access":       0.5,
			"syscall":           0.3,
			"container_event":   0.8,
			"kubernetes_event":  0.8,
			"error_condition":   0.9,
			"performance_issue": 0.6,
		},
	}
}

func (is *ImportanceScorer) CalculateImportance(event *EnrichedEvent) float64 {
	score := 0.5 // Base score

	// Adjust based on event type
	if weight, ok := is.weights[event.SemanticType]; ok {
		score = weight
	}

	// Boost for security events
	if event.Security != nil && event.Security.RiskScore > 0 {
		score = score + (event.Security.RiskScore * 0.3)
	}

	// Boost for errors
	if strings.Contains(strings.ToLower(event.Raw.Comm), "error") {
		score += 0.2
	}

	// Boost for privileged processes
	if event.Security != nil && event.Security.Privileged {
		score += 0.2
	}

	// Boost for container events
	if event.Container != nil {
		score += 0.1
	}

	// Boost for Kubernetes events
	if event.Kubernetes != nil {
		score += 0.1
	}

	// Boost for trace context
	if event.TraceID != "" {
		score += 0.1
	}

	// Clamp to [0.0, 1.0]
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}

	return score
}

// Adaptive sampler implementation
type SamplerConfig struct {
	DefaultRate        float64
	HighValueRate      float64
	LowValueRate       float64
	MaxEventsPerSecond int
	WindowDuration     time.Duration
}

func NewAdaptiveSampler(config *SamplerConfig) *AdaptiveSampler {
	return &AdaptiveSampler{
		targetRate:         config.DefaultRate,
		currentRate:        config.DefaultRate,
		windowDuration:     config.WindowDuration,
		windowStart:        time.Now(),
		highValueThreshold: 0.7,
		lowValueThreshold:  0.3,
		rateLimiters:       make(map[string]*RateLimiter),
	}
}

func (s *AdaptiveSampler) GetSampleRate(event *EnrichedEvent) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// High-importance events get higher sample rate
	if event.Importance >= s.highValueThreshold {
		return s.currentRate * 2.0 // Boost high-value events
	}

	// Low-importance events get lower sample rate
	if event.Importance <= s.lowValueThreshold {
		return s.currentRate * 0.5 // Reduce low-value events
	}

	return s.currentRate
}

func (s *AdaptiveSampler) ShouldSample(event *EnrichedEvent, rate float64) bool {
	// Use deterministic sampling based on event ID hash
	if event.EventID != "" {
		h := fnv.New64a()
		h.Write([]byte(event.EventID))
		hash := h.Sum64()

		threshold := uint64(rate * float64(^uint64(0)))
		return hash <= threshold
	}

	// Fallback to random sampling
	return rand.Float64() < rate
}

// Helper methods

func (fe *FilterEngine) getCacheKey(event *RawEvent) string {
	return fmt.Sprintf("%d:%s:%d", event.Type, event.Comm, event.UID)
}

func (fe *FilterEngine) getCachedResult(key string) (bool, bool) {
	fe.cacheMu.RLock()
	defer fe.cacheMu.RUnlock()

	result, ok := fe.filterCache[key]
	return result, ok
}

func (fe *FilterEngine) setCachedResult(key string, result bool) {
	fe.cacheMu.Lock()
	defer fe.cacheMu.Unlock()

	// Simple cache size management
	if len(fe.filterCache) >= fe.config.CacheSize {
		// Clear cache when it gets too large
		fe.filterCache = make(map[string]bool)
	}

	fe.filterCache[key] = result
}

func (fe *FilterEngine) updateEventCounts(eventType EventType) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	fe.eventCounts[eventType]++
	fe.totalCount++

	// Reset counts periodically
	if time.Since(fe.lastReset) > time.Minute {
		fe.eventCounts = make(map[EventType]uint64)
		fe.lastReset = time.Now()
	}
}

func (fe *FilterEngine) updateFilterStats(decision FilterDecision) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	if decision.SendSemantic {
		fe.interestingCount++
	}
}

func (fe *FilterEngine) checkRateLimit(event *EnrichedEvent) bool {
	// Simple global rate limiting for now
	// In production, this would implement per-process/container limits
	return true
}

// GetStatistics returns filtering statistics
func (fe *FilterEngine) GetStatistics() map[string]interface{} {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	stats := map[string]interface{}{
		"total_events":       fe.totalCount,
		"interesting_events": fe.interestingCount,
		"filter_ratio":       float64(fe.interestingCount) / float64(fe.totalCount),
		"event_counts":       fe.eventCounts,
		"cache_size":         len(fe.filterCache),
	}

	if fe.sampler != nil {
		fe.sampler.mu.RLock()
		stats["current_sample_rate"] = fe.sampler.currentRate
		stats["events_in_window"] = fe.sampler.eventsInWindow
		fe.sampler.mu.RUnlock()
	}

	return stats
}
