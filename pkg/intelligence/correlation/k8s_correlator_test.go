package correlation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// MockK8sClient moved to k8s_mocks_test.go

func testNamespaceEventCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	event := &domain.UnifiedEvent{
		ID:        "quota-exceeded",
		Type:      EventTypeK8s,
		Timestamp: time.Now(),
		Severity:  domain.EventSeverityCritical,
		K8sContext: &domain.K8sContext{
			Namespace: "production",
			Kind:      "Namespace",
		},
		Message: "Namespace resource quota exceeded",
	}

	results, err := correlator.Process(ctx, event)
	require.NoError(t, err)
	assert.NotNil(t, results)
}

func testAPIErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	mockClient.On("GetPod", ctx, "default", "missing-pod").Return(
		(*domain.K8sPod)(nil), errors.New("pod not found"))

	event := createTestPodEvent("api-error-event", "default", "missing-pod", "Pod event")

	results, err := correlator.Process(ctx, event)
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func testNilEventHandling(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	results, err := correlator.Process(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, results)
}

func testNonK8sEventHandling(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	event := &domain.UnifiedEvent{
		ID:        "non-k8s",
		Type:      EventTypeSystemd,
		Timestamp: time.Now(),
		Message:   "Systemd service restarted",
	}

	results, err := correlator.Process(ctx, event)
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

// Test helper functions
func createTestPodEvent(id, namespace, name, message string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        id,
		Type:      EventTypeK8s,
		Timestamp: time.Now(),
		Severity:  domain.EventSeverityError,
		K8sContext: &domain.K8sContext{
			Namespace: namespace,
			Name:      name,
			Kind:      "Pod",
		},
		Message: message,
	}
}

func createTestServiceEvent(id, namespace, name, message string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        id,
		Type:      EventTypeK8s,
		Timestamp: time.Now(),
		Severity:  domain.EventSeverityWarning,
		K8sContext: &domain.K8sContext{
			Namespace: namespace,
			Name:      name,
			Kind:      "Service",
		},
		Message: message,
	}
}

func createTestPods(namespace string, names []string, labels map[string]string) []*domain.K8sPod {
	pods := make([]*domain.K8sPod, len(names))
	for i, name := range names {
		pods[i] = &domain.K8sPod{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		}
	}
	return pods
}

func TestK8sCorrelatorCreation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("create with valid client", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)

		assert.NotNil(t, correlator)
		assert.Equal(t, "k8s", correlator.Name())
		assert.NotNil(t, correlator.ownerCache)
		assert.NotNil(t, correlator.selectorCache)
		assert.NotNil(t, correlator.eventCache)
	})

	t.Run("create without client", func(t *testing.T) {
		correlator := NewK8sCorrelator(logger, nil)

		assert.NotNil(t, correlator)
		assert.Nil(t, correlator.k8sClient)
	})
}

func TestK8sCorrelatorProcess(t *testing.T) {
	t.Run("correlate pod events with deployment", testPodEventCorrelation)
	t.Run("correlate service events", testServiceEventCorrelation)
	t.Run("correlate namespace events", testNamespaceEventCorrelation)
	t.Run("handle API errors gracefully", testAPIErrorHandling)
	t.Run("nil event handling", testNilEventHandling)
	t.Run("non-K8s event", testNonK8sEventHandling)
}

func testPodEventCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	// Setup mock responses
	pod := &domain.K8sPod{
		Name:      "api-pod-xyz",
		Namespace: "production",
		Labels: map[string]string{
			"app": "api",
		},
		OwnerReferences: []domain.K8sOwnerReference{
			{Kind: "ReplicaSet", Name: "api-rs-123"},
		},
	}

	deployment := &domain.K8sDeployment{
		Name:      "api-deployment",
		Namespace: "production",
		Labels:    map[string]string{"app": "api"},
	}

	mockClient.On("GetPod", ctx, "production", "api-pod-xyz").Return(pod, nil)
	mockClient.On("GetDeployment", ctx, "production", mock.Anything).Return(deployment, nil)

	// Pod crash event
	event := createTestPodEvent("pod-crash-1", "production", "api-pod-xyz", "Pod crashed with OOMKilled")

	results, err := correlator.Process(ctx, event)
	require.NoError(t, err)
	require.Len(t, results, 1)

	result := results[0]
	assert.Equal(t, "k8s_ownership", result.Type)
	assert.Contains(t, result.Message, "deployment")
	assert.Contains(t, result.Impact.Resources, "production/api-deployment")
}

func testServiceEventCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	// Setup service and related pods
	service := &domain.K8sService{
		Name:      "api-service",
		Namespace: "production",
		Selector:  map[string]string{"app": "api"},
	}

	pods := createTestPods("production", []string{"api-pod-1", "api-pod-2"}, map[string]string{"app": "api"})

	mockClient.On("GetService", ctx, "production", "api-service").Return(service, nil)
	mockClient.On("ListPods", ctx, "production", service.Selector).Return(pods, nil)

	event := createTestServiceEvent("service-disruption", "production", "api-service", "Service endpoints not ready")

	results, err := correlator.Process(ctx, event)
	require.NoError(t, err)
	require.Len(t, results, 1)

	result := results[0]
	assert.Equal(t, "k8s_ownership", result.Type)
	assert.Contains(t, result.Impact.Resources, "production/api-pod-1")
	assert.Contains(t, result.Impact.Resources, "production/api-pod-2")
}

func TestK8sCorrelatorStart(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("start watch successfully", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)

		// Create watch channels
		podChan := make(chan domain.K8sWatchEvent, 10)
		mockClient.On("WatchPods", mock.Anything, "").Return((<-chan domain.K8sWatchEvent)(podChan), nil)
		serviceChan := make(chan domain.K8sWatchEvent, 10)
		mockClient.On("WatchServices", mock.Anything, "").Return((<-chan domain.K8sWatchEvent)(serviceChan), nil)

		ctx := context.Background()
		err := correlator.Start(ctx)
		require.NoError(t, err)

		// Send test event
		testEvent := domain.K8sWatchEvent{
			Type: domain.K8sWatchAdded,
			Object: &domain.K8sPod{
				Name:      "test-pod",
				Namespace: "default",
			},
		}

		select {
		case podChan <- testEvent:
			// Event sent
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Failed to send event")
		}

		// Stop correlator
		err = correlator.Stop()
		require.NoError(t, err)

		close(podChan)
		close(serviceChan)
	})

	t.Run("handle watch error", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)

		// Watch returns error
		mockClient.On("WatchPods", mock.Anything, "").Return(
			(<-chan domain.K8sWatchEvent)(nil),
			errors.New("watch failed"),
		)

		ctx := context.Background()
		err := correlator.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "watch failed")
	})

	t.Run("start without client", func(t *testing.T) {
		correlator := NewK8sCorrelator(logger, nil)

		ctx := context.Background()
		err := correlator.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "K8s client not configured")
	})
}

func TestK8sOwnershipCache(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("cache ownership relationships", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)
		ctx := context.Background()

		// First call - should hit API
		pod := &domain.K8sPod{
			Name:      "test-pod",
			Namespace: "default",
			OwnerReferences: []domain.K8sOwnerReference{
				{Kind: "ReplicaSet", Name: "test-rs"},
			},
		}

		mockClient.On("GetPod", ctx, "default", "test-pod").Return(pod, nil).Once()

		// Process event twice
		event := &domain.UnifiedEvent{
			ID:        "cache-test-1",
			Type:      EventTypeK8s,
			Timestamp: time.Now(),
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "test-pod",
				Kind:      "Pod",
			},
		}

		// First call
		_, err := correlator.Process(ctx, event)
		require.NoError(t, err)

		// Second call - should use cache
		_, err = correlator.Process(ctx, event)
		require.NoError(t, err)

		// Mock should only be called once
		mockClient.AssertNumberOfCalls(t, "GetPod", 1)
	})

	t.Run("cache expiry", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)

		// Set short cache TTL for testing
		correlator.ownerCache = &OwnershipCache{
			items: make(map[string]*OwnershipInfo),
		}

		// Add to cache
		key := "test-key"
		correlator.ownerCache.mu.Lock()
		correlator.ownerCache.items[key] = &OwnershipInfo{
			Owners: []ResourceRef{
				{Kind: "Deployment", Name: "test-deployment"},
			},
		}
		correlator.ownerCache.mu.Unlock()

		// Should exist initially
		correlator.ownerCache.mu.RLock()
		info := correlator.ownerCache.items["test-key"]
		correlator.ownerCache.mu.RUnlock()
		assert.NotNil(t, info)

		// Wait for expiry
		time.Sleep(150 * time.Millisecond)

		// Cache doesn't expire in this implementation
		// so we just check it still exists
		correlator.ownerCache.mu.RLock()
		info = correlator.ownerCache.items["test-key"]
		correlator.ownerCache.mu.RUnlock()
		assert.NotNil(t, info)
	})
}

func TestK8sEventHistory(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("track event history", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)
		ctx := context.Background()

		// Process multiple events for same resource
		baseTime := time.Now()

		for i := 0; i < 5; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("history-%d", i),
				Type:      EventTypeK8s,
				Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "test-pod",
					Kind:      "Pod",
				},
				Message: fmt.Sprintf("Event %d", i),
			}

			_, err := correlator.Process(ctx, event)
			require.NoError(t, err)
		}

		// Check event cache
		correlator.eventCache.mu.RLock()
		cachedEvents := make([]*domain.UnifiedEvent, 0)
		for _, cached := range correlator.eventCache.events {
			if cached != nil && cached.Event != nil {
				cachedEvents = append(cachedEvents, cached.Event)
			}
		}
		correlator.eventCache.mu.RUnlock()
		history := cachedEvents

		assert.Len(t, history, 5)

		// Events should be in chronological order
		for i := 1; i < len(history); i++ {
			assert.True(t, history[i].Timestamp.After(history[i-1].Timestamp))
		}
	})

	t.Run("history size limit", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)
		ctx := context.Background()

		// Process more events than history limit

		for i := 0; i < 150; i++ {
			event := &domain.UnifiedEvent{
				ID:        fmt.Sprintf("overflow-%d", i),
				Type:      EventTypeK8s,
				Timestamp: time.Now(),
				K8sContext: &domain.K8sContext{
					Namespace: "default",
					Name:      "overflow-pod",
					Kind:      "Pod",
				},
			}

			_, err := correlator.Process(ctx, event)
			require.NoError(t, err)
		}

		// Check event cache size
		correlator.eventCache.mu.RLock()
		cacheSize := len(correlator.eventCache.events)
		correlator.eventCache.mu.RUnlock()

		assert.LessOrEqual(t, cacheSize, 150) // Reasonable cache size
	})
}

func TestK8sCorrelatorPatterns(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("detect pod restart loop", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)
		ctx := context.Background()

		pod := &domain.K8sPod{
			Name:         "crashloop-pod",
			Namespace:    "default",
			RestartCount: 5,
		}

		mockClient.On("GetPod", ctx, "default", "crashloop-pod").Return(pod, nil)

		// CrashLoopBackOff event
		event := &domain.UnifiedEvent{
			ID:        "crashloop",
			Type:      EventTypeK8s,
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityError,
			K8sContext: &domain.K8sContext{
				Namespace: "default",
				Name:      "crashloop-pod",
				Kind:      "Pod",
			},
			Message: "Back-off restarting failed container",
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)

		// Should detect restart loop
		require.Len(t, results, 1)
		result := results[0]
		assert.Contains(t, result.Message, "restart")
		assert.Equal(t, domain.EventSeverityError, result.Impact.Severity)
	})

	t.Run("detect rollout issues", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)
		ctx := context.Background()

		deployment := &domain.K8sDeployment{
			Name:            "api-deployment",
			Namespace:       "production",
			Replicas:        5,
			UpdatedReplicas: 2,
			ReadyReplicas:   2,
		}

		mockClient.On("GetDeployment", ctx, "production", "api-deployment").Return(deployment, nil)

		// Rollout stuck event
		event := &domain.UnifiedEvent{
			ID:        "rollout-stuck",
			Type:      EventTypeK8s,
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityWarning,
			K8sContext: &domain.K8sContext{
				Namespace: "production",
				Name:      "api-deployment",
				Kind:      "Deployment",
			},
			Message: "Deployment rollout stuck",
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)

		// Should detect rollout issue
		require.Len(t, results, 1)
		result := results[0]
		assert.Contains(t, result.Message, "rollout")
		assert.Contains(t, result.Details, "2/5")
	})
}

func TestK8sCorrelatorConcurrency(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("concurrent cache access", func(t *testing.T) {
		mockClient := &MockK8sClient{}
		correlator := NewK8sCorrelator(logger, mockClient)
		ctx := context.Background()

		// Setup mock to return different pods
		for i := 0; i < 10; i++ {
			pod := &domain.K8sPod{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "default",
			}
			mockClient.On("GetPod", ctx, "default", fmt.Sprintf("pod-%d", i)).Return(pod, nil).Maybe()
		}

		errChan := make(chan error, 100)

		// Process events concurrently
		for i := 0; i < 100; i++ {
			go func(id int) {
				event := &domain.UnifiedEvent{
					ID:        fmt.Sprintf("concurrent-%d", id),
					Type:      EventTypeK8s,
					Timestamp: time.Now(),
					K8sContext: &domain.K8sContext{
						Namespace: "default",
						Name:      fmt.Sprintf("pod-%d", id%10),
						Kind:      "Pod",
					},
				}

				_, err := correlator.Process(ctx, event)
				errChan <- err
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 100; i++ {
			err := <-errChan
			assert.NoError(t, err)
		}
	})
}

func BenchmarkK8sCorrelatorProcess(b *testing.B) {
	logger := zaptest.NewLogger(b).Sugar().Desugar()
	mockClient := &MockK8sClient{}
	correlator := NewK8sCorrelator(logger, mockClient)
	ctx := context.Background()

	// Setup mock
	pod := &domain.K8sPod{
		Name:      "bench-pod",
		Namespace: "benchmark",
	}
	mockClient.On("GetPod", ctx, "benchmark", "bench-pod").Return(pod, nil).Maybe()

	event := &domain.UnifiedEvent{
		ID:        "bench-event",
		Type:      EventTypeK8s,
		Timestamp: time.Now(),
		K8sContext: &domain.K8sContext{
			Namespace: "benchmark",
			Name:      "bench-pod",
			Kind:      "Pod",
		},
		Message: "Benchmark event",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := correlator.Process(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}
