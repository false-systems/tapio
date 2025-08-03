package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"
)

// Client wraps Neo4j operations for correlation queries
type Client struct {
	driver neo4j.DriverWithContext
	logger *zap.Logger
	config Config
}

// Config holds Neo4j configuration
type Config struct {
	URI      string
	Username string
	Password string
	Database string
}

// NewClient creates a new Neo4j client
func NewClient(config Config, logger *zap.Logger) (*Client, error) {
	driver, err := neo4j.NewDriverWithContext(
		config.URI,
		neo4j.BasicAuth(config.Username, config.Password, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver: %w", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("failed to verify connectivity: %w", err)
	}

	return &Client{
		driver: driver,
		logger: logger,
		config: config,
	}, nil
}

// Close closes the driver
func (c *Client) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// ExecuteQuery runs a Cypher query with parameters
func (c *Client) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: c.config.Database,
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to run query: %w", err)
	}

	var records []map[string]interface{}
	for result.Next(ctx) {
		record := make(map[string]interface{})
		values := result.Record().Values
		keys := result.Record().Keys

		for i, key := range keys {
			record[key] = values[i]
		}
		records = append(records, record)
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return records, nil
}

// ExecuteWrite runs a write transaction
func (c *Client) ExecuteWrite(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: c.config.Database,
	})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		return nil, fn(tx)
	})

	return err
}

// CreateIndexes creates required indexes for performance
func (c *Client) CreateIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.uid)",
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.namespace, p.name)",
		"CREATE INDEX IF NOT EXISTS FOR (s:Service) ON (s.namespace, s.name)",
		"CREATE INDEX IF NOT EXISTS FOR (d:Deployment) ON (d.namespace, d.name)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.timestamp)",
		"CREATE INDEX IF NOT EXISTS FOR (e:Event) ON (e.traceId)",
	}

	for _, index := range indexes {
		if err := c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
			_, err := tx.Run(ctx, index, nil)
			return err
		}); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}
