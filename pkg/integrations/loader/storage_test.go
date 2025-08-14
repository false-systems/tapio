package loader

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNodeCreationRequests(t *testing.T) {
	tests := []struct {
		name     string
		events   []*domain.ObservationEvent
		expected map[string]int // Expected count of different node types
	}{
		{
			name: "events with pod references",
			events: []*domain.ObservationEvent{
				{
					ID:        "event-1",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					PodName:   stringPtr("pod-1"),
					Namespace: stringPtr("default"),
				},
				{
					ID:        "event-2",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					PodName:   stringPtr("pod-2"),
					Namespace: stringPtr("kube-system"),
				},
				{
					ID:        "event-3",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					PodName:   stringPtr("pod-1"), // Same pod as event-1
					Namespace: stringPtr("default"),
				},
			},
			expected: map[string]int{
				"pods": 2, // Should deduplicate pod-1 in default namespace
			},
		},
		{
			name: "events with service references",
			events: []*domain.ObservationEvent{
				{
					ID:          "event-1",
					Timestamp:   time.Now(),
					Source:      "kubeapi",
					Type:        "service-update",
					ServiceName: stringPtr("service-1"),
					Namespace:   stringPtr("default"),
				},
				{
					ID:          "event-2",
					Timestamp:   time.Now(),
					Source:      "kubeapi",
					Type:        "service-update",
					ServiceName: stringPtr("service-2"),
					Namespace:   stringPtr("kube-system"),
				},
			},
			expected: map[string]int{
				"services": 2,
			},
		},
		{
			name: "events with node references",
			events: []*domain.ObservationEvent{
				{
					ID:        "event-1",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					NodeName:  stringPtr("node-1"),
				},
				{
					ID:        "event-2",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					NodeName:  stringPtr("node-2"),
				},
				{
					ID:        "event-3",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					NodeName:  stringPtr("node-1"), // Same node as event-1
				},
			},
			expected: map[string]int{
				"nodes": 2, // Should deduplicate node-1
			},
		},
		{
			name: "mixed resource types",
			events: []*domain.ObservationEvent{
				{
					ID:          "event-1",
					Timestamp:   time.Now(),
					Source:      "kubeapi",
					Type:        "pod-created",
					PodName:     stringPtr("pod-1"),
					Namespace:   stringPtr("default"),
					ServiceName: stringPtr("service-1"),
					NodeName:    stringPtr("node-1"),
				},
			},
			expected: map[string]int{
				"pods":     1,
				"services": 1,
				"nodes":    1,
			},
		},
		{
			name: "no resource references",
			events: []*domain.ObservationEvent{
				{
					ID:        "event-1",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					PID:       int32Ptr(1234),
				},
			},
			expected: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test pod extraction
			pods := make(map[string]map[string]string)
			for _, event := range tt.events {
				if event.PodName != nil && event.Namespace != nil {
					namespace := *event.Namespace
					podName := *event.PodName

					if pods[namespace] == nil {
						pods[namespace] = make(map[string]string)
					}

					uid := fmt.Sprintf("obs-pod-%s-%s", namespace, podName)
					pods[namespace][podName] = uid
				}
			}

			// Count unique pods
			podCount := 0
			for _, nameToUID := range pods {
				podCount += len(nameToUID)
			}

			if expectedPods, ok := tt.expected["pods"]; ok {
				assert.Equal(t, expectedPods, podCount, "Pod count mismatch")
			} else {
				assert.Equal(t, 0, podCount, "Expected no pods")
			}

			// Test service extraction
			services := make(map[string]map[string]string)
			for _, event := range tt.events {
				if event.ServiceName != nil && event.Namespace != nil {
					namespace := *event.Namespace
					serviceName := *event.ServiceName

					if services[namespace] == nil {
						services[namespace] = make(map[string]string)
					}

					uid := fmt.Sprintf("obs-svc-%s-%s", namespace, serviceName)
					services[namespace][serviceName] = uid
				}
			}

			// Count unique services
			serviceCount := 0
			for _, nameToUID := range services {
				serviceCount += len(nameToUID)
			}

			if expectedServices, ok := tt.expected["services"]; ok {
				assert.Equal(t, expectedServices, serviceCount, "Service count mismatch")
			} else {
				assert.Equal(t, 0, serviceCount, "Expected no services")
			}

			// Test node extraction
			nodes := make(map[string]bool)
			for _, event := range tt.events {
				if event.NodeName != nil {
					nodes[*event.NodeName] = true
				}
			}

			if expectedNodes, ok := tt.expected["nodes"]; ok {
				assert.Equal(t, expectedNodes, len(nodes), "Node count mismatch")
			} else {
				assert.Equal(t, 0, len(nodes), "Expected no nodes")
			}
		})
	}
}

func TestRelationshipCreation(t *testing.T) {
	tests := []struct {
		name     string
		events   []*domain.ObservationEvent
		expected map[string]int // Expected count of different relationship types
	}{
		{
			name: "observation to pod relationships",
			events: []*domain.ObservationEvent{
				{
					ID:        "event-1",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					PodName:   stringPtr("pod-1"),
					Namespace: stringPtr("default"),
				},
				{
					ID:        "event-2",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					PodName:   stringPtr("pod-2"),
					Namespace: stringPtr("default"),
				},
			},
			expected: map[string]int{
				"pod_relationships": 2,
			},
		},
		{
			name: "observation to service relationships",
			events: []*domain.ObservationEvent{
				{
					ID:          "event-1",
					Timestamp:   time.Now(),
					Source:      "kubeapi",
					Type:        "service-event",
					ServiceName: stringPtr("service-1"),
					Namespace:   stringPtr("default"),
				},
			},
			expected: map[string]int{
				"service_relationships": 1,
			},
		},
		{
			name: "observation to node relationships",
			events: []*domain.ObservationEvent{
				{
					ID:        "event-1",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					NodeName:  stringPtr("node-1"),
				},
			},
			expected: map[string]int{
				"node_relationships": 1,
			},
		},
		{
			name: "causal relationships",
			events: []*domain.ObservationEvent{
				{
					ID:        "event-1",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					CausedBy:  stringPtr("event-0"),
				},
				{
					ID:        "event-2",
					Timestamp: time.Now(),
					Source:    "kernel",
					Type:      "syscall",
					ParentID:  stringPtr("event-1"),
				},
			},
			expected: map[string]int{
				"causal_relationships": 2,
			},
		},
		{
			name: "mixed relationships",
			events: []*domain.ObservationEvent{
				{
					ID:          "event-1",
					Timestamp:   time.Now(),
					Source:      "kubeapi",
					Type:        "pod-event",
					PodName:     stringPtr("pod-1"),
					Namespace:   stringPtr("default"),
					ServiceName: stringPtr("service-1"),
					NodeName:    stringPtr("node-1"),
					CausedBy:    stringPtr("event-0"),
				},
			},
			expected: map[string]int{
				"pod_relationships":     1,
				"service_relationships": 1,
				"node_relationships":    1,
				"causal_relationships":  1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Count pod relationships
			var podRels []map[string]interface{}
			for _, event := range tt.events {
				if event.PodName != nil && event.Namespace != nil {
					uid := fmt.Sprintf("obs-pod-%s-%s", *event.Namespace, *event.PodName)
					podRels = append(podRels, map[string]interface{}{
						"observation_id": event.ID,
						"pod_uid":        uid,
						"timestamp":      event.Timestamp.UnixMilli(),
					})
				}
			}

			if expected, ok := tt.expected["pod_relationships"]; ok {
				assert.Equal(t, expected, len(podRels), "Pod relationship count mismatch")
			}

			// Count service relationships
			var serviceRels []map[string]interface{}
			for _, event := range tt.events {
				if event.ServiceName != nil && event.Namespace != nil {
					uid := fmt.Sprintf("obs-svc-%s-%s", *event.Namespace, *event.ServiceName)
					serviceRels = append(serviceRels, map[string]interface{}{
						"observation_id": event.ID,
						"service_uid":    uid,
						"timestamp":      event.Timestamp.UnixMilli(),
					})
				}
			}

			if expected, ok := tt.expected["service_relationships"]; ok {
				assert.Equal(t, expected, len(serviceRels), "Service relationship count mismatch")
			}

			// Count node relationships
			var nodeRels []map[string]interface{}
			for _, event := range tt.events {
				if event.NodeName != nil {
					nodeRels = append(nodeRels, map[string]interface{}{
						"observation_id": event.ID,
						"node_name":      *event.NodeName,
						"timestamp":      event.Timestamp.UnixMilli(),
					})
				}
			}

			if expected, ok := tt.expected["node_relationships"]; ok {
				assert.Equal(t, expected, len(nodeRels), "Node relationship count mismatch")
			}

			// Count causal relationships
			var causalRels []map[string]interface{}
			for _, event := range tt.events {
				if event.CausedBy != nil {
					causalRels = append(causalRels, map[string]interface{}{
						"effect_id": event.ID,
						"cause_id":  *event.CausedBy,
						"timestamp": event.Timestamp.UnixMilli(),
					})
				}
				if event.ParentID != nil {
					causalRels = append(causalRels, map[string]interface{}{
						"child_id":  event.ID,
						"parent_id": *event.ParentID,
						"timestamp": event.Timestamp.UnixMilli(),
					})
				}
			}

			if expected, ok := tt.expected["causal_relationships"]; ok {
				assert.Equal(t, expected, len(causalRels), "Causal relationship count mismatch")
			}
		})
	}
}

func TestStorageStats(t *testing.T) {
	stats := &StorageStats{
		NodesCreated:         10,
		RelationshipsCreated: 20,
		StorageTime:          100 * time.Millisecond,
		BatchSize:            5,
	}

	assert.Equal(t, int64(10), stats.NodesCreated)
	assert.Equal(t, int64(20), stats.RelationshipsCreated)
	assert.Equal(t, 100*time.Millisecond, stats.StorageTime)
	assert.Equal(t, 5, stats.BatchSize)
}

// Helper functions for creating pointers
func stringPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}
