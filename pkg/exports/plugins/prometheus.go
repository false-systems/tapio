package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/model"
	"github.com/yairfalse/tapio/pkg/exports"
)

// PrometheusExportPlugin implements Prometheus metrics export
type PrometheusExportPlugin struct {
	name          string
	config        *PrometheusExportConfig
	pusher        *push.Pusher
	registry      *prometheus.Registry
	metrics       *PrometheusMetrics
	customMetrics map[string]prometheus.Collector
	metricsMutex  sync.RWMutex
	httpClient    *http.Client
}

// PrometheusExportConfig configures the Prometheus export plugin
type PrometheusExportConfig struct {
	// Push Gateway settings
	PushGatewayURL string        `json:"push_gateway_url"`
	Job            string        `json:"job"`
	Instance       string        `json:"instance"`
	PushInterval   time.Duration `json:"push_interval"`

	// Authentication
	BasicAuth   *BasicAuth `json:"basic_auth,omitempty"`
	BearerToken string     `json:"bearer_token,omitempty"`

	// TLS settings
	TLSConfig *TLSConfig `json:"tls_config,omitempty"`

	// Metric settings
	MetricPrefix     string            `json:"metric_prefix"`
	IncludeTimestamp bool              `json:"include_timestamp"`
	Labels           map[string]string `json:"labels"`

	// Batching settings
	BatchSize     int           `json:"batch_size"`
	FlushInterval time.Duration `json:"flush_interval"`

	// Timeout settings
	RequestTimeout time.Duration `json:"request_timeout"`

	// Grouping settings
	GroupingKey      map[string]string `json:"grouping_key"`
	DeleteOnShutdown bool              `json:"delete_on_shutdown"`
}

// BasicAuth holds basic authentication credentials
type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	CAFile             string `json:"ca_file"`
	CertFile           string `json:"cert_file"`
	KeyFile            string `json:"key_file"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// PrometheusMetrics tracks plugin metrics
type PrometheusMetrics struct {
	ExportsTotal   prometheus.Counter
	ExportsSuccess prometheus.Counter
	ExportsFailed  prometheus.Counter
	MetricsPushed  prometheus.Counter
	PushDuration   prometheus.Histogram
	LastPushTime   time.Time
}

// NewPrometheusExportPlugin creates a new Prometheus export plugin
func NewPrometheusExportPlugin() *PrometheusExportPlugin {
	registry := prometheus.NewRegistry()

	// Create metrics
	metrics := &PrometheusMetrics{
		ExportsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tapio_prometheus_exports_total",
			Help: "Total number of export attempts",
		}),
		ExportsSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tapio_prometheus_exports_success_total",
			Help: "Total number of successful exports",
		}),
		ExportsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tapio_prometheus_exports_failed_total",
			Help: "Total number of failed exports",
		}),
		MetricsPushed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tapio_prometheus_metrics_pushed_total",
			Help: "Total number of metrics pushed",
		}),
		PushDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "tapio_prometheus_push_duration_seconds",
			Help:    "Duration of push operations",
			Buckets: prometheus.DefBuckets,
		}),
	}

	// Register metrics
	registry.MustRegister(
		metrics.ExportsTotal,
		metrics.ExportsSuccess,
		metrics.ExportsFailed,
		metrics.MetricsPushed,
		metrics.PushDuration,
	)

	return &PrometheusExportPlugin{
		name: "prometheus-export",
		config: &PrometheusExportConfig{
			PushGatewayURL: "http://localhost:9091",
			Job:            "tapio",
			Instance:       "tapio-1",
			MetricPrefix:   "tapio_",
			PushInterval:   30 * time.Second,
			BatchSize:      1000,
			FlushInterval:  10 * time.Second,
			RequestTimeout: 30 * time.Second,
		},
		registry:      registry,
		metrics:       metrics,
		customMetrics: make(map[string]prometheus.Collector),
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the plugin name
func (p *PrometheusExportPlugin) Name() string {
	return p.name
}

// Start starts the plugin
func (p *PrometheusExportPlugin) Start(ctx context.Context) error {
	p.metricsMutex.Lock()
	defer p.metricsMutex.Unlock()

	// Validate configuration
	if err := p.ValidateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create pusher
	pusher := push.New(p.config.PushGatewayURL, p.config.Job).
		Gatherer(p.registry)

	// Add instance if configured
	if p.config.Instance != "" {
		pusher = pusher.Grouping("instance", p.config.Instance)
	}

	// Add grouping keys
	for k, v := range p.config.GroupingKey {
		pusher = pusher.Grouping(k, v)
	}

	// Configure authentication
	if p.config.BasicAuth != nil {
		pusher = pusher.BasicAuth(p.config.BasicAuth.Username, p.config.BasicAuth.Password)
	}

	// Configure HTTP client
	if p.config.BearerToken != "" || p.config.TLSConfig != nil {
		client := p.createHTTPClient()
		pusher = pusher.Client(client)
	}

	p.pusher = pusher

	// Start periodic push if configured
	if p.config.PushInterval > 0 {
		go p.runPeriodicPush(ctx)
	}

	return nil
}

// Stop stops the plugin
func (p *PrometheusExportPlugin) Stop(ctx context.Context) error {
	p.metricsMutex.Lock()
	defer p.metricsMutex.Unlock()

	// Delete metrics from push gateway if configured
	if p.config.DeleteOnShutdown && p.pusher != nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := p.pusher.Delete(); err != nil {
			return fmt.Errorf("failed to delete metrics: %w", err)
		}
	}

	return nil
}

// Configure configures the plugin
func (p *PrometheusExportPlugin) Configure(config map[string]interface{}) error {
	p.metricsMutex.Lock()
	defer p.metricsMutex.Unlock()

	// Convert map to config struct
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	newConfig := &PrometheusExportConfig{}
	if err := json.Unmarshal(data, newConfig); err != nil {
		return err
	}

	// Set defaults
	if newConfig.Job == "" {
		newConfig.Job = "tapio"
	}
	if newConfig.MetricPrefix == "" {
		newConfig.MetricPrefix = "tapio_"
	}
	if newConfig.BatchSize <= 0 {
		newConfig.BatchSize = 1000
	}
	if newConfig.RequestTimeout <= 0 {
		newConfig.RequestTimeout = 30 * time.Second
	}

	p.config = newConfig
	return nil
}

// ValidateConfig validates the plugin configuration
func (p *PrometheusExportPlugin) ValidateConfig() error {
	if p.config.PushGatewayURL == "" {
		return fmt.Errorf("push_gateway_url is required")
	}

	// Validate URL
	if _, err := url.Parse(p.config.PushGatewayURL); err != nil {
		return fmt.Errorf("invalid push_gateway_url: %w", err)
	}

	if p.config.Job == "" {
		return fmt.Errorf("job is required")
	}

	return nil
}

// GetConfigSchema returns the configuration schema
func (p *PrometheusExportPlugin) GetConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"push_gateway_url": map[string]interface{}{
				"type":        "string",
				"description": "Prometheus push gateway URL",
				"default":     "http://localhost:9091",
			},
			"job": map[string]interface{}{
				"type":        "string",
				"description": "Job label for metrics",
				"default":     "tapio",
			},
			"instance": map[string]interface{}{
				"type":        "string",
				"description": "Instance label for metrics",
			},
			"metric_prefix": map[string]interface{}{
				"type":        "string",
				"description": "Prefix for metric names",
				"default":     "tapio_",
			},
			"push_interval": map[string]interface{}{
				"type":        "string",
				"description": "Interval for periodic pushes",
				"default":     "30s",
			},
			"labels": map[string]interface{}{
				"type":        "object",
				"description": "Additional labels for all metrics",
			},
		},
		"required": []string{"push_gateway_url", "job"},
	}
}

// Export exports data to Prometheus
func (p *PrometheusExportPlugin) Export(ctx context.Context, data exports.ExportData) error {
	p.metrics.ExportsTotal.Inc()

	start := time.Now()

	// Export based on data type
	var err error
	switch data.Type {
	case exports.DataTypeMetrics:
		err = p.exportMetrics(ctx, data)
	case exports.DataTypeEvents:
		err = p.exportEvents(ctx, data)
	case exports.DataTypeDriftReport, exports.DataTypeCorrelation, exports.DataTypePatternResult:
		err = p.exportAsMetrics(ctx, data)
	default:
		err = fmt.Errorf("unsupported data type: %s", data.Type)
	}

	if err != nil {
		p.metrics.ExportsFailed.Inc()
		return err
	}

	p.metrics.ExportsSuccess.Inc()

	// Call callback if provided
	if data.Callback != nil {
		data.Callback(&exports.ExportResult{
			Success:  true,
			Duration: time.Since(start),
			Details: map[string]interface{}{
				"push_gateway": p.config.PushGatewayURL,
				"job":          p.config.Job,
			},
		})
	}

	return nil
}

// SupportedFormats returns supported export formats
func (p *PrometheusExportPlugin) SupportedFormats() []exports.ExportFormat {
	return []exports.ExportFormat{
		exports.FormatPrometheus,
	}
}

// SupportedDataTypes returns supported data types
func (p *PrometheusExportPlugin) SupportedDataTypes() []exports.DataType {
	return []exports.DataType{
		exports.DataTypeDriftReport,
		exports.DataTypeSnapshot,
		exports.DataTypeCorrelation,
		exports.DataTypeMetrics,
		exports.DataTypeEvents,
		exports.DataTypePatternResult,
		exports.DataTypeAutoFix,
	}
}

// HealthCheck performs a health check
func (p *PrometheusExportPlugin) HealthCheck(ctx context.Context) (*exports.HealthStatus, error) {
	p.metricsMutex.RLock()
	defer p.metricsMutex.RUnlock()

	// Check push gateway connectivity
	healthy := true
	message := "Prometheus export plugin is healthy"

	// Try to reach push gateway
	if p.config.PushGatewayURL != "" {
		healthURL := strings.TrimSuffix(p.config.PushGatewayURL, "/") + "/-/healthy"
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err == nil {
			resp, err := p.httpClient.Do(req)
			if err != nil {
				healthy = false
				message = fmt.Sprintf("Push gateway unreachable: %v", err)
			} else {
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					healthy = false
					message = fmt.Sprintf("Push gateway unhealthy: status %d", resp.StatusCode)
				}
			}
		}
	}

	return &exports.HealthStatus{
		Healthy:   healthy,
		LastCheck: time.Now(),
		Message:   message,
		Details: map[string]interface{}{
			"push_gateway_url": p.config.PushGatewayURL,
			"job":              p.config.Job,
			"instance":         p.config.Instance,
			"last_push_time":   p.metrics.LastPushTime,
		},
		ResourceUsage: &exports.ResourceUsage{
			MemoryMB:      1.0, // Minimal memory usage
			CPUPercent:    0.1,
			ExportsPerSec: p.calculateExportRate(),
		},
	}, nil
}

// GetMetrics returns plugin metrics
func (p *PrometheusExportPlugin) GetMetrics() map[string]interface{} {
	// Gather metrics from registry
	mfs, err := p.registry.Gather()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	metrics := make(map[string]interface{})
	for _, mf := range mfs {
		name := mf.GetName()
		if len(mf.Metric) > 0 {
			// Get the first metric value
			m := mf.Metric[0]
			switch {
			case m.Counter != nil:
				metrics[name] = m.Counter.GetValue()
			case m.Gauge != nil:
				metrics[name] = m.Gauge.GetValue()
			case m.Histogram != nil:
				metrics[name] = m.Histogram.GetSampleCount()
			}
		}
	}

	metrics["last_push_time"] = p.metrics.LastPushTime

	return metrics
}

// exportMetrics exports metrics data
func (p *PrometheusExportPlugin) exportMetrics(ctx context.Context, data exports.ExportData) error {
	metrics, ok := data.Content.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid metrics format")
	}

	// Create gauges for each metric
	for name, value := range metrics {
		metricName := p.config.MetricPrefix + name

		// Check if metric already exists
		if _, exists := p.customMetrics[metricName]; !exists {
			gauge := prometheus.NewGaugeVec(
				prometheus.GaugeOpts{
					Name: metricName,
					Help: fmt.Sprintf("Tapio metric: %s", name),
				},
				p.getLabelNames(),
			)

			if err := p.registry.Register(gauge); err != nil {
				// Metric might already be registered
				continue
			}

			p.customMetrics[metricName] = gauge
		}

		// Set metric value
		if gauge, ok := p.customMetrics[metricName].(*prometheus.GaugeVec); ok {
			labels := p.getLabels(data.Tags)
			if v, ok := value.(float64); ok {
				gauge.With(labels).Set(v)
			} else if v, ok := value.(int); ok {
				gauge.With(labels).Set(float64(v))
			} else if v, ok := value.(int64); ok {
				gauge.With(labels).Set(float64(v))
			}
		}
	}

	// Push metrics
	return p.push(ctx)
}

// exportEvents exports events as metrics
func (p *PrometheusExportPlugin) exportEvents(ctx context.Context, data exports.ExportData) error {
	events, ok := data.Content.([]interface{})
	if !ok {
		return fmt.Errorf("invalid events format")
	}

	// Create event counter
	eventCounter, exists := p.customMetrics["tapio_events_total"]
	if !exists {
		counter := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tapio_events_total",
				Help: "Total number of events by type",
			},
			append(p.getLabelNames(), "event_type", "severity"),
		)

		if err := p.registry.Register(counter); err == nil {
			p.customMetrics["tapio_events_total"] = counter
			eventCounter = counter
		}
	}

	// Count events by type
	if counter, ok := eventCounter.(*prometheus.CounterVec); ok {
		for _, event := range events {
			if e, ok := event.(map[string]interface{}); ok {
				labels := p.getLabels(data.Tags)
				if eventType, ok := e["type"].(string); ok {
					labels["event_type"] = eventType
				}
				if severity, ok := e["severity"].(string); ok {
					labels["severity"] = severity
				}
				counter.With(labels).Inc()
			}
		}
	}

	p.metrics.MetricsPushed.Add(float64(len(events)))

	// Push metrics
	return p.push(ctx)
}

// exportAsMetrics exports generic data as metrics
func (p *PrometheusExportPlugin) exportAsMetrics(ctx context.Context, data exports.ExportData) error {
	// Convert data to metrics based on type
	metricName := fmt.Sprintf("%s%s_total", p.config.MetricPrefix, strings.ToLower(string(data.Type)))

	counter, exists := p.customMetrics[metricName]
	if !exists {
		c := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName,
				Help: fmt.Sprintf("Total number of %s", data.Type),
			},
			p.getLabelNames(),
		)

		if err := p.registry.Register(c); err == nil {
			p.customMetrics[metricName] = c
			counter = c
		}
	}

	if c, ok := counter.(*prometheus.CounterVec); ok {
		labels := p.getLabels(data.Tags)
		c.With(labels).Inc()
	}

	p.metrics.MetricsPushed.Inc()

	// Push metrics
	return p.push(ctx)
}

// push pushes metrics to the push gateway
func (p *PrometheusExportPlugin) push(ctx context.Context) error {
	if p.pusher == nil {
		return fmt.Errorf("pusher not initialized")
	}

	timer := prometheus.NewTimer(p.metrics.PushDuration)
	defer timer.ObserveDuration()

	// Push with context
	pushCtx, cancel := context.WithTimeout(ctx, p.config.RequestTimeout)
	defer cancel()

	// Create a custom pusher that respects context
	err := p.pushWithContext(pushCtx)
	if err != nil {
		return fmt.Errorf("failed to push metrics: %w", err)
	}

	p.metrics.LastPushTime = time.Now()

	return nil
}

// pushWithContext pushes metrics with context support
func (p *PrometheusExportPlugin) pushWithContext(ctx context.Context) error {
	// Since the prometheus push client doesn't support context directly,
	// we need to implement a workaround
	done := make(chan error, 1)

	go func() {
		done <- p.pusher.Push()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runPeriodicPush runs periodic metric pushes
func (p *PrometheusExportPlugin) runPeriodicPush(ctx context.Context) {
	ticker := time.NewTicker(p.config.PushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.push(ctx); err != nil {
				// Log error but continue
				fmt.Printf("Failed to push metrics: %v\n", err)
			}
		}
	}
}

// createHTTPClient creates a custom HTTP client
func (p *PrometheusExportPlugin) createHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Configure TLS if needed
	if p.config.TLSConfig != nil {
		// TLS configuration would be set here
		// This is a simplified implementation
	}

	// Add bearer token if configured
	if p.config.BearerToken != "" {
		transport = &bearerAuthTransport{
			Transport: transport,
			Token:     p.config.BearerToken,
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   p.config.RequestTimeout,
	}
}

// getLabelNames returns the label names for metrics
func (p *PrometheusExportPlugin) getLabelNames() []string {
	labelNames := make([]string, 0, len(p.config.Labels))
	for k := range p.config.Labels {
		labelNames = append(labelNames, k)
	}
	return labelNames
}

// getLabels returns labels for a metric
func (p *PrometheusExportPlugin) getLabels(tags map[string]string) prometheus.Labels {
	labels := make(prometheus.Labels)

	// Add configured labels
	for k, v := range p.config.Labels {
		labels[k] = v
	}

	// Add tags as labels
	for k, v := range tags {
		// Sanitize label name
		labelName := model.LabelNameRE.ReplaceAllString(k, "_")
		if model.IsValidLabelName(model.LabelName(labelName)) {
			labels[labelName] = v
		}
	}

	return labels
}

// calculateExportRate calculates exports per second
func (p *PrometheusExportPlugin) calculateExportRate() float64 {
	// Simple rate calculation - in production would use a sliding window
	return 0.0
}

// bearerAuthTransport adds bearer token authentication to requests
type bearerAuthTransport struct {
	Transport http.RoundTripper
	Token     string
}

func (t *bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.Token)
	return t.Transport.RoundTrip(req)
}

// PrometheusTextFormatter formats data as Prometheus text format
type PrometheusTextFormatter struct {
	config *PrometheusExportConfig
}

// Format formats data as Prometheus text format
func (f *PrometheusTextFormatter) Format(data exports.ExportData) ([]byte, error) {
	var buf bytes.Buffer

	switch data.Type {
	case exports.DataTypeMetrics:
		if metrics, ok := data.Content.(map[string]interface{}); ok {
			for name, value := range metrics {
				metricName := f.config.MetricPrefix + name
				fmt.Fprintf(&buf, "# HELP %s Tapio metric: %s\n", metricName, name)
				fmt.Fprintf(&buf, "# TYPE %s gauge\n", metricName)

				// Format labels
				var labelPairs []string
				for k, v := range data.Tags {
					labelPairs = append(labelPairs, fmt.Sprintf(`%s="%s"`, k, v))
				}

				labelStr := ""
				if len(labelPairs) > 0 {
					labelStr = "{" + strings.Join(labelPairs, ",") + "}"
				}

				fmt.Fprintf(&buf, "%s%s %v\n", metricName, labelStr, value)
			}
		}
	}

	return buf.Bytes(), nil
}
