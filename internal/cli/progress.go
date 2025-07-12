package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProgressIndicator provides visual feedback for long-running operations
type ProgressIndicator struct {
	message    string
	running    bool
	mu         sync.Mutex
	cancel     context.CancelFunc
	spinnerPos int
	showTime   bool
	startTime  time.Time
	verbose    bool
}

// NewProgressIndicator creates a new progress indicator
func NewProgressIndicator(message string) *ProgressIndicator {
	return &ProgressIndicator{
		message:   message,
		showTime:  true,
		startTime: time.Now(),
	}
}

// WithVerbose enables verbose mode
func (p *ProgressIndicator) WithVerbose(verbose bool) *ProgressIndicator {
	p.verbose = verbose
	return p
}

// WithoutTime disables time display
func (p *ProgressIndicator) WithoutTime() *ProgressIndicator {
	p.showTime = false
	return p
}

// Start begins the progress indicator
func (p *ProgressIndicator) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return
	}

	p.running = true
	p.startTime = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go p.animate(ctx)
}

// Stop stops the progress indicator
func (p *ProgressIndicator) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.running = false
	if p.cancel != nil {
		p.cancel()
	}

	// Clear the spinner line
	fmt.Print("\r\033[K")
}

// UpdateMessage updates the progress message
func (p *ProgressIndicator) UpdateMessage(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.message = message
}

// Success shows a success message and stops
func (p *ProgressIndicator) Success(message string) {
	p.Stop()
	duration := time.Since(p.startTime)
	if p.showTime && duration > time.Second {
		fmt.Printf("✅ %s (%.1fs)\n", message, duration.Seconds())
	} else {
		fmt.Printf("✅ %s\n", message)
	}
}

// Error shows an error message and stops
func (p *ProgressIndicator) Error(message string) {
	p.Stop()
	fmt.Printf("❌ %s\n", message)
}

// Warning shows a warning message and stops
func (p *ProgressIndicator) Warning(message string) {
	p.Stop()
	fmt.Printf("⚠️  %s\n", message)
}

// Info shows an info message and stops
func (p *ProgressIndicator) Info(message string) {
	p.Stop()
	fmt.Printf("ℹ️  %s\n", message)
}

// animate runs the spinner animation
func (p *ProgressIndicator) animate(ctx context.Context) {
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			if !p.running {
				p.mu.Unlock()
				return
			}

			// Build the progress line
			line := fmt.Sprintf("\r%s %s", spinner[p.spinnerPos], p.message)

			if p.showTime {
				elapsed := time.Since(p.startTime)
				if elapsed > 2*time.Second {
					line += fmt.Sprintf(" (%.1fs)", elapsed.Seconds())
				}
			}

			fmt.Print(line)
			p.spinnerPos = (p.spinnerPos + 1) % len(spinner)
			p.mu.Unlock()
		}
	}
}

// ProgressBar provides a visual progress bar for operations with known progress
type ProgressBar struct {
	total       int
	current     int
	width       int
	message     string
	showPercent bool
	showTime    bool
	startTime   time.Time
	mu          sync.Mutex
}

// NewProgressBar creates a new progress bar
func NewProgressBar(total int, message string) *ProgressBar {
	return &ProgressBar{
		total:       total,
		width:       40,
		message:     message,
		showPercent: true,
		showTime:    true,
		startTime:   time.Now(),
	}
}

// SetWidth sets the width of the progress bar
func (pb *ProgressBar) SetWidth(width int) *ProgressBar {
	pb.width = width
	return pb
}

// Update updates the progress
func (pb *ProgressBar) Update(current int) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.current = current
	if pb.current > pb.total {
		pb.current = pb.total
	}

	pb.render()
}

// Increment increments the progress by 1
func (pb *ProgressBar) Increment() {
	pb.Update(pb.current + 1)
}

// Finish completes the progress bar
func (pb *ProgressBar) Finish(message string) {
	pb.Update(pb.total)
	fmt.Print("\r\033[K") // Clear line

	duration := time.Since(pb.startTime)
	if pb.showTime && duration > time.Second {
		fmt.Printf("✅ %s (%.1fs)\n", message, duration.Seconds())
	} else {
		fmt.Printf("✅ %s\n", message)
	}
}

// render draws the progress bar
func (pb *ProgressBar) render() {
	percent := float64(pb.current) / float64(pb.total)
	if pb.total == 0 {
		percent = 1.0
	}

	filled := int(percent * float64(pb.width))
	if filled > pb.width {
		filled = pb.width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", pb.width-filled)

	line := fmt.Sprintf("\r%s [%s]", pb.message, bar)

	if pb.showPercent {
		line += fmt.Sprintf(" %.0f%%", percent*100)
	}

	line += fmt.Sprintf(" (%d/%d)", pb.current, pb.total)

	if pb.showTime {
		elapsed := time.Since(pb.startTime)
		if elapsed > time.Second {
			line += fmt.Sprintf(" %.1fs", elapsed.Seconds())
		}
	}

	fmt.Print(line)
}

// StepProgress provides step-by-step progress indication
type StepProgress struct {
	steps       []string
	currentStep int
	startTime   time.Time
	stepTimes   []time.Time
	verbose     bool
}

// NewStepProgress creates a new step progress indicator
func NewStepProgress(steps []string) *StepProgress {
	return &StepProgress{
		steps:     steps,
		startTime: time.Now(),
		stepTimes: make([]time.Time, len(steps)),
	}
}

// WithVerbose enables verbose output
func (sp *StepProgress) WithVerbose(verbose bool) *StepProgress {
	sp.verbose = verbose
	return sp
}

// Start begins the step progress
func (sp *StepProgress) Start() {
	if sp.verbose {
		fmt.Printf("Starting %d steps...\n", len(sp.steps))
	}
	sp.NextStep()
}

// NextStep advances to the next step
func (sp *StepProgress) NextStep() {
	if sp.currentStep > 0 {
		// Mark previous step as complete
		duration := time.Since(sp.stepTimes[sp.currentStep-1])
		if sp.verbose && duration > 500*time.Millisecond {
			fmt.Printf("✅ %s (%.1fs)\n", sp.steps[sp.currentStep-1], duration.Seconds())
		} else {
			fmt.Printf("✅ %s\n", sp.steps[sp.currentStep-1])
		}
	}

	if sp.currentStep < len(sp.steps) {
		sp.stepTimes[sp.currentStep] = time.Now()
		if sp.verbose {
			fmt.Printf("⏳ Step %d/%d: %s\n", sp.currentStep+1, len(sp.steps), sp.steps[sp.currentStep])
		} else {
			fmt.Printf("⏳ %s...\n", sp.steps[sp.currentStep])
		}
		sp.currentStep++
	}
}

// Finish completes all steps
func (sp *StepProgress) Finish(message string) {
	// Complete the last step if needed
	if sp.currentStep > 0 && sp.currentStep <= len(sp.steps) {
		duration := time.Since(sp.stepTimes[sp.currentStep-1])
		if sp.verbose && duration > 500*time.Millisecond {
			fmt.Printf("✅ %s (%.1fs)\n", sp.steps[sp.currentStep-1], duration.Seconds())
		} else {
			fmt.Printf("✅ %s\n", sp.steps[sp.currentStep-1])
		}
	}

	totalDuration := time.Since(sp.startTime)
	if totalDuration > time.Second {
		fmt.Printf("\n🎉 %s (%.1fs total)\n", message, totalDuration.Seconds())
	} else {
		fmt.Printf("\n🎉 %s\n", message)
	}
}

// Error shows an error for the current step
func (sp *StepProgress) Error(err error) {
	if sp.currentStep > 0 && sp.currentStep <= len(sp.steps) {
		fmt.Printf("❌ %s: %s\n", sp.steps[sp.currentStep-1], err.Error())
	} else {
		fmt.Printf("❌ Error: %s\n", err.Error())
	}
}

// Warning shows a warning for the current step
func (sp *StepProgress) Warning(message string) {
	if sp.currentStep > 0 && sp.currentStep <= len(sp.steps) {
		fmt.Printf("⚠️  %s: %s\n", sp.steps[sp.currentStep-1], message)
	} else {
		fmt.Printf("⚠️  Warning: %s\n", message)
	}
}

// Utility functions for quick progress indication

// WithProgress runs a function with a progress indicator
func WithProgress(message string, fn func() error) error {
	progress := NewProgressIndicator(message)
	progress.Start()

	err := fn()

	if err != nil {
		progress.Error("Failed")
		return err
	}

	progress.Success("Done")
	return nil
}

// WithProgressAndResult runs a function with progress and returns both result and error
func WithProgressAndResult[T any](message string, fn func() (T, error)) (T, error) {
	progress := NewProgressIndicator(message)
	progress.Start()

	result, err := fn()

	if err != nil {
		progress.Error("Failed")
		var zero T
		return zero, err
	}

	progress.Success("Done")
	return result, nil
}

// WithProgressSteps runs multiple steps with progress indication
func WithProgressSteps(steps []string, fns []func() error) error {
	if len(steps) != len(fns) {
		return fmt.Errorf("steps and functions count mismatch")
	}

	progress := NewStepProgress(steps).WithVerbose(verbose)
	progress.Start()

	for i, fn := range fns {
		if i > 0 {
			progress.NextStep()
		}

		if err := fn(); err != nil {
			progress.Error(err)
			return err
		}
	}

	progress.Finish("All steps completed successfully")
	return nil
}
