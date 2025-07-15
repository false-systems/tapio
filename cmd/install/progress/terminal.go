package progress

import (
	"fmt"
	"github.com/yairfalse/tapio/cmd/install/installer"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// TerminalReporter provides rich terminal UI for progress reporting
type TerminalReporter struct {
	mu           sync.Mutex
	phases       map[string]*terminalPhase
	currentPhase string
	isTerminal   bool
	width        int
	height       int
	lastRender   time.Time
	renderRate   time.Duration
	logger       io.Writer
	colors       *colorScheme
	spinner      *spinner
	done         chan struct{}
	wg           sync.WaitGroup
}

// terminalPhase tracks phase information for display
type terminalPhase struct {
	name        string
	total       int64
	current     int64
	startTime   time.Time
	status      string
	err         error
	progressBar *progressBar
}

// colorScheme defines terminal colors
type colorScheme struct {
	phase    *color.Color
	progress *color.Color
	success  *color.Color
	error    *color.Color
	warning  *color.Color
	info     *color.Color
	dim      *color.Color
}

// progressBar renders a progress bar
type progressBar struct {
	width     int
	fillChar  string
	emptyChar string
	format    string
}

// spinner provides animated spinner
type spinner struct {
	frames []string
	index  int
	mu     sync.Mutex
}

// NewTerminalReporter creates a new terminal reporter
func NewTerminalReporter() installer.ProgressReporter {
	isTerminal := isTerminal()
	width, height := getTerminalSize()

	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	tr := &TerminalReporter{
		phases:     make(map[string]*terminalPhase),
		isTerminal: isTerminal,
		width:      width,
		height:     height,
		renderRate: 100 * time.Millisecond,
		logger:     os.Stdout,
		done:       make(chan struct{}),
		colors: &colorScheme{
			phase:    color.New(color.FgCyan, color.Bold),
			progress: color.New(color.FgGreen),
			success:  color.New(color.FgGreen, color.Bold),
			error:    color.New(color.FgRed, color.Bold),
			warning:  color.New(color.FgYellow),
			info:     color.New(color.FgWhite),
			dim:      color.New(color.FgHiBlack),
		},
		spinner: &spinner{
			frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		},
	}

	if isTerminal {
		tr.wg.Add(1)
		go tr.renderLoop()
	}

	return tr
}

// Start begins a new phase
func (tr *TerminalReporter) Start(phase string, total int64) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.phases[phase] = &terminalPhase{
		name:      phase,
		total:     total,
		current:   0,
		startTime: time.Now(),
		status:    "running",
		progressBar: &progressBar{
			width:     tr.width - 40,
			fillChar:  "█",
			emptyChar: "░",
			format:    "[%s%s] %3d%% %s",
		},
	}
	tr.currentPhase = phase

	if !tr.isTerminal {
		fmt.Fprintf(tr.logger, "▶ Starting: %s\n", phase)
	}
}

// Update reports progress
func (tr *TerminalReporter) Update(current int64) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if phase, ok := tr.phases[tr.currentPhase]; ok {
		phase.current = current

		if !tr.isTerminal && phase.total > 0 {
			percentage := int(float64(current) / float64(phase.total) * 100)
			if percentage%10 == 0 && percentage != int(float64(phase.current-current)/float64(phase.total)*100) {
				fmt.Fprintf(tr.logger, "  Progress: %d%%\n", percentage)
			}
		}
	}
}

// Complete marks phase as complete
func (tr *TerminalReporter) Complete(phase string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if p, ok := tr.phases[phase]; ok {
		p.status = "complete"
		duration := time.Since(p.startTime)

		if !tr.isTerminal {
			fmt.Fprintf(tr.logger, "✓ Completed: %s (%.2fs)\n", phase, duration.Seconds())
		}
	}
}

// Error reports an error
func (tr *TerminalReporter) Error(phase string, err error) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if p, ok := tr.phases[phase]; ok {
		p.status = "error"
		p.err = err
	}

	if !tr.isTerminal {
		fmt.Fprintf(tr.logger, "✗ Error in %s: %v\n", phase, err)
	}
}

// Log writes a log message
func (tr *TerminalReporter) Log(level string, message string, fields ...interface{}) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if !tr.isTerminal {
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

		fmt.Fprintf(tr.logger, "%s %s%s\n", prefix, message, fieldStr)
	}
}

// renderLoop continuously renders the terminal UI
func (tr *TerminalReporter) renderLoop() {
	defer tr.wg.Done()

	ticker := time.NewTicker(tr.renderRate)
	defer ticker.Stop()

	// Clear screen
	fmt.Print("\033[2J\033[H")

	for {
		select {
		case <-tr.done:
			// Final render
			tr.render()
			return
		case <-ticker.C:
			tr.render()
		}
	}
}

// render draws the current state
func (tr *TerminalReporter) render() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if !tr.isTerminal || time.Since(tr.lastRender) < tr.renderRate {
		return
	}

	// Move cursor to top
	fmt.Print("\033[H")

	// Title
	title := "Tapio Installation Progress"
	padding := (tr.width - len(title)) / 2
	fmt.Printf("%s%s%s\n", strings.Repeat(" ", padding),
		tr.colors.phase.Sprint(title),
		strings.Repeat(" ", padding))
	fmt.Println(strings.Repeat("─", tr.width))

	// Render phases
	row := 3
	for _, phase := range tr.getOrderedPhases() {
		if row >= tr.height-2 {
			break
		}

		tr.renderPhase(phase, row)
		row += 3
	}

	// Clear remaining lines
	for i := row; i < tr.height; i++ {
		fmt.Printf("\033[%d;0H\033[K", i)
	}

	tr.lastRender = time.Now()
}

// renderPhase renders a single phase
func (tr *TerminalReporter) renderPhase(phase *terminalPhase, row int) {
	// Move to row
	fmt.Printf("\033[%d;0H", row)

	// Phase name and status
	statusIcon := tr.spinner.next()
	statusColor := tr.colors.info

	switch phase.status {
	case "complete":
		statusIcon = "✓"
		statusColor = tr.colors.success
	case "error":
		statusIcon = "✗"
		statusColor = tr.colors.error
	}

	duration := time.Since(phase.startTime)
	fmt.Printf("%s %s %s\n",
		statusColor.Sprint(statusIcon),
		tr.colors.phase.Sprint(phase.name),
		tr.colors.dim.Sprintf("(%.1fs)", duration.Seconds()))

	// Progress bar
	if phase.total > 0 && phase.status == "running" {
		percentage := float64(phase.current) / float64(phase.total)
		phase.progressBar.render(percentage, fmt.Sprintf("%s/%s",
			formatBytes(phase.current),
			formatBytes(phase.total)))
	} else if phase.err != nil {
		tr.colors.error.Printf("  Error: %v\n", phase.err)
	}
}

// getOrderedPhases returns phases in order
func (tr *TerminalReporter) getOrderedPhases() []*terminalPhase {
	var phases []*terminalPhase
	for _, p := range tr.phases {
		phases = append(phases, p)
	}
	return phases
}

// Close closes the reporter
func (tr *TerminalReporter) Close() error {
	close(tr.done)
	tr.wg.Wait()
	return nil
}

// render draws the progress bar
func (pb *progressBar) render(percentage float64, label string) {
	if percentage > 1.0 {
		percentage = 1.0
	}
	if percentage < 0 {
		percentage = 0
	}

	filled := int(float64(pb.width) * percentage)
	empty := pb.width - filled

	bar := strings.Repeat(pb.fillChar, filled) + strings.Repeat(pb.emptyChar, empty)

	fmt.Printf(pb.format, bar[:filled], bar[filled:], int(percentage*100), label)
	fmt.Println()
}

// next returns the next spinner frame
func (s *spinner) next() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	frame := s.frames[s.index]
	s.index = (s.index + 1) % len(s.frames)
	return frame
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

// SilentReporter is a no-op reporter for quiet mode
type SilentReporter struct{}

// NewSilentReporter creates a silent reporter
func NewSilentReporter() installer.ProgressReporter {
	return &SilentReporter{}
}

func (s *SilentReporter) Start(phase string, total int64)                         {}
func (s *SilentReporter) Update(current int64)                                    {}
func (s *SilentReporter) Complete(phase string)                                   {}
func (s *SilentReporter) Error(phase string, err error)                           {}
func (s *SilentReporter) Log(level string, message string, fields ...interface{}) {}
