package managers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/yairfalse/tapio/pkg/server/domain"
)

// MetricsCollector implements Prometheus metrics collection
type MetricsCollector struct {
	// Server metrics
	requestsTotal     *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	activeConnections prometheus.Gauge
	errorRate         prometheus.Gauge
	
	// Endpoint metrics
	endpointRequests  *prometheus.CounterVec
	endpointDuration  *prometheus.HistogramVec
	endpointErrors    *prometheus.CounterVec
	
	// Connection metrics
	connectionTotal    prometheus.Counter
	connectionDuration *prometheus.HistogramVec
	bytesReceived      prometheus.Counter
	bytesSent          prometheus.Counter
	
	// Custom metrics registry
	registry *prometheus.Registry
	
	// Internal state
	mu               sync.RWMutex
	serverMetrics    domain.ServerMetrics
	endpointMetrics  map[string]*domain.EndpointMetrics
	connectionMetrics map[string]*domain.ConnectionMetrics
	
	logger domain.Logger
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger domain.Logger) *MetricsCollector {
	registry := prometheus.NewRegistry()
	
	collector := &MetricsCollector{
		registry:          registry,
		endpointMetrics:   make(map[string]*domain.EndpointMetrics),
		connectionMetrics: make(map[string]*domain.ConnectionMetrics),
		logger:           logger,
		
		// Server metrics
		requestsTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_server_requests_total",
				Help: "Total number of requests",
			},
			[]string{"type", "status"},
		),
		
		requestDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_server_request_duration_seconds",
				Help:    "Request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"type"},
		),
		
		activeConnections: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Name: "tapio_server_active_connections",
				Help: "Number of active connections",
			},
		),
		
		errorRate: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Name: "tapio_server_error_rate",
				Help: "Current error rate",
			},
		),
		
		// Endpoint metrics
		endpointRequests: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_endpoint_requests_total",
				Help: "Total number of endpoint requests",
			},
			[]string{"endpoint", "method", "status"},
		),
		
		endpointDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_endpoint_duration_seconds",
				Help:    "Endpoint request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"endpoint", "method"},
		),
		
		endpointErrors: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_endpoint_errors_total",
				Help: "Total number of endpoint errors",
			},
			[]string{"endpoint", "error_type"},
		),
		
		// Connection metrics
		connectionTotal: promauto.With(registry).NewCounter(
			prometheus.CounterOpts{
				Name: "tapio_connections_total",
				Help: "Total number of connections",
			},
		),
		
		connectionDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tapio_connection_duration_seconds",
				Help:    "Connection duration in seconds",
				Buckets: prometheus.ExponentialBuckets(1, 2, 10),
			},
			[]string{"protocol"},
		),
		
		bytesReceived: promauto.With(registry).NewCounter(
			prometheus.CounterOpts{
				Name: "tapio_bytes_received_total",
				Help: "Total bytes received",
			},
		),
		
		bytesSent: promauto.With(registry).NewCounter(
			prometheus.CounterOpts{
				Name: "tapio_bytes_sent_total",
				Help: "Total bytes sent",
			},
		),
	}
	
	// Start metrics update goroutine
	go collector.updateMetricsLoop()
	
	return collector
}

// CollectMetrics collects all metrics
func (m *MetricsCollector) CollectMetrics(ctx context.Context) (*domain.Metrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Copy endpoint metrics
	endpoints := make(map[string]domain.EndpointMetrics)
	for name, metrics := range m.endpointMetrics {
		if metrics != nil {
			endpoints[name] = *metrics
		}
	}
	
	// Copy connection metrics
	connections := make(map[string]domain.ConnectionMetrics)
	for id, metrics := range m.connectionMetrics {
		if metrics != nil {
			connections[id] = *metrics
		}
	}
	
	return &domain.Metrics{
		Server:      m.serverMetrics,
		Endpoints:   endpoints,
		Connections: connections,
		Timestamp:   time.Now(),
	}, nil
}

// CollectServerMetrics collects server metrics
func (m *MetricsCollector) CollectServerMetrics(ctx context.Context) (*domain.ServerMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	metrics := m.serverMetrics
	metrics.LastUpdated = time.Now()
	
	return &metrics, nil
}

// CollectEndpointMetrics collects endpoint metrics
func (m *MetricsCollector) CollectEndpointMetrics(ctx context.Context, endpointName string) (*domain.EndpointMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	metrics, exists := m.endpointMetrics[endpointName]
	if !exists {
		return nil, domain.ErrResourceNotFound(fmt.Sprintf("endpoint metrics not found: %s", endpointName))
	}
	
	metricsCopy := *metrics
	return &metricsCopy, nil
}

// CollectConnectionMetrics collects connection metrics
func (m *MetricsCollector) CollectConnectionMetrics(ctx context.Context, connectionID string) (*domain.ConnectionMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	metrics, exists := m.connectionMetrics[connectionID]
	if !exists {
		return nil, domain.ErrResourceNotFound(fmt.Sprintf("connection metrics not found: %s", connectionID))
	}
	
	metricsCopy := *metrics
	return &metricsCopy, nil
}

// RecordRequest records a request
func (m *MetricsCollector) RecordRequest(ctx context.Context, request *domain.Request) error {
	if request == nil {
		return domain.ErrInvalidRequest("request cannot be nil")
	}
	
	// Update Prometheus metrics
	m.requestsTotal.WithLabelValues(string(request.Type), "success").Inc()
	
	// Update internal metrics
	m.mu.Lock()
	m.serverMetrics.RequestsTotal++
	m.serverMetrics.LastUpdated = time.Now()
	m.mu.Unlock()
	
	return nil
}

// RecordResponse records a response
func (m *MetricsCollector) RecordResponse(ctx context.Context, response *domain.Response) error {
	if response == nil {
		return domain.ErrInvalidRequest("response cannot be nil")
	}
	
	// Update based on response status
	status := "success"
	if response.Status != domain.ResponseStatusOK {
		status = "error"
	}
	
	m.requestsTotal.WithLabelValues(string(response.Type), status).Inc()
	
	return nil
}

// RecordError records an error
func (m *MetricsCollector) RecordError(ctx context.Context, err error) error {
	m.mu.Lock()
	m.serverMetrics.ErrorsTotal++
	m.serverMetrics.ErrorRate = float64(m.serverMetrics.ErrorsTotal) / float64(m.serverMetrics.RequestsTotal)
	m.serverMetrics.LastUpdated = time.Now()
	m.mu.Unlock()
	
	// Update Prometheus gauge
	m.errorRate.Set(m.serverMetrics.ErrorRate)
	
	return nil
}

// RecordRequestDuration records request duration
func (m *MetricsCollector) RecordRequestDuration(requestType string, duration time.Duration) {
	m.requestDuration.WithLabelValues(requestType).Observe(duration.Seconds())
	
	m.mu.Lock()
	// Update rolling average
	currentAvg := m.serverMetrics.AverageResponseTime
	newCount := m.serverMetrics.RequestsTotal + 1
	m.serverMetrics.AverageResponseTime = (currentAvg*time.Duration(m.serverMetrics.RequestsTotal) + duration) / time.Duration(newCount)
	m.mu.Unlock()
}

// RecordEndpointRequest records an endpoint request
func (m *MetricsCollector) RecordEndpointRequest(endpoint, method, status string, duration time.Duration) {
	m.endpointRequests.WithLabelValues(endpoint, method, status).Inc()
	m.endpointDuration.WithLabelValues(endpoint, method).Observe(duration.Seconds())
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Initialize endpoint metrics if needed
	if _, exists := m.endpointMetrics[endpoint]; !exists {
		m.endpointMetrics[endpoint] = &domain.EndpointMetrics{}
	}
	
	metrics := m.endpointMetrics[endpoint]
	metrics.RequestsTotal++
	metrics.LastRequest = time.Now()
	
	// Update response times
	if duration > metrics.P99ResponseTime {
		metrics.P99ResponseTime = duration
	}
	if duration > metrics.P95ResponseTime && duration < metrics.P99ResponseTime {
		metrics.P95ResponseTime = duration
	}
	
	// Update average (simplified)
	metrics.AverageResponseTime = (metrics.AverageResponseTime*time.Duration(metrics.RequestsTotal-1) + duration) / time.Duration(metrics.RequestsTotal)
	
	if status != "200" && status != "success" {
		metrics.ErrorsTotal++
	}
	
	// Calculate rates
	if elapsed := time.Since(m.serverMetrics.LastUpdated); elapsed > 0 {
		metrics.RequestsPerSecond = float64(metrics.RequestsTotal) / elapsed.Seconds()
		metrics.ErrorRate = float64(metrics.ErrorsTotal) / float64(metrics.RequestsTotal)
	}
}

// UpdateConnectionMetrics updates connection metrics
func (m *MetricsCollector) UpdateConnectionMetrics(connectionID string, received, sent uint64) {
	m.bytesReceived.Add(float64(received))
	m.bytesSent.Add(float64(sent))
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.connectionMetrics[connectionID]; !exists {
		m.connectionMetrics[connectionID] = &domain.ConnectionMetrics{}
	}
	
	metrics := m.connectionMetrics[connectionID]
	metrics.BytesReceived += received
	metrics.BytesSent += sent
	metrics.LastRequestTime = time.Now()
}

// SetActiveConnections sets the number of active connections
func (m *MetricsCollector) SetActiveConnections(count int) {
	m.activeConnections.Set(float64(count))
	
	m.mu.Lock()
	m.serverMetrics.ActiveConnections = uint64(count)
	m.mu.Unlock()
}

// GetRegistry returns the Prometheus registry
func (m *MetricsCollector) GetRegistry() *prometheus.Registry {
	return m.registry
}

// updateMetricsLoop periodically updates calculated metrics
func (m *MetricsCollector) updateMetricsLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	startTime := time.Now()
	
	for range ticker.C {
		m.mu.Lock()
		
		// Calculate requests per second
		elapsed := time.Since(m.serverMetrics.LastUpdated)
		if elapsed > 0 && m.serverMetrics.RequestsTotal > 0 {
			m.serverMetrics.RequestsPerSecond = float64(m.serverMetrics.RequestsTotal) / time.Since(startTime).Seconds()
		}
		
		// Update error rate
		if m.serverMetrics.RequestsTotal > 0 {
			m.serverMetrics.ErrorRate = float64(m.serverMetrics.ErrorsTotal) / float64(m.serverMetrics.RequestsTotal)
		}
		
		// Placeholder for memory and CPU usage
		m.serverMetrics.MemoryUsage = 64 * 1024 * 1024 // 64MB
		m.serverMetrics.CPUUsage = 0.15                // 15%
		
		m.serverMetrics.LastUpdated = time.Now()
		
		m.mu.Unlock()
		
		// Update Prometheus gauges
		m.errorRate.Set(m.serverMetrics.ErrorRate)
	}
}