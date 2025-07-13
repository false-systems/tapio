package exports

import (
	"context"
	"io"
	"time"
)

// DataType represents the type of data being exported
type DataType string

const (
	DataTypeDriftReport  DataType = "drift_report"
	DataTypeSnapshot     DataType = "snapshot"
	DataTypeCorrelation  DataType = "correlation"
	DataTypeMetrics      DataType = "metrics"
	DataTypeEvents       DataType = "events"
	DataTypePatternResult DataType = "pattern_result"
	DataTypeAutoFix      DataType = "autofix"
)

// ExportFormat represents the output format
type ExportFormat string

const (
	FormatJSON       ExportFormat = "json"
	FormatYAML       ExportFormat = "yaml"
	FormatMarkdown   ExportFormat = "markdown"
	FormatCSV        ExportFormat = "csv"
	FormatPrometheus ExportFormat = "prometheus"
	FormatOTEL       ExportFormat = "otel"
	FormatWebhook    ExportFormat = "webhook"
)

// ExportPlugin defines the interface that all export plugins must implement
type ExportPlugin interface {
	// Lifecycle management
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	
	// Configuration
	Configure(config map[string]interface{}) error
	ValidateConfig() error
	GetConfigSchema() map[string]interface{}
	
	// Export operations
	Export(ctx context.Context, data ExportData) error
	SupportedFormats() []ExportFormat
	SupportedDataTypes() []DataType
	
	// Health monitoring
	HealthCheck(ctx context.Context) (*HealthStatus, error)
	GetMetrics() map[string]interface{}
}

// ExportData contains the data to be exported
type ExportData struct {
	ID         string                 `json:"id"`
	Type       DataType               `json:"type"`
	Format     ExportFormat           `json:"format"`
	Data       interface{}            `json:"data"`
	Metadata   map[string]interface{} `json:"metadata"`
	Tags       map[string]string      `json:"tags"`
	Timestamp  time.Time              `json:"timestamp"`
}

// HealthStatus represents the health of a plugin
type HealthStatus struct {
	Healthy       bool                   `json:"healthy"`
	LastCheck     time.Time              `json:"last_check"`
	Message       string                 `json:"message,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
	ResourceUsage *ResourceUsage         `json:"resource_usage,omitempty"`
}

// ResourceUsage tracks plugin resource consumption
type ResourceUsage struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryMB      float64 `json:"memory_mb"`
	GoroutineCount int    `json:"goroutine_count"`
	ExportsPerSec float64 `json:"exports_per_sec"`
}

// ExportResult represents the result of an export operation
type ExportResult struct {
	ID            string        `json:"id"`
	Status        ExportStatus  `json:"status"`
	Message       string        `json:"message,omitempty"`
	ExportedAt    time.Time     `json:"exported_at"`
	Duration      time.Duration `json:"duration"`
	BytesExported int64         `json:"bytes_exported,omitempty"`
	Error         error         `json:"error,omitempty"`
}

// ExportStatus represents the status of an export operation
type ExportStatus string

const (
	StatusPending   ExportStatus = "pending"
	StatusRunning   ExportStatus = "running"
	StatusSuccess   ExportStatus = "success"
	StatusFailed    ExportStatus = "failed"
	StatusRetrying  ExportStatus = "retrying"
	StatusCancelled ExportStatus = "cancelled"
)

// ConfigurablePlugin extends ExportPlugin with advanced configuration
type ConfigurablePlugin interface {
	ExportPlugin
	
	// Hot reload support
	ReloadConfig(config map[string]interface{}) error
	GetCurrentConfig() map[string]interface{}
	
	// Dynamic configuration
	SetConfigValue(key string, value interface{}) error
	GetConfigValue(key string) (interface{}, error)
}

// BatchExporter supports batch export operations
type BatchExporter interface {
	ExportPlugin
	
	// Batch operations
	ExportBatch(ctx context.Context, batch []ExportData) error
	GetBatchSize() int
	SetBatchSize(size int) error
	FlushPending() error
}

// StreamExporter supports streaming export operations
type StreamExporter interface {
	ExportPlugin
	
	// Streaming operations
	OpenStream(ctx context.Context) (io.WriteCloser, error)
	StreamData(writer io.Writer, data ExportData) error
	CloseStream(writer io.WriteCloser) error
}

// TransformableExporter supports data transformation before export
type TransformableExporter interface {
	ExportPlugin
	
	// Transformation operations
	AddTransformer(name string, transformer DataTransformer) error
	RemoveTransformer(name string) error
	ListTransformers() []string
}

// DataTransformer transforms data before export
type DataTransformer interface {
	Transform(data interface{}) (interface{}, error)
	SupportedTypes() []DataType
}

// ExportManager manages all export plugins
type ExportManager interface {
	// Plugin management
	RegisterPlugin(plugin ExportPlugin) error
	UnregisterPlugin(name string) error
	GetPlugin(name string) (ExportPlugin, error)
	ListPlugins() []string
	
	// Export operations
	Export(ctx context.Context, pluginName string, data ExportData) (*ExportResult, error)
	ExportAsync(ctx context.Context, pluginName string, data ExportData) (string, error)
	GetExportStatus(exportID string) (*ExportResult, error)
	
	// Configuration
	ConfigurePlugin(name string, config map[string]interface{}) error
	GetPluginConfig(name string) (map[string]interface{}, error)
	
	// Health monitoring
	GetPluginHealth(name string) (*HealthStatus, error)
	GetAllHealth() map[string]*HealthStatus
	
	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	
	// Hot reload
	ReloadConfig(configPath string) error
	WatchConfig(ctx context.Context, configPath string) error
}

// ExportRouter routes data to appropriate plugins based on rules
type ExportRouter interface {
	// Route management
	AddRoute(route *ExportRoute) error
	RemoveRoute(routeID string) error
	GetRoute(routeID string) (*ExportRoute, error)
	ListRoutes() []*ExportRoute
	
	// Routing operations
	RouteData(ctx context.Context, data ExportData) ([]*RouteDecision, error)
	TestRoute(route *ExportRoute, data ExportData) (bool, error)
}

// ExportRoute defines a routing rule
type ExportRoute struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Pattern     *RoutePattern          `json:"pattern"`
	PluginName  string                 `json:"plugin_name"`
	Priority    int                    `json:"priority"`
	Enabled     bool                   `json:"enabled"`
	Transform   []string               `json:"transform,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RoutePattern defines matching criteria for routing
type RoutePattern struct {
	DataType   []DataType        `json:"data_type,omitempty"`
	Format     []ExportFormat    `json:"format,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Expression string            `json:"expression,omitempty"` // Advanced matching expression
}

// RouteDecision represents a routing decision
type RouteDecision struct {
	RouteID    string `json:"route_id"`
	PluginName string `json:"plugin_name"`
	Priority   int    `json:"priority"`
	Matched    bool   `json:"matched"`
}

// ExportQueue manages export operations
type ExportQueue interface {
	// Queue operations
	Enqueue(data ExportData) (string, error)
	Dequeue() (*ExportData, error)
	Peek() (*ExportData, error)
	Size() int
	Clear() error
	
	// Status tracking
	SetStatus(id string, status ExportStatus) error
	GetStatus(id string) (ExportStatus, error)
}

// PluginMetrics defines metrics that plugins should expose
type PluginMetrics struct {
	ExportsTotal      int64                  `json:"exports_total"`
	ExportsSuccess    int64                  `json:"exports_success"`
	ExportsFailed     int64                  `json:"exports_failed"`
	BytesExported     int64                  `json:"bytes_exported"`
	AvgExportTime     time.Duration          `json:"avg_export_time"`
	LastExportTime    time.Time              `json:"last_export_time"`
	LastError         string                 `json:"last_error,omitempty"`
	CustomMetrics     map[string]interface{} `json:"custom_metrics,omitempty"`
}

// Error types
type ExportError struct {
	Plugin    string
	Operation string
	Err       error
	Timestamp time.Time
	Retryable bool
}

func (e *ExportError) Error() string {
	return "export error in " + e.Plugin + " during " + e.Operation + ": " + e.Err.Error()
}

// Configuration validation
type ConfigSchema struct {
	Required   []string               `json:"required"`
	Properties map[string]PropSchema  `json:"properties"`
}

type PropSchema struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
}