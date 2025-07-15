package capabilities

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestCapabilityRegistry tests the capability registry functionality
func TestCapabilityRegistry(t *testing.T) {
	// Create isolated registry for testing
	registry := NewRegistry()

	// Create mock capability
	mockCap := &MockCapability{
		name:      "test-capability",
		available: true,
		started:   false,
	}

	// Test registration
	err := registry.Register(mockCap)
	if err != nil {
		t.Fatalf("Failed to register capability: %v", err)
	}

	// Test duplicate registration fails
	err = registry.Register(mockCap)
	if err == nil {
		t.Fatal("Expected error for duplicate registration")
	}

	// Test retrieval
	retrieved, err := registry.Get("test-capability")
	if err != nil {
		t.Fatalf("Failed to retrieve capability: %v", err)
	}

	if retrieved.Name() != "test-capability" {
		t.Errorf("Expected name 'test-capability', got '%s'", retrieved.Name())
	}

	// Test retrieval of non-existent capability
	_, err = registry.Get("non-existent")
	if err == nil {
		t.Fatal("Expected error for non-existent capability")
	}

	// Test capability error
	if !IsCapabilityError(err) {
		t.Fatal("Expected CapabilityError")
	}
}

func TestCapabilityManager(t *testing.T) {
	manager := NewManager()

	// Test requesting memory monitoring
	// This should return an error on most platforms except Linux with eBPF
	_, err := manager.RequestMemoryMonitoring()
	
	// We expect this to fail on most test environments
	if err != nil {
		if !IsCapabilityError(err) {
			t.Errorf("Expected CapabilityError, got %T: %v", err, err)
		}
		
		capErr := err.(*CapabilityError)
		if capErr.Platform != runtime.GOOS {
			t.Errorf("Expected platform %s, got %s", runtime.GOOS, capErr.Platform)
		}
	}
}

func TestPlatformDetection(t *testing.T) {
	detector := NewPlatformDetector()

	// Test eBPF detection
	supported, reason := detector.DetectEBPFSupport()
	if runtime.GOOS != "linux" {
		if supported {
			t.Error("eBPF should not be supported on non-Linux platforms")
		}
		if reason == "" {
			t.Error("Expected reason for eBPF not being supported")
		}
	}

	// Test journald detection
	supported, reason = detector.DetectJournaldSupport()
	if runtime.GOOS != "linux" {
		if supported {
			t.Error("journald should not be supported on non-Linux platforms")
		}
		if reason == "" {
			t.Error("Expected reason for journald not being supported")
		}
	}

	// Test platform info
	info := GetDetailedPlatformInfo()
	if info.OS != runtime.GOOS {
		t.Errorf("Expected OS %s, got %s", runtime.GOOS, info.OS)
	}
	if info.Architecture != runtime.GOARCH {
		t.Errorf("Expected arch %s, got %s", runtime.GOARCH, info.Architecture)
	}
}

func TestCapabilityReport(t *testing.T) {
	// Get capability report
	report := GetCapabilityReport()

	if report.Platform != runtime.GOOS {
		t.Errorf("Expected platform %s, got %s", runtime.GOOS, report.Platform)
	}

	if report.Summary.Total == 0 {
		t.Error("Expected some capabilities to be registered")
	}

	// Validate summary consistency
	calculatedTotal := report.Summary.Available + report.Summary.Enabled + 
					  report.Summary.NotAvailable + report.Summary.Errors

	if calculatedTotal != report.Summary.Total {
		t.Errorf("Summary totals don't match: calculated %d, reported %d", 
			calculatedTotal, report.Summary.Total)
	}
}

func TestGracefulDegradation(t *testing.T) {
	ctx := context.Background()
	report := StartWithGracefulDegradation(ctx)

	if report.Platform != runtime.GOOS {
		t.Errorf("Expected platform %s, got %s", runtime.GOOS, report.Platform)
	}

	// On most test platforms, we expect some capabilities to be skipped
	if len(report.Skipped) == 0 && runtime.GOOS != "linux" {
		t.Error("Expected some capabilities to be skipped on non-Linux platforms")
	}

	// Verify that started capabilities can be stopped
	err := StopAll()
	if err != nil {
		t.Errorf("Failed to stop capabilities: %v", err)
	}
}

func TestCapabilityError(t *testing.T) {
	err := NewCapabilityError("test-cap", "test reason", "test-platform")

	if !IsCapabilityError(err) {
		t.Fatal("IsCapabilityError returned false for CapabilityError")
	}

	expected := "capability 'test-cap' not available on test-platform: test reason"
	if err.Error() != expected {
		t.Errorf("Expected error message '%s', got '%s'", expected, err.Error())
	}
}

// MockCapability for testing
type MockCapability struct {
	name      string
	available bool
	started   bool
	health    CapabilityStatus
}

func (m *MockCapability) Name() string {
	return m.name
}

func (m *MockCapability) Info() *CapabilityInfo {
	status := CapabilityNotAvailable
	if m.available {
		if m.started {
			status = CapabilityEnabled
		} else {
			status = CapabilityAvailable
		}
	}

	return &CapabilityInfo{
		Name:     m.name,
		Status:   status,
		Platform: runtime.GOOS,
	}
}

func (m *MockCapability) IsAvailable() bool {
	return m.available
}

func (m *MockCapability) Start(ctx context.Context) error {
	if !m.available {
		return NewCapabilityError(m.name, "not available", runtime.GOOS)
	}
	m.started = true
	return nil
}

func (m *MockCapability) Stop() error {
	m.started = false
	return nil
}

func (m *MockCapability) Health() *HealthStatus {
	status := CapabilityNotAvailable
	message := "not available"

	if m.available {
		if m.started {
			status = CapabilityEnabled
			message = "running"
		} else {
			status = CapabilityAvailable
			message = "available but not started"
		}
	}

	return &HealthStatus{
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// MockMemoryCapability for testing memory capabilities
type MockMemoryCapability struct {
	MockCapability
}

func (m *MockMemoryCapability) GetMemoryStats() ([]ProcessMemoryStats, error) {
	if !m.started {
		return nil, NewCapabilityError(m.name, "not started", runtime.GOOS)
	}
	// Return empty slice - no fake data!
	return []ProcessMemoryStats{}, nil
}

func (m *MockMemoryCapability) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	if !m.started {
		return nil, NewCapabilityError(m.name, "not started", runtime.GOOS)
	}
	// Return empty map - no fake data!
	return map[uint32]*OOMPrediction{}, nil
}

func TestMemoryCapability(t *testing.T) {
	registry := NewRegistry()
	
	mockMemCap := &MockMemoryCapability{
		MockCapability: MockCapability{
			name:      "mock-memory",
			available: true,
			started:   false,
		},
	}

	err := registry.Register(mockMemCap)
	if err != nil {
		t.Fatalf("Failed to register memory capability: %v", err)
	}

	// Test retrieval as memory capability
	memCap, err := registry.GetMemoryCapability("mock-memory")
	if err != nil {
		t.Fatalf("Failed to get memory capability: %v", err)
	}

	// Test that it returns error when not started
	_, err = memCap.GetMemoryStats()
	if err == nil {
		t.Fatal("Expected error when getting stats from non-started capability")
	}

	// Start the capability
	ctx := context.Background()
	err = memCap.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start memory capability: %v", err)
	}

	// Now it should work (but return empty data, not fake data)
	stats, err := memCap.GetMemoryStats()
	if err != nil {
		t.Fatalf("Failed to get memory stats: %v", err)
	}

	// Verify no fake data
	if len(stats) != 0 {
		t.Error("Expected empty stats, not fake data")
	}
}