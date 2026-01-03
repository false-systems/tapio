package intelligence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"github.com/yairfalse/tapio/pkg/publisher"
)

// TestPolkuService_WithObserverDeps demonstrates how to wire PolkuService
// as the emitter for all observers via base.Deps.
func TestPolkuService_WithObserverDeps(t *testing.T) {
	// Create PolkuService (won't connect without real server)
	polkuSvc, err := intelligence.NewPolkuService(intelligence.PolkuConfig{
		Publisher: publisher.Config{
			Address:   "localhost:50051",
			ClusterID: "test-cluster",
			NodeName:  "test-node",
		},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, polkuSvc.Close()) }()

	// Create deps with PolkuService as emitter
	deps := base.NewDeps(nil, polkuSvc)

	// Verify deps has the polku emitter
	assert.Equal(t, "intelligence-polku", deps.Emitter.Name())
	assert.False(t, deps.Emitter.IsCritical())

	// Emit an event (will buffer since not connected)
	event := &domain.ObserverEvent{
		ID:        "test-event-1",
		Type:      "network",
		Subtype:   "connection_established",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			Protocol: "tcp",
			SrcIP:    "10.0.0.1",
			DstIP:    "10.0.0.2",
			SrcPort:  45678,
			DstPort:  80,
		},
	}

	// Emit returns nil even without connection (buffers event)
	err = deps.Emitter.Emit(context.Background(), event)
	assert.NoError(t, err)
}

// TestPolkuService_CriticalMode shows critical mode configuration.
func TestPolkuService_CriticalMode(t *testing.T) {
	polkuSvc, err := intelligence.NewPolkuService(intelligence.PolkuConfig{
		Publisher: publisher.Config{
			Address:   "localhost:50051",
			ClusterID: "prod",
			NodeName:  "node-1",
		},
		Critical: true, // Failures will block event processing
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, polkuSvc.Close()) }()

	assert.True(t, polkuSvc.IsCritical())
}

// TestPolkuService_MultipleEmitters shows using multiple emitters.
func TestPolkuService_MultipleEmitters(t *testing.T) {
	// Debug emitter for local logging
	debugSvc, err := intelligence.New(intelligence.Config{Tier: intelligence.TierDebug})
	require.NoError(t, err)
	defer func() { require.NoError(t, debugSvc.Close()) }()

	// Polku emitter for production streaming
	polkuSvc, err := intelligence.NewPolkuService(intelligence.PolkuConfig{
		Publisher: publisher.Config{
			Address:   "localhost:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, polkuSvc.Close()) }()

	// Both implement intelligence.Service
	emitters := []intelligence.Service{debugSvc, polkuSvc}

	event := &domain.ObserverEvent{
		ID:        "multi-emit-1",
		Type:      "container",
		Subtype:   "oom_kill",
		Source:    "container-observer",
		Timestamp: time.Now(),
	}

	// Fan-out to all emitters
	for _, emitter := range emitters {
		err := emitter.Emit(context.Background(), event)
		assert.NoError(t, err)
	}
}
