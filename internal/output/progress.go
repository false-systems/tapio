package output

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// ProgressBar provides visual progress indication
type ProgressBar struct {
	writer    io.Writer
	width     int
	total     int
	current   int
	message   string
	startTime time.Time
	mu        sync.Mutex
	active    bool
}

// NewProgressBar creates a new progress bar
func NewProgressBar(writer io.Writer, terminalWidth int) *ProgressBar {
	// Reserve space for percentage, time, and borders
	barWidth := terminalWidth - 30
	if barWidth < 20 {
		barWidth = 20
	}
	if barWidth > 60 {
		barWidth = 60
	}

	return &ProgressBar{
		writer: writer,
		width:  barWidth,
	}
}

// Start begins a new progress operation
func (p *ProgressBar) Start(message string, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.message = message
	p.total = total
	p.current = 0
	p.startTime = time.Now()
	p.active = true

	p.render()
}

// Update updates the progress bar
func (p *ProgressBar) Update(current int, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.active {
		return
	}

	p.current = current
	if message != "" {
		p.message = message
	}

	p.render()
}

// Complete finishes the progress bar
func (p *ProgressBar) Complete(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.active {
		return
	}

	p.current = p.total
	if message != "" {
		p.message = message
	}

	p.render()
	fmt.Fprintln(p.writer) // New line after completion
	p.active = false
}

// render draws the progress bar
func (p *ProgressBar) render() {
	if p.total == 0 {
		p.renderIndeterminate()
		return
	}

	percentage := float64(p.current) / float64(p.total)
	filled := int(percentage * float64(p.width))

	bar := strings.Builder{}
	bar.WriteString("\r") // Carriage return to overwrite line

	// Message
	if p.message != "" {
		bar.WriteString(p.message)
		bar.WriteString(" ")
	}

	// Progress bar
	bar.WriteString("[")
	for i := 0; i < p.width; i++ {
		if i < filled {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("] ")

	// Percentage
	bar.WriteString(fmt.Sprintf("%3d%%", int(percentage*100)))

	// Time elapsed
	elapsed := time.Since(p.startTime)
	if elapsed > time.Second {
		bar.WriteString(fmt.Sprintf(" %s", formatDuration(elapsed)))
	}

	// Clear to end of line
	bar.WriteString("\033[K")

	fmt.Fprint(p.writer, bar.String())
}

// renderIndeterminate shows a spinner for unknown progress
func (p *ProgressBar) renderIndeterminate() {
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinner := spinners[int(time.Since(p.startTime).Seconds())%len(spinners)]

	fmt.Fprintf(p.writer, "\r%s %s\033[K", spinner, p.message)
}

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// Spinner provides a simple activity indicator
type Spinner struct {
	writer  io.Writer
	message string
	active  bool
	mu      sync.Mutex
	done    chan bool
}

// NewSpinner creates a new spinner
func NewSpinner(writer io.Writer) *Spinner {
	return &Spinner{
		writer: writer,
		done:   make(chan bool),
	}
}

// Start begins spinning with a message
func (s *Spinner) Start(message string) {
	s.mu.Lock()
	s.message = message
	s.active = true
	s.mu.Unlock()

	go s.spin()
}

// Update changes the spinner message
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

// Stop stops the spinner
func (s *Spinner) Stop(finalMessage string) {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()

	s.done <- true

	// Clear the line and print final message
	fmt.Fprintf(s.writer, "\r\033[K%s\n", finalMessage)
}

// spin runs the spinner animation
func (s *Spinner) spin() {
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.active {
				fmt.Fprintf(s.writer, "\r%s %s\033[K", spinners[i%len(spinners)], s.message)
			}
			s.mu.Unlock()
			i++
		}
	}
}

// StepProgress tracks progress through a series of steps
type StepProgress struct {
	writer    io.Writer
	steps     []string
	current   int
	startTime time.Time
	mu        sync.Mutex
}

// NewStepProgress creates a new step progress tracker
func NewStepProgress(writer io.Writer, steps []string) *StepProgress {
	return &StepProgress{
		writer:    writer,
		steps:     steps,
		current:   -1,
		startTime: time.Now(),
	}
}

// NextStep advances to the next step
func (sp *StepProgress) NextStep() {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.current >= 0 && sp.current < len(sp.steps) {
		// Mark current step as complete
		fmt.Fprintf(sp.writer, "\r%s %s\033[K\n", Icons.Success, Colors.Success(sp.steps[sp.current]))
	}

	sp.current++
	if sp.current < len(sp.steps) {
		// Show next step in progress
		fmt.Fprintf(sp.writer, "%s %s", Icons.InProgress, Colors.Info(sp.steps[sp.current]))
	}
}

// Complete marks all steps as complete
func (sp *StepProgress) Complete() {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Complete current step if any
	if sp.current >= 0 && sp.current < len(sp.steps) {
		fmt.Fprintf(sp.writer, "\r%s %s\033[K\n", Icons.Success, Colors.Success(sp.steps[sp.current]))
	}

	// Mark any remaining steps as complete
	for i := sp.current + 1; i < len(sp.steps); i++ {
		fmt.Fprintf(sp.writer, "%s %s\n", Icons.Success, Colors.Success(sp.steps[i]))
	}

	elapsed := time.Since(sp.startTime)
	fmt.Fprintf(sp.writer, "\n%s Completed in %s\n", Icons.Clock, formatDuration(elapsed))
}

// Error marks the current step as failed
func (sp *StepProgress) Error(err error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.current >= 0 && sp.current < len(sp.steps) {
		fmt.Fprintf(sp.writer, "\r%s %s: %s\033[K\n", Icons.Error, Colors.Error(sp.steps[sp.current]), err.Error())
	}
}
