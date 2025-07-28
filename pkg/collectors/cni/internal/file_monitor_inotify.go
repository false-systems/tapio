package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
)

// InotifyFileMonitor monitors CNI configuration file changes using inotify
type InotifyFileMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	watcher   *fsnotify.Watcher
	wg        sync.WaitGroup
	mu        sync.RWMutex

	// Track file states
	fileStates map[string]*fileState
}

type fileState struct {
	path         string
	lastModified time.Time
	pluginName   string
	checksum     string
}

// NewInotifyFileMonitor creates a new inotify-based file monitor
func NewInotifyFileMonitor(config core.Config) (*InotifyFileMonitor, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	monitor := &InotifyFileMonitor{
		config:     config,
		eventChan:  make(chan core.CNIRawEvent, 100),
		watcher:    watcher,
		fileStates: make(map[string]*fileState),
	}
	return monitor, nil
}

func (m *InotifyFileMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Add CNI config directories to watch
	configPaths := m.getCNIConfigPaths()
	for _, path := range configPaths {
		if err := m.addWatchPath(path); err != nil {
			// Log error but continue with other paths
			continue
		}
	}

	// Start event processing
	m.wg.Add(1)
	go m.processEvents()

	// Initial scan of existing files
	m.wg.Add(1)
	go m.initialScan()

	return nil
}

func (m *InotifyFileMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	// Close watcher
	if m.watcher != nil {
		m.watcher.Close()
	}

	// Wait for goroutines to finish
	m.wg.Wait()

	close(m.eventChan)
	return nil
}

func (m *InotifyFileMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *InotifyFileMonitor) MonitorType() string {
	return "inotify-file"
}

func (m *InotifyFileMonitor) getCNIConfigPaths() []string {
	paths := []string{
		"/etc/cni/net.d",
		"/etc/cni/conf.d",
		"/opt/cni/conf",
		"/tmp", // For testing
	}

	if m.config.CNIConfPath != "" {
		paths = append([]string{m.config.CNIConfPath}, paths...)
	}

	// Filter out non-existent paths
	var validPaths []string
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			validPaths = append(validPaths, path)
		}
	}

	return validPaths
}

func (m *InotifyFileMonitor) addWatchPath(path string) error {
	// Add directory watch
	if err := m.watcher.Add(path); err != nil {
		return fmt.Errorf("failed to watch path %s: %w", path, err)
	}

	// Also watch subdirectories
	return filepath.Walk(path, func(subpath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip on error
		}
		if info.IsDir() && subpath != path {
			m.watcher.Add(subpath)
		}
		return nil
	})
}

func (m *InotifyFileMonitor) processEvents() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return

		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.handleFSEvent(event)

		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			// Log error but continue processing
			m.sendErrorEvent(fmt.Errorf("inotify error: %w", err))
		}
	}
}

func (m *InotifyFileMonitor) handleFSEvent(event fsnotify.Event) {
	// Filter for CNI config files
	if !m.isCNIConfigFile(event.Name) {
		return
	}

	var cniEvent *core.CNIRawEvent

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		cniEvent = m.handleFileCreate(event.Name)
	case event.Op&fsnotify.Write == fsnotify.Write:
		cniEvent = m.handleFileWrite(event.Name)
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		cniEvent = m.handleFileRemove(event.Name)
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		cniEvent = m.handleFileRename(event.Name)
	}

	if cniEvent != nil {
		select {
		case m.eventChan <- *cniEvent:
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *InotifyFileMonitor) isCNIConfigFile(path string) bool {
	// Check if it's a CNI config file
	ext := filepath.Ext(path)
	if ext != ".conf" && ext != ".conflist" && ext != ".json" {
		return false
	}

	// Check if it's in a CNI directory
	dir := filepath.Dir(path)
	for _, configPath := range m.getCNIConfigPaths() {
		if strings.HasPrefix(dir, configPath) {
			return true
		}
	}

	return false
}

func (m *InotifyFileMonitor) handleFileCreate(path string) *core.CNIRawEvent {
	// Read and parse the new config
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	pluginName := m.extractPluginFromConfig(content)

	// Update file state
	m.mu.Lock()
	m.fileStates[path] = &fileState{
		path:         path,
		lastModified: time.Now(),
		pluginName:   pluginName,
		checksum:     m.calculateChecksum(content),
	}
	m.mu.Unlock()

	return &core.CNIRawEvent{
		ID:         fmt.Sprintf("inotify_create_%s_%d", filepath.Base(path), time.Now().UnixNano()),
		Timestamp:  time.Now(),
		Source:     "inotify-file",
		Operation:  core.CNIOperationOther,
		Success:    true,
		PluginName: pluginName,
		RawConfig:  string(content),
		Annotations: map[string]string{
			"event_type": "config_created",
			"file_path":  path,
			"file_name":  filepath.Base(path),
		},
	}
}

func (m *InotifyFileMonitor) handleFileWrite(path string) *core.CNIRawEvent {
	// Read the updated config
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	newChecksum := m.calculateChecksum(content)
	pluginName := m.extractPluginFromConfig(content)

	// Check if content actually changed
	m.mu.RLock()
	state, exists := m.fileStates[path]
	m.mu.RUnlock()

	if exists && state.checksum == newChecksum {
		return nil // No actual change in content
	}

	// Update file state
	m.mu.Lock()
	m.fileStates[path] = &fileState{
		path:         path,
		lastModified: time.Now(),
		pluginName:   pluginName,
		checksum:     newChecksum,
	}
	m.mu.Unlock()

	annotations := map[string]string{
		"event_type": "config_modified",
		"file_path":  path,
		"file_name":  filepath.Base(path),
	}

	if exists {
		annotations["previous_plugin"] = state.pluginName
	}

	return &core.CNIRawEvent{
		ID:          fmt.Sprintf("inotify_write_%s_%d", filepath.Base(path), time.Now().UnixNano()),
		Timestamp:   time.Now(),
		Source:      "inotify-file",
		Operation:   core.CNIOperationOther,
		Success:     true,
		PluginName:  pluginName,
		RawConfig:   string(content),
		Annotations: annotations,
	}
}

func (m *InotifyFileMonitor) handleFileRemove(path string) *core.CNIRawEvent {
	m.mu.Lock()
	state, exists := m.fileStates[path]
	if exists {
		delete(m.fileStates, path)
	}
	m.mu.Unlock()

	pluginName := "unknown"
	if state != nil {
		pluginName = state.pluginName
	}

	return &core.CNIRawEvent{
		ID:         fmt.Sprintf("inotify_remove_%s_%d", filepath.Base(path), time.Now().UnixNano()),
		Timestamp:  time.Now(),
		Source:     "inotify-file",
		Operation:  core.CNIOperationOther,
		Success:    true,
		PluginName: pluginName,
		Annotations: map[string]string{
			"event_type": "config_removed",
			"file_path":  path,
			"file_name":  filepath.Base(path),
		},
	}
}

func (m *InotifyFileMonitor) handleFileRename(path string) *core.CNIRawEvent {
	// Rename is often followed by create/remove events
	// We'll handle it as a remove for now
	return m.handleFileRemove(path)
}

func (m *InotifyFileMonitor) initialScan() {
	defer m.wg.Done()

	for _, path := range m.getCNIConfigPaths() {
		m.scanDirectory(path)
	}
}

func (m *InotifyFileMonitor) scanDirectory(dir string) {
	patterns := []string{
		filepath.Join(dir, "*.conf"),
		filepath.Join(dir, "*.conflist"),
		filepath.Join(dir, "*.json"),
	}

	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				continue
			}

			info, err := os.Stat(file)
			if err != nil {
				continue
			}

			pluginName := m.extractPluginFromConfig(content)

			// Store initial state
			m.mu.Lock()
			m.fileStates[file] = &fileState{
				path:         file,
				lastModified: info.ModTime(),
				pluginName:   pluginName,
				checksum:     m.calculateChecksum(content),
			}
			m.mu.Unlock()

			// Send initial discovery event
			event := &core.CNIRawEvent{
				ID:         fmt.Sprintf("inotify_init_%s_%d", filepath.Base(file), time.Now().UnixNano()),
				Timestamp:  info.ModTime(),
				Source:     "inotify-file",
				Operation:  core.CNIOperationOther,
				Success:    true,
				PluginName: pluginName,
				RawConfig:  string(content),
				Annotations: map[string]string{
					"event_type": "config_discovered",
					"file_path":  file,
					"file_name":  filepath.Base(file),
				},
			}

			select {
			case m.eventChan <- *event:
			case <-m.ctx.Done():
				return
			}
		}
	}
}

func (m *InotifyFileMonitor) extractPluginFromConfig(content []byte) string {
	configStr := string(content)

	// Check for common CNI plugin identifiers
	plugins := map[string][]string{
		"cilium":     {"cilium", "cilium-cni"},
		"calico":     {"calico", "calico-ipam"},
		"flannel":    {"flannel"},
		"weave":      {"weave", "weave-net"},
		"bridge":     {"bridge", "cni-bridge"},
		"macvlan":    {"macvlan"},
		"ipvlan":     {"ipvlan"},
		"ptp":        {"ptp"},
		"host-local": {"host-local"},
		"dhcp":       {"dhcp"},
	}

	lowerConfig := strings.ToLower(configStr)
	for plugin, identifiers := range plugins {
		for _, id := range identifiers {
			if strings.Contains(lowerConfig, id) {
				return plugin
			}
		}
	}

	// Try to extract from "type" field in JSON
	if idx := strings.Index(configStr, `"type"`); idx >= 0 {
		rest := configStr[idx+6:]
		if colonIdx := strings.Index(rest, ":"); colonIdx >= 0 {
			value := strings.TrimSpace(rest[colonIdx+1:])
			if quoteIdx := strings.Index(value, `"`); quoteIdx >= 0 {
				value = value[quoteIdx+1:]
				if endQuoteIdx := strings.Index(value, `"`); endQuoteIdx >= 0 {
					return value[:endQuoteIdx]
				}
			}
		}
	}

	return "unknown"
}

func (m *InotifyFileMonitor) calculateChecksum(content []byte) string {
	// Simple checksum for change detection
	// In production, use crypto/sha256
	sum := uint32(0)
	for _, b := range content {
		sum = sum*31 + uint32(b)
	}
	return fmt.Sprintf("%x", sum)
}

func (m *InotifyFileMonitor) sendErrorEvent(err error) {
	event := &core.CNIRawEvent{
		ID:           fmt.Sprintf("inotify_error_%d", time.Now().UnixNano()),
		Timestamp:    time.Now(),
		Source:       "inotify-file",
		Operation:    core.CNIOperationOther,
		Success:      false,
		ErrorMessage: err.Error(),
		Annotations: map[string]string{
			"event_type": "monitor_error",
			"error":      err.Error(),
		},
	}

	select {
	case m.eventChan <- *event:
	case <-m.ctx.Done():
		return
	}
}
