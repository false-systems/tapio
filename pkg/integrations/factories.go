package integrations

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"go.uber.org/zap"
)

// NewGraphStorage creates a Neo4j-based graph storage implementation
// This factory bridges the intelligence layer with Neo4j implementation
func NewGraphStorage(config domain.GraphStorageConfig, logger interface{}) (domain.GraphStorage, error) {
	zapLogger, ok := logger.(*zap.Logger)
	if !ok {
		return nil, fmt.Errorf("logger must be *zap.Logger, got %T", logger)
	}

	// Convert to Neo4j config
	neo4jConfig := neo4j.Config{
		URI:      config.URI,
		Username: config.Username,
		Password: config.Password,
		Database: config.Database,
	}

	// Create Neo4j client
	client, err := neo4j.NewClient(neo4jConfig, zapLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j client: %w", err)
	}

	// Return the adapter that implements domain.GraphStorage
	return NewGraphStorageAdapter(client), nil
}
