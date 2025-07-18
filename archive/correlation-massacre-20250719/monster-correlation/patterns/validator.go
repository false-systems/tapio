package patterns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/types"
)

// DefaultPatternValidator implements the PatternValidator interface
type DefaultPatternValidator struct {
	config types.PatternConfig

	// Validation rules
	rules []ValidationRule

	// Validation history
	history      []ValidationRecord
	historyMutex sync.RWMutex

	// Statistics
	stats ValidationStats
}

// ValidationRule defines a rule for validating pattern results
type ValidationRule struct {
	Name        string
	Description string
	Validate    func(ctx context.Context, result *types.PatternResult) error
}

// ValidationRecord keeps track of validation history
type ValidationRecord struct {
	Timestamp time.Time
	PatternID string
	Result    bool
	Error     error
	Duration  time.Duration
}

// ValidationStats tracks validation statistics
type ValidationStats struct {
	TotalValidations      int64
	SuccessfulValidations int64
	FailedValidations     int64
	AverageValidationTime time.Duration
	mutex                 sync.RWMutex
}

// Ensure DefaultPatternValidator implements types.PatternValidator
var _ types.PatternValidator = (*DefaultPatternValidator)(nil)

// NewDefaultPatternValidator creates a new pattern validator
func NewDefaultPatternValidator(config types.PatternConfig) *DefaultPatternValidator {
	validator := &DefaultPatternValidator{
		config:  config,
		history: make([]ValidationRecord, 0, 1000),
		rules:   defaultValidationRules(),
	}

	return validator
}

// Validate validates a single pattern result
func (dpv *DefaultPatternValidator) Validate(ctx context.Context, result *types.PatternResult) error {
	start := time.Now()

	// Check basic validity
	if result == nil {
		return fmt.Errorf("pattern result is nil")
	}

	// Check confidence threshold
	if result.Confidence < dpv.config.MinConfidence {
		err := fmt.Errorf("confidence %.2f below minimum threshold %.2f",
			result.Confidence, dpv.config.MinConfidence)
		dpv.recordValidation(result.PatternID, false, err, time.Since(start))
		return err
	}

	// Run all validation rules
	for _, rule := range dpv.rules {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := rule.Validate(ctx, result); err != nil {
				dpv.recordValidation(result.PatternID, false, err, time.Since(start))
				return fmt.Errorf("validation rule '%s' failed: %w", rule.Name, err)
			}
		}
	}

	dpv.recordValidation(result.PatternID, true, nil, time.Since(start))
	return nil
}

// ValidateBatch validates multiple pattern results
func (dpv *DefaultPatternValidator) ValidateBatch(ctx context.Context, results []types.PatternResult) ([]types.PatternResult, []error) {
	validResults := make([]types.PatternResult, 0, len(results))
	errors := make([]error, 0)

	// Use semaphore to limit concurrent validations
	sem := make(chan struct{}, 10)
	resultChan := make(chan validationResult, len(results))

	var wg sync.WaitGroup

	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultChan <- validationResult{
					index: idx,
					err:   ctx.Err(),
				}
				return
			}

			err := dpv.Validate(ctx, &results[idx])
			resultChan <- validationResult{
				index: idx,
				err:   err,
			}
		}(i)
	}

	// Wait for all validations to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	resultMap := make(map[int]error)
	for res := range resultChan {
		resultMap[res.index] = res.err
	}

	// Build ordered results
	for i := range results {
		if err, exists := resultMap[i]; exists && err == nil {
			validResults = append(validResults, results[i])
		} else if exists {
			errors = append(errors, err)
		}
	}

	return validResults, errors
}

// SetConfig updates the validator configuration
func (dpv *DefaultPatternValidator) SetConfig(config types.PatternConfig) {
	dpv.config = config
}

// GetConfig returns the current configuration
func (dpv *DefaultPatternValidator) GetConfig() types.PatternConfig {
	return dpv.config
}

// recordValidation records a validation attempt
func (dpv *DefaultPatternValidator) recordValidation(patternID string, success bool, err error, duration time.Duration) {
	record := ValidationRecord{
		Timestamp: time.Now(),
		PatternID: patternID,
		Result:    success,
		Error:     err,
		Duration:  duration,
	}

	dpv.historyMutex.Lock()
	dpv.history = append(dpv.history, record)
	// Keep only last 1000 records
	if len(dpv.history) > 1000 {
		dpv.history = dpv.history[len(dpv.history)-1000:]
	}
	dpv.historyMutex.Unlock()

	// Update statistics
	dpv.stats.mutex.Lock()
	dpv.stats.TotalValidations++
	if success {
		dpv.stats.SuccessfulValidations++
	} else {
		dpv.stats.FailedValidations++
	}
	// Update average validation time using exponential moving average
	alpha := 0.1
	if dpv.stats.AverageValidationTime == 0 {
		dpv.stats.AverageValidationTime = duration
	} else {
		dpv.stats.AverageValidationTime = time.Duration(
			float64(dpv.stats.AverageValidationTime)*(1-alpha) + float64(duration)*alpha,
		)
	}
	dpv.stats.mutex.Unlock()
}

// GetStats returns validation statistics
func (dpv *DefaultPatternValidator) GetStats() ValidationStats {
	dpv.stats.mutex.RLock()
	defer dpv.stats.mutex.RUnlock()
	return dpv.stats
}

// GetHistory returns recent validation history
func (dpv *DefaultPatternValidator) GetHistory() []ValidationRecord {
	dpv.historyMutex.RLock()
	defer dpv.historyMutex.RUnlock()

	// Return a copy to avoid race conditions
	history := make([]ValidationRecord, len(dpv.history))
	copy(history, dpv.history)
	return history
}

// Helper types
type validationResult struct {
	index int
	err   error
}

// defaultValidationRules returns the default set of validation rules
func defaultValidationRules() []ValidationRule {
	return []ValidationRule{
		{
			Name:        "timestamp_validity",
			Description: "Ensures pattern timestamps are valid",
			Validate: func(ctx context.Context, result *types.PatternResult) error {
				now := time.Now()
				if result.DetectedAt.After(now) {
					return fmt.Errorf("detection time is in the future")
				}
				if result.DetectedAt.Before(now.Add(-24 * time.Hour)) {
					return fmt.Errorf("detection time is too old (>24h)")
				}
				return nil
			},
		},
		{
			Name:        "entity_validity",
			Description: "Ensures affected entities are valid",
			Validate: func(ctx context.Context, result *types.PatternResult) error {
				if result.AffectedEntity.Name == "" && result.AffectedEntity.UID == "" {
					return fmt.Errorf("affected entity has no name or UID")
				}
				return nil
			},
		},
		{
			Name:        "severity_validity",
			Description: "Ensures severity is within valid range",
			Validate: func(ctx context.Context, result *types.PatternResult) error {
				validSeverities := map[types.Severity]bool{
					types.SeverityLow:      true,
					types.SeverityMedium:   true,
					types.SeverityHigh:     true,
					types.SeverityCritical: true,
				}
				if !validSeverities[result.Severity] {
					return fmt.Errorf("invalid severity: %v", result.Severity)
				}
				return nil
			},
		},
		{
			Name:        "recommendation_validity",
			Description: "Ensures recommendations are properly structured",
			Validate: func(ctx context.Context, result *types.PatternResult) error {
				for i, rec := range result.Recommendations {
					if rec.Title == "" {
						return fmt.Errorf("recommendation %d has no title", i)
					}
					if rec.Description == "" {
						return fmt.Errorf("recommendation %d has no description", i)
					}
					if len(rec.Commands) == 0 && !rec.AutoApply {
						return fmt.Errorf("recommendation %d has no commands and is not auto-apply", i)
					}
				}
				return nil
			},
		},
		{
			Name:        "pattern_metadata",
			Description: "Ensures pattern has required metadata",
			Validate: func(ctx context.Context, result *types.PatternResult) error {
				if result.PatternID == "" {
					return fmt.Errorf("pattern ID is empty")
				}
				if result.PatternName == "" {
					return fmt.Errorf("pattern name is empty")
				}
				if result.Category == "" {
					return fmt.Errorf("pattern category is empty")
				}
				return nil
			},
		},
	}
}
