package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	
	"tapio/cmd/install/installer"
)

// SimpleTerminalReporter provides basic terminal UI for progress reporting
type SimpleTerminalReporter struct {
	mu          sync.Mutex
	phases      map[string]*terminalPhase
	currentPhase string
	isTerminal  bool
	logger      io.Writer
}

// simpleTerminalPhase tracks phase information
type simpleTerminalPhase struct {
	name       string
	total      int64
	current    int64
	startTime  time.Time
	status     string
	err        error
}

// NewSimpleTerminalReporter creates a new terminal reporter without external dependencies
func NewSimpleTerminalReporter() installer.ProgressReporter {
	return &SimpleTerminalReporter{
		phases:     make(map[string]*terminalPhase),
		isTerminal: isTerminal(),
		logger:     os.Stdout,
	}
}

// Start begins a new phase
func (r *SimpleTerminalReporter) Start(phase string, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.phases[phase] = &terminalPhase{
		name:      phase,
		total:     total,
		current:   0,
		startTime: time.Now(),
		status:    "running",
	}
	r.currentPhase = phase
	
	fmt.Fprintf(r.logger, "▶ Starting: %s\n", phase)
}

// Update reports progress
func (r *SimpleTerminalReporter) Update(current int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if phase, ok := r.phases[r.currentPhase]; ok {
		phase.current = current
		
		if phase.total > 0 {
			percentage := int(float64(current) / float64(phase.total) * 100)
			
			// Simple progress bar
			barWidth := 40
			filled := barWidth * percentage / 100
			empty := barWidth - filled
			
			bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
			
			// Update in place if terminal
			if r.isTerminal {
				fmt.Fprintf(r.logger, "\r  [%s] %3d%% %s", bar, percentage, formatBytes(current))
			} else if percentage%10 == 0 {
				fmt.Fprintf(r.logger, "  Progress: %d%%\n", percentage)
			}
		}
	}
}

// Complete marks phase as complete
func (r *SimpleTerminalReporter) Complete(phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if p, ok := r.phases[phase]; ok {
		p.status = "complete"
		duration := time.Since(p.startTime)
		
		if r.isTerminal {
			fmt.Fprintf(r.logger, "\r✓ Completed: %s (%.2fs)\n", phase, duration.Seconds())
		} else {
			fmt.Fprintf(r.logger, "✓ Completed: %s (%.2fs)\n", phase, duration.Seconds())
		}
	}
}

// Error reports an error
func (r *SimpleTerminalReporter) Error(phase string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if p, ok := r.phases[phase]; ok {
		p.status = "error"
		p.err = err
	}
	
	fmt.Fprintf(r.logger, "✗ Error in %s: %v\n", phase, err)
}

// Log writes a log message
func (r *SimpleTerminalReporter) Log(level string, message string, fields ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Format fields
	var fieldStr string
	if len(fields) > 0 {
		var parts []string
		for i := 0; i < len(fields)-1; i += 2 {
			if key, ok := fields[i].(string); ok {
				parts = append(parts, fmt.Sprintf("%s=%v", key, fields[i+1]))
			}
		}
		if len(parts) > 0 {
			fieldStr = " " + strings.Join(parts, " ")
		}
	}
	
	prefix := "ℹ"
	switch level {
	case "error":
		prefix = "✗"
	case "warn":
		prefix = "⚠"
	case "debug":
		prefix = "▪"
	}
	
	fmt.Fprintf(r.logger, "%s %s%s\n", prefix, message, fieldStr)
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fileInfo.Mode()&os.ModeCharDevice != 0
}

// formatBytes formats bytes in human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	
	return fmt.Sprintf("%.1f%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}