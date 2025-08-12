package neo4j

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockClient provides a mock implementation of the Client for testing
type mockClient struct {
	executeWriteFunc func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error
	parseRecordFunc  func(keys []string, values []any) Record
	extractSummaryFunc func(result neo4j.ResultWithContext) Summary
	callCount        int64
	operationDelay   time.Duration
	returnError      error
	mu               sync.RWMutex
	executedQueries  []string
	executedParams   []map[string]interface{}
}

func (m *mockClient) ExecuteWrite(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
	atomic.AddInt64(&m.callCount, 1)
	
	if m.operationDelay > 0 {
		time.Sleep(m.operationDelay)
	}
	
	if m.returnError != nil {
		return m.returnError
	}
	
	if m.executeWriteFunc != nil {
		return m.executeWriteFunc(ctx, fn)
	}
	
	return nil
}

func (m *mockClient) parseRecord(keys []string, values []any) Record {
	if m.parseRecordFunc != nil {
		return m.parseRecordFunc(keys, values)
	}
	
	return Record{
		StringValues: make(map[string]string),
		IntValues:    make(map[string]int64),
		FloatValues:  make(map[string]float64),
		BoolValues:   make(map[string]bool),
	}
}

func (m *mockClient) extractSummary(result neo4j.ResultWithContext) Summary {
	if m.extractSummaryFunc != nil {
		return m.extractSummaryFunc(result)
	}
	
	return Summary{}
}

func (m *mockClient) GetCallCount() int64 {
	return atomic.LoadInt64(&m.callCount)
}

func (m *mockClient) GetExecutedQueries() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.executedQueries))
	copy(result, m.executedQueries)
	return result
}

func (m *mockClient) GetExecutedParams() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]map[string]interface{}, len(m.executedParams))
	copy(result, m.executedParams)
	return result
}

// mockTransaction provides a mock implementation of neo4j.ManagedTransaction
type mockTransaction struct {
	queries []string
	params  []map[string]interface{}
	records [][]any
	keys    [][]string
	mu      sync.RWMutex
}

func newMockTransaction() *mockTransaction {
	return &mockTransaction{
		queries: make([]string, 0),
		params:  make([]map[string]interface{}, 0),
		records: make([][]any, 0),
		keys:    make([][]string, 0),
	}
}

func (m *mockTransaction) Run(ctx context.Context, query string, params map[string]interface{}) (neo4j.ResultWithContext, error) {
	m.mu.Lock()
	m.queries = append(m.queries, query)
	m.params = append(m.params, params)
	m.mu.Unlock()
	
	return &mockResult{
		records: [][]any{{"test_value"}},
		keys:    []string{"test_key"},
	}, nil
}

func (m *mockTransaction) GetQueries() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.queries))
	copy(result, m.queries)
	return result
}

// mockResult provides a mock implementation of neo4j.ResultWithContext
type mockResult struct {
	records [][]any
	keys    []string
	index   int
	mu      sync.RWMutex
}

func (m *mockResult) Next(ctx context.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.index < len(m.records) {
		m.index++
		return true
	}
	return false
}

func (m *mockResult) Record() *neo4j.Record {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.index <= 0 || m.index > len(m.records) {
		return nil
	}
	
	return &neo4j.Record{
		Values: m.records[m.index-1],
		Keys:   m.keys,
	}
}

func (m *mockResult) Err() error {
	return nil
}

func TestNewBatchClient(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockClient := &mockClient{}

	t.Run("successful creation", func(t *testing.T) {
		config := BatchConfig{
			BatchSize:    50,
			BatchTimeout: 100 * time.Millisecond,
		}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)
		require.NotNil(t, bc)

		assert.Equal(t, 50, bc.config.BatchSize)
		assert.Equal(t, 100*time.Millisecond, bc.config.BatchTimeout)
		assert.False(t, bc.closed)
		assert.False(t, bc.processing)
	})

	t.Run("nil client fails", func(t *testing.T) {
		config := BatchConfig{}
		bc, err := NewBatchClient(nil, config, logger)
		require.Error(t, err)
		assert.Nil(t, bc)
		assert.Contains(t, err.Error(), "client cannot be nil")
	})

	t.Run("nil logger fails", func(t *testing.T) {
		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, nil)
		require.Error(t, err)
		assert.Nil(t, bc)
		assert.Contains(t, err.Error(), "logger cannot be nil")
	})

	t.Run("default config values", func(t *testing.T) {
		config := BatchConfig{} // Empty config should get defaults

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		assert.Equal(t, DefaultBatchSize, bc.config.BatchSize)
		assert.Equal(t, DefaultBatchTimeout, bc.config.BatchTimeout)
	})

	t.Run("invalid config values", func(t *testing.T) {
		config := BatchConfig{
			BatchSize: 2000, // Too large
		}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.Error(t, err)
		assert.Nil(t, bc)
		assert.Contains(t, err.Error(), "batch size too large")
	})
}

func TestBatchClient_ExecuteQueryBatch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("single query execution", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{
			BatchSize:    2,
			BatchTimeout: 100 * time.Millisecond,
		}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		query := "MATCH (n) RETURN n"
		params := QueryParams{}
		params.AddString("test", "value")

		result, err := bc.ExecuteQueryBatch(ctx, query, params)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Wait for batch processing
		time.Sleep(200 * time.Millisecond)

		// Verify the query was executed
		queries := mockTx.GetQueries()
		assert.Contains(t, queries, query)
		assert.Equal(t, int64(1), mockClient.GetCallCount())
	})

	t.Run("batch size trigger", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{
			BatchSize:    2, // Small batch size for testing
			BatchTimeout: 1 * time.Second,
		}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// Execute exactly batch size operations
		results := make([]*QueryResult, config.BatchSize)
		for i := 0; i < config.BatchSize; i++ {
			query := fmt.Sprintf("MATCH (n) WHERE n.id = %d RETURN n", i)
			params := QueryParams{}
			params.AddInt("id", int64(i))
			
			var err error
			results[i], err = bc.ExecuteQueryBatch(ctx, query, params)
			require.NoError(t, err)
		}

		// Wait for batch processing
		time.Sleep(200 * time.Millisecond)

		// Verify all queries were executed in a single batch
		queries := mockTx.GetQueries()
		assert.Len(t, queries, config.BatchSize)
		assert.Equal(t, int64(1), mockClient.GetCallCount()) // Single batch execution

		// Verify results
		for _, result := range results {
			assert.NotNil(t, result)
		}
	})

	t.Run("timeout trigger", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{
			BatchSize:    10, // Large batch size
			BatchTimeout: 50 * time.Millisecond, // Short timeout
		}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		ctx := context.Background()

		// Execute fewer operations than batch size
		query := "MATCH (n) RETURN n"
		params := QueryParams{}
		
		result, err := bc.ExecuteQueryBatch(ctx, query, params)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Wait for timeout-based execution
		time.Sleep(200 * time.Millisecond)

		// Verify the query was executed via timeout
		queries := mockTx.GetQueries()
		assert.Contains(t, queries, query)
		assert.Equal(t, int64(1), mockClient.GetCallCount())
	})

	t.Run("closed client fails", func(t *testing.T) {
		mockClient := &mockClient{}
		config := BatchConfig{}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		// Close the client
		err = bc.Close(context.Background())
		require.NoError(t, err)

		// Try to execute query on closed client
		ctx := context.Background()
		query := "MATCH (n) RETURN n"
		params := QueryParams{}

		result, err := bc.ExecuteQueryBatch(ctx, query, params)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "batch client is closed")
	})
}

func TestBatchClient_ExecuteWriteBatch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("direct batch execution", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		operations := []BatchOperation{
			{
				Query:      "CREATE (n:Test {id: $id})",
				Parameters: map[string]interface{}{"id": 1},
				ID:         "op1",
			},
			{
				Query:      "CREATE (n:Test {id: $id})",
				Parameters: map[string]interface{}{"id": 2},
				ID:         "op2",
			},
		}

		ctx := context.Background()
		results, err := bc.ExecuteWriteBatch(ctx, operations)
		require.NoError(t, err)
		require.Len(t, results, 2)

		// Verify operations were executed
		queries := mockTx.GetQueries()
		assert.Len(t, queries, 2)
		assert.Equal(t, int64(1), mockClient.GetCallCount())
	})

	t.Run("empty operations", func(t *testing.T) {
		mockClient := &mockClient{}
		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		results, err := bc.ExecuteWriteBatch(ctx, []BatchOperation{})
		require.NoError(t, err)
		assert.Empty(t, results)
		assert.Equal(t, int64(0), mockClient.GetCallCount())
	})
}

func TestBatchClient_CreateNodesBatch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("single node type batch", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		nodes := []NodeCreationParams{
			{
				UID:       "pod-1",
				Name:      "test-pod-1",
				Namespace: "default",
				Kind:      "Pod",
				Timestamp: time.Now().Unix(),
			},
			{
				UID:       "pod-2",
				Name:      "test-pod-2",
				Namespace: "default",
				Kind:      "Pod",
				Timestamp: time.Now().Unix(),
			},
		}

		ctx := context.Background()
		err = bc.CreateNodesBatch(ctx, nodes)
		require.NoError(t, err)

		// Verify nodes were created
		queries := mockTx.GetQueries()
		assert.Len(t, queries, 1) // Single query for Pod nodes
		assert.Contains(t, queries[0], "UNWIND $nodes")
		assert.Contains(t, queries[0], "CREATE (n:Pod)")
	})

	t.Run("mixed node types", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		nodes := []NodeCreationParams{
			{
				UID:       "pod-1",
				Name:      "test-pod-1",
				Kind:      "Pod",
				Timestamp: time.Now().Unix(),
			},
			{
				UID:       "svc-1",
				Name:      "test-service-1",
				Kind:      "Service",
				Timestamp: time.Now().Unix(),
			},
		}

		ctx := context.Background()
		err = bc.CreateNodesBatch(ctx, nodes)
		require.NoError(t, err)

		// Verify separate queries for different node types
		queries := mockTx.GetQueries()
		assert.Len(t, queries, 2) // One query per node type
		
		foundPodQuery := false
		foundServiceQuery := false
		for _, query := range queries {
			if strings.Contains(query, "CREATE (n:Pod)") {
				foundPodQuery = true
			}
			if strings.Contains(query, "CREATE (n:Service)") {
				foundServiceQuery = true
			}
		}
		assert.True(t, foundPodQuery, "Pod creation query should exist")
		assert.True(t, foundServiceQuery, "Service creation query should exist")
	})

	t.Run("empty nodes", func(t *testing.T) {
		mockClient := &mockClient{}
		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = bc.CreateNodesBatch(ctx, []NodeCreationParams{})
		require.NoError(t, err)

		assert.Equal(t, int64(0), mockClient.GetCallCount())
	})
}

func TestBatchClient_CreateRelationshipsBatch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("batch relationship creation", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		stringVal := "test"
		intVal := int64(42)
		
		relationships := []RelationshipCreationParams{
			{
				FromUID:   "pod-1",
				ToUID:     "svc-1",
				Timestamp: time.Now().Unix(),
				Properties: map[string]PropertyValue{
					"type": {StringVal: &stringVal},
					"port": {IntVal: &intVal},
				},
			},
		}

		ctx := context.Background()
		err = bc.CreateRelationshipsBatch(ctx, relationships)
		require.NoError(t, err)

		// Verify relationship was created
		queries := mockTx.GetQueries()
		assert.Len(t, queries, 1)
		assert.Contains(t, queries[0], "UNWIND $relationships")
		assert.Contains(t, queries[0], "CREATE (from)-[r:RELATES_TO]->(to)")
	})

	t.Run("empty relationships", func(t *testing.T) {
		mockClient := &mockClient{}
		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		ctx := context.Background()
		err = bc.CreateRelationshipsBatch(ctx, []RelationshipCreationParams{})
		require.NoError(t, err)

		assert.Equal(t, int64(0), mockClient.GetCallCount())
	})
}

func TestBatchClient_PerformanceImprovement(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("batch vs individual operations performance", func(t *testing.T) {
		const operationCount = 100
		const operationDelay = time.Millisecond // Simulate network latency

		// Test individual operations (sequential)
		mockClientSequential := &mockClient{
			operationDelay: operationDelay,
		}
		mockTxSequential := newMockTransaction()
		mockClientSequential.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTxSequential)
		}

		// Simulate sequential execution (old way)
		sequentialStart := time.Now()
		for i := 0; i < operationCount; i++ {
			err := mockClientSequential.ExecuteWrite(context.Background(), func(tx neo4j.ManagedTransaction) error {
				_, err := tx.Run(context.Background(), "CREATE (n:Test {id: $id})", map[string]interface{}{"id": i})
				return err
			})
			require.NoError(t, err)
		}
		sequentialDuration := time.Since(sequentialStart)

		// Test batch operations
		mockClientBatch := &mockClient{
			operationDelay: operationDelay,
		}
		mockTxBatch := newMockTransaction()
		mockClientBatch.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTxBatch)
		}

		config := BatchConfig{
			BatchSize:    operationCount, // Batch everything together
			BatchTimeout: time.Second,
		}
		bc, err := NewBatchClient(mockClientBatch, config, logger)
		require.NoError(t, err)

		// Create batch operations
		operations := make([]BatchOperation, operationCount)
		for i := 0; i < operationCount; i++ {
			operations[i] = BatchOperation{
				Query:      "CREATE (n:Test {id: $id})",
				Parameters: map[string]interface{}{"id": i},
				ID:         fmt.Sprintf("op_%d", i),
			}
		}

		batchStart := time.Now()
		results, err := bc.ExecuteWriteBatch(context.Background(), operations)
		require.NoError(t, err)
		require.Len(t, results, operationCount)
		batchDuration := time.Since(batchStart)

		t.Logf("Performance comparison:")
		t.Logf("  Sequential: %v (%d operations)", sequentialDuration, operationCount)
		t.Logf("  Batch:      %v (%d operations)", batchDuration, operationCount)
		t.Logf("  Improvement: %.1fx faster", float64(sequentialDuration)/float64(batchDuration))

		// Verify we got significant performance improvement (at least 4x as promised)
		improvementFactor := float64(sequentialDuration) / float64(batchDuration)
		assert.Greater(t, improvementFactor, 4.0, "Batch processing should be at least 4x faster")

		// Verify both approaches executed the same number of operations
		assert.Equal(t, int64(operationCount), mockClientSequential.GetCallCount())
		assert.Equal(t, int64(1), mockClientBatch.GetCallCount()) // Single batch
		assert.Len(t, mockTxBatch.GetQueries(), operationCount)
	})
}

func TestBatchClient_FlushAndClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("flush pending operations", func(t *testing.T) {
		mockClient := &mockClient{}
		mockTx := newMockTransaction()
		
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{
			BatchSize:    10, // Large batch size
			BatchTimeout: time.Hour, // Long timeout to prevent auto-execution
		}

		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		// Add some operations that won't trigger batch execution
		ctx := context.Background()
		query := "CREATE (n:Test {id: $id})"
		params := QueryParams{}
		params.AddInt("id", 1)

		// Execute operation asynchronously (don't wait for result)
		go bc.ExecuteQueryBatch(ctx, query, params)
		
		// Give some time for operation to be added to batch
		time.Sleep(50 * time.Millisecond)

		// Verify no execution yet
		assert.Equal(t, int64(0), mockClient.GetCallCount())

		// Flush should trigger execution
		err = bc.Flush(ctx)
		require.NoError(t, err)

		// Verify execution occurred
		assert.Equal(t, int64(1), mockClient.GetCallCount())
		queries := mockTx.GetQueries()
		assert.Contains(t, queries, query)
	})

	t.Run("close client gracefully", func(t *testing.T) {
		mockClient := &mockClient{}
		config := BatchConfig{}
		bc, err := NewBatchClient(mockClient, config, logger)
		require.NoError(t, err)

		stats := bc.Stats()
		assert.False(t, stats["closed"].(bool))

		err = bc.Close(context.Background())
		require.NoError(t, err)

		stats = bc.Stats()
		assert.True(t, stats["closed"].(bool))

		// Double close should not error
		err = bc.Close(context.Background())
		require.NoError(t, err)
	})
}

func TestBatchClient_Stats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockClient := &mockClient{}
	
	config := BatchConfig{
		BatchSize:    50,
		BatchTimeout: 200 * time.Millisecond,
	}

	bc, err := NewBatchClient(mockClient, config, logger)
	require.NoError(t, err)

	stats := bc.Stats()
	
	assert.Equal(t, 0, stats["pending_operations"].(int))
	assert.Equal(t, 50, stats["batch_size"].(int))
	assert.Equal(t, int64(200), stats["batch_timeout_ms"].(int64))
	assert.False(t, stats["processing"].(bool))
	assert.False(t, stats["closed"].(bool))
}

// Benchmark to measure actual performance improvement
func BenchmarkBatchClient(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("individual_operations", func(b *testing.B) {
		mockClient := &mockClient{
			operationDelay: 100 * time.Microsecond, // Simulate network latency
		}
		mockTx := newMockTransaction()
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			err := mockClient.ExecuteWrite(context.Background(), func(tx neo4j.ManagedTransaction) error {
				_, err := tx.Run(context.Background(), "CREATE (n:Test {id: $id})", map[string]interface{}{"id": i})
				return err
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("batch_operations", func(b *testing.B) {
		mockClient := &mockClient{
			operationDelay: 100 * time.Microsecond, // Same latency as individual
		}
		mockTx := newMockTransaction()
		mockClient.executeWriteFunc = func(ctx context.Context, fn func(tx neo4j.ManagedTransaction) error) error {
			return fn(mockTx)
		}

		config := BatchConfig{
			BatchSize:    100, // Batch operations together
			BatchTimeout: 10 * time.Millisecond,
		}
		bc, err := NewBatchClient(mockClient, config, logger)
		if err != nil {
			b.Fatal(err)
		}

		// Create batch operations
		operations := make([]BatchOperation, b.N)
		for i := 0; i < b.N; i++ {
			operations[i] = BatchOperation{
				Query:      "CREATE (n:Test {id: $id})",
				Parameters: map[string]interface{}{"id": i},
				ID:         fmt.Sprintf("op_%d", i),
			}
		}

		b.ResetTimer()
		_, err = bc.ExecuteWriteBatch(context.Background(), operations)
		if err != nil {
			b.Fatal(err)
		}
	})
}