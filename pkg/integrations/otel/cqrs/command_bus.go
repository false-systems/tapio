package cqrs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/otel/domain"
	"github.com/yairfalse/tapio/pkg/otel/ports"
)

// CommandBus orchestrates command processing in the CQRS pattern
// Provides type-safe command routing, middleware, and transaction management
type CommandBus[T domain.TraceData] struct {
	// Command handlers registry
	handlers    map[CommandType]CommandHandler[T]
	handlersMux sync.RWMutex

	// Middleware pipeline
	middleware []CommandMiddleware[T]

	// Transaction management
	transactionManager TransactionManager

	// Event publishing
	eventBus *EventBus

	// Performance monitoring
	metrics *CommandMetrics

	// Configuration
	config CommandBusConfig

	// Async processing
	asyncProcessor *AsyncCommandProcessor[T]

	// Circuit breaker for resilience
	circuitBreaker *CircuitBreaker

	// Command validation
	validator *CommandValidator[T]

	// Audit logging
	auditLogger *AuditLogger
}

// CommandHandler defines the interface for handling specific commands
type CommandHandler[T domain.TraceData] interface {
	Handle(ctx context.Context, cmd Command[T]) (*CommandResult[T], error)
	CanHandle(cmdType CommandType) bool
	GetHandlerInfo() HandlerInfo
}

// Command represents a command in the CQRS pattern with type safety
type Command[T domain.TraceData] interface {
	GetCommandID() string
	GetCommandType() CommandType
	GetAggregateID() string
	GetTraceID() domain.TraceID
	GetTimestamp() time.Time
	GetMetadata() map[string]any
	Validate() error
	GetPayload() any
}

// CommandResult represents the result of command execution
type CommandResult[T domain.TraceData] struct {
	CommandID     string
	Success       bool
	AggregateID   string
	Version       int64
	Events        []domain.TraceEvent
	Result        any
	Error         error
	ExecutionTime time.Duration
	Metadata      map[string]any
}

// CommandMiddleware defines middleware for command processing pipeline
type CommandMiddleware[T domain.TraceData] interface {
	Execute(ctx context.Context, cmd Command[T], next CommandHandler[T]) (*CommandResult[T], error)
	GetMiddlewareName() string
	GetOrder() int
}

// CommandBusConfig configures the command bus behavior
type CommandBusConfig struct {
	// Processing configuration
	EnableAsyncProcessing bool
	AsyncBufferSize       int
	MaxConcurrentCommands int
	CommandTimeout        time.Duration

	// Transaction configuration
	EnableTransactions bool
	TransactionTimeout time.Duration
	IsolationLevel     TransactionIsolation

	// Retry configuration
	EnableRetry      bool
	MaxRetryAttempts int
	RetryBackoff     time.Duration
	RetryableErrors  []string

	// Circuit breaker configuration
	EnableCircuitBreaker bool
	FailureThreshold     int
	RecoveryTimeout      time.Duration

	// Monitoring configuration
	EnableMetrics        bool
	EnableAuditLogging   bool
	SlowCommandThreshold time.Duration

	// Event publishing
	EnableEventPublishing bool
	EventPublishTimeout   time.Duration
}

// NewCommandBus creates a new command bus with configuration
func NewCommandBus[T domain.TraceData](
	config CommandBusConfig,
	transactionManager TransactionManager,
	eventBus *EventBus,
) *CommandBus[T] {

	applyCommandBusDefaults(&config)

	bus := &CommandBus[T]{
		handlers:           make(map[CommandType]CommandHandler[T]),
		middleware:         make([]CommandMiddleware[T], 0),
		transactionManager: transactionManager,
		eventBus:           eventBus,
		config:             config,
		metrics:            NewCommandMetrics(),
		validator:          NewCommandValidator[T](),
		auditLogger:        NewAuditLogger(),
	}

	// Initialize async processor if enabled
	if config.EnableAsyncProcessing {
		bus.asyncProcessor = NewAsyncCommandProcessor[T](AsyncProcessorConfig{
			BufferSize:        config.AsyncBufferSize,
			MaxConcurrency:    config.MaxConcurrentCommands,
			ProcessingTimeout: config.CommandTimeout,
		})
	}

	// Initialize circuit breaker if enabled
	if config.EnableCircuitBreaker {
		bus.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: config.FailureThreshold,
			RecoveryTimeout:  config.RecoveryTimeout,
		})
	}

	// Register default middleware
	bus.registerDefaultMiddleware()

	return bus
}

// RegisterHandler registers a command handler for a specific command type
func (bus *CommandBus[T]) RegisterHandler(cmdType CommandType, handler CommandHandler[T]) error {
	bus.handlersMux.Lock()
	defer bus.handlersMux.Unlock()

	if _, exists := bus.handlers[cmdType]; exists {
		return fmt.Errorf("handler already registered for command type: %s", cmdType)
	}

	bus.handlers[cmdType] = handler

	bus.auditLogger.LogHandlerRegistration(AuditEvent{
		Type:        "handler_registered",
		CommandType: cmdType,
		HandlerInfo: handler.GetHandlerInfo(),
		Timestamp:   time.Now(),
	})

	return nil
}

// Execute executes a command through the middleware pipeline with transaction support
func (bus *CommandBus[T]) Execute(ctx context.Context, cmd Command[T]) (*CommandResult[T], error) {
	startTime := time.Now()
	commandID := cmd.GetCommandID()
	commandType := cmd.GetCommandType()

	// Record command execution attempt
	bus.metrics.RecordCommandAttempt(commandType)

	// Validate command
	if err := bus.validator.Validate(cmd); err != nil {
		bus.metrics.RecordCommandFailure(commandType, "validation_error")
		return &CommandResult[T]{
			CommandID: commandID,
			Success:   false,
			Error:     fmt.Errorf("command validation failed: %w", err),
		}, err
	}

	// Get handler
	handler, err := bus.getHandler(commandType)
	if err != nil {
		bus.metrics.RecordCommandFailure(commandType, "no_handler")
		return &CommandResult[T]{
			CommandID: commandID,
			Success:   false,
			Error:     err,
		}, err
	}

	// Use circuit breaker if enabled
	if bus.config.EnableCircuitBreaker {
		return bus.executeWithCircuitBreaker(ctx, cmd, handler, startTime)
	}

	// Execute with transaction support
	return bus.executeWithTransaction(ctx, cmd, handler, startTime)
}

// ExecuteAsync executes a command asynchronously
func (bus *CommandBus[T]) ExecuteAsync(ctx context.Context, cmd Command[T]) (*AsyncCommandResult[T], error) {
	if !bus.config.EnableAsyncProcessing {
		return nil, fmt.Errorf("async processing not enabled")
	}

	if bus.asyncProcessor == nil {
		return nil, fmt.Errorf("async processor not initialized")
	}

	// Submit command for async processing
	asyncResult, err := bus.asyncProcessor.SubmitCommand(ctx, cmd)
	if err != nil {
		bus.metrics.RecordCommandFailure(cmd.GetCommandType(), "async_submission_failed")
		return nil, fmt.Errorf("failed to submit async command: %w", err)
	}

	bus.metrics.RecordAsyncCommandSubmitted(cmd.GetCommandType())

	return asyncResult, nil
}

// ExecuteBatch executes multiple commands in a batch with transaction support
func (bus *CommandBus[T]) ExecuteBatch(
	ctx context.Context,
	commands []Command[T],
) (*BatchCommandResult[T], error) {

	if len(commands) == 0 {
		return &BatchCommandResult[T]{}, nil
	}

	startTime := time.Now()
	batchID := generateBatchID()

	bus.metrics.RecordBatchCommandAttempt(len(commands))

	result := &BatchCommandResult[T]{
		BatchID:   batchID,
		Commands:  len(commands),
		Results:   make([]*CommandResult[T], 0, len(commands)),
		StartTime: startTime,
	}

	// Execute batch within transaction if enabled
	if bus.config.EnableTransactions {
		return bus.executeBatchWithTransaction(ctx, commands, result)
	}

	// Execute commands sequentially without transaction
	for _, cmd := range commands {
		cmdResult, err := bus.Execute(ctx, cmd)
		if err != nil {
			cmdResult = &CommandResult[T]{
				CommandID: cmd.GetCommandID(),
				Success:   false,
				Error:     err,
			}
		}

		result.Results = append(result.Results, cmdResult)

		if cmdResult.Success {
			result.SuccessCount++
		} else {
			result.FailureCount++
		}
	}

	result.Duration = time.Since(startTime)
	bus.metrics.RecordBatchCommandComplete(result.SuccessCount, result.FailureCount, result.Duration)

	return result, nil
}

// AddMiddleware adds middleware to the processing pipeline
func (bus *CommandBus[T]) AddMiddleware(middleware CommandMiddleware[T]) {
	bus.middleware = append(bus.middleware, middleware)

	// Sort middleware by order
	bus.sortMiddleware()

	bus.auditLogger.LogMiddlewareAdded(AuditEvent{
		Type:           "middleware_added",
		MiddlewareName: middleware.GetMiddlewareName(),
		Order:          middleware.GetOrder(),
		Timestamp:      time.Now(),
	})
}

// Private implementation methods

func (bus *CommandBus[T]) getHandler(cmdType CommandType) (CommandHandler[T], error) {
	bus.handlersMux.RLock()
	defer bus.handlersMux.RUnlock()

	handler, exists := bus.handlers[cmdType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for command type: %s", cmdType)
	}

	return handler, nil
}

func (bus *CommandBus[T]) executeWithCircuitBreaker(
	ctx context.Context,
	cmd Command[T],
	handler CommandHandler[T],
	startTime time.Time,
) (*CommandResult[T], error) {

	var result *CommandResult[T]
	var err error

	cbErr := bus.circuitBreaker.Execute(func() error {
		result, err = bus.executeWithTransaction(ctx, cmd, handler, startTime)
		return err
	})

	if cbErr != nil {
		bus.metrics.RecordCommandFailure(cmd.GetCommandType(), "circuit_breaker_open")
		return &CommandResult[T]{
			CommandID: cmd.GetCommandID(),
			Success:   false,
			Error:     cbErr,
		}, cbErr
	}

	return result, err
}

func (bus *CommandBus[T]) executeWithTransaction(
	ctx context.Context,
	cmd Command[T],
	handler CommandHandler[T],
	startTime time.Time,
) (*CommandResult[T], error) {

	if !bus.config.EnableTransactions {
		return bus.executeWithMiddleware(ctx, cmd, handler, startTime)
	}

	// Begin transaction
	tx, err := bus.transactionManager.Begin(ctx, TransactionOptions{
		IsolationLevel: bus.config.IsolationLevel,
		Timeout:        bus.config.TransactionTimeout,
	})
	if err != nil {
		bus.metrics.RecordCommandFailure(cmd.GetCommandType(), "transaction_begin_failed")
		return &CommandResult[T]{
			CommandID: cmd.GetCommandID(),
			Success:   false,
			Error:     fmt.Errorf("failed to begin transaction: %w", err),
		}, err
	}

	// Create transaction context
	txCtx := bus.transactionManager.WithTransaction(ctx, tx)

	// Execute command within transaction
	result, err := bus.executeWithMiddleware(txCtx, cmd, handler, startTime)

	if err != nil || !result.Success {
		// Rollback transaction on error
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			bus.auditLogger.LogTransactionError(AuditEvent{
				Type:        "transaction_rollback_failed",
				CommandID:   cmd.GetCommandID(),
				CommandType: cmd.GetCommandType(),
				Error:       rollbackErr.Error(),
				Timestamp:   time.Now(),
			})
		}

		bus.metrics.RecordTransactionRollback(cmd.GetCommandType())
		return result, err
	}

	// Commit transaction on success
	if commitErr := tx.Commit(); commitErr != nil {
		bus.metrics.RecordCommandFailure(cmd.GetCommandType(), "transaction_commit_failed")
		return &CommandResult[T]{
			CommandID: cmd.GetCommandID(),
			Success:   false,
			Error:     fmt.Errorf("failed to commit transaction: %w", commitErr),
		}, commitErr
	}

	bus.metrics.RecordTransactionCommit(cmd.GetCommandType())
	return result, nil
}

func (bus *CommandBus[T]) executeWithMiddleware(
	ctx context.Context,
	cmd Command[T],
	handler CommandHandler[T],
	startTime time.Time,
) (*CommandResult[T], error) {

	// Build middleware chain
	var finalHandler CommandHandler[T] = &finalCommandHandler[T]{
		handler:   handler,
		bus:       bus,
		startTime: startTime,
	}

	// Apply middleware in reverse order (last middleware first)
	for i := len(bus.middleware) - 1; i >= 0; i-- {
		finalHandler = &middlewareHandler[T]{
			middleware: bus.middleware[i],
			next:       finalHandler,
		}
	}

	// Execute command through middleware chain
	result, err := finalHandler.Handle(ctx, cmd)

	// Record execution metrics
	executionTime := time.Since(startTime)
	bus.metrics.RecordCommandExecution(cmd.GetCommandType(), executionTime, err == nil)

	// Log slow commands
	if executionTime > bus.config.SlowCommandThreshold {
		bus.auditLogger.LogSlowCommand(AuditEvent{
			Type:          "slow_command",
			CommandID:     cmd.GetCommandID(),
			CommandType:   cmd.GetCommandType(),
			ExecutionTime: executionTime,
			Timestamp:     time.Now(),
		})
	}

	// Publish events if successful
	if result != nil && result.Success && len(result.Events) > 0 && bus.config.EnableEventPublishing {
		go bus.publishEvents(ctx, result.Events)
	}

	return result, err
}

func (bus *CommandBus[T]) executeBatchWithTransaction(
	ctx context.Context,
	commands []Command[T],
	result *BatchCommandResult[T],
) (*BatchCommandResult[T], error) {

	// Begin transaction
	tx, err := bus.transactionManager.Begin(ctx, TransactionOptions{
		IsolationLevel: bus.config.IsolationLevel,
		Timeout:        bus.config.TransactionTimeout,
	})
	if err != nil {
		return result, fmt.Errorf("failed to begin batch transaction: %w", err)
	}

	// Create transaction context
	txCtx := bus.transactionManager.WithTransaction(ctx, tx)

	// Execute all commands within transaction
	allSuccessful := true
	for _, cmd := range commands {
		cmdResult, err := bus.executeWithMiddleware(txCtx, cmd, nil, result.StartTime)
		if err != nil {
			cmdResult = &CommandResult[T]{
				CommandID: cmd.GetCommandID(),
				Success:   false,
				Error:     err,
			}
		}

		result.Results = append(result.Results, cmdResult)

		if cmdResult.Success {
			result.SuccessCount++
		} else {
			result.FailureCount++
			allSuccessful = false
		}
	}

	if allSuccessful {
		// Commit transaction if all commands successful
		if commitErr := tx.Commit(); commitErr != nil {
			return result, fmt.Errorf("failed to commit batch transaction: %w", commitErr)
		}
		bus.metrics.RecordBatchTransactionCommit(len(commands))
	} else {
		// Rollback transaction if any command failed
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			bus.auditLogger.LogTransactionError(AuditEvent{
				Type:      "batch_transaction_rollback_failed",
				BatchID:   result.BatchID,
				Error:     rollbackErr.Error(),
				Timestamp: time.Now(),
			})
		}
		bus.metrics.RecordBatchTransactionRollback(len(commands))
	}

	result.Duration = time.Since(result.StartTime)
	return result, nil
}

func (bus *CommandBus[T]) registerDefaultMiddleware() {
	// Register built-in middleware
	bus.AddMiddleware(NewLoggingMiddleware[T]())
	bus.AddMiddleware(NewValidationMiddleware[T]())
	bus.AddMiddleware(NewRetryMiddleware[T](bus.config))
	bus.AddMiddleware(NewMetricsMiddleware[T](bus.metrics))

	if bus.config.EnableAuditLogging {
		bus.AddMiddleware(NewAuditMiddleware[T](bus.auditLogger))
	}
}

func (bus *CommandBus[T]) sortMiddleware() {
	// Sort middleware by order (lower numbers execute first)
	for i := 0; i < len(bus.middleware)-1; i++ {
		for j := i + 1; j < len(bus.middleware); j++ {
			if bus.middleware[i].GetOrder() > bus.middleware[j].GetOrder() {
				bus.middleware[i], bus.middleware[j] = bus.middleware[j], bus.middleware[i]
			}
		}
	}
}

func (bus *CommandBus[T]) publishEvents(ctx context.Context, events []domain.TraceEvent) {
	if bus.eventBus == nil {
		return
	}

	// Publish events asynchronously
	for _, event := range events {
		if err := bus.eventBus.Publish(ctx, event); err != nil {
			bus.auditLogger.LogEventPublishError(AuditEvent{
				Type:      "event_publish_failed",
				EventID:   event.GetEventID().String(),
				Error:     err.Error(),
				Timestamp: time.Now(),
			})
		}
	}
}

// Supporting types for command execution

// finalCommandHandler wraps the actual command handler
type finalCommandHandler[T domain.TraceData] struct {
	handler   CommandHandler[T]
	bus       *CommandBus[T]
	startTime time.Time
}

func (h *finalCommandHandler[T]) Handle(ctx context.Context, cmd Command[T]) (*CommandResult[T], error) {
	return h.handler.Handle(ctx, cmd)
}

func (h *finalCommandHandler[T]) CanHandle(cmdType CommandType) bool {
	return h.handler.CanHandle(cmdType)
}

func (h *finalCommandHandler[T]) GetHandlerInfo() HandlerInfo {
	return h.handler.GetHandlerInfo()
}

// middlewareHandler wraps middleware execution
type middlewareHandler[T domain.TraceData] struct {
	middleware CommandMiddleware[T]
	next       CommandHandler[T]
}

func (h *middlewareHandler[T]) Handle(ctx context.Context, cmd Command[T]) (*CommandResult[T], error) {
	return h.middleware.Execute(ctx, cmd, h.next)
}

func (h *middlewareHandler[T]) CanHandle(cmdType CommandType) bool {
	return h.next.CanHandle(cmdType)
}

func (h *middlewareHandler[T]) GetHandlerInfo() HandlerInfo {
	return h.next.GetHandlerInfo()
}

// Supporting types and interfaces

type CommandType string
type TransactionIsolation int

const (
	CommandTypeCreateSpan CommandType = "create_span"
	CommandTypeUpdateSpan CommandType = "update_span"
	CommandTypeFinishSpan CommandType = "finish_span"
	CommandTypeDeleteSpan CommandType = "delete_span"
)

type HandlerInfo struct {
	Name              string
	Version           string
	Description       string
	SupportedCommands []CommandType
}

type BatchCommandResult[T domain.TraceData] struct {
	BatchID      string
	Commands     int
	Results      []*CommandResult[T]
	SuccessCount int
	FailureCount int
	Duration     time.Duration
	StartTime    time.Time
}

type AsyncCommandResult[T domain.TraceData] struct {
	CommandID   string
	Status      AsyncStatus
	ResultChan  <-chan *CommandResult[T]
	SubmittedAt time.Time
}

type AsyncStatus string

const (
	AsyncStatusPending   AsyncStatus = "pending"
	AsyncStatusExecuting AsyncStatus = "executing"
	AsyncStatusCompleted AsyncStatus = "completed"
	AsyncStatusFailed    AsyncStatus = "failed"
)

// Helper functions

func applyCommandBusDefaults(config *CommandBusConfig) {
	if config.AsyncBufferSize == 0 {
		config.AsyncBufferSize = 1000
	}
	if config.MaxConcurrentCommands == 0 {
		config.MaxConcurrentCommands = 10
	}
	if config.CommandTimeout == 0 {
		config.CommandTimeout = 30 * time.Second
	}
	if config.TransactionTimeout == 0 {
		config.TransactionTimeout = 30 * time.Second
	}
	if config.MaxRetryAttempts == 0 {
		config.MaxRetryAttempts = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 100 * time.Millisecond
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 30 * time.Second
	}
	if config.SlowCommandThreshold == 0 {
		config.SlowCommandThreshold = time.Second
	}
	if config.EventPublishTimeout == 0 {
		config.EventPublishTimeout = 5 * time.Second
	}
}

func generateBatchID() string {
	// Implementation would generate a unique batch ID
	return fmt.Sprintf("batch_%d", time.Now().UnixNano())
}

// Additional supporting types would be defined here...
