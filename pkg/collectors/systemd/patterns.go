package systemd

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// RestartPatternDetector detects restart patterns and anomalies in service behavior
type RestartPatternDetector struct {
	config RestartPatternConfig
	
	// Pattern storage with efficient lookups
	patterns     map[string]*ServicePattern
	patternsMu   sync.RWMutex
	
	// Anomaly detection state
	baselineData map[string]*BaselineMetrics
	baselineMu   sync.RWMutex
	
	// ML-based detection (simplified for now)
	anomalyThresholds map[string]float64
}

// RestartPatternConfig configures pattern detection
type RestartPatternConfig struct {
	Window               time.Duration
	Threshold            int
	AnomalyDetection     bool
	BaselinePeriod       time.Duration
	AnomalyStdDevs       float64
	PatternRetention     time.Duration
}

// ServicePattern represents detected patterns for a service
type ServicePattern struct {
	ServiceName      string
	Type             string
	FirstDetected    time.Time
	LastDetected     time.Time
	Occurrences      int
	
	// Pattern specifics
	RestartIntervals []time.Duration
	AvgInterval      time.Duration
	StdDevInterval   time.Duration
	
	// Failure correlation
	FailureReasons   map[string]int
	TimeOfDayPattern map[int]int // Hour -> count
	
	// Anomaly scoring
	IsAnomaly        bool
	AnomalyScore     float64
	AnomalyReason    string
}

// BaselineMetrics tracks normal behavior for anomaly detection
type BaselineMetrics struct {
	ServiceName         string
	BaselinePeriod      time.Duration
	
	// Normal restart behavior
	NormalRestartRate   float64 // restarts per hour
	NormalIntervals     []time.Duration
	MeanInterval        time.Duration
	StdDevInterval      time.Duration
	
	// Time patterns
	HourlyDistribution  [24]float64 // Expected restarts per hour of day
	DailyDistribution   [7]float64  // Expected restarts per day of week
	
	// Resource usage at restart
	TypicalMemoryUsage  uint64
	TypicalCPUUsage     uint64
	
	LastUpdated         time.Time
}

// Pattern types
const (
	PatternCrashLoop      = "crash_loop"
	PatternPeriodicCrash  = "periodic_crash"
	PatternMemoryLeak     = "memory_leak"
	PatternTimeBasedCrash = "time_based_crash"
	PatternDependencyCrash = "dependency_crash"
	PatternRandomCrash    = "random_crash"
	PatternRapidRestart   = "rapid_restart"
)

// NewRestartPatternDetector creates a new pattern detector
func NewRestartPatternDetector(config RestartPatternConfig) *RestartPatternDetector {
	return &RestartPatternDetector{
		config:            config,
		patterns:          make(map[string]*ServicePattern),
		baselineData:      make(map[string]*BaselineMetrics),
		anomalyThresholds: make(map[string]float64),
	}
}

// DetectPattern analyzes restart history and detects patterns
func (rpd *RestartPatternDetector) DetectPattern(serviceName string, restartHistory []time.Time) *ServicePattern {
	if len(restartHistory) < 2 {
		return nil
	}
	
	// Filter recent restarts within window
	now := time.Now()
	cutoff := now.Add(-rpd.config.Window)
	
	var recentRestarts []time.Time
	for _, t := range restartHistory {
		if t.After(cutoff) {
			recentRestarts = append(recentRestarts, t)
		}
	}
	
	// Check if we meet threshold
	if len(recentRestarts) < rpd.config.Threshold {
		return nil
	}
	
	// Calculate intervals
	intervals := rpd.calculateIntervals(recentRestarts)
	
	// Detect pattern type
	patternType := rpd.classifyPattern(intervals, recentRestarts)
	
	// Calculate statistics
	avgInterval, stdDev := rpd.calculateStats(intervals)
	
	// Create or update pattern
	pattern := &ServicePattern{
		ServiceName:      serviceName,
		Type:             patternType,
		FirstDetected:    recentRestarts[0],
		LastDetected:     recentRestarts[len(recentRestarts)-1],
		Occurrences:      len(recentRestarts),
		RestartIntervals: intervals,
		AvgInterval:      avgInterval,
		StdDevInterval:   stdDev,
		FailureReasons:   make(map[string]int),
		TimeOfDayPattern: rpd.analyzeTimePattern(recentRestarts),
	}
	
	// Check for anomalies if enabled
	if rpd.config.AnomalyDetection {
		rpd.detectAnomaly(serviceName, pattern, recentRestarts)
	}
	
	// Store pattern
	rpd.patternsMu.Lock()
	rpd.patterns[serviceName] = pattern
	rpd.patternsMu.Unlock()
	
	return pattern
}

// calculateIntervals calculates time intervals between restarts
func (rpd *RestartPatternDetector) calculateIntervals(restarts []time.Time) []time.Duration {
	if len(restarts) < 2 {
		return nil
	}
	
	// Sort restarts chronologically
	sort.Slice(restarts, func(i, j int) bool {
		return restarts[i].Before(restarts[j])
	})
	
	intervals := make([]time.Duration, 0, len(restarts)-1)
	for i := 1; i < len(restarts); i++ {
		intervals = append(intervals, restarts[i].Sub(restarts[i-1]))
	}
	
	return intervals
}

// classifyPattern determines the type of restart pattern
func (rpd *RestartPatternDetector) classifyPattern(intervals []time.Duration, restarts []time.Time) string {
	if len(intervals) == 0 {
		return PatternRandomCrash
	}
	
	// Rapid restart detection (crash loop)
	rapidCount := 0
	for _, interval := range intervals {
		if interval < 30*time.Second {
			rapidCount++
		}
	}
	if float64(rapidCount)/float64(len(intervals)) > 0.8 {
		return PatternCrashLoop
	}
	
	// Check for rapid consecutive restarts
	if len(intervals) >= 3 {
		lastThree := intervals[len(intervals)-3:]
		allRapid := true
		for _, interval := range lastThree {
			if interval > 1*time.Minute {
				allRapid = false
				break
			}
		}
		if allRapid {
			return PatternRapidRestart
		}
	}
	
	// Periodic pattern detection
	if rpd.isPeriodicPattern(intervals) {
		return PatternPeriodicCrash
	}
	
	// Time-based pattern detection
	if rpd.isTimeBasedPattern(restarts) {
		return PatternTimeBasedCrash
	}
	
	// Memory leak pattern (would need memory data)
	// This is a placeholder - real implementation would check memory usage
	if rpd.hasIncreasingMemoryPattern(restarts) {
		return PatternMemoryLeak
	}
	
	return PatternRandomCrash
}

// isPeriodicPattern checks if restarts follow a periodic pattern
func (rpd *RestartPatternDetector) isPeriodicPattern(intervals []time.Duration) bool {
	if len(intervals) < 3 {
		return false
	}
	
	// Calculate coefficient of variation
	avg, stdDev := rpd.calculateStats(intervals)
	if avg == 0 {
		return false
	}
	
	cv := float64(stdDev) / float64(avg)
	
	// Low coefficient of variation indicates periodic pattern
	return cv < 0.3
}

// isTimeBasedPattern checks if restarts correlate with time of day
func (rpd *RestartPatternDetector) isTimeBasedPattern(restarts []time.Time) bool {
	if len(restarts) < 5 {
		return false
	}
	
	// Count restarts by hour
	hourCounts := make(map[int]int)
	for _, t := range restarts {
		hourCounts[t.Hour()]++
	}
	
	// Check if restarts cluster around specific hours
	maxCount := 0
	totalHours := len(hourCounts)
	
	for _, count := range hourCounts {
		if count > maxCount {
			maxCount = count
		}
	}
	
	// If >50% of restarts happen in <25% of hours, it's time-based
	return totalHours <= 6 && float64(maxCount)/float64(len(restarts)) > 0.5
}

// hasIncreasingMemoryPattern placeholder for memory leak detection
func (rpd *RestartPatternDetector) hasIncreasingMemoryPattern(restarts []time.Time) bool {
	// This would need actual memory usage data
	// For now, return false
	return false
}

// calculateStats calculates average and standard deviation
func (rpd *RestartPatternDetector) calculateStats(intervals []time.Duration) (time.Duration, time.Duration) {
	if len(intervals) == 0 {
		return 0, 0
	}
	
	// Calculate mean
	var sum time.Duration
	for _, interval := range intervals {
		sum += interval
	}
	mean := sum / time.Duration(len(intervals))
	
	// Calculate standard deviation
	var varianceSum float64
	for _, interval := range intervals {
		diff := float64(interval - mean)
		varianceSum += diff * diff
	}
	
	variance := varianceSum / float64(len(intervals))
	stdDev := time.Duration(math.Sqrt(variance))
	
	return mean, stdDev
}

// analyzeTimePattern analyzes time-of-day patterns
func (rpd *RestartPatternDetector) analyzeTimePattern(restarts []time.Time) map[int]int {
	pattern := make(map[int]int)
	
	for _, t := range restarts {
		pattern[t.Hour()]++
	}
	
	return pattern
}

// detectAnomaly checks if the pattern is anomalous
func (rpd *RestartPatternDetector) detectAnomaly(serviceName string, pattern *ServicePattern, restarts []time.Time) {
	rpd.baselineMu.RLock()
	baseline, hasBaseline := rpd.baselineData[serviceName]
	rpd.baselineMu.RUnlock()
	
	if !hasBaseline {
		// No baseline yet, can't detect anomaly
		// Start building baseline
		rpd.updateBaseline(serviceName, restarts)
		return
	}
	
	// Compare current pattern to baseline
	anomalyScore := 0.0
	anomalyReasons := []string{}
	
	// Check restart rate
	currentRate := float64(len(restarts)) / rpd.config.Window.Hours()
	if baseline.NormalRestartRate > 0 {
		rateDiff := math.Abs(currentRate - baseline.NormalRestartRate)
		rateStdDevs := rateDiff / (baseline.NormalRestartRate * 0.25) // Assume 25% std dev
		
		if rateStdDevs > rpd.config.AnomalyStdDevs {
			anomalyScore += rateStdDevs
			anomalyReasons = append(anomalyReasons, 
				fmt.Sprintf("restart rate %.2f/hr (normal: %.2f/hr)", 
					currentRate, baseline.NormalRestartRate))
		}
	}
	
	// Check interval consistency
	if pattern.AvgInterval > 0 && baseline.MeanInterval > 0 {
		intervalDiff := math.Abs(float64(pattern.AvgInterval - baseline.MeanInterval))
		intervalStdDevs := intervalDiff / float64(baseline.StdDevInterval)
		
		if intervalStdDevs > rpd.config.AnomalyStdDevs {
			anomalyScore += intervalStdDevs
			anomalyReasons = append(anomalyReasons, 
				fmt.Sprintf("unusual interval %s (normal: %s)", 
					pattern.AvgInterval, baseline.MeanInterval))
		}
	}
	
	// Check time-of-day pattern
	anomalousHours := rpd.checkTimeAnomalies(pattern.TimeOfDayPattern, baseline.HourlyDistribution)
	if len(anomalousHours) > 0 {
		anomalyScore += float64(len(anomalousHours))
		anomalyReasons = append(anomalyReasons, 
			fmt.Sprintf("unusual time pattern at hours %v", anomalousHours))
	}
	
	// Set anomaly flags
	if anomalyScore > rpd.config.AnomalyStdDevs {
		pattern.IsAnomaly = true
		pattern.AnomalyScore = anomalyScore
		if len(anomalyReasons) > 0 {
			pattern.AnomalyReason = anomalyReasons[0]
		}
	}
}

// checkTimeAnomalies finds anomalous hours in restart pattern
func (rpd *RestartPatternDetector) checkTimeAnomalies(current map[int]int, baseline [24]float64) []int {
	var anomalousHours []int
	
	// Calculate total restarts
	totalRestarts := 0
	for _, count := range current {
		totalRestarts += count
	}
	
	if totalRestarts == 0 {
		return anomalousHours
	}
	
	// Check each hour
	for hour, count := range current {
		expectedRate := baseline[hour]
		actualRate := float64(count) / float64(totalRestarts)
		
		if expectedRate > 0 {
			diff := math.Abs(actualRate - expectedRate)
			if diff > 0.2 { // 20% difference threshold
				anomalousHours = append(anomalousHours, hour)
			}
		} else if actualRate > 0.3 { // Significant activity in normally quiet hour
			anomalousHours = append(anomalousHours, hour)
		}
	}
	
	return anomalousHours
}

// updateBaseline updates baseline metrics for a service
func (rpd *RestartPatternDetector) updateBaseline(serviceName string, restarts []time.Time) {
	// Calculate baseline metrics
	baseline := &BaselineMetrics{
		ServiceName:    serviceName,
		BaselinePeriod: rpd.config.BaselinePeriod,
		LastUpdated:    time.Now(),
	}
	
	// Calculate restart rate
	if len(restarts) > 0 {
		timeSpan := restarts[len(restarts)-1].Sub(restarts[0])
		if timeSpan > 0 {
			baseline.NormalRestartRate = float64(len(restarts)) / timeSpan.Hours()
		}
	}
	
	// Calculate interval statistics
	intervals := rpd.calculateIntervals(restarts)
	if len(intervals) > 0 {
		baseline.NormalIntervals = intervals
		baseline.MeanInterval, baseline.StdDevInterval = rpd.calculateStats(intervals)
	}
	
	// Calculate time distribution
	for _, t := range restarts {
		baseline.HourlyDistribution[t.Hour()]++
		baseline.DailyDistribution[int(t.Weekday())]++
	}
	
	// Normalize distributions
	if len(restarts) > 0 {
		for i := range baseline.HourlyDistribution {
			baseline.HourlyDistribution[i] /= float64(len(restarts))
		}
		for i := range baseline.DailyDistribution {
			baseline.DailyDistribution[i] /= float64(len(restarts))
		}
	}
	
	// Store baseline
	rpd.baselineMu.Lock()
	rpd.baselineData[serviceName] = baseline
	rpd.baselineMu.Unlock()
}

// GetPattern retrieves the current pattern for a service
func (rpd *RestartPatternDetector) GetPattern(serviceName string) *ServicePattern {
	rpd.patternsMu.RLock()
	defer rpd.patternsMu.RUnlock()
	
	return rpd.patterns[serviceName]
}

// GetAllPatterns returns all detected patterns
func (rpd *RestartPatternDetector) GetAllPatterns() map[string]*ServicePattern {
	rpd.patternsMu.RLock()
	defer rpd.patternsMu.RUnlock()
	
	// Return a copy
	patterns := make(map[string]*ServicePattern)
	for k, v := range rpd.patterns {
		patterns[k] = v
	}
	
	return patterns
}

// CleanupOldPatterns removes patterns older than retention period
func (rpd *RestartPatternDetector) CleanupOldPatterns() {
	rpd.patternsMu.Lock()
	defer rpd.patternsMu.Unlock()
	
	cutoff := time.Now().Add(-rpd.config.PatternRetention)
	
	for service, pattern := range rpd.patterns {
		if pattern.LastDetected.Before(cutoff) {
			delete(rpd.patterns, service)
		}
	}
}

// GetAnomalousServices returns services with anomalous patterns
func (rpd *RestartPatternDetector) GetAnomalousServices() []string {
	rpd.patternsMu.RLock()
	defer rpd.patternsMu.RUnlock()
	
	var anomalous []string
	for service, pattern := range rpd.patterns {
		if pattern.IsAnomaly {
			anomalous = append(anomalous, service)
		}
	}
	
	return anomalous
}

// ServiceDependencyGraph tracks service dependencies
type ServiceDependencyGraph struct {
	// Graph representation
	dependencies  map[string][]string // service -> services it depends on
	dependents    map[string][]string // service -> services that depend on it
	mu            sync.RWMutex
}

// NewServiceDependencyGraph creates a new dependency graph
func NewServiceDependencyGraph() *ServiceDependencyGraph {
	return &ServiceDependencyGraph{
		dependencies: make(map[string][]string),
		dependents:   make(map[string][]string),
	}
}

// AddDependency adds a dependency relationship
func (sdg *ServiceDependencyGraph) AddDependency(service, dependsOn string) {
	sdg.mu.Lock()
	defer sdg.mu.Unlock()
	
	// Add to dependencies
	if !sdg.contains(sdg.dependencies[service], dependsOn) {
		sdg.dependencies[service] = append(sdg.dependencies[service], dependsOn)
	}
	
	// Add to dependents
	if !sdg.contains(sdg.dependents[dependsOn], service) {
		sdg.dependents[dependsOn] = append(sdg.dependents[dependsOn], service)
	}
}

// GetDependencies returns services that the given service depends on
func (sdg *ServiceDependencyGraph) GetDependencies(service string) []string {
	sdg.mu.RLock()
	defer sdg.mu.RUnlock()
	
	deps := make([]string, len(sdg.dependencies[service]))
	copy(deps, sdg.dependencies[service])
	return deps
}

// GetDependents returns services that depend on the given service
func (sdg *ServiceDependencyGraph) GetDependents(service string) []string {
	sdg.mu.RLock()
	defer sdg.mu.RUnlock()
	
	deps := make([]string, len(sdg.dependents[service]))
	copy(deps, sdg.dependents[service])
	return deps
}

// GetTransitiveDependents returns all services affected by a service failure
func (sdg *ServiceDependencyGraph) GetTransitiveDependents(service string, maxDepth int) []string {
	sdg.mu.RLock()
	defer sdg.mu.RUnlock()
	
	visited := make(map[string]bool)
	var result []string
	
	sdg.dfs(service, visited, &result, 0, maxDepth, true)
	
	return result
}

// GetTransitiveDependencies returns all services that a service depends on
func (sdg *ServiceDependencyGraph) GetTransitiveDependencies(service string, maxDepth int) []string {
	sdg.mu.RLock()
	defer sdg.mu.RUnlock()
	
	visited := make(map[string]bool)
	var result []string
	
	sdg.dfs(service, visited, &result, 0, maxDepth, false)
	
	return result
}

// dfs performs depth-first search for dependencies
func (sdg *ServiceDependencyGraph) dfs(service string, visited map[string]bool, 
	result *[]string, depth, maxDepth int, followDependents bool) {
	
	if depth >= maxDepth || visited[service] {
		return
	}
	
	visited[service] = true
	
	var neighbors []string
	if followDependents {
		neighbors = sdg.dependents[service]
	} else {
		neighbors = sdg.dependencies[service]
	}
	
	for _, neighbor := range neighbors {
		if !visited[neighbor] {
			*result = append(*result, neighbor)
			sdg.dfs(neighbor, visited, result, depth+1, maxDepth, followDependents)
		}
	}
}

// contains checks if a slice contains a string
func (sdg *ServiceDependencyGraph) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}