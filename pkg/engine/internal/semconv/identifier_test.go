package semconv

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestFullyQualifiedName(t *testing.T) {
	tc := []struct {
		name       string
		columnType physicalpb.ColumnType
		dataType   types.DataType
		expected   string
	}{
		// Resource scope
		{"service_name", physicalpb.COLUMN_TYPE_LABEL, types.Loki.String, "utf8.COLUMN_TYPE_LABEL.service_name"},
		{"service.name", physicalpb.COLUMN_TYPE_LABEL, types.Loki.String, "utf8.COLUMN_TYPE_LABEL.service.name"},
		// Record scope
		{"message", physicalpb.COLUMN_TYPE_BUILTIN, types.Loki.String, "utf8.COLUMN_TYPE_BUILTIN.message"},
		{"timestamp", physicalpb.COLUMN_TYPE_BUILTIN, types.Loki.Timestamp, "timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp"},
		{"trace_id", physicalpb.COLUMN_TYPE_METADATA, types.Loki.String, "utf8.COLUMN_TYPE_METADATA.trace_id"},
		// Generated scope
		{"value", physicalpb.COLUMN_TYPE_GENERATED, types.Loki.Float, "float64.COLUMN_TYPE_GENERATED.value"},
		{"caller", physicalpb.COLUMN_TYPE_PARSED, types.Loki.String, "utf8.COLUMN_TYPE_PARSED.caller"},
		// Unscoped
		{"service.name", physicalpb.COLUMN_TYPE_AMBIGUOUS, types.Loki.String, "utf8.COLUMN_TYPE_AMBIGUOUS.service.name"},
	}

	for _, tt := range tc {
		t.Run(fmt.Sprintf("name=%s/type=%s/data=%s", tt.name, tt.columnType, tt.dataType), func(t *testing.T) {
			got := FQN(tt.name, tt.columnType, tt.dataType)
			require.Equal(t, tt.expected, got)
			t.Log(got)
			_, err := ParseFQN(got)
			require.NoError(t, err)
		})
	}
}

func TestParsingInvalidColumnNames(t *testing.T) {
	tc := []struct {
		name          string
		expectedError string
	}{
		{"", "empty identifier"},
		{"utf8", "missing data type separator"},
		{"utf8.", "missing column type"},
		{"decimal128.invalid:.", "invalid data type: decimal128"},
		{"utf8.unscoped?trace_id", "missing column type"},
		{"utf8.invalid:.", "missing column name"},
	}

	for _, tt := range tc {
		t.Run(fmt.Sprintf("name=%s", tt.name), func(t *testing.T) {
			_, err := ParseFQN(tt.name)
			require.ErrorContains(t, err, tt.expectedError)
		})
	}
}

func TestScope(t *testing.T) {
	tc := []struct {
		name     string
		expected SemanticType
	}{
		{"utf8.COLUMN_TYPE_BUILTIN.message", SemanticType{Record, Builtin}},
		{"utf8.COLUMN_TYPE_LABEL.service_name", SemanticType{Resource, Attribute}},
		{"utf8.COLUMN_TYPE_METADATA.service_name", SemanticType{Record, Attribute}},
		{"utf8.COLUMN_TYPE_PARSED.level", SemanticType{Generated, Attribute}},
		{"utf8.COLUMN_TYPE_GENERATED.value", SemanticType{Generated, Builtin}},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFQN(tt.name)
			require.NoError(t, err)
			t.Log(got)
			require.Equal(t, tt.expected, got.SemType())
		})
	}
}
