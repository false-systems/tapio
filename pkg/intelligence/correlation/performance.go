package correlation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// PerformanceCorrelator correlates performance issues from eBPF, Kubelet, and service map data
type PerformanceCorrelator struct {
	logger *zap.Logger

	// Recent events cache for correlation
	recentEvents *RecentEventsCache

	// Service connection state from eBPF
	serviceConnections map[string]*ServiceConnectionState
	connMu             sync.RWMutex
}

type RecentEventsCache struct {
	mu     sync.RWMutex
	events map[string][]*domain.UnifiedEvent // key: namespace/pod
	ttl    time.Duration
}

type ServiceConnectionState struct {
	Source       string
	Destination  string
	LastSeen     time.Time
	FailureCount int
}

func NewPerformanceCorrelator(logger *zap.Logger) *PerformanceCorrelator {
	return &PerformanceCorrelator{
		logger: logger,
		recentEvents: &RecentEventsCache{
			events: make(map[string][]*domain.UnifiedEvent),
			ttl:    5 * time.Minute,
		},
		serviceConnections: make(map[string]*ServiceConnectionState),
	}
}

func (p *PerformanceCorrelator) Name() string {
	return "performance"
}

func (p *PerformanceCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Cache event for future correlations
	p.cacheEvent(event)

	// Get event type from metadata if available
	eventType := ""
	if event.Attributes != nil {
		if et, ok := event.Attributes["event_type"].(string); ok {
			eventType = et
		}
	}

	// Route to specific handlers based on event type
	switch eventType {
	// Kubelet events
	case "kubelet_cpu_throttling":
		return p.handleCPUThrottling(ctx, event)
	case "kubelet_memory_pressure":
		return p.handleMemoryPressure(ctx, event)
	case "kubelet_crash_loop":
		return p.handleCrashLoop(ctx, event)
	case "kubelet_container_waiting":
		return p.handleContainerWaiting(ctx, event)
	case "kubelet_ephemeral_storage":
		return p.handleStorageIssue(ctx, event)

	// eBPF events
	case "network_conn":
		return p.handleNetworkConnection(ctx, event)
	case "memory_alloc", "memory_free":
		return p.handleMemoryOperation(ctx, event)
	case "file_open":
		return p.handleFileOperation(ctx, event)

	// Service map events
	case "service_map":
		return p.handleServiceMapUpdate(ctx, event)
	}

	return nil, nil
}

// CPU Throttling often leads to cascade failures
func (p *PerformanceCorrelator) handleCPUThrottling(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	podKey := p.getPodKey(event)

	// Look for related events in the same pod
	relatedEvents := p.findRelatedEvents(podKey, []string{
		"kubelet_memory_pressure",
		"kubelet_crash_loop",
		"network_conn",
	}, 2*time.Minute)

	// Check if this is leading to memory issues
	var memoryPressure *domain.UnifiedEvent
	var crashLoop *domain.UnifiedEvent

	for _, e := range relatedEvents {
		eventType := p.getMetadata(e, "event_type")
		switch eventType {
		case "kubelet_memory_pressure":
			memoryPressure = e
		case "kubelet_crash_loop":
			crashLoop = e
		}
	}

	// Build correlation result
	result := &CorrelationResult{
		ID:         fmt.Sprintf("perf-cpu-cascade-%s", event.ID),
		Type:       "resource_exhaustion",
		Confidence: MediumHighConfidence,
		Events:     []string{event.ID},
		Summary:    fmt.Sprintf("CPU throttling detected in %s", podKey),
		StartTime:  event.Timestamp,
		EndTime:    event.Timestamp,
	}

	// Determine the cascade pattern
	if memoryPressure != nil && crashLoop != nil {
		// Full cascade: CPU → Memory → Crash
		result.Confidence = CriticalConfidence
		result.Events = append(result.Events, memoryPressure.ID, crashLoop.ID)
		result.Summary = "Resource exhaustion cascade: CPU throttling → Memory pressure → Pod crash"
		result.Details = CorrelationDetails{
			Pattern:        "CPU throttling → Memory pressure → Pod crash",
			Algorithm:      "performance_cascade_detector",
			ProcessingTime: time.Since(result.StartTime),
			DataPoints:     3,
		}
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  HighConfidence,
			Description: "Insufficient CPU resources for workload",
			Evidence: CreateEvidenceData(
				[]string{event.ID, memoryPressure.ID, crashLoop.ID},
				[]string{podKey},
				map[string]string{
					"cpu_usage_nano":        p.getMetadata(event, "cpu_usage_nano"),
					"memory_pressure_delay": memoryPressure.Timestamp.Sub(event.Timestamp).String(),
					"last_exit_code":        p.getMetadata(crashLoop, "last_exit_code"),
				},
			),
		}
		result.Impact = &Impact{
			Severity:  domain.EventSeverityCritical,
			Resources: []string{podKey},
		}
	} else if memoryPressure != nil {
		// Partial cascade: CPU → Memory
		result.Confidence = MediumConfidence
		result.Events = append(result.Events, memoryPressure.ID)
		result.Summary = "CPU throttling leading to memory pressure"
		result.Details = CorrelationDetails{
			Pattern:        "CPU throttling → Memory pressure",
			Algorithm:      "performance_cascade_detector",
			ProcessingTime: time.Since(result.StartTime),
			DataPoints:     2,
		}
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  MediumLowConfidence,
			Description: "CPU throttling causing processing backlog",
			Evidence: CreateEvidenceData(
				[]string{event.ID, memoryPressure.ID},
				[]string{podKey},
				map[string]string{
					"pattern": "CPU at limit for extended period",
					"impact":  "Memory usage increasing after CPU throttle",
				},
			),
		}
	} else {
		// Just CPU throttling
		result.Details = CorrelationDetails{
			Pattern:        "CPU throttling",
			Algorithm:      "performance_cascade_detector",
			ProcessingTime: time.Since(result.StartTime),
			DataPoints:     1,
		}
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  LowConfidence,
			Description: "Pod hitting CPU limits",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{podKey},
				map[string]string{
					"container_name": p.getMetadata(event, "container_name"),
					"recommendation": "Consider increasing CPU limits",
				},
			),
		}
	}

	return []*CorrelationResult{result}, nil
}

// Memory pressure correlation
func (p *PerformanceCorrelator) handleMemoryPressure(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	podKey := p.getPodKey(event)

	// Check if there was prior CPU throttling
	relatedEvents := p.findRelatedEvents(podKey, []string{
		"kubelet_cpu_throttling",
		"memory_alloc",
	}, 5*time.Minute)

	// Use related events for enhanced analysis
	_ = len(relatedEvents) // eventCount not currently used

	workingSet, _ := strconv.ParseInt(p.getMetadata(event, "memory_working_set"), 10, 64)
	usage, _ := strconv.ParseInt(p.getMetadata(event, "memory_usage"), 10, 64)

	result := &CorrelationResult{
		ID:         fmt.Sprintf("perf-mem-%s", event.ID),
		Type:       "memory_exhaustion",
		Confidence: HighConfidence,
		Events:     []string{event.ID},
		Summary:    fmt.Sprintf("Memory pressure in %s (usage: %d MB)", podKey, usage/1024/1024),
		Details: CorrelationDetails{
			Pattern:        "Memory pressure",
			Algorithm:      "memory_exhaustion_detector",
			ProcessingTime: time.Since(event.Timestamp),
			DataPoints:     1,
		},
		StartTime: event.Timestamp,
		EndTime:   event.Timestamp,
	}

	// Check for memory leak pattern
	if p.isMemoryLeakPattern(podKey, workingSet) {
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  MediumHighConfidence,
			Description: "Possible memory leak detected",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{podKey},
				map[string]string{
					"pattern":         "Memory usage continuously increasing",
					"workload_status": "No corresponding workload increase",
					"working_set_mb":  fmt.Sprintf("%d", workingSet/1024/1024),
				},
			),
		}
	} else {
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  LowConfidence,
			Description: "High memory usage",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{podKey},
				map[string]string{
					"usage_mb":       fmt.Sprintf("%d", usage/1024/1024),
					"working_set_mb": fmt.Sprintf("%d", workingSet/1024/1024),
				},
			),
		}
	}

	result.Impact = &Impact{
		Severity:  domain.EventSeverityWarning,
		Resources: []string{podKey},
	}

	return []*CorrelationResult{result}, nil
}

// Crash loop analysis
func (p *PerformanceCorrelator) handleCrashLoop(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	exitCode := p.getMetadata(event, "last_exit_code")
	restartCount, _ := strconv.Atoi(p.getMetadata(event, "restart_count"))
	podKey := p.getPodKey(event)

	// Look for recent events that might explain the crash
	recentEvents := p.findRelatedEvents(podKey, []string{
		"kubelet_memory_pressure",
		"kubelet_cpu_throttling",
		"file_open",
		"kubelet_container_waiting",
	}, 10*time.Minute)

	result := &CorrelationResult{
		ID:         fmt.Sprintf("perf-crash-%s", event.ID),
		Type:       "crash_analysis",
		Confidence: HighConfidence,
		Events:     []string{event.ID},
		Summary:    fmt.Sprintf("Pod %s crashed %d times (exit: %s)", podKey, restartCount, exitCode),
		StartTime:  event.Timestamp,
		EndTime:    event.Timestamp,
	}

	// Analyze based on exit code
	switch exitCode {
	case "137": // SIGKILL - usually OOM
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  CriticalConfidence,
			Description: "Container killed due to Out Of Memory",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{p.getPodKey(event)},
				map[string]string{
					"exit_code": "137",
					"signal":    "SIGKILL from OOM killer",
					"cause":     "Container exceeded memory limits",
				},
			),
		}

	case "1": // General error
		// Check if config related
		var configAccess *domain.UnifiedEvent
		for _, e := range recentEvents {
			if p.getMetadata(e, "event_type") == "file_open" && strings.Contains(p.getMetadata(e, "filename"), "config") {
				configAccess = e
				break
			}
		}

		if configAccess != nil {
			result.RootCause = &RootCause{
				EventID:     event.ID,
				Confidence:  LowConfidence,
				Description: "Application error, possibly configuration related",
				Evidence: CreateEvidenceData(
					[]string{event.ID, configAccess.ID},
					[]string{p.getPodKey(event)},
					map[string]string{
						"exit_code":   "1",
						"cause":       "application error",
						"config_file": p.getMetadata(configAccess, "filename"),
					},
				),
			}
		} else {
			result.RootCause = &RootCause{
				EventID:     event.ID,
				Confidence:  VeryLowConfidence,
				Description: "Application startup or runtime error",
				Evidence: CreateEvidenceData(
					[]string{event.ID},
					[]string{p.getPodKey(event)},
					map[string]string{
						"exit_code":      "1",
						"recommendation": "Check application logs for specific error",
					},
				),
			}
		}

	case "139": // SIGSEGV
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  HighConfidence,
			Description: "Segmentation fault - memory access violation",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{p.getPodKey(event)},
				map[string]string{
					"exit_code": "139",
					"signal":    "SIGSEGV",
					"cause":     "Application bug or corrupted memory",
				},
			),
		}
	}

	result.Impact = &Impact{
		Severity:  domain.EventSeverityCritical,
		Resources: []string{podKey},
	}

	return []*CorrelationResult{result}, nil
}

// Container waiting (image pull, etc)
func (p *PerformanceCorrelator) handleContainerWaiting(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	reason := p.getMetadata(event, "waiting_reason")
	message := p.getMetadata(event, "waiting_message")

	result := &CorrelationResult{
		ID:         fmt.Sprintf("perf-waiting-%s", event.ID),
		Type:       "startup_failure",
		Confidence: CriticalConfidence,
		Events:     []string{event.ID},
		Summary:    fmt.Sprintf("Container cannot start: %s", reason),
		Details: CorrelationDetails{
			Pattern:        "Container startup failure",
			Algorithm:      "waiting_reason_analyzer",
			ProcessingTime: time.Since(event.Timestamp),
			DataPoints:     1,
		},
		StartTime: event.Timestamp,
		EndTime:   event.Timestamp,
	}

	switch reason {
	case "ImagePullBackOff", "ErrImagePull":
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  CriticalConfidence,
			Description: "Cannot pull container image",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{p.getPodKey(event)},
				map[string]string{
					"waiting_reason":  reason,
					"waiting_message": message,
					"check_1":         "Registry may be unreachable",
					"check_2":         "Image name may be incorrect",
					"check_3":         "Registry credentials may be missing",
				},
			),
		}

	case "CreateContainerConfigError":
		result.RootCause = &RootCause{
			EventID:     event.ID,
			Confidence:  HighConfidence,
			Description: "Container configuration error",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{p.getPodKey(event)},
				map[string]string{
					"error_type": "CreateContainerConfigError",
					"check_1":    "ConfigMap or Secret may be missing",
					"check_2":    "Volume mounts may be invalid",
				},
			),
		}
	}

	result.Impact = &Impact{
		Severity:  domain.EventSeverityCritical,
		Resources: []string{p.getPodKey(event)},
	}

	return []*CorrelationResult{result}, nil
}

// Network connection tracking
func (p *PerformanceCorrelator) handleNetworkConnection(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Track service connections for later correlation
	if p.getMetadata(event, "service_name") != "" {
		connKey := fmt.Sprintf("%s:%s->%s:%s",
			p.getMetadata(event, "src_ip"), p.getMetadata(event, "src_port"),
			p.getMetadata(event, "dst_ip"), p.getMetadata(event, "dst_port"))

		p.connMu.Lock()
		p.serviceConnections[connKey] = &ServiceConnectionState{
			Source:      p.getMetadata(event, "k8s_name"),
			Destination: p.getMetadata(event, "service_name"),
			LastSeen:    event.Timestamp,
		}
		p.connMu.Unlock()
	}

	// For now, just track - correlate when service issues arise
	return nil, nil
}

// Service map updates
func (p *PerformanceCorrelator) handleServiceMapUpdate(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Service map gives us visibility into connection patterns
	activeConns, _ := strconv.Atoi(p.getMetadata(event, "active_connections"))

	if activeConns == 0 {
		return []*CorrelationResult{{
			ID:         fmt.Sprintf("perf-svcmap-%s", event.ID),
			Type:       "service_isolation",
			Confidence: 0.7,
			Events:     []string{event.ID},
			Summary:    "No active service connections detected",
			Details: CorrelationDetails{
				Pattern:        "Service isolation detected",
				Algorithm:      "service_map_analyzer",
				ProcessingTime: time.Since(event.Timestamp),
				DataPoints:     1,
			},
			Impact: &Impact{
				Severity: domain.EventSeverityWarning,
			},
		}}, nil
	}

	return nil, nil
}

// Storage issues
func (p *PerformanceCorrelator) handleStorageIssue(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	usagePercent, _ := strconv.ParseFloat(p.getMetadata(event, "storage_usage_percent"), 64)

	return []*CorrelationResult{{
		ID:         fmt.Sprintf("perf-storage-%s", event.ID),
		Type:       "storage_exhaustion",
		Confidence: HighConfidence,
		Events:     []string{event.ID},
		Summary:    fmt.Sprintf("Ephemeral storage at %.1f%% capacity", usagePercent),
		Details: CorrelationDetails{
			Pattern:        "Storage exhaustion",
			Algorithm:      "storage_pressure_analyzer",
			ProcessingTime: time.Since(event.Timestamp),
			DataPoints:     1,
		},
		RootCause: &RootCause{
			EventID:     event.ID,
			Confidence:  MediumHighConfidence,
			Description: "Excessive log output or temp file creation",
			Evidence: CreateEvidenceData(
				[]string{event.ID},
				[]string{p.getPodKey(event)},
				map[string]string{
					"storage_usage_percent": fmt.Sprintf("%.1f", usagePercent),
					"check_1":               "Check for verbose logging",
					"check_2":               "Look for temp file cleanup issues",
				},
			),
		},
		Impact: &Impact{
			Severity:  domain.EventSeverityWarning,
			Resources: []string{p.getPodKey(event)},
		},
	}}, nil
}

// Memory operations from eBPF
func (p *PerformanceCorrelator) handleMemoryOperation(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Track but don't create correlations unless part of a pattern
	return nil, nil
}

// File operations from eBPF
func (p *PerformanceCorrelator) handleFileOperation(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	filename := p.getMetadata(event, "filename")

	// Only correlate config/secret access for now
	if strings.Contains(filename, "configmap") || strings.Contains(filename, "secret") {
		return []*CorrelationResult{{
			ID:         fmt.Sprintf("perf-config-%s", event.ID),
			Type:       "configuration_change",
			Confidence: 0.6,
			Events:     []string{event.ID},
			Summary:    "Configuration file accessed",
			Details: CorrelationDetails{
				Pattern:        "Configuration file access",
				Algorithm:      "file_access_monitor",
				ProcessingTime: time.Since(event.Timestamp),
				DataPoints:     1,
			},
		}}, nil
	}

	return nil, nil
}

// Helper methods

func (p *PerformanceCorrelator) cacheEvent(event *domain.UnifiedEvent) {
	if event.K8sContext == nil {
		return
	}

	key := p.getPodKey(event)
	p.recentEvents.mu.Lock()
	defer p.recentEvents.mu.Unlock()

	p.recentEvents.events[key] = append(p.recentEvents.events[key], event)

	// Keep only recent events
	cutoff := time.Now().Add(-p.recentEvents.ttl)
	filtered := make([]*domain.UnifiedEvent, 0)
	for _, e := range p.recentEvents.events[key] {
		if e.Timestamp.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	p.recentEvents.events[key] = filtered
}

func (p *PerformanceCorrelator) findRelatedEvents(podKey string, eventTypes []string, window time.Duration) []*domain.UnifiedEvent {
	p.recentEvents.mu.RLock()
	defer p.recentEvents.mu.RUnlock()

	related := make([]*domain.UnifiedEvent, 0)
	cutoff := time.Now().Add(-window)

	for _, event := range p.recentEvents.events[podKey] {
		if event.Timestamp.After(cutoff) {
			eventType := p.getMetadata(event, "event_type")
			for _, targetType := range eventTypes {
				if eventType == targetType {
					related = append(related, event)
					break
				}
			}
		}
	}

	return related
}

func (p *PerformanceCorrelator) getPodKey(event *domain.UnifiedEvent) string {
	if event.K8sContext != nil {
		return fmt.Sprintf("%s/%s", event.K8sContext.Namespace, event.K8sContext.Name)
	}
	// Fallback to metadata
	if ns := p.getMetadata(event, "k8s_namespace"); ns != "" {
		if name := p.getMetadata(event, "k8s_name"); name != "" {
			return fmt.Sprintf("%s/%s", ns, name)
		}
	}
	return "unknown"
}

func (p *PerformanceCorrelator) isMemoryLeakPattern(podKey string, currentUsage int64) bool {
	// Simple check: if we have 3+ memory events with increasing usage
	events := p.recentEvents.events[podKey]
	memEvents := make([]int64, 0)

	for _, e := range events {
		if p.getMetadata(e, "event_type") == "kubelet_memory_pressure" {
			if usage, err := strconv.ParseInt(p.getMetadata(e, "memory_usage"), 10, 64); err == nil {
				memEvents = append(memEvents, usage)
			}
		}
	}

	if len(memEvents) < 3 {
		return false
	}

	// Check if monotonically increasing
	for i := 1; i < len(memEvents); i++ {
		if memEvents[i] <= memEvents[i-1] {
			return false
		}
	}

	return true
}

// getMetadata retrieves metadata from event attributes
func (p *PerformanceCorrelator) getMetadata(event *domain.UnifiedEvent, key string) string {
	if event.Attributes != nil {
		if val, ok := event.Attributes[key].(string); ok {
			return val
		}
	}
	return ""
}
