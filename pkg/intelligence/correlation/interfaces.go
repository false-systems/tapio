package correlation
import (
	"context"
	"time"
)
// PatternDetector defines the interface for pattern detection
type PatternDetector interface {
	// Metadata
	ID() string
	Name() string
	Description() string
	Category() Category
	// Detection
	Detect(ctx context.Context, events []Event, metrics map[string]MetricSeries) (*PatternResult, error)
	// Configuration
	RequiredEventTypes() []string
	RequiredMetricTypes() []string
	TimeWindow() time.Duration
}
// PatternRegistry manages pattern detectors
type PatternRegistry interface {
	// Registration
	Register(detector PatternDetector) error
	Unregister(patternID string) error
	// Retrieval
	Get(patternID string) (PatternDetector, error)
	List() []PatternDetector
	ListByCategory(category Category) []PatternDetector
	// Execution
	DetectAll(ctx context.Context, events []Event, metrics map[string]MetricSeries) ([]PatternResult, error)
}
// PatternValidator validates pattern detection results
type PatternValidator interface {
	// Validation
	Validate(ctx context.Context, result *PatternResult) error
	ValidateBatch(ctx context.Context, results []PatternResult) ([]PatternResult, []error)
	// Configuration
	SetConfig(config PatternConfig)
	GetConfig() PatternConfig
}
