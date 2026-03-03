package assertions

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func init() {
	Enabled = true
}

func TestCheckColumnDuplicates(t *testing.T) {
	t.Run("does not panic on correct records without duplicates", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.message", true),
			semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
			semconv.FieldFromFQN("utf8.label.service", true),
			semconv.FieldFromFQN("utf8.metadata.org_id", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.builtin.message":           "test message",
				"timestamp_ns.builtin.timestamp": time.Unix(1000, 0).UTC(),
				"utf8.label.service":             "api",
				"utf8.metadata.org_id":           "1",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.NotPanics(t, func() {
			CheckColumnDuplicates(record)
		})
	})

	t.Run("panics on duplicate column names", func(t *testing.T) {
		// Create a schema with duplicate column names
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.service", true),
			semconv.FieldFromFQN("utf8.label.env", true),
			semconv.FieldFromFQN("utf8.label.service", true), // duplicate
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.service": "api",
				"utf8.label.env":     "prod",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.PanicsWithValue(t, "duplicate column name: utf8.label.service", func() {
			CheckColumnDuplicates(record)
		})
	})

	t.Run("does not panic on nil record", func(t *testing.T) {
		require.NotPanics(t, func() {
			CheckColumnDuplicates(nil)
		})
	})
}

func TestCheckLabelValuesDuplicates(t *testing.T) {
	t.Run("does not panic on correct records with same short name but different values", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.status", true),
			semconv.FieldFromFQN("utf8.metadata.status", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.status":    "success",
				"utf8.metadata.status": nil,
			},
			{
				"utf8.label.status":    nil,
				"utf8.metadata.status": "200",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.NotPanics(t, func() {
			CheckLabelValuesDuplicates(record)
		})
	})

	t.Run("does not panic when both columns with same short name are null", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.status", true),
			semconv.FieldFromFQN("utf8.metadata.status", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.status":    nil,
				"utf8.metadata.status": nil,
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.NotPanics(t, func() {
			CheckLabelValuesDuplicates(record)
		})
	})

	t.Run("does not panic when columns with same short name have empty strings", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.status", true),
			semconv.FieldFromFQN("utf8.metadata.status", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.status":    "",
				"utf8.metadata.status": "200",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.NotPanics(t, func() {
			CheckLabelValuesDuplicates(record)
		})
	})

	t.Run("panics on duplicate label values with same short name", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.status", true),
			semconv.FieldFromFQN("utf8.metadata.status", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.status":    "success",
				"utf8.metadata.status": "200",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.PanicsWithValue(t, "duplicate label values: status=success", func() {
			CheckLabelValuesDuplicates(record)
		})
	})

	t.Run("panics on duplicate label values across multiple rows", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.status", true),
			semconv.FieldFromFQN("utf8.metadata.status", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.status":    "success",
				"utf8.metadata.status": nil,
			},
			{
				"utf8.label.status":    "error",
				"utf8.metadata.status": "500",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.PanicsWithValue(t, "duplicate label values: status=error", func() {
			CheckLabelValuesDuplicates(record)
		})
	})

	t.Run("does not panic on nil record", func(t *testing.T) {
		require.NotPanics(t, func() {
			CheckLabelValuesDuplicates(nil)
		})
	})

	t.Run("does not panic with unique short names", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.service", true),
			semconv.FieldFromFQN("utf8.label.env", true),
			semconv.FieldFromFQN("utf8.metadata.status", true),
		}, nil)

		rows := arrowtest.Rows{
			{
				"utf8.label.service":   "api",
				"utf8.label.env":       "prod",
				"utf8.metadata.status": "200",
			},
		}

		record := rows.Record(memory.DefaultAllocator, schema)
		defer record.Release()

		require.NotPanics(t, func() {
			CheckLabelValuesDuplicates(record)
		})
	})
}
