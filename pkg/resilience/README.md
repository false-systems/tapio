# Resilience Framework

A comprehensive resilience framework for building bulletproof applications with circuit breakers, timeouts, retries, validation, and health checking.

## Overview

The resilience package provides reusable patterns for handling failures, preventing cascade failures, and ensuring data integrity in distributed systems.

## Components

### 1. Circuit Breaker

Prevents cascade failures by stopping calls to failing services.

```go
// Create circuit breaker
cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
    Name:             "api-breaker",
    MaxFailures:      5,
    ResetTimeout:     30 * time.Second,
    HalfOpenMaxCalls: 2,
})

// Execute with circuit breaker
err := cb.Execute(ctx, func() error {
    return makeAPICall()
})

// Execute with fallback
err := cb.ExecuteWithFallback(ctx, 
    func() error { return makeAPICall() },
    func() error { return getCachedData() },
)
```

**States:**
- **Closed**: Normal operation, requests pass through
- **Open**: Failures exceeded threshold, requests are blocked
- **Half-Open**: Testing if service recovered, limited requests allowed

### 2. Timeout and Retry Framework

Manages operation timeouts with configurable retry strategies.

```go
// Create timeout manager with exponential backoff
tm := resilience.NewTimeoutManager(resilience.TimeoutConfig{
    Timeout: 5 * time.Second,
    RetryStrategy: &resilience.ExponentialBackoff{
        InitialDelay: 100 * time.Millisecond,
        MaxDelay:     5 * time.Second,
        Multiplier:   2.0,
        Jitter:       true,
    },
    MaxRetries: 3,
})

// Execute with timeout and retry
err := tm.Execute(ctx, "operation-name", func(ctx context.Context) error {
    return performOperation(ctx)
})
```

**Retry Strategies:**
- **ExponentialBackoff**: Delays increase exponentially with optional jitter
- **LinearBackoff**: Constant delay between retries

### 3. Data Validation Pipeline

Validates data integrity with schema validation and custom rules.

```go
// Define validation rules
rules := []resilience.ValidationRule{
    {
        Field:    "email",
        Required: true,
        Type:     "string",
        Pattern:  `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
    },
    {
        Field:    "age",
        Required: true,
        Type:     "integer",
        Min:      &minAge,
        Max:      &maxAge,
    },
}

// Create validator
validator, _ := resilience.NewSchemaValidator("user-validator", rules)

// Validate data
err := validator.Validate(ctx, userData)
```

**Features:**
- Type validation (string, number, integer, boolean, array, object)
- Range validation (min/max)
- Pattern matching (regex)
- Enum validation
- Custom validation functions
- Nested field support

### 4. Health Check Framework

Monitors component health with dependency tracking.

```go
// Create health checker
hc := resilience.NewHealthChecker(5*time.Second, 30*time.Second)

// Register components
hc.RegisterComponent(resilience.Component{
    Name:        "database",
    Critical:    true,
    HealthCheck: func(ctx context.Context) error {
        return checkDatabaseConnection(ctx)
    },
})

// Start background checks
hc.StartBackgroundChecks(ctx)

// Get system status
status := hc.GetStatus(ctx) // Returns: healthy, degraded, or unhealthy
```

**Features:**
- Component health tracking
- Critical vs non-critical components
- Dependency health checks
- Background monitoring
- Health status aggregation
- Caching to prevent excessive checks

## Integration Patterns

### Combining Circuit Breaker with Timeout

```go
// Circuit breaker protects against cascading failures
cb := resilience.NewCircuitBreaker(config)

// Timeout manager uses circuit breaker
tm := resilience.NewTimeoutManager(resilience.TimeoutConfig{
    CircuitBreaker: cb,
    // ... other config
})
```

### Validation Pipeline

Chain multiple validators for comprehensive validation:

```go
pipeline := resilience.NewValidationPipeline(
    "complete-validation",
    false, // Sequential execution
    schemaValidator,
    businessRulesValidator,
    dataIntegrityValidator,
)
```

### Bounded Execution

Limit concurrent operations to prevent resource exhaustion:

```go
executor := resilience.NewBoundedExecutor(10, 5*time.Second)

err := executor.Execute(ctx, func() error {
    return processItem()
})
```

## Best Practices

1. **Circuit Breaker Configuration**
   - Set `MaxFailures` based on service SLA
   - Use appropriate `ResetTimeout` for recovery time
   - Monitor state changes for alerting

2. **Timeout Strategy**
   - Set timeouts slightly higher than p99 latency
   - Use exponential backoff for transient failures
   - Add jitter to prevent thundering herd

3. **Validation**
   - Validate early in request processing
   - Use parallel validation for independent checks
   - Cache validation results when appropriate

4. **Health Checks**
   - Keep health checks lightweight
   - Use appropriate intervals to avoid overload
   - Define clear criteria for each health status

## Metrics and Monitoring

All components provide metrics for monitoring:

```go
// Circuit breaker metrics
cbMetrics := cb.GetMetrics()
// Includes: total calls, failures, successes, state, open duration

// Timeout manager metrics
tmMetrics := tm.GetMetrics()
// Includes: attempts, timeouts, retries, successes

// Validation metrics
valMetrics := validator.GetMetrics()
// Includes: total validations, errors, average time

// Health checker metrics
hcMetrics := hc.GetMetrics()
// Includes: component status, check counts, durations
```

## Error Handling

The framework provides specific error types:

```go
var (
    ErrTimeout            = errors.New("operation timed out")
    ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")
    ErrValidationFailed   = errors.New("validation failed")
    ErrDataCorrupted      = errors.New("data corrupted")
)
```

Use `errors.Is()` for proper error checking:

```go
if errors.Is(err, resilience.ErrTimeout) {
    // Handle timeout
}
```

## Performance Considerations

1. **Circuit Breaker**: Minimal overhead, atomic operations for state
2. **Timeout Manager**: Goroutine per operation, context-based cancellation
3. **Validation**: Compiled regex patterns, concurrent validation option
4. **Health Checker**: Result caching, concurrent checks

## Testing

The framework includes comprehensive tests demonstrating:
- State transitions
- Concurrent access patterns
- Failure scenarios
- Integration testing

Run tests:
```bash
go test ./pkg/resilience/...
```

## Examples

See `examples_test.go` for complete examples including:
- Basic usage of each component
- Integration patterns
- Production-ready configurations
- Error handling strategies