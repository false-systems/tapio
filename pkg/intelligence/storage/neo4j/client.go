package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"
)

// Client provides Neo4j operations for Tapio correlation storage
type Client struct {
	driver neo4j.DriverWithContext
	logger *zap.Logger
	config Config
}

// Config holds Neo4j connection configuration
type Config struct {
	URI      string
	Username string
	Password string
	Database string

	// Connection pool settings
	MaxConnectionPoolSize int
	MaxConnectionLifetime time.Duration
	ConnectionTimeout     time.Duration
}

// DefaultConfig returns default Neo4j configuration
func DefaultConfig() Config {
	return Config{
		URI:                   "bolt://localhost:7687",
		Username:              "neo4j",
		Password:              "tapio123",
		Database:              "neo4j",
		MaxConnectionPoolSize: 50,
		MaxConnectionLifetime: 30 * time.Minute,
		ConnectionTimeout:     30 * time.Second,
	}
}

// NewClient creates a new Neo4j client
func NewClient(config Config, logger *zap.Logger) (*Client, error) {
	if config.URI == "" {
		return nil, fmt.Errorf("neo4j URI is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Create driver with connection pool settings
	driver, err := neo4j.NewDriverWithContext(
		config.URI,
		neo4j.BasicAuth(config.Username, config.Password, ""),
		func(c *neo4j.Config) {
			c.MaxConnectionPoolSize = config.MaxConnectionPoolSize
			c.MaxConnectionLifetime = config.MaxConnectionLifetime
			c.ConnectionAcquisitionTimeout = config.ConnectionTimeout
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create neo4j driver: %w", err)
	}

	client := &Client{
		driver: driver,
		logger: logger,
		config: config,
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.VerifyConnectivity(ctx); err != nil {
		driver.Close(context.Background())
		return nil, fmt.Errorf("failed to verify neo4j connectivity: %w", err)
	}

	logger.Info("Neo4j client initialized",
		zap.String("uri", config.URI),
		zap.String("database", config.Database))

	return client, nil
}

// Close closes the Neo4j driver
func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// VerifyConnectivity checks if Neo4j is reachable
func (c *Client) VerifyConnectivity(ctx context.Context) error {
	return c.driver.VerifyConnectivity(ctx)
}

// Session creates a new Neo4j session
func (c *Client) Session(ctx context.Context) neo4j.SessionWithContext {
	return c.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: c.config.Database,
	})
}

// ExecuteWrite executes a write transaction
func (c *Client) ExecuteWrite(ctx context.Context, work func(tx neo4j.ManagedTransaction) (interface{}, error)) (interface{}, error) {
	session := c.Session(ctx)
	defer session.Close(ctx)

	return session.ExecuteWrite(ctx, work)
}

// ExecuteRead executes a read transaction
func (c *Client) ExecuteRead(ctx context.Context, work func(tx neo4j.ManagedTransaction) (interface{}, error)) (interface{}, error) {
	session := c.Session(ctx)
	defer session.Close(ctx)

	return session.ExecuteRead(ctx, work)
}

// Health checks Neo4j health
func (c *Client) Health(ctx context.Context) error {
	// Check connectivity
	if err := c.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("neo4j connectivity check failed: %w", err)
	}

	// Run a simple query to verify database access
	session := c.Session(ctx)
	defer session.Close(ctx)

	result, err := session.Run(ctx, "RETURN 1 as health", nil)
	if err != nil {
		return fmt.Errorf("neo4j health query failed: %w", err)
	}

	if result.Next(ctx) {
		health, _ := result.Record().Get("health")
		if health != int64(1) {
			return fmt.Errorf("unexpected health check result: %v", health)
		}
	}

	return result.Err()
}

// GetDriver returns the underlying Neo4j driver
// This is needed for correlators that need direct driver access
func (c *Client) GetDriver() neo4j.DriverWithContext {
	return c.driver
}
