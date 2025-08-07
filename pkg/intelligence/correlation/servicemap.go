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

// ServiceMapCorrelator analyzes service-to-service communication patterns
type ServiceMapCorrelator struct {
	logger *zap.Logger

	// Service topology tracking
	serviceGraph *ServiceGraph
	graphMu      sync.RWMutex

	// Connection health tracking
	healthTracker *ConnectionHealthTracker

	// Recent events cache
	recentEvents *ServiceEventCache
}

// ServiceGraph represents the service dependency graph
type ServiceGraph struct {
	nodes map[string]*ServiceNode
	edges map[string]*ServiceEdge
}

type ServiceNode struct {
	Name      string
	Namespace string
	Type      string // deployment, statefulset, daemonset
	LastSeen  time.Time
	Endpoints []string
	Version   string
}

type ServiceEdge struct {
	Source       string
	Destination  string
	Protocol     string
	Port         int
	LastSeen     time.Time
	RequestCount int64
	ErrorCount   int64
	AvgLatency   time.Duration
}

// ConnectionHealthTracker monitors connection health over time
type ConnectionHealthTracker struct {
	mu          sync.RWMutex
	connections map[string]*ConnectionHealth
}

type ConnectionHealth struct {
	Key              string // source->dest
	SuccessCount     int64
	FailureCount     int64
	LastSuccess      time.Time
	LastFailure      time.Time
	ConsecutiveFails int
	State            string // healthy, degraded, broken
}

// ServiceEventCache caches recent service events
type ServiceEventCache struct {
	mu     sync.RWMutex
	events map[string][]*domain.UnifiedEvent // key: service name
	ttl    time.Duration
}

func NewServiceMapCorrelator(logger *zap.Logger) *ServiceMapCorrelator {
	return &ServiceMapCorrelator{
		logger: logger,
		serviceGraph: &ServiceGraph{
			nodes: make(map[string]*ServiceNode),
			edges: make(map[string]*ServiceEdge),
		},
		healthTracker: &ConnectionHealthTracker{
			connections: make(map[string]*ConnectionHealth),
		},
		recentEvents: &ServiceEventCache{
			events: make(map[string][]*domain.UnifiedEvent),
			ttl:    10 * time.Minute,
		},
	}
}

func (s *ServiceMapCorrelator) Name() string {
	return "servicemap"
}

func (s *ServiceMapCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Cache event
	s.cacheEvent(event)

	// Get event type from metadata
	eventType := s.getMetadata(event, "event_type")

	// Update service graph based on event
	s.updateServiceGraph(event)

	// Route to specific handlers
	switch eventType {
	case "service_map":
		return s.handleServiceMapUpdate(ctx, event)
	case "network_conn":
		return s.handleNetworkConnection(ctx, event)
	case "connection_refused", "connection_timeout":
		return s.handleConnectionFailure(ctx, event)
	case "http_request":
		return s.handleHTTPRequest(ctx, event)
	}

	// Check for service-level patterns
	if patterns := s.detectServicePatterns(event); len(patterns) > 0 {
		return patterns, nil
	}

	return nil, nil
}

// handleServiceMapUpdate processes service topology updates
func (s *ServiceMapCorrelator) handleServiceMapUpdate(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// Extract service information
	serviceName := s.getMetadata(event, "service_name")
	namespace := s.getMetadata(event, "k8s_namespace")

	// Check for service isolation
	activeConns, _ := strconv.Atoi(s.getMetadata(event, "active_connections"))
	incomingConns, _ := strconv.Atoi(s.getMetadata(event, "incoming_connections"))
	outgoingConns, _ := strconv.Atoi(s.getMetadata(event, "outgoing_connections"))

	if activeConns == 0 && (incomingConns > 0 || outgoingConns > 0) {
		// Service should have connections but doesn't
		return []*CorrelationResult{{
			ID:         fmt.Sprintf("svcmap-isolation-%s", event.ID),
			Type:       "service_isolation",
			Confidence: 0.9,
			Events:     []string{event.ID},
			Summary:    fmt.Sprintf("Service %s/%s is isolated from network", namespace, serviceName),
			Details:    "Service has endpoints but no active connections",
			RootCause: &RootCause{
				EventID:     event.ID,
				Confidence:  0.85,
				Description: "Network policy or connectivity issue",
				Evidence: []string{
					fmt.Sprintf("Expected connections: in=%d, out=%d", incomingConns, outgoingConns),
					"Active connections: 0",
					"Check NetworkPolicies",
					"Verify service endpoints",
				},
			},
			Impact: &Impact{
				Severity:  domain.EventSeverityCritical,
				Services:  []string{serviceName},
				Resources: []string{fmt.Sprintf("Service/%s/%s", namespace, serviceName)},
			},
		}}, nil
	}

	return nil, nil
}

// handleNetworkConnection tracks service connections
func (s *ServiceMapCorrelator) handleNetworkConnection(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	srcService := s.getMetadata(event, "src_service")
	dstService := s.getMetadata(event, "dst_service")

	if srcService == "" || dstService == "" {
		return nil, nil
	}

	// Update connection health
	connKey := fmt.Sprintf("%s->%s", srcService, dstService)
	s.updateConnectionHealth(connKey, true)

	// Check for connection patterns
	return s.checkConnectionPatterns(srcService, dstService, event)
}

// handleConnectionFailure detects service communication breakdowns
func (s *ServiceMapCorrelator) handleConnectionFailure(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	srcService := s.getMetadata(event, "src_service")
	dstService := s.getMetadata(event, "dst_service")
	errorType := s.getMetadata(event, "error_type")

	if srcService == "" || dstService == "" {
		return nil, nil
	}

	connKey := fmt.Sprintf("%s->%s", srcService, dstService)
	s.updateConnectionHealth(connKey, false)

	// Get connection health
	health := s.getConnectionHealth(connKey)
	if health == nil {
		return nil, nil
	}

	// Check if this is a persistent failure
	if health.ConsecutiveFails >= 5 {
		// Find dependent services
		dependents := s.findDependentServices(srcService)

		result := &CorrelationResult{
			ID:         fmt.Sprintf("svcmap-breakdown-%s", event.ID),
			Type:       "service_communication_failure",
			Confidence: 0.95,
			Events:     []string{event.ID},
			Summary:    fmt.Sprintf("Service communication breakdown: %s → %s", srcService, dstService),
			Details:    fmt.Sprintf("Persistent connection failures (%d consecutive) with error: %s", health.ConsecutiveFails, errorType),
			RootCause: &RootCause{
				EventID:     event.ID,
				Confidence:  0.9,
				Description: s.determineFailureRootCause(errorType, health),
				Evidence: []string{
					fmt.Sprintf("Failure rate: %d/%d", health.FailureCount, health.SuccessCount+health.FailureCount),
					fmt.Sprintf("Last success: %v ago", time.Since(health.LastSuccess)),
					fmt.Sprintf("Error type: %s", errorType),
				},
			},
			Impact: &Impact{
				Severity:  domain.EventSeverityCritical,
				Services:  append([]string{srcService}, dependents...),
				Resources: s.getAffectedResources(srcService, dstService),
			},
		}

		return []*CorrelationResult{result}, nil
	}

	return nil, nil
}

// handleHTTPRequest analyzes HTTP request patterns
func (s *ServiceMapCorrelator) handleHTTPRequest(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	statusCode, _ := strconv.Atoi(s.getMetadata(event, "status_code"))
	latencyMs, _ := strconv.ParseFloat(s.getMetadata(event, "latency_ms"), 64)
	endpoint := s.getMetadata(event, "endpoint")

	// Check for error patterns
	if statusCode >= 500 {
		return s.handle5xxErrors(event, statusCode, endpoint)
	}

	// Check for latency issues
	if latencyMs > 1000 { // 1 second
		return s.handleHighLatency(event, latencyMs, endpoint)
	}

	return nil, nil
}

// detectServicePatterns looks for broader service patterns
func (s *ServiceMapCorrelator) detectServicePatterns(event *domain.UnifiedEvent) []*CorrelationResult {
	var results []*CorrelationResult

	// Pattern 1: Cascading service failures
	if cascade := s.detectCascadeFailure(event); cascade != nil {
		results = append(results, cascade)
	}

	// Pattern 2: Service version incompatibility
	if incompatibility := s.detectVersionIncompatibility(event); incompatibility != nil {
		results = append(results, incompatibility)
	}

	// Pattern 3: Circuit breaker patterns
	if circuitBreaker := s.detectCircuitBreakerPattern(event); circuitBreaker != nil {
		results = append(results, circuitBreaker)
	}

	return results
}

// detectCascadeFailure identifies cascading service failures
func (s *ServiceMapCorrelator) detectCascadeFailure(event *domain.UnifiedEvent) *CorrelationResult {
	serviceName := s.getServiceName(event)
	if serviceName == "" {
		return nil
	}

	// Look for failures in dependent services
	dependents := s.findDependentServices(serviceName)
	if len(dependents) == 0 {
		return nil
	}

	// Check if dependents are also failing
	failingDependents := []string{}
	for _, dep := range dependents {
		if s.isServiceFailing(dep) {
			failingDependents = append(failingDependents, dep)
		}
	}

	if len(failingDependents) >= 2 {
		return &CorrelationResult{
			ID:         fmt.Sprintf("svcmap-cascade-%s-%d", serviceName, time.Now().Unix()),
			Type:       "cascade_failure",
			Confidence: 0.85,
			Summary:    fmt.Sprintf("Cascading failure starting from %s", serviceName),
			Details:    fmt.Sprintf("Service %s failure is affecting %d dependent services", serviceName, len(failingDependents)),
			RootCause: &RootCause{
				EventID:     event.ID,
				Confidence:  0.8,
				Description: fmt.Sprintf("Root service %s failure", serviceName),
				Evidence: []string{
					fmt.Sprintf("Affected services: %s", strings.Join(failingDependents, ", ")),
					"Failure propagation through service dependencies",
				},
			},
			Impact: &Impact{
				Severity: domain.EventSeverityCritical,
				Services: append([]string{serviceName}, failingDependents...),
			},
		}
	}

	return nil
}

// Helper methods

func (s *ServiceMapCorrelator) updateServiceGraph(event *domain.UnifiedEvent) {
	s.graphMu.Lock()
	defer s.graphMu.Unlock()

	// Update based on event type
	if svcName := s.getMetadata(event, "service_name"); svcName != "" {
		node := s.serviceGraph.nodes[svcName]
		if node == nil {
			node = &ServiceNode{
				Name:      svcName,
				Namespace: s.getMetadata(event, "k8s_namespace"),
				Type:      s.getMetadata(event, "k8s_kind"),
			}
			s.serviceGraph.nodes[svcName] = node
		}
		node.LastSeen = time.Now()

		// Update version if available
		if version := s.getMetadata(event, "version"); version != "" {
			node.Version = version
		}
	}

	// Update edges for connections
	if src := s.getMetadata(event, "src_service"); src != "" {
		if dst := s.getMetadata(event, "dst_service"); dst != "" {
			edgeKey := fmt.Sprintf("%s->%s", src, dst)
			edge := s.serviceGraph.edges[edgeKey]
			if edge == nil {
				edge = &ServiceEdge{
					Source:      src,
					Destination: dst,
				}
				s.serviceGraph.edges[edgeKey] = edge
			}
			edge.LastSeen = time.Now()
			edge.RequestCount++
		}
	}
}

func (s *ServiceMapCorrelator) updateConnectionHealth(connKey string, success bool) {
	s.healthTracker.mu.Lock()
	defer s.healthTracker.mu.Unlock()

	health := s.healthTracker.connections[connKey]
	if health == nil {
		health = &ConnectionHealth{
			Key: connKey,
		}
		s.healthTracker.connections[connKey] = health
	}

	if success {
		health.SuccessCount++
		health.LastSuccess = time.Now()
		health.ConsecutiveFails = 0
		health.State = "healthy"
	} else {
		health.FailureCount++
		health.LastFailure = time.Now()
		health.ConsecutiveFails++

		if health.ConsecutiveFails >= 5 {
			health.State = "broken"
		} else if health.ConsecutiveFails >= 3 {
			health.State = "degraded"
		}
	}
}

func (s *ServiceMapCorrelator) getConnectionHealth(connKey string) *ConnectionHealth {
	s.healthTracker.mu.RLock()
	defer s.healthTracker.mu.RUnlock()
	return s.healthTracker.connections[connKey]
}

func (s *ServiceMapCorrelator) findDependentServices(serviceName string) []string {
	s.graphMu.RLock()
	defer s.graphMu.RUnlock()

	dependents := []string{}
	for _, edge := range s.serviceGraph.edges {
		if edge.Destination == serviceName && edge.LastSeen.After(time.Now().Add(-5*time.Minute)) {
			dependents = append(dependents, edge.Source)
		}
	}
	return dependents
}

func (s *ServiceMapCorrelator) determineFailureRootCause(errorType string, health *ConnectionHealth) string {
	switch errorType {
	case "connection_refused":
		return "Target service is not listening on the expected port or is down"
	case "connection_timeout":
		return "Network connectivity issue or target service is overloaded"
	case "dns_failure":
		return "Service discovery failure - DNS cannot resolve service name"
	default:
		if health.State == "broken" {
			return "Persistent service unavailability"
		}
		return "Intermittent service communication issue"
	}
}

func (s *ServiceMapCorrelator) handle5xxErrors(event *domain.UnifiedEvent, statusCode int, endpoint string) ([]*CorrelationResult, error) {
	service := s.getServiceName(event)

	// Look for pattern of 5xx errors
	recentErrors := s.count5xxErrors(service, 5*time.Minute)

	if recentErrors >= 10 {
		return []*CorrelationResult{{
			ID:         fmt.Sprintf("svcmap-5xx-%s", event.ID),
			Type:       "service_error_spike",
			Confidence: 0.9,
			Events:     []string{event.ID},
			Summary:    fmt.Sprintf("Service %s experiencing high error rate", service),
			Details:    fmt.Sprintf("%d errors in last 5 minutes on endpoint %s", recentErrors, endpoint),
			RootCause: &RootCause{
				EventID:     event.ID,
				Confidence:  0.8,
				Description: "Service internal error or resource exhaustion",
				Evidence: []string{
					fmt.Sprintf("Status code: %d", statusCode),
					fmt.Sprintf("Error count: %d", recentErrors),
					"Check service logs for stack traces",
					"Verify database connections",
				},
			},
			Impact: &Impact{
				Severity: domain.EventSeverityCritical,
				Services: []string{service},
			},
		}}, nil
	}

	return nil, nil
}

func (s *ServiceMapCorrelator) handleHighLatency(event *domain.UnifiedEvent, latencyMs float64, endpoint string) ([]*CorrelationResult, error) {
	service := s.getServiceName(event)

	// Check if this is a pattern
	avgLatency := s.getAverageLatency(service, endpoint, 5*time.Minute)

	if avgLatency > 500 && latencyMs > avgLatency*2 {
		return []*CorrelationResult{{
			ID:         fmt.Sprintf("svcmap-latency-%s", event.ID),
			Type:       "service_performance_degradation",
			Confidence: 0.8,
			Events:     []string{event.ID},
			Summary:    fmt.Sprintf("Service %s experiencing high latency", service),
			Details:    fmt.Sprintf("Latency %.0fms (avg: %.0fms) on endpoint %s", latencyMs, avgLatency, endpoint),
			RootCause: &RootCause{
				EventID:     event.ID,
				Confidence:  0.7,
				Description: "Service performance bottleneck",
				Evidence: []string{
					fmt.Sprintf("Current latency: %.0fms", latencyMs),
					fmt.Sprintf("Average latency: %.0fms", avgLatency),
					"Possible causes: CPU throttling, database slowdown, network congestion",
				},
			},
			Impact: &Impact{
				Severity: domain.EventSeverityWarning,
				Services: []string{service},
			},
		}}, nil
	}

	return nil, nil
}

func (s *ServiceMapCorrelator) detectVersionIncompatibility(event *domain.UnifiedEvent) *CorrelationResult {
	// This would check for version mismatches between services
	// Simplified for now
	return nil
}

func (s *ServiceMapCorrelator) detectCircuitBreakerPattern(event *domain.UnifiedEvent) *CorrelationResult {
	// This would detect circuit breaker activations
	// Simplified for now
	return nil
}

func (s *ServiceMapCorrelator) cacheEvent(event *domain.UnifiedEvent) {
	service := s.getServiceName(event)
	if service == "" {
		return
	}

	s.recentEvents.mu.Lock()
	defer s.recentEvents.mu.Unlock()

	s.recentEvents.events[service] = append(s.recentEvents.events[service], event)

	// Cleanup old events
	cutoff := time.Now().Add(-s.recentEvents.ttl)
	filtered := make([]*domain.UnifiedEvent, 0)
	for _, e := range s.recentEvents.events[service] {
		if e.Timestamp.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	s.recentEvents.events[service] = filtered
}

func (s *ServiceMapCorrelator) getServiceName(event *domain.UnifiedEvent) string {
	if name := s.getMetadata(event, "service_name"); name != "" {
		return name
	}
	if event.K8sContext != nil && event.K8sContext.Kind == "Service" {
		return event.K8sContext.Name
	}
	return ""
}

func (s *ServiceMapCorrelator) isServiceFailing(serviceName string) bool {
	s.recentEvents.mu.RLock()
	defer s.recentEvents.mu.RUnlock()

	errorCount := 0
	for _, event := range s.recentEvents.events[serviceName] {
		if event.Timestamp.After(time.Now().Add(-5 * time.Minute)) {
			if s.getMetadata(event, "error") != "" || s.getMetadata(event, "status_code") >= "500" {
				errorCount++
			}
		}
	}

	return errorCount >= 5
}

func (s *ServiceMapCorrelator) count5xxErrors(service string, window time.Duration) int {
	s.recentEvents.mu.RLock()
	defer s.recentEvents.mu.RUnlock()

	count := 0
	cutoff := time.Now().Add(-window)

	for _, event := range s.recentEvents.events[service] {
		if event.Timestamp.After(cutoff) {
			if code, _ := strconv.Atoi(s.getMetadata(event, "status_code")); code >= 500 {
				count++
			}
		}
	}

	return count
}

func (s *ServiceMapCorrelator) getAverageLatency(service, endpoint string, window time.Duration) float64 {
	s.recentEvents.mu.RLock()
	defer s.recentEvents.mu.RUnlock()

	var total float64
	count := 0
	cutoff := time.Now().Add(-window)

	for _, event := range s.recentEvents.events[service] {
		if event.Timestamp.After(cutoff) && s.getMetadata(event, "endpoint") == endpoint {
			if latency, err := strconv.ParseFloat(s.getMetadata(event, "latency_ms"), 64); err == nil {
				total += latency
				count++
			}
		}
	}

	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func (s *ServiceMapCorrelator) getAffectedResources(srcService, dstService string) []string {
	resources := []string{}

	// Add service resources
	if src := s.serviceGraph.nodes[srcService]; src != nil {
		resources = append(resources, fmt.Sprintf("Service/%s/%s", src.Namespace, src.Name))
	}
	if dst := s.serviceGraph.nodes[dstService]; dst != nil {
		resources = append(resources, fmt.Sprintf("Service/%s/%s", dst.Namespace, dst.Name))
	}

	return resources
}

func (s *ServiceMapCorrelator) checkConnectionPatterns(srcService, dstService string, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	// This would check for patterns like retry storms, connection pooling issues, etc.
	// Simplified for now
	return nil, nil
}

// getMetadata retrieves metadata from event attributes
func (s *ServiceMapCorrelator) getMetadata(event *domain.UnifiedEvent, key string) string {
	if event.Attributes != nil {
		if val, ok := event.Attributes[key].(string); ok {
			return val
		}
	}
	return ""
}
