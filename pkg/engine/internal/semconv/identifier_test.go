package semconv

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestFullyQualifiedName(t *testing.T) {
	tc := []struct {
		name       string
		columnType types.ColumnType
		dataType   types.DataType
		expected   string
	}{
		// Resource scope
		{"service_name", types.ColumnTypeLabel, types.Loki.String, "utf8.label.service_name"},
		{"service.name", types.ColumnTypeLabel, types.Loki.String, "utf8.label.service.name"},
		// Record scope
		{"message", types.ColumnTypeBuiltin, types.Loki.String, "utf8.builtin.message"},
		{"timestamp", types.ColumnTypeBuiltin, types.Loki.Timestamp, "timestamp_ns.builtin.timestamp"},
		{"trace_id", types.ColumnTypeMetadata, types.Loki.String, "utf8.metadata.trace_id"},
		// Generated scope
		{"value", types.ColumnTypeGenerated, types.Loki.Float, "float64.generated.value"},
		{"caller", types.ColumnTypeParsed, types.Loki.String, "utf8.parsed.caller"},
		// Unscoped
		{"service.name", types.ColumnTypeAmbiguous, types.Loki.String, "utf8.ambiguous.service.name"},
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
		expected Scope
	}{
		{"utf8.builtin.message", Scope{Record, Builtin}},
		{"utf8.label.service_name", Scope{Resource, Attribute}},
		{"utf8.metadata.service_name", Scope{Record, Attribute}},
		{"utf8.parsed.level", Scope{Generated, Attribute}},
		{"utf8.generated.value", Scope{Generated, Builtin}},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFQN(tt.name)
			require.NoError(t, err)
			t.Log(got)
			require.Equal(t, tt.expected, got.Scope())
		})
	}
}
