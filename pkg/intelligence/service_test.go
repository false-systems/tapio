package intelligence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test NewIntelligenceService creates service
func TestNewIntelligenceService(t *testing.T) {
	svc := NewIntelligenceService()
	require.NotNil(t, svc)
}

// RED: Test ProcessEvent accepts ObserverEvent
func TestIntelligenceService_ProcessEvent(t *testing.T) {
	svc := NewIntelligenceService()

	event := &domain.ObserverEvent{
		Type:    string(domain.EventTypeNetwork),
		Subtype: "dns_query",
	}

	err := svc.ProcessEvent(context.Background(), event)
	assert.NoError(t, err)
}

// RED: Test Shutdown cleans up resources
func TestIntelligenceService_Shutdown(t *testing.T) {
	svc := NewIntelligenceService()

	err := svc.Shutdown(context.Background())
	assert.NoError(t, err)
}
