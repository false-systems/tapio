package degradation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FeatureState represents the current state of a feature
type FeatureState int

const (
	FeatureEnabled FeatureState = iota
	FeatureDegraded
	FeatureDisabled
)

// Feature represents a degradable feature in the system
type Feature struct {
	Name           string
	Description    string
	State          FeatureState
	Dependencies   []string
	Fallback       func() error
	LastChecked    time.Time
	ErrorCount     int
	ErrorThreshold int
}

// Manager handles graceful degradation of features
type Manager struct {
	mu           sync.RWMutex
	features     map[string]*Feature
	healthChecks map[string]func() error
	callbacks    []StateChangeCallback
}

// StateChangeCallback is called when a feature state changes
type StateChangeCallback func(feature string, oldState, newState FeatureState)

// NewManager creates a new degradation manager
func NewManager() *Manager {
	return &Manager{
		features:     make(map[string]*Feature),
		healthChecks: make(map[string]func() error),
		callbacks:    make([]StateChangeCallback, 0),
	}
}

// RegisterFeature registers a degradable feature
func (m *Manager) RegisterFeature(name, description string, dependencies []string, healthCheck func() error, fallback func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.features[name] = &Feature{
		Name:           name,
		Description:    description,
		State:          FeatureEnabled,
		Dependencies:   dependencies,
		Fallback:       fallback,
		LastChecked:    time.Now(),
		ErrorThreshold: 3, // Default threshold
	}

	if healthCheck != nil {
		m.healthChecks[name] = healthCheck
	}
}

// CheckFeature checks if a feature is available
func (m *Manager) CheckFeature(name string) (FeatureState, error) {
	m.mu.RLock()
	feature, exists := m.features[name]
	m.mu.RUnlock()

	if !exists {
		return FeatureDisabled, fmt.Errorf("feature %s not registered", name)
	}

	// Check if feature needs health check
	if time.Since(feature.LastChecked) > 30*time.Second {
		m.updateFeatureHealth(name)
	}

	return feature.State, nil
}

// ExecuteWithDegradation executes a function with graceful degradation
func (m *Manager) ExecuteWithDegradation(ctx context.Context, featureName string, primary func() error) error {
	state, err := m.CheckFeature(featureName)
	if err != nil {
		return err
	}

	switch state {
	case FeatureEnabled:
		// Try primary function
		err := primary()
		if err != nil {
			m.recordError(featureName)
			// Try fallback if available
			if fallback := m.getFallback(featureName); fallback != nil {
				return fallback()
			}
			return err
		}
		m.recordSuccess(featureName)
		return nil

	case FeatureDegraded:
		// Use fallback directly
		if fallback := m.getFallback(featureName); fallback != nil {
			return fallback()
		}
		return fmt.Errorf("feature %s is degraded with no fallback", featureName)

	case FeatureDisabled:
		return fmt.Errorf("feature %s is disabled", featureName)
	}

	return nil
}

// updateFeatureHealth updates the health status of a feature
func (m *Manager) updateFeatureHealth(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	feature, exists := m.features[name]
	if !exists {
		return
	}

	// Check dependencies first
	for _, dep := range feature.Dependencies {
		if depFeature, exists := m.features[dep]; exists {
			if depFeature.State == FeatureDisabled {
				m.changeFeatureState(name, FeatureDisabled)
				return
			}
		}
	}

	// Run health check if available
	if healthCheck, exists := m.healthChecks[name]; exists {
		if err := healthCheck(); err != nil {
			feature.ErrorCount++
			if feature.ErrorCount >= feature.ErrorThreshold {
				if feature.Fallback != nil {
					m.changeFeatureState(name, FeatureDegraded)
				} else {
					m.changeFeatureState(name, FeatureDisabled)
				}
			}
		} else {
			feature.ErrorCount = 0
			if feature.State != FeatureEnabled {
				m.changeFeatureState(name, FeatureEnabled)
			}
		}
	}

	feature.LastChecked = time.Now()
}

// changeFeatureState changes the state of a feature and notifies callbacks
func (m *Manager) changeFeatureState(name string, newState FeatureState) {
	feature := m.features[name]
	if feature.State == newState {
		return
	}

	oldState := feature.State
	feature.State = newState

	// Notify callbacks
	for _, callback := range m.callbacks {
		callback(name, oldState, newState)
	}
}

// recordError records an error for a feature
func (m *Manager) recordError(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if feature, exists := m.features[name]; exists {
		feature.ErrorCount++
		if feature.ErrorCount >= feature.ErrorThreshold {
			if feature.Fallback != nil {
				m.changeFeatureState(name, FeatureDegraded)
			} else {
				m.changeFeatureState(name, FeatureDisabled)
			}
		}
	}
}

// recordSuccess records a successful operation for a feature
func (m *Manager) recordSuccess(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if feature, exists := m.features[name]; exists {
		feature.ErrorCount = 0
		if feature.State != FeatureEnabled {
			m.changeFeatureState(name, FeatureEnabled)
		}
	}
}

// getFallback returns the fallback function for a feature
func (m *Manager) getFallback(name string) func() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if feature, exists := m.features[name]; exists {
		return feature.Fallback
	}
	return nil
}

// RegisterCallback registers a state change callback
func (m *Manager) RegisterCallback(callback StateChangeCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, callback)
}

// GetFeatureStatus returns the status of all features
func (m *Manager) GetFeatureStatus() map[string]FeatureInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]FeatureInfo)
	for name, feature := range m.features {
		status[name] = FeatureInfo{
			Name:        feature.Name,
			Description: feature.Description,
			State:       getStateString(feature.State),
			ErrorCount:  feature.ErrorCount,
			LastChecked: feature.LastChecked,
			HasFallback: feature.Fallback != nil,
		}
	}
	return status
}

// FeatureInfo contains information about a feature
type FeatureInfo struct {
	Name        string
	Description string
	State       string
	ErrorCount  int
	LastChecked time.Time
	HasFallback bool
}

func getStateString(state FeatureState) string {
	switch state {
	case FeatureEnabled:
		return "enabled"
	case FeatureDegraded:
		return "degraded"
	case FeatureDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// MonitorFeatures starts monitoring all features periodically
func (m *Manager) MonitorFeatures(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAllFeatures()
		}
	}
}

// checkAllFeatures checks the health of all features
func (m *Manager) checkAllFeatures() {
	m.mu.RLock()
	features := make([]string, 0, len(m.features))
	for name := range m.features {
		features = append(features, name)
	}
	m.mu.RUnlock()

	for _, name := range features {
		m.updateFeatureHealth(name)
	}
}
