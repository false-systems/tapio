package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnrichWithK8sContext_NetworkEvent verifies network event enrichment with graph entities
func TestEnrichWithK8sContext_NetworkEvent(t *testing.T) {
	observerEvent := &ObserverEvent{
		ID:        "evt-123",
		Type:      "tcp_rtt_spike",
		Subtype:   "performance_degradation",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &NetworkEventData{
			Protocol:       "tcp",
			SrcIP:          "10.0.1.5",
			DstIP:          "10.0.2.10",
			RTTCurrent:     50.3,
			RTTBaseline:    10.2,
			RTTDegradation: 393.0, // (50.3-10.2)/10.2 * 100 = 393%
			HTTPStatusCode: 200,
			PodName:        "web-server-abc",
			Namespace:      "production",
		},
	}

	k8sContext := &K8sContext{
		ClusterID:    "prod-us-east",
		PodName:      "web-server-abc",
		PodNamespace: "production",
		PodLabels: map[string]string{
			"app": "web-server",
			"env": "prod",
		},
		PodIP:       "10.0.1.5",
		HostIP:      "192.168.1.100",
		OwnerKind:   "Deployment",
		OwnerName:   "web-server",
		ServiceName: "api-gateway",
		ServiceIP:   "10.0.2.10",
		NodeName:    "node-7",
		Zone:        "us-east-1a",
		Region:      "us-east-1",
	}

	tapioEvent, err := EnrichWithK8sContext(observerEvent, k8sContext)
	require.NoError(t, err, "Enrichment should succeed")
	require.NotNil(t, tapioEvent)

	// Verify basic fields
	assert.Equal(t, "evt-123", tapioEvent.ID)
	assert.Equal(t, EventTypePerformance, tapioEvent.Type) // "rtt_" maps to performance
	assert.Equal(t, "performance_degradation", tapioEvent.Subtype)
	assert.Equal(t, SeverityWarning, tapioEvent.Severity) // RTT degradation > 50%
	assert.Equal(t, OutcomeSuccess, tapioEvent.Outcome)   // HTTP 200 = success

	// Verify multi-cluster fields
	assert.Equal(t, "prod-us-east", tapioEvent.ClusterID)
	assert.Equal(t, "production", tapioEvent.Namespace)
	assert.Equal(t, "web-server", tapioEvent.Labels["app"])

	// Verify same data is preserved
	assert.Equal(t, observerEvent.NetworkData, tapioEvent.NetworkData)

	// Verify graph entities
	require.Len(t, tapioEvent.Entities, 4, "Should have pod, deployment, node, service entities")

	// Find entities by type
	var podEntity, deploymentEntity, nodeEntity, serviceEntity *Entity
	for i := range tapioEvent.Entities {
		switch tapioEvent.Entities[i].Type {
		case EntityTypePod:
			podEntity = &tapioEvent.Entities[i]
		case EntityTypeDeployment:
			deploymentEntity = &tapioEvent.Entities[i]
		case EntityTypeNode:
			nodeEntity = &tapioEvent.Entities[i]
		case EntityTypeService:
			serviceEntity = &tapioEvent.Entities[i]
		}
	}

	// Verify pod entity
	require.NotNil(t, podEntity)
	assert.Equal(t, "production/web-server-abc", podEntity.ID)
	assert.Equal(t, "web-server-abc", podEntity.Name)
	assert.Equal(t, "prod-us-east", podEntity.ClusterID)
	assert.Equal(t, "production", podEntity.Namespace)

	// Verify deployment entity
	require.NotNil(t, deploymentEntity)
	assert.Equal(t, "production/web-server", deploymentEntity.ID)
	assert.Equal(t, "web-server", deploymentEntity.Name)

	// Verify node entity
	require.NotNil(t, nodeEntity)
	assert.Equal(t, "node-7", nodeEntity.ID)
	assert.Equal(t, "us-east-1a", nodeEntity.Attributes["zone"])
	assert.Equal(t, "us-east-1", nodeEntity.Attributes["region"])

	// Verify service entity
	require.NotNil(t, serviceEntity)
	assert.Equal(t, "production/api-gateway", serviceEntity.ID)
	assert.Equal(t, "api-gateway", serviceEntity.Name)

	// Verify relationships
	require.Len(t, tapioEvent.Relationships, 3, "Should have 3 relationships")

	// Find relationships by type
	var managesRel, containsRel, connectsRel *Relationship
	for i := range tapioEvent.Relationships {
		switch tapioEvent.Relationships[i].Type {
		case RelationshipManages:
			managesRel = &tapioEvent.Relationships[i]
		case RelationshipContains:
			containsRel = &tapioEvent.Relationships[i]
		case RelationshipConnectsTo:
			connectsRel = &tapioEvent.Relationships[i]
		}
	}

	// Verify Deployment → manages → Pod
	require.NotNil(t, managesRel)
	assert.Equal(t, "web-server", managesRel.Source.Name)
	assert.Equal(t, "web-server-abc", managesRel.Target.Name)

	// Verify Node → contains → Pod
	require.NotNil(t, containsRel)
	assert.Equal(t, "node-7", containsRel.Source.Name)
	assert.Equal(t, "web-server-abc", containsRel.Target.Name)

	// Verify Pod → connects_to → Service
	require.NotNil(t, connectsRel)
	assert.Equal(t, "web-server-abc", connectsRel.Source.Name)
	assert.Equal(t, "api-gateway", connectsRel.Target.Name)
	assert.Equal(t, "tcp", connectsRel.Labels["protocol"])
}

// TestEnrichWithK8sContext_KernelEvent verifies kernel event enrichment
func TestEnrichWithK8sContext_KernelEvent(t *testing.T) {
	observerEvent := &ObserverEvent{
		ID:        "evt-456",
		Type:      "oom_kill",
		Subtype:   "memory_exhausted",
		Source:    "kernel-observer",
		Timestamp: time.Now(),
		KernelData: &KernelEventData{
			OOMKilledPID:   12345,
			OOMMemoryUsage: 2147483648, // 2GB
			OOMMemoryLimit: 2147483648, // 2GB
		},
	}

	k8sContext := &K8sContext{
		ClusterID:    "prod-us-east",
		PodName:      "memory-hog-xyz",
		PodNamespace: "default",
		PodLabels:    map[string]string{"app": "memory-hog"},
		OwnerKind:    "StatefulSet",
		OwnerName:    "database",
		NodeName:     "node-3",
	}

	tapioEvent, err := EnrichWithK8sContext(observerEvent, k8sContext)
	require.NoError(t, err)

	// Verify severity and outcome
	assert.Equal(t, EventTypeKernel, tapioEvent.Type)
	assert.Equal(t, SeverityError, tapioEvent.Severity) // oom_kill is error
	assert.Equal(t, OutcomeFailure, tapioEvent.Outcome) // oom_kill is failure

	// Verify entities (no service for kernel events)
	require.Len(t, tapioEvent.Entities, 3, "Should have pod, statefulset, node")

	// Find StatefulSet entity
	var statefulSetEntity *Entity
	for i := range tapioEvent.Entities {
		if tapioEvent.Entities[i].Type == EntityTypeStatefulSet {
			statefulSetEntity = &tapioEvent.Entities[i]
			break
		}
	}
	require.NotNil(t, statefulSetEntity)
	assert.Equal(t, "database", statefulSetEntity.Name)
}

// TestEnrichWithK8sContext_HTTPError verifies HTTP error severity
func TestEnrichWithK8sContext_HTTPError(t *testing.T) {
	tests := []struct {
		name           string
		httpStatusCode int
		expectedSev    Severity
		expectedOut    Outcome
	}{
		{"2xx success", 200, SeverityInfo, OutcomeSuccess},
		{"4xx warning", 404, SeverityWarning, OutcomeFailure},
		{"5xx error", 500, SeverityError, OutcomeFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &ObserverEvent{
				Type:   "http_request",
				Source: "network-observer",
				NetworkData: &NetworkEventData{
					HTTPStatusCode: tt.httpStatusCode,
				},
			}

			ctx := &K8sContext{
				ClusterID:    "test",
				PodName:      "test-pod",
				PodNamespace: "default",
			}

			tapioEvent, err := EnrichWithK8sContext(event, ctx)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedSev, tapioEvent.Severity)
			assert.Equal(t, tt.expectedOut, tapioEvent.Outcome)
		})
	}
}

// TestEnrichWithK8sContext_NilInputs verifies error handling
func TestEnrichWithK8sContext_NilInputs(t *testing.T) {
	tests := []struct {
		name  string
		event *ObserverEvent
		ctx   *K8sContext
	}{
		{"nil event", nil, &K8sContext{}},
		{"nil context", &ObserverEvent{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EnrichWithK8sContext(tt.event, tt.ctx)
			assert.Error(t, err)
		})
	}
}

// TestMapToEventType verifies event type classification
func TestMapToEventType(t *testing.T) {
	tests := []struct {
		eventType    string
		expectedType EventType
	}{
		{"tcp_connect", EventTypeNetwork},
		{"http_request", EventTypeNetwork},
		{"dns_query", EventTypeNetwork},
		{"oom_kill", EventTypeKernel},
		{"syscall_open", EventTypeKernel},
		{"container_start", EventTypeContainer},
		{"deployment_rollout", EventTypeDeployment},
		{"pod_crash", EventTypePod},
		{"service_unreachable", EventTypeService},
		{"rtt_spike", EventTypePerformance},
		{"cpu_throttle", EventTypeResource},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			result := mapToEventType(tt.eventType)
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

// TestDetermineSeverity verifies severity classification
func TestDetermineSeverity(t *testing.T) {
	tests := []struct {
		name     string
		event    *ObserverEvent
		expected Severity
	}{
		{
			name:     "oom_kill is error",
			event:    &ObserverEvent{Type: "oom_kill"},
			expected: SeverityError,
		},
		{
			name:     "timeout is error",
			event:    &ObserverEvent{Type: "connection_timeout"},
			expected: SeverityError,
		},
		{
			name: "high RTT degradation is warning",
			event: &ObserverEvent{
				NetworkData: &NetworkEventData{RTTDegradation: 75.0},
			},
			expected: SeverityWarning,
		},
		{
			name:     "normal event is info",
			event:    &ObserverEvent{Type: "tcp_established"},
			expected: SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineSeverity(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDetermineOutcome verifies outcome classification
func TestDetermineOutcome(t *testing.T) {
	tests := []struct {
		name     string
		event    *ObserverEvent
		expected Outcome
	}{
		{
			name:     "failed is failure",
			event:    &ObserverEvent{Type: "deployment_failed"},
			expected: OutcomeFailure,
		},
		{
			name:     "established is success",
			event:    &ObserverEvent{Type: "connection_established"},
			expected: OutcomeSuccess,
		},
		{
			name:     "unknown event is unknown",
			event:    &ObserverEvent{Type: "some_random_event"},
			expected: OutcomeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineOutcome(tt.event)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractEntities_MinimalContext verifies entity extraction with minimal K8s context
func TestExtractEntities_MinimalContext(t *testing.T) {
	event := &ObserverEvent{Type: "tcp_connect"}
	ctx := &K8sContext{
		ClusterID:    "test",
		PodName:      "test-pod",
		PodNamespace: "default",
	}

	entities := extractEntities(event, ctx)

	// Should only have pod entity (no owner, no node, no service)
	require.Len(t, entities, 1)
	assert.Equal(t, EntityTypePod, entities[0].Type)
	assert.Equal(t, "test-pod", entities[0].Name)
}

// TestExtractRelationships_NoEntities verifies relationship extraction with missing entities
func TestExtractRelationships_NoEntities(t *testing.T) {
	event := &ObserverEvent{Type: "tcp_connect"}
	ctx := &K8sContext{}
	entities := []Entity{} // No entities

	relationships := extractRelationships(event, ctx, entities)

	// Should have no relationships
	assert.Len(t, relationships, 0)
}

// RED: Test mapToEventType with additional patterns
func TestMapToEventType_AdditionalPatterns(t *testing.T) {
	tests := []struct {
		eventType    string
		expectedType EventType
	}{
		// Performance patterns
		{"performance_degradation", EventTypePerformance},
		{"latency_spike", EventTypePerformance},
		{"degradation_detected", EventTypePerformance},
		// Resource patterns
		{"resource_exhaustion", EventTypeResource},
		{"memory_pressure", EventTypeResource},
		// Volume patterns
		{"volume_mount_failed", EventTypeVolume},
		{"pvc_pending", EventTypeVolume},
		// Config patterns
		{"config_update", EventTypeConfig},
		{"configmap_changed", EventTypeConfig},
		{"secret_rotation", EventTypeConfig},
		// Health patterns
		{"health_check_failed", EventTypeHealth},
		// Network patterns (UDP specifically)
		{"udp_packet_loss", EventTypeNetwork},
		// Default fallback
		{"unknown_event", EventTypeNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			result := mapToEventType(tt.eventType)
			assert.Equal(t, tt.expectedType, result, "Event type %s should map to %s", tt.eventType, tt.expectedType)
		})
	}
}

// RED: Test mapOwnerToEntityType with all owner kinds
func TestMapOwnerToEntityType(t *testing.T) {
	tests := []struct {
		ownerKind    string
		expectedType EntityType
	}{
		{"Deployment", EntityTypeDeployment},
		{"StatefulSet", EntityTypeStatefulSet},
		{"DaemonSet", EntityTypeDaemonSet},
		{"ReplicaSet", EntityTypePod}, // Fallback
		{"Job", EntityTypePod},        // Fallback
		{"", EntityTypePod},           // Empty fallback
	}

	for _, tt := range tests {
		t.Run(tt.ownerKind, func(t *testing.T) {
			result := mapOwnerToEntityType(tt.ownerKind)
			assert.Equal(t, tt.expectedType, result, "Owner kind %s should map to %s", tt.ownerKind, tt.expectedType)
		})
	}
}

// RED: Test EnrichWithK8sContext with DaemonSet owner
func TestEnrichWithK8sContext_DaemonSetOwner(t *testing.T) {
	observerEvent := &ObserverEvent{
		ID:        "evt-789",
		Type:      "tcp_connect",
		Source:    "network-observer",
		Timestamp: time.Now(),
	}

	k8sContext := &K8sContext{
		ClusterID:    "test-cluster",
		PodName:      "logging-agent-xyz",
		PodNamespace: "kube-system",
		OwnerKind:    "DaemonSet",
		OwnerName:    "logging-agent",
		NodeName:     "node-1",
	}

	tapioEvent, err := EnrichWithK8sContext(observerEvent, k8sContext)
	require.NoError(t, err)

	// Find DaemonSet entity
	var daemonSetEntity *Entity
	for i := range tapioEvent.Entities {
		if tapioEvent.Entities[i].Type == EntityTypeDaemonSet {
			daemonSetEntity = &tapioEvent.Entities[i]
			break
		}
	}
	require.NotNil(t, daemonSetEntity, "Should have DaemonSet entity")
	assert.Equal(t, "logging-agent", daemonSetEntity.Name)
	assert.Equal(t, EntityTypeDaemonSet, daemonSetEntity.Type)
}

// RED: Test EnrichWithK8sContext with no owner kind (fallback)
func TestEnrichWithK8sContext_NoOwner(t *testing.T) {
	observerEvent := &ObserverEvent{
		ID:        "evt-standalone",
		Type:      "tcp_connect",
		Source:    "network-observer",
		Timestamp: time.Now(),
	}

	k8sContext := &K8sContext{
		ClusterID:    "test-cluster",
		PodName:      "standalone-pod",
		PodNamespace: "default",
		// No OwnerKind or OwnerName
		NodeName: "node-1",
	}

	tapioEvent, err := EnrichWithK8sContext(observerEvent, k8sContext)
	require.NoError(t, err)

	// Should have pod and node entities, but no owner entity
	assert.Len(t, tapioEvent.Entities, 2, "Should have pod and node only (no owner)")

	for _, entity := range tapioEvent.Entities {
		assert.NotEqual(t, EntityTypeDeployment, entity.Type, "Should not have Deployment entity")
		assert.NotEqual(t, EntityTypeStatefulSet, entity.Type, "Should not have StatefulSet entity")
		assert.NotEqual(t, EntityTypeDaemonSet, entity.Type, "Should not have DaemonSet entity")
	}
}
