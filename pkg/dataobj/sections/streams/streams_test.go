package streams

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSection_HasColumn(t *testing.T) {
	tests := map[string]struct {
		columns []*Column
		lookup  ColumnType
		want    bool
	}{
		"a section with no columns at all holds nothing": {
			columns: nil,
			lookup:  ColumnTypeShardBucket,
			want:    false,
		},
		"a section written before the column type existed does not hold it": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeMinTimestamp},
				{Type: ColumnTypeLabel, Name: "app"},
			},
			lookup: ColumnTypeShardBucket,
			want:   false,
		},
		"a section holding it last is recognised": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeShardBucket},
			},
			lookup: ColumnTypeShardBucket,
			want:   true,
		},
		"a section holding it first is recognised too": {
			columns: []*Column{
				{Type: ColumnTypeShardBucket},
				{Type: ColumnTypeStreamID},
			},
			lookup: ColumnTypeShardBucket,
			want:   true,
		},
		"a label column is found by its type, whatever its name": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeLabel, Name: "app"},
			},
			lookup: ColumnTypeLabel,
			want:   true,
		},
		"a type no column carries is not found": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeShardBucket},
			},
			lookup: ColumnTypeMaxTimestamp,
			want:   false,
		},
		"the invalid type is not found in a section of valid columns": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeShardBucket},
			},
			lookup: ColumnTypeInvalid,
			want:   false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			section := &Section{columns: test.columns}
			require.Equal(t, test.want, section.HasColumn(test.lookup))
		})
	}
}
