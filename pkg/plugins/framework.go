package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Plugin defines the core plugin interface
type Plugin interface {
	// Identity
	Name() string
	Version() string
	Description() string
	
	// Lifecycle
	Initialize(ctx context.Context, config Config) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	
	// Health and Status
	HealthCheck(ctx context.Context) (*HealthStatus, error)
	GetMetrics() *PluginMetrics
	
	// Configuration
	GetConfigSchema() ConfigSchema
	ValidateConfig(config Config) error
	UpdateConfig(config Config) error
}

// ExportPlugin defines the export plugin interface
type ExportPlugin interface {
	Plugin
	
	// Export functionality
	Export(ctx context.Context, data ExportData) error
	SupportedFormats() []ExportFormat
	SupportedDataTypes() []DataType
	
	// Batching support
	SupportsBatching() bool
	GetBatchConfig() *BatchConfig
}

// CollectorPlugin defines the collector plugin interface  
type CollectorPlugin interface {
	Plugin
	
	// Collection functionality
	Collect(ctx context.Context) (<-chan CollectorData, error)
	GetCollectorConfig() *CollectorConfig
	
	// Data source management
	GetDataSources() []DataSource
	EnableDataSource(source DataSource) error
	DisableDataSource(source DataSource) error
}

// AnalysisPlugin defines the analysis plugin interface
type AnalysisPlugin interface {
	Plugin
	
	// Analysis functionality
	Analyze(ctx context.Context, data AnalysisData) (*AnalysisResult, error)
	GetSupportedAnalysisTypes() []AnalysisType
	
	// Pattern detection
	DetectPatterns(ctx context.Context, events []Event) ([]PatternResult, error)
	GetPatternTypes() []PatternType
}

// PluginManager manages plugin lifecycle and registry
type PluginManager struct {
	plugins    map[string]Plugin
	registry   *PluginRegistry
	lifecycle  *LifecycleManager
	health     *HealthMonitor
	metrics    *MetricsCollector
	mutex      sync.RWMutex
}

// PluginRegistry manages plugin discovery and loading
type PluginRegistry struct {
	plugins        map[string]*PluginInfo
	pluginDirs     []string
	autoReload     bool
	reloadInterval time.Duration
	mutex          sync.RWMutex
}

// LifecycleManager manages plugin lifecycle
type LifecycleManager struct {
	plugins     map[string]*PluginState
	startOrder  []string
	stopOrder   []string
	timeout     time.Duration
	mutex       sync.RWMutex
}

// HealthMonitor monitors plugin health
type HealthMonitor struct {
	plugins      map[string]*HealthStatus
	checkInterval time.Duration
	healthChecks  map[string]chan *HealthStatus
	mutex        sync.RWMutex
}

// MetricsCollector collects plugin metrics
type MetricsCollector struct {
	plugins   map[string]*PluginMetrics
	collectors map[string]func() *PluginMetrics
	interval  time.Duration
	mutex     sync.RWMutex
}

// Data structures
type Config map[string]interface{}

type ConfigSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required"`
}

type HealthStatus struct {
	Status    HealthStatusType `json:"status"`
	Message   string           `json:"message"`
	Timestamp time.Time        `json:"timestamp"`
	Details   map[string]interface{} `json:"details"`
}

type HealthStatusType string

const (
	HealthStatusHealthy   HealthStatusType = "healthy"
	HealthStatusUnhealthy HealthStatusType = "unhealthy"
	HealthStatusDegraded  HealthStatusType = "degraded"
	HealthStatusUnknown   HealthStatusType = "unknown"
)

type PluginMetrics struct {
	RequestsTotal    int64     `json:"requests_total"`
	RequestsSuccess  int64     `json:"requests_success"`
	RequestsFailed   int64     `json:"requests_failed"`
	AverageLatency   float64   `json:"average_latency_ms"`
	MemoryUsage      float64   `json:"memory_usage_mb"`
	CPUUsage         float64   `json:"cpu_usage_percent"`
	LastActivity     time.Time `json:"last_activity"`
	CustomMetrics    map[string]interface{} `json:"custom_metrics"`
}

type PluginInfo struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Type        PluginType        `json:"type"`
	Path        string            `json:"path"`
	Config      Config            `json:"config"`
	Enabled     bool              `json:"enabled"`
	Metadata    map[string]string `json:"metadata"`
}

type PluginType string

const (
	PluginTypeExport    PluginType = "export"
	PluginTypeCollector PluginType = "collector"
	PluginTypeAnalysis  PluginType = "analysis"
)

type PluginState struct {
	Status    PluginStatus `json:"status"`
	StartTime time.Time    `json:"start_time"`
	Error     error        `json:"error,omitempty"`
}

type PluginStatus string

const (
	PluginStatusStopped  PluginStatus = "stopped"
	PluginStatusStarting PluginStatus = "starting"
	PluginStatusRunning  PluginStatus = "running"
	PluginStatusStopping PluginStatus = "stopping"
	PluginStatusError    PluginStatus = "error"
)

// Export plugin types
type ExportFormat string

const (
	FormatJSON       ExportFormat = "json"
	FormatPrometheus ExportFormat = "prometheus"
	FormatOTEL       ExportFormat = "otel"
	FormatInfluxDB   ExportFormat = "influxdb"
	FormatElastic    ExportFormat = "elasticsearch"
)

type DataType string

const (
	DataTypeEvents      DataType = "events"
	DataTypeMetrics     DataType = "metrics"
	DataTypeLogs        DataType = "logs"
	DataTypeTraces      DataType = "traces"
	DataTypeCorrelation DataType = "correlation"
)

type ExportData struct {
	Type      DataType                `json:"type"`
	Format    ExportFormat            `json:"format"`
	Content   interface{}             `json:"content"`
	Metadata  map[string]interface{}  `json:"metadata"`
	Timestamp time.Time               `json:"timestamp"`
	Source    string                  `json:"source"`
	Tags      map[string]string       `json:"tags"`
	Callback  func(*ExportResult)     `json:"-"`
}

type ExportResult struct {
	Success  bool                   `json:"success"`
	Error    error                  `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
	Details  map[string]interface{} `json:"details"`
}

type BatchConfig struct {
	BatchSize    int           `json:"batch_size"`
	BatchTimeout time.Duration `json:"batch_timeout"`
	MaxQueueSize int           `json:"max_queue_size"`
}

// Collector plugin types
type CollectorData struct {
	Type      DataType               `json:"type"`
	Content   interface{}            `json:"content"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
	Tags      map[string]string      `json:"tags"`
}

type CollectorConfig struct {
	Interval     time.Duration `json:"interval"`
	BufferSize   int           `json:"buffer_size"`
	BatchSize    int           `json:"batch_size"`
	Timeout      time.Duration `json:"timeout"`
	Retries      int           `json:"retries"`
	RetryBackoff time.Duration `json:"retry_backoff"`
}

type DataSource struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Endpoint    string            `json:"endpoint"`
	Config      Config            `json:"config"`
	Enabled     bool              `json:"enabled"`
	Metadata    map[string]string `json:"metadata"`
}

// Analysis plugin types
type AnalysisData struct {
	Type      AnalysisType           `json:"type"`
	Content   interface{}            `json:"content"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
	Context   map[string]interface{} `json:"context"`
}

type AnalysisType string

const (
	AnalysisTypeCorrelation AnalysisType = "correlation"
	AnalysisTypeAnomaly     AnalysisType = "anomaly"
	AnalysisTypePattern     AnalysisType = "pattern"
	AnalysisTypeRootCause   AnalysisType = "root_cause"
)

type AnalysisResult struct {
	Type        AnalysisType           `json:"type"`
	Confidence  float64                `json:"confidence"`
	Results     []interface{}          `json:"results"`
	Insights    []string               `json:"insights"`
	Suggestions []string               `json:"suggestions"`
	Metadata    map[string]interface{} `json:"metadata"`
	Timestamp   time.Time              `json:"timestamp"`
}

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Tags      map[string]string      `json:"tags"`
}

type PatternResult struct {
	PatternID   string    `json:"pattern_id"`
	PatternName string    `json:"pattern_name"`
	Detected    bool      `json:"detected"`
	Confidence  float64   `json:"confidence"`
	Evidence    []Event   `json:"evidence"`
	Timestamp   time.Time `json:"timestamp"`
}

type PatternType struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	EventTypes  []string `json:"event_types"`
}

// NewPluginManager creates a new plugin manager
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins:   make(map[string]Plugin),
		registry:  NewPluginRegistry(),
		lifecycle: NewLifecycleManager(),
		health:    NewHealthMonitor(),
		metrics:   NewMetricsCollector(),
	}
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins:        make(map[string]*PluginInfo),
		pluginDirs:     []string{"./plugins", "/usr/local/lib/tapio/plugins"},
		autoReload:     true,
		reloadInterval: 5 * time.Minute,
	}
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		plugins: make(map[string]*PluginState),
		timeout: 30 * time.Second,
	}
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor() *HealthMonitor {
	return &HealthMonitor{
		plugins:       make(map[string]*HealthStatus),
		checkInterval: 30 * time.Second,
		healthChecks:  make(map[string]chan *HealthStatus),
	}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		plugins:    make(map[string]*PluginMetrics),
		collectors: make(map[string]func() *PluginMetrics),
		interval:   15 * time.Second,
	}
}

// Plugin Manager Methods

func (pm *PluginManager) RegisterPlugin(plugin Plugin) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	name := plugin.Name()
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}
	
	pm.plugins[name] = plugin
	return nil
}

func (pm *PluginManager) GetPlugin(name string) (Plugin, bool) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	plugin, exists := pm.plugins[name]
	return plugin, exists
}

func (pm *PluginManager) ListPlugins() []string {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}

func (pm *PluginManager) StartPlugin(ctx context.Context, name string) error {
	plugin, exists := pm.GetPlugin(name)
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}
	
	return pm.lifecycle.StartPlugin(ctx, name, plugin)
}

func (pm *PluginManager) StopPlugin(ctx context.Context, name string) error {
	plugin, exists := pm.GetPlugin(name)
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}
	
	return pm.lifecycle.StopPlugin(ctx, name, plugin)
}

func (pm *PluginManager) GetPluginHealth(name string) (*HealthStatus, error) {
	return pm.health.GetHealth(name)
}

func (pm *PluginManager) GetPluginMetrics(name string) (*PluginMetrics, error) {
	return pm.metrics.GetMetrics(name)
}

// Lifecycle Manager Methods

func (lm *LifecycleManager) StartPlugin(ctx context.Context, name string, plugin Plugin) error {
	lm.mutex.Lock()
	defer lm.mutex.Unlock()
	
	state := &PluginState{
		Status:    PluginStatusStarting,
		StartTime: time.Now(),
	}
	lm.plugins[name] = state
	
	// Start with timeout
	ctx, cancel := context.WithTimeout(ctx, lm.timeout)
	defer cancel()
	
	if err := plugin.Start(ctx); err != nil {
		state.Status = PluginStatusError
		state.Error = err
		return err
	}
	
	state.Status = PluginStatusRunning
	return nil
}

func (lm *LifecycleManager) StopPlugin(ctx context.Context, name string, plugin Plugin) error {
	lm.mutex.Lock()
	defer lm.mutex.Unlock()
	
	state, exists := lm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found in lifecycle manager", name)
	}
	
	state.Status = PluginStatusStopping
	
	// Stop with timeout
	ctx, cancel := context.WithTimeout(ctx, lm.timeout)
	defer cancel()
	
	if err := plugin.Stop(ctx); err != nil {
		state.Status = PluginStatusError
		state.Error = err
		return err
	}
	
	state.Status = PluginStatusStopped
	return nil
}

func (lm *LifecycleManager) GetPluginState(name string) (*PluginState, bool) {
	lm.mutex.RLock()
	defer lm.mutex.RUnlock()
	
	state, exists := lm.plugins[name]
	return state, exists
}

// Health Monitor Methods

func (hm *HealthMonitor) GetHealth(name string) (*HealthStatus, error) {
	hm.mutex.RLock()
	defer hm.mutex.RUnlock()
	
	health, exists := hm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found in health monitor", name)
	}
	
	return health, nil
}

func (hm *HealthMonitor) UpdateHealth(name string, status *HealthStatus) {
	hm.mutex.Lock()
	defer hm.mutex.Unlock()
	
	hm.plugins[name] = status
}

// Metrics Collector Methods

func (mc *MetricsCollector) GetMetrics(name string) (*PluginMetrics, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	
	metrics, exists := mc.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found in metrics collector", name)
	}
	
	return metrics, nil
}

func (mc *MetricsCollector) UpdateMetrics(name string, metrics *PluginMetrics) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	mc.plugins[name] = metrics
}