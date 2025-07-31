package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// MockCollector generates synthetic events for testing the pipeline
type MockCollector struct {
	name       string
	eventChan  chan domain.UnifiedEvent
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	isRunning  bool
	startTime  time.Time
	eventCount uint64
}

// NewMockCollector creates a new mock collector
func NewMockCollector(name string) *MockCollector {
	return &MockCollector{
		name:      name,
		eventChan: make(chan domain.UnifiedEvent, 1000),
	}
}

func (m *MockCollector) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("collector already running")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.startTime = time.Now()
	m.isRunning = true

	// Start event generation
	m.wg.Add(1)
	go m.generateEvents()

	return nil
}

func (m *MockCollector) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	m.cancel()
	m.wg.Wait()
	close(m.eventChan)
	m.isRunning = false

	return nil
}

func (m *MockCollector) Events() <-chan domain.UnifiedEvent {
	return m.eventChan
}

func (m *MockCollector) Health() collectors.CollectorHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := collectors.HealthStatusHealthy
	message := "Mock collector is running"

	if !m.isRunning {
		status = collectors.HealthStatusUnhealthy
		message = "Mock collector is stopped"
	}

	return collectors.CollectorHealth{
		Status:          status,
		Message:         message,
		LastEventTime:   time.Now(),
		EventsProcessed: m.eventCount,
		EventsDropped:   0,
		ErrorCount:      0,
		Metrics: map[string]float64{
			"generation_rate": float64(m.eventCount) / time.Since(m.startTime).Seconds(),
		},
	}
}

func (m *MockCollector) Statistics() collectors.CollectorStatistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return collectors.CollectorStatistics{
		StartTime:       m.startTime,
		EventsCollected: m.eventCount,
		EventsDropped:   0,
		Custom: map[string]interface{}{
			"uptime_seconds": time.Since(m.startTime).Seconds(),
		},
	}
}

func (m *MockCollector) Name() string {
	return m.name
}

func (m *MockCollector) Type() string {
	return "mock"
}

// generateEvents generates synthetic events with correlatable patterns
func (m *MockCollector) generateEvents() {
	defer m.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	scenarios := []func(){
		m.generateDeploymentScenario,
		m.generateOOMScenario,
		m.generateNetworkIssueScenario,
		m.generateApplicationErrorScenario,
	}

	for {
		select {
		case <-ticker.C:
			// Randomly pick a scenario
			scenario := scenarios[rand.Intn(len(scenarios))]
			scenario()

		case <-m.ctx.Done():
			return
		}
	}
}

// generateDeploymentScenario generates a deployment-related event sequence
func (m *MockCollector) generateDeploymentScenario() {
	traceID := fmt.Sprintf("deploy-%d", time.Now().Unix())
	namespace := "production"
	service := fmt.Sprintf("service-%d", rand.Intn(5))

	// Deployment started event
	m.sendEvent(domain.UnifiedEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "k8s-api",
		Message:   fmt.Sprintf("Deployment %s started", service),
		Category:  "deployment",
		Severity:  "info",
		Kubernetes: &domain.KubernetesData{
			EventType:  "deployment_started",
			ObjectKind: "Deployment",
			Object:     service,
			Reason:     "DeploymentStarted",
		},
		TraceContext: &domain.TraceContext{
			TraceID: traceID,
			SpanID:  fmt.Sprintf("span-%d", rand.Int63()),
		},
		Entity: &domain.EntityContext{
			Type:      "deployment",
			Name:      service,
			Namespace: namespace,
		},
		Semantic: &domain.SemanticContext{
			Intent:     "deployment",
			Category:   "infrastructure",
			Tags:       []string{"deployment", "rollout"},
			Confidence: 0.9,
		},
	})

	// Pods created events
	for i := 0; i < 3; i++ {
		time.Sleep(50 * time.Millisecond)
		podName := fmt.Sprintf("%s-pod-%d", service, i)
		m.sendEvent(domain.UnifiedEvent{
			ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
			Type:      domain.EventTypeKubernetes,
			Timestamp: time.Now(),
			Source:    "k8s-api",
			Message:   fmt.Sprintf("Pod %s created", podName),
			Category:  "deployment",
			Severity:  "info",
			Kubernetes: &domain.KubernetesData{
				EventType:  "pod_created",
				ObjectKind: "Pod",
				Object:     podName,
				Labels: map[string]string{
					"app": service,
				},
			},
			TraceContext: &domain.TraceContext{
				TraceID:      traceID,
				SpanID:       fmt.Sprintf("span-%d", rand.Int63()),
				ParentSpanID: traceID,
			},
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      podName,
				Namespace: namespace,
			},
		})
	}
}

// generateOOMScenario generates an OOM kill event sequence
func (m *MockCollector) generateOOMScenario() {
	traceID := fmt.Sprintf("oom-%d", time.Now().Unix())
	namespace := "production"
	service := fmt.Sprintf("service-%d", rand.Intn(5))
	podName := fmt.Sprintf("%s-pod-%d", service, rand.Intn(10))

	// Memory pressure warning
	m.sendEvent(domain.UnifiedEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "node-exporter",
		Message:   "High memory usage detected",
		Category:  "resource",
		Severity:  "warning",
		Kernel: &domain.KernelData{
			Comm: "memory-monitor",
			PID:  uint32(rand.Intn(10000)),
		},
		TraceContext: &domain.TraceContext{
			TraceID: traceID,
			SpanID:  fmt.Sprintf("span-%d", rand.Int63()),
		},
		Impact: &domain.ImpactContext{
			Severity:         "warning",
			BusinessImpact:   0.3,
			CustomerFacing:   false,
			AffectedServices: []string{service},
		},
	})

	time.Sleep(100 * time.Millisecond)

	// OOM Kill event
	m.sendEvent(domain.UnifiedEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeKubernetes,
		Timestamp: time.Now(),
		Source:    "kubelet",
		Message:   fmt.Sprintf("Pod %s OOM killed", podName),
		Category:  "resource",
		Severity:  "error",
		Kubernetes: &domain.KubernetesData{
			EventType:  "pod_oom_killed",
			ObjectKind: "Pod",
			Object:     podName,
			Reason:     "OOMKilled",
			Labels: map[string]string{
				"app": service,
			},
		},
		TraceContext: &domain.TraceContext{
			TraceID:      traceID,
			SpanID:       fmt.Sprintf("span-%d", rand.Int63()),
			ParentSpanID: traceID,
		},
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      podName,
			Namespace: namespace,
		},
		Impact: &domain.ImpactContext{
			Severity:         "high",
			BusinessImpact:   0.8,
			CustomerFacing:   true,
			AffectedServices: []string{service},
		},
		Semantic: &domain.SemanticContext{
			Intent:     "resource_exhaustion",
			Category:   "reliability",
			Tags:       []string{"oom", "memory", "failure"},
			Confidence: 0.95,
			Narrative:  "Service exceeded memory limits and was terminated by the system",
		},
	})
}

// generateNetworkIssueScenario generates network-related events
func (m *MockCollector) generateNetworkIssueScenario() {
	traceID := fmt.Sprintf("net-%d", time.Now().Unix())
	sourceIP := fmt.Sprintf("10.0.%d.%d", rand.Intn(255), rand.Intn(255))
	destIP := fmt.Sprintf("10.0.%d.%d", rand.Intn(255), rand.Intn(255))

	// Connection timeout event
	m.sendEvent(domain.UnifiedEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeNetwork,
		Timestamp: time.Now(),
		Source:    "ebpf",
		Message:   "Connection timeout detected",
		Category:  "network",
		Severity:  "warning",
		Network: &domain.NetworkData{
			SourceIP:   sourceIP,
			SourcePort: 45678,
			DestIP:     destIP,
			DestPort:   443,
			Protocol:   "tcp",
			Direction:  "outbound",
		},
		TraceContext: &domain.TraceContext{
			TraceID: traceID,
			SpanID:  fmt.Sprintf("span-%d", rand.Int63()),
		},
		Entity: &domain.EntityContext{
			Type: "connection",
			Name: fmt.Sprintf("%s->%s", sourceIP, destIP),
		},
	})

	// Retransmission events
	for i := 0; i < 3; i++ {
		time.Sleep(50 * time.Millisecond)
		m.sendEvent(domain.UnifiedEvent{
			ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
			Type:      domain.EventTypeNetwork,
			Timestamp: time.Now(),
			Source:    "ebpf",
			Message:   "TCP retransmission detected",
			Category:  "network",
			Severity:  "warning",
			Network: &domain.NetworkData{
				SourceIP:   sourceIP,
				SourcePort: 45678,
				DestIP:     destIP,
				DestPort:   443,
				Protocol:   "tcp",
				Direction:  "outbound",
			},
			TraceContext: &domain.TraceContext{
				TraceID:      traceID,
				SpanID:       fmt.Sprintf("span-%d", rand.Int63()),
				ParentSpanID: traceID,
			},
		})
	}
}

// generateApplicationErrorScenario generates application error events
func (m *MockCollector) generateApplicationErrorScenario() {
	traceID := fmt.Sprintf("app-%d", time.Now().Unix())
	service := fmt.Sprintf("service-%d", rand.Intn(5))

	// Application error log
	m.sendEvent(domain.UnifiedEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeLog,
		Timestamp: time.Now(),
		Source:    "fluentd",
		Message:   "Database connection failed: timeout after 30s",
		Category:  "application",
		Severity:  "error",
		Application: &domain.ApplicationData{
			Level:      "error",
			Logger:     "db.connection",
			ErrorType:  "DatabaseConnectionTimeout",
			StackTrace: "at db.connect()\nat service.init()\nat main()",
		},
		TraceContext: &domain.TraceContext{
			TraceID: traceID,
			SpanID:  fmt.Sprintf("span-%d", rand.Int63()),
		},
		Entity: &domain.EntityContext{
			Type: "service",
			Name: service,
		},
		Semantic: &domain.SemanticContext{
			Intent:     "error_condition",
			Category:   "application",
			Tags:       []string{"database", "timeout", "connection"},
			Confidence: 0.85,
		},
	})

	// Follow-up retry attempts
	for i := 0; i < 3; i++ {
		time.Sleep(100 * time.Millisecond)
		m.sendEvent(domain.UnifiedEvent{
			ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
			Type:      domain.EventTypeLog,
			Timestamp: time.Now(),
			Source:    "fluentd",
			Message:   fmt.Sprintf("Database connection retry %d/3 failed", i+1),
			Category:  "application",
			Severity:  "warning",
			Application: &domain.ApplicationData{
				Level:   "warn",
				Logger:  "db.connection",
				Message: fmt.Sprintf("Database connection retry %d/3 failed", i+1),
			},
			TraceContext: &domain.TraceContext{
				TraceID:      traceID,
				SpanID:       fmt.Sprintf("span-%d", rand.Int63()),
				ParentSpanID: traceID,
			},
		})
	}
}

// sendEvent sends an event to the channel
func (m *MockCollector) sendEvent(event domain.UnifiedEvent) {
	select {
	case m.eventChan <- event:
		m.mu.Lock()
		m.eventCount++
		m.mu.Unlock()
	default:
		// Channel full, drop event
	}
}
