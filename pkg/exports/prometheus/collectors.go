package prometheus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/prometheus/client_golang/prometheus"
)

// CustomCollector implements prometheus.Collector for Tapio-specific metrics
type CustomCollector struct {
	// Metric descriptors
	correlationDesc       *prometheus.Desc
	patternDesc           *prometheus.Desc
	systemHealthDesc      *prometheus.Desc
	entityHealthDesc      *prometheus.Desc
	processingLatencyDesc *prometheus.Desc
	
	// Data sources
	correlationProvider   CorrelationProvider
	systemHealthProvider  SystemHealthProvider
	
	// Configuration
	config *CollectorConfig
	
	// State management
	mutex        sync.RWMutex
	lastScrape   time.Time
	cacheTimeout time.Duration
	
	// Cached metrics
	cachedMetrics []prometheus.Metric
	cacheValid    bool
}

// CollectorConfig configures the custom collector
type CollectorConfig struct {
	// Metric configuration
	Namespace           string
	Subsystem           string
	IncludeTimestamp    bool
	IncludeInstanceInfo bool
	
	// Performance settings
	CacheTimeout        time.Duration
	MaxMetricsPerScrape int
	CollectionTimeout   time.Duration
	
	// Data source settings
	EnableRealTimeData    bool
	EnableHistoricalData  bool
	HistoricalLookback    time.Duration
	
	// Feature flags
	EnablePatternMetrics  bool
	EnableSystemMetrics   bool
	EnableEntityMetrics   bool
	
	// Labels
	ConstLabels    map[string]string
	DynamicLabels  []string
}

// CorrelationProvider interface for retrieving correlation data
type CorrelationProvider interface {
	GetActiveCorrelations(ctx context.Context) ([]*correlation.Result, error)
	GetCorrelationStats(ctx context.Context) (*CorrelationStats, error)
	GetPatternStats(ctx context.Context) (*PatternStats, error)
}

// SystemHealthProvider interface for retrieving system health data
type SystemHealthProvider interface {
	GetSystemHealth(ctx context.Context) (*SystemHealth, error)
	GetResourceUsage(ctx context.Context) (*ResourceUsage, error)
	GetEntityHealth(ctx context.Context) ([]*EntityHealth, error)
}

// CorrelationStats represents correlation statistics
type CorrelationStats struct {
	TotalCorrelations    int64
	ActiveCorrelations   int64
	ResolvedCorrelations int64
	ByCategory          map[string]int64
	BySeverity          map[string]int64
	AvgConfidence       float64
	AvgProcessingTime   time.Duration
}

// PatternStats represents pattern detection statistics
type PatternStats struct {
	PatternsDetected    int64
	TruePositives       int64
	FalsePositives      int64
	DetectionAccuracy   float64
	AvgConfidence       float64
	ByPatternType       map[string]int64
}

// SystemHealth represents overall system health
type SystemHealth struct {
	OverallScore       float64
	ComponentScores    map[string]float64
	ActiveIssues       int64
	CriticalIssues     int64
	LastUpdate         time.Time
}

// ResourceUsage represents resource utilization
type ResourceUsage struct {
	CPUUsage           map[string]float64 // node -> usage
	MemoryUsage        map[string]float64 // node -> usage
	StorageUsage       map[string]float64 // node -> usage
	NetworkUsage       map[string]float64 // node -> usage
	PodCount           map[string]int64   // namespace -> count
}

// EntityHealth represents health of individual entities
type EntityHealth struct {
	Entity     correlation.Entity
	HealthScore float64
	Issues     []string
	LastSeen   time.Time
}

// NewCustomCollector creates a new custom Prometheus collector
func NewCustomCollector(correlationProvider CorrelationProvider, systemHealthProvider SystemHealthProvider, config *CollectorConfig) *CustomCollector {
	if config == nil {
		config = DefaultCollectorConfig()
	}
	
	cc := &CustomCollector{
		correlationProvider:  correlationProvider,
		systemHealthProvider: systemHealthProvider,
		config:              config,
		cacheTimeout:        config.CacheTimeout,
		cachedMetrics:       make([]prometheus.Metric, 0),
	}
	
	cc.initializeDescriptors()
	return cc
}

// DefaultCollectorConfig returns default collector configuration
func DefaultCollectorConfig() *CollectorConfig {
	return &CollectorConfig{
		Namespace:             "tapio",
		Subsystem:             "collector",
		IncludeTimestamp:      true,
		IncludeInstanceInfo:   true,
		CacheTimeout:          30 * time.Second,
		MaxMetricsPerScrape:   1000,
		CollectionTimeout:     10 * time.Second,
		EnableRealTimeData:    true,
		EnableHistoricalData:  false,
		HistoricalLookback:    1 * time.Hour,
		EnablePatternMetrics:  true,
		EnableSystemMetrics:   true,
		EnableEntityMetrics:   true,
		ConstLabels:           make(map[string]string),
		DynamicLabels:         []string{"namespace", "node", "cluster"},
	}
}

// initializeDescriptors creates metric descriptors
func (cc *CustomCollector) initializeDescriptors() {
	// Correlation metrics
	cc.correlationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cc.config.Namespace, cc.config.Subsystem, "correlations_info"),
		"Information about active correlations",
		[]string{"rule_id", "rule_name", "severity", "category", "confidence_range"},
		cc.config.ConstLabels,
	)
	
	// Pattern metrics
	if cc.config.EnablePatternMetrics {
		cc.patternDesc = prometheus.NewDesc(
			prometheus.BuildFQName(cc.config.Namespace, cc.config.Subsystem, "patterns_info"),
			"Information about detected patterns",
			[]string{"pattern_type", "accuracy_range", "detection_rate"},
			cc.config.ConstLabels,
		)
	}
	
	// System health metrics
	if cc.config.EnableSystemMetrics {
		cc.systemHealthDesc = prometheus.NewDesc(
			prometheus.BuildFQName(cc.config.Namespace, cc.config.Subsystem, "system_health"),
			"Overall system health score",
			[]string{"component", "namespace", "cluster"},
			cc.config.ConstLabels,
		)
	}
	
	// Entity health metrics
	if cc.config.EnableEntityMetrics {
		cc.entityHealthDesc = prometheus.NewDesc(
			prometheus.BuildFQName(cc.config.Namespace, cc.config.Subsystem, "entity_health"),
			"Health score of individual entities",
			[]string{"entity_type", "entity_name", "namespace", "node"},
			cc.config.ConstLabels,
		)
	}
	
	// Processing latency
	cc.processingLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cc.config.Namespace, cc.config.Subsystem, "processing_latency_seconds"),
		"Processing latency for correlation analysis",
		[]string{"operation", "success"},
		cc.config.ConstLabels,
	)
}

// Describe implements prometheus.Collector interface
func (cc *CustomCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- cc.correlationDesc
	
	if cc.config.EnablePatternMetrics && cc.patternDesc != nil {
		ch <- cc.patternDesc
	}
	
	if cc.config.EnableSystemMetrics && cc.systemHealthDesc != nil {
		ch <- cc.systemHealthDesc
	}
	
	if cc.config.EnableEntityMetrics && cc.entityHealthDesc != nil {
		ch <- cc.entityHealthDesc
	}
	
	ch <- cc.processingLatencyDesc
}

// Collect implements prometheus.Collector interface
func (cc *CustomCollector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		// Record collection latency
		ch <- prometheus.MustNewConstMetric(
			cc.processingLatencyDesc,
			prometheus.GaugeValue,
			duration.Seconds(),
			"collection", "true",
		)
	}()
	
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	
	// Use cache if still valid
	if cc.cacheValid && time.Since(cc.lastScrape) < cc.cacheTimeout {
		for _, metric := range cc.cachedMetrics {
			ch <- metric
		}
		return
	}
	
	// Collect fresh metrics
	ctx, cancel := context.WithTimeout(context.Background(), cc.config.CollectionTimeout)
	defer cancel()
	
	metrics := make([]prometheus.Metric, 0, cc.config.MaxMetricsPerScrape)
	
	// Collect correlation metrics
	if correlationMetrics := cc.collectCorrelationMetrics(ctx); correlationMetrics != nil {
		metrics = append(metrics, correlationMetrics...)
	}
	
	// Collect pattern metrics
	if cc.config.EnablePatternMetrics {
		if patternMetrics := cc.collectPatternMetrics(ctx); patternMetrics != nil {
			metrics = append(metrics, patternMetrics...)
		}
	}
	
	// Collect system health metrics
	if cc.config.EnableSystemMetrics {
		if systemMetrics := cc.collectSystemMetrics(ctx); systemMetrics != nil {
			metrics = append(metrics, systemMetrics...)
		}
	}
	
	// Collect entity health metrics
	if cc.config.EnableEntityMetrics {
		if entityMetrics := cc.collectEntityMetrics(ctx); entityMetrics != nil {
			metrics = append(metrics, entityMetrics...)
		}
	}
	
	// Update cache
	cc.cachedMetrics = metrics
	cc.cacheValid = true
	cc.lastScrape = time.Now()
	
	// Send metrics
	for _, metric := range metrics {
		if len(metrics) >= cc.config.MaxMetricsPerScrape {
			break
		}
		ch <- metric
	}
}

// collectCorrelationMetrics collects correlation-related metrics
func (cc *CustomCollector) collectCorrelationMetrics(ctx context.Context) []prometheus.Metric {
	stats, err := cc.correlationProvider.GetCorrelationStats(ctx)
	if err != nil {
		// Record error metric
		return []prometheus.Metric{
			prometheus.MustNewConstMetric(
				cc.processingLatencyDesc,
				prometheus.GaugeValue,
				0,
				"correlation_collection", "false",
			),
		}
	}
	
	var metrics []prometheus.Metric
	
	// Create metrics for each category
	for category, count := range stats.ByCategory {
		for severity, sevCount := range stats.BySeverity {
			confidenceRange := cc.getConfidenceRange(stats.AvgConfidence)
			
			metric := prometheus.MustNewConstMetric(
				cc.correlationDesc,
				prometheus.GaugeValue,
				float64(count),
				"", // rule_id - aggregate
				category,
				severity,
				category,
				confidenceRange,
			)
			metrics = append(metrics, metric)
		}
	}
	
	return metrics
}

// collectPatternMetrics collects pattern detection metrics
func (cc *CustomCollector) collectPatternMetrics(ctx context.Context) []prometheus.Metric {
	if cc.patternDesc == nil {
		return nil
	}
	
	stats, err := cc.correlationProvider.GetPatternStats(ctx)
	if err != nil {
		return nil
	}
	
	var metrics []prometheus.Metric
	
	// Create metrics for each pattern type
	for patternType, count := range stats.ByPatternType {
		accuracyRange := cc.getAccuracyRange(stats.DetectionAccuracy)
		detectionRate := cc.calculateDetectionRate(stats.TruePositives, stats.FalsePositives)
		
		metric := prometheus.MustNewConstMetric(
			cc.patternDesc,
			prometheus.GaugeValue,
			float64(count),
			patternType,
			accuracyRange,
			detectionRate,
		)
		metrics = append(metrics, metric)
	}
	
	return metrics
}

// collectSystemMetrics collects system health metrics
func (cc *CustomCollector) collectSystemMetrics(ctx context.Context) []prometheus.Metric {
	if cc.systemHealthDesc == nil {
		return nil
	}
	
	health, err := cc.systemHealthProvider.GetSystemHealth(ctx)
	if err != nil {
		return nil
	}
	
	var metrics []prometheus.Metric
	
	// Overall health score
	overallMetric := prometheus.MustNewConstMetric(
		cc.systemHealthDesc,
		prometheus.GaugeValue,
		health.OverallScore,
		"overall",
		"", // namespace
		"", // cluster
	)
	metrics = append(metrics, overallMetric)
	
	// Component health scores
	for component, score := range health.ComponentScores {
		metric := prometheus.MustNewConstMetric(
			cc.systemHealthDesc,
			prometheus.GaugeValue,
			score,
			component,
			"", // namespace
			"", // cluster
		)
		metrics = append(metrics, metric)
	}
	
	return metrics
}

// collectEntityMetrics collects entity health metrics
func (cc *CustomCollector) collectEntityMetrics(ctx context.Context) []prometheus.Metric {
	if cc.entityHealthDesc == nil {
		return nil
	}
	
	entities, err := cc.systemHealthProvider.GetEntityHealth(ctx)
	if err != nil {
		return nil
	}
	
	var metrics []prometheus.Metric
	
	// Limit number of entity metrics to prevent explosion
	maxEntities := cc.config.MaxMetricsPerScrape / 4 // Reserve space for other metrics
	for i, entity := range entities {
		if i >= maxEntities {
			break
		}
		
		metric := prometheus.MustNewConstMetric(
			cc.entityHealthDesc,
			prometheus.GaugeValue,
			entity.HealthScore,
			entity.Entity.Type,
			entity.Entity.Name,
			entity.Entity.Namespace,
			entity.Entity.Node,
		)
		metrics = append(metrics, metric)
	}
	
	return metrics
}

// Helper methods

func (cc *CustomCollector) getConfidenceRange(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence >= 0.7:
		return "medium"
	case confidence >= 0.5:
		return "low"
	default:
		return "very_low"
	}
}

func (cc *CustomCollector) getAccuracyRange(accuracy float64) string {
	switch {
	case accuracy >= 0.95:
		return "excellent"
	case accuracy >= 0.85:
		return "good"
	case accuracy >= 0.7:
		return "fair"
	default:
		return "poor"
	}
}

func (cc *CustomCollector) calculateDetectionRate(truePositives, falsePositives int64) string {
	total := truePositives + falsePositives
	if total == 0 {
		return "no_data"
	}
	
	rate := float64(truePositives) / float64(total)
	switch {
	case rate >= 0.9:
		return "high"
	case rate >= 0.7:
		return "medium"
	default:
		return "low"
	}
}

// InvalidateCache forces cache invalidation
func (cc *CustomCollector) InvalidateCache() {
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	cc.cacheValid = false
}

// SetCacheTimeout updates the cache timeout
func (cc *CustomCollector) SetCacheTimeout(timeout time.Duration) {
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	cc.cacheTimeout = timeout
}

// GetLastScrapeTime returns the timestamp of the last successful scrape
func (cc *CustomCollector) GetLastScrapeTime() time.Time {
	cc.mutex.RLock()
	defer cc.mutex.RUnlock()
	return cc.lastScrape
}

// FactoryCollector creates collectors for specific Tapio components
type FactoryCollector struct {
	collectors map[string]*CustomCollector
	mutex      sync.RWMutex
}

// NewFactoryCollector creates a new factory for component-specific collectors
func NewFactoryCollector() *FactoryCollector {
	return &FactoryCollector{
		collectors: make(map[string]*CustomCollector),
	}
}

// RegisterCollector registers a collector for a specific component
func (fc *FactoryCollector) RegisterCollector(component string, collector *CustomCollector) {
	fc.mutex.Lock()
	defer fc.mutex.Unlock()
	fc.collectors[component] = collector
}

// GetCollector retrieves a collector for a specific component
func (fc *FactoryCollector) GetCollector(component string) (*CustomCollector, bool) {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()
	collector, exists := fc.collectors[component]
	return collector, exists
}

// GetAllCollectors returns all registered collectors
func (fc *FactoryCollector) GetAllCollectors() map[string]*CustomCollector {
	fc.mutex.RLock()
	defer fc.mutex.RUnlock()
	
	result := make(map[string]*CustomCollector)
	for k, v := range fc.collectors {
		result[k] = v
	}
	return result
}