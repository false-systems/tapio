//go:build linux
// +build linux

package network

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Constants for configuration - no magic numbers
const (
	// Latency baseline smoothing factor (exponential moving average)
	LatencyAlpha = 0.1

	// Map cleanup intervals
	DependencyCacheTTL = 5 * time.Minute
	BaselineStaleTime  = 10 * time.Minute
	ErrorWindowTTL     = 2 * time.Minute

	// Map size limits to prevent unbounded growth
	MaxServiceDependencies = 10000
	MaxLatencyBaselines    = 5000
	MaxErrorTrackers       = 1000

	// Channel saturation thresholds
	ChannelHighWaterMark = 0.8  // 80% full triggers warning
	ChannelDropThreshold = 0.95 // 95% full starts dropping
)

// safeIncrementStats safely increments statistics with mutex protection
func (ic *IntelligenceCollector) safeIncrementStats(field string, value int64) {
	ic.mutex.Lock()
	defer ic.mutex.Unlock()

	switch field {
	case "events_processed":
		ic.intelStats.EventsProcessed += value
	case "dependencies_found":
		ic.intelStats.DependenciesFound += value
	case "error_patterns":
		ic.intelStats.ErrorPatternsFound += value
	case "latency_anomalies":
		ic.intelStats.LatencyAnomalies += value
	case "dns_failures":
		ic.intelStats.DNSFailures += value
	case "security_concerns":
		ic.intelStats.SecurityConcerns += value
	case "intelligent_events":
		ic.intelStats.IntelligentEventsEmitted += value
	}
}

// cleanupStaleEntries periodically removes old entries from maps to prevent unbounded growth
func (ic *IntelligenceCollector) cleanupStaleEntries() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ic.ctx.Done():
			return
		case <-ticker.C:
			ic.performMapCleanup()
		}
	}
}

// performMapCleanup removes stale entries from all tracking maps
func (ic *IntelligenceCollector) performMapCleanup() {
	ic.mutex.Lock()
	defer ic.mutex.Unlock()

	now := time.Now()

	// Cleanup service dependencies
	if len(ic.serviceDependencies) > MaxServiceDependencies {
		ic.evictOldestDependencies(MaxServiceDependencies / 2)
	}
	for key, dep := range ic.serviceDependencies {
		if now.Sub(dep.LastSeen) > DependencyCacheTTL {
			delete(ic.serviceDependencies, key)
		}
	}

	// Cleanup latency baselines
	if len(ic.latencyBaselines) > MaxLatencyBaselines {
		ic.evictOldestBaselines(MaxLatencyBaselines / 2)
	}
	for key, baseline := range ic.latencyBaselines {
		if now.Sub(baseline.LastUpdate) > BaselineStaleTime {
			delete(ic.latencyBaselines, key)
		}
	}

	// Cleanup error cascade trackers
	if len(ic.errorCascadeTracker) > MaxErrorTrackers {
		ic.evictOldestTrackers(MaxErrorTrackers / 2)
	}
	for key, tracker := range ic.errorCascadeTracker {
		if now.Sub(tracker.WindowStart) > ErrorWindowTTL {
			delete(ic.errorCascadeTracker, key)
		}
	}

	ic.logger.Debug("Cleaned up stale map entries",
		zap.Int("dependencies", len(ic.serviceDependencies)),
		zap.Int("baselines", len(ic.latencyBaselines)),
		zap.Int("error_trackers", len(ic.errorCascadeTracker)))
}

// evictOldestDependencies removes the oldest N service dependencies
func (ic *IntelligenceCollector) evictOldestDependencies(count int) {
	type entry struct {
		key      string
		lastSeen time.Time
	}

	entries := make([]entry, 0, len(ic.serviceDependencies))
	for k, v := range ic.serviceDependencies {
		entries = append(entries, entry{k, v.LastSeen})
	}

	// Sort by LastSeen (oldest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastSeen.Before(entries[j].lastSeen)
	})

	// Remove oldest entries
	for i := 0; i < count && i < len(entries); i++ {
		delete(ic.serviceDependencies, entries[i].key)
	}
}

// evictOldestBaselines removes the oldest N latency baselines
func (ic *IntelligenceCollector) evictOldestBaselines(count int) {
	type entry struct {
		key        string
		lastUpdate time.Time
	}

	entries := make([]entry, 0, len(ic.latencyBaselines))
	for k, v := range ic.latencyBaselines {
		entries = append(entries, entry{k, v.LastUpdate})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastUpdate.Before(entries[j].lastUpdate)
	})

	for i := 0; i < count && i < len(entries); i++ {
		delete(ic.latencyBaselines, entries[i].key)
	}
}

// evictOldestTrackers removes the oldest N error trackers
func (ic *IntelligenceCollector) evictOldestTrackers(count int) {
	type entry struct {
		key         string
		windowStart time.Time
	}

	entries := make([]entry, 0, len(ic.errorCascadeTracker))
	for k, v := range ic.errorCascadeTracker {
		entries = append(entries, entry{k, v.WindowStart})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].windowStart.Before(entries[j].windowStart)
	})

	for i := 0; i < count && i < len(entries); i++ {
		delete(ic.errorCascadeTracker, entries[i].key)
	}
}

// sendIntelligenceEventSafe safely sends an event to the intelligence channel with overflow protection
func (ic *IntelligenceCollector) sendIntelligenceEventSafe(event *IntelligenceEvent) {
	// Calculate channel utilization
	channelLen := len(ic.intelligenceEvents)
	channelCap := cap(ic.intelligenceEvents)
	utilization := float64(channelLen) / float64(channelCap)

	// Log warning if channel is getting full
	if utilization > ChannelHighWaterMark {
		ic.logger.Warn("Intelligence events channel nearing capacity",
			zap.Int("current", channelLen),
			zap.Int("capacity", channelCap),
			zap.Float64("utilization", utilization))
	}

	// Drop events if channel is too full
	if utilization > ChannelDropThreshold {
		ic.logger.Error("Dropping intelligence event due to channel overflow",
			zap.String("event_type", fmt.Sprintf("%d", event.Type)),
			zap.Float64("utilization", utilization))
		ic.safeIncrementStats("events_dropped", 1)
		return
	}

	// Try to send with timeout
	select {
	case ic.intelligenceEvents <- event:
		// Success
	case <-time.After(100 * time.Millisecond):
		ic.logger.Warn("Timeout sending intelligence event, dropping",
			zap.String("event_type", fmt.Sprintf("%d", event.Type)))
		ic.safeIncrementStats("events_dropped", 1)
	}
}

// channelCloseOnce ensures the channel is only closed once
var channelCloseOnce sync.Once

// safeCloseIntelligenceChannel safely closes the intelligence events channel
func (ic *IntelligenceCollector) safeCloseIntelligenceChannel() {
	channelCloseOnce.Do(func() {
		close(ic.intelligenceEvents)
	})
}
