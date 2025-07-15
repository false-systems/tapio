package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// SDK provides utilities for plugin development
type SDK struct {
	pluginName string
	logger     *Logger
	config     Config
	metrics    *MetricsTracker
	health     *HealthTracker
}

// Logger provides structured logging for plugins
type Logger struct {
	pluginName string
	level      LogLevel
}

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// MetricsTracker tracks plugin metrics
type MetricsTracker struct {
	pluginName string
	metrics    *PluginMetrics
}

// HealthTracker tracks plugin health
type HealthTracker struct {
	pluginName string
	status     *HealthStatus
}

// BasePlugin provides common plugin functionality
type BasePlugin struct {
	name        string
	version     string
	description string
	config      Config
	sdk         *SDK
	started     bool
	metrics     *PluginMetrics
	health      *HealthStatus
}

// NewSDK creates a new plugin SDK
func NewSDK(pluginName string) *SDK {
	return &SDK{
		pluginName: pluginName,
		logger:     NewLogger(pluginName),
		config:     make(Config),
		metrics:    NewMetricsTracker(pluginName),
		health:     NewHealthTracker(pluginName),
	}
}

// NewLogger creates a new plugin logger
func NewLogger(pluginName string) *Logger {
	return &Logger{
		pluginName: pluginName,
		level:      LogLevelInfo,
	}
}

// NewMetricsTracker creates a new metrics tracker
func NewMetricsTracker(pluginName string) *MetricsTracker {
	return &MetricsTracker{
		pluginName: pluginName,
		metrics: &PluginMetrics{
			CustomMetrics: make(map[string]interface{}),
		},
	}
}

// NewHealthTracker creates a new health tracker
func NewHealthTracker(pluginName string) *HealthTracker {
	return &HealthTracker{
		pluginName: pluginName,
		status: &HealthStatus{
			Status:    HealthStatusHealthy,
			Message:   "Plugin initialized",
			Timestamp: time.Now(),
			Details:   make(map[string]interface{}),
		},
	}
}

// NewBasePlugin creates a new base plugin
func NewBasePlugin(name, version, description string) *BasePlugin {
	return &BasePlugin{
		name:        name,
		version:     version,
		description: description,
		config:      make(Config),
		sdk:         NewSDK(name),
		metrics: &PluginMetrics{
			CustomMetrics: make(map[string]interface{}),
		},
		health: &HealthStatus{
			Status:    HealthStatusHealthy,
			Message:   "Plugin ready",
			Timestamp: time.Now(),
			Details:   make(map[string]interface{}),
		},
	}
}

// SDK Methods

func (sdk *SDK) Log() *Logger {
	return sdk.logger
}

func (sdk *SDK) Metrics() *MetricsTracker {
	return sdk.metrics
}

func (sdk *SDK) Health() *HealthTracker {
	return sdk.health
}

func (sdk *SDK) LoadConfig(configPath string) error {
	if configPath == "" {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, &sdk.config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	return nil
}

func (sdk *SDK) GetConfig() Config {
	return sdk.config
}

func (sdk *SDK) SetConfig(config Config) {
	sdk.config = config
}

// Logger Methods

func (l *Logger) Debug(msg string, args ...interface{}) {
	if l.level == LogLevelDebug {
		l.log("DEBUG", msg, args...)
	}
}

func (l *Logger) Info(msg string, args ...interface{}) {
	if l.level == LogLevelDebug || l.level == LogLevelInfo {
		l.log("INFO", msg, args...)
	}
}

func (l *Logger) Warn(msg string, args ...interface{}) {
	if l.level != LogLevelError {
		l.log("WARN", msg, args...)
	}
}

func (l *Logger) Error(msg string, args ...interface{}) {
	l.log("ERROR", msg, args...)
}

func (l *Logger) log(level, msg string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formatted := fmt.Sprintf(msg, args...)
	log.Printf("[%s] [%s] [%s] %s", timestamp, level, l.pluginName, formatted)
}

func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// MetricsTracker Methods

func (mt *MetricsTracker) IncrementCounter(name string, value int64) {
	switch name {
	case "requests_total":
		mt.metrics.RequestsTotal += value
	case "requests_success":
		mt.metrics.RequestsSuccess += value
	case "requests_failed":
		mt.metrics.RequestsFailed += value
	default:
		mt.metrics.CustomMetrics[name] = value
	}
}

func (mt *MetricsTracker) SetGauge(name string, value float64) {
	switch name {
	case "memory_usage":
		mt.metrics.MemoryUsage = value
	case "cpu_usage":
		mt.metrics.CPUUsage = value
	case "average_latency":
		mt.metrics.AverageLatency = value
	default:
		mt.metrics.CustomMetrics[name] = value
	}
}

func (mt *MetricsTracker) RecordActivity() {
	mt.metrics.LastActivity = time.Now()
}

func (mt *MetricsTracker) GetMetrics() *PluginMetrics {
	return mt.metrics
}

// HealthTracker Methods

func (ht *HealthTracker) SetHealthy(message string) {
	ht.status.Status = HealthStatusHealthy
	ht.status.Message = message
	ht.status.Timestamp = time.Now()
}

func (ht *HealthTracker) SetUnhealthy(message string) {
	ht.status.Status = HealthStatusUnhealthy
	ht.status.Message = message
	ht.status.Timestamp = time.Now()
}

func (ht *HealthTracker) SetDegraded(message string) {
	ht.status.Status = HealthStatusDegraded
	ht.status.Message = message
	ht.status.Timestamp = time.Now()
}

func (ht *HealthTracker) AddDetail(key string, value interface{}) {
	ht.status.Details[key] = value
}

func (ht *HealthTracker) GetHealth() *HealthStatus {
	return ht.status
}

// BasePlugin Methods

func (bp *BasePlugin) Name() string {
	return bp.name
}

func (bp *BasePlugin) Version() string {
	return bp.version
}

func (bp *BasePlugin) Description() string {
	return bp.description
}

func (bp *BasePlugin) Initialize(ctx context.Context, config Config) error {
	bp.config = config
	bp.sdk.SetConfig(config)
	bp.sdk.Log().Info("Plugin initialized with config")
	return nil
}

func (bp *BasePlugin) Start(ctx context.Context) error {
	if bp.started {
		return fmt.Errorf("plugin already started")
	}
	
	bp.started = true
	bp.sdk.Health().SetHealthy("Plugin started successfully")
	bp.sdk.Log().Info("Plugin started")
	return nil
}

func (bp *BasePlugin) Stop(ctx context.Context) error {
	if !bp.started {
		return fmt.Errorf("plugin not started")
	}
	
	bp.started = false
	bp.sdk.Health().SetHealthy("Plugin stopped")
	bp.sdk.Log().Info("Plugin stopped")
	return nil
}

func (bp *BasePlugin) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	return bp.sdk.Health().GetHealth(), nil
}

func (bp *BasePlugin) GetMetrics() *PluginMetrics {
	return bp.sdk.Metrics().GetMetrics()
}

func (bp *BasePlugin) GetConfigSchema() ConfigSchema {
	return ConfigSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"enabled": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable or disable the plugin",
				"default":     true,
			},
			"debug": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable debug logging",
				"default":     false,
			},
		},
		Required: []string{},
	}
}

func (bp *BasePlugin) ValidateConfig(config Config) error {
	// Basic validation - can be overridden by specific plugins
	if enabled, ok := config["enabled"].(bool); ok && !enabled {
		return fmt.Errorf("plugin is disabled")
	}
	return nil
}

func (bp *BasePlugin) UpdateConfig(config Config) error {
	if err := bp.ValidateConfig(config); err != nil {
		return err
	}
	
	bp.config = config
	bp.sdk.SetConfig(config)
	bp.sdk.Log().Info("Plugin config updated")
	return nil
}

// SDK returns the plugin SDK
func (bp *BasePlugin) SDK() *SDK {
	return bp.sdk
}

// Helper functions for plugin development

// MustGetString gets a string config value or panics
func MustGetString(config Config, key string) string {
	if value, ok := config[key].(string); ok {
		return value
	}
	panic(fmt.Sprintf("required config key %s not found or not a string", key))
}

// GetString gets a string config value with default
func GetString(config Config, key, defaultValue string) string {
	if value, ok := config[key].(string); ok {
		return value
	}
	return defaultValue
}

// MustGetInt gets an int config value or panics
func MustGetInt(config Config, key string) int {
	if value, ok := config[key].(int); ok {
		return value
	}
	if value, ok := config[key].(float64); ok {
		return int(value)
	}
	panic(fmt.Sprintf("required config key %s not found or not an integer", key))
}

// GetInt gets an int config value with default
func GetInt(config Config, key string, defaultValue int) int {
	if value, ok := config[key].(int); ok {
		return value
	}
	if value, ok := config[key].(float64); ok {
		return int(value)
	}
	return defaultValue
}

// MustGetBool gets a bool config value or panics
func MustGetBool(config Config, key string) bool {
	if value, ok := config[key].(bool); ok {
		return value
	}
	panic(fmt.Sprintf("required config key %s not found or not a boolean", key))
}

// GetBool gets a bool config value with default
func GetBool(config Config, key string, defaultValue bool) bool {
	if value, ok := config[key].(bool); ok {
		return value
	}
	return defaultValue
}

// GetDuration gets a duration config value with default
func GetDuration(config Config, key string, defaultValue time.Duration) time.Duration {
	if value, ok := config[key].(string); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// ValidateRequired validates that required config keys are present
func ValidateRequired(config Config, keys ...string) error {
	for _, key := range keys {
		if _, ok := config[key]; !ok {
			return fmt.Errorf("required config key %s is missing", key)
		}
	}
	return nil
}