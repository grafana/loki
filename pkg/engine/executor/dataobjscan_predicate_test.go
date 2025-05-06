package executor

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

var (
	testOpenStart = time.Unix(0, math.MinInt64).UTC()
	testOpenEnd   = time.Unix(0, math.MaxInt64).UTC()
)

func newColumnExpr(column string, ty types.ColumnType) *physical.ColumnExpr {
	return &physical.ColumnExpr{
		Ref: types.ColumnRef{
			Column: column,
			Type:   ty,
		},
	}
}

func tsColExpr() *physical.ColumnExpr {
	return newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin)
}

func TestMapTimestampPredicate(t *testing.T) {
	time100 := time.Unix(0, 100).UTC()
	time200 := time.Unix(0, 200).UTC()

	for _, tc := range []struct {
		desc     string
		expr     physical.Expression
		want     dataobj.TimeRangePredicate[dataobj.LogsPredicate]
		errMatch string
	}{
		// Simple Valid Cases
		{
			desc: "timestamp == 100",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral(time100),
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    time100,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp > 100",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpGt,
				Right: physical.NewLiteral(time100),
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    time100,
				EndTime:      testOpenEnd,
				IncludeStart: false,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp >= 100",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpGte,
				Right: physical.NewLiteral(time100),
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    time100,
				EndTime:      testOpenEnd,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp < 100",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpLt,
				Right: physical.NewLiteral(time100),
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    testOpenStart,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   false,
			},
		},
		{
			desc: "timestamp <= 100",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpLte,
				Right: physical.NewLiteral(time100),
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    testOpenStart,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		// Nested RHS Valid Cases (outermost op with innermost literal)
		{
			desc: "timestamp > (timestamp < 100)", // Effectively timestamp > 100
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpGt,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(time100),
				},
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    time100,
				EndTime:      testOpenEnd,
				IncludeStart: false,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp == (timestamp >= (timestamp <= 200))", // Effectively timestamp == 200
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpEq,
				Right: &physical.BinaryExpr{
					Left: tsColExpr(),
					Op:   types.BinaryOpGte,
					Right: &physical.BinaryExpr{
						Left:  tsColExpr(),
						Op:    types.BinaryOpLte,
						Right: physical.NewLiteral(time200),
					},
				},
			},
			want: dataobj.TimeRangePredicate[dataobj.LogsPredicate]{
				StartTime:    time200,
				EndTime:      time200,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		// Error Cases: Verification Phase
		{
			desc:     "input is not BinaryExpr",
			expr:     physical.NewLiteral(time100),
			errMatch: "unsupported expression type: *physical.LiteralExpr",
		},
		{
			desc: "LHS of BinaryExpr is not ColumnExpr",
			expr: &physical.BinaryExpr{
				Left:  physical.NewLiteral(time100),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral(time200),
			},
			errMatch: "unsupported LHS type: *physical.LiteralExpr",
		},
		{
			desc: "LHS column is not timestamp",
			expr: &physical.BinaryExpr{
				Left:  newColumnExpr("other_col", types.ColumnTypeBuiltin),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral(time100),
			},
			errMatch: "unsupported LHS column: other_col",
		},
		{
			desc: "RHS Literal is not a timestamp type",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral("not_a_timestamp"),
			},
			errMatch: "unsupported literal type: string",
		},
		{
			desc: "RHS is an unsupported type (ColumnExpr)",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: newColumnExpr("another_ts", types.ColumnTypeBuiltin),
			},
			errMatch: "unsupported RHS type: *physical.ColumnExpr",
		},
		{
			desc: "Unsupported operator (AND) for rebound",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpAnd,
				Right: physical.NewLiteral(time100),
			},
			errMatch: "unsupported operator: AND",
		},
		{
			desc: "Nested RHS fails verification (inner LHS not timestamp)",
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpGt,
				Right: &physical.BinaryExpr{
					Left:  newColumnExpr("not_ts", types.ColumnTypeBuiltin),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(time100),
				},
			},
			errMatch: "unsupported LHS column: not_ts",
		},
		{
			desc: "Nested RHS fails verification (inner RHS unsupported type)",
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpGt,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: newColumnExpr("not_literal", types.ColumnTypeBuiltin),
				},
			},
			errMatch: "unsupported RHS type: *physical.ColumnExpr",
		},
		{
			desc: "Unsupported operator (OR) from outer op in nested scenario",
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpOr,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(time100),
				},
			},
			errMatch: "unsupported operator: OR",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := mapTimestampPredicate(tc.expr)
			if tc.errMatch != "" {
				require.ErrorContains(t, err, tc.errMatch)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestMapMetadataPredicate(t *testing.T) {
	tests := []struct {
		name          string
		expr          physical.Expression
		expectedPred  dataobj.Predicate
		expectedErr   bool
		expectedErrAs any // For errors.As checks
	}{
		{
			name: "simple metadata matcher",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
				Right: physical.NewLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
			expectedPred: dataobj.MetadataMatcherPredicate{Key: "foo", Value: "bar"},
			expectedErr:  false,
		},
		{
			name: "AND predicate",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
					Right: physical.NewLiteral("bar"),
					Op:    types.BinaryOpEq,
				},
				Right: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "baz", Type: types.ColumnTypeMetadata}},
					Right: physical.NewLiteral("qux"),
					Op:    types.BinaryOpEq,
				},
				Op: types.BinaryOpAnd,
			},
			expectedPred: dataobj.AndPredicate[dataobj.LogsPredicate]{
				Left:  dataobj.MetadataMatcherPredicate{Key: "foo", Value: "bar"},
				Right: dataobj.MetadataMatcherPredicate{Key: "baz", Value: "qux"},
			},
			expectedErr: false,
		},
		{
			name: "OR predicate",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
					Right: physical.NewLiteral("bar"),
					Op:    types.BinaryOpEq,
				},
				Right: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "baz", Type: types.ColumnTypeMetadata}},
					Right: physical.NewLiteral("qux"),
					Op:    types.BinaryOpEq,
				},
				Op: types.BinaryOpOr,
			},
			expectedPred: dataobj.OrPredicate[dataobj.LogsPredicate]{
				Left:  dataobj.MetadataMatcherPredicate{Key: "foo", Value: "bar"},
				Right: dataobj.MetadataMatcherPredicate{Key: "baz", Value: "qux"},
			},
			expectedErr: false,
		},
		{
			name: "NOT predicate",
			expr: &physical.UnaryExpr{
				Left: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
					Right: physical.NewLiteral("bar"),
					Op:    types.BinaryOpEq,
				},
				Op: types.UnaryOpNot,
			},
			expectedPred: dataobj.NotPredicate[dataobj.LogsPredicate]{
				Inner: dataobj.MetadataMatcherPredicate{Key: "foo", Value: "bar"},
			},
			expectedErr: false,
		},
		{
			name: "nested AND OR predicate",
			expr: &physical.BinaryExpr{ // AND
				Left: &physical.BinaryExpr{ // foo == "bar"
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
					Right: physical.NewLiteral("bar"),
					Op:    types.BinaryOpEq,
				},
				Right: &physical.BinaryExpr{ // OR
					Left: &physical.BinaryExpr{ // baz == "qux"
						Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "baz", Type: types.ColumnTypeMetadata}},
						Right: physical.NewLiteral("qux"),
						Op:    types.BinaryOpEq,
					},
					Right: &physical.BinaryExpr{ // faz == "fuzz"
						Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "faz", Type: types.ColumnTypeMetadata}},
						Right: physical.NewLiteral("fuzz"),
						Op:    types.BinaryOpEq,
					},
					Op: types.BinaryOpOr,
				},
				Op: types.BinaryOpAnd,
			},
			expectedPred: dataobj.AndPredicate[dataobj.LogsPredicate]{
				Left: dataobj.MetadataMatcherPredicate{Key: "foo", Value: "bar"},
				Right: dataobj.OrPredicate[dataobj.LogsPredicate]{
					Left:  dataobj.MetadataMatcherPredicate{Key: "baz", Value: "qux"},
					Right: dataobj.MetadataMatcherPredicate{Key: "faz", Value: "fuzz"},
				},
			},
			expectedErr: false,
		},
		{
			name: "error: unsupported binary operator",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
				Right: physical.NewLiteral("bar"),
				Op:    types.BinaryOpGt, // GT is not supported for metadata
			},
			expectedPred: nil,
			expectedErr:  true,
		},
		{
			name: "error: unsupported unary operator",
			expr: &physical.UnaryExpr{
				Left: &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
				Op:   types.UnaryOpAbs, // ABS is not supported
			},
			expectedPred: nil,
			expectedErr:  true,
		},
		{
			name: "error: LHS not column expr in binary expr",
			expr: &physical.BinaryExpr{
				Left:  physical.NewLiteral("foo"),
				Right: physical.NewLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
			expectedPred: nil,
			expectedErr:  true,
		},
		{
			name: "error: LHS column not metadata type",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeBuiltin}}, // Not metadata
				Right: physical.NewLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
			expectedPred: nil,
			expectedErr:  true,
		},
		{
			name: "error: RHS not literal for EQ",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
				Right: &physical.ColumnExpr{Ref: types.ColumnRef{Column: "bar", Type: types.ColumnTypeMetadata}}, // Not literal
				Op:    types.BinaryOpEq,
			},
			expectedPred: nil,
			expectedErr:  true,
		},
		{
			name: "error: RHS literal not string for EQ",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "foo", Type: types.ColumnTypeMetadata}},
				Right: physical.NewLiteral(123), // Not string
				Op:    types.BinaryOpEq,
			},
			expectedPred: nil,
			expectedErr:  true,
		},
		{
			name:         "error: unsupported expression type (LiteralExpr)",
			expr:         physical.NewLiteral("just a string"),
			expectedPred: nil,
			expectedErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pred, err := mapMetadataPredicate(tc.expr)

			if tc.expectedErr {
				require.Error(t, err)
				if tc.expectedErrAs != nil {
					require.ErrorAs(t, err, tc.expectedErrAs)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedPred, pred)
			}
		})
	}
}
