package intelligence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Test NewIntelligenceService creates service
func TestNewIntelligenceService(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	require.NotNil(t, svc)
	defer func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	}()
}

// Test ProcessEvent accepts ObserverEvent
func TestIntelligenceService_ProcessEvent(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      string(domain.EventTypeNetwork),
		Subtype:   "dns_query",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	err = svc.ProcessEvent(context.Background(), event)
	assert.NoError(t, err)
}

// Test Shutdown cleans up resources
func TestIntelligenceService_Shutdown(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}

	err = svc.Shutdown(context.Background())
	assert.NoError(t, err)

	// Multiple Shutdown() calls should be safe
	err = svc.Shutdown(context.Background())
	assert.NoError(t, err)
}

// Test context cancellation
func TestIntelligenceService_ContextCancellation(t *testing.T) {
	svc, err := NewIntelligenceService("nats://localhost:4222")
	if err != nil {
		t.Skipf("Skipping test - NATS server not available: %v", err)
	}
	defer func() {
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Logf("failed to shutdown service: %v", err)
		}
	}()

	event := &domain.ObserverEvent{
		ID:        "test-789",
		Type:      string(domain.EventTypeNetwork),
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// ProcessEvent should fail fast due to cancelled context
	err = svc.ProcessEvent(ctx, event)
	assert.Error(t, err, "ProcessEvent should fail with cancelled context")
}
