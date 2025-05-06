package executor

import (
	"math"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/stretchr/testify/require"
)

var (
	testOpenStart = time.Unix(0, math.MinInt64)
	testOpenEnd   = time.Unix(0, math.MaxInt64)
)

func literalTsExpr(nanos int64) *physical.LiteralExpr {
	return physical.NewLiteral(uint64(time.Unix(0, nanos).UnixNano()))
}

func literalStrExpr(s string) *physical.LiteralExpr {
	return physical.NewLiteral(s)
}

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
	time100 := time.Unix(0, 100)
	time200 := time.Unix(0, 200)

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
				Right: literalTsExpr(100),
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
				Right: literalTsExpr(100),
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
				Right: literalTsExpr(100),
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
				Right: literalTsExpr(100),
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
				Right: literalTsExpr(100),
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
					Right: literalTsExpr(100),
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
						Right: literalTsExpr(200),
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
			expr:     literalTsExpr(100),
			errMatch: "unsupported expression type: *physical.LiteralExpr",
		},
		{
			desc: "LHS of BinaryExpr is not ColumnExpr",
			expr: &physical.BinaryExpr{
				Left:  literalTsExpr(50),
				Op:    types.BinaryOpEq,
				Right: literalTsExpr(100),
			},
			errMatch: "unsupported LHS type: *physical.LiteralExpr",
		},
		{
			desc: "LHS column is not timestamp",
			expr: &physical.BinaryExpr{
				Left:  newColumnExpr("other_col", types.ColumnTypeBuiltin),
				Op:    types.BinaryOpEq,
				Right: literalTsExpr(100),
			},
			errMatch: "unsupported LHS column: other_col",
		},
		{
			desc: "RHS Literal is not a timestamp type",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: literalStrExpr("not_a_timestamp"),
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
				Right: literalTsExpr(100),
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
					Right: literalTsExpr(100),
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
					Right: literalTsExpr(100),
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
