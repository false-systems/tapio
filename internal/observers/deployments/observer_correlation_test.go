package deployments

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

// Test-specific typed structs are defined in observer_correlation_simple_test.go
// to avoid duplication while maintaining type safety

// TestCorrelationContext verifies rich context data is captured for correlation
func TestCorrelationContext(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	config.BufferSize = 100

	observer, err := NewObserver("correlation-test", config)
	require.NoError(t, err)

	// Replace with fake client for service discovery
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a Service first (for correlation testing)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": "webapp",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	_, err = fakeClient.CoreV1().Services("default").Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a ConfigMap (for dependency tracking)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"database_url": "postgres://localhost",
			"api_key":      "test-key",
		},
	}
	_, err = fakeClient.CoreV1().ConfigMaps("default").Create(ctx, configMap, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a complex deployment with rich context
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp",
			Namespace: "default",
			Labels: map[string]string{
				"app":     "webapp",
				"version": "v1.2.0",
				"tier":    "frontend",
			},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
				"app.kubernetes.io/version":         "1.2.0",
				"app.kubernetes.io/component":       "web-server",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "webapp",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "webapp",
						"version": "v1.2.0",
						"tier":    "frontend",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "webapp",
							Image: "webapp:v1.2.0",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: 9090,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENV",
									Value: "production",
								},
								{
									Name: "DATABASE_URL",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "webapp-config",
											},
											Key: "database_url",
										},
									},
								},
								{
									Name: "API_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "webapp-secrets",
											},
											Key: "api_secret",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-volume",
									MountPath: "/etc/config",
									ReadOnly:  true,
								},
								{
									Name:      "data-volume",
									MountPath: "/var/data",
									ReadOnly:  false,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "webapp-config",
									},
								},
							},
						},
						{
							Name: "data-volume",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "webapp-data",
								},
							},
						},
					},
				},
			},
		},
	}

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Create the deployment
	_, err = fakeClient.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for the deployment event (may need to skip ConfigMap events)
	var deploymentEvent *domain.CollectorEvent
	for i := 0; i < 5; i++ { // Try up to 5 events
		select {
		case event := <-events:
			t.Logf("Received event type: %s, action: %s", event.Type, event.EventData.KubernetesEvent.Action)
			if event.Type == domain.EventTypeK8sDeployment && event.EventData.KubernetesEvent.Action == "created" {
				deploymentEvent = event
				break
			}
		case <-time.After(500 * time.Millisecond):
			continue
		}
	}

	require.NotNil(t, deploymentEvent, "Should have received deployment created event")

	// Debug logging
	t.Logf("Event metadata: %+v", deploymentEvent.Metadata)
	t.Logf("Event labels: %+v", deploymentEvent.Metadata.Labels)
	t.Logf("Event attributes: %+v", deploymentEvent.Metadata.Attributes)

	// Verify enhanced metadata labels
	labels := deploymentEvent.Metadata.Labels
	assert.Equal(t, "webapp", labels["deployment"])
	assert.Equal(t, "default", labels["namespace"])
	assert.Equal(t, "webapp:v1.2.0", labels["image"])
	assert.Equal(t, "webapp", labels["container"])
	assert.Equal(t, "webapp", labels["app.app"])
	assert.Equal(t, "v1.2.0", labels["app.version"])
	assert.Equal(t, "frontend", labels["app.tier"])

	// Verify correlation context exists in attributes
	require.NotNil(t, deploymentEvent.Metadata.Attributes)
	correlationJSON, exists := deploymentEvent.Metadata.Attributes["correlation_context"]
	require.True(t, exists)
	require.NotEmpty(t, correlationJSON)

	// Parse correlation context
	var context testCorrelationContext
	err = json.Unmarshal([]byte(correlationJSON), &context)
	require.NoError(t, err)

	// Verify deployment context
	assert.Equal(t, "webapp", context.Deployment.Name)
	assert.Equal(t, "default", context.Deployment.Namespace)
	require.NotNil(t, context.Deployment.Replicas)
	assert.Equal(t, int32(3), *context.Deployment.Replicas)
	assert.Equal(t, "RollingUpdate", context.Deployment.Strategy)

	// Verify container context
	require.Len(t, context.Containers, 1)
	container := context.Containers[0]
	assert.Equal(t, "webapp", container.Name)
	assert.Equal(t, "webapp:v1.2.0", container.Image)

	// Verify container ports
	require.Len(t, container.Ports, 2)
	port := container.Ports[0]
	assert.Equal(t, "http", port.Name)
	assert.Equal(t, int32(8080), port.ContainerPort)

	// Verify environment variables with ConfigMap/Secret refs
	require.Len(t, container.Env, 3)

	// Find DATABASE_URL env var with ConfigMap reference
	var databaseEnv *testEnvContext
	for i := range container.Env {
		if container.Env[i].Name == "DATABASE_URL" {
			databaseEnv = &container.Env[i]
			break
		}
	}
	require.NotNil(t, databaseEnv)
	require.NotNil(t, databaseEnv.ConfigMapRef)
	assert.Equal(t, "webapp-config", databaseEnv.ConfigMapRef.Name)
	assert.Equal(t, "database_url", databaseEnv.ConfigMapRef.Key)

	// Verify volume mounts
	require.Len(t, container.VolumeMounts, 2)
	volumeMount := container.VolumeMounts[0]
	assert.Equal(t, "config-volume", volumeMount.Name)
	assert.Equal(t, "/etc/config", volumeMount.MountPath)
	assert.Equal(t, true, volumeMount.ReadOnly)

	// Verify volume specifications
	require.Len(t, context.Volumes, 2)

	// Find ConfigMap volume
	var configVolume *testVolumeContext
	for i := range context.Volumes {
		if context.Volumes[i].Name == "config-volume" {
			configVolume = &context.Volumes[i]
			break
		}
	}
	require.NotNil(t, configVolume)
	require.NotNil(t, configVolume.ConfigMap)
	assert.Equal(t, "webapp-config", configVolume.ConfigMap.Name)

	// Verify related services are found
	require.Len(t, context.Services, 1)
	relatedService := context.Services[0]
	assert.Equal(t, "webapp-service", relatedService.Name)
	assert.Equal(t, "default", relatedService.Namespace)
	assert.Equal(t, string(corev1.ServiceTypeClusterIP), relatedService.Type)

	require.Len(t, relatedService.Ports, 1)
	servicePort := relatedService.Ports[0]
	assert.Equal(t, "http", servicePort.Name)
	assert.Equal(t, int32(80), servicePort.Port)

	t.Logf("✅ Rich correlation context captured: %d containers, %d volumes, %d services",
		len(context.Containers), len(context.Volumes), len(context.Services))
}

// TestCorrelationContextWithOwnerRefs tests owner reference tracking
func TestCorrelationContextWithOwnerRefs(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true

	observer, err := NewObserver("owner-test", config)
	require.NoError(t, err)

	// Create deployment with owner references (simulating ReplicaSet ownership)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-app",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "owned-app-rs-abc123",
					UID:        "test-uid-123",
					Controller: boolPtr(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "owned-app"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "owned-app"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "app:latest",
						},
					},
				},
			},
		},
	}

	// Test the context gathering directly
	correlationContext := observer.gatherCorrelationContext(deployment)

	// Verify owner references are captured
	owners := correlationContext.Owners
	require.Len(t, owners, 1)

	owner := owners[0]
	assert.Equal(t, "ReplicaSet", owner.Kind)
	assert.Equal(t, "owned-app-rs-abc123", owner.Name)
	assert.Equal(t, "test-uid-123", owner.UID)
	require.NotNil(t, owner.Controller)
	assert.Equal(t, true, *owner.Controller)

	t.Logf("✅ Owner references captured in correlation context")
}

// Helper function for bool pointer
func boolPtr(b bool) *bool {
	return &b
}
