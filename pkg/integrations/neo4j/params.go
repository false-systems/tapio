package neo4j

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// QueryParams provides a type-safe way to build Neo4j query parameters
// This encapsulates the map[string]interface{} requirement of the Neo4j driver
// while maintaining strict type safety in the rest of our codebase
type QueryParams struct {
	params map[string]any // Internal use only - never exposed
}

// NewQueryParams creates a new type-safe parameter builder
func NewQueryParams() *QueryParams {
	return &QueryParams{
		params: make(map[string]any),
	}
}

// SetString adds a string parameter
func (q *QueryParams) SetString(key, value string) *QueryParams {
	q.params[key] = value
	return q
}

// SetStringPtr adds an optional string parameter
func (q *QueryParams) SetStringPtr(key string, value *string) *QueryParams {
	if value != nil {
		q.params[key] = *value
	} else {
		q.params[key] = nil
	}
	return q
}

// SetInt adds an integer parameter
func (q *QueryParams) SetInt(key string, value int) *QueryParams {
	q.params[key] = value
	return q
}

// SetInt64 adds an int64 parameter
func (q *QueryParams) SetInt64(key string, value int64) *QueryParams {
	q.params[key] = value
	return q
}

// SetFloat64 adds a float64 parameter
func (q *QueryParams) SetFloat64(key string, value float64) *QueryParams {
	q.params[key] = value
	return q
}

// SetBool adds a boolean parameter
func (q *QueryParams) SetBool(key string, value bool) *QueryParams {
	q.params[key] = value
	return q
}

// SetTime adds a time parameter as Unix timestamp
func (q *QueryParams) SetTime(key string, value time.Time) *QueryParams {
	q.params[key] = value.Unix()
	return q
}

// SetTimePtr adds an optional time parameter
func (q *QueryParams) SetTimePtr(key string, value *time.Time) *QueryParams {
	if value != nil {
		q.params[key] = value.Unix()
	} else {
		q.params[key] = nil
	}
	return q
}

// SetStringSlice adds a string slice parameter
func (q *QueryParams) SetStringSlice(key string, values []string) *QueryParams {
	q.params[key] = values
	return q
}

// SetIntSlice adds an integer slice parameter
func (q *QueryParams) SetIntSlice(key string, values []int) *QueryParams {
	q.params[key] = values
	return q
}

// SetRaw adds a raw value for edge cases (use sparingly)
func (q *QueryParams) SetRaw(key string, value any) *QueryParams {
	q.params[key] = value
	return q
}

// build returns the internal map for use with Neo4j driver
// This method is package-private and only used internally
func (q *QueryParams) build() map[string]any {
	if q.params == nil {
		return nil
	}
	return q.params
}

// Size returns the number of parameters
func (q *QueryParams) Size() int {
	return len(q.params)
}

// Has checks if a parameter exists
func (q *QueryParams) Has(key string) bool {
	_, exists := q.params[key]
	return exists
}

// QueryBuilder provides a fluent interface for building Cypher queries with type-safe parameters
type QueryBuilder struct {
	query  strings.Builder
	params *QueryParams
}

// NewQueryBuilder creates a new query builder
func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		params: NewQueryParams(),
	}
}

// Query appends to the query string
func (b *QueryBuilder) Query(query string) *QueryBuilder {
	b.query.WriteString(query)
	return b
}

// Queryf appends a formatted query string
func (b *QueryBuilder) Queryf(format string, args ...any) *QueryBuilder {
	b.query.WriteString(fmt.Sprintf(format, args...))
	return b
}

// Param adds a parameter using the fluent interface
func (b *QueryBuilder) Param(key string, value any) *QueryBuilder {
	switch v := value.(type) {
	case string:
		b.params.SetString(key, v)
	case *string:
		b.params.SetStringPtr(key, v)
	case int:
		b.params.SetInt(key, v)
	case int64:
		b.params.SetInt64(key, v)
	case float64:
		b.params.SetFloat64(key, v)
	case bool:
		b.params.SetBool(key, v)
	case time.Time:
		b.params.SetTime(key, v)
	case *time.Time:
		b.params.SetTimePtr(key, v)
	case []string:
		b.params.SetStringSlice(key, v)
	case []int:
		b.params.SetIntSlice(key, v)
	default:
		b.params.SetRaw(key, v)
	}
	return b
}

// Build returns the query string and parameters
func (b *QueryBuilder) Build() (string, *QueryParams) {
	return b.query.String(), b.params
}

// StructToParams converts a struct to QueryParams using struct tags
// Example struct:
//
//	type EventParams struct {
//	    ID        string    `neo4j:"id"`
//	    Timestamp time.Time `neo4j:"timestamp"`
//	    Count     int       `neo4j:"count,omitempty"`
//	}
func StructToParams(v any) (*QueryParams, error) {
	params := NewQueryParams()

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %v", val.Kind())
	}

	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Parse struct tag
		tag := field.Tag.Get("neo4j")
		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		paramName := parts[0]
		omitempty := len(parts) > 1 && parts[1] == "omitempty"

		// Skip zero values if omitempty is set
		if omitempty && fieldValue.IsZero() {
			continue
		}

		// Add parameter based on type
		if err := addFieldToParams(params, paramName, fieldValue); err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
	}

	return params, nil
}

// addFieldToParams adds a struct field to the parameters
func addFieldToParams(params *QueryParams, name string, value reflect.Value) error {
	// Handle pointer types
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			params.SetRaw(name, nil)
			return nil
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.String:
		params.SetString(name, value.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		params.SetInt64(name, value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		params.SetInt64(name, int64(value.Uint()))
	case reflect.Float32, reflect.Float64:
		params.SetFloat64(name, value.Float())
	case reflect.Bool:
		params.SetBool(name, value.Bool())
	case reflect.Slice:
		// Handle common slice types
		switch value.Interface().(type) {
		case []string:
			params.SetStringSlice(name, value.Interface().([]string))
		case []int:
			params.SetIntSlice(name, value.Interface().([]int))
		default:
			params.SetRaw(name, value.Interface())
		}
	default:
		// Check if it's a time.Time
		if t, ok := value.Interface().(time.Time); ok {
			params.SetTime(name, t)
		} else {
			params.SetRaw(name, value.Interface())
		}
	}

	return nil
}

// RunWithParams executes a query with type-safe parameters
// This is an extension method for neo4j.ManagedTransaction
func RunWithParams(ctx context.Context, tx neo4j.ManagedTransaction, query string, params *QueryParams) (neo4j.ResultWithContext, error) {
	var p map[string]any
	if params != nil {
		p = params.build()
	}
	return tx.Run(ctx, query, p)
}

// TransactionWork wraps a transaction function with type-safe parameters
type TransactionWork func(ctx context.Context, tx *TypedTransaction) error

// TypedTransaction wraps neo4j.ManagedTransaction with type-safe methods
type TypedTransaction struct {
	tx neo4j.ManagedTransaction
}

// Run executes a query with typed parameters
func (t *TypedTransaction) Run(ctx context.Context, query string, params *QueryParams) (neo4j.ResultWithContext, error) {
	return RunWithParams(ctx, t.tx, query, params)
}

// RunQuery executes a QueryBuilder query
func (t *TypedTransaction) RunQuery(ctx context.Context, builder *QueryBuilder) (neo4j.ResultWithContext, error) {
	query, params := builder.Build()
	return t.Run(ctx, query, params)
}

// RunStruct executes a query with struct-based parameters
func (t *TypedTransaction) RunStruct(ctx context.Context, query string, structParams any) (neo4j.ResultWithContext, error) {
	params, err := StructToParams(structParams)
	if err != nil {
		return nil, fmt.Errorf("failed to convert struct to params: %w", err)
	}
	return t.Run(ctx, query, params)
}

// wrapTransaction wraps a neo4j.ManagedTransaction with our typed wrapper
func wrapTransaction(tx neo4j.ManagedTransaction) *TypedTransaction {
	return &TypedTransaction{tx: tx}
}
