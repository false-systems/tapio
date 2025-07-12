package resilience_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/resilience"
)

// Example_circuitBreaker demonstrates how to use the circuit breaker pattern
func Example_circuitBreaker() {
	// Create a circuit breaker for API calls
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:             "api-breaker",
		MaxFailures:      5,
		ResetTimeout:     30 * time.Second,
		HalfOpenMaxCalls: 2,
		OnStateChange: func(oldState, newState resilience.State) {
			log.Printf("Circuit breaker state changed from %s to %s", oldState, newState)
		},
	})

	// Use the circuit breaker to protect API calls
	err := cb.Execute(context.Background(), func() error {
		// Simulate API call
		return makeAPICall()
	})

	if err != nil {
		log.Printf("API call failed: %v", err)
	}

	// With fallback
	err = cb.ExecuteWithFallback(context.Background(),
		func() error {
			return makeAPICall()
		},
		func() error {
			// Fallback to cached data
			return getCachedData()
		},
	)

	// Get metrics
	metrics := cb.GetMetrics()
	fmt.Printf("Circuit breaker stats: %+v\n", metrics)
}

// Example_timeoutManager demonstrates timeout and retry patterns
func Example_timeoutManager() {
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

	// Execute operation with timeout and retry
	err := tm.Execute(context.Background(), "database-query", func(ctx context.Context) error {
		// Simulate database query
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Query completed
			return nil
		}
	})

	if err != nil {
		log.Printf("Operation failed: %v", err)
	}
}

// Example_timeoutWithCircuitBreaker demonstrates combining timeout and circuit breaker
func Example_timeoutWithCircuitBreaker() {
	// Create circuit breaker
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:         "db-breaker",
		MaxFailures:  3,
		ResetTimeout: 60 * time.Second,
	})

	// Create timeout manager with circuit breaker
	tm := resilience.NewTimeoutManager(resilience.TimeoutConfig{
		Timeout:        2 * time.Second,
		MaxRetries:     2,
		CircuitBreaker: cb,
		RetryStrategy: &resilience.LinearBackoff{
			Delay: 500 * time.Millisecond,
		},
	})

	// Execute protected operation
	err := tm.Execute(context.Background(), "protected-operation", func(ctx context.Context) error {
		// Your operation here
		return performDatabaseOperation(ctx)
	})

	if err != nil {
		log.Printf("Protected operation failed: %v", err)
	}
}

// Example_dataValidation demonstrates the validation pipeline
func Example_dataValidation() {
	// Define validation rules
	minAge := 0.0
	maxAge := 150.0

	rules := []resilience.ValidationRule{
		{
			Field:    "username",
			Required: true,
			Type:     "string",
			Pattern:  "^[a-zA-Z0-9_]{3,20}$",
		},
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
		{
			Field: "role",
			Type:  "string",
			Enum:  []interface{}{"user", "admin", "moderator"},
		},
		{
			Field:    "profile.bio",
			Required: false,
			Type:     "string",
			Custom: func(value interface{}) error {
				bio, ok := value.(string)
				if ok && len(bio) > 500 {
					return errors.New("bio must be 500 characters or less")
				}
				return nil
			},
		},
	}

	// Create schema validator
	validator, err := resilience.NewSchemaValidator("user-validator", rules)
	if err != nil {
		log.Fatal(err)
	}

	// Validate user data
	userData := map[string]interface{}{
		"username": "john_doe",
		"email":    "john@example.com",
		"age":      25,
		"role":     "user",
		"profile": map[string]interface{}{
			"bio": "Software developer interested in distributed systems",
		},
	}

	err = validator.Validate(context.Background(), userData)
	if err != nil {
		log.Printf("Validation failed: %v", err)
	}
}

// Example_validationPipeline demonstrates chaining multiple validators
func Example_validationPipeline() {
	// Create basic validator
	basicValidator, _ := resilience.NewSchemaValidator("basic", []resilience.ValidationRule{
		{Field: "id", Required: true, Type: "string"},
		{Field: "timestamp", Required: true, Type: "number"},
	})

	// Create business logic validator
	businessValidator := &customValidator{name: "business-rules"}

	// Create validation pipeline
	pipeline := resilience.NewValidationPipeline(
		"complete-validation",
		false, // Run sequentially
		basicValidator,
		businessValidator,
	)

	// Validate data through pipeline
	data := map[string]interface{}{
		"id":        "12345",
		"timestamp": time.Now().Unix(),
		"amount":    100.50,
	}

	err := pipeline.Validate(context.Background(), data)
	if err != nil {
		log.Printf("Pipeline validation failed: %v", err)
	}
}

// Example_healthChecker demonstrates health checking system
func Example_healthChecker() {
	// Create health checker
	hc := resilience.NewHealthChecker(5*time.Second, 30*time.Second)

	// Register components
	hc.RegisterComponent(resilience.Component{
		Name:        "database",
		Description: "PostgreSQL database",
		Critical:    true,
		HealthCheck: func(ctx context.Context) error {
			// Check database connectivity
			return checkDatabaseHealth(ctx)
		},
	})

	hc.RegisterComponent(resilience.Component{
		Name:        "cache",
		Description: "Redis cache",
		Critical:    false,
		HealthCheck: func(ctx context.Context) error {
			// Check cache connectivity
			return checkCacheHealth(ctx)
		},
	})

	hc.RegisterComponent(resilience.Component{
		Name:        "external-api",
		Description: "Third-party API",
		Critical:    false,
		Timeout:     3 * time.Second,
		HealthCheck: resilience.DependencyHealthCheck(map[string]func() error{
			"api-endpoint": func() error { return pingAPI() },
			"api-auth":     func() error { return validateAPIAuth() },
		}),
	})

	// Set status change callback
	hc.SetStatusChangeCallback(func(component string, oldStatus, newStatus resilience.HealthStatus) {
		log.Printf("Component %s changed from %s to %s", component, oldStatus, newStatus)
	})

	// Start background health checks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.StartBackgroundChecks(ctx)

	// Check overall system health
	status := hc.GetStatus(context.Background())
	fmt.Printf("System health: %s\n", status)

	// Get detailed health report
	results := hc.CheckAll(context.Background())
	for _, result := range results {
		fmt.Printf("Component: %s, Status: %s, Duration: %v\n",
			result.Name, result.Status, result.Duration)
	}
}

// Example_boundedExecutor demonstrates limiting concurrent operations
func Example_boundedExecutor() {
	// Create bounded executor for rate limiting
	executor := resilience.NewBoundedExecutor(10, 5*time.Second)

	// Process items with concurrency limit
	items := make([]string, 100)
	for i := range items {
		items[i] = fmt.Sprintf("item-%d", i)
	}

	for _, item := range items {
		itemCopy := item // Capture for goroutine
		go func() {
			err := executor.Execute(context.Background(), func() error {
				// Process item with guaranteed concurrency limit
				return processItem(itemCopy)
			})

			if err != nil {
				if errors.Is(err, resilience.ErrTimeout) {
					log.Printf("Item %s rejected due to timeout", itemCopy)
				} else {
					log.Printf("Item %s failed: %v", itemCopy, err)
				}
			}
		}()
	}

	// Get executor metrics
	metrics := executor.GetMetrics()
	fmt.Printf("Executor stats: Total=%d, Active=%d, Rejected=%d\n",
		metrics.TotalExecutions, metrics.ActiveExecutions, metrics.RejectedExecutions)
}

// Example_completeResilienceStack demonstrates using all patterns together
func Example_completeResilienceStack() {
	// 1. Create circuit breaker
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:         "api-breaker",
		MaxFailures:  5,
		ResetTimeout: 30 * time.Second,
	})

	// 2. Create timeout manager with circuit breaker
	tm := resilience.NewTimeoutManager(resilience.TimeoutConfig{
		Timeout:        5 * time.Second,
		MaxRetries:     3,
		CircuitBreaker: cb,
		RetryStrategy: &resilience.ExponentialBackoff{
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     2 * time.Second,
			Multiplier:   2.0,
			Jitter:       true,
		},
	})

	// 3. Create validator for responses
	validator, _ := resilience.NewSchemaValidator("response-validator", []resilience.ValidationRule{
		{Field: "status", Required: true, Type: "string"},
		{Field: "data", Required: true, Type: "object"},
		{Field: "timestamp", Required: true, Type: "number"},
	})

	// 4. Create health checker
	hc := resilience.NewHealthChecker(5*time.Second, 30*time.Second)
	hc.RegisterComponent(resilience.Component{
		Name:     "api-service",
		Critical: true,
		HealthCheck: func(ctx context.Context) error {
			// Use circuit breaker state for health
			if cb.GetState() == resilience.StateOpen {
				return errors.New("circuit breaker is open")
			}
			return nil
		},
	})

	// 5. Use all components together
	processRequest := func(ctx context.Context, request interface{}) (interface{}, error) {
		var response interface{}

		// Execute with timeout and circuit breaker
		err := tm.Execute(ctx, "api-call", func(ctx context.Context) error {
			// Make API call
			resp, err := makeAPICallWithContext(ctx, request)
			if err != nil {
				return err
			}

			// Validate response
			if err := validator.Validate(ctx, resp); err != nil {
				return fmt.Errorf("invalid response: %w", err)
			}

			response = resp
			return nil
		})

		if err != nil {
			return nil, err
		}

		return response, nil
	}

	// Use the resilient function
	response, err := processRequest(context.Background(), "test-request")
	if err != nil {
		log.Printf("Request failed: %v", err)

		// Check health status
		health := hc.GetStatus(context.Background())
		log.Printf("System health: %s", health)
	} else {
		fmt.Printf("Request successful: %v\n", response)
	}
}

// Helper functions for examples
func makeAPICall() error {
	// Simulate API call
	return nil
}

func getCachedData() error {
	// Simulate cache retrieval
	return nil
}

func performDatabaseOperation(ctx context.Context) error {
	// Simulate database operation
	return nil
}

func checkDatabaseHealth(ctx context.Context) error {
	// Simulate database health check
	return nil
}

func checkCacheHealth(ctx context.Context) error {
	// Simulate cache health check
	return nil
}

func pingAPI() error {
	// Simulate API ping
	return nil
}

func validateAPIAuth() error {
	// Simulate API auth validation
	return nil
}

func processItem(item string) error {
	// Simulate item processing
	return nil
}

func makeAPICallWithContext(ctx context.Context, request interface{}) (interface{}, error) {
	// Simulate API call with context
	return map[string]interface{}{
		"status":    "success",
		"data":      map[string]interface{}{"result": "ok"},
		"timestamp": time.Now().Unix(),
	}, nil
}

// customValidator implements the Validator interface
type customValidator struct {
	name string
}

func (v *customValidator) Validate(ctx context.Context, data interface{}) error {
	// Custom business logic validation
	return nil
}

func (v *customValidator) Name() string {
	return v.name
}
