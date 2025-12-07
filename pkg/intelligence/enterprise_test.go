package intelligence

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yairfalse/tapio/pkg/domain"
)

// mockContextLookup implements ContextLookup for testing
type mockContextLookup struct {
	podsByIP map[string]*domain.K8sContext
}

func (m *mockContextLookup) GetContextByIP(ip string) (*domain.K8sContext, error) {
	if ctx, ok := m.podsByIP[ip]; ok {
		return ctx, nil
	}
	return nil, nil
}

// startTestNATS starts an embedded NATS server for testing.
func startTestNATS(t *testing.T) *server.Server {
	t.Helper()
	opts := &server.Options{
		Host: "127.0.0.1",
		Port: -1, // Random available port
	}
	ns, err := server.NewServer(opts)
	require.NoError(t, err)
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	return ns
}

func TestEnterpriseService_ProcessEvent(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	// Subscribe to verify TapioEvent arrives (not ObserverEvent)
	received := make(chan *domain.TapioEvent, 1)
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	defer nc.Close()

	_, err = nc.Subscribe("tapio.events.>", func(m *nats.Msg) {
		var event domain.TapioEvent
		if err := json.Unmarshal(m.Data, &event); err == nil {
			received <- &event
		}
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())

	// Mock K8s context lookup
	ctxLookup := &mockContextLookup{
		podsByIP: map[string]*domain.K8sContext{
			"10.0.1.42": {
				ClusterID:    "cluster-1",
				PodName:      "nginx-abc123",
				PodNamespace: "production",
				OwnerKind:    "Deployment",
				OwnerName:    "nginx",
				NodeName:     "node-1",
			},
		},
	}

	// Create enterprise service
	svc, err := NewEnterpriseService(ns.ClientURL(), ctxLookup)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	})

	// Process event
	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "network",
		Subtype: "connection_established",
		Source:  "network-observer",
		NetworkData: &domain.NetworkEventData{
			SrcIP: "10.0.1.42",
			DstIP: "10.0.2.100",
		},
	}
	err = svc.ProcessEvent(context.Background(), event)
	require.NoError(t, err)

	// Verify TapioEvent received with graph entities
	select {
	case tapioEvent := <-received:
		assert.Equal(t, "test-123", tapioEvent.ID)
		assert.Equal(t, domain.EventTypeNetwork, tapioEvent.Type)
		assert.Equal(t, "connection_established", tapioEvent.Subtype)
		assert.Equal(t, "cluster-1", tapioEvent.ClusterID)
		assert.Equal(t, "production", tapioEvent.Namespace)

		// Verify entities exist
		assert.NotEmpty(t, tapioEvent.Entities)
		// Should have pod entity
		var hasPod bool
		for _, e := range tapioEvent.Entities {
			if e.Type == domain.EntityTypePod && e.Name == "nginx-abc123" {
				hasPod = true
			}
		}
		assert.True(t, hasPod, "should have pod entity")

	case <-time.After(2 * time.Second):
		t.Fatal("TapioEvent not received")
	}
}

func TestEnterpriseService_NoContextFound(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	// Empty context lookup - no pods known
	ctxLookup := &mockContextLookup{
		podsByIP: map[string]*domain.K8sContext{},
	}

	svc, err := NewEnterpriseService(ns.ClientURL(), ctxLookup)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	})

	// Event with unknown IP
	event := &domain.ObserverEvent{
		ID:   "test-456",
		Type: "network",
		NetworkData: &domain.NetworkEventData{
			SrcIP: "10.0.99.99", // Unknown IP
		},
	}

	// Should return error since we can't enrich without context
	err = svc.ProcessEvent(context.Background(), event)
	assert.Error(t, err)
}

func TestEnterpriseService_NilEvent(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	ctxLookup := &mockContextLookup{}
	svc, err := NewEnterpriseService(ns.ClientURL(), ctxLookup)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	})

	err = svc.ProcessEvent(context.Background(), nil)
	assert.Error(t, err)
}

// TestEnterpriseService_NonNetworkEvent verifies that non-network events
// fail gracefully since EnterpriseService currently only supports network events.
func TestEnterpriseService_NonNetworkEvent(t *testing.T) {
	ns := startTestNATS(t)
	defer ns.Shutdown()

	ctxLookup := &mockContextLookup{}
	svc, err := NewEnterpriseService(ns.ClientURL(), ctxLookup)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	})

	// Deployment event without network data - should fail
	event := &domain.ObserverEvent{
		ID:      "test-deploy-123",
		Type:    "deployment",
		Subtype: "rollout_stuck",
		Source:  "deployments-observer",
		// No NetworkData - can't extract source IP
	}

	err = svc.ProcessEvent(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no source IP")
}
