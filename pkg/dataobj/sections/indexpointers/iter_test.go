package indexpointers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/indexpointersmd"
)

func TestDecodeRow(t *testing.T) {
	tests := []struct {
		name     string
		columns  []*indexpointersmd.ColumnDesc
		row      dataset.Row
		expected IndexPointer
		wantErr  bool
	}{
		{
			name: "all fields present",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.ByteArrayValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
				},
			},
			expected: IndexPointer{
				Path:    "test-path",
				StartTs: time.Unix(0, 1234567890000000000),
				EndTs:   time.Unix(0, 1234567890000000000),
			},
		},
		{
			name: "missing path value",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.ByteArrayValue([]byte("")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "missing min_timestamp value",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.ByteArrayValue([]byte("test-path")),
					dataset.Int64Value(0),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "missing max_timestamp value",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.ByteArrayValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(0),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid path type",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid min_timestamp type",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.ByteArrayValue([]byte("test-path")),
					dataset.ByteArrayValue([]byte("invalid")),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid max_timestamp type",
			columns: []*indexpointersmd.ColumnDesc{
				{Type: indexpointersmd.COLUMN_TYPE_PATH, Info: &datasetmd.ColumnInfo{Name: "path"}},
				{Type: indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "min_timestamp"}},
				{Type: indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, Info: &datasetmd.ColumnInfo{Name: "max_timestamp"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.ByteArrayValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.ByteArrayValue([]byte("invalid")),
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexPointer := IndexPointer{}
			err := decodeRow(tt.columns, tt.row, &indexPointer, nil)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, indexPointer)
			}
		})
	}
}
