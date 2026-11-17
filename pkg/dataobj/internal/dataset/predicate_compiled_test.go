package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestCompilePredicate(t *testing.T) {
	t.Run("evaluates each predicate shape against a row", func(t *testing.T) {
		colA := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
		colB := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
		lookup := map[Column]int{colA: 0, colB: 1}

		row := func(a, b Value) []Value { return []Value{a, b} }
		i := Int64Value
		nilV := Value{}

		tests := []struct {
			name   string
			pred   Predicate
			values []Value
			want   bool
		}{
			{"equal matches", EqualPredicate{Column: colA, Value: i(5)}, row(i(5), nilV), true},
			{"equal does not match", EqualPredicate{Column: colA, Value: i(5)}, row(i(6), nilV), false},
			{"equal matches nil column against nil value", EqualPredicate{Column: colA, Value: nilV}, row(nilV, nilV), true},
			{"equal does not match non-nil column against nil value", EqualPredicate{Column: colA, Value: nilV}, row(i(5), nilV), false},

			{"in matches a present value", InPredicate{Column: colA, Values: NewInt64ValueSetOf(1, 5)}, row(i(5), nilV), true},
			{"in does not match an absent value", InPredicate{Column: colA, Values: NewInt64ValueSetOf(1, 5)}, row(i(9), nilV), false},
			{"in does not match a nil value", InPredicate{Column: colA, Values: NewInt64ValueSetOf(1)}, row(nilV, nilV), false},
			{"in does not match a value of a different type", InPredicate{Column: colA, Values: NewInt64ValueSetOf(1)}, row(BinaryValue([]byte("x")), nilV), false},

			{"greater than matches a larger value", GreaterThanPredicate{Column: colA, Value: i(5)}, row(i(6), nilV), true},
			{"greater than does not match an equal value", GreaterThanPredicate{Column: colA, Value: i(5)}, row(i(5), nilV), false},
			{"greater than does not match a smaller value", GreaterThanPredicate{Column: colA, Value: i(5)}, row(i(4), nilV), false},

			{"less than matches a smaller value", LessThanPredicate{Column: colA, Value: i(5)}, row(i(4), nilV), true},
			{"less than does not match an equal value", LessThanPredicate{Column: colA, Value: i(5)}, row(i(5), nilV), false},
			{"less than does not match a larger value", LessThanPredicate{Column: colA, Value: i(5)}, row(i(6), nilV), false},

			{"greater than or equal reads its own column", GreaterThanOrEqualPredicate{Column: colB, Value: i(5)}, row(i(1), i(5)), true},
			{"greater than or equal ignores the other column", GreaterThanOrEqualPredicate{Column: colB, Value: i(5)}, row(i(9), i(1)), false},
			{"greater than or equal matches a nil column against an empty binary value", GreaterThanOrEqualPredicate{Column: colA, Value: BinaryValue(nil)}, row(nilV, nilV), true},
			{"greater than or equal does not match a nil column against a binary value", GreaterThanOrEqualPredicate{Column: colA, Value: BinaryValue([]byte("a"))}, row(nilV, nilV), false},
			{"greater than or equal matches a larger value", GreaterThanOrEqualPredicate{Column: colA, Value: i(5)}, row(i(6), nilV), true},
			{"greater than or equal matches an equal value", GreaterThanOrEqualPredicate{Column: colA, Value: i(5)}, row(i(5), nilV), true},
			{"greater than or equal does not match a smaller value", GreaterThanOrEqualPredicate{Column: colA, Value: i(5)}, row(i(4), nilV), false},
			{"greater than or equal does not match a nil column against a value", GreaterThanOrEqualPredicate{Column: colA, Value: i(5)}, row(nilV, nilV), false},
			{"greater than or equal matches a nil column against a nil value", GreaterThanOrEqualPredicate{Column: colA, Value: nilV}, row(nilV, nilV), true},

			{"less than or equal reads its own column", LessThanOrEqualPredicate{Column: colB, Value: i(5)}, row(i(9), i(5)), true},
			{"less than or equal ignores the other column", LessThanOrEqualPredicate{Column: colB, Value: i(5)}, row(i(1), i(9)), false},
			{"less than or equal matches a nil column against an empty binary value", LessThanOrEqualPredicate{Column: colA, Value: BinaryValue(nil)}, row(nilV, nilV), true},
			{"less than or equal matches a nil column against a binary value", LessThanOrEqualPredicate{Column: colA, Value: BinaryValue([]byte("a"))}, row(nilV, nilV), true},
			{"less than or equal matches a smaller value", LessThanOrEqualPredicate{Column: colA, Value: i(5)}, row(i(4), nilV), true},
			{"less than or equal matches an equal value", LessThanOrEqualPredicate{Column: colA, Value: i(5)}, row(i(5), nilV), true},
			{"less than or equal does not match a larger value", LessThanOrEqualPredicate{Column: colA, Value: i(5)}, row(i(6), nilV), false},
			{"less than or equal matches a nil column against a value", LessThanOrEqualPredicate{Column: colA, Value: i(5)}, row(nilV, nilV), true},
			{"less than or equal matches a nil column against a nil value", LessThanOrEqualPredicate{Column: colA, Value: nilV}, row(nilV, nilV), true},

			{"and matches when both sides match", AndPredicate{Left: GreaterThanPredicate{Column: colA, Value: i(0)}, Right: LessThanPredicate{Column: colB, Value: i(100)}}, row(i(5), i(50)), true},
			{"and does not match when the right side fails", AndPredicate{Left: GreaterThanPredicate{Column: colA, Value: i(0)}, Right: LessThanPredicate{Column: colB, Value: i(100)}}, row(i(5), i(150)), false},
			{"and does not match when the left side fails", AndPredicate{Left: GreaterThanPredicate{Column: colA, Value: i(0)}, Right: LessThanPredicate{Column: colB, Value: i(100)}}, row(i(-1), i(50)), false},

			{"or matches when only the left side matches", OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colB, Value: i(99)}}, row(i(5), i(1)), true},
			{"or matches when only the right side matches", OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colB, Value: i(99)}}, row(i(1), i(99)), true},
			{"or does not match when neither side matches", OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colB, Value: i(99)}}, row(i(1), i(1)), false},

			{"not inverts a matching inner predicate", NotPredicate{Inner: EqualPredicate{Column: colA, Value: i(5)}}, row(i(5), nilV), false},
			{"not inverts a non-matching inner predicate", NotPredicate{Inner: EqualPredicate{Column: colA, Value: i(5)}}, row(i(6), nilV), true},

			{"true always matches", TruePredicate{}, row(i(0), nilV), true},
			{"false never matches", FalsePredicate{}, row(i(0), nilV), false},
			{"nil always matches", nil, row(i(0), nilV), true},

			{"nested and/or/not matches when the or branch matches and the not inner fails", AndPredicate{
				Left:  OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colA, Value: i(6)}},
				Right: NotPredicate{Inner: GreaterThanPredicate{Column: colB, Value: i(100)}},
			}, row(i(6), i(50)), true},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				row := Row{Values: tc.values}

				cp, err := compilePredicate(tc.pred, lookup)
				require.NoError(t, err)
				require.Equal(t, tc.want, cp.eval(row))
			})
		}
	})

	t.Run("passes FuncPredicate its resolved column and value", func(t *testing.T) {
		colA := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
		lookup := map[Column]int{colA: 0}

		var gotColumn Column
		var gotValue Value
		cp, err := compilePredicate(FuncPredicate{
			Column: colA,
			Keep: func(column Column, value Value) bool {
				gotColumn = column
				gotValue = value
				return value.Int64() > 3
			},
		}, lookup)
		require.NoError(t, err)

		require.True(t, cp.eval(Row{Values: []Value{Int64Value(5)}}))
		require.Same(t, colA.(*MemColumn), gotColumn.(*MemColumn))
		require.Equal(t, int64(5), gotValue.Int64())

		require.False(t, cp.eval(Row{Values: []Value{Int64Value(2)}}))
	})

	t.Run("returns an error when a predicate references a column outside the lookup", func(t *testing.T) {
		known := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
		missing := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
		lookup := map[Column]int{known: 0}

		tests := []struct {
			name string
			pred Predicate
		}{
			{"the missing column is an equal leaf", EqualPredicate{Column: missing, Value: Int64Value(1)}},
			{"the missing column is an in leaf", InPredicate{Column: missing, Values: NewInt64ValueSetOf(1)}},
			{"the missing column is a greater than or equal leaf", GreaterThanOrEqualPredicate{Column: missing, Value: Int64Value(1)}},
			{"the missing column is a less than or equal leaf", LessThanOrEqualPredicate{Column: missing, Value: Int64Value(1)}},
			{"the missing column is a func leaf", FuncPredicate{Column: missing, Keep: func(Column, Value) bool { return true }}},
			{"the missing column is the left side of an and", AndPredicate{Left: EqualPredicate{Column: missing, Value: Int64Value(1)}, Right: TruePredicate{}}},
			{"the missing column is the right side of an or", OrPredicate{Left: TruePredicate{}, Right: LessThanPredicate{Column: missing, Value: Int64Value(1)}}},
			{"the missing column is inside a not", NotPredicate{Inner: GreaterThanPredicate{Column: missing, Value: Int64Value(1)}}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				_, err := compilePredicate(tc.pred, lookup)
				require.ErrorContains(t, err, "not found in RowReader columns")
			})
		}
	})

	t.Run("panics on an unsupported predicate type", func(t *testing.T) {
		require.Panics(t, func() {
			_, _ = compilePredicate(unknownPredicate{}, map[Column]int{})
		})
	})
}

// testColumn returns a minimal Column for predicate tests. Predicate evaluation
// only needs the physical type (for InPredicate) and the column's identity as a
// lookup key; it never reads pages.
func testColumn(physical datasetmd.PhysicalType) Column {
	return &MemColumn{Desc: ColumnDesc{Type: ColumnType{Physical: physical}}}
}

type unknownPredicate struct{}

func (unknownPredicate) isPredicate() {}
