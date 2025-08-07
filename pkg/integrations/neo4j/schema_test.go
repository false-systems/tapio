package neo4j

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestCreateOrUpdateNode(t *testing.T) {
	client := &Client{
		logger: zap.NewNop(),
	}

	tests := []struct {
		name  string
		event *domain.UnifiedEvent
		err   error
	}{
		{
			name: "nil entity skips",
			event: &domain.UnifiedEvent{
				Entity: nil,
			},
			err: nil,
		},
		{
			name: "with entity",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Type:      "Pod",
					Name:      "test-pod",
					Namespace: "default",
					UID:       "uid-123",
					Labels: map[string]string{
						"app": "test",
					},
				},
				Timestamp: time.Now(),
			},
			err: nil,
		},
		{
			name: "with k8s context",
			event: &domain.UnifiedEvent{
				Entity: &domain.EntityContext{
					Type:      "Service",
					Name:      "test-svc",
					Namespace: "default",
					UID:       "svc-123",
				},
				K8sContext: &domain.K8sContext{
					Annotations: map[string]string{
						"annotation1": "value1",
					},
					ResourceVersion: "v1",
				},
				Timestamp: time.Now(),
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test doesn't execute write since no driver, just validates logic
			err := client.CreateOrUpdateNode(context.Background(), tt.event)

			if tt.event.Entity == nil {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLinkEventCausality(t *testing.T) {
	client := &Client{
		logger: zap.NewNop(),
	}

	ctx := context.Background()
	effectID := "effect-123"
	causeID := "cause-456"
	confidence := 0.85

	// Without a driver, this will return an error
	err := client.LinkEventCausality(ctx, effectID, causeID, confidence)
	assert.Error(t, err) // Expected since no driver is set
}

func TestLinkEventTrigger(t *testing.T) {
	client := &Client{
		logger: zap.NewNop(),
	}

	ctx := context.Background()
	triggerID := "trigger-123"
	triggeredID := "triggered-456"
	confidence := 0.75

	// Without a driver, this will return an error
	err := client.LinkEventTrigger(ctx, triggerID, triggeredID, confidence)
	assert.Error(t, err) // Expected since no driver is set
}

func TestGetNodeType(t *testing.T) {
	tests := []struct {
		input    string
		expected NodeType
	}{
		{"pod", NodePod},
		{"service", NodeService},
		{"deployment", NodeDeployment},
		{"replicaset", NodeReplicaSet},
		{"configmap", NodeConfigMap},
		{"secret", NodeSecret},
		{"node", NodeNode},
		{"namespace", NodeNamespace},
		{"customtype", NodeType("customtype")},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := getNodeType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapToStringArray(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected int // Expected length since order is not guaranteed
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: 0,
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: 0,
		},
		{
			name: "single entry",
			input: map[string]string{
				"key1": "value1",
			},
			expected: 1,
		},
		{
			name: "multiple entries",
			input: map[string]string{
				"app":     "test",
				"version": "1.0",
				"env":     "prod",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapToStringArray(tt.input)
			assert.Len(t, result, tt.expected)

			// For non-empty maps, verify format
			if tt.expected > 0 {
				for _, item := range result {
					assert.Contains(t, item, "=")
				}
			}
		})
	}
}

func TestCreateEvent(t *testing.T) {
	client := &Client{
		logger: zap.NewNop(),
	}

	tests := []struct {
		name  string
		event *domain.UnifiedEvent
	}{
		{
			name: "basic event",
			event: &domain.UnifiedEvent{
				ID:        "evt-123",
				Type:      "PodCrash",
				Timestamp: time.Now(),
				Severity:  domain.EventSeverityHigh,
				Message:   "Pod crashed",
				Source:    "kubelet",
			},
		},
		{
			name: "event with trace context",
			event: &domain.UnifiedEvent{
				ID:        "evt-456",
				Type:      "ServiceError",
				Timestamp: time.Now(),
				Severity:  domain.EventSeverityMedium,
				Message:   "Service error",
				Source:    "api-server",
				TraceContext: &domain.TraceContext{
					TraceID: "trace-789",
					SpanID:  "span-012",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Without a driver, this will return an error
			err := client.CreateEvent(context.Background(), tt.event)
			assert.Error(t, err) // Expected since no driver is set
		})
	}
}

func TestCreateEventRelationship(t *testing.T) {
	client := &Client{
		logger: zap.NewNop(),
	}

	ctx := context.Background()
	eventID := "evt-123"
	entityUID := "pod-456"
	relType := RelAffects

	// Without a driver, this will return an error
	err := client.CreateEventRelationship(ctx, eventID, entityUID, relType)
	assert.Error(t, err) // Expected since no driver is set
}

func TestRelationshipTypes(t *testing.T) {
	// Test that all RelationType values are defined
	relTypes := []RelationType{
		RelOwnedBy,
		RelSelectedBy,
		RelMounts,
		RelRunsOn,
		RelCausedBy,
		RelTriggeredBy,
		RelAffects,
		RelConnectsTo,
		RelInNamespace,
	}

	// Test that all RelationshipType values are defined
	relshipTypes := []RelationshipType{
		RelOwns,
		RelSelects,
		RelUses,
		RelExposedBy,
		RelPartOf,
		RelCorrelatedWith,
		RelRootCauseOf,
		RelImpactOf,
		RelRoutesTo,
	}

	// Ensure all RelationType values are unique
	seen := make(map[RelationType]bool)
	for _, rt := range relTypes {
		assert.False(t, seen[rt], "Duplicate RelationType: %s", rt)
		seen[rt] = true
	}

	// Ensure all RelationshipType values are unique
	seen2 := make(map[RelationshipType]bool)
	for _, rt := range relshipTypes {
		assert.False(t, seen2[rt], "Duplicate RelationshipType: %s", rt)
		seen2[rt] = true
	}
}

func TestNodeTypes(t *testing.T) {
	// Test that all node types are defined
	nodeTypes := []NodeType{
		NodePod,
		NodeService,
		NodeDeployment,
		NodeReplicaSet,
		NodeConfigMap,
		NodeSecret,
		NodeNode,
		NodeEvent,
		NodeNamespace,
	}

	// Ensure all node types are unique
	seen := make(map[NodeType]bool)
	for _, nt := range nodeTypes {
		assert.False(t, seen[nt], "Duplicate node type: %s", nt)
		seen[nt] = true
	}
}
