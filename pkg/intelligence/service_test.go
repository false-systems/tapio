package intelligence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestService_ProcessEvent(t *testing.T) {
	// This would use testcontainers or embedded Neo4j in real implementation
	logger := zap.NewNop()

	// Mock service for unit testing
	service := &Service{
		logger: logger,
	}

	tests := []struct {
		name    string
		event   *domain.UnifiedEvent
		wantErr bool
	}{
		{
			name: "process pod event",
			event: &domain.UnifiedEvent{
				ID:        "event-1",
				Type:      "pod_created",
				Source:    "kubeapi",
				Timestamp: time.Now(),
				Entity: &domain.EntityContext{
					Type:      "pod",
					Name:      "nginx-123",
					Namespace: "default",
					UID:       "pod-uid-123",
					Labels: map[string]string{
						"app": "nginx",
					},
				},
				K8sContext: &domain.K8sContext{
					Name:      "nginx-123",
					Namespace: "default",
					Kind:      "Pod",
					OwnerReferences: []domain.OwnerReference{
						{
							Kind: "ReplicaSet",
							Name: "nginx-rs",
							UID:  "rs-uid-123",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "process service event",
			event: &domain.UnifiedEvent{
				ID:        "event-2",
				Type:      "service_created",
				Source:    "kubeapi",
				Timestamp: time.Now(),
				Entity: &domain.EntityContext{
					Type:      "service",
					Name:      "web",
					Namespace: "default",
					UID:       "svc-uid-123",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// In real test, would process event and verify graph state
			_ = tt
			_ = service
		})
	}
}

func TestService_WhyDidThisFail(t *testing.T) {
	logger := zap.NewNop()
	service := &Service{
		logger: logger,
	}

	tests := []struct {
		name         string
		resourceType string
		namespace    string
		resourceName string
		wantErr      bool
	}{
		{
			name:         "pod failure analysis",
			resourceType: "pod",
			namespace:    "default",
			resourceName: "nginx-123",
			wantErr:      false,
		},
		{
			name:         "unsupported resource type",
			resourceType: "configmap",
			namespace:    "default",
			resourceName: "config",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// In real test, would query actual Neo4j
			_ = tt
			_ = service
		})
	}
}

// Integration test example (requires Neo4j)
func TestService_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would set up test Neo4j instance
	config := Config{
		Neo4jURI:      "bolt://localhost:7687",
		Neo4jUsername: "neo4j",
		Neo4jPassword: "password",
		Neo4jDatabase: "test",
	}

	logger := zap.NewNop()
	service, err := NewService(config, logger)
	if err != nil {
		t.Skip("Neo4j not available:", err)
	}
	defer service.Close(context.Background())

	ctx := context.Background()

	// Create test scenario
	// 1. ConfigMap change
	configMapEvent := &domain.UnifiedEvent{
		ID:        "cm-change-1",
		Type:      "modified",
		Source:    "kubeapi",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "configmap",
			Name:      "app-config",
			Namespace: "default",
			UID:       "cm-123",
		},
	}

	err = service.ProcessEvent(ctx, configMapEvent)
	assert.NoError(t, err)

	// 2. Pod restart due to ConfigMap
	podRestartEvent := &domain.UnifiedEvent{
		ID:        "pod-restart-1",
		Type:      "pod_restarted",
		Source:    "kubeapi",
		Timestamp: time.Now().Add(30 * time.Second),
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "app-pod-1",
			Namespace: "default",
			UID:       "pod-456",
		},
		K8sContext: &domain.K8sContext{
			Name:      "app-pod-1",
			Namespace: "default",
			Kind:      "Pod",
		},
		CorrelationHints: []string{"cm-123"}, // Hint about ConfigMap
	}

	err = service.ProcessEvent(ctx, podRestartEvent)
	assert.NoError(t, err)

	// 3. Query for root cause
	analysis, err := service.WhyDidThisFail(ctx, "pod", "default", "app-pod-1")
	assert.NoError(t, err)
	assert.NotNil(t, analysis)

	// Should identify ConfigMap change as root cause
	assert.NotEmpty(t, analysis.RootCauses)

	// 4. Check cascade detection
	cascades, err := service.GetCascadingFailures(ctx, 5*time.Minute)
	assert.NoError(t, err)
	assert.NotEmpty(t, cascades)
}

// Benchmark event processing
func BenchmarkService_ProcessEvent(b *testing.B) {
	logger := zap.NewNop()
	service := &Service{
		logger: logger,
	}

	event := &domain.UnifiedEvent{
		ID:        "bench-event",
		Type:      "pod_created",
		Source:    "kubeapi",
		Timestamp: time.Now(),
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "bench-pod",
			Namespace: "default",
			UID:       "bench-uid",
		},
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = service.ProcessEvent(ctx, event)
	}
}
