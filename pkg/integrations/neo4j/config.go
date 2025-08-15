package neo4j

import (
	"fmt"
	"time"
)

// Config holds Neo4j connection configuration
type Config struct {
	// Connection settings
	URI      string `json:"uri"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`

	// Connection pool settings
	MaxConnections    int           `json:"max_connections"`
	ConnectionTimeout time.Duration `json:"connection_timeout"`
	MaxTransactionRetryTime time.Duration `json:"max_transaction_retry_time"`

	// Performance settings
	FetchSize               int  `json:"fetch_size"`
	EnableLivenessCheck     bool `json:"enable_liveness_check"`
	EnableConnectionLogging bool `json:"enable_connection_logging"`
}

// DefaultConfig returns default Neo4j configuration
func DefaultConfig() Config {
	return Config{
		URI:      "neo4j://localhost:7687",
		Username: "neo4j",
		Password: "password",
		Database: "neo4j",

		MaxConnections:    50,
		ConnectionTimeout: 30 * time.Second,
		MaxTransactionRetryTime: 15 * time.Second,

		FetchSize:               1000,
		EnableLivenessCheck:     true,
		EnableConnectionLogging: false,
	}
}

// Validate ensures the configuration is valid
func (c Config) Validate() error {
	if c.URI == "" {
		return fmt.Errorf("URI cannot be empty")
	}
	if c.Username == "" {
		return fmt.Errorf("Username cannot be empty")
	}
	if c.Password == "" {
		return fmt.Errorf("Password cannot be empty")
	}
	if c.Database == "" {
		return fmt.Errorf("Database cannot be empty")
	}
	if c.MaxConnections <= 0 {
		return fmt.Errorf("MaxConnections must be positive")
	}
	if c.ConnectionTimeout <= 0 {
		return fmt.Errorf("ConnectionTimeout must be positive")
	}
	if c.MaxTransactionRetryTime <= 0 {
		return fmt.Errorf("MaxTransactionRetryTime must be positive")
	}
	if c.FetchSize <= 0 {
		return fmt.Errorf("FetchSize must be positive")
	}
	return nil
}