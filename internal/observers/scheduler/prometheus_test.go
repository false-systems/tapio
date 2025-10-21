package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPrometheusScraper_DefaultValues verifies default configuration
func TestNewPrometheusScraper_DefaultValues(t *testing.T) {
	config := PrometheusConfig{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
	}

	scraper := NewPrometheusScraper(config)
	require.NotNil(t, scraper)
	assert.Equal(t, 30*time.Second, scraper.config.ScrapeInterval)
	assert.Equal(t, 10*time.Second, scraper.config.HTTPTimeout)
	assert.NotNil(t, scraper.httpClient)
}

// TestNewPrometheusScraper_CustomValues verifies custom configuration
func TestNewPrometheusScraper_CustomValues(t *testing.T) {
	config := PrometheusConfig{
		SchedulerMetricsURL: "http://custom:9090/metrics",
		ScrapeInterval:      15 * time.Second,
		HTTPTimeout:         5 * time.Second,
	}

	scraper := NewPrometheusScraper(config)
	require.NotNil(t, scraper)
	assert.Equal(t, 15*time.Second, scraper.config.ScrapeInterval)
	assert.Equal(t, 5*time.Second, scraper.config.HTTPTimeout)
}

// TestScrapeMetrics_Success verifies successful metric scraping
func TestScrapeMetrics_Success(t *testing.T) {
	// Create test server with Prometheus exposition format
	metricsData := `# HELP scheduler_pending_pods Number of pending pods
# TYPE scheduler_pending_pods gauge
scheduler_pending_pods{queue="active"} 42
scheduler_pending_pods{queue="backoff"} 5
scheduler_plugin_execution_duration_seconds{plugin="NodeAffinity",extension_point="Filter"} 0.15
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(metricsData)); err != nil {
			panic(err) // Test setup issue
		}
	}))
	defer server.Close()

	config := PrometheusConfig{
		SchedulerMetricsURL: server.URL,
	}
	scraper := NewPrometheusScraper(config)

	samples, err := scraper.ScrapeMetrics(context.Background())
	require.NoError(t, err)
	assert.Len(t, samples, 3)

	// Verify first sample
	assert.Equal(t, "scheduler_pending_pods", samples[0].Name)
	assert.Equal(t, "active", samples[0].Labels["queue"])
	assert.Equal(t, 42.0, samples[0].Value)

	// Verify second sample
	assert.Equal(t, "scheduler_pending_pods", samples[1].Name)
	assert.Equal(t, "backoff", samples[1].Labels["queue"])
	assert.Equal(t, 5.0, samples[1].Value)

	// Verify third sample
	assert.Equal(t, "scheduler_plugin_execution_duration_seconds", samples[2].Name)
	assert.Equal(t, "NodeAffinity", samples[2].Labels["plugin"])
	assert.Equal(t, "Filter", samples[2].Labels["extension_point"])
	assert.Equal(t, 0.15, samples[2].Value)
}

// TestScrapeMetrics_HTTPError verifies error handling for HTTP failures
func TestScrapeMetrics_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := PrometheusConfig{
		SchedulerMetricsURL: server.URL,
	}
	scraper := NewPrometheusScraper(config)

	_, err := scraper.ScrapeMetrics(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected HTTP status")
}

// TestScrapeMetrics_InvalidURL verifies error handling for invalid URLs
func TestScrapeMetrics_InvalidURL(t *testing.T) {
	config := PrometheusConfig{
		SchedulerMetricsURL: "http://nonexistent-host:99999/metrics",
		HTTPTimeout:         100 * time.Millisecond,
	}
	scraper := NewPrometheusScraper(config)

	_, err := scraper.ScrapeMetrics(context.Background())
	assert.Error(t, err)
}

// TestParsePrometheusExposition_ValidData verifies parsing logic
func TestParsePrometheusExposition_ValidData(t *testing.T) {
	data := `# HELP metric_name Help text
# TYPE metric_name gauge
metric_name{label="value"} 123.45
metric_without_labels 67.89
`
	scraper := NewPrometheusScraper(PrometheusConfig{})
	samples, err := scraper.parsePrometheusExposition(strings.NewReader(data))
	require.NoError(t, err)
	assert.Len(t, samples, 2)

	// First sample with labels
	assert.Equal(t, "metric_name", samples[0].Name)
	assert.Equal(t, "value", samples[0].Labels["label"])
	assert.Equal(t, 123.45, samples[0].Value)

	// Second sample without labels
	assert.Equal(t, "metric_without_labels", samples[1].Name)
	assert.Empty(t, samples[1].Labels)
	assert.Equal(t, 67.89, samples[1].Value)
}

// TestParsePrometheusExposition_EmptyLines verifies empty line handling
func TestParsePrometheusExposition_EmptyLines(t *testing.T) {
	data := `
metric1 10

metric2 20

`
	scraper := NewPrometheusScraper(PrometheusConfig{})
	samples, err := scraper.parsePrometheusExposition(strings.NewReader(data))
	require.NoError(t, err)
	assert.Len(t, samples, 2)
}

// TestParsePrometheusExposition_CommentsIgnored verifies comment handling
func TestParsePrometheusExposition_CommentsIgnored(t *testing.T) {
	data := `# This is a comment
metric1 10
# Another comment
metric2 20
`
	scraper := NewPrometheusScraper(PrometheusConfig{})
	samples, err := scraper.parsePrometheusExposition(strings.NewReader(data))
	require.NoError(t, err)
	assert.Len(t, samples, 2)
}

// TestParseLine_WithLabels verifies label parsing
func TestParseLine_WithLabels(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	sample, err := scraper.parseLine(`metric{label1="value1",label2="value2"} 42.5`)
	require.NoError(t, err)
	assert.Equal(t, "metric", sample.Name)
	assert.Equal(t, "value1", sample.Labels["label1"])
	assert.Equal(t, "value2", sample.Labels["label2"])
	assert.Equal(t, 42.5, sample.Value)
}

// TestParseLine_WithoutLabels verifies parsing without labels
func TestParseLine_WithoutLabels(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	sample, err := scraper.parseLine(`simple_metric 99.9`)
	require.NoError(t, err)
	assert.Equal(t, "simple_metric", sample.Name)
	assert.Empty(t, sample.Labels)
	assert.Equal(t, 99.9, sample.Value)
}

// TestParseLine_InvalidFormat verifies error handling for invalid format
func TestParseLine_InvalidFormat(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	_, err := scraper.parseLine(`invalid_line_without_value`)
	assert.Error(t, err)
}

// TestParseLine_InvalidValue verifies error handling for non-numeric values
func TestParseLine_InvalidValue(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	_, err := scraper.parseLine(`metric not_a_number`)
	assert.Error(t, err)
}

// TestParseLabels_MultiplePairs verifies multiple label parsing
func TestParseLabels_MultiplePairs(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	labels := scraper.parseLabels(`key1="val1",key2="val2",key3="val3"`)
	assert.Len(t, labels, 3)
	assert.Equal(t, "val1", labels["key1"])
	assert.Equal(t, "val2", labels["key2"])
	assert.Equal(t, "val3", labels["key3"])
}

// TestParseLabels_EmptyString verifies empty label string handling
func TestParseLabels_EmptyString(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	labels := scraper.parseLabels("")
	assert.Empty(t, labels)
}

// TestParseLabels_WithSpaces verifies whitespace handling
func TestParseLabels_WithSpaces(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	labels := scraper.parseLabels(` key1 = "val1" , key2 = "val2" `)
	assert.Len(t, labels, 2)
	assert.Equal(t, "val1", labels["key1"])
	assert.Equal(t, "val2", labels["key2"])
}

// TestFilterSchedulerMetrics_MatchingMetrics verifies filtering logic
func TestFilterSchedulerMetrics_MatchingMetrics(t *testing.T) {
	samples := []MetricSample{
		{Name: "scheduler_pending_pods", Value: 42},
		{Name: "scheduler_plugin_execution_duration_seconds", Value: 0.15},
		{Name: "unrelated_metric", Value: 100},
		{Name: "scheduler_preemption_victims", Value: 5},
		{Name: "another_unrelated_metric", Value: 200},
	}

	scraper := NewPrometheusScraper(PrometheusConfig{})
	filtered := scraper.FilterSchedulerMetrics(samples)

	assert.Len(t, filtered, 3)
	assert.Equal(t, "scheduler_pending_pods", filtered[0].Name)
	assert.Equal(t, "scheduler_plugin_execution_duration_seconds", filtered[1].Name)
	assert.Equal(t, "scheduler_preemption_victims", filtered[2].Name)
}

// TestFilterSchedulerMetrics_NoMatches verifies empty result when no matches
func TestFilterSchedulerMetrics_NoMatches(t *testing.T) {
	samples := []MetricSample{
		{Name: "unrelated_metric1", Value: 100},
		{Name: "unrelated_metric2", Value: 200},
	}

	scraper := NewPrometheusScraper(PrometheusConfig{})
	filtered := scraper.FilterSchedulerMetrics(samples)

	assert.Empty(t, filtered)
}

// TestFilterSchedulerMetrics_EmptyInput verifies empty input handling
func TestFilterSchedulerMetrics_EmptyInput(t *testing.T) {
	scraper := NewPrometheusScraper(PrometheusConfig{})
	filtered := scraper.FilterSchedulerMetrics([]MetricSample{})

	assert.Empty(t, filtered)
}
