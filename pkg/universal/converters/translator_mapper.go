package converters

import (
	"fmt"
	"sync"
	"time"

	"github.com/falseyair/tapio/pkg/translator"
	"github.com/falseyair/tapio/pkg/universal"
)

// TranslatorPIDMapper uses the translator engine for PID to pod mapping
type TranslatorPIDMapper struct {
	engine       *translator.Engine
	fallbackData map[int32]*universal.Target
	mu           sync.RWMutex
}

// NewTranslatorPIDMapper creates a new PID mapper using the translator engine
func NewTranslatorPIDMapper(engine *translator.Engine) *TranslatorPIDMapper {
	return &TranslatorPIDMapper{
		engine:       engine,
		fallbackData: make(map[int32]*universal.Target),
	}
}

// MapPIDToTarget maps a PID to a universal target using the translator
func (m *TranslatorPIDMapper) MapPIDToTarget(pid int32) (*universal.Target, error) {
	// Use translator if available
	if m.engine != nil {
		result, err := m.engine.Translate(pid)
		if err == nil && result != nil {
			target := &universal.Target{
				Type:      universal.TargetTypePod,
				Name:      result.PodName,
				Namespace: result.Namespace,
				PID:       pid,
				Container: result.ContainerName,
				Pod:       result.PodName,
				Node:      result.NodeName,
			}

			// Update fallback data
			m.mu.Lock()
			m.fallbackData[pid] = target
			m.mu.Unlock()

			return target, nil
		}

		// Try fallback data on error
		m.mu.RLock()
		if fallback, exists := m.fallbackData[pid]; exists {
			m.mu.RUnlock()
			return fallback, nil
		}
		m.mu.RUnlock()

		// Return error if no fallback available
		if err != nil {
			return nil, fmt.Errorf("translation failed: %w", err)
		}
	}

	// No translator available, return process-only target
	return &universal.Target{
		Type: universal.TargetTypeProcess,
		Name: fmt.Sprintf("process-%d", pid),
		PID:  pid,
	}, nil
}

// UpdateMapping updates the fallback mapping
func (m *TranslatorPIDMapper) UpdateMapping(pid int32, target *universal.Target) {
	if target != nil {
		m.mu.Lock()
		m.fallbackData[pid] = target
		m.mu.Unlock()
	}
}

// ClearCache clears the fallback data
func (m *TranslatorPIDMapper) ClearCache() {
	m.mu.Lock()
	m.fallbackData = make(map[int32]*universal.Target)
	m.mu.Unlock()
}

// CleanupOldEntries removes entries older than the specified duration
func (m *TranslatorPIDMapper) CleanupOldEntries(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	// In a real implementation, we would track timestamps
	// For now, just return 0
	return 0
}
