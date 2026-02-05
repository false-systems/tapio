package intelligence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestDebugService_Emit_NilEvent(t *testing.T) {
	svc, err := New(Config{Tier: TierDebug})
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	err = svc.Emit(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil event")
}

func TestDebugService_Emit_Closed(t *testing.T) {
	svc, err := New(Config{Tier: TierDebug})
	require.NoError(t, err)

	// Close first
	require.NoError(t, svc.Close())

	// Emit after close should fail
	event := &domain.ObserverEvent{ID: "test", Type: "network"}
	err = svc.Emit(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestDebugService_Close_Idempotent(t *testing.T) {
	svc, err := New(Config{Tier: TierDebug})
	require.NoError(t, err)

	// Close multiple times should not panic
	err1 := svc.Close()
	err2 := svc.Close()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

func TestDebugService_Name(t *testing.T) {
	svc, err := New(Config{Tier: TierDebug})
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	assert.Equal(t, "intelligence-debug", svc.Name())
}

func TestDebugService_IsCritical(t *testing.T) {
	tests := []struct {
		name     string
		critical bool
	}{
		{"critical true", true},
		{"critical false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := New(Config{Tier: TierDebug, Critical: tt.critical})
			require.NoError(t, err)
			defer func() { require.NoError(t, svc.Close()) }()

			assert.Equal(t, tt.critical, svc.IsCritical())
		})
	}
}

func TestNew_UnknownTier(t *testing.T) {
	_, err := New(Config{Tier: "unknown"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tier")
}

func TestNew_DefaultTier(t *testing.T) {
	// Empty tier defaults to debug
	svc, err := New(Config{})
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	assert.Equal(t, "intelligence-debug", svc.Name())
}

func TestDebugService_Emit_ContextCancelled(t *testing.T) {
	svc, err := New(Config{Tier: TierDebug})
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := &domain.ObserverEvent{ID: "test", Type: "network"}
	err = svc.Emit(ctx, event)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDebugService_Emit_Success(t *testing.T) {
	svc, err := New(Config{Tier: TierDebug})
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	event := &domain.ObserverEvent{
		ID:      "test-123",
		Type:    "network",
		Subtype: "connection",
		NetworkData: &domain.NetworkEventData{
			Protocol: "tcp",
			SrcIP:    "10.0.0.1",
			DstIP:    "10.0.0.2",
		},
	}

	err = svc.Emit(context.Background(), event)
	assert.NoError(t, err)
}
