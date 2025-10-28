package deployments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 1: Config validation

func TestConfig_Validate_RequiresClientset(t *testing.T) {
	config := Config{
		Namespace: "default",
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientset is required")
}

func TestConfig_Validate_Success(t *testing.T) {
	config := Config{
		Clientset: fake.NewSimpleClientset(),
		Namespace: "default",
	}

	err := config.Validate()
	assert.NoError(t, err)
}

func TestConfig_Validate_DefaultsNamespace(t *testing.T) {
	config := Config{
		Clientset: fake.NewSimpleClientset(),
		// Namespace empty - should default to all namespaces
	}

	err := config.Validate()
	assert.NoError(t, err)
}
