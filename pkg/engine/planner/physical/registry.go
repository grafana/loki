package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ColumnRegistry tracks available columns and their types for query execution.
// It handles both static columns (builtin) and dynamically created columns (parsed).
type ColumnRegistry struct {
	columns map[string]types.ColumnRef
}

// NewColumnRegistry creates a new column registry with builtin columns pre-registered.
func NewColumnRegistry() *ColumnRegistry {
	registry := &ColumnRegistry{
		columns: make(map[string]types.ColumnRef),
	}

	// Register builtin columns
	for _, name := range []string{types.ColumnNameBuiltinTimestamp, types.ColumnNameBuiltinMessage} {
		registry.columns[name] = types.ColumnRef{
			Column: name,
			Type:   types.ColumnTypeBuiltin,
		}
	}

	return registry
}

// ResolveColumn looks up a column by name and returns its reference.
func (r *ColumnRegistry) ResolveColumn(name string) (types.ColumnRef, error) {
	col, exists := r.columns[name]
	if !exists {
		return types.ColumnRef{}, fmt.Errorf("column not found: %s", name)
	}
	return col, nil
}

// RegisterParsedColumns registers columns that were dynamically parsed from log content.
func (r *ColumnRegistry) RegisterParsedColumns(columnNames []string) error {
	for _, name := range columnNames {
		r.registerColumn(name, types.ColumnTypeParsed)
	}
	return nil
}

// RegisterLabelColumns registers columns from stream labels.
func (r *ColumnRegistry) RegisterLabelColumns(columnNames []string) error {
	for _, name := range columnNames {
		r.registerColumn(name, types.ColumnTypeLabel)
	}
	return nil
}

// RegisterMetadataColumns registers columns from log metadata.
func (r *ColumnRegistry) RegisterMetadataColumns(columnNames []string) error {
	for _, name := range columnNames {
		r.registerColumn(name, types.ColumnTypeMetadata)
	}
	return nil
}

// RegisterGeneratedColumns registers columns that are generated from expressions.
func (r *ColumnRegistry) RegisterGeneratedColumns(columnNames []string) error {
	for _, name := range columnNames {
		r.registerColumn(name, types.ColumnTypeGenerated)
	}
	return nil
}

// registerColumn is a helper that registers a column with precedence checking.
func (r *ColumnRegistry) registerColumn(name string, colType types.ColumnType) {
	if existing, exists := r.columns[name]; exists {
		// Only override if new type has higher precedence (lower number)
		if types.ColumnTypePrecedence(colType) < types.ColumnTypePrecedence(existing.Type) {
			r.columns[name] = types.ColumnRef{
				Column: name,
				Type:   colType,
			}
		}
	} else {
		// New column
		r.columns[name] = types.ColumnRef{
			Column: name,
			Type:   colType,
		}
	}
}
