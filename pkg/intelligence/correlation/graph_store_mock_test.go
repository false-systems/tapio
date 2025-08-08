package correlation

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockGraphStore is a mock implementation of the GraphStore interface for testing
type MockGraphStore struct {
	mock.Mock
}

// ExecuteQuery mocks the ExecuteQuery method
func (m *MockGraphStore) ExecuteQuery(ctx context.Context, query string, params QueryParams) (ResultIterator, error) {
	args := m.Called(ctx, query, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ResultIterator), args.Error(1)
}

// ExecuteWrite mocks the ExecuteWrite method
func (m *MockGraphStore) ExecuteWrite(ctx context.Context, query string, params QueryParams) error {
	args := m.Called(ctx, query, params)
	return args.Error(0)
}

// ExecuteTypedQuery mocks the ExecuteTypedQuery method
func (m *MockGraphStore) ExecuteTypedQuery(ctx context.Context, query string, params QueryParams) (*QueryResult, error) {
	args := m.Called(ctx, query, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*QueryResult), args.Error(1)
}

// HealthCheck mocks the HealthCheck method
func (m *MockGraphStore) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// BeginTransaction mocks the BeginTransaction method
func (m *MockGraphStore) BeginTransaction(ctx context.Context) (Transaction, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(Transaction), args.Error(1)
}

// MockResultIterator is a mock implementation of ResultIterator for testing
type MockResultIterator struct {
	mock.Mock
	records []*GraphRecord
	index   int
}

// NewMockResultIterator creates a new mock result iterator with given records
func NewMockResultIterator(records []*GraphRecord) *MockResultIterator {
	return &MockResultIterator{
		records: records,
		index:   -1,
	}
}

// Next mocks the Next method
func (m *MockResultIterator) Next(ctx context.Context) bool {
	m.index++
	return m.index < len(m.records)
}

// Record mocks the Record method
func (m *MockResultIterator) Record() *GraphRecord {
	if m.index >= 0 && m.index < len(m.records) {
		return m.records[m.index]
	}
	return nil
}

// Node mocks the Node method
func (m *MockResultIterator) Node(key string) (*GraphNode, error) {
	if m.Record() != nil {
		return m.Record().GetNode(key)
	}
	return nil, ErrNodeNotFound("GraphNode", key)
}

// Relationship mocks the Relationship method
func (m *MockResultIterator) Relationship(key string) (*GraphRelationship, error) {
	if m.Record() != nil {
		return m.Record().GetRelationship(key)
	}
	return nil, ErrNodeNotFound("GraphRelationship", key)
}

// Path mocks the Path method
func (m *MockResultIterator) Path(key string) (*GraphPath, error) {
	if m.Record() != nil {
		return m.Record().GetPath(key)
	}
	return nil, ErrNodeNotFound("GraphPath", key)
}

// Err mocks the Err method
func (m *MockResultIterator) Err() error {
	args := m.Called()
	return args.Error(0)
}

// Close mocks the Close method
func (m *MockResultIterator) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockTransaction is a mock implementation of Transaction for testing
type MockTransaction struct {
	mock.Mock
}

// ExecuteQuery mocks the ExecuteQuery method
func (m *MockTransaction) ExecuteQuery(ctx context.Context, query string, params QueryParams) (ResultIterator, error) {
	args := m.Called(ctx, query, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ResultIterator), args.Error(1)
}

// ExecuteWrite mocks the ExecuteWrite method
func (m *MockTransaction) ExecuteWrite(ctx context.Context, query string, params QueryParams) error {
	args := m.Called(ctx, query, params)
	return args.Error(0)
}

// ExecuteTypedQuery mocks the ExecuteTypedQuery method
func (m *MockTransaction) ExecuteTypedQuery(ctx context.Context, query string, params QueryParams) (*QueryResult, error) {
	args := m.Called(ctx, query, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*QueryResult), args.Error(1)
}

// Commit mocks the Commit method
func (m *MockTransaction) Commit(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// Rollback mocks the Rollback method
func (m *MockTransaction) Rollback(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
