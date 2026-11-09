package indexpointers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestDecodeRow(t *testing.T) {
	optionalColumns := []*Column{
		{Name: "path", Type: ColumnTypePath},
		{Name: "min_timestamp", Type: ColumnTypeMinTimestamp},
		{Name: "max_timestamp", Type: ColumnTypeMaxTimestamp},
		{Name: "run_id", Type: ColumnTypeRunID},
		{Name: "min_posting_key", Type: ColumnTypeMinPostingKey},
		{Name: "max_posting_key", Type: ColumnTypeMaxPostingKey},
		{Name: "shard_buckets", Type: ColumnTypeShardBuckets},
	}
	lowKey := postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api"}
	highKey := postings.Key{Kind: postings.KindLabel, ColumnName: "env", LabelValue: "prod"}
	keyRange, err := postings.NewKeyRange(lowKey, highKey)
	require.NoError(t, err)
	minEncoded, maxEncoded := keyRange.Encode()
	shardLayout := streams.MustShardLayoutFromBuckets(64)

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
		{
			name:    "decodes the run ID, posting keys and shard layout when all are present",
			columns: optionalColumns,
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(7),
					dataset.BinaryValue(minEncoded),
					dataset.BinaryValue(maxEncoded),
					dataset.Int64Value(64),
				},
			},
			expected: IndexPointer{
				Path:        "test-path",
				StartTs:     time.Unix(0, 1234567890000000000),
				EndTs:       time.Unix(0, 1234567890000000000),
				RunID:       7,
				PostingKeys: &keyRange,
				ShardLayout: &shardLayout,
			},
		},
		{
			name:    "leaves the optional fields unset when their values are null",
			columns: optionalColumns,
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
					{},
					{},
					{},
					{},
				},
			},
			expected: IndexPointer{
				Path:    "test-path",
				StartTs: time.Unix(0, 1234567890000000000),
				EndTs:   time.Unix(0, 1234567890000000000),
			},
		},
		{
			name:    "returns an error when only one posting key bound is present",
			columns: optionalColumns,
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
					{},
					dataset.BinaryValue(minEncoded),
					{},
					{},
				},
			},
			wantErr: true,
		},
		{
			name:    "returns an error when the shard bucket count is not a power of two",
			columns: optionalColumns,
			row: dataset.Row{
				Values: []dataset.Value{
					dataset.BinaryValue([]byte("test-path")),
					dataset.Int64Value(1234567890000000000),
					dataset.Int64Value(1234567890000000000),
					{},
					{},
					{},
					dataset.Int64Value(3),
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
