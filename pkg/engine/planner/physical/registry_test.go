package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestColumnRegistry_BasicRegistration(t *testing.T) {
	t.Run("Builtin columns are pre-registered", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Verify builtin columns exist
		col, err := registry.ResolveColumn(types.ColumnNameBuiltinTimestamp)
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeBuiltin, col.Type)
		require.Equal(t, types.ColumnNameBuiltinTimestamp, col.Column)

		col, err = registry.ResolveColumn(types.ColumnNameBuiltinMessage)
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeBuiltin, col.Type)
		require.Equal(t, types.ColumnNameBuiltinMessage, col.Column)
	})

	t.Run("Register label columns", func(t *testing.T) {
		registry := NewColumnRegistry()

		err := registry.RegisterLabelColumns([]string{"app", "namespace", "pod"})
		require.NoError(t, err)

		col, err := registry.ResolveColumn("app")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeLabel, col.Type)
		require.Equal(t, "app", col.Column)

		col, err = registry.ResolveColumn("namespace")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeLabel, col.Type)
	})

	t.Run("Register metadata columns", func(t *testing.T) {
		registry := NewColumnRegistry()

		err := registry.RegisterMetadataColumns([]string{"trace_id", "span_id"})
		require.NoError(t, err)

		col, err := registry.ResolveColumn("trace_id")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeMetadata, col.Type)
		require.Equal(t, "trace_id", col.Column)
	})

	t.Run("Register parsed columns", func(t *testing.T) {
		registry := NewColumnRegistry()

		err := registry.RegisterParsedColumns([]string{"level", "status", "duration"})
		require.NoError(t, err)

		col, err := registry.ResolveColumn("level")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeParsed, col.Type)
		require.Equal(t, "level", col.Column)
	})

	t.Run("Register generated columns", func(t *testing.T) {
		registry := NewColumnRegistry()

		err := registry.RegisterGeneratedColumns([]string{"value", "computed_field"})
		require.NoError(t, err)

		col, err := registry.ResolveColumn("value")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeGenerated, col.Type)
		require.Equal(t, "value", col.Column)
	})

	t.Run("Column not found error", func(t *testing.T) {
		registry := NewColumnRegistry()

		_, err := registry.ResolveColumn("nonexistent")
		require.Error(t, err)
		require.Contains(t, err.Error(), "column not found: nonexistent")
	})
}

func TestColumnRegistry_Precedence(t *testing.T) {
	t.Run("higher precedence overrides lower", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Register as label first
		err := registry.RegisterLabelColumns([]string{"app"})
		require.NoError(t, err)

		// Verify it's a label
		col, err := registry.ResolveColumn("app")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeLabel, col.Type)

		// Register same column as metadata
		err = registry.RegisterMetadataColumns([]string{"app"})
		require.NoError(t, err)

		// Should now be metadata, higher than label
		col, err = registry.ResolveColumn("app")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeMetadata, col.Type)

		// Register same column as parsed
		err = registry.RegisterParsedColumns([]string{"app"})
		require.NoError(t, err)

		// Should now be parsed (higher precedence)
		col, err = registry.ResolveColumn("app")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeParsed, col.Type)

	})

	t.Run("lower precedence cannot override higher", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Register as parsed first (higher precedence)
		err := registry.RegisterParsedColumns([]string{"field"})
		require.NoError(t, err)

		// Try to register as label (lower precedence)
		err = registry.RegisterLabelColumns([]string{"field"})
		require.NoError(t, err)

		// Should still be parsed
		col, err := registry.ResolveColumn("field")
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeParsed, col.Type)
	})

	t.Run("Complete precedence chain", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Register different columns at each precedence level
		err := registry.RegisterLabelColumns([]string{"col1", "col2", "col3", "col4"})
		require.NoError(t, err)

		// Upgrade col2 to metadata
		err = registry.RegisterMetadataColumns([]string{"col2", "col3", "col4"})
		require.NoError(t, err)

		// Upgrade col3 to parsed
		err = registry.RegisterParsedColumns([]string{"col3", "col4"})
		require.NoError(t, err)

		// Upgrade col4 to generated
		err = registry.RegisterGeneratedColumns([]string{"col4"})
		require.NoError(t, err)

		// Verify final types
		col, _ := registry.ResolveColumn("col1")
		require.Equal(t, types.ColumnTypeLabel, col.Type)

		col, _ = registry.ResolveColumn("col2")
		require.Equal(t, types.ColumnTypeMetadata, col.Type)

		col, _ = registry.ResolveColumn("col3")
		require.Equal(t, types.ColumnTypeParsed, col.Type)

		col, _ = registry.ResolveColumn("col4")
		require.Equal(t, types.ColumnTypeGenerated, col.Type)
	})

	t.Run("Builtin columns have lowest precedence", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Builtin columns start registered
		col, err := registry.ResolveColumn(types.ColumnNameBuiltinTimestamp)
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeBuiltin, col.Type)

		// Parsed can override builtin (parsed has higher precedence)
		err = registry.RegisterParsedColumns([]string{types.ColumnNameBuiltinTimestamp})
		require.NoError(t, err)

		// Should now be parsed
		col, err = registry.ResolveColumn(types.ColumnNameBuiltinTimestamp)
		require.NoError(t, err)
		require.Equal(t, types.ColumnTypeParsed, col.Type)
	})
}

func TestColumnRegistry_MultipleRegistrations(t *testing.T) {
	t.Run("Register multiple columns at once", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Register multiple labels
		err := registry.RegisterLabelColumns([]string{"app", "namespace", "pod", "container"})
		require.NoError(t, err)

		// Register multiple parsed
		err = registry.RegisterParsedColumns([]string{"level", "status", "duration", "bytes"})
		require.NoError(t, err)

		// Verify all are registered
		col, _ := registry.ResolveColumn("app")
		require.Equal(t, types.ColumnTypeLabel, col.Type)

		col, _ = registry.ResolveColumn("level")
		require.Equal(t, types.ColumnTypeParsed, col.Type)

		col, _ = registry.ResolveColumn("container")
		require.Equal(t, types.ColumnTypeLabel, col.Type)

		col, _ = registry.ResolveColumn("bytes")
		require.Equal(t, types.ColumnTypeParsed, col.Type)
	})

	t.Run("Empty registration does nothing", func(t *testing.T) {
		registry := NewColumnRegistry()

		// Register empty slices
		err := registry.RegisterLabelColumns([]string{})
		require.NoError(t, err)

		err = registry.RegisterParsedColumns(nil)
		require.NoError(t, err)

		// Only builtins should exist
		_, err = registry.ResolveColumn("anything")
		require.Error(t, err)
	})
}
