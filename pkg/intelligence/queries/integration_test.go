package queries

import (
	"context"
	"testing"

	graph "github.com/yairfalse/tapio/pkg/integrations/neo4j"
	"go.uber.org/zap"
)

// TestGraphClientInterface verifies that graph.Client implements queries.GraphClient
func TestGraphClientInterface(t *testing.T) {
	// This test verifies interface compatibility by attempting to assign
	// a graph.Client to a queries.GraphClient variable

	// Skip actual connection for unit test - just verify interface compatibility
	t.Run("interface compatibility", func(t *testing.T) {
		// Mock config
		config := neo4j.Config{
			URI:      "neo4j://localhost:7687",
			Username: "neo4j",
			Password: "test",
			Database: "test",
		}

		logger := zap.NewNop()

		// This would fail to compile if neo4j.Client doesn't implement GraphClient
		var _ GraphClient = func() GraphClient {
			client, err := neo4j.NewClient(config, logger)
			if err != nil {
				// Return a mock that implements the interface for compilation test
				return &mockImplementation{client}
			}
			return client
		}()

		// Test that we can create CorrelationQuery with the interface
		// (This line would fail to compile if interface doesn't match)
		client, err := graph.NewClient(config, logger)
		if err != nil {
			t.Skip("Neo4j connection not available, but interface compatibility verified")
			return
		}
		defer client.Close(context.Background())

		query := NewCorrelationQuery(client)
		if query == nil {
			t.Error("Failed to create CorrelationQuery with graph.Client")
		}
	})
}

// mockImplementation is a wrapper to help with interface verification in tests
type mockImplementation struct {
	*graph.Client
}

func (m *mockImplementation) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	if m.Client != nil {
		return m.Client.ExecuteQuery(ctx, query, params)
	}
	return nil, nil
}

func (m *mockImplementation) ExecuteQueryTyped(ctx context.Context, query string, params graph.QueryParams) (*graph.QueryResult, error) {
	if m.Client != nil {
		return m.Client.ExecuteQueryTyped(ctx, query, params)
	}
	return &graph.QueryResult{}, nil
}
