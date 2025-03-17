package logs

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name     string
		columns  []*logsmd.ColumnDesc
		row      dataset.Row
		expected Record
		wantErr  bool
	}{
		{
			name: "all fields present",
			columns: []*logsmd.ColumnDesc{
				{Type: logsmd.COLUMN_TYPE_STREAM_ID},
				{Type: logsmd.COLUMN_TYPE_TIMESTAMP},
				{Type: logsmd.COLUMN_TYPE_METADATA, Info: &datasetmd.ColumnInfo{Name: "app"}},
				{Type: logsmd.COLUMN_TYPE_METADATA, Info: &datasetmd.ColumnInfo{Name: "env"}},
				{Type: logsmd.COLUMN_TYPE_MESSAGE},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(123),
					dataset.Int64Value(1234567890000000000),
					dataset.StringValue("test-app"),
					dataset.StringValue("prod"),
					dataset.ByteArrayValue([]byte("test message")),
				},
			},
			expected: Record{
				StreamID:  123,
				Timestamp: time.Date(2009, 2, 13, 23, 31, 30, 0, time.UTC),
				Metadata:  labels.FromStrings("app", "test-app", "env", "prod"),
				Line:      []byte("test message"),
			},
		},
		{
			name: "nil values are skipped",
			columns: []*logsmd.ColumnDesc{
				{Type: logsmd.COLUMN_TYPE_STREAM_ID},
				{Type: logsmd.COLUMN_TYPE_TIMESTAMP},
				{Type: logsmd.COLUMN_TYPE_METADATA, Info: &datasetmd.ColumnInfo{Name: "app"}},
				{Type: logsmd.COLUMN_TYPE_MESSAGE},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(123),
					dataset.Int64Value(1234567890000000000),
					{},
					dataset.ByteArrayValue([]byte("test message")),
				},
			},
			expected: Record{
				StreamID:  123,
				Timestamp: time.Date(2009, 2, 13, 23, 31, 30, 0, time.UTC),
				Metadata:  labels.FromStrings(),
				Line:      []byte("test message"),
			},
		},
		{
			name: "invalid stream_id type",
			columns: []*logsmd.ColumnDesc{
				{Type: logsmd.COLUMN_TYPE_STREAM_ID},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.StringValue("invalid"),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid timestamp type",
			columns: []*logsmd.ColumnDesc{
				{Type: logsmd.COLUMN_TYPE_TIMESTAMP},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.StringValue("invalid"),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid metadata type",
			columns: []*logsmd.ColumnDesc{
				{Type: logsmd.COLUMN_TYPE_METADATA, Info: &datasetmd.ColumnInfo{Name: "app"}},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(123),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid message type",
			columns: []*logsmd.ColumnDesc{
				{Type: logsmd.COLUMN_TYPE_MESSAGE},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(123),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := Decode(tt.columns, tt.row)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, record)
		})
	}
}

func TestMetadataColumns(t *testing.T) {
	columns := []*logsmd.ColumnDesc{
		{Type: logsmd.COLUMN_TYPE_STREAM_ID},
		{Type: logsmd.COLUMN_TYPE_TIMESTAMP},
		{Type: logsmd.COLUMN_TYPE_METADATA, Info: &datasetmd.ColumnInfo{Name: "app"}},
		{Type: logsmd.COLUMN_TYPE_METADATA, Info: &datasetmd.ColumnInfo{Name: "env"}},
		{Type: logsmd.COLUMN_TYPE_MESSAGE},
	}

	count := metadataColumns(columns)
	require.Equal(t, 2, count)
}
