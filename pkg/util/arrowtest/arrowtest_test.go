package arrowtest_test

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestRows_Schema(t *testing.T) {
	t.Run("Consistent types", func(t *testing.T) {
		rows := arrowtest.Rows{
			{"name": "Alice", "age": int64(30), "location": "Wonderland"},
			{"name": "Bob", "age": int64(25), "location": "Builderland"},
		}

		expect := arrow.NewSchema([]arrow.Field{
			{Name: "age", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "location", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		actual := rows.Schema()

		require.True(t, expect.Equal(actual), "schemas are not equal:\nexpected: %s\nactual: %s", expect, actual)
	})

	t.Run("Null at start", func(t *testing.T) {
		rows := arrowtest.Rows{
			{"name": "Alice", "age": int64(30), "location": nil},
			{"name": "Bob", "age": int64(25), "location": "Builderland"},
		}

		expect := arrow.NewSchema([]arrow.Field{
			{Name: "age", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "location", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		actual := rows.Schema()

		require.True(t, expect.Equal(actual), "schemas are not equal:\nexpected: %s\nactual: %s", expect, actual)
	})

	t.Run("Null at end", func(t *testing.T) {
		rows := arrowtest.Rows{
			{"name": "Alice", "age": int64(30), "location": "Wonderland"},
			{"name": "Bob", "age": int64(25), "location": nil},
		}

		expect := arrow.NewSchema([]arrow.Field{
			{Name: "age", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "location", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)

		actual := rows.Schema()

		require.True(t, expect.Equal(actual), "schemas are not equal:\nexpected: %s\nactual: %s", expect, actual)
	})
}

func TestRows_Record(t *testing.T) {
	expect := arrowtest.Rows{
		{"name": "Alice", "age": int64(30), "location": "Wonderland"},
		{"name": "Bob", "age": int64(25), "location": "Builderland"},
		{"name": "Dennis", "age": int64(40), "location": nil},
		{"name": "Eve", "age": int64(35), "location": "Eden"},
	}

	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	record := expect.Record(alloc, expect.Schema())
	defer record.Release()

	actual, err := arrowtest.RecordRows(record)
	require.NoError(t, err)

	require.Equal(t, expect, actual)
}

func TestRows_Record_Bytes(t *testing.T) {
	expect := arrowtest.Rows{
		{"name": []byte("Alice")},
		{"name": []byte("Bob")},
		{"name": []byte("Dennis")},
		{"name": []byte("Eve")},
	}

	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	record := expect.Record(alloc, expect.Schema())
	defer record.Release()

	actual, err := arrowtest.RecordRows(record)
	require.NoError(t, err)

	require.Equal(t, expect, actual)
}

func TestRows_Record_Time(t *testing.T) {
	expect := arrowtest.Rows{
		{"name": "Alice", "last_ping": time.Unix(30, 0)},
		{"name": "Bob", "last_ping": time.Unix(25, 0)},
		{"name": "Dennis", "last_ping": time.Unix(40, 0)},
		{"name": "Eve", "last_ping": time.Unix(35, 0)},
	}

	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	record := expect.Record(alloc, expect.Schema())
	defer record.Release()

	actual, err := arrowtest.RecordRows(record)
	require.NoError(t, err)

	require.Equal(t, expect, actual)
}
