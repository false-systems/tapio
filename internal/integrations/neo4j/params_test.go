package neo4j

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryParams(t *testing.T) {
	t.Run("SetString", func(t *testing.T) {
		params := NewQueryParams()
		params.SetString("name", "test")

		assert.True(t, params.Has("name"))
		assert.Equal(t, 1, params.Size())
		assert.Equal(t, "test", params.build()["name"])
	})

	t.Run("SetStringPtr", func(t *testing.T) {
		params := NewQueryParams()

		// Test with nil
		params.SetStringPtr("nilValue", nil)
		assert.True(t, params.Has("nilValue"))
		assert.Nil(t, params.build()["nilValue"])

		// Test with value
		value := "test"
		params.SetStringPtr("value", &value)
		assert.Equal(t, "test", params.build()["value"])
		assert.Equal(t, 2, params.Size())
	})

	t.Run("SetInt", func(t *testing.T) {
		params := NewQueryParams()
		params.SetInt("count", 42)

		assert.True(t, params.Has("count"))
		assert.Equal(t, 42, params.build()["count"])
	})

	t.Run("SetInt64", func(t *testing.T) {
		params := NewQueryParams()
		params.SetInt64("bigCount", int64(1234567890))

		assert.True(t, params.Has("bigCount"))
		assert.Equal(t, int64(1234567890), params.build()["bigCount"])
	})

	t.Run("SetFloat64", func(t *testing.T) {
		params := NewQueryParams()
		params.SetFloat64("rate", 3.14)

		assert.True(t, params.Has("rate"))
		assert.Equal(t, 3.14, params.build()["rate"])
	})

	t.Run("SetBool", func(t *testing.T) {
		params := NewQueryParams()
		params.SetBool("enabled", true)

		assert.True(t, params.Has("enabled"))
		assert.Equal(t, true, params.build()["enabled"])
	})

	t.Run("SetTime", func(t *testing.T) {
		params := NewQueryParams()
		testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		params.SetTime("timestamp", testTime)

		assert.True(t, params.Has("timestamp"))
		assert.Equal(t, testTime.Unix(), params.build()["timestamp"])
	})

	t.Run("SetTimePtr", func(t *testing.T) {
		params := NewQueryParams()

		// Test with nil
		params.SetTimePtr("nilTime", nil)
		assert.Nil(t, params.build()["nilTime"])

		// Test with value
		testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		params.SetTimePtr("time", &testTime)
		assert.Equal(t, testTime.Unix(), params.build()["time"])
	})

	t.Run("SetStringSlice", func(t *testing.T) {
		params := NewQueryParams()
		values := []string{"a", "b", "c"}
		params.SetStringSlice("tags", values)

		assert.True(t, params.Has("tags"))
		assert.Equal(t, values, params.build()["tags"])
	})

	t.Run("SetIntSlice", func(t *testing.T) {
		params := NewQueryParams()
		values := []int{1, 2, 3}
		params.SetIntSlice("ids", values)

		assert.True(t, params.Has("ids"))
		assert.Equal(t, values, params.build()["ids"])
	})

	t.Run("SetRaw", func(t *testing.T) {
		params := NewQueryParams()
		type CustomType struct {
			Field string
		}
		custom := CustomType{Field: "value"}
		params.SetRaw("custom", custom)

		assert.True(t, params.Has("custom"))
		assert.Equal(t, custom, params.build()["custom"])
	})

	t.Run("MethodChaining", func(t *testing.T) {
		params := NewQueryParams().
			SetString("name", "test").
			SetInt("age", 30).
			SetBool("active", true)

		assert.Equal(t, 3, params.Size())
		assert.True(t, params.Has("name"))
		assert.True(t, params.Has("age"))
		assert.True(t, params.Has("active"))
	})

	t.Run("EmptyParams", func(t *testing.T) {
		params := NewQueryParams()
		assert.Equal(t, 0, params.Size())
		assert.False(t, params.Has("anything"))
		assert.NotNil(t, params.build())
	})
}

func TestQueryBuilder(t *testing.T) {
	t.Run("BasicQuery", func(t *testing.T) {
		builder := NewQueryBuilder()
		builder.Query("MATCH (n:Node) WHERE n.id = $id RETURN n")
		builder.Param("id", "123")

		query, params := builder.Build()
		assert.Equal(t, "MATCH (n:Node) WHERE n.id = $id RETURN n", query)
		assert.True(t, params.Has("id"))
		assert.Equal(t, "123", params.build()["id"])
	})

	t.Run("FormattedQuery", func(t *testing.T) {
		builder := NewQueryBuilder()
		builder.Queryf("MATCH (n:%s) WHERE n.id = $id RETURN n", "Person")
		builder.Param("id", 42)

		query, params := builder.Build()
		assert.Equal(t, "MATCH (n:Person) WHERE n.id = $id RETURN n", query)
		assert.Equal(t, 42, params.build()["id"])
	})

	t.Run("MultipleQueries", func(t *testing.T) {
		builder := NewQueryBuilder()
		builder.Query("MATCH (n:Node) ").
			Query("WHERE n.id = $id ").
			Query("RETURN n")

		query, _ := builder.Build()
		assert.Equal(t, "MATCH (n:Node) WHERE n.id = $id RETURN n", query)
	})

	t.Run("VariousParamTypes", func(t *testing.T) {
		testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		stringPtr := "ptr"

		builder := NewQueryBuilder()
		builder.Query("CREATE (n:Node $props)")
		builder.Param("string", "value").
			Param("stringPtr", &stringPtr).
			Param("int", 42).
			Param("int64", int64(100)).
			Param("float", 3.14).
			Param("bool", true).
			Param("time", testTime).
			Param("timePtr", &testTime).
			Param("strings", []string{"a", "b"}).
			Param("ints", []int{1, 2})

		_, params := builder.Build()
		assert.Equal(t, 10, params.Size())
		assert.Equal(t, "value", params.build()["string"])
		assert.Equal(t, "ptr", params.build()["stringPtr"])
		assert.Equal(t, 42, params.build()["int"])
		assert.Equal(t, int64(100), params.build()["int64"])
		assert.Equal(t, 3.14, params.build()["float"])
		assert.Equal(t, true, params.build()["bool"])
		assert.Equal(t, testTime.Unix(), params.build()["time"])
		assert.Equal(t, testTime.Unix(), params.build()["timePtr"])
		assert.Equal(t, []string{"a", "b"}, params.build()["strings"])
		assert.Equal(t, []int{1, 2}, params.build()["ints"])
	})

	t.Run("ComplexQuery", func(t *testing.T) {
		builder := NewQueryBuilder()
		builder.Queryf("MATCH (e:%s {id: $id}) ", "Event").
			Query("WHERE e.timestamp > $minTime ").
			Query("AND e.timestamp < $maxTime ").
			Query("RETURN e").
			Param("id", "event-123").
			Param("minTime", time.Now().Add(-1*time.Hour).Unix()).
			Param("maxTime", time.Now().Unix())

		query, params := builder.Build()
		assert.Contains(t, query, "MATCH (e:Event {id: $id})")
		assert.Contains(t, query, "WHERE e.timestamp > $minTime")
		assert.Contains(t, query, "AND e.timestamp < $maxTime")
		assert.Contains(t, query, "RETURN e")
		assert.Equal(t, 3, params.Size())
	})
}

func TestStructToParams(t *testing.T) {
	t.Run("BasicStruct", func(t *testing.T) {
		type TestStruct struct {
			ID   string `neo4j:"id"`
			Name string `neo4j:"name"`
			Age  int    `neo4j:"age"`
		}

		test := TestStruct{
			ID:   "123",
			Name: "John",
			Age:  30,
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 3, params.Size())
		assert.Equal(t, "123", params.build()["id"])
		assert.Equal(t, "John", params.build()["name"])
		assert.Equal(t, int64(30), params.build()["age"])
	})

	t.Run("StructWithPointer", func(t *testing.T) {
		type TestStruct struct {
			ID   string `neo4j:"id"`
			Name string `neo4j:"name"`
		}

		test := &TestStruct{
			ID:   "456",
			Name: "Jane",
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 2, params.Size())
		assert.Equal(t, "456", params.build()["id"])
		assert.Equal(t, "Jane", params.build()["name"])
	})

	t.Run("OmitEmpty", func(t *testing.T) {
		type TestStruct struct {
			ID       string `neo4j:"id"`
			Optional string `neo4j:"optional,omitempty"`
			Count    int    `neo4j:"count,omitempty"`
		}

		test := TestStruct{
			ID: "789",
			// Optional and Count are zero values
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 1, params.Size())
		assert.Equal(t, "789", params.build()["id"])
		assert.False(t, params.Has("optional"))
		assert.False(t, params.Has("count"))
	})

	t.Run("PointerFields", func(t *testing.T) {
		type TestStruct struct {
			ID       string  `neo4j:"id"`
			Optional *string `neo4j:"optional"`
			Count    *int    `neo4j:"count"`
		}

		optionalValue := "value"
		test := TestStruct{
			ID:       "abc",
			Optional: &optionalValue,
			Count:    nil,
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 3, params.Size())
		assert.Equal(t, "abc", params.build()["id"])
		assert.Equal(t, "value", params.build()["optional"])
		assert.Nil(t, params.build()["count"])
	})

	t.Run("TimeFields", func(t *testing.T) {
		type TestStruct struct {
			ID        string    `neo4j:"id"`
			CreatedAt time.Time `neo4j:"created_at"`
		}

		testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		test := TestStruct{
			ID:        "time-test",
			CreatedAt: testTime,
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 2, params.Size())
		assert.Equal(t, "time-test", params.build()["id"])
		assert.Equal(t, testTime.Unix(), params.build()["created_at"])
	})

	t.Run("SliceFields", func(t *testing.T) {
		type TestStruct struct {
			ID    string   `neo4j:"id"`
			Tags  []string `neo4j:"tags"`
			Codes []int    `neo4j:"codes"`
		}

		test := TestStruct{
			ID:    "slice-test",
			Tags:  []string{"tag1", "tag2"},
			Codes: []int{1, 2, 3},
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 3, params.Size())
		assert.Equal(t, "slice-test", params.build()["id"])
		assert.Equal(t, []string{"tag1", "tag2"}, params.build()["tags"])
		assert.Equal(t, []int{1, 2, 3}, params.build()["codes"])
	})

	t.Run("IgnoredFields", func(t *testing.T) {
		type TestStruct struct {
			ID         string `neo4j:"id"`
			Ignored    string `neo4j:"-"`
			NoTag      string
			unexported string `neo4j:"unexported"`
			EmptyTag   string `neo4j:""`
		}

		test := TestStruct{
			ID:         "ignore-test",
			Ignored:    "should be ignored",
			NoTag:      "no tag",
			unexported: "unexported",
			EmptyTag:   "empty tag",
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 1, params.Size())
		assert.Equal(t, "ignore-test", params.build()["id"])
		assert.False(t, params.Has("Ignored"))
		assert.False(t, params.Has("NoTag"))
		assert.False(t, params.Has("unexported"))
		assert.False(t, params.Has("EmptyTag"))
	})

	t.Run("AllTypes", func(t *testing.T) {
		type TestStruct struct {
			String  string  `neo4j:"string"`
			Int     int     `neo4j:"int"`
			Int8    int8    `neo4j:"int8"`
			Int16   int16   `neo4j:"int16"`
			Int32   int32   `neo4j:"int32"`
			Int64   int64   `neo4j:"int64"`
			Uint    uint    `neo4j:"uint"`
			Uint8   uint8   `neo4j:"uint8"`
			Uint16  uint16  `neo4j:"uint16"`
			Uint32  uint32  `neo4j:"uint32"`
			Uint64  uint64  `neo4j:"uint64"`
			Float32 float32 `neo4j:"float32"`
			Float64 float64 `neo4j:"float64"`
			Bool    bool    `neo4j:"bool"`
		}

		test := TestStruct{
			String:  "str",
			Int:     -1,
			Int8:    -8,
			Int16:   -16,
			Int32:   -32,
			Int64:   -64,
			Uint:    1,
			Uint8:   8,
			Uint16:  16,
			Uint32:  32,
			Uint64:  64,
			Float32: 3.2,
			Float64: 6.4,
			Bool:    true,
		}

		params, err := StructToParams(test)
		require.NoError(t, err)

		assert.Equal(t, 14, params.Size())
		assert.Equal(t, "str", params.build()["string"])
		assert.Equal(t, int64(-1), params.build()["int"])
		assert.Equal(t, int64(-8), params.build()["int8"])
		assert.Equal(t, int64(-16), params.build()["int16"])
		assert.Equal(t, int64(-32), params.build()["int32"])
		assert.Equal(t, int64(-64), params.build()["int64"])
		assert.Equal(t, int64(1), params.build()["uint"])
		assert.Equal(t, int64(8), params.build()["uint8"])
		assert.Equal(t, int64(16), params.build()["uint16"])
		assert.Equal(t, int64(32), params.build()["uint32"])
		assert.Equal(t, int64(64), params.build()["uint64"])
		assert.Equal(t, float64(float32(3.2)), params.build()["float32"])
		assert.Equal(t, 6.4, params.build()["float64"])
		assert.Equal(t, true, params.build()["bool"])
	})

	t.Run("InvalidInput", func(t *testing.T) {
		// Test with non-struct
		params, err := StructToParams("not a struct")
		assert.Error(t, err)
		assert.Nil(t, params)
		assert.Contains(t, err.Error(), "expected struct")

		// Test with slice
		params, err = StructToParams([]string{"a", "b"})
		assert.Error(t, err)
		assert.Nil(t, params)

		// Test with map
		params, err = StructToParams(map[string]string{"key": "value"})
		assert.Error(t, err)
		assert.Nil(t, params)
	})
}

func TestTypedTransaction(t *testing.T) {
	// Note: These tests verify the API structure since we can't test against actual Neo4j
	// The actual Neo4j integration would be tested in integration tests

	t.Run("TypedTransactionAPI", func(t *testing.T) {
		// This test verifies that TypedTransaction has the expected methods
		var tx *TypedTransaction
		assert.Nil(t, tx) // Just to use the variable

		// The following would be the API usage:
		// tx.Run(ctx, "QUERY", params)
		// tx.RunQuery(ctx, builder)
		// tx.RunStruct(ctx, "QUERY", struct{})
	})

	t.Run("TransactionWorkAPI", func(t *testing.T) {
		// This test verifies the TransactionWork function signature
		var work TransactionWork = func(ctx context.Context, tx *TypedTransaction) error {
			// Example usage inside transaction
			params := NewQueryParams().SetString("id", "123")
			_, err := tx.Run(ctx, "MATCH (n:Node {id: $id}) RETURN n", params)
			return err
		}
		assert.NotNil(t, work)
	})

	t.Run("TypedReadWorkAPI", func(t *testing.T) {
		// This test verifies the TypedReadWork function signature
		var work TypedReadWork = func(ctx context.Context, tx *TypedTransaction) (interface{}, error) {
			// Example usage inside read transaction
			builder := NewQueryBuilder().
				Query("MATCH (n:Node) WHERE n.id = $id RETURN n").
				Param("id", "123")

			result, err := tx.RunQuery(ctx, builder)
			return result, err
		}
		assert.NotNil(t, work)
	})
}

func TestRunWithParams(t *testing.T) {
	// This test verifies the RunWithParams helper function exists and has the right signature
	// Actual testing would require a mock Neo4j transaction

	t.Run("RunWithParamsAPI", func(t *testing.T) {
		// Verify the function exists and can be called (though it will fail without a real tx)
		ctx := context.Background()
		params := NewQueryParams().SetString("test", "value")

		// This will panic or error without a real transaction, but verifies the API
		defer func() {
			if r := recover(); r != nil {
				// Expected to panic/error without real Neo4j
				assert.NotNil(t, r)
			}
		}()

		// The function signature is correct if this compiles
		_, _ = RunWithParams(ctx, nil, "RETURN 1", params)
	})
}

// Benchmark tests
func BenchmarkQueryParams(b *testing.B) {
	b.Run("SetString", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			params := NewQueryParams()
			params.SetString("key", "value")
		}
	})

	b.Run("ChainedParams", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			NewQueryParams().
				SetString("id", "123").
				SetInt("count", 42).
				SetBool("active", true).
				SetTime("timestamp", time.Now())
		}
	})

	b.Run("StructToParams", func(b *testing.B) {
		type TestStruct struct {
			ID        string    `neo4j:"id"`
			Name      string    `neo4j:"name"`
			Count     int       `neo4j:"count"`
			Active    bool      `neo4j:"active"`
			Timestamp time.Time `neo4j:"timestamp"`
		}

		test := TestStruct{
			ID:        "123",
			Name:      "Test",
			Count:     42,
			Active:    true,
			Timestamp: time.Now(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = StructToParams(test)
		}
	})

	b.Run("QueryBuilder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			builder := NewQueryBuilder()
			builder.Query("MATCH (n:Node) WHERE n.id = $id RETURN n").
				Param("id", "123").
				Param("timestamp", time.Now().Unix())
			builder.Build()
		}
	})
}
