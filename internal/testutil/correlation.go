package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestEventData represents structured test event data
type TestEventData struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
}

// Kubernetes resource type constants for testing
const (
	ResourceTypePod         = "Pod"
	ResourceTypeService     = "Service"
	ResourceTypeConfigMap   = "ConfigMap"
	ResourceTypeSecret      = "Secret"
	ResourceTypeDeployment  = "Deployment"
	ResourceTypeStatefulSet = "StatefulSet"
	ResourceTypeDaemonSet   = "DaemonSet"
	ResourceTypeNode        = "Node"
)

// Common test namespaces
const (
	DefaultNamespace    = "default"
	KubeSystemNamespace = "kube-system"
	ProductionNamespace = "production"
)

// TestDataDir is the base directory for test data files
const TestDataDir = "testdata"

// LoadTestData loads test data from testdata directory
func LoadTestData(filename string) ([]byte, error) {
	path := filepath.Join(TestDataDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load test data from %s: %w", path, err)
	}
	return data, nil
}

// LoadTestEvents loads test events from JSON file
func LoadTestEvents(filename string) (map[string]TestEventData, error) {
	data, err := LoadTestData(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load test events: %w", err)
	}

	var events map[string]TestEventData
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("failed to unmarshal test events: %w", err)
	}

	return events, nil
}

// CreateTestEvent creates an ObservationEvent from test data for testing purposes
func CreateTestEvent(eventData TestEventData, eventType string, timestamp time.Time) *domain.ObservationEvent {
	namespace := eventData.Namespace
	name := eventData.Name

	event := &domain.ObservationEvent{
		ID:        eventData.ID,
		Type:      eventType,
		Timestamp: timestamp,
		Source:    "test",
		Namespace: &namespace,
		PodName:   &name,
		Data: map[string]string{
			"kind": eventData.Kind,
		},
	}

	return event
}

// CreateSimpleTestEvent creates a basic test event with minimal data
func CreateSimpleTestEvent(id, namespace, name string) *domain.ObservationEvent {
	return &domain.ObservationEvent{
		ID:        id,
		Type:      "kubernetes",
		Timestamp: time.Now(),
		Source:    "test",
		Namespace: &namespace,
		PodName:   &name,
		Data: map[string]string{
			"kind": ResourceTypePod,
		},
	}
}

// CreateTestPodEvent creates a test Pod event
func CreateTestPodEvent(id, namespace, name string, timestamp time.Time) *domain.ObservationEvent {
	return &domain.ObservationEvent{
		ID:        id,
		Type:      "pod",
		Timestamp: timestamp,
		Source:    "kubernetes",
		Namespace: &namespace,
		PodName:   &name,
		Data: map[string]string{
			"kind": ResourceTypePod,
		},
	}
}

// CreateTestServiceEvent creates a test Service event
func CreateTestServiceEvent(id, namespace, name string, timestamp time.Time) *domain.ObservationEvent {
	serviceName := name
	return &domain.ObservationEvent{
		ID:          id,
		Type:        "service",
		Timestamp:   timestamp,
		Source:      "kubernetes",
		Namespace:   &namespace,
		ServiceName: &serviceName,
		Data: map[string]string{
			"kind": ResourceTypeService,
		},
	}
}

// CreateTestConfigMapEvent creates a test ConfigMap event
func CreateTestConfigMapEvent(id, namespace, name string, timestamp time.Time) *domain.ObservationEvent {
	resourceName := name
	return &domain.ObservationEvent{
		ID:        id,
		Type:      "configmap",
		Timestamp: timestamp,
		Source:    "kubernetes",
		Namespace: &namespace,
		Data: map[string]string{
			"kind": ResourceTypeConfigMap,
			"name": resourceName,
		},
	}
}

// CreateTestDeploymentEvent creates a test Deployment event
func CreateTestDeploymentEvent(id, namespace, name string, timestamp time.Time) *domain.ObservationEvent {
	deploymentName := name
	return &domain.ObservationEvent{
		ID:        id,
		Type:      "deployment",
		Timestamp: timestamp,
		Source:    "kubernetes",
		Namespace: &namespace,
		Data: map[string]string{
			"kind": ResourceTypeDeployment,
			"name": deploymentName,
		},
	}
}

// GetTestNamespaces returns common test namespaces
func GetTestNamespaces() []string {
	return []string{
		DefaultNamespace,
		ProductionNamespace,
		KubeSystemNamespace,
	}
}

// GetTestResourceTypes returns common test resource types
func GetTestResourceTypes() []string {
	return []string{
		ResourceTypePod,
		ResourceTypeService,
		ResourceTypeConfigMap,
		ResourceTypeSecret,
		ResourceTypeDeployment,
		ResourceTypeStatefulSet,
		ResourceTypeDaemonSet,
		ResourceTypeNode,
	}
}
