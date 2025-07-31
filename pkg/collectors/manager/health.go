package manager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthStatus represents the overall health status
type HealthStatus struct {
	Healthy    bool                       `json:"healthy"`
	Timestamp  time.Time                  `json:"timestamp"`
	Collectors map[string]CollectorStatus `json:"collectors"`
	Uptime     string                     `json:"uptime"`
}

// CollectorStatus represents individual collector status
type CollectorStatus struct {
	Name          string    `json:"name"`
	Healthy       bool      `json:"healthy"`
	EventsEmitted int64     `json:"events_emitted"`
	LastHealthy   time.Time `json:"last_healthy"`
	LastError     string    `json:"last_error,omitempty"`
	Uptime        string    `json:"uptime"`
}

// StartHealthEndpoint starts an HTTP health endpoint
func (m *CollectorManager) StartHealthEndpoint(addr string) error {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", m.handleHealth)

	// Ready check endpoint (for K8s readiness probe)
	mux.HandleFunc("/ready", m.handleReady)

	// Metrics endpoint (basic)
	mux.HandleFunc("/metrics", m.handleMetrics)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error
		}
	}()

	// Graceful shutdown
	go func() {
		<-m.ctx.Done()
		server.Close()
	}()

	return nil
}

// handleHealth returns detailed health status
func (m *CollectorManager) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := m.GetHealth()

	status := HealthStatus{
		Healthy:    m.IsHealthy(),
		Timestamp:  time.Now(),
		Collectors: make(map[string]CollectorStatus),
	}

	// Calculate overall uptime
	var earliestStart time.Time
	for _, h := range health {
		if earliestStart.IsZero() || h.StartTime.Before(earliestStart) {
			earliestStart = h.StartTime
		}

		collectorStatus := CollectorStatus{
			Name:          h.Name,
			Healthy:       h.Healthy,
			EventsEmitted: h.EventsEmitted,
			LastHealthy:   h.LastHealthy,
			Uptime:        time.Since(h.StartTime).Round(time.Second).String(),
		}

		if h.LastError != nil {
			collectorStatus.LastError = h.LastError.Error()
		}

		status.Collectors[h.Name] = collectorStatus
	}

	if !earliestStart.IsZero() {
		status.Uptime = time.Since(earliestStart).Round(time.Second).String()
	}

	// Set response status
	if !status.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleReady returns simple ready status
func (m *CollectorManager) handleReady(w http.ResponseWriter, r *http.Request) {
	if m.IsHealthy() {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ready\n")
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "not ready\n")
	}
}

// handleMetrics returns basic Prometheus-style metrics
func (m *CollectorManager) handleMetrics(w http.ResponseWriter, r *http.Request) {
	health := m.GetHealth()

	w.Header().Set("Content-Type", "text/plain")

	// Collector health metric
	fmt.Fprintf(w, "# HELP tapio_collector_healthy Collector health status (1=healthy, 0=unhealthy)\n")
	fmt.Fprintf(w, "# TYPE tapio_collector_healthy gauge\n")
	for _, h := range health {
		value := 0
		if h.Healthy {
			value = 1
		}
		fmt.Fprintf(w, "tapio_collector_healthy{collector=\"%s\"} %d\n", h.Name, value)
	}

	// Events emitted metric
	fmt.Fprintf(w, "\n# HELP tapio_collector_events_total Total events emitted by collector\n")
	fmt.Fprintf(w, "# TYPE tapio_collector_events_total counter\n")
	for _, h := range health {
		fmt.Fprintf(w, "tapio_collector_events_total{collector=\"%s\"} %d\n", h.Name, h.EventsEmitted)
	}

	// Uptime metric
	fmt.Fprintf(w, "\n# HELP tapio_collector_uptime_seconds Collector uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE tapio_collector_uptime_seconds gauge\n")
	for _, h := range health {
		uptime := time.Since(h.StartTime).Seconds()
		fmt.Fprintf(w, "tapio_collector_uptime_seconds{collector=\"%s\"} %.0f\n", h.Name, uptime)
	}
}
