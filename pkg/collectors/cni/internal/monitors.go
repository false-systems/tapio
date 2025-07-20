package internal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
)

// LogMonitor monitors CNI plugin logs
type LogMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	logPaths  []string
}

// NewLogMonitor creates a new log monitor
func NewLogMonitor(config core.Config) (*LogMonitor, error) {
	monitor := &LogMonitor{
		config:    config,
		eventChan: make(chan core.CNIRawEvent, 100),
		logPaths:  []string{"/var/log/cni.log", "/var/log/pods/"},
	}
	return monitor, nil
}

func (m *LogMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start monitoring each log path
	for _, logPath := range m.logPaths {
		if _, err := os.Stat(logPath); err == nil {
			go m.monitorLogPath(logPath)
		}
	}

	return nil
}

func (m *LogMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	close(m.eventChan)
	return nil
}

func (m *LogMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *LogMonitor) MonitorType() string {
	return "log"
}

func (m *LogMonitor) monitorLogPath(logPath string) {
	// Simple log parsing - in production, this would use tools like tail -f
	// or file watchers for real-time monitoring
	cmd := exec.CommandContext(m.ctx, "tail", "-f", logPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if event := m.parseLogLine(line); event != nil {
			select {
			case m.eventChan <- *event:
			case <-m.ctx.Done():
				return
			}
		}
	}
}

func (m *LogMonitor) parseLogLine(line string) *core.CNIRawEvent {
	// Parse CNI log entries - this is a simplified parser
	// Real implementation would parse structured logs from different CNI plugins
	if !strings.Contains(line, "CNI") {
		return nil
	}

	event := &core.CNIRawEvent{
		ID:        fmt.Sprintf("log_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    "log",
	}

	// Extract operation
	if strings.Contains(line, "ADD") {
		event.Operation = core.CNIOperationAdd
	} else if strings.Contains(line, "DEL") {
		event.Operation = core.CNIOperationDel
	} else if strings.Contains(line, "CHECK") {
		event.Operation = core.CNIOperationCheck
	}

	// Extract success/failure
	event.Success = !strings.Contains(line, "error") && !strings.Contains(line, "failed")

	// Extract plugin name
	if idx := strings.Index(line, "plugin="); idx >= 0 {
		rest := line[idx+7:]
		if spaceIdx := strings.Index(rest, " "); spaceIdx >= 0 {
			event.PluginName = rest[:spaceIdx]
		} else {
			event.PluginName = rest
		}
	}

	return event
}

// ProcessMonitor monitors CNI binary executions
type ProcessMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewProcessMonitor creates a new process monitor
func NewProcessMonitor(config core.Config) (*ProcessMonitor, error) {
	monitor := &ProcessMonitor{
		config:    config,
		eventChan: make(chan core.CNIRawEvent, 100),
	}
	return monitor, nil
}

func (m *ProcessMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Monitor process execution using ps or similar tools
	go m.monitorProcesses()

	return nil
}

func (m *ProcessMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	close(m.eventChan)
	return nil
}

func (m *ProcessMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *ProcessMonitor) MonitorType() string {
	return "process"
}

func (m *ProcessMonitor) monitorProcesses() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.scanForCNIProcesses()
		}
	}
}

func (m *ProcessMonitor) scanForCNIProcesses() {
	// Use ps to find CNI processes
	cmd := exec.CommandContext(m.ctx, "ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if m.isCNIProcess(line) {
			if event := m.parseProcessLine(line); event != nil {
				select {
				case m.eventChan <- *event:
				case <-m.ctx.Done():
					return
				}
			}
		}
	}
}

func (m *ProcessMonitor) isCNIProcess(line string) bool {
	return strings.Contains(line, "/opt/cni/bin/") ||
		strings.Contains(line, "cilium") ||
		strings.Contains(line, "calico") ||
		strings.Contains(line, "flannel")
}

func (m *ProcessMonitor) parseProcessLine(line string) *core.CNIRawEvent {
	fields := strings.Fields(line)
	if len(fields) < 11 {
		return nil
	}

	event := &core.CNIRawEvent{
		ID:        fmt.Sprintf("proc_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    "process",
		Success:   true, // Assume success if process is running
	}

	// Parse command
	event.Command = strings.Join(fields[10:], " ")

	// Extract plugin name from command path
	for _, field := range fields {
		if strings.Contains(field, "/opt/cni/bin/") {
			event.PluginName = filepath.Base(field)
			break
		}
	}

	return event
}

// EventMonitor monitors Kubernetes CNI events
type EventMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewEventMonitor creates a new Kubernetes event monitor
func NewEventMonitor(config core.Config) (*EventMonitor, error) {
	monitor := &EventMonitor{
		config:    config,
		eventChan: make(chan core.CNIRawEvent, 100),
	}
	return monitor, nil
}

func (m *EventMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Monitor Kubernetes events related to networking
	go m.monitorKubernetesEvents()

	return nil
}

func (m *EventMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	close(m.eventChan)
	return nil
}

func (m *EventMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *EventMonitor) MonitorType() string {
	return "event"
}

func (m *EventMonitor) monitorKubernetesEvents() {
	// Use kubectl to watch for networking events
	var cmd *exec.Cmd
	if m.config.Namespace != "" {
		cmd = exec.CommandContext(m.ctx, "kubectl", "get", "events",
			"--watch", "--namespace", m.config.Namespace, "--output", "json")
	} else {
		cmd = exec.CommandContext(m.ctx, "kubectl", "get", "events",
			"--watch", "--all-namespaces", "--output", "json")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if event := m.parseKubernetesEvent(line); event != nil {
			select {
			case m.eventChan <- *event:
			case <-m.ctx.Done():
				return
			}
		}
	}
}

func (m *EventMonitor) parseKubernetesEvent(jsonLine string) *core.CNIRawEvent {
	// Parse Kubernetes event JSON - simplified implementation
	// Real implementation would use proper JSON parsing
	if !strings.Contains(jsonLine, "NetworkPolicy") &&
		!strings.Contains(jsonLine, "CNI") &&
		!strings.Contains(jsonLine, "network") {
		return nil
	}

	event := &core.CNIRawEvent{
		ID:        fmt.Sprintf("k8s_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    "k8s-event",
		Success:   !strings.Contains(jsonLine, "Warning"),
	}

	// Extract pod information if available
	if strings.Contains(jsonLine, "Pod") {
		// Simple extraction - real implementation would parse JSON properly
		if idx := strings.Index(jsonLine, "\"name\":\""); idx >= 0 {
			rest := jsonLine[idx+8:]
			if endIdx := strings.Index(rest, "\""); endIdx >= 0 {
				event.PodName = rest[:endIdx]
			}
		}
	}

	return event
}

// FileMonitor monitors CNI configuration file changes
type FileMonitor struct {
	config    core.Config
	eventChan chan core.CNIRawEvent
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewFileMonitor creates a new file monitor
func NewFileMonitor(config core.Config) (*FileMonitor, error) {
	monitor := &FileMonitor{
		config:    config,
		eventChan: make(chan core.CNIRawEvent, 100),
	}
	return monitor, nil
}

func (m *FileMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Monitor CNI configuration directory for changes
	go m.monitorConfigDirectory()

	return nil
}

func (m *FileMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	close(m.eventChan)
	return nil
}

func (m *FileMonitor) Events() <-chan core.CNIRawEvent {
	return m.eventChan
}

func (m *FileMonitor) MonitorType() string {
	return "file"
}

func (m *FileMonitor) monitorConfigDirectory() {
	// Monitor CNI config directory for file changes
	// This is a simplified implementation - production would use inotify
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	lastModTimes := make(map[string]time.Time)

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkConfigChanges(lastModTimes)
		}
	}
}

func (m *FileMonitor) checkConfigChanges(lastModTimes map[string]time.Time) {
	configPath := m.config.CNIConfPath
	if configPath == "" {
		configPath = "/etc/cni/net.d"
	}

	files, err := filepath.Glob(filepath.Join(configPath, "*.conf"))
	if err != nil {
		return
	}

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		modTime := info.ModTime()
		lastMod, exists := lastModTimes[file]

		if !exists {
			lastModTimes[file] = modTime
			continue
		}

		if modTime.After(lastMod) {
			lastModTimes[file] = modTime

			event := &core.CNIRawEvent{
				ID:         fmt.Sprintf("file_%s_%d", filepath.Base(file), modTime.Unix()),
				Timestamp:  modTime,
				Source:     "file",
				Operation:  core.CNIOperationOther,
				Success:    true,
				PluginName: m.extractPluginFromConfig(file),
				RawConfig:  file,
			}

			select {
			case m.eventChan <- *event:
			case <-m.ctx.Done():
				return
			}
		}
	}
}

func (m *FileMonitor) extractPluginFromConfig(configFile string) string {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return "unknown"
	}

	configStr := string(content)

	// Simple plugin detection from config content
	if strings.Contains(configStr, "cilium") {
		return "cilium"
	} else if strings.Contains(configStr, "calico") {
		return "calico"
	} else if strings.Contains(configStr, "flannel") {
		return "flannel"
	} else if strings.Contains(configStr, "bridge") {
		return "bridge"
	}

	return "unknown"
}
