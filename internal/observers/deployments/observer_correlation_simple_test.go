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

// Test-specific typed structs to avoid map[string]interface{}
type testCorrelationContext struct {
	Deployment testDeploymentContext  `json:"deployment"`
	Containers []testContainerContext `json:"containers"`
	Services   []testServiceContext   `json:"services"`
	Volumes    []testVolumeContext    `json:"volumes,omitempty"`
	Owners     []testOwnerContext     `json:"owners,omitempty"`
}

type testDeploymentContext struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Replicas    *int32            `json:"replicas,omitempty"`
	Strategy    string            `json:"strategy,omitempty"`
}

type testContainerContext struct {
	Name         string                   `json:"name"`
	Image        string                   `json:"image"`
	Ports        []testPortContext        `json:"ports,omitempty"`
	Env          []testEnvContext         `json:"env,omitempty"`
	VolumeMounts []testVolumeMountContext `json:"volumeMounts,omitempty"`
}

type testPortContext struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
}

type testEnvContext struct {
	Name         string                   `json:"name"`
	Value        string                   `json:"value,omitempty"`
	ConfigMapRef *testConfigMapRefContext `json:"configMapRef,omitempty"`
	SecretRef    *testSecretRefContext    `json:"secretRef,omitempty"`
}

type testConfigMapRefContext struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type testSecretRefContext struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type testVolumeMountContext struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

type testVolumeContext struct {
	Name      string                      `json:"name"`
	ConfigMap *testConfigMapVolumeContext `json:"configMap,omitempty"`
	Secret    *testSecretVolumeContext    `json:"secret,omitempty"`
	PVC       *testPVCVolumeContext       `json:"pvc,omitempty"`
}

type testConfigMapVolumeContext struct {
	Name        string `json:"name"`
	DefaultMode *int32 `json:"defaultMode,omitempty"`
}

type testSecretVolumeContext struct {
	SecretName  string `json:"secretName"`
	DefaultMode *int32 `json:"defaultMode,omitempty"`
}

type testPVCVolumeContext struct {
	ClaimName string `json:"claimName"`
}

type testServiceContext struct {
	Name      string                   `json:"name"`
	Namespace string                   `json:"namespace"`
	Type      string                   `json:"type"`
	Selector  map[string]string        `json:"selector,omitempty"`
	Ports     []testServicePortContext `json:"ports,omitempty"`
}

type testServicePortContext struct {
	Name       string `json:"name,omitempty"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol"`
}

type testOwnerContext struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller *bool  `json:"controller,omitempty"`
}

// TestSimpleCorrelationContext verifies that correlation context is captured
func TestSimpleCorrelationContext(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true
	config.BufferSize = 100

	observer, err := NewObserver("simple-correlation", config)
	require.NoError(t, err)

	// Replace with fake client for service discovery
	fakeClient := fake.NewSimpleClientset()
	observer.client = fakeClient
	observer.config.MockMode = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a service that will be correlated
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": "web",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
	_, err = fakeClient.CoreV1().Services("default").Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create a deployment with context for correlation
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app",
			Namespace: "default",
			Labels: map[string]string{
				"app": "web",
				"env": "production",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "web"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "web",
							Image: "nginx:1.20",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENV",
									Value: "production",
								},
								{
									Name: "DB_HOST",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "db-config"},
											Key:                  "host",
										},
									},
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

	// Find the deployment event
	var deploymentEvent *domain.CollectorEvent
	for i := 0; i < 5; i++ {
		select {
		case event := <-events:
			if event.Type == domain.EventTypeK8sDeployment {
				deploymentEvent = event
				break
			}
		case <-time.After(200 * time.Millisecond):
			continue
		}
	}

	require.NotNil(t, deploymentEvent, "Should receive deployment event")

	t.Logf("🎯 Received deployment event: %s", deploymentEvent.EventData.KubernetesEvent.Action)

	// Verify enhanced labels include deployment context
	labels := deploymentEvent.Metadata.Labels
	assert.Equal(t, "web-app", labels["deployment"])
	assert.Equal(t, "default", labels["namespace"])
	assert.Equal(t, "nginx:1.20", labels["image"])
	assert.Equal(t, "web", labels["container"])
	assert.Equal(t, "web", labels["app.app"])
	assert.Equal(t, "production", labels["app.env"])

	// Verify correlation context exists
	require.NotNil(t, deploymentEvent.Metadata.Attributes)
	correlationJSON, exists := deploymentEvent.Metadata.Attributes["correlation_context"]
	require.True(t, exists, "Should have correlation context")
	require.NotEmpty(t, correlationJSON)

	// Parse and verify key correlation data
	var context testCorrelationContext
	err = json.Unmarshal([]byte(correlationJSON), &context)
	require.NoError(t, err)

	// Verify deployment details
	assert.Equal(t, "web-app", context.Deployment.Name)
	assert.Equal(t, "default", context.Deployment.Namespace)

	// Verify container details
	require.Len(t, context.Containers, 1)
	container := context.Containers[0]
	assert.Equal(t, "web", container.Name)
	assert.Equal(t, "nginx:1.20", container.Image)

	// Verify environment variables with ConfigMap refs
	require.NotEmpty(t, container.Env, "Should have environment variables")

	// Check that ConfigMap reference is tracked
	foundConfigMapRef := false
	for _, env := range container.Env {
		if env.Name == "DB_HOST" {
			if env.ConfigMapRef != nil {
				assert.Equal(t, "db-config", env.ConfigMapRef.Name)
				assert.Equal(t, "host", env.ConfigMapRef.Key)
				foundConfigMapRef = true
			}
		}
	}
	assert.True(t, foundConfigMapRef, "Should track ConfigMap reference")

	// Verify related services are discovered
	require.Len(t, context.Services, 1)
	relatedService := context.Services[0]
	assert.Equal(t, "web-service", relatedService.Name)
	assert.Equal(t, "default", relatedService.Namespace)

	t.Logf("✅ Successfully captured rich correlation context:")
	t.Logf("   - Deployment: %s/%s", context.Deployment.Namespace, context.Deployment.Name)
	t.Logf("   - Container: %s (%s)", container.Name, container.Image)
	t.Logf("   - ConfigMap refs: %v", foundConfigMapRef)
	t.Logf("   - Related services: %d", len(context.Services))
}

// TestCorrelationDataIntegrity verifies correlation data structure
func TestCorrelationDataIntegrity(t *testing.T) {
	config := DefaultConfig()
	config.MockMode = true

	observer, err := NewObserver("integrity-test", config)
	require.NoError(t, err)

	// Create deployment with complex configuration
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "complex-app",
			Namespace: "prod",
			Labels: map[string]string{
				"app":     "complex",
				"version": "v2.1.0",
				"tier":    "backend",
			},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "5",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(5),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "complex"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "complex"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "api",
							Image: "complex-api:v2.1.0",
							Ports: []corev1.ContainerPort{
								{Name: "api", ContainerPort: 8080},
								{Name: "health", ContainerPort: 8081},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc/app", ReadOnly: true},
								{Name: "data", MountPath: "/var/data"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
								},
							},
						},
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "app-data",
								},
							},
						},
					},
				},
			},
		},
	}

	// Test context gathering directly
	correlationContext := observer.gatherCorrelationContext(deployment)

	// Verify deployment metadata structure
	deploymentCtx := correlationContext.Deployment
	assert.Equal(t, "complex-app", deploymentCtx.Name)
	assert.Equal(t, "prod", deploymentCtx.Namespace)
	assert.NotNil(t, deploymentCtx.Labels)
	assert.NotNil(t, deploymentCtx.Annotations)
	assert.Equal(t, int32(5), *deploymentCtx.Replicas)
	assert.Equal(t, "RollingUpdate", deploymentCtx.Strategy)

	// Verify container structure
	containers := correlationContext.Containers
	require.Len(t, containers, 1)
	container := containers[0]
	assert.Equal(t, "api", container.Name)
	assert.Equal(t, "complex-api:v2.1.0", container.Image)
	assert.Len(t, container.Ports, 2)
	assert.Len(t, container.VolumeMounts, 2)

	// Verify volume structure
	volumes := correlationContext.Volumes
	require.Len(t, volumes, 2)

	// Find ConfigMap volume
	var configMapVolume *VolumeContext
	for i := range volumes {
		if volumes[i].Name == "config" {
			configMapVolume = &volumes[i]
			break
		}
	}
	require.NotNil(t, configMapVolume)
	assert.NotNil(t, configMapVolume.ConfigMap)
	assert.Equal(t, "app-config", configMapVolume.ConfigMap.Name)

	t.Logf("✅ Correlation context structure validated")
	t.Logf("   - Deployment: %s/%s with %d replicas", deploymentCtx.Namespace, deploymentCtx.Name, *deploymentCtx.Replicas)
	t.Logf("   - Container details: %d ports, %d mounts", len(container.Ports), len(container.VolumeMounts))
	t.Logf("   - Volume configurations: %d", len(volumes))
}
