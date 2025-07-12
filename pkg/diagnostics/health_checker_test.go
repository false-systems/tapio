package diagnostics

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/yairfalse/tapio/pkg/ebpf"
)

func TestHealthChecker_RunHealthCheck(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create fake eBPF monitor
	ebpfMonitor := ebpf.NewMonitor(nil)

	// Create health checker
	healthChecker := NewHealthChecker(fakeClient, nil, ebpfMonitor)

	// Run health check
	report, err := healthChecker.RunHealthCheck(context.Background())
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	if report == nil {
		t.Fatal("Expected health report, got nil")
	}

	// Should have multiple components checked
	if len(report.Components) == 0 {
		t.Error("Expected at least one component to be checked")
	}

	// Should have overall health status
	if report.OverallHealth == "" {
		t.Error("Expected overall health status")
	}

	// Check for eBPF component
	if ebpfHealth, exists := report.Components["ebpf-monitoring"]; exists {
		if ebpfHealth.Healthy {
			t.Log("eBPF monitoring detected as healthy (this may vary by system)")
		} else {
			t.Log("eBPF monitoring not available (expected on non-Linux or restricted systems)")
		}
	}
}

func TestHealthChecker_GetQuickDiagnostics(t *testing.T) {
	// Create fake client
	fakeClient := fake.NewSimpleClientset()
	ebpfMonitor := ebpf.NewMonitor(nil)

	healthChecker := NewHealthChecker(fakeClient, nil, ebpfMonitor)

	// Run health check first
	_, err := healthChecker.RunHealthCheck(context.Background())
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	// Get quick diagnostics
	diagnostics := healthChecker.GetQuickDiagnostics()
	if diagnostics == nil {
		t.Fatal("Expected diagnostics, got nil")
	}

	components, exists := diagnostics["components"]
	if !exists {
		t.Error("Expected components in diagnostics")
	}

	if componentsMap, ok := components.(map[string]bool); ok {
		if len(componentsMap) == 0 {
			t.Error("Expected at least one component in diagnostics")
		}
	} else {
		t.Error("Expected components to be map[string]bool")
	}
}
