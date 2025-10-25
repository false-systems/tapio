package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseSchedulingFailure_StandardFormat verifies parsing standard scheduler messages
func TestParseSchedulingFailure_StandardFormat(t *testing.T) {
	message := "0/5 nodes are available: 2 Insufficient cpu, 3 node(s) had taints that the pod didn't tolerate."

	failure := parseSchedulingFailure(message)

	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 5, failure.NodesTotal)
	assert.Equal(t, 2, len(failure.Reasons))
	assert.Equal(t, 2, failure.Reasons["Insufficient cpu"])
	assert.Equal(t, 3, failure.Reasons["node(s) had taints that the pod didn't tolerate."])
}

// TestParseSchedulingFailure_InsufficientMemory verifies memory failure parsing
func TestParseSchedulingFailure_InsufficientMemory(t *testing.T) {
	message := "0/3 nodes are available: 3 Insufficient memory."

	failure := parseSchedulingFailure(message)

	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 3, failure.NodesTotal)
	assert.Equal(t, 1, len(failure.Reasons))
	assert.Equal(t, 3, failure.Reasons["Insufficient memory."])
}

// TestParseSchedulingFailure_PodAffinity verifies affinity constraint parsing
func TestParseSchedulingFailure_PodAffinity(t *testing.T) {
	message := "0/4 nodes are available: 4 node(s) didn't match pod affinity rules."

	failure := parseSchedulingFailure(message)

	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 4, failure.NodesTotal)
	assert.Equal(t, 1, len(failure.Reasons))
	assert.Equal(t, 4, failure.Reasons["node(s) didn't match pod affinity rules."])
}

// TestParseSchedulingFailure_MultipleReasons verifies multiple failure reasons
func TestParseSchedulingFailure_MultipleReasons(t *testing.T) {
	message := "0/10 nodes are available: 2 Insufficient cpu, 3 Insufficient memory, 5 node(s) had taints."

	failure := parseSchedulingFailure(message)

	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 10, failure.NodesTotal)
	assert.Equal(t, 3, len(failure.Reasons))
	assert.Equal(t, 2, failure.Reasons["Insufficient cpu"])
	assert.Equal(t, 3, failure.Reasons["Insufficient memory"])
	assert.Equal(t, 5, failure.Reasons["node(s) had taints."])
}

// TestParseSchedulingFailure_EmptyMessage verifies empty message handling
func TestParseSchedulingFailure_EmptyMessage(t *testing.T) {
	failure := parseSchedulingFailure("")

	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 0, failure.NodesTotal)
	assert.Equal(t, 0, len(failure.Reasons))
}

// TestParseSchedulingFailure_MalformedMessage verifies malformed message handling
func TestParseSchedulingFailure_MalformedMessage(t *testing.T) {
	failure := parseSchedulingFailure("random text without numbers or structure")

	assert.Equal(t, 0, failure.NodesFailed)
	assert.Equal(t, 0, failure.NodesTotal)
	assert.Equal(t, 0, len(failure.Reasons))
}

// TestGenerateEventID verifies event ID generation
func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()
	id2 := generateEventID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 36) // UUID format
}
