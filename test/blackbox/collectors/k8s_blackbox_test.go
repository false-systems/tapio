package blackbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sBlackBoxTestSuite tests Kubernetes collector from external perspective
type K8sBlackBoxTestSuite struct {
	client       kubernetes.Interface
	tapioClient  *TapioAPIClient
	namespace    string
	collectorURL string
}

// TapioAPIClient represents external API client
type TapioAPIClient struct {
	baseURL    string
	httpClient *http.Client
}

// Event represents a Tapio event from API perspective
type Event struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Source       string                 `json:"source"`
	Timestamp    time.Time              `json:"timestamp"`
	Data         map[string]interface{} `json:"data"`
	Correlations []string               `json:"correlations,omitempty"`
}

func TestK8sCollectorBlackBox(t *testing.T) {
	suite := setupTestSuite(t)
	defer suite.cleanup()

	t.Run("PodLifecycleEvents", func(t *testing.T) {
		suite.testPodLifecycleEvents(t)
	})

	t.Run("ServiceDiscovery", func(t *testing.T) {
		suite.testServiceDiscovery(t)
	})

	t.Run("ResourceQuotaEvents", func(t *testing.T) {
		suite.testResourceQuotaEvents(t)
	})

	t.Run("HighVolumeEvents", func(t *testing.T) {
		suite.testHighVolumeEvents(t)
	})

	t.Run("FailureScenarios", func(t *testing.T) {
		suite.testFailureScenarios(t)
	})
}

func (s *K8sBlackBoxTestSuite) testPodLifecycleEvents(t *testing.T) {
	// Create a test pod
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-lifecycle",
			Namespace: s.namespace,
			Labels: map[string]string{
				"test": "blackbox",
				"app":  "lifecycle-test",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "test-container",
					Image: "nginx:alpine",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    "100m",
							v1.ResourceMemory: "128Mi",
						},
					},
				},
			},
		},
	}

	// Track event collection
	eventCollector := s.startEventCollection(t, "pod_lifecycle_test")
	defer eventCollector.stop()

	// Create pod
	created, err := s.client.CoreV1().Pods(s.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for pod to be running
	require.Eventually(t, func() bool {
		pod, err := s.client.CoreV1().Pods(s.namespace).Get(context.TODO(), created.Name, metav1.GetOptions{})
		return err == nil && pod.Status.Phase == v1.PodRunning
	}, 30*time.Second, 1*time.Second, "Pod should be running")

	// Delete pod
	err = s.client.CoreV1().Pods(s.namespace).Delete(context.TODO(), created.Name, metav1.DeleteOptions{})
	require.NoError(t, err)

	// Wait for events to be processed
	time.Sleep(5 * time.Second)

	// Validate collected events
	events := eventCollector.getEvents()

	// Should have pod created, running, and terminated events
	assert.GreaterOrEqual(t, len(events), 3, "Should have at least 3 pod lifecycle events")

	// Validate event sequence
	var createdEvent, runningEvent, terminatedEvent *Event
	for _, e := range events {
		switch e.Type {
		case "pod_created":
			createdEvent = e
		case "pod_running":
			runningEvent = e
		case "pod_terminated":
			terminatedEvent = e
		}
	}

	assert.NotNil(t, createdEvent, "Should have pod_created event")
	assert.NotNil(t, runningEvent, "Should have pod_running event")
	assert.NotNil(t, terminatedEvent, "Should have pod_terminated event")

	// Validate event ordering
	if createdEvent != nil && runningEvent != nil {
		assert.True(t, createdEvent.Timestamp.Before(runningEvent.Timestamp),
			"Created event should come before running event")
	}

	// Validate event data
	if createdEvent != nil {
		assert.Equal(t, "test-pod-lifecycle", createdEvent.Data["pod_name"])
		assert.Equal(t, s.namespace, createdEvent.Data["namespace"])
		assert.Equal(t, "blackbox", createdEvent.Data["labels.test"])
	}
}

func (s *K8sBlackBoxTestSuite) testServiceDiscovery(t *testing.T) {
	// Create a deployment with service
	deployment := createTestDeployment("test-service-discovery", s.namespace, 3)
	service := createTestService("test-service-discovery", s.namespace)

	eventCollector := s.startEventCollection(t, "service_discovery_test")
	defer eventCollector.stop()

	// Create deployment
	_, err := s.client.AppsV1().Deployments(s.namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create service
	_, err = s.client.CoreV1().Services(s.namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for deployment to be ready
	require.Eventually(t, func() bool {
		dep, err := s.client.AppsV1().Deployments(s.namespace).Get(context.TODO(), deployment.Name, metav1.GetOptions{})
		return err == nil && dep.Status.ReadyReplicas == 3
	}, 60*time.Second, 2*time.Second, "Deployment should have 3 ready replicas")

	// Wait for events
	time.Sleep(10 * time.Second)

	// Validate events
	events := eventCollector.getEvents()

	// Should have service created and endpoint events
	serviceEvents := filterEventsByType(events, "service_created", "endpoints_updated")
	assert.GreaterOrEqual(t, len(serviceEvents), 2, "Should have service and endpoint events")

	// Validate service discovery correlation
	var serviceCreated *Event
	var endpointsUpdated []*Event

	for _, e := range events {
		if e.Type == "service_created" {
			serviceCreated = e
		} else if e.Type == "endpoints_updated" {
			endpointsUpdated = append(endpointsUpdated, e)
		}
	}

	assert.NotNil(t, serviceCreated, "Should have service_created event")
	assert.NotEmpty(t, endpointsUpdated, "Should have endpoints_updated events")

	// Verify correlation between service and pods
	if serviceCreated != nil && len(endpointsUpdated) > 0 {
		// Check that endpoints reference the service
		for _, ep := range endpointsUpdated {
			assert.Equal(t, serviceCreated.Data["service_name"], ep.Data["service_name"],
				"Endpoints should reference the correct service")
		}
	}

	// Cleanup
	s.client.CoreV1().Services(s.namespace).Delete(context.TODO(), service.Name, metav1.DeleteOptions{})
	s.client.AppsV1().Deployments(s.namespace).Delete(context.TODO(), deployment.Name, metav1.DeleteOptions{})
}

func (s *K8sBlackBoxTestSuite) testHighVolumeEvents(t *testing.T) {
	eventCollector := s.startEventCollection(t, "high_volume_test")
	defer eventCollector.stop()

	// Create multiple pods rapidly
	podCount := 20
	created := make([]*v1.Pod, 0, podCount)

	for i := 0; i < podCount; i++ {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("volume-test-pod-%d", i),
				Namespace: s.namespace,
				Labels: map[string]string{
					"test": "high-volume",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "test",
						Image:   "busybox",
						Command: []string{"sh", "-c", "echo 'test' && sleep 10"},
					},
				},
			},
		}

		createdPod, err := s.client.CoreV1().Pods(s.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		require.NoError(t, err)
		created = append(created, createdPod)
	}

	// Wait for events to be collected
	time.Sleep(15 * time.Second)

	// Validate high volume handling
	events := eventCollector.getEvents()

	// Should have events for all pods
	podEvents := filterEventsByLabel(events, "test", "high-volume")
	assert.GreaterOrEqual(t, len(podEvents), podCount,
		"Should have events for all %d pods", podCount)

	// Check for duplicate events
	eventIDs := make(map[string]bool)
	duplicates := 0
	for _, e := range podEvents {
		if eventIDs[e.ID] {
			duplicates++
		}
		eventIDs[e.ID] = true
	}
	assert.Equal(t, 0, duplicates, "Should have no duplicate events")

	// Check event ordering is preserved
	timestamps := make([]time.Time, len(events))
	for i, e := range events {
		timestamps[i] = e.Timestamp
	}
	assert.True(t, isTimestampsSorted(timestamps), "Events should be ordered by timestamp")

	// Cleanup
	for _, pod := range created {
		s.client.CoreV1().Pods(s.namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	}
}

func (s *K8sBlackBoxTestSuite) testFailureScenarios(t *testing.T) {
	t.Run("InvalidPodSpec", func(t *testing.T) {
		eventCollector := s.startEventCollection(t, "failure_invalid_spec")
		defer eventCollector.stop()

		// Create pod with invalid image
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-image-pod",
				Namespace: s.namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "invalid",
						Image: "this-image-definitely-does-not-exist:latest",
					},
				},
			},
		}

		_, err := s.client.CoreV1().Pods(s.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for failure events
		time.Sleep(30 * time.Second)

		events := eventCollector.getEvents()

		// Should have pod failure events
		failureEvents := filterEventsByType(events, "pod_failed", "image_pull_failed")
		assert.NotEmpty(t, failureEvents, "Should have failure events for invalid pod")

		// Validate error details are captured
		for _, e := range failureEvents {
			if e.Type == "image_pull_failed" {
				assert.Contains(t, e.Data["reason"], "ImagePullBackOff",
					"Should capture image pull failure reason")
			}
		}

		// Cleanup
		s.client.CoreV1().Pods(s.namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	})

	t.Run("ResourceExhaustion", func(t *testing.T) {
		eventCollector := s.startEventCollection(t, "failure_resource_exhaustion")
		defer eventCollector.stop()

		// Create pod requesting excessive resources
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "resource-exhaustion-pod",
				Namespace: s.namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "greedy",
						Image: "nginx:alpine",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    "100000m", // 100 CPUs
								v1.ResourceMemory: "1Ti",     // 1TB memory
							},
						},
					},
				},
			},
		}

		_, err := s.client.CoreV1().Pods(s.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		require.NoError(t, err)

		// Wait for scheduling failure
		time.Sleep(10 * time.Second)

		events := eventCollector.getEvents()

		// Should have scheduling failure events
		scheduleEvents := filterEventsByType(events, "pod_scheduling_failed", "insufficient_resources")
		assert.NotEmpty(t, scheduleEvents, "Should have resource exhaustion events")

		// Cleanup
		s.client.CoreV1().Pods(s.namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	})
}

// Helper functions

func setupTestSuite(t *testing.T) *K8sBlackBoxTestSuite {
	// Load kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	require.NoError(t, err)

	// Create k8s client
	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	// Create test namespace
	namespace := fmt.Sprintf("tapio-blackbox-test-%d", time.Now().Unix())
	_, err = client.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create Tapio API client
	tapioClient := &TapioAPIClient{
		baseURL: getEnvOrDefault("TAPIO_API_URL", "http://localhost:8080"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	return &K8sBlackBoxTestSuite{
		client:       client,
		tapioClient:  tapioClient,
		namespace:    namespace,
		collectorURL: getEnvOrDefault("TAPIO_COLLECTOR_URL", "http://localhost:9090"),
	}
}

func (s *K8sBlackBoxTestSuite) cleanup() {
	// Delete test namespace
	s.client.CoreV1().Namespaces().Delete(context.TODO(), s.namespace, metav1.DeleteOptions{})
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

type eventCollector struct {
	events   []*Event
	stopChan chan struct{}
	t        *testing.T
	client   *TapioAPIClient
	filter   string
}

func (s *K8sBlackBoxTestSuite) startEventCollection(t *testing.T, testName string) *eventCollector {
	ec := &eventCollector{
		events:   make([]*Event, 0),
		stopChan: make(chan struct{}),
		t:        t,
		client:   s.tapioClient,
		filter:   fmt.Sprintf("test=%s", testName),
	}

	go ec.collect()
	return ec
}

func (ec *eventCollector) collect() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ec.stopChan:
			return
		case <-ticker.C:
			// Query API for events
			resp, err := ec.client.httpClient.Get(
				fmt.Sprintf("%s/api/v1/events?filter=%s", ec.client.baseURL, ec.filter))
			if err != nil {
				ec.t.Logf("Failed to query events: %v", err)
				continue
			}

			var events []*Event
			if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
				ec.t.Logf("Failed to decode events: %v", err)
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			ec.events = append(ec.events, events...)
		}
	}
}

func (ec *eventCollector) stop() {
	close(ec.stopChan)
}

func (ec *eventCollector) getEvents() []*Event {
	return ec.events
}

func filterEventsByType(events []*Event, types ...string) []*Event {
	typeMap := make(map[string]bool)
	for _, t := range types {
		typeMap[t] = true
	}

	filtered := make([]*Event, 0)
	for _, e := range events {
		if typeMap[e.Type] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func filterEventsByLabel(events []*Event, key, value string) []*Event {
	filtered := make([]*Event, 0)
	for _, e := range events {
		if labels, ok := e.Data["labels"].(map[string]interface{}); ok {
			if labels[key] == value {
				filtered = append(filtered, e)
			}
		}
	}
	return filtered
}

func isTimestampsSorted(timestamps []time.Time) bool {
	for i := 1; i < len(timestamps); i++ {
		if timestamps[i].Before(timestamps[i-1]) {
			return false
		}
	}
	return true
}

// Test data creation helpers

func createTestDeployment(name, namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "nginx",
							Image: "nginx:alpine",
							Ports: []v1.ContainerPort{
								{ContainerPort: 80},
							},
						},
					},
				},
			},
		},
	}
}

func createTestService(name, namespace string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
			},
			Ports: []v1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			},
		},
	}
}
