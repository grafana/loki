package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	records = []arrowtest.Rows{
		{
			{"ts": int64(10), "table": "A", "line": "line A"},
			{"ts": int64(15), "table": "A", "line": "line B"},
			{"ts": int64(5), "table": "A", "line": "line C"},
			{"ts": int64(20), "table": "A", "line": "line D"},
		},
		{
			{"ts": int64(1), "table": "A", "line": "line A"},
			{"ts": int64(50), "table": "A", "line": "line B"},
		},
		{
			// This record contains an additional column not found in the other
			// records; this tests to make sure topkBatch properly merges schemas.
			{"ts": int64(100), "table": "B", "app": "loki", "line": "line A"},
			{"ts": int64(75), "table": "B", "app": "loki", "line": "line B"},
			{"ts": int64(25), "table": "B", "app": "loki", "line": "line C"},
		},
		{
			{"ts": int64(13), "table": "C", "line": "line A"},
			{"ts": int64(15), "table": "C", "line": "line B"},
			{"ts": int64(17), "table": "C", "line": "line C"},
			{"ts": int64(19), "table": "C", "line": "line D"},
		},
		{
			// This record contains a nil sort key to test the behaviour of
			// NullsFirst.
			{"table": "D", "line": "line A"},
		},
	}
)

func Test_topkBatch(t *testing.T) {
	tt := []struct {
		name       string
		ascending  bool
		nullsFirst bool
		expect     arrowtest.Rows
	}{
		{
			name: "Default",
			expect: arrowtest.Rows{
				{"ts": nil, "table": "D", "app": nil, "line": "line A"},
				{"ts": int64(100), "table": "B", "app": "loki", "line": "line A"},
				{"ts": int64(75), "table": "B", "app": "loki", "line": "line B"},
				{"ts": int64(50), "table": "A", "app": nil, "line": "line B"},
				{"ts": int64(25), "table": "B", "app": "loki", "line": "line C"},
			},
		},

		{
			name:      "Ascending",
			ascending: true,
			expect: arrowtest.Rows{
				// No app column because none of the rows used have it.
				{"ts": int64(1), "table": "A", "line": "line A"},
				{"ts": int64(5), "table": "A", "line": "line C"},
				{"ts": int64(10), "table": "A", "line": "line A"},
				{"ts": int64(13), "table": "C", "line": "line A"},
				{"ts": int64(15), "table": "C", "line": "line B"},
			},
		},

		{
			name:       "NullsFirst",
			nullsFirst: true,
			expect: arrowtest.Rows{
				{"ts": int64(100), "table": "B", "app": "loki", "line": "line A"},
				{"ts": int64(75), "table": "B", "app": "loki", "line": "line B"},
				{"ts": int64(50), "table": "A", "app": nil, "line": "line B"},
				{"ts": int64(25), "table": "B", "app": "loki", "line": "line C"},
				{"ts": int64(20), "table": "A", "app": nil, "line": "line D"},
			},
		},

		{
			name:       "NullsFirst Ascending",
			nullsFirst: true,
			ascending:  true,
			expect: arrowtest.Rows{
				// No app column because none of the rows used have it.
				{"ts": nil, "table": "D", "line": "line A"},
				{"ts": int64(1), "table": "A", "line": "line A"},
				{"ts": int64(5), "table": "A", "line": "line C"},
				{"ts": int64(10), "table": "A", "line": "line A"},
				{"ts": int64(13), "table": "C", "line": "line A"},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			b := topkBatch{
				Fields:     []arrow.Field{{Name: "ts", Type: arrow.PrimitiveTypes.Int64, Nullable: true}},
				K:          5,
				MaxUnused:  5,
				Ascending:  tc.ascending,
				NullsFirst: tc.nullsFirst,
			}
			defer b.Reset()

			for _, rows := range records {
				rec := rows.Record(memory.DefaultAllocator, rows.Schema())
				b.Put(rec)
			}

			output := b.Compact()

			actual, err := arrowtest.RecordRows(output)
			require.NoError(t, err)
			require.Len(t, actual, len(tc.expect))
			require.ElementsMatch(t, tc.expect, actual, "rows should match (order may differ)")
		})
	}
}

// Test_topkBatch_MaxUnused ensures that compaction is automatically triggered
// upon appending a record when the number of unused rows gets too high.
func Test_topkBatch_MaxUnused(t *testing.T) {
	// TopK 2, descending order.
	b := topkBatch{
		Fields:    []arrow.Field{{Name: "ts", Type: arrow.PrimitiveTypes.Int64, Nullable: true}},
		K:         2,
		MaxUnused: 5,
	}
	defer b.Reset()

	// seq tracks a sequence of Put operations into b. Each time we Put a record,
	// the number of unused records should account for any record that still has
	// rows contributing to the topk.
	//
	// Records that get fully pushed out of the topk do not contribute to this
	// count.
	seq := []struct {
		rows         arrowtest.Rows
		expectUsed   int
		expectUnused int
	}{
		{
			rows: arrowtest.Rows{
				{"ts": int64(100)},
				{"ts": int64(10)},
				{"ts": int64(9)},
				{"ts": int64(8)},
				{"ts": int64(7)},
			},
			expectUsed:   2,
			expectUnused: 3, // 3 unused from this initial record.
		},

		{
			rows: arrowtest.Rows{
				{"ts": int64(50)},
				{"ts": int64(6)},
			},
			expectUsed:   2,
			expectUnused: 5, // 4 from previous record, 1 from this record.
		},

		{
			rows: arrowtest.Rows{
				{"ts": int64(75)},
				{"ts": int64(5)},
				{"ts": int64(4)},
				{"ts": int64(3)},
			},
			expectUsed:   2,
			expectUnused: 0, // Expect compaction here, since 5/4/3 would be unused put us above the limit.
		},

		{
			rows: arrowtest.Rows{
				{"ts": int64(90)},
				{"ts": int64(2)},
				{"ts": int64(1)},
				{"ts": int64(0)},
			},
			expectUsed:   2,
			expectUnused: 4, // 3 from this record and 1 from the previous compacted record
		},
	}

	for i, tc := range seq {
		rec := tc.rows.Record(memory.DefaultAllocator, tc.rows.Schema())
		b.Put(rec)

		used, unused := b.Size()
		assert.Equal(t, tc.expectUsed, used, "unexpected number of used rows after record %d", i)
		assert.Equal(t, tc.expectUnused, unused, "unexpected number of unused rows after record %d", i)
	}
}

func Test_iterContiguousRanges(t *testing.T) {
	tt := []struct {
		name   string
		rows   []int
		ranges []struct{ start, end int }
	}{
		{
			name: "empty slice",
			rows: []int{},
		},
		{
			name:   "single row",
			rows:   []int{5},
			ranges: []struct{ start, end int }{{5, 6}},
		},
		{
			name:   "single contiguous range",
			rows:   []int{1, 2, 3},
			ranges: []struct{ start, end int }{{1, 4}},
		},
		{
			name:   "multiple contiguous ranges",
			rows:   []int{1, 2, 3, 5, 6, 7},
			ranges: []struct{ start, end int }{{1, 4}, {5, 8}},
		},
		{
			name:   "non-contiguous single rows",
			rows:   []int{1, 3, 5},
			ranges: []struct{ start, end int }{{1, 2}, {3, 4}, {5, 6}},
		},
		{
			name:   "all rows",
			rows:   []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			ranges: []struct{ start, end int }{{0, 10}},
		},
		{
			name:   "first and last row",
			rows:   []int{0, 9},
			ranges: []struct{ start, end int }{{0, 1}, {9, 10}},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var got []struct{ start, end int }
			iterContiguousRanges(tc.rows, func(start, end int) bool {
				got = append(got, struct{ start, end int }{start, end})
				return true
			})
			require.Equal(t, tc.ranges, got, "ranges should match")
		})
	}
}
