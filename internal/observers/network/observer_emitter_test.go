//go:build linux

package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
)

func TestNetworkObserver_EmitterCreation(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{
			Stdout: true,
			OTEL:   true,
		},
	}

	observer, err := NewNetworkObserver("test-emitter", config)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Verify emitter was created
	assert.NotNil(t, observer.emitter, "Emitter should be created from OutputConfig")
}

func TestNetworkObserver_EmitterStdoutOnly(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{
			Stdout: true,
		},
	}

	observer, err := NewNetworkObserver("test-stdout", config)
	require.NoError(t, err)

	// Should have stdout emitter
	assert.NotNil(t, observer.emitter)
}
