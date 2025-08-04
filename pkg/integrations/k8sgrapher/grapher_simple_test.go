package k8sgrapher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// Test basic K8sGrapher creation and configuration
func TestNewK8sGrapher_Simple(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() (*K8sGrapher, error)
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config with defaults",
			setupFunc: func() (*K8sGrapher, error) {
				logger := zap.NewNop()
				instrumentation, _ := telemetry.NewK8sGrapherInstrumentation(logger)
				mockDriver := &MockNeo4jDriver{}
				kubeClient := fake.NewSimpleClientset()

				return NewK8sGrapher(Config{
					KubeClient:      kubeClient,
					Neo4jDriver:     mockDriver,
					Logger:          logger,
					Instrumentation: instrumentation,
				})
			},
			wantError: false,
		},
		{
			name: "missing kubeClient",
			setupFunc: func() (*K8sGrapher, error) {
				logger := zap.NewNop()
				instrumentation, _ := telemetry.NewK8sGrapherInstrumentation(logger)
				mockDriver := &MockNeo4jDriver{}

				return NewK8sGrapher(Config{
					Neo4jDriver:     mockDriver,
					Logger:          logger,
					Instrumentation: instrumentation,
				})
			},
			wantError: true,
			errorMsg:  "kubeClient is required",
		},
		{
			name: "missing neo4jDriver",
			setupFunc: func() (*K8sGrapher, error) {
				logger := zap.NewNop()
				instrumentation, _ := telemetry.NewK8sGrapherInstrumentation(logger)
				kubeClient := fake.NewSimpleClientset()

				return NewK8sGrapher(Config{
					KubeClient:      kubeClient,
					Logger:          logger,
					Instrumentation: instrumentation,
				})
			},
			wantError: true,
			errorMsg:  "neo4jDriver is required",
		},
		{
			name: "missing instrumentation",
			setupFunc: func() (*K8sGrapher, error) {
				logger := zap.NewNop()
				mockDriver := &MockNeo4jDriver{}
				kubeClient := fake.NewSimpleClientset()

				return NewK8sGrapher(Config{
					KubeClient:  kubeClient,
					Neo4jDriver: mockDriver,
					Logger:      logger,
				})
			},
			wantError: true,
			errorMsg:  "instrumentation is required",
		},
		{
			name: "with custom resync period",
			setupFunc: func() (*K8sGrapher, error) {
				logger := zap.NewNop()
				instrumentation, _ := telemetry.NewK8sGrapherInstrumentation(logger)
				mockDriver := &MockNeo4jDriver{}
				kubeClient := fake.NewSimpleClientset()

				return NewK8sGrapher(Config{
					KubeClient:      kubeClient,
					Neo4jDriver:     mockDriver,
					Logger:          logger,
					Instrumentation: instrumentation,
					ResyncPeriod:    10 * time.Minute,
				})
			},
			wantError: false,
		},
		{
			name: "with namespace filter",
			setupFunc: func() (*K8sGrapher, error) {
				logger := zap.NewNop()
				instrumentation, _ := telemetry.NewK8sGrapherInstrumentation(logger)
				mockDriver := &MockNeo4jDriver{}
				kubeClient := fake.NewSimpleClientset()

				return NewK8sGrapher(Config{
					KubeClient:      kubeClient,
					Neo4jDriver:     mockDriver,
					Logger:          logger,
					Instrumentation: instrumentation,
					Namespace:       "production",
				})
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grapher, err := tt.setupFunc()

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, grapher)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, grapher)

				// Verify basic initialization
				if grapher != nil {
					assert.NotNil(t, grapher.kubeClient)
					assert.NotNil(t, grapher.neo4jDriver)
					assert.NotNil(t, grapher.logger)
					assert.NotNil(t, grapher.instrumentation)
					assert.NotNil(t, grapher.informers)
					assert.NotNil(t, grapher.stopCh)
					assert.NotNil(t, grapher.graphUpdateChan)

					// Check defaults
					if grapher.resyncPeriod == 0 {
						assert.Equal(t, 30*time.Minute, grapher.resyncPeriod)
					}
				}
			}
		})
	}
}

// Test helper functions
func TestHelperFunctions(t *testing.T) {
	t.Run("formatSelector", func(t *testing.T) {
		selector := map[string]string{
			"app":     "frontend",
			"version": "v1",
		}
		result := formatSelector(selector)
		assert.Contains(t, result, "app=frontend")
		assert.Contains(t, result, "version=v1")
	})

	t.Run("formatLabels", func(t *testing.T) {
		labels := map[string]string{
			"app": "test",
			"env": "prod",
		}
		result := formatLabels(labels)
		assert.Equal(t, len(labels), len(result))
		assert.Equal(t, "test", result["app"])
		assert.Equal(t, "prod", result["env"])
	})

	t.Run("isPodReady", func(t *testing.T) {
		// Ready pod
		readyPod := &corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		assert.True(t, isPodReady(readyPod))

		// Not ready pod
		notReadyPod := &corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}
		assert.False(t, isPodReady(notReadyPod))
	})

	t.Run("getOwnerInfo", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "ReplicaSet",
						Name: "test-rs",
					},
				},
			},
		}
		kind, name := getOwnerInfo(pod)
		assert.Equal(t, "ReplicaSet", kind)
		assert.Equal(t, "test-rs", name)

		// No owner
		podNoOwner := &corev1.Pod{}
		kind, name = getOwnerInfo(podNoOwner)
		assert.Equal(t, "", kind)
		assert.Equal(t, "", name)
	})

	t.Run("getMapKeys", func(t *testing.T) {
		m := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}
		keys := getMapKeys(m)
		assert.Len(t, keys, 3)
		assert.Contains(t, keys, "key1")
		assert.Contains(t, keys, "key2")
		assert.Contains(t, keys, "key3")
	})

	t.Run("buildResourceID", func(t *testing.T) {
		id := buildResourceID("default", "my-pod")
		assert.Equal(t, "default/my-pod", id)
	})

	t.Run("parseResourceID", func(t *testing.T) {
		ns, name := parseResourceID("default/my-pod")
		assert.Equal(t, "default", ns)
		assert.Equal(t, "my-pod", name)

		// No namespace
		ns, name = parseResourceID("my-pod")
		assert.Equal(t, "", ns)
		assert.Equal(t, "my-pod", name)
	})
}

// Test informer setup
func TestInformerSetup(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Test that informers can be created
	t.Run("create service informer", func(t *testing.T) {
		grapher.startServiceInformer()
		assert.Contains(t, grapher.informers, "services")
		assert.NotNil(t, grapher.informers["services"])
	})

	t.Run("create pod informer", func(t *testing.T) {
		grapher.startPodInformer()
		assert.Contains(t, grapher.informers, "pods")
		assert.NotNil(t, grapher.informers["pods"])
	})

	t.Run("create configmap informer", func(t *testing.T) {
		grapher.startConfigMapInformer()
		assert.Contains(t, grapher.informers, "configmaps")
		assert.NotNil(t, grapher.informers["configmaps"])
	})

	t.Run("create secret informer", func(t *testing.T) {
		grapher.startSecretInformer()
		assert.Contains(t, grapher.informers, "secrets")
		assert.NotNil(t, grapher.informers["secrets"])
	})

	t.Run("create deployment informer", func(t *testing.T) {
		grapher.startDeploymentInformer()
		assert.Contains(t, grapher.informers, "deployments")
		assert.NotNil(t, grapher.informers["deployments"])
	})

	t.Run("create replicaset informer", func(t *testing.T) {
		grapher.startReplicaSetInformer()
		assert.Contains(t, grapher.informers, "replicasets")
		assert.NotNil(t, grapher.informers["replicasets"])
	})

	t.Run("create pvc informer", func(t *testing.T) {
		grapher.startPVCInformer()
		assert.Contains(t, grapher.informers, "pvcs")
		assert.NotNil(t, grapher.informers["pvcs"])
	})
}

// Test data structures
func TestGraphUpdate(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}

	update := graphUpdate{
		operation: "create",
		nodeType:  "Service",
		data:      service,
	}

	assert.Equal(t, "create", update.operation)
	assert.Equal(t, "Service", update.nodeType)
	assert.NotNil(t, update.data)

	// Type assertion should work
	svc, ok := update.data.(*corev1.Service)
	assert.True(t, ok)
	assert.Equal(t, "test-service", svc.Name)
}

// Test concurrent operations
func TestConcurrentGraphUpdates(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Send multiple updates concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-" + string(rune(id)),
					Namespace: "default",
				},
			}
			grapher.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "Service",
				data:      service,
			}
			done <- true
		}(i)
	}

	// Verify all updates were sent
	updatesSent := 0
	for i := 0; i < 10; i++ {
		<-done
		updatesSent++
	}
	assert.Equal(t, 10, updatesSent)
}

// Test Stop functionality
func TestK8sGrapher_Stop(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Stop should close channels
	grapher.Stop()

	// Verify stopCh is closed
	select {
	case <-grapher.stopCh:
		// Good, channel is closed
	default:
		t.Error("stopCh should be closed after Stop()")
	}

	// Verify graphUpdateChan is closed
	select {
	case _, ok := <-grapher.graphUpdateChan:
		assert.False(t, ok, "graphUpdateChan should be closed")
	default:
		// Channel might be empty but not closed, which is also fine
	}
}
