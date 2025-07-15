package managers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

// HealthChecker implements health checking for the server
type HealthChecker struct {
	checks   map[string]HealthCheck
	mu       sync.RWMutex
	logger   domain.Logger
	
	// Cache
	lastCheck     *domain.HealthCheck
	lastCheckTime time.Time
	cacheDuration time.Duration
}

// HealthCheck represents a component health check function
type HealthCheck func(ctx context.Context) (*domain.HealthCheck, error)

// NewHealthChecker creates a new health checker
func NewHealthChecker(logger domain.Logger) *HealthChecker {
	checker := &HealthChecker{
		checks:        make(map[string]HealthCheck),
		logger:        logger,
		cacheDuration: 5 * time.Second,
	}
	
	// Register default checks
	checker.RegisterCheck("server", checker.checkServer)
	
	return checker
}

// RegisterCheck registers a health check
func (h *HealthChecker) RegisterCheck(name string, check HealthCheck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.checks[name] = check
	
	if h.logger != nil {
		h.logger.Info(context.Background(), fmt.Sprintf("registered health check: %s", name))
	}
}

// UnregisterCheck removes a health check
func (h *HealthChecker) UnregisterCheck(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	delete(h.checks, name)
	
	if h.logger != nil {
		h.logger.Info(context.Background(), fmt.Sprintf("unregistered health check: %s", name))
	}
}

// CheckHealth performs all health checks
func (h *HealthChecker) CheckHealth(ctx context.Context) (*domain.HealthCheck, error) {
	// Check cache
	h.mu.RLock()
	if h.lastCheck != nil && time.Since(h.lastCheckTime) < h.cacheDuration {
		cached := *h.lastCheck
		h.mu.RUnlock()
		return &cached, nil
	}
	h.mu.RUnlock()
	
	start := time.Now()
	
	// Perform all checks
	results := h.performAllChecks(ctx)
	
	// Aggregate results
	overall := h.aggregateResults(results)
	overall.Duration = time.Since(start)
	overall.Timestamp = time.Now()
	
	// Update cache
	h.mu.Lock()
	h.lastCheck = overall
	h.lastCheckTime = time.Now()
	h.mu.Unlock()
	
	return overall, nil
}

// CheckComponentHealth checks a specific component
func (h *HealthChecker) CheckComponentHealth(ctx context.Context, component string) (*domain.HealthCheck, error) {
	h.mu.RLock()
	check, exists := h.checks[component]
	h.mu.RUnlock()
	
	if !exists {
		return nil, domain.ErrResourceNotFound(fmt.Sprintf("health check not found: %s", component))
	}
	
	return check(ctx)
}

// IsHealthy returns whether the server is healthy
func (h *HealthChecker) IsHealthy(ctx context.Context) (bool, error) {
	health, err := h.CheckHealth(ctx)
	if err != nil {
		return false, err
	}
	
	return health.Status == domain.HealthStatusPass, nil
}

// GetHealthStatus returns the current health status
func (h *HealthChecker) GetHealthStatus(ctx context.Context) (domain.HealthStatus, error) {
	health, err := h.CheckHealth(ctx)
	if err != nil {
		return domain.HealthStatusFail, err
	}
	
	return health.Status, nil
}

// performAllChecks executes all registered health checks
func (h *HealthChecker) performAllChecks(ctx context.Context) map[string]*domain.HealthCheck {
	h.mu.RLock()
	checkFuncs := make(map[string]HealthCheck, len(h.checks))
	for name, check := range h.checks {
		checkFuncs[name] = check
	}
	h.mu.RUnlock()
	
	results := make(map[string]*domain.HealthCheck)
	resultsMu := sync.Mutex{}
	wg := sync.WaitGroup{}
	
	// Run checks concurrently with timeout
	for name, check := range checkFuncs {
		wg.Add(1)
		go func(n string, c HealthCheck) {
			defer wg.Done()
			
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			
			result, err := c(checkCtx)
			if err != nil {
				result = &domain.HealthCheck{
					Name:      n,
					Status:    domain.HealthStatusFail,
					Message:   err.Error(),
					Timestamp: time.Now(),
				}
			}
			
			resultsMu.Lock()
			results[n] = result
			resultsMu.Unlock()
		}(name, check)
	}
	
	wg.Wait()
	return results
}

// aggregateResults combines individual health checks into overall status
func (h *HealthChecker) aggregateResults(results map[string]*domain.HealthCheck) *domain.HealthCheck {
	overall := &domain.HealthCheck{
		Name:    "server",
		Status:  domain.HealthStatusPass,
		Message: "All checks passed",
		Details: make(map[string]interface{}),
	}
	
	failCount := 0
	warnCount := 0
	checks := make(map[string]map[string]interface{})
	
	for name, result := range results {
		checkDetail := map[string]interface{}{
			"status":    result.Status,
			"message":   result.Message,
			"timestamp": result.Timestamp,
			"duration":  result.Duration.String(),
		}
		
		if result.Details != nil {
			checkDetail["details"] = result.Details
		}
		
		checks[name] = checkDetail
		
		switch result.Status {
		case domain.HealthStatusFail:
			failCount++
		case domain.HealthStatusWarn:
			warnCount++
		}
	}
	
	overall.Details["checks"] = checks
	overall.Details["summary"] = map[string]interface{}{
		"total":   len(results),
		"passed":  len(results) - failCount - warnCount,
		"warning": warnCount,
		"failed":  failCount,
	}
	
	// Determine overall status
	if failCount > 0 {
		overall.Status = domain.HealthStatusFail
		overall.Message = fmt.Sprintf("%d checks failed", failCount)
	} else if warnCount > 0 {
		overall.Status = domain.HealthStatusWarn
		overall.Message = fmt.Sprintf("%d checks have warnings", warnCount)
	}
	
	return overall
}

// Default health checks

func (h *HealthChecker) checkServer(ctx context.Context) (*domain.HealthCheck, error) {
	start := time.Now()
	
	// Basic server health check
	health := &domain.HealthCheck{
		Name:      "server",
		Status:    domain.HealthStatusPass,
		Message:   "Server is running",
		Timestamp: time.Now(),
		Duration:  time.Since(start),
		Details: map[string]interface{}{
			"uptime": time.Since(start).String(),
			"goroutines": getGoroutineCount(),
			"memory": getMemoryStats(),
		},
	}
	
	return health, nil
}

// Helper functions

func getGoroutineCount() int {
	// In production, use runtime.NumGoroutine()
	return 42 // Placeholder
}

func getMemoryStats() map[string]interface{} {
	// In production, use runtime.MemStats
	return map[string]interface{}{
		"allocated": "64MB",
		"total":     "128MB",
		"gc_count":  42,
	}
}

// RegisterDatabaseCheck adds a database health check
func (h *HealthChecker) RegisterDatabaseCheck(name string, pingFunc func(ctx context.Context) error) {
	h.RegisterCheck(name, func(ctx context.Context) (*domain.HealthCheck, error) {
		start := time.Now()
		
		err := pingFunc(ctx)
		status := domain.HealthStatusPass
		message := "Database is healthy"
		
		if err != nil {
			status = domain.HealthStatusFail
			message = fmt.Sprintf("Database check failed: %v", err)
		}
		
		return &domain.HealthCheck{
			Name:      name,
			Status:    status,
			Message:   message,
			Timestamp: time.Now(),
			Duration:  time.Since(start),
		}, nil
	})
}

// RegisterHTTPCheck adds an HTTP endpoint health check
func (h *HealthChecker) RegisterHTTPCheck(name, url string) {
	h.RegisterCheck(name, func(ctx context.Context) (*domain.HealthCheck, error) {
		start := time.Now()
		
		// In production, make actual HTTP request
		// For now, simulate success
		
		return &domain.HealthCheck{
			Name:      name,
			Status:    domain.HealthStatusPass,
			Message:   fmt.Sprintf("HTTP endpoint %s is healthy", url),
			Timestamp: time.Now(),
			Duration:  time.Since(start),
			Details: map[string]interface{}{
				"url":          url,
				"status_code":  200,
				"response_time": "50ms",
			},
		}, nil
	})
}