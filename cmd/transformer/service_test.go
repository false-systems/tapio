package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransformerService_WithEnv(t *testing.T) {
	os.Setenv("NATS_URL", "nats://test:4222")
	defer os.Unsetenv("NATS_URL")

	// This will fail to connect but tests the initialization
	service, err := NewTransformerService()
	
	// We expect an error because we can't connect to the test URL
	require.Error(t, err)
	assert.Nil(t, service)
	assert.Contains(t, err.Error(), "failed to connect to NATS")
}

func TestNewTransformerService_DefaultURL(t *testing.T) {
	// This will use the default localhost:4222
	service, err := NewTransformerService()
	
	// We expect an error because NATS isn't running locally
	require.Error(t, err)
	assert.Nil(t, service)
}