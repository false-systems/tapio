package exports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Manager implements the ExportManager interface
type Manager struct {
	// Plugin registry
	plugins map[string]ExportPlugin
	mutex   sync.RWMutex

	// Export queue and workers
	queue     ExportQueue
	workers   *WorkerPool
	exports   map[string]*ExportResult
	exportMux sync.RWMutex

	// Configuration
	config       *ManagerConfig
	configPath   string
	configHash   string
	watchStop    chan struct{}
	hotReloadMux sync.Mutex

	// Health monitoring
	healthMonitor *HealthMonitor

	// Routing
	router ExportRouter

	// State
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// ManagerConfig contains manager configuration
type ManagerConfig struct {
	MaxWorkers          int           `yaml:"max_workers"`
	QueueSize           int           `yaml:"queue_size"`
	HotReloadEnabled    bool          `yaml:"hot_reload_enabled"`
	ConfigWatchPath     string        `yaml:"config_watch_path"`
	ReloadInterval      time.Duration `yaml:"reload_interval"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`

	// Plugin configurations
	Plugins []PluginConfig `yaml:"plugins"`

	// Routing rules
	Routes []ExportRoute `yaml:"routes"`
}

// PluginConfig contains plugin-specific configuration
type PluginConfig struct {
	Name     string                 `yaml:"name"`
	Type     string                 `yaml:"type"`
	Enabled  bool                   `yaml:"enabled"`
	Settings map[string]interface{} `yaml:"settings"`
}

// NewManager creates a new export manager
func NewManager(config *ManagerConfig) (*Manager, error) {
	if config == nil {
		config = DefaultManagerConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		plugins:   make(map[string]ExportPlugin),
		exports:   make(map[string]*ExportResult),
		config:    config,
		watchStop: make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Initialize components
	m.queue = NewMemoryQueue(config.QueueSize)
	m.workers = NewWorkerPool(config.MaxWorkers, config.QueueSize)
	m.healthMonitor = NewHealthMonitor(config.HealthCheckInterval)
	m.router = NewRouter()

	// Set worker result callback
	m.workers.SetResultCallback(m.handleWorkerResult)

	return m, nil
}

// DefaultManagerConfig returns default configuration
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		MaxWorkers:          10,
		QueueSize:           1000,
		HotReloadEnabled:    true,
		ConfigWatchPath:     "config/exports.yaml",
		ReloadInterval:      30 * time.Second,
		HealthCheckInterval: 60 * time.Second,
	}
}

// RegisterPlugin registers a new export plugin
func (m *Manager) RegisterPlugin(plugin ExportPlugin) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	name := plugin.Name()
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	m.plugins[name] = plugin

	// Start health monitoring for the plugin
	if m.running {
		m.healthMonitor.RegisterPlugin(name, plugin)

		// Perform immediate health check
		if health, err := plugin.HealthCheck(m.ctx); err == nil {
			m.healthMonitor.UpdateHealth(name, health)
		}
	}

	return nil
}

// UnregisterPlugin removes a plugin
func (m *Manager) UnregisterPlugin(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// Stop the plugin
	if err := plugin.Stop(m.ctx); err != nil {
		return fmt.Errorf("failed to stop plugin %s: %w", name, err)
	}

	delete(m.plugins, name)
	m.healthMonitor.UnregisterPlugin(name)

	return nil
}

// GetPlugin retrieves a plugin by name
func (m *Manager) GetPlugin(name string) (ExportPlugin, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin, nil
}

// ListPlugins returns all registered plugin names
func (m *Manager) ListPlugins() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// Export performs a synchronous export
func (m *Manager) Export(ctx context.Context, pluginName string, data ExportData) (*ExportResult, error) {
	plugin, err := m.GetPlugin(pluginName)
	if err != nil {
		return nil, err
	}

	// Generate export ID
	if data.ID == "" {
		data.ID = uuid.New().String()
	}

	result := &ExportResult{
		ID:         data.ID,
		Status:     StatusRunning,
		ExportedAt: time.Now(),
	}

	// Store result
	m.exportMux.Lock()
	m.exports[data.ID] = result
	m.exportMux.Unlock()

	// Perform export
	start := time.Now()
	err = plugin.Export(ctx, data)
	result.Duration = time.Since(start)

	if err != nil {
		result.Status = StatusFailed
		result.Error = err
		result.Message = err.Error()
	} else {
		result.Status = StatusSuccess
	}

	return result, err
}

// ExportAsync performs an asynchronous export
func (m *Manager) ExportAsync(ctx context.Context, pluginName string, data ExportData) (string, error) {
	// Verify plugin exists
	if _, err := m.GetPlugin(pluginName); err != nil {
		return "", err
	}

	// Generate export ID
	if data.ID == "" {
		data.ID = uuid.New().String()
	}

	// Create initial result
	result := &ExportResult{
		ID:     data.ID,
		Status: StatusPending,
	}

	// Store result
	m.exportMux.Lock()
	m.exports[data.ID] = result
	m.exportMux.Unlock()

	// Create job
	job := &ExportJob{
		ID:         data.ID,
		PluginName: pluginName,
		Data:       data,
		CreatedAt:  time.Now(),
	}

	// Submit to worker pool
	if err := m.workers.Submit(job); err != nil {
		result.Status = StatusFailed
		result.Error = err
		return "", err
	}

	return data.ID, nil
}

// GetExportStatus retrieves the status of an export
func (m *Manager) GetExportStatus(exportID string) (*ExportResult, error) {
	m.exportMux.RLock()
	defer m.exportMux.RUnlock()

	result, exists := m.exports[exportID]
	if !exists {
		return nil, fmt.Errorf("export %s not found", exportID)
	}

	return result, nil
}

// ConfigurePlugin configures a specific plugin
func (m *Manager) ConfigurePlugin(name string, config map[string]interface{}) error {
	plugin, err := m.GetPlugin(name)
	if err != nil {
		return err
	}

	// Configure the plugin
	if err := plugin.Configure(config); err != nil {
		return fmt.Errorf("failed to configure plugin %s: %w", name, err)
	}

	// Validate configuration
	if err := plugin.ValidateConfig(); err != nil {
		return fmt.Errorf("configuration validation failed for plugin %s: %w", name, err)
	}

	return nil
}

// GetPluginConfig retrieves plugin configuration
func (m *Manager) GetPluginConfig(name string) (map[string]interface{}, error) {
	plugin, err := m.GetPlugin(name)
	if err != nil {
		return nil, err
	}

	// Check if plugin supports dynamic configuration
	if configurable, ok := plugin.(ConfigurablePlugin); ok {
		return configurable.GetCurrentConfig(), nil
	}

	return nil, fmt.Errorf("plugin %s does not support configuration retrieval", name)
}

// GetPluginHealth retrieves health status for a plugin
func (m *Manager) GetPluginHealth(name string) (*HealthStatus, error) {
	return m.healthMonitor.GetPluginHealth(name)
}

// GetAllHealth retrieves health status for all plugins
func (m *Manager) GetAllHealth() map[string]*HealthStatus {
	return m.healthMonitor.GetAllHealth()
}

// Start starts the export manager
func (m *Manager) Start(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.running {
		return fmt.Errorf("manager already running")
	}

	// Start all enabled plugins
	for _, pluginConfig := range m.config.Plugins {
		if !pluginConfig.Enabled {
			continue
		}

		plugin, exists := m.plugins[pluginConfig.Name]
		if !exists {
			continue
		}

		// Configure plugin
		if err := plugin.Configure(pluginConfig.Settings); err != nil {
			return fmt.Errorf("failed to configure plugin %s: %w", pluginConfig.Name, err)
		}

		// Start plugin
		if err := plugin.Start(ctx); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", pluginConfig.Name, err)
		}

		// Register with health monitor
		m.healthMonitor.RegisterPlugin(pluginConfig.Name, plugin)
	}

	// Load routes
	for _, route := range m.config.Routes {
		if err := m.router.AddRoute(&route); err != nil {
			return fmt.Errorf("failed to add route %s: %w", route.ID, err)
		}
	}

	// Start components
	if err := m.workers.Start(ctx); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	if err := m.healthMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start health monitor: %w", err)
	}

	// Start hot reload if enabled
	if m.config.HotReloadEnabled && m.config.ConfigWatchPath != "" {
		go m.watchConfig(ctx)
	}

	m.running = true
	return nil
}

// Stop stops the export manager
func (m *Manager) Stop(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.running {
		return nil
	}

	// Stop hot reload
	close(m.watchStop)

	// Stop components
	m.workers.Stop()
	m.healthMonitor.Stop()

	// Stop all plugins
	var errors []error
	for name, plugin := range m.plugins {
		if err := plugin.Stop(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop plugin %s: %w", name, err))
		}
	}

	m.running = false
	m.cancel()

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping plugins: %v", errors)
	}

	return nil
}

// ReloadConfig reloads configuration from file
func (m *Manager) ReloadConfig(configPath string) error {
	m.hotReloadMux.Lock()
	defer m.hotReloadMux.Unlock()

	// Read configuration file
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse configuration
	var config ManagerConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Update configuration
	m.config = &config
	m.configPath = configPath

	// Reload plugin configurations
	for _, pluginConfig := range config.Plugins {
		plugin, exists := m.plugins[pluginConfig.Name]
		if !exists {
			continue
		}

		// Check if plugin supports hot reload
		if configurable, ok := plugin.(ConfigurablePlugin); ok {
			if err := configurable.ReloadConfig(pluginConfig.Settings); err != nil {
				return fmt.Errorf("failed to reload config for plugin %s: %w", pluginConfig.Name, err)
			}
		}
	}

	// Reload routes
	// Clear existing routes
	for _, route := range m.router.ListRoutes() {
		m.router.RemoveRoute(route.ID)
	}

	// Add new routes
	for _, route := range config.Routes {
		if err := m.router.AddRoute(&route); err != nil {
			return fmt.Errorf("failed to add route %s: %w", route.ID, err)
		}
	}

	return nil
}

// WatchConfig watches configuration file for changes
func (m *Manager) WatchConfig(ctx context.Context, configPath string) error {
	m.configPath = configPath

	// Load initial configuration
	if err := m.ReloadConfig(configPath); err != nil {
		return err
	}

	// Start watching in background
	go m.watchConfig(ctx)

	return nil
}

// watchConfig implements configuration file watching
func (m *Manager) watchConfig(ctx context.Context) {
	ticker := time.NewTicker(m.config.ReloadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.watchStop:
			return
		case <-ticker.C:
			m.checkAndReloadConfig(ctx)
		}
	}
}

// checkAndReloadConfig checks for configuration changes and reloads if needed
func (m *Manager) checkAndReloadConfig(ctx context.Context) {
	// Check if file exists
	info, err := os.Stat(m.configPath)
	if err != nil {
		return
	}

	// Read file content
	data, err := ioutil.ReadFile(m.configPath)
	if err != nil {
		return
	}

	// Calculate hash
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Check if content changed
	if hashStr != m.configHash {
		// Reload configuration
		if err := m.ReloadConfig(m.configPath); err == nil {
			m.configHash = hashStr
		}
	}
}

// handleWorkerResult handles results from worker pool
func (m *Manager) handleWorkerResult(job interface{}, result interface{}, err error) {
	exportJob, ok := job.(*ExportJob)
	if !ok {
		return
	}

	// Update export result
	m.exportMux.Lock()
	defer m.exportMux.Unlock()

	exportResult, exists := m.exports[exportJob.ID]
	if !exists {
		return
	}

	exportResult.ExportedAt = time.Now()
	exportResult.Duration = time.Since(exportJob.CreatedAt)

	if err != nil {
		exportResult.Status = StatusFailed
		exportResult.Error = err
		exportResult.Message = err.Error()
	} else {
		exportResult.Status = StatusSuccess
		if res, ok := result.(*ExportResult); ok {
			exportResult.BytesExported = res.BytesExported
		}
	}
}

// ExportJob represents an export job for the worker pool
type ExportJob struct {
	ID         string
	PluginName string
	Data       ExportData
	CreatedAt  time.Time
	Retries    int
}

// Execute implements the Job interface for ExportJob
func (j *ExportJob) Execute(ctx context.Context) (interface{}, error) {
	// This will be called by the worker pool
	// The actual execution is handled by the worker pool implementation
	return nil, nil
}
