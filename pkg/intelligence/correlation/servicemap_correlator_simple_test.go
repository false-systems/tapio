package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestServiceMapCorrelatorBasic(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar().Desugar()

	t.Run("create and process events", func(t *testing.T) {
		correlator := NewServiceMapCorrelator(logger)

		assert.NotNil(t, correlator)
		assert.Equal(t, "servicemap", correlator.Name())

		// Test basic event processing
		ctx := context.Background()
		event := &domain.UnifiedEvent{
			ID:        "test-event",
			Type:      domain.EventTypeNetwork,
			Timestamp: time.Now(),
			Attributes: map[string]interface{}{
				"src_service": "frontend",
				"dst_service": "backend",
			},
		}

		results, err := correlator.Process(ctx, event)
		require.NoError(t, err)
		assert.NotNil(t, results)
	})

	t.Run("nil event handling", func(t *testing.T) {
		correlator := NewServiceMapCorrelator(logger)
		ctx := context.Background()

		results, err := correlator.Process(ctx, nil)
		assert.Error(t, err)
		assert.Nil(t, results)
	})
}
