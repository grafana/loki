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
				Timestamp: time.Unix(0, 1234567890000000000).UTC(),
				Metadata:  labels.New(labels.Label{Name: "app", Value: "test-app"}, labels.Label{Name: "env", Value: "prod"}),
				Line:      []byte("test message"),
			},
		},
		{
			name: "nil metadata columns are skipped",
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
				Timestamp: time.Unix(0, 1234567890000000000).UTC(),
				Metadata:  labels.EmptyLabels(),
				Line:      []byte("test message"),
			},
		},
		{
			name: "empty message clears the previous Record.Line",
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
					dataset.BinaryValue(nil),
				},
			},
			expected: Record{
				StreamID:  123,
				Timestamp: time.Unix(0, 1234567890000000000).UTC(),
				Metadata:  labels.EmptyLabels(),
				Line:      []byte(""),
			},
		},
		{
			// A physical zero is not an absent cell: a timestamp at the Unix epoch are both valid values.
			name: "zero stream_id and epoch timestamp are decoded",
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeTimestamp},
				{Type: ColumnTypeMessage},
			},
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.Int64Value(1),
					dataset.Int64Value(0),
					dataset.BinaryValue([]byte("test message")),
				},
			},
			expected: Record{
				StreamID:  1,
				Timestamp: time.Unix(0, 0).UTC(),
				Metadata:  labels.EmptyLabels(),
				Line:      []byte("test message"),
			},
		},
		{
			// An empty metadata value is a label whose value is empty, which the chunk path
			// keeps too. Only a nil cell means the key is absent.
			name: "empty metadata value is kept as a label",
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
					dataset.BinaryValue([]byte("")),
					dataset.BinaryValue([]byte("prod")),
					dataset.BinaryValue([]byte("test message")),
				},
			},
			expected: Record{
				StreamID:  123,
				Timestamp: time.Unix(0, 1234567890000000000).UTC(),
				Metadata:  labels.New(labels.Label{Name: "app", Value: ""}, labels.Label{Name: "env", Value: "prod"}),
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

	// reuse record to capture potential issues with stale data from previous rows
	record := Record{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
