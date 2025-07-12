package timeout

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager handles timeouts for long-running operations with intelligent backoffs
type Manager struct {
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	mu             sync.RWMutex
	operationStats map[string]*OperationStats
}

// OperationStats tracks timing statistics for operations
type OperationStats struct {
	Name            string
	TotalCalls      int
	SuccessfulCalls int
	TimeoutCalls    int
	LastDuration    time.Duration
	AverageDuration time.Duration
	MaxDuration     time.Duration
	totalDuration   time.Duration
}

// Config holds timeout manager configuration
type Config struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
}

// DefaultConfig returns default timeout configuration
func DefaultConfig() *Config {
	return &Config{
		DefaultTimeout: 30 * time.Second,
		MaxTimeout:     5 * time.Minute,
	}
}

// NewManager creates a new timeout manager
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	return &Manager{
		defaultTimeout: config.DefaultTimeout,
		maxTimeout:     config.MaxTimeout,
		operationStats: make(map[string]*OperationStats),
	}
}

// ExecuteWithTimeout runs an operation with adaptive timeout
func (m *Manager) ExecuteWithTimeout(ctx context.Context, operation string, fn func(context.Context) error) error {
	timeout := m.getAdaptiveTimeout(operation)

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Track operation start
	start := time.Now()

	// Create channels for result
	errCh := make(chan error, 1)

	// Run operation in goroutine
	go func() {
		errCh <- fn(timeoutCtx)
	}()

	// Wait for completion or timeout
	select {
	case err := <-errCh:
		// Operation completed
		duration := time.Since(start)
		m.recordOperationDuration(operation, duration, err == nil)
		return err

	case <-timeoutCtx.Done():
		// Timeout occurred
		m.recordTimeout(operation)
		return fmt.Errorf("operation '%s' timed out after %v", operation, timeout)
	}
}

// ExecuteWithProgress runs an operation with progress updates
func (m *Manager) ExecuteWithProgress(ctx context.Context, operation string, progressInterval time.Duration, fn func(context.Context, chan<- Progress) error) error {
	timeout := m.getAdaptiveTimeout(operation)

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create progress channel
	progressCh := make(chan Progress, 10)

	// Start progress monitor
	go m.monitorProgress(operation, progressCh, timeout)

	// Track operation start
	start := time.Now()

	// Create channels for result
	errCh := make(chan error, 1)

	// Run operation in goroutine
	go func() {
		errCh <- fn(timeoutCtx, progressCh)
		close(progressCh)
	}()

	// Wait for completion or timeout
	select {
	case err := <-errCh:
		// Operation completed
		duration := time.Since(start)
		m.recordOperationDuration(operation, duration, err == nil)
		return err

	case <-timeoutCtx.Done():
		// Timeout occurred
		m.recordTimeout(operation)
		return fmt.Errorf("operation '%s' timed out after %v", operation, timeout)
	}
}

// getAdaptiveTimeout calculates timeout based on historical data
func (m *Manager) getAdaptiveTimeout(operation string) time.Duration {
	m.mu.RLock()
	stats, exists := m.operationStats[operation]
	m.mu.RUnlock()

	if !exists || stats.TotalCalls < 3 {
		// Not enough data, use default timeout
		return m.defaultTimeout
	}

	// Calculate adaptive timeout based on statistics
	// Use P95 approach: avg + 2*stddev, but simplified here
	adaptiveTimeout := time.Duration(float64(stats.AverageDuration) * 1.5)

	// Consider max duration seen
	if stats.MaxDuration > adaptiveTimeout {
		adaptiveTimeout = time.Duration(float64(stats.MaxDuration) * 1.2)
	}

	// Apply bounds
	if adaptiveTimeout < m.defaultTimeout {
		adaptiveTimeout = m.defaultTimeout
	}
	if adaptiveTimeout > m.maxTimeout {
		adaptiveTimeout = m.maxTimeout
	}

	return adaptiveTimeout
}

// recordOperationDuration records timing statistics
func (m *Manager) recordOperationDuration(operation string, duration time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, exists := m.operationStats[operation]
	if !exists {
		stats = &OperationStats{
			Name: operation,
		}
		m.operationStats[operation] = stats
	}

	stats.TotalCalls++
	if success {
		stats.SuccessfulCalls++
	}

	stats.LastDuration = duration
	stats.totalDuration += duration
	stats.AverageDuration = stats.totalDuration / time.Duration(stats.TotalCalls)

	if duration > stats.MaxDuration {
		stats.MaxDuration = duration
	}
}

// recordTimeout records a timeout event
func (m *Manager) recordTimeout(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, exists := m.operationStats[operation]
	if !exists {
		stats = &OperationStats{
			Name: operation,
		}
		m.operationStats[operation] = stats
	}

	stats.TotalCalls++
	stats.TimeoutCalls++
}

// Progress represents operation progress
type Progress struct {
	Operation   string
	Current     int
	Total       int
	Message     string
	LastUpdated time.Time
}

// monitorProgress monitors and logs operation progress
func (m *Manager) monitorProgress(operation string, progressCh <-chan Progress, timeout time.Duration) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	lastProgress := Progress{Operation: operation}
	startTime := time.Now()

	for {
		select {
		case progress, ok := <-progressCh:
			if !ok {
				return // Channel closed, operation completed
			}
			lastProgress = progress

		case <-ticker.C:
			elapsed := time.Since(startTime)
			remaining := timeout - elapsed

			if lastProgress.Total > 0 {
				percentComplete := float64(lastProgress.Current) / float64(lastProgress.Total) * 100
				fmt.Printf("⏳ %s: %.0f%% complete (%d/%d) - %v remaining\n",
					operation, percentComplete, lastProgress.Current, lastProgress.Total, remaining.Round(time.Second))
			} else if lastProgress.Message != "" {
				fmt.Printf("⏳ %s: %s - %v remaining\n",
					operation, lastProgress.Message, remaining.Round(time.Second))
			}
		}
	}
}

// GetOperationStats returns statistics for a specific operation
func (m *Manager) GetOperationStats(operation string) *OperationStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if stats, exists := m.operationStats[operation]; exists {
		// Return a copy to avoid race conditions
		statsCopy := *stats
		return &statsCopy
	}
	return nil
}

// GetAllStats returns statistics for all operations
func (m *Manager) GetAllStats() map[string]*OperationStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*OperationStats)
	for op, stats := range m.operationStats {
		statsCopy := *stats
		result[op] = &statsCopy
	}
	return result
}

// WrapOperation creates a timeout-aware wrapper for an operation
func (m *Manager) WrapOperation(operation string, fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		return m.ExecuteWithTimeout(ctx, operation, fn)
	}
}

// ResetStats resets statistics for an operation
func (m *Manager) ResetStats(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.operationStats, operation)
}
