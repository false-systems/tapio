package kernel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ProductionMonitoring provides comprehensive monitoring for production environments
type ProductionMonitoring struct {
	config *Config
	
	// Prometheus metrics
	promRegistry     *prometheus.Registry
	httpServer       *http.Server
	
	// OpenTelemetry
	tracer           trace.Tracer
	meter            metric.Meter
	
	// Core metrics
	collectorStatus          prometheus.Gauge
	eventsTotalCounter       *prometheus.CounterVec
	eventsProcessedDuration  *prometheus.HistogramVec
	memoryUsageGauge         prometheus.Gauge
	cpuUsageGauge            prometheus.Gauge
	bufferUsageGauge         *prometheus.GaugeVec
	errorCounter             *prometheus.CounterVec
	alertCounter             *prometheus.CounterVec
	
	// eBPF specific metrics
	ebpfProgramStatus        *prometheus.GaugeVec
	ebpfMapUsage             *prometheus.GaugeVec
	ebpfVerifierErrors       prometheus.Counter
	ebpfLoadTime             *prometheus.HistogramVec
	
	// Health metrics
	healthCheckStatus        *prometheus.GaugeVec
	lastHealthCheck          prometheus.Gauge
	consecutiveFailures      prometheus.Gauge
	uptimeGauge              prometheus.Gauge
	
	// Performance metrics
	eventLatency             *prometheus.HistogramVec
	throughputGauge          *prometheus.GaugeVec
	backpressureEvents       prometheus.Counter
	samplingRateGauge        prometheus.Gauge
	
	// Alert manager
	alertManager *AlertManager
	
	// State
	startTime    time.Time
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// AlertManager manages alerts and notifications
type AlertManager struct {
	rules      []AlertRule
	channels   []AlertChannel
	fired      map[string]time.Time
	mu         sync.RWMutex
}

// AlertRule defines an alerting rule
type AlertRule struct {
	Name        string
	Description string
	Query       string
	Threshold   float64
	Duration    time.Duration
	Severity    AlertSeverity
	Labels      map[string]string
	Enabled     bool
}

// AlertChannel defines an alert delivery channel
type AlertChannel struct {
	Name    string
	Type    string // "webhook", "email", "slack", "pagerduty"
	Config  map[string]string
	Enabled bool
}

// AlertSeverity represents alert severity levels
type AlertSeverity string

const (
	SeverityCritical AlertSeverity = "critical"
	SeverityWarning  AlertSeverity = "warning"
	SeverityInfo     AlertSeverity = "info"
)

// Alert represents a fired alert
type Alert struct {
	Name        string
	Description string
	Severity    AlertSeverity
	Value       float64
	Threshold   float64
	Labels      map[string]string
	FiredAt     time.Time
}

// NewProductionMonitoring creates a new production monitoring instance
func NewProductionMonitoring(config *Config) *ProductionMonitoring {
	ctx, cancel := context.WithCancel(context.Background())
	
	pm := &ProductionMonitoring{
		config:       config,
		promRegistry: prometheus.NewRegistry(),
		startTime:    time.Now(),
		ctx:          ctx,
		cancel:       cancel,
		tracer:       otel.Tracer("tapio/collectors/kernel"),
		meter:        otel.Meter("tapio/collectors/kernel"),
		alertManager: NewAlertManager(),
	}
	
	pm.initializeMetrics()
	pm.setupDefaultAlerts()
	
	return pm
}

// NewAlertManager creates a new alert manager
func NewAlertManager() *AlertManager {
	return &AlertManager{
		fired: make(map[string]time.Time),
	}
}

// initializeMetrics initializes all Prometheus metrics
func (pm *ProductionMonitoring) initializeMetrics() {
	// Core collector metrics
	pm.collectorStatus = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "status",
		Help:      "Status of kernel collector (1=running, 0=stopped)",
	})
	
	pm.eventsTotalCounter = promauto.With(pm.promRegistry).NewCounterVec(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "events_total",
		Help:      "Total number of events processed by type",
	}, []string{"event_type", "source"})
	
	pm.eventsProcessedDuration = promauto.With(pm.promRegistry).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "event_processing_duration_seconds",
		Help:      "Time spent processing events",
		Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 20), // 1μs to ~1s
	}, []string{"event_type"})
	
	pm.memoryUsageGauge = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "memory_bytes",
		Help:      "Current memory usage in bytes",
	})
	
	pm.cpuUsageGauge = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "cpu_percent",
		Help:      "Current CPU usage percentage",
	})
	
	pm.bufferUsageGauge = promauto.With(pm.promRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "buffer_usage_percent",
		Help:      "Buffer usage percentage by buffer type",
	}, []string{"buffer_type"})
	
	pm.errorCounter = promauto.With(pm.promRegistry).NewCounterVec(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "errors_total",
		Help:      "Total number of errors by type",
	}, []string{"error_type"})
	
	pm.alertCounter = promauto.With(pm.promRegistry).NewCounterVec(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "alerts_total",
		Help:      "Total number of fired alerts by severity",
	}, []string{"severity"})
	
	// eBPF specific metrics
	pm.ebpfProgramStatus = promauto.With(pm.promRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "ebpf_program_status",
		Help:      "Status of eBPF programs (1=loaded, 0=not loaded)",
	}, []string{"program_name"})
	
	pm.ebpfMapUsage = promauto.With(pm.promRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "ebpf_map_usage_percent",
		Help:      "eBPF map usage percentage",
	}, []string{"map_name"})
	
	pm.ebpfVerifierErrors = promauto.With(pm.promRegistry).NewCounter(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "ebpf_verifier_errors_total",
		Help:      "Total number of eBPF verifier errors",
	})
	
	pm.ebpfLoadTime = promauto.With(pm.promRegistry).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "ebpf_load_duration_seconds",
		Help:      "Time spent loading eBPF programs",
		Buckets:   prometheus.DefBuckets,
	}, []string{"program_name"})
	
	// Health metrics
	pm.healthCheckStatus = promauto.With(pm.promRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "health_check_status",
		Help:      "Health check status (1=healthy, 0=unhealthy)",
	}, []string{"component"})
	
	pm.lastHealthCheck = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "last_health_check_timestamp",
		Help:      "Timestamp of last health check",
	})
	
	pm.consecutiveFailures = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "consecutive_failures",
		Help:      "Number of consecutive health check failures",
	})
	
	pm.uptimeGauge = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "uptime_seconds",
		Help:      "Collector uptime in seconds",
	})
	
	// Performance metrics
	pm.eventLatency = promauto.With(pm.promRegistry).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "event_latency_seconds",
		Help:      "Event processing latency from kernel to output",
		Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 20),
	}, []string{"event_type"})
	
	pm.throughputGauge = promauto.With(pm.promRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "throughput_events_per_second",
		Help:      "Current event throughput per second",
	}, []string{"event_type"})
	
	pm.backpressureEvents = promauto.With(pm.promRegistry).NewCounter(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "backpressure_events_total",
		Help:      "Total number of backpressure events",
	})
	
	pm.samplingRateGauge = promauto.With(pm.promRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: "tapio",
		Subsystem: "kernel_collector",
		Name:      "sampling_rate",
		Help:      "Current sampling rate (1 = no sampling)",
	})
}

// setupDefaultAlerts sets up default alert rules
func (pm *ProductionMonitoring) setupDefaultAlerts() {
	rules := []AlertRule{
		{
			Name:        "KernelCollectorDown",
			Description: "Kernel collector is not running",
			Query:       "tapio_kernel_collector_status",
			Threshold:   0.5,
			Duration:    1 * time.Minute,
			Severity:    SeverityCritical,
			Labels:      map[string]string{"component": "kernel_collector"},
			Enabled:     true,
		},
		{
			Name:        "HighMemoryUsage", 
			Description: "Kernel collector memory usage is high",
			Query:       "tapio_kernel_collector_memory_bytes",
			Threshold:   float64(pm.config.ResourceLimits.MaxMemoryMB * 1024 * 1024) * 0.9, // 90% of limit
			Duration:    5 * time.Minute,
			Severity:    SeverityWarning,
			Labels:      map[string]string{"component": "memory"},
			Enabled:     true,
		},
		{
			Name:        "HighCPUUsage",
			Description: "Kernel collector CPU usage is high", 
			Query:       "tapio_kernel_collector_cpu_percent",
			Threshold:   float64(pm.config.ResourceLimits.MaxCPUPercent) * 0.9, // 90% of limit
			Duration:    5 * time.Minute,
			Severity:    SeverityWarning,
			Labels:      map[string]string{"component": "cpu"},
			Enabled:     true,
		},
		{
			Name:        "HighErrorRate",
			Description: "High error rate in kernel collector",
			Query:       "rate(tapio_kernel_collector_errors_total[5m])",
			Threshold:   10, // 10 errors per second
			Duration:    2 * time.Minute,
			Severity:    SeverityCritical,
			Labels:      map[string]string{"component": "errors"},
			Enabled:     true,
		},
		{
			Name:        "EBPFProgramFailure",
			Description: "eBPF program failed to load",
			Query:       "tapio_kernel_collector_ebpf_program_status",
			Threshold:   0.5,
			Duration:    30 * time.Second,
			Severity:    SeverityCritical,
			Labels:      map[string]string{"component": "ebpf"},
			Enabled:     true,
		},
		{
			Name:        "HealthCheckFailure",
			Description: "Health check is failing",
			Query:       "tapio_kernel_collector_health_check_status",
			Threshold:   0.5,
			Duration:    3 * time.Minute,
			Severity:    SeverityWarning,
			Labels:      map[string]string{"component": "health"},
			Enabled:     true,
		},
		{
			Name:        "HighEventLatency",
			Description: "Event processing latency is high",
			Query:       "histogram_quantile(0.95, tapio_kernel_collector_event_latency_seconds)",
			Threshold:   0.1, // 100ms
			Duration:    5 * time.Minute,
			Severity:    SeverityWarning,
			Labels:      map[string]string{"component": "latency"},
			Enabled:     true,
		},
		{
			Name:        "BufferOverflow",
			Description: "eBPF buffer usage is critically high",
			Query:       "tapio_kernel_collector_buffer_usage_percent",
			Threshold:   95.0, // 95%
			Duration:    1 * time.Minute,
			Severity:    SeverityCritical,
			Labels:      map[string]string{"component": "buffer"},
			Enabled:     true,
		},
	}
	
	pm.alertManager.SetRules(rules)
}

// Start starts the production monitoring
func (pm *ProductionMonitoring) Start(port int) error {
	// Set initial status
	pm.collectorStatus.Set(1)
	
	// Start metrics HTTP server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(pm.promRegistry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", pm.healthHandler)
	mux.HandleFunc("/status", pm.statusHandler)
	mux.HandleFunc("/alerts", pm.alertsHandler)
	
	pm.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	
	go func() {
		if err := pm.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error
		}
	}()
	
	// Start background monitoring
	go pm.monitoringLoop()
	go pm.alertingLoop()
	
	return nil
}

// monitoringLoop runs the main monitoring loop
func (pm *ProductionMonitoring) monitoringLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			pm.updateMetrics()
		case <-pm.ctx.Done():
			return
		}
	}
}

// alertingLoop runs the alerting evaluation loop
func (pm *ProductionMonitoring) alertingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			pm.evaluateAlerts()
		case <-pm.ctx.Done():
			return
		}
	}
}

// updateMetrics updates all metrics
func (pm *ProductionMonitoring) updateMetrics() {
	// Update uptime
	uptime := time.Since(pm.startTime).Seconds()
	pm.uptimeGauge.Set(uptime)
	
	// Update last health check time
	pm.lastHealthCheck.Set(float64(time.Now().Unix()))
}

// evaluateAlerts evaluates alert rules
func (pm *ProductionMonitoring) evaluateAlerts() {
	// This is a simplified implementation
	// In production, you'd integrate with a proper metrics backend like Prometheus
	
	pm.alertManager.mu.RLock()
	rules := pm.alertManager.rules
	pm.alertManager.mu.RUnlock()
	
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		
		// Simulate metric evaluation (in real implementation, query metrics backend)
		value := pm.evaluateMetricQuery(rule.Query)
		
		if pm.shouldFireAlert(rule, value) {
			alert := Alert{
				Name:        rule.Name,
				Description: rule.Description,
				Severity:    rule.Severity,
				Value:       value,
				Threshold:   rule.Threshold,
				Labels:      rule.Labels,
				FiredAt:     time.Now(),
			}
			
			pm.fireAlert(alert)
		}
	}
}

// evaluateMetricQuery evaluates a metric query (simplified)
func (pm *ProductionMonitoring) evaluateMetricQuery(query string) float64 {
	// This is a placeholder - in real implementation, query the metrics backend
	return 0.0
}

// shouldFireAlert determines if an alert should fire
func (pm *ProductionMonitoring) shouldFireAlert(rule AlertRule, value float64) bool {
	switch rule.Name {
	case "KernelCollectorDown":
		return value < rule.Threshold
	case "HighMemoryUsage", "HighCPUUsage", "HighEventLatency", "BufferOverflow":
		return value > rule.Threshold
	case "HighErrorRate":
		return value > rule.Threshold
	default:
		return false
	}
}

// fireAlert fires an alert
func (pm *ProductionMonitoring) fireAlert(alert Alert) {
	pm.alertManager.mu.Lock()
	defer pm.alertManager.mu.Unlock()
	
	// Check if already fired recently
	if lastFired, exists := pm.alertManager.fired[alert.Name]; exists {
		if time.Since(lastFired) < 5*time.Minute { // 5 minute cooldown
			return
		}
	}
	
	pm.alertManager.fired[alert.Name] = alert.FiredAt
	
	// Update alert counter metric
	pm.alertCounter.WithLabelValues(string(alert.Severity)).Inc()
	
	// Send alert through channels
	for _, channel := range pm.alertManager.channels {
		if channel.Enabled {
			go pm.sendAlert(alert, channel)
		}
	}
}

// sendAlert sends an alert through a channel
func (pm *ProductionMonitoring) sendAlert(alert Alert, channel AlertChannel) {
	// Implementation depends on channel type
	switch channel.Type {
	case "webhook":
		pm.sendWebhookAlert(alert, channel)
	case "email":
		pm.sendEmailAlert(alert, channel)
	case "slack":
		pm.sendSlackAlert(alert, channel)
	}
}

// Placeholder implementations for alert channels
func (pm *ProductionMonitoring) sendWebhookAlert(alert Alert, channel AlertChannel) {
	// Implement webhook sending
}

func (pm *ProductionMonitoring) sendEmailAlert(alert Alert, channel AlertChannel) {
	// Implement email sending
}

func (pm *ProductionMonitoring) sendSlackAlert(alert Alert, channel AlertChannel) {
	// Implement Slack notification
}

// HTTP handlers
func (pm *ProductionMonitoring) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "healthy", "uptime": %f}`, time.Since(pm.startTime).Seconds())
}

func (pm *ProductionMonitoring) statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	status := map[string]interface{}{
		"status":    "running",
		"uptime":    time.Since(pm.startTime).Seconds(),
		"started_at": pm.startTime,
	}
	
	// Convert to JSON (simplified)
	fmt.Fprintf(w, `{"status": "%v", "uptime": %f, "started_at": "%v"}`, 
		status["status"], status["uptime"], status["started_at"])
}

func (pm *ProductionMonitoring) alertsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	pm.alertManager.mu.RLock()
	fired := pm.alertManager.fired
	pm.alertManager.mu.RUnlock()
	
	fmt.Fprintf(w, `{"active_alerts": %d}`, len(fired))
}

// Metric recording methods
func (pm *ProductionMonitoring) RecordEvent(eventType, source string) {
	pm.eventsTotalCounter.WithLabelValues(eventType, source).Inc()
}

func (pm *ProductionMonitoring) RecordEventProcessingDuration(eventType string, duration time.Duration) {
	pm.eventsProcessedDuration.WithLabelValues(eventType).Observe(duration.Seconds())
}

func (pm *ProductionMonitoring) UpdateMemoryUsage(bytes int64) {
	pm.memoryUsageGauge.Set(float64(bytes))
}

func (pm *ProductionMonitoring) UpdateCPUUsage(percent float64) {
	pm.cpuUsageGauge.Set(percent)
}

func (pm *ProductionMonitoring) UpdateBufferUsage(bufferType string, percent float64) {
	pm.bufferUsageGauge.WithLabelValues(bufferType).Set(percent)
}

func (pm *ProductionMonitoring) RecordError(errorType string) {
	pm.errorCounter.WithLabelValues(errorType).Inc()
}

func (pm *ProductionMonitoring) UpdateEBPFProgramStatus(programName string, loaded bool) {
	value := 0.0
	if loaded {
		value = 1.0
	}
	pm.ebpfProgramStatus.WithLabelValues(programName).Set(value)
}

func (pm *ProductionMonitoring) UpdateEBPFMapUsage(mapName string, percent float64) {
	pm.ebpfMapUsage.WithLabelValues(mapName).Set(percent)
}

func (pm *ProductionMonitoring) RecordEBPFVerifierError() {
	pm.ebpfVerifierErrors.Inc()
}

func (pm *ProductionMonitoring) RecordEBPFLoadTime(programName string, duration time.Duration) {
	pm.ebpfLoadTime.WithLabelValues(programName).Observe(duration.Seconds())
}

func (pm *ProductionMonitoring) UpdateHealthCheckStatus(component string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	pm.healthCheckStatus.WithLabelValues(component).Set(value)
}

func (pm *ProductionMonitoring) UpdateConsecutiveFailures(count float64) {
	pm.consecutiveFailures.Set(count)
}

func (pm *ProductionMonitoring) RecordEventLatency(eventType string, latency time.Duration) {
	pm.eventLatency.WithLabelValues(eventType).Observe(latency.Seconds())
}

func (pm *ProductionMonitoring) UpdateThroughput(eventType string, eventsPerSecond float64) {
	pm.throughputGauge.WithLabelValues(eventType).Set(eventsPerSecond)
}

func (pm *ProductionMonitoring) RecordBackpressureEvent() {
	pm.backpressureEvents.Inc()
}

func (pm *ProductionMonitoring) UpdateSamplingRate(rate float64) {
	pm.samplingRateGauge.Set(rate)
}

// SetRules sets alert rules
func (am *AlertManager) SetRules(rules []AlertRule) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.rules = rules
}

// AddChannel adds an alert channel
func (am *AlertManager) AddChannel(channel AlertChannel) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.channels = append(am.channels, channel)
}

// Stop stops the production monitoring
func (pm *ProductionMonitoring) Stop() {
	pm.cancel()
	
	pm.collectorStatus.Set(0)
	
	if pm.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pm.httpServer.Shutdown(ctx)
	}
}