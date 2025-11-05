package logs

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name     string
		columns  []*Column
		row      dataset.Row
		expected Record
		wantErr  bool
	}{
		{
			name: "all fields present",
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeTimestamp},
				{Type: ColumnTypeMetadata, Name: "app"},
				{Type: ColumnTypeMetadata, Name: "env"},
				{Type: ColumnTypeMessage},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(123),
					dataset.Int64Value(1234567890000000000),
					dataset.BinaryValue([]byte("test-app")),
					dataset.BinaryValue([]byte("prod")),
					dataset.BinaryValue([]byte("test message")),
				},
			},
			expected: Record{
				StreamID:  123,
				Timestamp: time.Unix(0, 1234567890000000000),
				Metadata:  labels.New(labels.Label{Name: "app", Value: "test-app"}, labels.Label{Name: "env", Value: "prod"}),
				Line:      []byte("test message"),
			},
		},
		{
			name: "nil values are skipped",
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeTimestamp},
				{Type: ColumnTypeMetadata, Name: "app"},
				{Type: ColumnTypeMessage},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(123),
					dataset.Int64Value(1234567890000000000),
					{},
					dataset.BinaryValue([]byte("test message")),
				},
			},
			expected: Record{
				StreamID:  123,
				Timestamp: time.Unix(0, 1234567890000000000),
				Metadata:  labels.EmptyLabels(),
				Line:      []byte("test message"),
			},
		},
		{
			name: "invalid stream_id type",
			columns: []*Column{
				{Type: ColumnTypeStreamID},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("invalid")),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid timestamp type",
			columns: []*Column{
				{Type: ColumnTypeTimestamp},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("invalid")),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid metadata type",
			columns: []*Column{
				{Type: ColumnTypeMetadata, Name: "app"},
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
			columns: []*Column{
				{Type: ColumnTypeMessage},
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
			record := Record{}
			err := DecodeRow(tt.columns, tt.row, &record, nil)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, record)
		})
	}
}
