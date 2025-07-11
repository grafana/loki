package executor

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
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

func ts(t time.Time) datatype.Timestamp {
	return datatype.Timestamp(t.UnixNano())
}

func TestMapTimestampPredicate(t *testing.T) {
	time100 := time.Unix(0, 100).UTC()
	time200 := time.Unix(0, 200).UTC()
	time300 := time.Unix(0, 300).UTC()

	for _, tc := range []struct {
		desc     string
		expr     physical.Expression
		want     logs.TimeRangeRowPredicate
		errMatch string
	}{
		{
			desc: "timestamp == 100",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral(ts(time100)),
			},
			want: logs.TimeRangeRowPredicate{
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
				Right: physical.NewLiteral(ts(time100)),
			},
			want: logs.TimeRangeRowPredicate{
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
				Right: physical.NewLiteral(ts(time100)),
			},
			want: logs.TimeRangeRowPredicate{
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
				Right: physical.NewLiteral(ts(time100)),
			},
			want: logs.TimeRangeRowPredicate{
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
				Right: physical.NewLiteral(ts(time100)),
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    testOpenStart,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp > (timestamp < 100) - now invalid",
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpGt,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time100)),
				},
			},
			errMatch: "invalid RHS for comparison: expected literal timestamp, got *physical.BinaryExpr",
		},
		{
			desc: "timestamp == (timestamp >= (timestamp <= 200)) - now invalid",
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpEq,
				Right: &physical.BinaryExpr{
					Left: tsColExpr(),
					Op:   types.BinaryOpGte,
					Right: &physical.BinaryExpr{
						Left:  tsColExpr(),
						Op:    types.BinaryOpLte,
						Right: physical.NewLiteral(ts(time200)),
					},
				},
			},
			errMatch: "invalid RHS for comparison: expected literal timestamp, got *physical.BinaryExpr",
		},
		{
			desc:     "input is not BinaryExpr",
			expr:     physical.NewLiteral(ts(time100)),
			errMatch: "unsupported expression type for timestamp predicate: *physical.LiteralExpr, expected *physical.BinaryExpr",
		},
		{
			desc: "LHS of BinaryExpr is not ColumnExpr",
			expr: &physical.BinaryExpr{
				Left:  physical.NewLiteral(ts(time100)),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral(ts(time200)),
			},
			errMatch: "invalid LHS for comparison: expected timestamp column, got 1970-01-01T00:00:00.0000001Z",
		},
		{
			desc: "LHS column is not timestamp",
			expr: &physical.BinaryExpr{
				Left:  newColumnExpr("other_col", types.ColumnTypeBuiltin),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral(ts(time100)),
			},
			errMatch: "invalid LHS for comparison: expected timestamp column, got builtin.other_col",
		},
		{
			desc: "RHS Literal is not a timestamp type",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: physical.NewLiteral("not_a_timestamp"),
			},
			errMatch: "unsupported literal type for RHS: string, expected timestamp",
		},
		{
			desc: "RHS is an unsupported type (ColumnExpr)",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpEq,
				Right: newColumnExpr("another_ts", types.ColumnTypeBuiltin),
			},
			errMatch: "invalid RHS for comparison: expected literal timestamp, got *physical.ColumnExpr",
		},
		{
			desc: "Unsupported operator (AND) used as a simple comparison",
			expr: &physical.BinaryExpr{
				Left:  tsColExpr(),
				Op:    types.BinaryOpAnd,
				Right: physical.NewLiteral(ts(time100)),
			},
			errMatch: "invalid left operand for AND: unsupported expression type for timestamp predicate: *physical.ColumnExpr, expected *physical.BinaryExpr",
		},
		{
			desc: "Unsupported operator (OR) from outer op in nested scenario", // OR is still unsupported directly
			expr: &physical.BinaryExpr{
				Left: tsColExpr(),
				Op:   types.BinaryOpOr,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(ts(time100)),
				},
			},
			errMatch: "unsupported operator for timestamp predicate: OR",
		},
		{
			desc: "timestamp > 100 AND timestamp < 200",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    time100,
				EndTime:      time200,
				IncludeStart: false,
				IncludeEnd:   false,
			},
		},
		{
			desc: "timestamp >= 100 AND timestamp <= 200",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGte,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLte,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    time100,
				EndTime:      time200,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp >= 100 AND timestamp < 100 (point exclusive)", // leads to impossible range
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGte,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time100)),
				},
			},
			errMatch: "impossible time range: start_time (1970-01-01 00:00:00.0000001 +0000 UTC) equals end_time (1970-01-01 00:00:00.0000001 +0000 UTC) but the range is exclusive",
		},
		{
			desc: "timestamp > 100 AND timestamp <= 100 (point exclusive)", // leads to impossible range
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLte,
					Right: physical.NewLiteral(ts(time100)),
				},
			},
			errMatch: "impossible time range: start_time (1970-01-01 00:00:00.0000001 +0000 UTC) equals end_time (1970-01-01 00:00:00.0000001 +0000 UTC) but the range is exclusive",
		},
		{
			desc: "timestamp >= 100 AND timestamp <= 100 (point inclusive)",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGte,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLte,
					Right: physical.NewLiteral(ts(time100)),
				},
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    time100,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		{
			desc: "timestamp == 100 AND timestamp == 200", // impossible
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpEq,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpEq,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			errMatch: "impossible time range: start_time (1970-01-01 00:00:00.0000002 +0000 UTC) is after end_time (1970-01-01 00:00:00.0000001 +0000 UTC)",
		},
		{
			desc: "timestamp < 100 AND timestamp > 200", // impossible
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			errMatch: "impossible time range: start_time (1970-01-01 00:00:00.0000002 +0000 UTC) is after end_time (1970-01-01 00:00:00.0000001 +0000 UTC)",
		},
		{
			desc: "(timestamp > 100 AND timestamp < 300) AND timestamp == 200",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left: &physical.BinaryExpr{
						Left:  tsColExpr(),
						Op:    types.BinaryOpGt,
						Right: physical.NewLiteral(ts(time100)),
					},
					Op: types.BinaryOpAnd,
					Right: &physical.BinaryExpr{
						Left:  tsColExpr(),
						Op:    types.BinaryOpLt,
						Right: physical.NewLiteral(ts(time300)),
					},
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpEq,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    time200,
				EndTime:      time200,
				IncludeStart: true,
				IncludeEnd:   true,
			},
		},
		{
			desc: "AND with invalid left operand",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{ // Invalid: LHS not timestamp column
					Left:  newColumnExpr("not_ts", types.ColumnTypeBuiltin),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			errMatch: "invalid left operand for AND: invalid LHS for comparison: expected timestamp column, got builtin.not_ts",
		},
		{
			desc: "AND with invalid right operand (RHS literal type)",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpGt,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{ // Invalid: RHS literal not timestamp
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral("not-a-time"),
				},
			},
			errMatch: "invalid right operand for AND: unsupported literal type for RHS: string, expected timestamp",
		},
		{
			desc: "timestamp < 100 AND timestamp < 200 (should be ts < 100)",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time100)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time200)),
				},
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    testOpenStart,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   false,
			},
		},
		{
			desc: "timestamp < 200 AND timestamp < 100 (should be ts < 100)",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time200)),
				},
				Op: types.BinaryOpAnd,
				Right: &physical.BinaryExpr{
					Left:  tsColExpr(),
					Op:    types.BinaryOpLt,
					Right: physical.NewLiteral(ts(time100)),
				},
			},
			want: logs.TimeRangeRowPredicate{
				StartTime:    testOpenStart,
				EndTime:      time100,
				IncludeStart: true,
				IncludeEnd:   false,
			},
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
		expectedPred  logs.RowPredicate
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
			expectedPred: logs.MetadataMatcherRowPredicate{Key: "foo", Value: "bar"},
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
			expectedPred: logs.AndRowPredicate{
				Left:  logs.MetadataMatcherRowPredicate{Key: "foo", Value: "bar"},
				Right: logs.MetadataMatcherRowPredicate{Key: "baz", Value: "qux"},
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
			expectedPred: logs.OrRowPredicate{
				Left:  logs.MetadataMatcherRowPredicate{Key: "foo", Value: "bar"},
				Right: logs.MetadataMatcherRowPredicate{Key: "baz", Value: "qux"},
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
			expectedPred: logs.NotRowPredicate{
				Inner: logs.MetadataMatcherRowPredicate{Key: "foo", Value: "bar"},
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
			expectedPred: logs.AndRowPredicate{
				Left: logs.MetadataMatcherRowPredicate{Key: "foo", Value: "bar"},
				Right: logs.OrRowPredicate{
					Left:  logs.MetadataMatcherRowPredicate{Key: "baz", Value: "qux"},
					Right: logs.MetadataMatcherRowPredicate{Key: "faz", Value: "fuzz"},
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
				Right: physical.NewLiteral(int64(123)), // Not string
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

func TestMapMessagePredicate(t *testing.T) {
	for _, tc := range []struct {
		name         string
		expr         physical.Expression
		expectedErr  string
		expectedType string
	}{
		{
			name: "string match filter",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral("dataobjscan"),
				Op:    types.BinaryOpMatchSubstr,
			},
			expectedType: "logs.LogMessageFilterRowPredicate",
		},
		{
			name: "not string match filter",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral("dataobjscan"),
				Op:    types.BinaryOpNotMatchSubstr,
			},
			expectedType: "logs.LogMessageFilterRowPredicate",
		},
		{
			name: "regex match filter",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral(`\d{4}-\d{2}-\d{2}`),
				Op:    types.BinaryOpMatchRe,
			},
			expectedType: "logs.LogMessageFilterRowPredicate",
		},
		{
			name: "not regex match filter",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral(`\d{4}-\d{2}-\d{2}`),
				Op:    types.BinaryOpNotMatchRe,
			},
			expectedType: "logs.LogMessageFilterRowPredicate",
		},
		{
			name: "pattern match filter",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral("<_> dataobj <_>"),
				Op:    types.BinaryOpMatchPattern,
			},
			expectedErr: "unsupported binary operator (MATCH_PAT) for log message predicate",
		},
		{
			name: "not pattern match filter",
			expr: &physical.BinaryExpr{
				Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral("<_> dataobj <_>"),
				Op:    types.BinaryOpNotMatchPattern,
			},
			expectedErr: "unsupported binary operator (NOT_MATCH_PAT) for log message predicate",
		},
		{
			name: "and filter",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
					Right: physical.NewLiteral("foo"),
					Op:    types.BinaryOpMatchSubstr,
				},
				Right: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
					Right: physical.NewLiteral("bar"),
					Op:    types.BinaryOpNotMatchSubstr,
				},
				Op: types.BinaryOpAnd,
			},
			expectedType: "logs.AndRowPredicate",
		},
		{
			name: "or filter",
			expr: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
					Right: physical.NewLiteral("foo"),
					Op:    types.BinaryOpMatchSubstr,
				},
				Right: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
					Right: physical.NewLiteral("bar"),
					Op:    types.BinaryOpNotMatchSubstr,
				},
				Op: types.BinaryOpOr,
			},
			expectedType: "logs.OrRowPredicate",
		},
		{
			name: "not filter",
			expr: &physical.UnaryExpr{
				Left: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
					Right: physical.NewLiteral("foo"),
					Op:    types.BinaryOpMatchSubstr,
				},
				Op: types.UnaryOpNot,
			},
			expectedType: "logs.NotRowPredicate",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s", tc.expr)
			got, err := mapMessagePredicate(tc.expr)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedType, fmt.Sprintf("%T", got))
			}
		})
	}
}
