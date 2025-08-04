package k8sgrapher

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// Test helper functions
func createTestService(name, namespace string, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("uid-" + name),
		},
		Spec: corev1.ServiceSpec{
			Selector:  selector,
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.1",
		},
	}
}

func createTestPod(name, namespace string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("uid-" + name),
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func TestNewK8sGrapher(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	kubeClient := fake.NewSimpleClientset()

	tests := []struct {
		name      string
		config    Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				KubeClient:      kubeClient,
				Neo4jDriver:     mockDriver,
				Logger:          logger,
				Instrumentation: instrumentation,
				ResyncPeriod:    30 * time.Minute,
			},
			wantError: false,
		},
		{
			name: "missing kube client",
			config: Config{
				Neo4jDriver:     mockDriver,
				Logger:          logger,
				Instrumentation: instrumentation,
			},
			wantError: true,
			errorMsg:  "kubeClient is required",
		},
		{
			name: "missing neo4j driver",
			config: Config{
				KubeClient:      kubeClient,
				Logger:          logger,
				Instrumentation: instrumentation,
			},
			wantError: true,
			errorMsg:  "neo4jDriver is required",
		},
		{
			name: "missing instrumentation",
			config: Config{
				KubeClient:  kubeClient,
				Neo4jDriver: mockDriver,
				Logger:      logger,
			},
			wantError: true,
			errorMsg:  "instrumentation is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grapher, err := NewK8sGrapher(tt.config)
			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, grapher)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, grapher)
			}
		})
	}
}

func TestK8sGrapher_InitializeSchema(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = grapher.initializeSchema(ctx)
	assert.NoError(t, err)

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}

func TestK8sGrapher_UpdateServiceNode(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}
	mockResult := NewMockResult()
	mockSummary := NewMockResultSummary()
	mockCounters := &MockCounters{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	// Mock for Consume to return summary
	mockResult.On("Consume", mock.Anything).Return(mockSummary, nil)
	mockResult.On("Err").Return(nil)
	mockResult.On("IsOpen").Return(true)

	// Mock summary methods
	mockSummary.On("Counters").Return(mockCounters)
	mockCounters.On("RelationshipsCreated").Return(1)

	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Test creating a service node
	service := createTestService("test-service", "default", map[string]string{"app": "test"})
	update := graphUpdate{
		operation: "create",
		nodeType:  "Service",
		data:      service,
	}

	ctx := context.Background()
	err = grapher.updateServiceNode(ctx, update)
	assert.NoError(t, err)

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}

func TestK8sGrapher_UpdatePodNode(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Test creating a pod node
	pod := createTestPod("test-pod", "default", map[string]string{"app": "test"})
	update := graphUpdate{
		operation: "create",
		nodeType:  "Pod",
		data:      pod,
	}

	ctx := context.Background()
	err = grapher.updatePodNode(ctx, update)
	assert.NoError(t, err)

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}

func TestK8sGrapher_ProcessGraphUpdates(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Start processing updates
	ctx, cancel := context.WithCancel(context.Background())
	grapher.wg.Add(1)
	go grapher.processGraphUpdates(ctx)

	// Send an update
	service := createTestService("test-service", "default", map[string]string{"app": "test"})
	grapher.graphUpdateChan <- graphUpdate{
		operation: "create",
		nodeType:  "Service",
		data:      service,
	}

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Stop processing
	cancel()
	close(grapher.graphUpdateChan)
	grapher.wg.Wait()

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}

func TestK8sGrapher_StartStop(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	// Create a fake kube client with some initial resources
	service := createTestService("test-service", "default", map[string]string{"app": "test"})
	pod := createTestPod("test-pod", "default", map[string]string{"app": "test"})

	kubeClient := fake.NewSimpleClientset(service, pod)

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
		ResyncPeriod:    1 * time.Second, // Short for testing
	})
	require.NoError(t, err)

	// Start the grapher
	ctx := context.Background()
	err = grapher.Start(ctx)
	assert.NoError(t, err)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop the grapher
	grapher.Stop()

	// Verify it stopped
	select {
	case <-grapher.stopCh:
		// Good, channel is closed
	default:
		t.Error("stopCh should be closed")
	}

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}

func TestK8sGrapher_UpdateConfigMapNode(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	// Test creating a configmap node
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
			UID:       "uid-test-configmap",
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	update := graphUpdate{
		operation: "create",
		nodeType:  "ConfigMap",
		data:      configMap,
	}

	ctx := context.Background()
	err = grapher.updateConfigMapNode(ctx, update)
	assert.NoError(t, err)

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}

func TestK8sGrapher_ApplyGraphUpdate(t *testing.T) {
	logger := zap.NewNop()
	instrumentation, err := telemetry.NewK8sGrapherInstrumentation(logger)
	require.NoError(t, err)

	mockDriver := &MockNeo4jDriver{}
	mockSession := &MockSession{}

	// Setup mock expectations
	mockDriver.On("NewSession", mock.Anything, mock.Anything).Return(mockSession)
	mockSession.On("Close", mock.Anything).Return(nil)
	mockSession.On("ExecuteWrite", mock.Anything, mock.Anything).Return(nil, nil)

	kubeClient := fake.NewSimpleClientset()

	grapher, err := NewK8sGrapher(Config{
		KubeClient:      kubeClient,
		Neo4jDriver:     mockDriver,
		Logger:          logger,
		Instrumentation: instrumentation,
	})
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		name      string
		update    graphUpdate
		wantError bool
	}{
		{
			name: "service update",
			update: graphUpdate{
				operation: "create",
				nodeType:  "Service",
				data:      createTestService("test-svc", "default", map[string]string{"app": "test"}),
			},
			wantError: false,
		},
		{
			name: "pod update",
			update: graphUpdate{
				operation: "create",
				nodeType:  "Pod",
				data:      createTestPod("test-pod", "default", map[string]string{"app": "test"}),
			},
			wantError: false,
		},
		{
			name: "unknown node type",
			update: graphUpdate{
				operation: "create",
				nodeType:  "Unknown",
				data:      nil,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := grapher.applyGraphUpdate(ctx, tt.update)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	mockDriver.AssertExpectations(t)
	mockSession.AssertExpectations(t)
}
