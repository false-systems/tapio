package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PrometheusConfig holds Prometheus scraper configuration
type PrometheusConfig struct {
	SchedulerMetricsURL string        // e.g., "http://kube-scheduler:10251/metrics"
	ScrapeInterval      time.Duration // How often to scrape (default: 30s)
	HTTPTimeout         time.Duration // HTTP request timeout (default: 10s)
}

// PrometheusScraper scrapes kube-scheduler metrics
type PrometheusScraper struct {
	config     PrometheusConfig
	httpClient *http.Client
}

// NewPrometheusScraper creates a new Prometheus scraper
func NewPrometheusScraper(config PrometheusConfig) *PrometheusScraper {
	if config.ScrapeInterval == 0 {
		config.ScrapeInterval = 30 * time.Second
	}
	if config.HTTPTimeout == 0 {
		config.HTTPTimeout = 10 * time.Second
	}

	return &PrometheusScraper{
		config: config,
		httpClient: &http.Client{
			Timeout: config.HTTPTimeout,
		},
	}
}

// MetricSample represents a single Prometheus metric sample
type MetricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// ScrapeMetrics fetches and parses Prometheus metrics
func (p *PrometheusScraper) ScrapeMetrics(ctx context.Context) ([]MetricSample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.SchedulerMetricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		//nolint:errcheck // Response already read, close error not actionable
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	return p.parsePrometheusExposition(resp.Body)
}

// parsePrometheusExposition parses Prometheus text exposition format
// Format: metric_name{label1="value1",label2="value2"} 123.45
func (p *PrometheusScraper) parsePrometheusExposition(r io.Reader) ([]MetricSample, error) {
	var samples []MetricSample
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		sample, err := p.parseLine(line)
		if err != nil {
			continue
		}

		samples = append(samples, sample)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return samples, nil
}

// parseLine parses a single Prometheus metric line
// Example: scheduler_pending_pods{queue="active"} 42
func (p *PrometheusScraper) parseLine(line string) (MetricSample, error) {
	var sample MetricSample

	openBrace := strings.Index(line, "{")
	closeBrace := strings.Index(line, "}")
	space := strings.LastIndex(line, " ")

	if space == -1 {
		return sample, fmt.Errorf("invalid line format: no space separator")
	}

	valueStr := line[space+1:]
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return sample, fmt.Errorf("invalid value: %w", err)
	}

	sample.Value = value

	if openBrace == -1 || closeBrace == -1 {
		sample.Name = line[:space]
		sample.Labels = make(map[string]string)
		return sample, nil
	}

	sample.Name = line[:openBrace]
	labelsStr := line[openBrace+1 : closeBrace]
	sample.Labels = p.parseLabels(labelsStr)

	return sample, nil
}

// parseLabels parses Prometheus label string
// Example: plugin="NodeAffinity",extension_point="Filter"
func (p *PrometheusScraper) parseLabels(labelsStr string) map[string]string {
	labels := make(map[string]string)

	if labelsStr == "" {
		return labels
	}

	pairs := strings.Split(labelsStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		labels[key] = value
	}

	return labels
}

// FilterSchedulerMetrics filters metrics relevant to scheduler observability
func (p *PrometheusScraper) FilterSchedulerMetrics(samples []MetricSample) []MetricSample {
	var filtered []MetricSample

	schedulerMetrics := map[string]bool{
		"scheduler_plugin_execution_duration_seconds":          true,
		"scheduler_framework_extension_point_duration_seconds": true,
		"scheduler_pending_pods":                               true,
		"scheduler_preemption_victims":                         true,
		"scheduler_scheduling_attempt_duration_seconds":        true,
		"scheduler_scheduling_algorithm_duration_seconds":      true,
		"scheduler_e2e_scheduling_duration_seconds":            true,
	}

	for _, sample := range samples {
		if schedulerMetrics[sample.Name] {
			filtered = append(filtered, sample)
		}
	}

	return filtered
}
