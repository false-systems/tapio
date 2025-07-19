// Package health provides standardized health check functionality for Tapio services
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	// StatusHealthy indicates the component is functioning normally
	StatusHealthy Status = "healthy"
	// StatusDegraded indicates the component is functioning but with issues
	StatusDegraded Status = "degraded"
	// StatusUnhealthy indicates the component is not functioning properly
	StatusUnhealthy Status = "unhealthy"
)

// Response represents the standardized health check response
type Response struct {
	Status    Status           `json:"status"`
	Timestamp time.Time        `json:"timestamp"`
	Service   string           `json:"service"`
	Version   string           `json:"version"`
	Uptime    string           `json:"uptime,omitempty"`
	Checks    map[string]Check `json:"checks,omitempty"`
}

// Check represents an individual health check result
type Check struct {
	Status   Status                 `json:"status"`
	Message  string                 `json:"message,omitempty"`
	Latency  string                 `json:"latency,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Checker interface for components that can be health checked
type Checker interface {
	// Name returns the name of this health check
	Name() string
	// Check performs the health check
	Check(ctx context.Context) Check
}

// Handler manages health checks and provides HTTP handler
type Handler struct {
	service   string
	version   string
	startTime time.Time
	checkers  []Checker
	mu        sync.RWMutex
}

// NewHandler creates a new health check handler
func NewHandler(service, version string) *Handler {
	return &Handler{
		service:   service,
		version:   version,
		startTime: time.Now(),
		checkers:  []Checker{},
	}
}

// AddChecker adds a health checker to the handler
func (h *Handler) AddChecker(checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers = append(h.checkers, checker)
}

// ServeHTTP implements http.Handler for health checks
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Run all health checks
	checks := h.runChecks(ctx)

	// Determine overall status
	status := h.calculateOverallStatus(checks)

	// Build response
	response := Response{
		Status:    status,
		Timestamp: time.Now(),
		Service:   h.service,
		Version:   h.version,
		Uptime:    time.Since(h.startTime).Round(time.Second).String(),
		Checks:    checks,
	}

	// Set appropriate status code
	statusCode := http.StatusOK
	if status == StatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	} else if status == StatusDegraded {
		statusCode = http.StatusOK // Still return 200 for degraded
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// LivenessHandler returns a simple liveness check handler
func (h *Handler) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status":    "alive",
			"timestamp": time.Now(),
			"service":   h.service,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// ReadinessHandler returns a readiness check handler
func (h *Handler) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		checks := h.runChecks(ctx)
		status := h.calculateOverallStatus(checks)

		response := map[string]interface{}{
			"status":    string(status),
			"timestamp": time.Now(),
			"service":   h.service,
			"ready":     status != StatusUnhealthy,
		}

		statusCode := http.StatusOK
		if status == StatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
	}
}

// runChecks executes all registered health checks
func (h *Handler) runChecks(ctx context.Context) map[string]Check {
	h.mu.RLock()
	checkers := make([]Checker, len(h.checkers))
	copy(checkers, h.checkers)
	h.mu.RUnlock()

	checks := make(map[string]Check)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, checker := range checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()

			// Run check with timeout
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			start := time.Now()
			check := c.Check(checkCtx)
			check.Latency = time.Since(start).String()

			mu.Lock()
			checks[c.Name()] = check
			mu.Unlock()
		}(checker)
	}

	wg.Wait()
	return checks
}

// calculateOverallStatus determines the overall health status
func (h *Handler) calculateOverallStatus(checks map[string]Check) Status {
	if len(checks) == 0 {
		return StatusHealthy
	}

	hasUnhealthy := false
	hasDegraded := false

	for _, check := range checks {
		switch check.Status {
		case StatusUnhealthy:
			hasUnhealthy = true
		case StatusDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return StatusUnhealthy
	}
	if hasDegraded {
		return StatusDegraded
	}
	return StatusHealthy
}
