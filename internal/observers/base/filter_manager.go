// Package base provides filter management for Tapio observers
package base

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// FilterManager manages allow and deny filters for events
type FilterManager struct {
	// Named filters for debugging
	allowFilters map[string]FilterFunc
	denyFilters  map[string]FilterFunc
	mu           sync.RWMutex

	// Statistics
	eventsProcessed atomic.Int64
	eventsAllowed   atomic.Int64
	eventsDenied    atomic.Int64

	// Configuration
	configVersion atomic.Int32
	compiler      *FilterCompiler

	// File watching
	watcher  *fsnotify.Watcher
	stopChan chan struct{}

	// Logging
	collectorName string
	logger        *zap.Logger
}

// NewFilterManager creates a new filter manager
func NewFilterManager(collectorName string, logger *zap.Logger) *FilterManager {
	return &FilterManager{
		allowFilters:  make(map[string]FilterFunc),
		denyFilters:   make(map[string]FilterFunc),
		compiler:      NewFilterCompiler(logger),
		stopChan:      make(chan struct{}),
		collectorName: collectorName,
		logger:        logger,
	}
}

// LoadFromFile loads filter configuration from a YAML file
func (fm *FilterManager) LoadFromFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read filter config: %w", err)
	}

	var config FilterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse filter config: %w", err)
	}

	if err := fm.ApplyConfig(&config); err != nil {
		return fmt.Errorf("failed to apply filter config: %w", err)
	}

	if fm.logger != nil {
		fm.logger.Info("Loaded filter configuration",
			zap.String("collector", fm.collectorName),
			zap.String("path", configPath),
			zap.Int("version", config.Version),
			zap.Int("allow_filters", len(config.AllowFilters)),
			zap.Int("deny_filters", len(config.DenyFilters)))
	}

	return nil
}

// ApplyConfig applies a filter configuration
func (fm *FilterManager) ApplyConfig(config *FilterConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Clear existing filters
	fm.allowFilters = make(map[string]FilterFunc)
	fm.denyFilters = make(map[string]FilterFunc)

	// Compile and add allow filters
	for _, rule := range config.AllowFilters {
		if !rule.Enabled && rule.Name != "" {
			continue // Skip disabled rules
		}

		filter, err := fm.compiler.CompileRule(&rule)
		if err != nil {
			if fm.logger != nil {
				fm.logger.Warn("Failed to compile allow filter",
					zap.String("collector", fm.collectorName),
					zap.String("filter", rule.Name),
					zap.Error(err))
			}
			continue
		}

		filterName := rule.Name
		if filterName == "" {
			filterName = fmt.Sprintf("allow_%d", len(fm.allowFilters))
		}
		fm.allowFilters[filterName] = filter
	}

	// Compile and add deny filters
	for _, rule := range config.DenyFilters {
		if !rule.Enabled && rule.Name != "" {
			continue // Skip disabled rules
		}

		filter, err := fm.compiler.CompileRule(&rule)
		if err != nil {
			if fm.logger != nil {
				fm.logger.Warn("Failed to compile deny filter",
					zap.String("collector", fm.collectorName),
					zap.String("filter", rule.Name),
					zap.Error(err))
			}
			continue
		}

		filterName := rule.Name
		if filterName == "" {
			filterName = fmt.Sprintf("deny_%d", len(fm.denyFilters))
		}
		fm.denyFilters[filterName] = filter
	}

	// Update version
	fm.configVersion.Store(int32(config.Version))

	return nil
}

// WatchConfigFile watches a configuration file for changes
func (fm *FilterManager) WatchConfigFile(configPath string) error {
	// Initial load
	if err := fm.LoadFromFile(configPath); err != nil {
		return fmt.Errorf("initial config load failed: %w", err)
	}

	// Create watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	// Add config file to watcher
	if err := watcher.Add(configPath); err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch config file: %w", err)
	}

	fm.watcher = watcher

	// Start watching in background
	go fm.watchLoop(configPath)

	return nil
}

// watchLoop handles file system events for config file changes
func (fm *FilterManager) watchLoop(configPath string) {
	debounceTimer := time.NewTimer(0)
	<-debounceTimer.C // Drain initial tick

	for {
		select {
		case event, ok := <-fm.watcher.Events:
			if !ok {
				return
			}

			// Handle file changes with debouncing
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				// Reset timer for debouncing rapid changes
				debounceTimer.Reset(100 * time.Millisecond)

				select {
				case <-debounceTimer.C:
					// Reload configuration
					if err := fm.LoadFromFile(configPath); err != nil {
						if fm.logger != nil {
							fm.logger.Error("Failed to reload filter config",
								zap.String("collector", fm.collectorName),
								zap.String("path", configPath),
								zap.Error(err))
						}
					} else {
						if fm.logger != nil {
							fm.logger.Info("Reloaded filter configuration",
								zap.String("collector", fm.collectorName),
								zap.String("path", configPath))
						}
					}
				case <-fm.stopChan:
					return
				}
			}

		case err, ok := <-fm.watcher.Errors:
			if !ok {
				return
			}
			if fm.logger != nil {
				fm.logger.Error("Filter config watcher error",
					zap.String("collector", fm.collectorName),
					zap.Error(err))
			}

		case <-fm.stopChan:
			return
		}
	}
}

// ShouldAllow checks if an event should be allowed based on filters
func (fm *FilterManager) ShouldAllow(event *domain.CollectorEvent) bool {
	fm.eventsProcessed.Add(1)

	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// First check deny filters - if any match, deny the event
	for name, filter := range fm.denyFilters {
		if filter(event) {
			fm.eventsDenied.Add(1)
			if fm.logger != nil {
				fm.logger.Debug("Event denied by filter",
					zap.String("collector", fm.collectorName),
					zap.String("filter", name),
					zap.String("event_type", string(event.Type)))
			}
			return false
		}
	}

	// If no allow filters, allow by default
	if len(fm.allowFilters) == 0 {
		fm.eventsAllowed.Add(1)
		return true
	}

	// Check allow filters - at least one must match
	for name, filter := range fm.allowFilters {
		if filter(event) {
			fm.eventsAllowed.Add(1)
			if fm.logger != nil {
				fm.logger.Debug("Event allowed by filter",
					zap.String("collector", fm.collectorName),
					zap.String("filter", name),
					zap.String("event_type", string(event.Type)))
			}
			return true
		}
	}

	// No allow filter matched
	fm.eventsDenied.Add(1)
	return false
}

// AddAllowFilter adds a named allow filter at runtime
func (fm *FilterManager) AddAllowFilter(name string, filter FilterFunc) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.allowFilters[name] = filter

	if fm.logger != nil {
		fm.logger.Debug("Added allow filter",
			zap.String("collector", fm.collectorName),
			zap.String("filter", name))
	}
}

// AddDenyFilter adds a named deny filter at runtime
func (fm *FilterManager) AddDenyFilter(name string, filter FilterFunc) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.denyFilters[name] = filter

	if fm.logger != nil {
		fm.logger.Debug("Added deny filter",
			zap.String("collector", fm.collectorName),
			zap.String("filter", name))
	}
}

// RemoveFilter removes a filter by name
func (fm *FilterManager) RemoveFilter(name string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	deleted := false
	if _, exists := fm.allowFilters[name]; exists {
		delete(fm.allowFilters, name)
		deleted = true
	}
	if _, exists := fm.denyFilters[name]; exists {
		delete(fm.denyFilters, name)
		deleted = true
	}

	if deleted && fm.logger != nil {
		fm.logger.Debug("Removed filter",
			zap.String("collector", fm.collectorName),
			zap.String("filter", name))
	}
}

// GetStatistics returns filter statistics
func (fm *FilterManager) GetStatistics() FilterStatistics {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	return FilterStatistics{
		Version:         int(fm.configVersion.Load()),
		AllowFilters:    len(fm.allowFilters),
		DenyFilters:     len(fm.denyFilters),
		EventsProcessed: fm.eventsProcessed.Load(),
		EventsAllowed:   fm.eventsAllowed.Load(),
		EventsDenied:    fm.eventsDenied.Load(),
	}
}

// Stop stops the filter manager and any file watchers
func (fm *FilterManager) Stop() {
	close(fm.stopChan)
	if fm.watcher != nil {
		fm.watcher.Close()
	}
}
