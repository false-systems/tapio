//go:build linux
// +build linux

package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadBPF_ValidPath verifies BPF loading with valid object file
func TestLoadBPF_ValidPath(t *testing.T) {
	// Create temporary valid BPF object for testing
	// For now, we skip if BPF object doesn't exist
	bpfPath := "testdata/container_monitor.o"

	spec, err := loadBPFSpec(bpfPath)
	if err != nil {
		t.Skipf("BPF object not available: %v", err)
	}

	require.NotNil(t, spec, "BPF spec should be loaded")
	assert.NotEmpty(t, spec.Programs, "BPF spec should have programs")
}

// TestLoadBPF_InvalidPath verifies error handling for missing file
func TestLoadBPF_InvalidPath(t *testing.T) {
	spec, err := loadBPFSpec("/nonexistent/path.o")
	require.Error(t, err, "Should fail with invalid path")
	assert.Nil(t, spec, "Spec should be nil on error")
	assert.Contains(t, err.Error(), "failed to load BPF spec")
}

// TestLoadBPF_EmptyPath verifies error handling for empty path
func TestLoadBPF_EmptyPath(t *testing.T) {
	spec, err := loadBPFSpec("")
	require.Error(t, err, "Should fail with empty path")
	assert.Nil(t, spec, "Spec should be nil on error")
}

// TestNewCollectionSpec_FromSpec verifies collection creation
func TestNewCollectionSpec_FromSpec(t *testing.T) {
	bpfPath := "testdata/container_monitor.o"

	spec, err := loadBPFSpec(bpfPath)
	if err != nil {
		t.Skipf("BPF object not available: %v", err)
	}

	require.NotNil(t, spec, "Spec should be loaded")

	// Verify spec has expected structure
	assert.NotNil(t, spec.Programs, "Spec should have Programs map")
	assert.NotNil(t, spec.Maps, "Spec should have Maps map")
}
