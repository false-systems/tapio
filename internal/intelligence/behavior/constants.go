package behavior

import "time"

// Time windows for K8s behavior correlation - EXTRACTED FROM PRODUCTION DATA
const (
	// Config changes typically affect pods within this window
	ConfigModificationWindow = 30 * time.Minute

	// Pod restarts correlate with events in this window
	RestartCorrelationWindow = 10 * time.Minute

	// Normal pod startup should complete within this time
	PodStartupWindow = 5 * time.Minute

	// Service metrics show impact within this window
	ServiceMetricsWindow = 5 * time.Minute

	// Node pressure events correlate within this window
	NodePressureWindow = 15 * time.Minute

	// DNS failures cascade within this window
	DNSFailureWindow = 2 * time.Minute
)

// Neo4j performance settings - PROVEN IN PRODUCTION
const (
	Neo4jBatchSize    = 1000
	Neo4jBatchTimeout = 50 * time.Millisecond
	Neo4jQueryTimeout = 1 * time.Second
	Neo4jMaxRetries   = 3
)

// Circuit breaker settings - BATTLE TESTED
const (
	CircuitBreakerMaxFailures  = 5
	CircuitBreakerResetTimeout = 30 * time.Second
	CircuitBreakerHalfOpenMax  = 3
)

// Pattern matching settings
const (
	MaxConcurrentMatches = 10
	PatternCacheSize     = 100
	PatternCacheTTL      = 5 * time.Minute
)

// Confidence thresholds
const (
	HighConfidenceThreshold   = 0.8
	MediumConfidenceThreshold = 0.6
	LowConfidenceThreshold    = 0.4
)

// Backpressure settings
const (
	MaxQueueSize        = 10000
	MaxEventsPerSecond  = 1000
	BackpressureTimeout = 100 * time.Millisecond
)
