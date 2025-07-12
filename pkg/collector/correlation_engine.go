package collector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// CorrelationEngine processes events from multiple collectors and generates insights
type CorrelationEngine struct {
	// Core components
	collectors       map[string]Collector
	eventChan      chan Event
	insightChan    chan Insight
	ctx            context.Context
	cancel         context.CancelFunc

	// Event processing
	eventBuffer    []Event
	bufferMutex    sync.Mutex
	batchSize      int
	batchTimeout   time.Duration

	// State tracking
	correlationMap map[string]*CorrelationState // pod -> state
	stateMutex     sync.RWMutex

	// Circuit breaker
	breaker        *LegacyCircuitBreaker

	// Metrics
	eventsProcessed uint64
	insightsCreated uint64
	correlationHits uint64
}

// CorrelationState tracks events and patterns for a specific entity
type CorrelationState struct {
	PodName          string
	Namespace        string
	Events           []Event
	LastUpdated      time.Time
	
	// Pattern detection
	MemoryTrend      []float64
	RestartCount     int
	LastRestartTime  time.Time
	NetworkErrors    int
	LastNetworkError time.Time
	
	// Predictions
	OOMRisk          float64
	TimeToOOM        time.Duration
	CrashLoopRisk    float64
}

// Insight represents a correlated finding with actionable recommendations
type Insight struct {
	ID          string
	Timestamp   time.Time
	Type        string
	Severity    Severity
	Title       string
	Description string
	
	// Related events that led to this insight
	RelatedEvents []string
	
	// Affected resources
	Resources []AffectedResource
	
	// Actionable recommendations
	Actions []ActionableItem
	
	// Prediction details
	Prediction *Prediction
}

// AffectedResource identifies resources affected by an insight
type AffectedResource struct {
	Type      string // "pod", "node", "service"
	Name      string
	Namespace string
	Labels    map[string]string
}

// Prediction contains prediction details
type Prediction struct {
	Type        string    // "oom", "crash_loop", "disk_full"
	Probability float64   // 0.0-1.0
	TimeToEvent time.Duration
	Confidence  float64   // 0.0-1.0
}

// LegacyCircuitBreaker prevents overwhelming the system (legacy implementation)
type LegacyCircuitBreaker struct {
	mu              sync.Mutex
	failureCount    int
	lastFailureTime time.Time
	state           string // "closed", "open", "half-open"
	threshold       int
	timeout         time.Duration
}

// NewCorrelationEngine creates a new correlation engine
func NewCorrelationEngine(batchSize int, batchTimeout time.Duration) *CorrelationEngine {
	return &CorrelationEngine{
		collectors:       make(map[string]Collector),
		eventChan:      make(chan Event, 10000),
		insightChan:    make(chan Insight, 1000),
		eventBuffer:    make([]Event, 0, batchSize),
		batchSize:      batchSize,
		batchTimeout:   batchTimeout,
		correlationMap: make(map[string]*CorrelationState),
		breaker:        NewLegacyCircuitBreaker(5, 30*time.Second),
	}
}

// RegisterCollector adds a sniffer to the engine
func (e *CorrelationEngine) RegisterCollector(c Collector) error {
	e.collectors[c.Name()] = c
	return nil
}

// Start begins correlation processing
func (e *CorrelationEngine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)

	// Start all collectors
	config := DefaultConfig()
	for name, c := range e.collectors {
		if err := c.Start(e.ctx, config); err != nil {
			return fmt.Errorf("failed to start collector %s: %w", name, err)
		}
		
		// Forward events to correlation engine
		go e.forwardEvents(c)
	}

	// Start processing
	go e.processEvents()
	go e.generateInsights()

	return nil
}

// forwardEvents forwards events from a sniffer to the correlation engine
func (e *CorrelationEngine) forwardEvents(c Collector) {
	events := c.Events()
	
	for {
		select {
		case <-e.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			
			// Check circuit breaker
			if !e.breaker.Allow() {
				continue
			}
			
			select {
			case e.eventChan <- event:
			default:
				// Drop event if buffer full
			}
		}
	}
}

// processEvents processes incoming events in batches
func (e *CorrelationEngine) processEvents() {
	ticker := time.NewTicker(e.batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
			
		case event := <-e.eventChan:
			e.bufferMutex.Lock()
			e.eventBuffer = append(e.eventBuffer, event)
			
			if len(e.eventBuffer) >= e.batchSize {
				batch := make([]Event, len(e.eventBuffer))
				copy(batch, e.eventBuffer)
				e.eventBuffer = e.eventBuffer[:0]
				e.bufferMutex.Unlock()
				
				e.processBatch(batch)
			} else {
				e.bufferMutex.Unlock()
			}
			
		case <-ticker.C:
			e.bufferMutex.Lock()
			if len(e.eventBuffer) > 0 {
				batch := make([]Event, len(e.eventBuffer))
				copy(batch, e.eventBuffer)
				e.eventBuffer = e.eventBuffer[:0]
				e.bufferMutex.Unlock()
				
				e.processBatch(batch)
			} else {
				e.bufferMutex.Unlock()
			}
		}
	}
}

// processBatch processes a batch of events
func (e *CorrelationEngine) processBatch(batch []Event) {
	e.stateMutex.Lock()
	defer e.stateMutex.Unlock()

	for _, event := range batch {
		atomic.AddUint64(&e.eventsProcessed, 1)
		
		// Get or create correlation state
		key := e.getCorrelationKey(&event)
		if key == "" {
			continue
		}
		
		state, exists := e.correlationMap[key]
		if !exists {
			state = &CorrelationState{
				PodName:   event.Context.Pod,
				Namespace: event.Context.Namespace,
				Events:    make([]Event, 0, 100),
			}
			e.correlationMap[key] = state
		}
		
		// Update state
		state.Events = append(state.Events, event)
		state.LastUpdated = time.Now()
		
		// Keep only recent events (last 5 minutes)
		cutoff := time.Now().Add(-5 * time.Minute)
		newEvents := make([]Event, 0, len(state.Events))
		for _, e := range state.Events {
			if e.Timestamp.After(cutoff) {
				newEvents = append(newEvents, e)
			}
		}
		state.Events = newEvents
		
		// Update specific patterns
		e.updatePatterns(state, &event)
		
		// Check for correlations
		if insight := e.checkCorrelations(state); insight != nil {
			atomic.AddUint64(&e.correlationHits, 1)
			select {
			case e.insightChan <- *insight:
				atomic.AddUint64(&e.insightsCreated, 1)
			default:
				// Drop insight if buffer full
			}
		}
	}
}

// updatePatterns updates pattern tracking based on event type
func (e *CorrelationEngine) updatePatterns(state *CorrelationState, event *Event) {
	switch event.Type {
	case "container_restart":
		state.RestartCount++
		state.LastRestartTime = event.Timestamp
		
	case "high_memory", "memory_leak":
		if usage, ok := event.Data["current_usage"].(uint64); ok {
			state.MemoryTrend = append(state.MemoryTrend, float64(usage))
			if len(state.MemoryTrend) > 20 {
				state.MemoryTrend = state.MemoryTrend[1:]
			}
		}
		
	case "network_error", "network_timeout":
		state.NetworkErrors++
		state.LastNetworkError = event.Timestamp
		
	case "oom_prediction":
		if prob, ok := event.Data["confidence"].(float64); ok {
			state.OOMRisk = prob
		}
		if timeToOOM, ok := event.Data["time_to_oom"].(float64); ok {
			state.TimeToOOM = time.Duration(timeToOOM) * time.Second
		}
	}
}

// checkCorrelations checks for patterns that warrant an insight
func (e *CorrelationEngine) checkCorrelations(state *CorrelationState) *Insight {
	// Memory pressure + restarts = likely OOM
	if state.RestartCount > 2 && len(state.MemoryTrend) > 5 {
		avgMemory := average(state.MemoryTrend)
		recentMemory := average(state.MemoryTrend[len(state.MemoryTrend)-3:])
		
		if recentMemory > avgMemory*1.5 {
			return e.createOOMInsight(state)
		}
	}
	
	// Rapid restarts = crash loop
	if state.RestartCount > 5 && time.Since(state.LastRestartTime) < 5*time.Minute {
		return e.createCrashLoopInsight(state)
	}
	
	// Network errors + restarts = connectivity issue
	if state.NetworkErrors > 10 && state.RestartCount > 1 {
		return e.createNetworkInsight(state)
	}
	
	// High OOM risk from eBPF predictions
	if state.OOMRisk > 0.8 && state.TimeToOOM < 10*time.Minute {
		return e.createOOMPredictionInsight(state)
	}
	
	return nil
}

// createOOMInsight creates an OOM-related insight
func (e *CorrelationEngine) createOOMInsight(state *CorrelationState) *Insight {
	eventIDs := make([]string, 0, len(state.Events))
	for _, event := range state.Events {
		if event.Type == "high_memory" || event.Type == "memory_leak" || event.Type == "container_restart" {
			eventIDs = append(eventIDs, event.ID)
		}
	}
	
	// Calculate memory increase rate
	memoryGrowth := calculateGrowthRate(state.MemoryTrend)
	
	return &Insight{
		ID:          uuid.New().String(),
		Timestamp:   time.Now(),
		Type:        "oom_correlation",
		Severity:    SeverityHigh,
		Title:       "Memory Pressure Leading to OOM Kills",
		Description: fmt.Sprintf("Pod %s is experiencing memory pressure with %d restarts. Memory growing at %.2f MB/min", 
			state.PodName, state.RestartCount, memoryGrowth/(1024*1024)),
		RelatedEvents: eventIDs,
		Resources: []AffectedResource{{
			Type:      "pod",
			Name:      state.PodName,
			Namespace: state.Namespace,
		}},
		Actions: []ActionableItem{{
			Title:       "Increase Memory Limit",
			Description: "The pod is being OOM killed due to insufficient memory",
			Commands: []string{
				fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"main\",\"resources\":{\"limits\":{\"memory\":\"2Gi\"}}}]}}}}'", 
					getDeploymentFromPod(state.PodName), state.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Pod will have more memory available",
		}, {
			Title:       "Analyze Memory Usage",
			Description: "Investigate why the application is using more memory",
			Commands: []string{
				fmt.Sprintf("kubectl exec -it %s -n %s -- /bin/sh -c 'ps aux | sort -k4 -nr | head -10'", state.PodName, state.Namespace),
				fmt.Sprintf("kubectl exec -it %s -n %s -- /bin/sh -c 'cat /proc/meminfo'", state.PodName, state.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic only",
		}},
		Prediction: &Prediction{
			Type:        "oom",
			Probability: 0.85,
			TimeToEvent: time.Duration(float64(1024*1024*1024)/memoryGrowth) * time.Second,
			Confidence:  0.9,
		},
	}
}

// createCrashLoopInsight creates a crash loop insight
func (e *CorrelationEngine) createCrashLoopInsight(state *CorrelationState) *Insight {
	eventIDs := make([]string, 0, len(state.Events))
	for _, event := range state.Events {
		if event.Type == "container_restart" || event.Type == "crash_loop_backoff" {
			eventIDs = append(eventIDs, event.ID)
		}
	}
	
	return &Insight{
		ID:          uuid.New().String(),
		Timestamp:   time.Now(),
		Type:        "crash_loop_correlation",
		Severity:    SeverityCritical,
		Title:       "Application Crash Loop Detected",
		Description: fmt.Sprintf("Pod %s has restarted %d times in the last 5 minutes", state.PodName, state.RestartCount),
		RelatedEvents: eventIDs,
		Resources: []AffectedResource{{
			Type:      "pod",
			Name:      state.PodName,
			Namespace: state.Namespace,
		}},
		Actions: []ActionableItem{{
			Title:       "Check Recent Logs",
			Description: "View logs from the crashed container",
			Commands: []string{
				fmt.Sprintf("kubectl logs %s -n %s --previous", state.PodName, state.Namespace),
				fmt.Sprintf("kubectl describe pod %s -n %s", state.PodName, state.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic only",
		}, {
			Title:       "Rollback Deployment",
			Description: "Revert to the previous working version",
			Commands: []string{
				fmt.Sprintf("kubectl rollout undo deployment/%s -n %s", 
					getDeploymentFromPod(state.PodName), state.Namespace),
			},
			Risk:            "medium",
			EstimatedImpact: "Will revert to previous deployment version",
		}},
		Prediction: &Prediction{
			Type:        "crash_loop",
			Probability: 0.95,
			TimeToEvent: 30 * time.Second, // Next crash
			Confidence:  0.95,
		},
	}
}

// createNetworkInsight creates a network-related insight
func (e *CorrelationEngine) createNetworkInsight(state *CorrelationState) *Insight {
	return &Insight{
		ID:          uuid.New().String(),
		Timestamp:   time.Now(),
		Type:        "network_correlation",
		Severity:    SeverityMedium,
		Title:       "Network Connectivity Issues",
		Description: fmt.Sprintf("Pod %s experiencing %d network errors with %d restarts", 
			state.PodName, state.NetworkErrors, state.RestartCount),
		Resources: []AffectedResource{{
			Type:      "pod",
			Name:      state.PodName,
			Namespace: state.Namespace,
		}},
		Actions: []ActionableItem{{
			Title:       "Check Network Policies",
			Description: "Verify network policies aren't blocking traffic",
			Commands: []string{
				fmt.Sprintf("kubectl get networkpolicies -n %s", state.Namespace),
				fmt.Sprintf("kubectl exec -it %s -n %s -- nc -zv service-name 80", state.PodName, state.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Diagnostic only",
		}},
	}
}

// createOOMPredictionInsight creates an insight based on eBPF OOM predictions
func (e *CorrelationEngine) createOOMPredictionInsight(state *CorrelationState) *Insight {
	return &Insight{
		ID:          uuid.New().String(),
		Timestamp:   time.Now(),
		Type:        "oom_prediction",
		Severity:    SeverityCritical,
		Title:       fmt.Sprintf("OOM Kill Predicted in %.0f minutes", state.TimeToOOM.Minutes()),
		Description: fmt.Sprintf("Pod %s will likely be OOM killed based on memory growth patterns", state.PodName),
		Resources: []AffectedResource{{
			Type:      "pod",
			Name:      state.PodName,
			Namespace: state.Namespace,
		}},
		Actions: []ActionableItem{{
			Title:       "Preemptive Memory Increase",
			Description: "Increase memory limit before OOM occurs",
			Commands: []string{
				fmt.Sprintf("kubectl patch deployment %s -n %s -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"main\",\"resources\":{\"limits\":{\"memory\":\"4Gi\"}}}]}}}}'", 
					getDeploymentFromPod(state.PodName), state.Namespace),
			},
			Risk:            "low",
			EstimatedImpact: "Prevents OOM kill, uses more cluster resources",
		}},
		Prediction: &Prediction{
			Type:        "oom",
			Probability: state.OOMRisk,
			TimeToEvent: state.TimeToOOM,
			Confidence:  0.85,
		},
	}
}

// generateInsights periodically generates proactive insights
func (e *CorrelationEngine) generateInsights() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.generateProactiveInsights()
		}
	}
}

// generateProactiveInsights looks for patterns across all states
func (e *CorrelationEngine) generateProactiveInsights() {
	e.stateMutex.RLock()
	defer e.stateMutex.RUnlock()

	// Look for cluster-wide patterns
	totalPods := len(e.correlationMap)
	problematicPods := 0
	
	for _, state := range e.correlationMap {
		if state.RestartCount > 0 || state.OOMRisk > 0.5 {
			problematicPods++
		}
	}
	
	// Generate cluster health insight if needed
	if float64(problematicPods)/float64(totalPods) > 0.2 && totalPods > 10 {
		insight := &Insight{
			ID:          uuid.New().String(),
			Timestamp:   time.Now(),
			Type:        "cluster_health",
			Severity:    SeverityHigh,
			Title:       "Cluster-Wide Stability Issues",
			Description: fmt.Sprintf("%d out of %d pods are experiencing issues", problematicPods, totalPods),
			Actions: []ActionableItem{{
				Title:       "Review Cluster Resources",
				Description: "Check if cluster is under-provisioned",
				Commands: []string{
					"kubectl top nodes",
					"kubectl describe nodes | grep -A 5 'Allocated resources'",
				},
				Risk:            "low",
				EstimatedImpact: "Diagnostic only",
			}},
		}
		
		select {
		case e.insightChan <- *insight:
			atomic.AddUint64(&e.insightsCreated, 1)
		default:
		}
	}
}

// Insights returns the insights channel
func (e *CorrelationEngine) Insights() <-chan Insight {
	return e.insightChan
}

// GetStats returns engine statistics
func (e *CorrelationEngine) GetStats() map[string]interface{} {
	e.stateMutex.RLock()
	defer e.stateMutex.RUnlock()

	return map[string]interface{}{
		"events_processed":  atomic.LoadUint64(&e.eventsProcessed),
		"insights_created":  atomic.LoadUint64(&e.insightsCreated),
		"correlation_hits":  atomic.LoadUint64(&e.correlationHits),
		"tracked_pods":      len(e.correlationMap),
		"breaker_state":     e.breaker.State(),
	}
}

// Stop stops the correlation engine
func (e *CorrelationEngine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// Helper functions

// getCorrelationKey generates a correlation key from an event
func (e *CorrelationEngine) getCorrelationKey(event *Event) string {
	if event.Context == nil {
		return ""
	}
	
	if event.Context.Pod != "" {
		return fmt.Sprintf("%s/%s", event.Context.Namespace, event.Context.Pod)
	}
	
	return ""
}

// average calculates the average of a slice
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// calculateGrowthRate calculates the growth rate from a trend
func calculateGrowthRate(trend []float64) float64 {
	if len(trend) < 2 {
		return 0
	}
	
	// Simple linear regression
	n := float64(len(trend))
	sumX := n * (n - 1) / 2
	sumY := 0.0
	sumXY := 0.0
	sumX2 := n * (n - 1) * (2*n - 1) / 6
	
	for i, y := range trend {
		x := float64(i)
		sumY += y
		sumXY += x * y
	}
	
	// Calculate slope
	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	
	// Return growth per minute (assuming 1 sample per second)
	return slope * 60
}

// getDeploymentFromPod extracts deployment name from pod name
func getDeploymentFromPod(podName string) string {
	// Simple heuristic - remove last two segments
	parts := strings.Split(podName, "-")
	if len(parts) > 2 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return podName
}

// Circuit breaker implementation

// NewLegacyCircuitBreaker creates a new legacy circuit breaker
func NewLegacyCircuitBreaker(threshold int, timeout time.Duration) *LegacyCircuitBreaker {
	return &LegacyCircuitBreaker{
		state:     "closed",
		threshold: threshold,
		timeout:   timeout,
	}
}

// Allow checks if a request should be allowed
func (cb *LegacyCircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "open":
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = "half-open"
			cb.failureCount = 0
			return true
		}
		return false
		
	case "half-open":
		return true
		
	default: // closed
		return true
	}
}

// RecordSuccess records a successful operation
func (cb *LegacyCircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == "half-open" {
		cb.state = "closed"
		cb.failureCount = 0
	}
}

// RecordFailure records a failed operation
func (cb *LegacyCircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.failureCount >= cb.threshold {
		cb.state = "open"
	}
}

// State returns the current state
func (cb *LegacyCircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}