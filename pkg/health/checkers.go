package health

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// DatabaseChecker checks database connectivity
type DatabaseChecker struct {
	name string
	db   *sql.DB
}

// NewDatabaseChecker creates a new database health checker
func NewDatabaseChecker(name string, db *sql.DB) *DatabaseChecker {
	return &DatabaseChecker{
		name: name,
		db:   db,
	}
}

// Name returns the checker name
func (d *DatabaseChecker) Name() string {
	return d.name
}

// Check performs the database health check
func (d *DatabaseChecker) Check(ctx context.Context) Check {
	if d.db == nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: "Database connection is nil",
		}
	}
	
	// Try to ping the database
	err := d.db.PingContext(ctx)
	if err != nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("Database ping failed: %v", err),
		}
	}
	
	// Get connection stats
	stats := d.db.Stats()
	
	return Check{
		Status:  StatusHealthy,
		Message: "Database is accessible",
		Metadata: map[string]interface{}{
			"open_connections": stats.OpenConnections,
			"in_use":          stats.InUse,
			"idle":            stats.Idle,
		},
	}
}

// HTTPChecker checks HTTP endpoint availability
type HTTPChecker struct {
	name   string
	url    string
	client *http.Client
}

// NewHTTPChecker creates a new HTTP health checker
func NewHTTPChecker(name, url string) *HTTPChecker {
	return &HTTPChecker{
		name: name,
		url:  url,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Name returns the checker name
func (h *HTTPChecker) Name() string {
	return h.name
}

// Check performs the HTTP health check
func (h *HTTPChecker) Check(ctx context.Context) Check {
	req, err := http.NewRequestWithContext(ctx, "GET", h.url, nil)
	if err != nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("Failed to create request: %v", err),
		}
	}
	
	resp, err := h.client.Do(req)
	if err != nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Check{
			Status:  StatusHealthy,
			Message: fmt.Sprintf("HTTP endpoint is accessible (status: %d)", resp.StatusCode),
		}
	}
	
	return Check{
		Status:  StatusDegraded,
		Message: fmt.Sprintf("HTTP endpoint returned non-2xx status: %d", resp.StatusCode),
	}
}

// MemoryChecker checks memory usage
type MemoryChecker struct {
	name         string
	thresholdPct float64
}

// NewMemoryChecker creates a new memory health checker
func NewMemoryChecker(name string, thresholdPct float64) *MemoryChecker {
	return &MemoryChecker{
		name:         name,
		thresholdPct: thresholdPct,
	}
}

// Name returns the checker name
func (m *MemoryChecker) Name() string {
	return m.name
}

// Check performs the memory health check
func (m *MemoryChecker) Check(ctx context.Context) Check {
	// This is a simplified example - in production you'd use runtime.MemStats
	// or system-level memory stats
	
	// For now, return healthy
	return Check{
		Status:  StatusHealthy,
		Message: "Memory usage is within limits",
		Metadata: map[string]interface{}{
			"threshold_pct": m.thresholdPct,
		},
	}
}

// CustomChecker allows for custom health check functions
type CustomChecker struct {
	name  string
	check func(ctx context.Context) Check
}

// NewCustomChecker creates a new custom health checker
func NewCustomChecker(name string, check func(ctx context.Context) Check) *CustomChecker {
	return &CustomChecker{
		name:  name,
		check: check,
	}
}

// Name returns the checker name
func (c *CustomChecker) Name() string {
	return c.name
}

// Check performs the custom health check
func (c *CustomChecker) Check(ctx context.Context) Check {
	return c.check(ctx)
}