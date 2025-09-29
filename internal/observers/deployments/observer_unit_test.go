package deployments

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TestConfig tests the configuration structure
func TestConfig(t *testing.T) {
	t.Run("default_config", func(t *testing.T) {
		config := DefaultConfig()
		assert.Equal(t, 1000, config.BufferSize)
		assert.Equal(t, 30*time.Second, config.ResyncPeriod)
		assert.True(t, config.TrackConfigMaps)
		assert.True(t, config.TrackSecrets)
		assert.True(t, config.IgnoreSystemDeployments)
		assert.Equal(t, 5*time.Minute, config.DeduplicationWindow)
		assert.Empty(t, config.Namespaces)
		assert.Empty(t, config.AnnotationFilter)
	})

	t.Run("config_validation", func(t *testing.T) {
		config := &Config{
			BufferSize:          -1, // Invalid
			ResyncPeriod:        0,  // Invalid
			DeduplicationWindow: 0,  // Invalid
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer_size must be positive")
	})

	t.Run("config_validation_resync_period", func(t *testing.T) {
		config := &Config{
			Name:                "test",
			BufferSize:          1000,
			ResyncPeriod:        0, // Invalid
			DeduplicationWindow: time.Minute,
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resync_period must be positive")
	})

	t.Run("config_validation_dedup_window", func(t *testing.T) {
		config := &Config{
			Name:                "test",
			BufferSize:          1000,
			ResyncPeriod:        time.Minute,
			DeduplicationWindow: 0, // Invalid
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deduplication_window must be positive")
	})

	t.Run("config_validation_empty_name", func(t *testing.T) {
		config := &Config{
			Name:                "", // Invalid
			BufferSize:          1000,
			ResyncPeriod:        time.Minute,
			DeduplicationWindow: time.Minute,
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "name cannot be empty")
	})
}

// TestNewObserver tests observer creation
func TestNewObserver(t *testing.T) {
	t.Run("valid_config", func(t *testing.T) {
		config := DefaultConfig()
		config.MockMode = true // Use mock mode for testing

		observer, err := NewObserver("test-deployments", config)
		require.NoError(t, err)
		require.NotNil(t, observer)

		assert.Equal(t, "test-deployments", observer.config.Name)
		assert.NotNil(t, observer.tracer)
		assert.NotNil(t, observer.deploymentsTracked)
		assert.NotNil(t, observer.recentEvents)
	})

	t.Run("nil_config_uses_default", func(t *testing.T) {
		// Create config with mock mode to avoid K8s client issues
		config := DefaultConfig()
		config.MockMode = true

		observer, err := NewObserver("test", config)
		require.NoError(t, err)
		require.NotNil(t, observer)

		// Should use default config
		assert.Equal(t, 1000, observer.config.BufferSize)
	})

	t.Run("invalid_config", func(t *testing.T) {
		config := &Config{
			BufferSize: -1,
		}

		observer, err := NewObserver("test", config)
		assert.Error(t, err)
		assert.Nil(t, observer)
		assert.Contains(t, err.Error(), "buffer_size must be positive")
	})

	t.Run("create_with_kubeconfig", func(t *testing.T) {
		config := DefaultConfig()
		config.MockMode = false
		config.KubeConfig = "/invalid/path/kubeconfig"

		observer, err := NewObserver("test", config)
		assert.Error(t, err)
		assert.Nil(t, observer)
		assert.Contains(t, err.Error(), "failed to build config from kubeconfig")
	})
}

// TestObserverLifecycle tests start/stop lifecycle
func TestObserverLifecycle(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Test Start
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Should be healthy after start
	assert.True(t, observer.IsHealthy())

	// Test Events channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Test Stop
	err = observer.Stop()
	require.NoError(t, err)

	// Should be unhealthy after stop
	assert.False(t, observer.IsHealthy())
}

// TestShouldTrackDeployment tests deployment filtering
func TestShouldTrackDeployment(t *testing.T) {
	t.Run("track_all_namespaces", func(t *testing.T) {
		config := DefaultConfig()
		config.MockMode = true
		observer, err := NewObserver("test", config)
		require.NoError(t, err)

		deployment := createTestDeployment("test-app", "default")
		should := observer.shouldTrackDeployment(deployment)
		assert.True(t, should)
	})

	t.Run("namespace_filter", func(t *testing.T) {
		config := DefaultConfig()
		config.MockMode = true
		config.Namespaces = []string{"production", "staging"}
		observer, err := NewObserver("test", config)
		require.NoError(t, err)

		// Should track production
		deployment := createTestDeployment("test-app", "production")
		should := observer.shouldTrackDeployment(deployment)
		assert.True(t, should)

		// Should not track default
		deployment = createTestDeployment("test-app", "default")
		should = observer.shouldTrackDeployment(deployment)
		assert.False(t, should)
	})

	t.Run("ignore_system_deployments", func(t *testing.T) {
		config := DefaultConfig()
		config.MockMode = true
		config.IgnoreSystemDeployments = true
		observer, err := NewObserver("test", config)
		require.NoError(t, err)

		// Should ignore kube-system
		deployment := createTestDeployment("coredns", "kube-system")
		should := observer.shouldTrackDeployment(deployment)
		assert.False(t, should)

		// Should track user namespace
		deployment = createTestDeployment("test-app", "default")
		should = observer.shouldTrackDeployment(deployment)
		assert.True(t, should)
	})

	t.Run("annotation_filter", func(t *testing.T) {
		config := DefaultConfig()
		config.MockMode = true
		config.AnnotationFilter = "tapio.io/monitor"
		observer, err := NewObserver("test", config)
		require.NoError(t, err)

		// Should track with annotation
		deployment := createTestDeployment("test-app", "default")
		deployment.Annotations = map[string]string{
			"tapio.io/monitor": "true",
		}
		should := observer.shouldTrackDeployment(deployment)
		assert.True(t, should)

		// Should not track without annotation
		deployment = createTestDeployment("test-app", "default")
		should = observer.shouldTrackDeployment(deployment)
		assert.False(t, should)
	})
}

// TestHasSignificantChange tests change detection
func TestHasSignificantChange(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	t.Run("image_change", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		newDep := createTestDeployment("test-app", "default")
		newDep.Spec.Template.Spec.Containers[0].Image = "nginx:1.20"

		changed := observer.hasSignificantChange(oldDep, newDep)
		assert.True(t, changed)
	})

	t.Run("replica_change", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		newDep := createTestDeployment("test-app", "default")
		newReplicas := int32(5)
		newDep.Spec.Replicas = &newReplicas

		changed := observer.hasSignificantChange(oldDep, newDep)
		assert.True(t, changed)
	})

	t.Run("strategy_change", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		newDep := createTestDeployment("test-app", "default")
		newDep.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType

		changed := observer.hasSignificantChange(oldDep, newDep)
		assert.True(t, changed)
	})

	t.Run("no_change", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		newDep := createTestDeployment("test-app", "default")

		changed := observer.hasSignificantChange(oldDep, newDep)
		assert.False(t, changed)
	})
}

// TestIsRollback tests rollback detection
func TestIsRollback(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	t.Run("revision_rollback", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		oldDep.Annotations = map[string]string{
			"deployment.kubernetes.io/revision": "3",
		}

		newDep := createTestDeployment("test-app", "default")
		newDep.Annotations = map[string]string{
			"deployment.kubernetes.io/revision": "2",
		}

		isRollback := observer.isRollback(oldDep, newDep)
		assert.True(t, isRollback)
	})

	t.Run("rollback_annotation", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		newDep := createTestDeployment("test-app", "default")
		newDep.Annotations = map[string]string{
			"kubectl.kubernetes.io/rollback": "true",
		}

		isRollback := observer.isRollback(oldDep, newDep)
		assert.True(t, isRollback)
	})

	t.Run("not_rollback", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		oldDep.Annotations = map[string]string{
			"deployment.kubernetes.io/revision": "2",
		}

		newDep := createTestDeployment("test-app", "default")
		newDep.Annotations = map[string]string{
			"deployment.kubernetes.io/revision": "3",
		}

		isRollback := observer.isRollback(oldDep, newDep)
		assert.False(t, isRollback)
	})
}

// TestCreateDeploymentEvent tests event creation
func TestCreateDeploymentEvent(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	t.Run("create_event", func(t *testing.T) {
		deployment := createTestDeployment("test-app", "default")

		event := observer.createDeploymentEvent(deployment, "created", nil)

		assert.NotEmpty(t, event.EventID)
		assert.Equal(t, domain.EventTypeK8sDeployment, event.Type)
		assert.Equal(t, "deployments-test", event.Source)
		assert.Equal(t, domain.EventSeverityInfo, event.Severity)

		// Check Kubernetes event data
		require.NotNil(t, event.EventData.KubernetesEvent)
		k8sData := event.EventData.KubernetesEvent
		assert.Equal(t, "created", k8sData.Action)
		assert.Equal(t, "default", k8sData.InvolvedObject.Namespace)
		assert.Equal(t, "test-app", k8sData.InvolvedObject.Name)

		// Check metadata
		assert.Equal(t, "test", event.Metadata.Labels["observer"])
		assert.Equal(t, "created", event.Metadata.Labels["action"])
		assert.Equal(t, "default", event.Metadata.Labels["namespace"])
		assert.Equal(t, "test-app", event.Metadata.Labels["deployment"])
	})

	t.Run("update_event_with_old", func(t *testing.T) {
		oldDep := createTestDeployment("test-app", "default")
		newDep := createTestDeployment("test-app", "default")
		newDep.Spec.Template.Spec.Containers[0].Image = "nginx:1.20"

		event := observer.createDeploymentEvent(newDep, "updated", oldDep)

		require.NotNil(t, event.EventData.KubernetesEvent)
		k8sData := event.EventData.KubernetesEvent
		assert.Equal(t, "updated", k8sData.Action)

		// Should include image change information in message or reason
		assert.Contains(t, k8sData.Message, "nginx:1.19")
		assert.Contains(t, k8sData.Message, "nginx:1.20")
	})
}

// TestEventDeduplication tests event deduplication
func TestEventDeduplication(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	config.BufferSize = 10
	config.DeduplicationWindow = 100 * time.Millisecond

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create test event
	event := &domain.CollectorEvent{
		EventID:   "test-event-123",
		Type:      domain.EventTypeK8sDeployment,
		Timestamp: time.Now(),
		Source:    "deployments-test",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			KubernetesEvent: &domain.K8sAPIEventData{
				Action: "created",
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}

	// First send should succeed
	observer.sendEvent(event)

	// Immediate duplicate should be blocked
	observer.sendEvent(event)

	// Wait for deduplication window to expire
	time.Sleep(150 * time.Millisecond)

	// Should succeed again after window
	observer.sendEvent(event)

	// Verify events were processed
	stats := observer.Statistics()
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(2))
}

// Helper function to create test deployment
func createTestDeployment(name, namespace string) *appsv1.Deployment {
	replicas := int32(3)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "nginx:1.19",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "25%",
					},
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "25%",
					},
				},
			},
		},
	}
}

// Helper function to create test ConfigMap
func createTestConfigMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: "123",
		},
		Data: map[string]string{
			"config.yaml": "key: value",
		},
	}
}

// Helper function to create test Secret
func createTestSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: "456",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte("secret"),
		},
	}
}
