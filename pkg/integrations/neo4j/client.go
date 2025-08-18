package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Client wraps the Neo4j driver with observability and best practices
type Client struct {
	driver neo4j.DriverWithContext
	config Config
	logger *zap.Logger

	// OTEL instrumentation
	tracer              trace.Tracer
	transactionsTotal   metric.Int64Counter
	transactionDuration metric.Float64Histogram
	errorsTotal         metric.Int64Counter
}

// NewClient creates a new Neo4j client with the given configuration
func NewClient(config Config, logger *zap.Logger) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Create driver
	auth := neo4j.BasicAuth(config.Username, config.Password, "")

	driverConfig := func(c *neo4j.Config) {
		c.MaxConnectionPoolSize = config.MaxConnections
		c.ConnectionAcquisitionTimeout = config.ConnectionTimeout
		c.MaxTransactionRetryTime = config.MaxTransactionRetryTime
		c.FetchSize = config.FetchSize

		if config.EnableConnectionLogging {
			c.Log = neo4j.ConsoleLogger(neo4j.INFO)
		}
	}

	driver, err := neo4j.NewDriverWithContext(config.URI, auth, driverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	client := &Client{
		driver: driver,
		config: config,
		logger: logger,
	}

	// Initialize OTEL instrumentation
	if err := client.initOTEL(); err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to initialize OTEL: %w", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectionTimeout)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to verify Neo4j connectivity: %w", err)
	}

	logger.Info("Neo4j client created successfully",
		zap.String("uri", config.URI),
		zap.String("database", config.Database))

	return client, nil
}

// initOTEL initializes OpenTelemetry instrumentation
func (c *Client) initOTEL() error {
	c.tracer = otel.Tracer("integrations.neo4j")
	meter := otel.Meter("integrations.neo4j")

	var err error

	c.transactionsTotal, err = meter.Int64Counter(
		"neo4j_transactions_total",
		metric.WithDescription("Total Neo4j transactions executed"),
	)
	if err != nil {
		c.logger.Warn("Failed to create transactions counter", zap.Error(err))
	}

	c.transactionDuration, err = meter.Float64Histogram(
		"neo4j_transaction_duration_ms",
		metric.WithDescription("Neo4j transaction duration in milliseconds"),
	)
	if err != nil {
		c.logger.Warn("Failed to create transaction duration histogram", zap.Error(err))
	}

	c.errorsTotal, err = meter.Int64Counter(
		"neo4j_errors_total",
		metric.WithDescription("Total Neo4j errors"),
	)
	if err != nil {
		c.logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	return nil
}

// ExecuteWrite executes a write transaction
func (c *Client) ExecuteWrite(ctx context.Context, work func(tx neo4j.ManagedTransaction) error) error {
	ctx, span := c.tracer.Start(ctx, "neo4j.execute_write")
	defer span.End()

	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.transactionDuration != nil {
			c.transactionDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.String("transaction_type", "write"),
			))
		}
	}()

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: c.config.Database,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		return nil, work(tx)
	})

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("transaction_type", "write"),
				attribute.String("error", err.Error()),
			))
		}
		return fmt.Errorf("write transaction failed: %w", err)
	}

	if c.transactionsTotal != nil {
		c.transactionsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("transaction_type", "write"),
			attribute.String("status", "success"),
		))
	}

	span.SetStatus(codes.Ok, "Write transaction completed")
	return nil
}

// ExecuteTypedWrite executes a write transaction with type-safe parameters
func (c *Client) ExecuteTypedWrite(ctx context.Context, work TransactionWork) error {
	return c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		typedTx := wrapTransaction(tx)
		return work(ctx, typedTx)
	})
}

// ExecuteRead executes a read transaction
func (c *Client) ExecuteRead(ctx context.Context, work func(tx neo4j.ManagedTransaction) (interface{}, error)) (interface{}, error) {
	ctx, span := c.tracer.Start(ctx, "neo4j.execute_read")
	defer span.End()

	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.transactionDuration != nil {
			c.transactionDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.String("transaction_type", "read"),
			))
		}
	}()

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: c.config.Database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, work)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("transaction_type", "read"),
				attribute.String("error", err.Error()),
			))
		}
		return nil, fmt.Errorf("read transaction failed: %w", err)
	}

	if c.transactionsTotal != nil {
		c.transactionsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("transaction_type", "read"),
			attribute.String("status", "success"),
		))
	}

	span.SetStatus(codes.Ok, "Read transaction completed")
	return result, nil
}

// TypedReadWork represents a read transaction with type-safe parameters
type TypedReadWork func(ctx context.Context, tx *TypedTransaction) (interface{}, error)

// ExecuteTypedRead executes a read transaction with type-safe parameters
func (c *Client) ExecuteTypedRead(ctx context.Context, work TypedReadWork) (interface{}, error) {
	return c.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		typedTx := wrapTransaction(tx)
		return work(ctx, typedTx)
	})
}

// VerifyConnectivity checks if the connection to Neo4j is working
func (c *Client) VerifyConnectivity(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "neo4j.verify_connectivity")
	defer span.End()

	if err := c.driver.VerifyConnectivity(ctx); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("Neo4j connectivity check failed: %w", err)
	}

	span.SetStatus(codes.Ok, "Connectivity verified")
	return nil
}

// Close closes the Neo4j driver
func (c *Client) Close(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "neo4j.close")
	defer span.End()

	if err := c.driver.Close(ctx); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to close Neo4j driver: %w", err)
	}

	span.SetStatus(codes.Ok, "Driver closed")
	c.logger.Info("Neo4j client closed")
	return nil
}

// GetConfig returns the current configuration
func (c *Client) GetConfig() Config {
	return c.config
}
