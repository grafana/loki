package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

type testColumn struct {
	name string
	Column
}

func (c *testColumn) ColumnInfo() *ColumnInfo {
	return &ColumnInfo{Name: c.name}
}

func TestGenerateBoundsForColumn(t *testing.T) {
	col := &testColumn{name: "foo"}
	otherCol := &testColumn{name: "bar"}

	tests := []struct {
		name      string
		predicate Predicate
		sortDir   datasetmd.SortDirection
		testVals  []Value
		expects   []bool
	}{
		{
			name:      "EqualPredicate ascending",
			predicate: EqualPredicate{Column: col, Value: Int64Value(5)},
			sortDir:   datasetmd.SORT_DIRECTION_ASCENDING,
			testVals:  []Value{Int64Value(5), Int64Value(4), Int64Value(6)},
			expects:   []bool{true, true, false},
		},
		{
			name:      "EqualPredicate descending",
			predicate: EqualPredicate{Column: col, Value: Int64Value(5)},
			sortDir:   datasetmd.SORT_DIRECTION_DESCENDING,
			testVals:  []Value{Int64Value(5), Int64Value(6), Int64Value(4)},
			expects:   []bool{true, true, false},
		},
		{
			name:      "LessThanPredicate ascending",
			predicate: LessThanPredicate{Column: col, Value: Int64Value(5)},
			sortDir:   datasetmd.SORT_DIRECTION_ASCENDING,
			testVals:  []Value{Int64Value(4), Int64Value(5)},
			expects:   []bool{true, false},
		},
		{
			name:      "LessThanPredicate descending (should be nil)",
			predicate: LessThanPredicate{Column: col, Value: Int64Value(5)},
			sortDir:   datasetmd.SORT_DIRECTION_DESCENDING,
			testVals:  []Value{Int64Value(4)},
			expects:   nil, // bound should be nil
		},
		{
			name:      "GreaterThanPredicate descending",
			predicate: GreaterThanPredicate{Column: col, Value: Int64Value(5)},
			sortDir:   datasetmd.SORT_DIRECTION_DESCENDING,
			testVals:  []Value{Int64Value(6), Int64Value(5)},
			expects:   []bool{true, false},
		},
		{
			name:      "GreaterThanPredicate ascending (should be nil)",
			predicate: GreaterThanPredicate{Column: col, Value: Int64Value(5)},
			sortDir:   datasetmd.SORT_DIRECTION_ASCENDING,
			testVals:  []Value{Int64Value(6)},
			expects:   nil, // bound should be nil
		},
		{
			name:      "InPredicate ascending",
			predicate: InPredicate{Column: col, Values: []Value{Int64Value(2), Int64Value(5), Int64Value(3)}},
			sortDir:   datasetmd.SORT_DIRECTION_ASCENDING,
			testVals:  []Value{Int64Value(5), Int64Value(4), Int64Value(6)},
			expects:   []bool{true, true, false},
		},
		{
			name:      "InPredicate descending",
			predicate: InPredicate{Column: col, Values: []Value{Int64Value(2), Int64Value(5), Int64Value(3)}},
			sortDir:   datasetmd.SORT_DIRECTION_DESCENDING,
			testVals:  []Value{Int64Value(2), Int64Value(3), Int64Value(1)},
			expects:   []bool{true, true, false},
		},
		{
			name:      "AndPredicate (only one side returns a bound)",
			predicate: AndPredicate{Left: EqualPredicate{Column: col, Value: Int64Value(5)}, Right: EqualPredicate{Column: otherCol, Value: Int64Value(1)}},
			sortDir:   datasetmd.SORT_DIRECTION_ASCENDING,
			testVals:  []Value{Int64Value(5), Int64Value(4), Int64Value(6)},
			expects:   []bool{true, true, false},
		},
		{
			name:      "OrPredicate (only one side returns a bound)",
			predicate: OrPredicate{Left: EqualPredicate{Column: col, Value: Int64Value(5)}, Right: EqualPredicate{Column: otherCol, Value: Int64Value(1)}},
			sortDir:   datasetmd.SORT_DIRECTION_ASCENDING,
			testVals:  []Value{Int64Value(5), Int64Value(4), Int64Value(6)},
			expects:   []bool{true, true, false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bound := GenerateBoundsForColumn(tc.predicate, col, tc.sortDir)
			if tc.expects == nil {
				if bound != nil {
					t.Errorf("expected nil bound, got non-nil")
				}
				return
			}
			if bound == nil {
				t.Fatalf("expected non-nil bound, got nil")
			}

			require.Equal(t, col, bound.column)
			for i, val := range tc.testVals {
				if got := bound.Checker(col).Check(val); got != tc.expects[i] {
					t.Errorf("bound(%v) = %v, want %v", val, got, tc.expects[i])
				}
			}
		})
	}
}
