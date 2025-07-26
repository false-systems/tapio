package extraction

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// TestK8sExtractionSimple tests K8s context extraction without informers
func TestK8sExtractionSimple(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid-123",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node-1",
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "test:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase:    corev1.PodRunning,
			PodIP:    "10.244.1.5",
			QOSClass: corev1.PodQOSBurstable,
		},
	}

	// Create fake client with test data
	k8sClient := fake.NewSimpleClientset(testPod)

	// Create a simple extractor that uses direct API calls
	extractor := &simpleK8sExtractor{
		k8sClient: k8sClient,
		logger:    logger,
	}

	// Test extraction
	event := &domain.UnifiedEvent{
		ID:        "test-1",
		Timestamp: time.Now(),
		Source:    "test",
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	err := extractor.ExtractK8sContext(ctx, event)
	require.NoError(t, err)
	require.NotNil(t, event.K8sContext)

	assert.Equal(t, "test-pod", event.K8sContext.Name)
	assert.Equal(t, "default", event.K8sContext.Namespace)
	assert.Equal(t, "test-node-1", event.K8sContext.NodeName)
	assert.Equal(t, "Running", event.K8sContext.Phase)
}

// simpleK8sExtractor demonstrates a simple K8s context extraction
type simpleK8sExtractor struct {
	k8sClient kubernetes.Interface
	logger    *zap.Logger
}

func (e *simpleK8sExtractor) ExtractK8sContext(ctx context.Context, event *domain.UnifiedEvent) error {
	if event.Entity == nil || event.Entity.Type != "pod" {
		return nil
	}

	// Direct API call to get pod
	pod, err := e.k8sClient.CoreV1().Pods(event.Entity.Namespace).Get(ctx, event.Entity.Name, metav1.GetOptions{})
	if err != nil {
		return nil // Skip if pod not found
	}

	// Initialize K8s context
	if event.K8sContext == nil {
		event.K8sContext = &domain.K8sContext{}
	}

	// Extract basic information
	k8sCtx := event.K8sContext
	k8sCtx.APIVersion = pod.APIVersion
	k8sCtx.Kind = pod.Kind
	k8sCtx.UID = string(pod.UID)
	k8sCtx.Name = pod.Name
	k8sCtx.Namespace = pod.Namespace
	k8sCtx.ResourceVersion = pod.ResourceVersion
	k8sCtx.NodeName = pod.Spec.NodeName
	k8sCtx.Phase = string(pod.Status.Phase)
	k8sCtx.QoSClass = string(pod.Status.QOSClass)
	k8sCtx.Labels = pod.Labels

	// Extract ownership
	for _, ref := range pod.OwnerReferences {
		ownerRef := domain.OwnerReference{
			APIVersion:         ref.APIVersion,
			Kind:               ref.Kind,
			Name:               ref.Name,
			UID:                string(ref.UID),
			Controller:         ref.Controller,
			BlockOwnerDeletion: ref.BlockOwnerDeletion,
		}
		k8sCtx.OwnerReferences = append(k8sCtx.OwnerReferences, ownerRef)
	}

	return nil
}
