package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNoopClient(t *testing.T) {
	client := &noopClient{}

	// Should not error
	err := client.Send(context.Background(), collectors.RawEvent{
		Type:      "test",
		Timestamp: time.Now(),
		Data:      []byte("test data"),
		Metadata:  map[string]string{"key": "value"},
	})
	assert.NoError(t, err)

	// Close should not error
	err = client.Close()
	assert.NoError(t, err)
}

func TestNewClientWithMap(t *testing.T) {
	config := map[string]interface{}{
		"endpoint":       "localhost:0", // Valid address format
		"batch_size":     50,
		"flush_interval": "10s",
		"timeout":        "1s",
	}

	// Should create client successfully
	client, err := NewClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	defer client.Close()
}

func TestNewClientWithStruct(t *testing.T) {
	config := &ClientConfig{
		Endpoint:      "localhost:0",
		BatchSize:     100,
		FlushInterval: "5s",
		Timeout:       "30s",
	}

	// Should create client successfully
	client, err := NewClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	defer client.Close()
}

func TestNewClientInvalidConfig(t *testing.T) {
	// Invalid config type
	client, err := NewClient("invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported config type")
	assert.Nil(t, client)
}

func TestNewClientInvalidTimeout(t *testing.T) {
	config := &ClientConfig{
		Endpoint:      "localhost:0",
		BatchSize:     100,
		FlushInterval: "invalid-duration",
		Timeout:       "invalid-timeout",
	}

	// Should create client with default timeouts
	client, err := NewClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	defer client.Close()
}

func TestConfigHelpers(t *testing.T) {
	m := map[string]interface{}{
		"string_val": "hello",
		"int_val":    42,
		"float_val":  42.5,
	}

	// Test getString
	assert.Equal(t, "hello", getString(m, "string_val", "default"))
	assert.Equal(t, "default", getString(m, "missing", "default"))
	assert.Equal(t, "default", getString(m, "int_val", "default"))

	// Test getInt
	assert.Equal(t, 42, getInt(m, "int_val", 99))
	assert.Equal(t, 42, getInt(m, "float_val", 99))
	assert.Equal(t, 99, getInt(m, "missing", 99))
	assert.Equal(t, 99, getInt(m, "string_val", 99))
}
