package runtime_signals

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, 10000, cfg.BufferSize)
		assert.True(t, cfg.EnableEBPF)
	})

	t.Run("ValidConfig", func(t *testing.T) {
		cfg := Config{
			BufferSize: 5000,
			EnableEBPF: false,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("InvalidBufferSizeZero", func(t *testing.T) {
		cfg := Config{
			BufferSize: 0,
			EnableEBPF: true,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer size must be greater than 0")
	})

	t.Run("InvalidBufferSizeNegative", func(t *testing.T) {
		cfg := Config{
			BufferSize: -100,
			EnableEBPF: true,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer size must be greater than 0")
	})

	t.Run("InvalidBufferSizeTooLarge", func(t *testing.T) {
		cfg := Config{
			BufferSize: 1000001,
			EnableEBPF: true,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer size must not exceed 1,000,000")
	})

	t.Run("BoundaryBufferSizeMin", func(t *testing.T) {
		cfg := Config{
			BufferSize: 1,
			EnableEBPF: true,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("BoundaryBufferSizeMax", func(t *testing.T) {
		cfg := Config{
			BufferSize: 1000000,
			EnableEBPF: true,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})
}
