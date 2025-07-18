package core
import "errors"
// Common errors
var (
	// Configuration errors
	ErrInvalidConfig          = errors.New("invalid configuration")
	ErrInvalidTimeWindow      = errors.New("invalid time window")
	ErrInvalidConfidenceScore = errors.New("invalid confidence score")
	// Runtime errors
	ErrEngineNotStarted       = errors.New("correlation engine not started")
	ErrEngineAlreadyStarted   = errors.New("correlation engine already started")
	ErrEngineShuttingDown     = errors.New("correlation engine shutting down")
	ErrContextCanceled        = errors.New("context canceled")
	// Event processing errors
	ErrEventValidation        = errors.New("event validation failed")
	ErrEventProcessing        = errors.New("event processing failed")
	ErrEventBufferFull        = errors.New("event buffer full")
	ErrEventNotFound          = errors.New("event not found")
	ErrEventExpired           = errors.New("event expired")
	// Pattern errors
	ErrPatternNotFound        = errors.New("pattern not found")
	ErrPatternAlreadyExists   = errors.New("pattern already exists")
	ErrPatternInvalid         = errors.New("pattern invalid")
	ErrPatternMatchTimeout    = errors.New("pattern match timeout")
	ErrPatternDisabled        = errors.New("pattern disabled")
	// Algorithm errors
	ErrAlgorithmNotFound      = errors.New("algorithm not found")
	ErrAlgorithmFailed        = errors.New("algorithm execution failed")
	ErrAlgorithmTimeout       = errors.New("algorithm timeout")
	ErrInsufficientData       = errors.New("insufficient data for correlation")
	// Correlation errors
	ErrCorrelationNotFound    = errors.New("correlation not found")
	ErrCorrelationExpired     = errors.New("correlation expired")
	ErrInvalidCriteria        = errors.New("invalid correlation criteria")
	ErrNoCorrelationsFound    = errors.New("no correlations found")
	// Buffer errors
	ErrBufferEmpty            = errors.New("buffer empty")
	ErrBufferCapacityExceeded = errors.New("buffer capacity exceeded")
	ErrBufferCorrupted        = errors.New("buffer corrupted")
	// Temporal analysis errors
	ErrInvalidTimeRange       = errors.New("invalid time range")
	ErrTemporalAnalysisFailed = errors.New("temporal analysis failed")
	ErrCausalAnalysisFailed   = errors.New("causal analysis failed")
	// Resource errors
	ErrResourceExhausted      = errors.New("resource exhausted")
	ErrMemoryLimitExceeded    = errors.New("memory limit exceeded")
	ErrProcessingOverload     = errors.New("processing overload")
)