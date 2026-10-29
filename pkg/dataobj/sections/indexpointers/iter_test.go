package indexpointers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func TestDecodeRow(t *testing.T) {
	tests := []struct {
		name     string
		columns  []*Column
		row      dataset.Row
		expected IndexPointer
		wantErr  bool
	}{
		{
			name: "all fields present",
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
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
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "missing min_timestamp value",
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(0),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "missing max_timestamp value",
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(0),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid path type",
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
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
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.BinaryValue([]byte("invalid")),
					dataset.Int64Value(1234567890000000000),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid max_timestamp type",
			columns: []*Column{
				{Name: "path", Type: ColumnTypePath},
				{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
				{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.BinaryValue([]byte("invalid")),
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
