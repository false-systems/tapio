package k8scontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveOwner_Deployment(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "ReplicaSet",
					Name:       "nginx-7d8f9xxxx",
					Controller: boolPtr(true),
				},
			},
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "Deployment", kind)
	assert.Equal(t, "nginx", name)
}

func TestResolveOwner_StatefulSet(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "StatefulSet",
					Name:       "postgres",
					Controller: boolPtr(true),
				},
			},
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "StatefulSet", kind)
	assert.Equal(t, "postgres", name)
}

func TestResolveOwner_CronJob(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Job",
					Name:       "backup-28391234",
					Controller: boolPtr(true),
				},
			},
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "CronJob", kind)
	assert.Equal(t, "backup", name)
}

func TestResolveOwner_ArgoCD(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/instance": "my-app",
			},
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "Application", kind)
	assert.Equal(t, "my-app", name)
}

func TestResolveOwner_Knative(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"serving.knative.dev/service": "hello",
			},
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "KnativeService", kind)
	assert.Equal(t, "hello", name)
}

func TestResolveOwner_StandalonePod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "debug-pod",
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "Pod", kind)
	assert.Equal(t, "debug-pod", name)
}

func TestResolveOwner_ExplicitAnnotation(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"resource.k8s.deployment.name": "my-service",
			},
		},
	}

	kind, name := ResolveOwner(pod)
	assert.Equal(t, "Deployment", kind)
	assert.Equal(t, "my-service", name)
}
