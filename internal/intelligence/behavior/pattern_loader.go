package behavior

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// PatternLoader handles loading and hot-reloading of behavior patterns from YAML files
type PatternLoader struct {
	logger *zap.Logger

	// Pattern storage
	mu       sync.RWMutex
	patterns map[string]*domain.BehaviorPattern

	// File watching
	watcher     *fsnotify.Watcher
	patternDirs []string
	stopCh      chan struct{}
	reloadCh    chan string

	// Validation
	validator *PatternValidator

	// OTEL instrumentation
	tracer         trace.Tracer
	patternsLoaded metric.Int64Gauge
	reloadCount    metric.Int64Counter
	loadErrors     metric.Int64Counter
	validationTime metric.Float64Histogram
}

// PatternLoaderConfig configures the pattern loader
type PatternLoaderConfig struct {
	PatternDirs     []string
	WatchForChanges bool
	ValidationLevel ValidationLevel
	ReloadDebounce  time.Duration
}

// ValidationLevel controls how strict pattern validation is
type ValidationLevel int

const (
	ValidationMinimal ValidationLevel = iota
	ValidationStandard
	ValidationStrict
)

// NewPatternLoader creates a new pattern loader
func NewPatternLoader(logger *zap.Logger, config PatternLoaderConfig) (*PatternLoader, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	if len(config.PatternDirs) == 0 {
		return nil, fmt.Errorf("at least one pattern directory must be specified")
	}

	// Verify directories exist
	for _, dir := range config.PatternDirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("pattern directory %s does not exist: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("pattern path %s is not a directory", dir)
		}
	}

	// Initialize OTEL
	tracer := otel.Tracer("behavior.pattern_loader")
	meter := otel.Meter("behavior.pattern_loader")

	patternsLoaded, err := meter.Int64Gauge(
		"behavior_patterns_loaded_total",
		metric.WithDescription("Total number of behavior patterns currently loaded"),
	)
	if err != nil {
		logger.Warn("Failed to create patterns loaded gauge", zap.Error(err))
	}

	reloadCount, err := meter.Int64Counter(
		"behavior_pattern_reloads_total",
		metric.WithDescription("Total number of pattern reloads"),
	)
	if err != nil {
		logger.Warn("Failed to create reload counter", zap.Error(err))
	}

	loadErrors, err := meter.Int64Counter(
		"behavior_pattern_load_errors_total",
		metric.WithDescription("Total number of pattern load errors"),
	)
	if err != nil {
		logger.Warn("Failed to create load errors counter", zap.Error(err))
	}

	validationTime, err := meter.Float64Histogram(
		"behavior_pattern_validation_duration_ms",
		metric.WithDescription("Time taken to validate patterns in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create validation time histogram", zap.Error(err))
	}

	loader := &PatternLoader{
		logger:         logger,
		patterns:       make(map[string]*domain.BehaviorPattern),
		patternDirs:    config.PatternDirs,
		stopCh:         make(chan struct{}),
		reloadCh:       make(chan string, 100),
		validator:      NewPatternValidator(config.ValidationLevel),
		tracer:         tracer,
		patternsLoaded: patternsLoaded,
		reloadCount:    reloadCount,
		loadErrors:     loadErrors,
		validationTime: validationTime,
	}

	// Initial load
	if err := loader.loadAllPatterns(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to load initial patterns: %w", err)
	}

	// Setup file watching if requested
	if config.WatchForChanges {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, fmt.Errorf("failed to create file watcher: %w", err)
		}
		loader.watcher = watcher

		// Add all pattern directories to watcher
		for _, dir := range config.PatternDirs {
			if err := watcher.Add(dir); err != nil {
				watcher.Close()
				return nil, fmt.Errorf("failed to watch directory %s: %w", dir, err)
			}
		}

		// Start watching in background
		go loader.watchPatterns(config.ReloadDebounce)
	}

	return loader, nil
}

// GetPattern retrieves a pattern by ID
func (l *PatternLoader) GetPattern(id string) (*domain.BehaviorPattern, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	pattern, exists := l.patterns[id]
	return pattern, exists
}

// GetAllPatterns returns all loaded patterns
func (l *PatternLoader) GetAllPatterns() []*domain.BehaviorPattern {
	l.mu.RLock()
	defer l.mu.RUnlock()

	patterns := make([]*domain.BehaviorPattern, 0, len(l.patterns))
	for _, p := range l.patterns {
		patterns = append(patterns, p)
	}
	return patterns
}

// GetPatternsByCategory returns patterns matching a specific category
func (l *PatternLoader) GetPatternsByCategory(category string) []*domain.BehaviorPattern {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var patterns []*domain.BehaviorPattern
	for _, p := range l.patterns {
		if p.Category == category {
			patterns = append(patterns, p)
		}
	}
	return patterns
}

// loadAllPatterns loads all patterns from configured directories
func (l *PatternLoader) loadAllPatterns(ctx context.Context) error {
	ctx, span := l.tracer.Start(ctx, "pattern_loader.load_all_patterns")
	defer span.End()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Clear existing patterns
	l.patterns = make(map[string]*domain.BehaviorPattern)

	totalLoaded := 0
	totalErrors := 0

	for _, dir := range l.patternDirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			l.logger.Error("Failed to list pattern files",
				zap.String("directory", dir),
				zap.Error(err))
			totalErrors++
			continue
		}

		for _, file := range files {
			pattern, err := l.loadPatternFile(ctx, file)
			if err != nil {
				l.logger.Error("Failed to load pattern file",
					zap.String("file", file),
					zap.Error(err))
				totalErrors++
				if l.loadErrors != nil {
					l.loadErrors.Add(ctx, 1, metric.WithAttributes(
						attribute.String("file", filepath.Base(file)),
						attribute.String("error_type", "load_failed"),
					))
				}
				continue
			}

			// Check for duplicate IDs
			if existing, exists := l.patterns[pattern.ID]; exists {
				l.logger.Warn("Duplicate pattern ID found, keeping first",
					zap.String("id", pattern.ID),
					zap.String("existing_file", existing.Name),
					zap.String("duplicate_file", file))
				continue
			}

			l.patterns[pattern.ID] = pattern
			totalLoaded++
		}
	}

	if l.patternsLoaded != nil {
		l.patternsLoaded.Record(ctx, int64(totalLoaded))
	}

	span.SetAttributes(
		attribute.Int("patterns.loaded", totalLoaded),
		attribute.Int("patterns.errors", totalErrors),
	)

	l.logger.Info("Patterns loaded",
		zap.Int("total", totalLoaded),
		zap.Int("errors", totalErrors))

	return nil
}

// loadPatternFile loads and validates a single pattern file
func (l *PatternLoader) loadPatternFile(ctx context.Context, filepath string) (*domain.BehaviorPattern, error) {
	ctx, span := l.tracer.Start(ctx, "pattern_loader.load_pattern_file")
	defer span.End()

	span.SetAttributes(attribute.String("file", filepath))

	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open pattern file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read pattern file: %w", err)
	}

	var pattern domain.BehaviorPattern
	if err := yaml.Unmarshal(data, &pattern); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Validate pattern
	start := time.Now()
	if err := l.validator.Validate(&pattern); err != nil {
		return nil, fmt.Errorf("pattern validation failed: %w", err)
	}

	validationMs := float64(time.Since(start).Microseconds()) / 1000.0
	if l.validationTime != nil {
		l.validationTime.Record(ctx, validationMs, metric.WithAttributes(
			attribute.String("pattern_id", pattern.ID),
		))
	}

	// Set defaults if not specified
	if pattern.TimeWindow == 0 {
		pattern.TimeWindow = 5 * time.Minute
	}
	if pattern.MinConfidence == 0 {
		pattern.MinConfidence = 0.7
	}

	return &pattern, nil
}

// watchPatterns watches for pattern file changes
func (l *PatternLoader) watchPatterns(debounce time.Duration) {
	defer func() {
		if l.watcher != nil {
			l.watcher.Close()
		}
	}()

	reloadTimer := time.NewTimer(debounce)
	reloadTimer.Stop()

	pendingReload := false

	for {
		select {
		case event, ok := <-l.watcher.Events:
			if !ok {
				return
			}

			// Only handle write and create events for YAML files
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				if filepath.Ext(event.Name) == ".yaml" {
					l.logger.Debug("Pattern file changed",
						zap.String("file", event.Name),
						zap.String("op", event.Op.String()))

					// Debounce reloads
					if !pendingReload {
						pendingReload = true
						reloadTimer.Reset(debounce)
					}
				}
			}

		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			l.logger.Error("File watcher error", zap.Error(err))

		case <-reloadTimer.C:
			if pendingReload {
				pendingReload = false
				ctx := context.Background()

				l.logger.Info("Reloading patterns due to file changes")
				if err := l.loadAllPatterns(ctx); err != nil {
					l.logger.Error("Failed to reload patterns", zap.Error(err))
				} else {
					if l.reloadCount != nil {
						l.reloadCount.Add(ctx, 1)
					}
				}
			}

		case <-l.stopCh:
			return
		}
	}
}

// Stop stops the pattern loader and file watching
func (l *PatternLoader) Stop() error {
	close(l.stopCh)

	if l.watcher != nil {
		return l.watcher.Close()
	}

	return nil
}

// PatternValidator validates behavior patterns
type PatternValidator struct {
	level ValidationLevel
}

// NewPatternValidator creates a new pattern validator
func NewPatternValidator(level ValidationLevel) *PatternValidator {
	return &PatternValidator{level: level}
}

// Validate validates a behavior pattern
func (v *PatternValidator) Validate(pattern *domain.BehaviorPattern) error {
	// Basic validation (always performed)
	if pattern.ID == "" {
		return fmt.Errorf("pattern ID is required")
	}
	if pattern.Name == "" {
		return fmt.Errorf("pattern name is required")
	}
	if pattern.Category == "" {
		return fmt.Errorf("pattern category is required")
	}
	if len(pattern.Conditions) == 0 {
		return fmt.Errorf("at least one condition is required")
	}

	// Validate each condition
	for i, cond := range pattern.Conditions {
		if err := v.validateCondition(&cond, i); err != nil {
			return fmt.Errorf("condition %d: %w", i, err)
		}
	}

	// Standard validation
	if v.level >= ValidationStandard {
		if pattern.Description == "" {
			return fmt.Errorf("pattern description is required for standard validation")
		}
		if pattern.Severity == "" {
			return fmt.Errorf("pattern severity is required for standard validation")
		}
		if len(pattern.PredictionTemplate.PotentialImpacts) == 0 {
			return fmt.Errorf("at least one potential impact is required for standard validation")
		}
	}

	// Strict validation
	if v.level >= ValidationStrict {
		if len(pattern.PredictionTemplate.RecommendedActions) == 0 {
			return fmt.Errorf("at least one recommended action is required for strict validation")
		}
		if pattern.TimeWindow < time.Second {
			return fmt.Errorf("time window must be at least 1 second for strict validation")
		}
		if pattern.MinConfidence < 0.5 || pattern.MinConfidence > 1.0 {
			return fmt.Errorf("min confidence must be between 0.5 and 1.0 for strict validation")
		}
	}

	return nil
}

// validateCondition validates a single condition
func (v *PatternValidator) validateCondition(cond *domain.Condition, index int) error {
	if cond.EventType == "" {
		return fmt.Errorf("event type is required")
	}

	// Validate match type
	switch cond.Match.Type {
	case "exact", "regex", "contains", "threshold":
		// Valid match types
	case "":
		return fmt.Errorf("match type is required")
	default:
		return fmt.Errorf("invalid match type: %s", cond.Match.Type)
	}

	// Validate required fields based on match type
	if cond.Match.Type == "threshold" {
		if cond.Match.Threshold == 0 {
			return fmt.Errorf("threshold value is required for threshold match type")
		}
	} else {
		if cond.Match.Field == "" {
			return fmt.Errorf("field is required for match type %s", cond.Match.Type)
		}
		if cond.Match.Value == "" && cond.Match.Type != "exists" {
			return fmt.Errorf("value is required for match type %s", cond.Match.Type)
		}
	}

	// Validate aggregation if present
	if cond.Aggregation != nil {
		if cond.Aggregation.Type == "" {
			return fmt.Errorf("aggregation type is required when aggregation is specified")
		}
		if cond.Aggregation.Window == 0 {
			return fmt.Errorf("aggregation window is required when aggregation is specified")
		}
	}

	return nil
}
