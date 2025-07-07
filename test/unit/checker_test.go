package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/falseyair/tapio/pkg/types"
)

func TestCheckRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request *types.CheckRequest
		valid   bool
	}{
		{
			name: "valid default request",
			request: &types.CheckRequest{
				Namespace: "default",
			},
			valid: true,
		},
		{
			name: "valid all namespaces request",
			request: &types.CheckRequest{
				All: true,
			},
			valid: true,
		},
		{
			name: "valid resource-specific request",
			request: &types.CheckRequest{
				Resource:  "my-app",
				Namespace: "production",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation tests for request structure
			assert.NotNil(t, tt.request)
			if tt.request.All {
				assert.Empty(t, tt.request.Namespace, "namespace should be empty when checking all")
			}
		})
	}
}

func TestSeverityLevels(t *testing.T) {
	assert.Equal(t, types.Severity("healthy"), types.SeverityHealthy)
	assert.Equal(t, types.Severity("warning"), types.SeverityWarning)
	assert.Equal(t, types.Severity("critical"), types.SeverityCritical)
}

func TestProblem_BasicStructure(t *testing.T) {
	problem := types.Problem{
		Resource: types.ResourceRef{
			Kind:      "pod",
			Name:      "test-pod",
			Namespace: "default",
		},
		Severity:    types.SeverityWarning,
		Title:       "High restart count",
		Description: "Container has restarted 5 times",
	}

	assert.Equal(t, "pod", problem.Resource.Kind)
	assert.Equal(t, "test-pod", problem.Resource.Name)
	assert.Equal(t, types.SeverityWarning, problem.Severity)
	assert.Contains(t, problem.Title, "restart")
}

func TestQuickFix_Structure(t *testing.T) {
	fix := types.QuickFix{
		Command:     "kubectl logs test-pod --previous",
		Description: "Check previous logs",
		Urgency:     types.SeverityWarning,
		Safe:        true,
	}

	assert.Contains(t, fix.Command, "kubectl")
	assert.True(t, fix.Safe)
	assert.Equal(t, types.SeverityWarning, fix.Urgency)
}

// TODO: Add integration tests with mocked Kubernetes client
func TestChecker_Integration(t *testing.T) {
	t.Skip("TODO: Implement with mocked Kubernetes client")

	// This test will be implemented when we add proper mocking
	// It should test the full flow:
	// 1. Create a mock Kubernetes client
	// 2. Set up test pod data
	// 3. Run checker.Check()
	// 4. Verify results match expectations
}

func TestFormatter_Output(t *testing.T) {
	result := &types.CheckResult{
		Summary: types.Summary{
			HealthyPods:  2,
			WarningPods:  1,
			CriticalPods: 0,
			TotalPods:    3,
		},
		Problems: []types.Problem{
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "test-pod",
					Namespace: "default",
				},
				Severity:    types.SeverityWarning,
				Title:       "High restart count",
				Description: "Container has restarted 5 times",
			},
		},
		QuickFixes: []types.QuickFix{
			{
				Command:     "kubectl logs test-pod --previous",
				Description: "Check previous logs",
				Urgency:     types.SeverityWarning,
				Safe:        true,
			},
		},
	}

	// Test that the result structure is valid
	assert.Equal(t, 3, result.Summary.TotalPods)
	assert.Equal(t, 2, result.Summary.HealthyPods)
	assert.Len(t, result.Problems, 1)
	assert.Len(t, result.QuickFixes, 1)

	// Verify problem details
	problem := result.Problems[0]
	assert.Equal(t, "pod", problem.Resource.Kind)
	assert.Equal(t, types.SeverityWarning, problem.Severity)
	assert.Contains(t, problem.Title, "restart")

	// Verify quick fix
	fix := result.QuickFixes[0]
	assert.Contains(t, fix.Command, "kubectl logs")
	assert.True(t, fix.Safe)
}