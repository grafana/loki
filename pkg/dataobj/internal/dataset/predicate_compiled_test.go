package dataset

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestCompilePredicate_Eval(t *testing.T) {
	// Row layout: index 0 = colA (int64), 1 = colB (int64), 2 = colBin (binary).
	colA := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
	colB := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
	colBin := testColumn(datasetmd.PHYSICAL_TYPE_BINARY)
	lookup := map[Column]int{colA: 0, colB: 1, colBin: 2}

	row := func(a, b Value, bin Value) []Value { return []Value{a, b, bin} }
	i := Int64Value
	nilV := Value{}

	tests := []struct {
		name   string
		pred   Predicate
		values []Value
		want   bool
	}{
		{"equal_match", EqualPredicate{Column: colA, Value: i(5)}, row(i(5), nilV, nilV), true},
		{"equal_no_match", EqualPredicate{Column: colA, Value: i(5)}, row(i(6), nilV, nilV), false},
		{"equal_nil_row_and_value", EqualPredicate{Column: colA, Value: nilV}, row(nilV, nilV, nilV), true},
		{"equal_nil_value_only", EqualPredicate{Column: colA, Value: nilV}, row(i(5), nilV, nilV), false},

		{"in_present", InPredicate{Column: colA, Values: NewInt64ValueSet([]Value{i(1), i(5)})}, row(i(5), nilV, nilV), true},
		{"in_absent", InPredicate{Column: colA, Values: NewInt64ValueSet([]Value{i(1), i(5)})}, row(i(9), nilV, nilV), false},
		{"in_nil_value", InPredicate{Column: colA, Values: NewInt64ValueSet([]Value{i(1)})}, row(nilV, nilV, nilV), false},
		{"in_type_mismatch", InPredicate{Column: colA, Values: NewInt64ValueSet([]Value{i(1)})}, row(BinaryValue([]byte("x")), nilV, nilV), false},

		{"greater_than_true", GreaterThanPredicate{Column: colA, Value: i(5)}, row(i(6), nilV, nilV), true},
		{"greater_than_equal", GreaterThanPredicate{Column: colA, Value: i(5)}, row(i(5), nilV, nilV), false},
		{"greater_than_false", GreaterThanPredicate{Column: colA, Value: i(5)}, row(i(4), nilV, nilV), false},

		{"less_than_true", LessThanPredicate{Column: colA, Value: i(5)}, row(i(4), nilV, nilV), true},
		{"less_than_equal", LessThanPredicate{Column: colA, Value: i(5)}, row(i(5), nilV, nilV), false},
		{"less_than_false", LessThanPredicate{Column: colA, Value: i(5)}, row(i(6), nilV, nilV), false},

		{"and_both_true", AndPredicate{Left: GreaterThanPredicate{Column: colA, Value: i(0)}, Right: LessThanPredicate{Column: colB, Value: i(100)}}, row(i(5), i(50), nilV), true},
		{"and_right_false", AndPredicate{Left: GreaterThanPredicate{Column: colA, Value: i(0)}, Right: LessThanPredicate{Column: colB, Value: i(100)}}, row(i(5), i(150), nilV), false},
		{"and_left_false", AndPredicate{Left: GreaterThanPredicate{Column: colA, Value: i(0)}, Right: LessThanPredicate{Column: colB, Value: i(100)}}, row(i(-1), i(50), nilV), false},

		{"or_left_true", OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colB, Value: i(99)}}, row(i(5), i(1), nilV), true},
		{"or_right_true", OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colB, Value: i(99)}}, row(i(1), i(99), nilV), true},
		{"or_both_false", OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colB, Value: i(99)}}, row(i(1), i(1), nilV), false},

		{"not_of_true", NotPredicate{Inner: EqualPredicate{Column: colA, Value: i(5)}}, row(i(5), nilV, nilV), false},
		{"not_of_false", NotPredicate{Inner: EqualPredicate{Column: colA, Value: i(5)}}, row(i(6), nilV, nilV), true},

		{"true", TruePredicate{}, row(i(0), nilV, nilV), true},
		{"false", FalsePredicate{}, row(i(0), nilV, nilV), false},
		{"nil", nil, row(i(0), nilV, nilV), true},

		{"nested", AndPredicate{
			Left:  OrPredicate{Left: EqualPredicate{Column: colA, Value: i(5)}, Right: EqualPredicate{Column: colA, Value: i(6)}},
			Right: NotPredicate{Inner: GreaterThanPredicate{Column: colB, Value: i(100)}},
		}, row(i(6), i(50), nilV), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := Row{Values: tc.values}

			cp, err := compilePredicate(tc.pred, lookup)
			require.NoError(t, err)

			got := cp.eval(row)
			assert.Equal(t, tc.want, got, "compiled result")
			// The compiled predicate must agree with the uncompiled evaluation
			// it replaced.
			assert.Equal(t, got, referenceEval(tc.pred, lookup, row), "compiled vs uncompiled result")
		})
	}
}

// TestCompilePredicate_FuncPredicate verifies FuncPredicate receives the resolved column
// and its row value, and that its result drives the row.
func TestCompilePredicate_FuncPredicate(t *testing.T) {
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

	assert.True(t, cp.eval(Row{Values: []Value{Int64Value(5)}}))
	assert.Same(t, colA.(*MemColumn), gotColumn.(*MemColumn))
	assert.Equal(t, int64(5), gotValue.Int64())

	assert.False(t, cp.eval(Row{Values: []Value{Int64Value(2)}}))
}

func TestCompilePredicate_MissingColumn(t *testing.T) {
	known := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
	missing := testColumn(datasetmd.PHYSICAL_TYPE_INT64)
	lookup := map[Column]int{known: 0}

	tests := []struct {
		name string
		pred Predicate
	}{
		{"leaf", EqualPredicate{Column: missing, Value: Int64Value(1)}},
		{"in_and_left", AndPredicate{Left: EqualPredicate{Column: missing, Value: Int64Value(1)}, Right: TruePredicate{}}},
		{"in_or_right", OrPredicate{Left: TruePredicate{}, Right: LessThanPredicate{Column: missing, Value: Int64Value(1)}}},
		{"in_not", NotPredicate{Inner: GreaterThanPredicate{Column: missing, Value: Int64Value(1)}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compilePredicate(tc.pred, lookup)
			require.Error(t, err)
		})
	}
}

func TestCompilePredicate_UnsupportedType(t *testing.T) {
	require.Panics(t, func() {
		_, _ = compilePredicate(unknownPredicate{}, map[Column]int{})
	})
}

// referenceEval evaluates the original (uncompiled) predicate directly,
// resolving each leaf's column by a map lookup per node. It is the
// straightforward algorithm that compilePredicate replaced, kept here as an
// oracle: a compiled predicate must produce the same result as referenceEval
// for every row.
func referenceEval(p Predicate, lookup map[Column]int, row Row) bool {
	switch p := p.(type) {
	case nil:
		return true
	case AndPredicate:
		return referenceEval(p.Left, lookup, row) && referenceEval(p.Right, lookup, row)
	case OrPredicate:
		return referenceEval(p.Left, lookup, row) || referenceEval(p.Right, lookup, row)
	case NotPredicate:
		return !referenceEval(p.Inner, lookup, row)
	case TruePredicate:
		return true
	case FalsePredicate:
		return false
	case EqualPredicate:
		return CompareValues(&row.Values[lookup[p.Column]], &p.Value) == 0
	case InPredicate:
		value := row.Values[lookup[p.Column]]
		if value.IsNil() || value.Type() != p.Column.ColumnDesc().Type.Physical {
			return false
		}
		return p.Values.Contains(value)
	case GreaterThanPredicate:
		return CompareValues(&row.Values[lookup[p.Column]], &p.Value) > 0
	case LessThanPredicate:
		return CompareValues(&row.Values[lookup[p.Column]], &p.Value) < 0
	case FuncPredicate:
		return p.Keep(p.Column, row.Values[lookup[p.Column]])
	default:
		panic(fmt.Sprintf("referenceEval: unsupported predicate type %T", p))
	}
}

// testColumn returns a minimal Column for predicate tests. Predicate evaluation
// only needs the physical type (for InPredicate) and the column's identity as a
// lookup key; it never reads pages.
func testColumn(physical datasetmd.PhysicalType) Column {
	return &MemColumn{Desc: ColumnDesc{Type: ColumnType{Physical: physical}}}
}

type unknownPredicate struct{}

func (unknownPredicate) isPredicate() {}
