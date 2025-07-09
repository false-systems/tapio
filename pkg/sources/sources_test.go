package sources

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/falseyair/tapio/pkg/collectors"
)

func TestDataSource_Interface(t *testing.T) {
	// Test that all sources implement the DataSource interface
	var sources []DataSource

	sources = append(sources, NewMockSource())
	sources = append(sources, NewEBPFSource())

	// K8s source requires cluster connection, test separately

	for _, source := range sources {
		t.Run(source.Name(), func(t *testing.T) {
			// Test basic interface methods
			if source.Name() == "" {
				t.Error("Name() should return non-empty string")
			}

			ctx := context.Background()

			// Test availability check
			available := source.IsAvailable(ctx)
			t.Logf("Source %s availability: %v", source.Name(), available)

			if available {
				// Test start/stop lifecycle
				if err := source.Start(ctx); err != nil {
					t.Errorf("Start() failed: %v", err)
				}

				// Test target support
				testTarget := Target{
					Type: "pod",
					Name: "test-pod",
				}

				if !source.SupportsTarget(testTarget) {
					t.Logf("Source %s doesn't support pod targets", source.Name())
				}

				// Test data collection
				targets := []Target{testTarget}
				dataset, err := source.Collect(ctx, targets)
				if err != nil {
					t.Errorf("Collect() failed: %v", err)
				}

				if dataset.Source != source.Name() {
					t.Errorf("Dataset source mismatch: got %s, want %s", dataset.Source, source.Name())
				}

				// Test stop
				if err := source.Stop(ctx); err != nil {
					t.Errorf("Stop() failed: %v", err)
				}
			}
		})
	}
}

func TestMockSource(t *testing.T) {
	source := NewMockSource()
	ctx := context.Background()

	// Test availability
	if !source.IsAvailable(ctx) {
		t.Error("Mock source should always be available")
	}

	// Test start
	if err := source.Start(ctx); err != nil {
		t.Fatalf("Failed to start mock source: %v", err)
	}
	defer source.Stop(ctx)

	// Test target creation
	target := source.CreateTestTarget("pod", "test-pod")
	if target.Type != "pod" || target.Name != "test-pod" {
		t.Errorf("CreateTestTarget failed: got %+v", target)
	}

	// Test multiple targets
	targets := source.CreateTestTargets(4)
	if len(targets) != 4 {
		t.Errorf("CreateTestTargets failed: got %d targets, want 4", len(targets))
	}

	// Test data collection
	dataset, err := source.Collect(ctx, targets)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(dataset.Metrics) == 0 {
		t.Error("No metrics collected")
	}

	if len(dataset.Events) == 0 {
		t.Error("No events collected")
	}

	// Test scenarios
	scenarios := source.GetAvailableScenarios()
	if len(scenarios) == 0 {
		t.Error("No scenarios available")
	}

	// Test scenario setting
	if err := source.SetScenario("healthy"); err != nil {
		t.Errorf("Failed to set scenario: %v", err)
	}

	// Test stress scenario
	stressDataset, err := source.SimulateStressScenario(ctx, targets)
	if err != nil {
		t.Errorf("SimulateStressScenario failed: %v", err)
	}

	if len(stressDataset.Metrics) == 0 {
		t.Error("Stress scenario should produce metrics")
	}
}

func TestEBPFSource(t *testing.T) {
	source := NewEBPFSource()
	ctx := context.Background()

	// Test platform detection
	platform := source.GetPlatformInfo()
	if platform == nil {
		t.Error("Platform info should not be nil")
	}

	if platform.OS != runtime.GOOS {
		t.Errorf("Platform OS mismatch: got %s, want %s", platform.OS, runtime.GOOS)
	}

	// Test capabilities
	caps := source.GetCapabilities(ctx)
	if caps == nil {
		t.Error("Capabilities should not be nil")
	}

	// Test availability based on platform
	available := source.IsAvailable(ctx)
	expectedAvailable := platform.SupportseBPF

	if available != expectedAvailable {
		t.Logf("eBPF availability: %v (expected: %v)", available, expectedAvailable)
	}

	// Test mock usage detection
	usingMock := source.IsUsingMock()
	expectedMock := !platform.SupportseBPF

	if usingMock != expectedMock {
		t.Errorf("Mock usage detection failed: got %v, want %v", usingMock, expectedMock)
	}

	// Test start/stop
	if err := source.Start(ctx); err != nil {
		t.Fatalf("Failed to start eBPF source: %v", err)
	}
	defer source.Stop(ctx)

	// Test target support
	targets := []Target{
		{Type: "pod", Name: "test-pod"},
		{Type: "container", Name: "test-container"},
		{Type: "process", Name: "test-process", PID: 1234},
		{Type: "service", Name: "test-service", PID: 5678},
		{Type: "unsupported", Name: "test-unsupported"},
	}

	for _, target := range targets {
		supported := source.SupportsTarget(target)

		switch target.Type {
		case "pod", "container", "process":
			if !supported {
				t.Errorf("Should support %s targets", target.Type)
			}
		case "service":
			if target.PID > 0 && !supported {
				t.Errorf("Should support service targets with PID")
			}
		case "unsupported":
			if supported {
				t.Errorf("Should not support unsupported targets")
			}
		}
	}

	// Test data collection
	supportedTargets := targets[:3] // pod, container, process
	dataset, err := source.Collect(ctx, supportedTargets)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(dataset.Metrics) == 0 {
		t.Error("No metrics collected")
	}

	// Test mock scenarios if using mock
	if usingMock {
		scenarios := source.GetAvailableMockScenarios()
		if len(scenarios) == 0 {
			t.Error("No mock scenarios available")
		}

		if err := source.SetMockScenario("healthy"); err != nil {
			t.Errorf("Failed to set mock scenario: %v", err)
		}
	}
}

func TestK8sSource(t *testing.T) {
	source := NewK8sSource()
	ctx := context.Background()

	// Test availability (might fail if no cluster)
	available := source.IsAvailable(ctx)
	t.Logf("K8s source availability: %v", available)

	if !available {
		t.Skip("Kubernetes cluster not available, skipping K8s source tests")
	}

	// Test start
	if err := source.Start(ctx); err != nil {
		t.Fatalf("Failed to start K8s source: %v", err)
	}
	defer source.Stop(ctx)

	// Test target support
	targets := []Target{
		{Type: "pod", Name: "test-pod", Namespace: "default"},
		{Type: "service", Name: "test-service", Namespace: "default"},
		{Type: "namespace", Name: "default"},
		{Type: "container", Name: "test-container", Namespace: "default"},
		{Type: "unsupported", Name: "test-unsupported"},
	}

	for _, target := range targets {
		supported := source.SupportsTarget(target)

		switch target.Type {
		case "pod", "service", "namespace":
			if !supported {
				t.Errorf("Should support %s targets", target.Type)
			}
		case "container":
			if target.Namespace != "" && !supported {
				t.Errorf("Should support container targets with namespace")
			}
		case "unsupported":
			if supported {
				t.Errorf("Should not support unsupported targets")
			}
		}
	}

	// Test namespace loading
	namespaces := source.GetNamespaces()
	if len(namespaces) == 0 {
		t.Error("No namespaces loaded")
	}

	// Test capabilities
	caps := source.GetCapabilities(ctx)
	if !caps.HasKubernetes {
		t.Error("Should detect Kubernetes availability")
	}

	// Test data collection with namespace target (most likely to exist)
	namespaceTargets := []Target{
		{Type: "namespace", Name: "default"},
	}

	dataset, err := source.Collect(ctx, namespaceTargets)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(dataset.Metrics) == 0 {
		t.Error("No metrics collected for namespace")
	}
}

func TestPlatformDetection(t *testing.T) {
	platform := collectors.DetectPlatform()

	if platform == nil {
		t.Fatal("Platform detection returned nil")
	}

	if platform.OS != runtime.GOOS {
		t.Errorf("OS detection failed: got %s, want %s", platform.OS, runtime.GOOS)
	}

	if platform.Architecture != runtime.GOARCH {
		t.Errorf("Architecture detection failed: got %s, want %s", platform.Architecture, runtime.GOARCH)
	}

	// Test platform-specific flags
	switch runtime.GOOS {
	case "linux":
		if !platform.IsLinux {
			t.Error("Should detect Linux platform")
		}
		if platform.IsDarwin || platform.IsWindows {
			t.Error("Should not detect other platforms on Linux")
		}

	case "darwin":
		if !platform.IsDarwin {
			t.Error("Should detect Darwin platform")
		}
		if platform.IsLinux || platform.IsWindows {
			t.Error("Should not detect other platforms on Darwin")
		}
		if platform.SupportseBPF {
			t.Error("Darwin should not support eBPF")
		}

	case "windows":
		if !platform.IsWindows {
			t.Error("Should detect Windows platform")
		}
		if platform.IsLinux || platform.IsDarwin {
			t.Error("Should not detect other platforms on Windows")
		}
		if platform.SupportseBPF {
			t.Error("Windows should not support eBPF")
		}
	}
}

func TestCapabilityDetection(t *testing.T) {
	ctx := context.Background()
	caps := collectors.DetectCapabilities(ctx)

	if caps == nil {
		t.Fatal("Capability detection returned nil")
	}

	// Test platform-specific capabilities
	if runtime.GOOS == "linux" {
		// Linux might support eBPF depending on kernel and environment
		t.Logf("Linux kernel tracing: %v", caps.CanAccessKernelTracing)
		t.Logf("Linux network tracing: %v", caps.CanAccessNetworkTracing)
	} else {
		// Non-Linux platforms should not support eBPF
		if caps.CanAccessKernelTracing {
			t.Error("Non-Linux platforms should not support kernel tracing")
		}
		if caps.CanAccessNetworkTracing {
			t.Error("Non-Linux platforms should not support network tracing")
		}
	}

	// Process tracing should be available on Linux and Darwin
	if (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && !caps.CanAccessProcessTracing {
		t.Error("Should support process tracing on Linux/Darwin")
	}

	// File system access should always be available
	if !caps.CanAccessFileSystem {
		t.Error("Should always support file system access")
	}
}

func TestTargetCreation(t *testing.T) {
	// Test target creation with different types
	testCases := []struct {
		targetType  string
		name        string
		namespace   string
		expectedPID int
	}{
		{"pod", "test-pod", "default", 0},
		{"container", "test-container", "default", 0},
		{"process", "test-process", "", 1234},
		{"service", "test-service", "default", 0},
	}

	for _, tc := range testCases {
		target := Target{
			Type:      tc.targetType,
			Name:      tc.name,
			Namespace: tc.namespace,
			PID:       tc.expectedPID,
			Labels:    make(map[string]string),
		}

		if target.Type != tc.targetType {
			t.Errorf("Target type mismatch: got %s, want %s", target.Type, tc.targetType)
		}

		if target.Name != tc.name {
			t.Errorf("Target name mismatch: got %s, want %s", target.Name, tc.name)
		}

		if target.Namespace != tc.namespace {
			t.Errorf("Target namespace mismatch: got %s, want %s", target.Namespace, tc.namespace)
		}

		if target.PID != tc.expectedPID {
			t.Errorf("Target PID mismatch: got %d, want %d", target.PID, tc.expectedPID)
		}
	}
}

func TestDataSetCreation(t *testing.T) {
	now := time.Now()

	dataset := DataSet{
		Timestamp: now,
		Source:    "test-source",
		Metrics:   []Metric{},
		Events:    []Event{},
		Errors:    []error{},
	}

	// Add test metric
	metric := Metric{
		Name:      "test_metric",
		Value:     42.0,
		Unit:      "count",
		Timestamp: now,
		Labels:    make(map[string]string),
	}

	dataset.Metrics = append(dataset.Metrics, metric)

	// Add test event
	event := Event{
		Type:      "test_event",
		Message:   "Test event message",
		Timestamp: now,
		Severity:  "info",
		Data:      make(map[string]interface{}),
	}

	dataset.Events = append(dataset.Events, event)

	// Validate dataset
	if len(dataset.Metrics) != 1 {
		t.Errorf("Expected 1 metric, got %d", len(dataset.Metrics))
	}

	if len(dataset.Events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(dataset.Events))
	}

	if dataset.Metrics[0].Name != "test_metric" {
		t.Errorf("Metric name mismatch: got %s, want test_metric", dataset.Metrics[0].Name)
	}

	if dataset.Events[0].Type != "test_event" {
		t.Errorf("Event type mismatch: got %s, want test_event", dataset.Events[0].Type)
	}
}

func BenchmarkMockDataCollection(b *testing.B) {
	source := NewMockSource()
	ctx := context.Background()

	if err := source.Start(ctx); err != nil {
		b.Fatalf("Failed to start mock source: %v", err)
	}
	defer source.Stop(ctx)

	targets := source.CreateTestTargets(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := source.Collect(ctx, targets)
		if err != nil {
			b.Fatalf("Collect failed: %v", err)
		}
	}
}

func BenchmarkEBPFDataCollection(b *testing.B) {
	source := NewEBPFSource()
	ctx := context.Background()

	if !source.IsAvailable(ctx) {
		b.Skip("eBPF source not available")
	}

	if err := source.Start(ctx); err != nil {
		b.Fatalf("Failed to start eBPF source: %v", err)
	}
	defer source.Stop(ctx)

	targets := []Target{
		{Type: "pod", Name: "test-pod"},
		{Type: "container", Name: "test-container"},
		{Type: "process", Name: "test-process", PID: 1234},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := source.Collect(ctx, targets)
		if err != nil {
			b.Fatalf("Collect failed: %v", err)
		}
	}
}
