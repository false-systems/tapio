package neo4j

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestEventProcessor_ProcessEvent(t *testing.T) {
	// Skip if no Neo4j connection
	if testing.Short() {
		t.Skip("Skipping Neo4j integration test")
	}

	// Setup
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	client, err := NewClient(config, logger)
	if err != nil {
		t.Skip("Neo4j not available:", err)
	}
	defer client.Close(context.Background())

	// Initialize schema
	schema := NewSchemaManager(client, logger)
	err = schema.Initialize(context.Background())
	require.NoError(t, err)

	// Create processor
	processor, err := NewEventProcessor(client, logger)
	require.NoError(t, err)

	t.Run("process pod restart event", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-event-1",
			Type:      "pod_restart",
			Timestamp: time.Now(),
			Source:    "test",
			Severity:  domain.EventSeverityMedium,
			Message:   "Pod frontend-123 restarted",
			K8sContext: &domain.K8sContext{
				UID:         "pod-uid-123",
				Name:        "frontend-123",
				Namespace:   "default",
				Kind:        "Pod",
				ClusterName: "test-cluster",
				NodeName:    "node-1",
				Labels: map[string]string{
					"app": "frontend",
				},
			},
		}

		err = processor.ProcessEvent(context.Background(), event)
		assert.NoError(t, err)

		// Verify pod node was created
		session := client.Session(context.Background())
		defer session.Close(context.Background())

		result, err := session.Run(context.Background(),
			"MATCH (p:Pod {uid: $uid}) RETURN p",
			map[string]interface{}{"uid": "pod-uid-123"})
		require.NoError(t, err)

		assert.True(t, result.Next(context.Background()))
		record := result.Record()
		node, found := record.Get("p")
		assert.True(t, found)
		assert.NotNil(t, node)
	})

	t.Run("process config change event", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:        "test-event-2",
			Type:      "config_changed",
			Timestamp: time.Now(),
			Source:    "test",
			Severity:  domain.EventSeverityLow,
			Message:   "ConfigMap app-config changed",
			K8sContext: &domain.K8sContext{
				UID:         "cm-uid-456",
				Name:        "app-config",
				Namespace:   "default",
				Kind:        "ConfigMap",
				ClusterName: "test-cluster",
			},
		}

		err = processor.ProcessEvent(context.Background(), event)
		assert.NoError(t, err)
	})

	t.Run("detect causality", func(t *testing.T) {
		// First create a config change event
		configEvent := &domain.UnifiedEvent{
			ID:        "config-change-event",
			Type:      "config_changed",
			Timestamp: time.Now().Add(-5 * time.Minute),
			Source:    "test",
			K8sContext: &domain.K8sContext{
				UID:         "cm-uid-789",
				Name:        "nginx-config",
				Namespace:   "default",
				Kind:        "ConfigMap",
				ClusterName: "test-cluster",
			},
		}
		err = processor.ProcessEvent(context.Background(), configEvent)
		require.NoError(t, err)

		// Create relationship between pod and configmap
		session := client.Session(context.Background())
		defer session.Close(context.Background())

		_, err = session.Run(context.Background(), `
			MATCH (p:Pod {uid: $pod_uid})
			MATCH (cm:ConfigMap {uid: $cm_uid})
			CREATE (p)-[:MOUNTS]->(cm)
		`, map[string]interface{}{
			"pod_uid": "pod-uid-123",
			"cm_uid":  "cm-uid-789",
		})
		require.NoError(t, err)

		// Now create a pod restart event
		restartEvent := &domain.UnifiedEvent{
			ID:        "pod-restart-after-config",
			Type:      "pod_restart",
			Timestamp: time.Now(),
			Source:    "test",
			K8sContext: &domain.K8sContext{
				UID:         "pod-uid-123",
				Name:        "frontend-123",
				Namespace:   "default",
				Kind:        "Pod",
				ClusterName: "test-cluster",
			},
		}
		err = processor.ProcessEvent(context.Background(), restartEvent)
		require.NoError(t, err)

		// Verify causality was detected
		result, err := session.Run(context.Background(), `
			MATCH (e1:Event)-[:TRIGGERED]->(e2:Event)
			WHERE e1.id = $config_event_id AND e2.id = $restart_event_id
			RETURN e1, e2
		`, map[string]interface{}{
			"config_event_id":  "config-change-event",
			"restart_event_id": "pod-restart-after-config",
		})
		require.NoError(t, err)

		assert.True(t, result.Next(context.Background()), "Expected causality relationship to be created")
	})

	// Cleanup
	t.Cleanup(func() {
		schema.DropAll(context.Background())
	})
}

func TestEventProcessor_ResourceTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Neo4j integration test")
	}

	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	client, err := NewClient(config, logger)
	if err != nil {
		t.Skip("Neo4j not available:", err)
	}
	defer client.Close(context.Background())

	processor, err := NewEventProcessor(client, logger)
	require.NoError(t, err)

	testCases := []struct {
		name      string
		eventType domain.EventType
		kind      string
		verify    func(t *testing.T, client *Client, uid string)
	}{
		{
			name:      "service event",
			eventType: "service_unavailable",
			kind:      "Service",
			verify: func(t *testing.T, client *Client, uid string) {
				session := client.Session(context.Background())
				defer session.Close(context.Background())

				result, err := session.Run(context.Background(),
					"MATCH (s:Service {uid: $uid}) RETURN s",
					map[string]interface{}{"uid": uid})
				require.NoError(t, err)
				assert.True(t, result.Next(context.Background()))
			},
		},
		{
			name:      "deployment event",
			eventType: "deployment_rollout",
			kind:      "Deployment",
			verify: func(t *testing.T, client *Client, uid string) {
				session := client.Session(context.Background())
				defer session.Close(context.Background())

				result, err := session.Run(context.Background(),
					"MATCH (d:Deployment {uid: $uid}) RETURN d",
					map[string]interface{}{"uid": uid})
				require.NoError(t, err)
				assert.True(t, result.Next(context.Background()))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uid := "test-" + tc.kind + "-uid"
			event := &domain.UnifiedEvent{
				ID:        "event-" + tc.name,
				Type:      tc.eventType,
				Timestamp: time.Now(),
				Source:    "test",
				K8sContext: &domain.K8sContext{
					UID:         uid,
					Name:        "test-" + tc.kind,
					Namespace:   "default",
					Kind:        tc.kind,
					ClusterName: "test-cluster",
				},
			}

			err = processor.ProcessEvent(context.Background(), event)
			assert.NoError(t, err)

			tc.verify(t, client, uid)
		})
	}
}
