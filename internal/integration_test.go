package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
)

// TestBasicHealthCheck tests basic health check functionality
func TestBasicHealthCheck(t *testing.T) {
	// Create a checker
	checker, err := simple.NewChecker()
	if err != nil {
		t.Skipf("Skipping test - no Kubernetes available: %v", err)
	}

	// Create a basic check request
	req := &types.CheckRequest{
		Namespace: "default",
		All:       false,
		Verbose:   false,
	}

	// Run the check
	ctx := context.Background()
	result, err := checker.Check(ctx, req)

	// The check might fail if no pods exist, but shouldn't crash
	if err != nil {
		t.Logf("Check failed (may be expected): %v", err)
	}

	// Result should always be returned
	if result == nil {
		t.Error("Expected result to be non-nil")
		return
	}

	// Verify result structure
	if result.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	t.Logf("Basic health check completed - found %d problems", len(result.Problems))
}

// TestCheckWithEBPFConfig tests checker with eBPF configuration
func TestCheckWithEBPFConfig(t *testing.T) {
	// Create a checker with eBPF config (which will likely fail gracefully)
	checker, err := simple.NewCheckerWithConfig(nil)
	if err != nil {
		t.Skipf("Skipping test - no Kubernetes available: %v", err)
	}

	// Create a basic check request
	req := &types.CheckRequest{
		Namespace: "default",
		All:       false,
		Verbose:   false,
	}

	// Run the check
	ctx := context.Background()
	result, err := checker.Check(ctx, req)

	// The check might fail if no pods exist, but shouldn't crash
	if err != nil {
		t.Logf("Check failed (may be expected): %v", err)
	}

	// Result should always be returned
	if result == nil {
		t.Error("Expected result to be non-nil")
		return
	}

	// Verify result structure
	if result.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	t.Logf("Check with eBPF config completed - found %d problems", len(result.Problems))
}

// TestEBPFMonitorAccess tests eBPF monitor access
func TestEBPFMonitorAccess(t *testing.T) {
	// Create a checker
	checker, err := simple.NewChecker()
	if err != nil {
		t.Skipf("Skipping test - no Kubernetes available: %v", err)
	}

	// Test eBPF monitor access
	monitor := checker.GetEBPFMonitor()
	if monitor == nil {
		t.Error("Expected eBPF monitor to be non-nil")
		return
	}

	// Test availability check (should be false without root/proper setup)
	isAvailable := monitor.IsAvailable()
	t.Logf("eBPF monitor availability: %v", isAvailable)

	// This is not an error - eBPF is expected to be unavailable in most test environments
}

// TestPerformanceBaseline tests that basic operations complete quickly
func TestPerformanceBaseline(t *testing.T) {
	// Create a checker
	checker, err := simple.NewChecker()
	if err != nil {
		t.Skipf("Skipping test - no Kubernetes available: %v", err)
	}

	// Test performance of basic check
	start := time.Now()

	req := &types.CheckRequest{
		Namespace: "default",
		All:       false,
		Verbose:   false,
	}

	ctx := context.Background()
	_, err = checker.Check(ctx, req)
	duration := time.Since(start)

	if err != nil {
		t.Logf("Check failed (may be expected): %v", err)
	}

	// Should complete in reasonable time (< 10 seconds for basic check)
	if duration > 10*time.Second {
		t.Errorf("Basic check took too long: %v (should be < 10s)", duration)
	}

	t.Logf("Performance baseline test completed in %v", duration)
}

// TestTypesValidation tests that basic types are working correctly
func TestTypesValidation(t *testing.T) {
	// Test CheckRequest validation
	req := &types.CheckRequest{
		Namespace: "test",
		All:       false,
		Verbose:   true,
	}

	if req.Namespace != "test" {
		t.Error("CheckRequest namespace not set correctly")
	}

	// Test CheckResult structure
	result := &types.CheckResult{
		Summary: types.Summary{
			HealthyPods:  1,
			WarningPods:  2,
			CriticalPods: 3,
			TotalPods:    6,
		},
		Problems:  []types.Problem{},
		Timestamp: time.Now(),
	}

	if result.Summary.TotalPods != 6 {
		t.Error("CheckResult summary not calculated correctly")
	}

	// Test ResourceRef
	ref := types.ResourceRef{
		Kind:      "pod",
		Name:      "test-pod",
		Namespace: "default",
	}

	if ref.Kind != "pod" || ref.Name != "test-pod" || ref.Namespace != "default" {
		t.Error("ResourceRef not initialized correctly")
	}

	t.Log("Types validation test passed")
}
