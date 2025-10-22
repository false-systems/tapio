package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestComputeOTELAttributes_FromLabels(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name":    "my-service",
				"app.kubernetes.io/version": "v1.0.0",
			},
		},
	}

	attrs := ComputeOTELAttributes(pod)

	// Should extract from app.kubernetes.io/ labels
	assert.Equal(t, "my-service", attrs["service.name"])
	assert.Equal(t, "v1.0.0", attrs["service.version"])
}

func TestComputeOTELAttributes_FromAnnotations(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "from-label",
			},
			Annotations: map[string]string{
				"resource.opentelemetry.io/service.name": "from-annotation",
			},
		},
	}

	attrs := ComputeOTELAttributes(pod)

	// Annotations override labels (priority cascade)
	assert.Equal(t, "from-annotation", attrs["service.name"])
}

func TestComputeOTELAttributes_FromEnvVars(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "from-label",
			},
			Annotations: map[string]string{
				"resource.opentelemetry.io/service.name": "from-annotation",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Env: []corev1.EnvVar{
						{Name: "OTEL_SERVICE_NAME", Value: "from-env"},
					},
				},
			},
		},
	}

	attrs := ComputeOTELAttributes(pod)

	// Env vars have highest priority
	assert.Equal(t, "from-env", attrs["service.name"])
}

func TestComputeOTELAttributes_EmptyPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	attrs := ComputeOTELAttributes(pod)

	// Should use pod name as fallback
	assert.Equal(t, "test-pod", attrs["service.name"])
}

func TestComputeOTELAttributes_NilPod(t *testing.T) {
	attrs := ComputeOTELAttributes(nil)

	// Should return empty map (not panic)
	assert.NotNil(t, attrs)
	assert.Empty(t, attrs)
}
