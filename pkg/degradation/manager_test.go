package degradation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManager_RegisterFeature(t *testing.T) {
	manager := NewManager()

	manager.RegisterFeature(
		"test-feature",
		"Test feature",
		[]string{},
		func() error { return nil },
		func() error { return nil },
	)

	status := manager.GetFeatureStatus()
	if len(status) != 1 {
		t.Errorf("Expected 1 feature, got %d", len(status))
	}

	feature, exists := status["test-feature"]
	if !exists {
		t.Error("Expected test-feature to exist")
	}
	if feature.State != "enabled" {
		t.Errorf("Expected enabled state, got %s", feature.State)
	}
}

func TestManager_ExecuteWithDegradation(t *testing.T) {
	manager := NewManager()

	// Register feature with healthy check
	manager.RegisterFeature(
		"healthy-feature",
		"Healthy feature",
		[]string{},
		func() error { return nil },
		func() error { return errors.New("fallback used") },
	)

	// Should execute primary function
	err := manager.ExecuteWithDegradation(context.Background(), "healthy-feature", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error for healthy feature, got: %v", err)
	}

	// Register feature with failing check
	manager.RegisterFeature(
		"failing-feature",
		"Failing feature",
		[]string{},
		func() error { return errors.New("health check failed") },
		func() error { return nil }, // Fallback succeeds
	)

	// Force health check update
	manager.updateFeatureHealth("failing-feature")
	time.Sleep(10 * time.Millisecond) // Small delay for state change

	// Should use fallback
	err = manager.ExecuteWithDegradation(context.Background(), "failing-feature", func() error {
		return errors.New("primary function should not be called")
	})
	if err != nil {
		t.Errorf("Expected fallback to succeed, got: %v", err)
	}
}

func TestManager_FeatureStates(t *testing.T) {
	manager := NewManager()

	stateChanges := make([]FeatureState, 0)
	manager.RegisterCallback(func(feature string, oldState, newState FeatureState) {
		stateChanges = append(stateChanges, newState)
	})

	manager.RegisterFeature(
		"test-feature",
		"Test feature",
		[]string{},
		func() error { return errors.New("failing") },
		func() error { return nil },
	)

	// Trigger enough errors to change state
	for i := 0; i < 5; i++ {
		manager.recordError("test-feature")
	}

	if len(stateChanges) == 0 {
		t.Error("Expected state change callback to be called")
	}

	status := manager.GetFeatureStatus()["test-feature"]
	if status.State == "enabled" {
		t.Error("Expected feature to be degraded or disabled after errors")
	}
}
