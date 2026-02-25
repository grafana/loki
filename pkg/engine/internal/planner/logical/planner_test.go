package logical

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

type query struct {
	statement      string
	start, end     int64
	step, interval time.Duration
	direction      logproto.Direction
	limit          uint32
}

// Direction implements logql.Params.
func (q *query) Direction() logproto.Direction {
	return q.direction
}

// End implements logql.Params.
func (q *query) End() time.Time {
	return time.Unix(q.end, 0)
}

// Start implements logql.Params.
func (q *query) Start() time.Time {
	return time.Unix(q.start, 0)
}

// Limit implements logql.Params.
func (q *query) Limit() uint32 {
	return q.limit
}

// QueryString implements logql.Params.
func (q *query) QueryString() string {
	return q.statement
}

// GetExpression implements logql.Params.
func (q *query) GetExpression() syntax.Expr {
	return syntax.MustParseExpr(q.statement)
}

// CachingOptions implements logql.Params.
func (q *query) CachingOptions() resultscache.CachingOptions {
	panic("unimplemented")
}

// GetStoreChunks implements logql.Params.
func (q *query) GetStoreChunks() *logproto.ChunkRefGroup {
	panic("unimplemented")
}

// Interval implements logql.Params.
func (q *query) Interval() time.Duration {
	return q.interval
}

// Shards implements logql.Params.
func (q *query) Shards() []string {
	return []string{"0_of_1"} // 0_of_1 == noShard
}

// Step implements logql.Params.
func (q *query) Step() time.Duration {
	return q.step
}

var _ logql.Params = (*query)(nil)

func TestConvertAST_Success(t *testing.T) {
	q := &query{
		statement: `{cluster="prod", namespace=~"loki-.*"} | foo="bar" or bar="baz" |= "metric.go" |= "foo" or "bar" !~ "(a|b|c)" `,
		start:     3600,
		end:       7200,
		direction: logproto.BACKWARD, // ASC is not supported
		limit:     1000,
	}
	logicalPlan, err := BuildPlan(context.Background(), q)
	require.NoError(t, err)
	t.Logf("\n%s\n", logicalPlan.String())

	expected := `%1 = EQ label.cluster "prod"
%2 = MATCH_RE label.namespace "loki-.*"
%3 = AND %1 %2
%4 = EQ ambiguous.foo "bar"
%5 = EQ ambiguous.bar "baz"
%6 = OR %4 %5
%7 = MATCH_STR builtin.message "metric.go"
%8 = MATCH_STR builtin.message "foo"
%9 = AND %7 %8
%10 = NOT_MATCH_RE builtin.message "(a|b|c)"
%11 = AND %9 %10
%12 = MAKETABLE [selector=%3, predicates=[%6, %11], shard=0_of_1]
%13 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%14 = SELECT %12 [predicate=%13]
%15 = LT builtin.timestamp 1970-01-01T02:00:00Z
%16 = SELECT %14 [predicate=%15]
%17 = SELECT %16 [predicate=%6]
%18 = SELECT %17 [predicate=%11]
%19 = TOPK %18 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%20 = LOGQL_COMPAT %19
RETURN %20
`

	require.Equal(t, expected, logicalPlan.String())

	var sb strings.Builder
	PrintTree(&sb, logicalPlan.Value())

	t.Logf("\n%s\n", sb.String())
}

func TestConvertAST_MetricQuery_Success(t *testing.T) {
	t.Run("simple metric query", func(t *testing.T) {
		q := &query{
			statement: `sum by (level) (count_over_time({cluster="prod", namespace=~"loki-.*"} |= "metric.go"[5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		logicalPlan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", logicalPlan.String())

		expected := `%1 = EQ label.cluster "prod"
%2 = MATCH_RE label.namespace "loki-.*"
%3 = AND %1 %2
%4 = MATCH_STR builtin.message "metric.go"
%5 = MAKETABLE [selector=%3, predicates=[%4], shard=0_of_1]
%6 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%7 = SELECT %5 [predicate=%6]
%8 = LT builtin.timestamp 1970-01-01T02:00:00Z
%9 = SELECT %7 [predicate=%8]
%10 = SELECT %9 [predicate=%4]
%11 = RANGE_AGGREGATION %10 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%12 = VECTOR_AGGREGATION %11 [operation=sum, group_by=(ambiguous.level)]
%13 = LOGQL_COMPAT %12
RETURN %13
`

		require.Equal(t, expected, logicalPlan.String())

		var sb strings.Builder
		PrintTree(&sb, logicalPlan.Value())

		t.Logf("\n%s\n", sb.String())
	})

	t.Run(`metric query with one binary math operation`, func(t *testing.T) {
		q := &query{
			statement: `sum by (level) (count_over_time({cluster="prod", namespace=~"loki-.*"}[5m]) / 300)`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		logicalPlan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", logicalPlan.String())

		expected := `%1 = EQ label.cluster "prod"
%2 = MATCH_RE label.namespace "loki-.*"
%3 = AND %1 %2
%4 = MAKETABLE [selector=%3, predicates=[], shard=0_of_1]
%5 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%6 = SELECT %4 [predicate=%5]
%7 = LT builtin.timestamp 1970-01-01T02:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = RANGE_AGGREGATION %8 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%10 = DIV %9 300
%11 = VECTOR_AGGREGATION %10 [operation=sum, group_by=(ambiguous.level)]
%12 = LOGQL_COMPAT %11
RETURN %12
`

		require.Equal(t, expected, logicalPlan.String())

		var sb strings.Builder
		PrintTree(&sb, logicalPlan.Value())

		t.Logf("\n%s\n", sb.String())
	})

	t.Run(`rate metric query with nested math expression`, func(t *testing.T) {
		q := &query{
			statement: `sum by (level) ((rate({cluster="prod"}[5m]) - 100) ^ 2)`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		logicalPlan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", logicalPlan.String())

		expected := `%1 = EQ label.cluster "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = RANGE_AGGREGATION %6 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%8 = DIV %7 300
%9 = SUB %8 100
%10 = POW %9 2
%11 = VECTOR_AGGREGATION %10 [operation=sum, group_by=(ambiguous.level)]
%12 = LOGQL_COMPAT %11
RETURN %12
`

		require.Equal(t, expected, logicalPlan.String())

		var sb strings.Builder
		PrintTree(&sb, logicalPlan.Value())

		t.Logf("\n%s\n", sb.String())
	})
}

func TestCanExecuteQuery(t *testing.T) {
	for _, tt := range []struct {
		statement string
		expected  bool
	}{
		{
			statement: `{env="prod"}`,
			expected:  true,
		},
		{
			statement: `{env="prod"} |= "metrics.go"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} |= "metrics.go"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | tenant="loki"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | tenant="loki" != "foo"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | json`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | json foo="bar"`,
		},
		{
			statement: `{env="prod"} | logfmt`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | logfmt foo="bar"`,
		},
		{
			statement: `{env="prod"} | pattern "<_> foo=<foo> <_>"`,
		},
		{
			statement: `{env="prod"} | regexp ".* foo=(?P<foo>.+) .*"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | unpack`,
		},
		{
			statement: `{env="prod"} |= "metrics.go" | logfmt`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | line_format "{.cluster}"`,
		},
		{
			statement: `{env="prod"} | label_format cluster="us"`,
		},
		{
			statement: `{env="prod"} |= "metric.go" | retry > 2`,
		},
		{
			statement: `sum by (level) (count_over_time({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `sum by (level) (count_over_time({env="prod"}[1m]) / 60)`,
			expected:  true,
		},
		{
			statement: `sum by (level) (count_over_time({env="prod"}[1m]) / 60 * 4)`,
			expected:  true,
		},
		{
			// two inputs are not supported
			statement: `sum by (level) (count_over_time({env="prod"} |= "error" [1m]) / count_over_time({env="prod"}[1m]))`,
		},
		{
			statement: `sum without (level) (count_over_time({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `count_over_time({env="prod"}[1m])`,
			expected:  true,
		},
		{
			statement: `sum(count_over_time({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `max(avg_over_time({env="prod"} | unwrap size [1m]))`,
			expected:  true,
		},
		{
			statement: `sum by (level) (rate({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `avg by (level) (rate({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `max by (level) (count_over_time({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			// offset is not supported
			statement: `sum by (level) (count_over_time({env="prod"}[1m] offset 5m))`,
		},
		{
			statement: `sum by (level) (sum_over_time({env="prod"} | unwrap size [1m]))`,
			expected:  true,
		},
		{
			statement: `sum_over_time({env="prod"} | unwrap size [1m])`,
			expected:  true,
		},
		{
			statement: `sum(sum_over_time({env="prod"} | unwrap size [1m]))`,
			expected:  true,
		},
		{
			statement: `max by (level) (sum_over_time({env="prod"} | unwrap size [1m]))`,
			expected:  true,
		},
		{
			// offset is not supported
			statement: `sum by (level) (sum_over_time({env="prod"} | unwrap size [1m] offset 5m))`,
		},
		{
			statement: `max_over_time({env="prod"} | unwrap size [1m])`,
			expected:  true,
		},
		{
			statement: `sum(count_over_time({env="prod"} | logfmt | drop __error__ [1m]))`,
			expected:  true,
		},
		{
			statement: `sum(count_over_time({env="prod"} | logfmt | drop __error__=~"Unknown Error: .*" [1m]))`,
		},
	} {
		t.Run(tt.statement, func(t *testing.T) {
			q := &query{
				statement: tt.statement,
				start:     1000,
				end:       2000,
				direction: logproto.BACKWARD,
				limit:     1000,
			}

			logicalPlan, err := BuildPlan(context.Background(), q)
			if tt.expected {
				require.NoError(t, err)
				t.Logf("\n%s\n", logicalPlan.String())
			} else {
				require.Nil(t, logicalPlan)
				require.ErrorContains(t, err, "failed to convert AST into logical plan")
			}
		})
	}
}

func TestConvertAST_WithDeletes(t *testing.T) {
	t.Run("query with simple delete request - success", func(t *testing.T) {
		q := &query{
			statement: `{cluster="prod"} |= "error"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		deletes := []*deletion.Request{
			{
				Selector: `{job="test"}`,
				Start:    4000 * 1e9, // 4000 seconds in nanoseconds
				End:      5000 * 1e9, // 5000 seconds in nanoseconds
			},
		}

		logicalPlan, err := BuildPlanWithDeletes(context.Background(), q, deletes)
		require.NoError(t, err)

		// Delete request is within query range (3600-7200), it should generate:
		// ts < 4000 OR ts > 5000 OR NOT(job="test")
		expected := `%1 = EQ label.cluster "prod"
%2 = MATCH_STR builtin.message "error"
%3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
%4 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%5 = SELECT %3 [predicate=%4]
%6 = LT builtin.timestamp 1970-01-01T02:00:00Z
%7 = SELECT %5 [predicate=%6]
%8 = SELECT %7 [predicate=%2]
%9 = LT builtin.timestamp 1970-01-01T01:06:40Z
%10 = GT builtin.timestamp 1970-01-01T01:23:20Z
%11 = OR %9 %10
%12 = EQ label.job "test"
%13 = NOT(%12)
%14 = OR %11 %13
%15 = SELECT %8 [predicate=%14]
%16 = TOPK %15 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%17 = LOGQL_COMPAT %16
RETURN %17
`

		require.Equal(t, expected, logicalPlan.String())
	})

	t.Run("delete request containing parser should fail", func(t *testing.T) {
		q := &query{
			statement: `{cluster="prod"} |= "error"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		deletes := []*deletion.Request{
			{
				Selector: `{job="test"} | json`, // Parser is not supported in delete requests
				Start:    4000 * 1e9,
				End:      5000 * 1e9,
			},
		}

		logicalPlan, err := BuildPlanWithDeletes(context.Background(), q, deletes)
		require.Nil(t, logicalPlan)

		require.Error(t, err)
		require.ErrorIs(t, err, errUnimplemented)

		require.ErrorContains(t, err, "delete request with unsupported stages")
	})
}

func TestPlannerCreatesCastOperationForUnwrap(t *testing.T) {
	t.Run("creates projection with unary cast operation instruction for metric query with unwrap duration", func(t *testing.T) {
		// Query with duration unwrap in a sum_over_time metric query
		q := &query{
			statement: `sum by (status) (sum_over_time({app="api"} | unwrap duration(response_time) [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the correct SSA representation
		// The UNWRAP should appear after SELECT operations but before RANGE_AGGREGATION
		// Error filtering is added after unwrap to exclude rows with invalid numeric values
		expected := `%1 = EQ label.app "api"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = CAST_DURATION(ambiguous.response_time)
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = PROJECT %8 [mode=*D, expr=ambiguous.response_time]
%10 = EQ generated.__error__ ""
%11 = EQ generated.__error_details__ ""
%12 = AND %10 %11
%13 = SELECT %9 [predicate=%12]
%14 = RANGE_AGGREGATION %13 [operation=sum, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%15 = VECTOR_AGGREGATION %14 [operation=sum, group_by=(ambiguous.status)]
%16 = LOGQL_COMPAT %15
RETURN %16
`
		require.Equal(t, expected, plan.String())
	})
}

func TestPlannerCreatesProjectionWithParseOperation(t *testing.T) {
	t.Run("creates projection instruction with logfmt parse operation for metric query", func(t *testing.T) {
		// Query with logfmt parser followed by label filter in an instant metric query
		q := &query{
			statement: `sum by (level) (count_over_time({app="test"} | logfmt | level="error" [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the correct SSA representation
		// Since there are no filters before logfmt, parse comes right after MAKETABLE
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PARSE_LOGFMT(builtin.message, [], false, false)
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = EQ ambiguous.level "error"
%10 = SELECT %8 [predicate=%9]
%11 = RANGE_AGGREGATION %10 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%12 = VECTOR_AGGREGATION %11 [operation=sum, group_by=(ambiguous.level)]
%13 = LOGQL_COMPAT %12
RETURN %13
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("creates projection instruction with logfmt parse operation for log query", func(t *testing.T) {
		q := &query{
			statement: `{app="test"} | logfmt | level="error"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the SSA representation for log query
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PARSE_LOGFMT(builtin.message, [], false, false)
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = EQ ambiguous.level "error"
%10 = SELECT %8 [predicate=%9]
%11 = TOPK %10 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%12 = LOGQL_COMPAT %11
RETURN %12
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("creates projection instruction with json parse operation for metric query", func(t *testing.T) {
		// Query with logfmt parser followed by label filter in an instant metric query
		q := &query{
			statement: `sum by (level) (count_over_time({app="test"} | json | level="error" [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)

		// Assert against the correct SSA representation
		// Since there are no filters before logfmt, parse comes right after MAKETABLE
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PARSE_JSON(builtin.message, [], false, false)
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = EQ ambiguous.level "error"
%10 = SELECT %8 [predicate=%9]
%11 = RANGE_AGGREGATION %10 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%12 = VECTOR_AGGREGATION %11 [operation=sum, group_by=(ambiguous.level)]
%13 = LOGQL_COMPAT %12
RETURN %13
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("creates projection instruction with json parse operation for log query", func(t *testing.T) {
		q := &query{
			statement: `{app="test"} | json | level="error"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)

		// Assert against the SSA representation for log query
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PARSE_JSON(builtin.message, [], false, false)
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = EQ ambiguous.level "error"
%10 = SELECT %8 [predicate=%9]
%11 = TOPK %10 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%12 = LOGQL_COMPAT %11
RETURN %12
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("creates projection instruction with regexp parse operation for metric query", func(t *testing.T) {
		// Query with regexp parser followed by label filter in an instant metric query
		q := &query{
			statement: `sum by (foo) (count_over_time({app="test"} | regexp ".* foo=(?P<foo>.+) .*" | foo="bar" [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the correct SSA representation
		// Since there are no filters before regexp, parse comes right after MAKETABLE
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PARSE_REGEXP(builtin.message, ".* foo=(?P<foo>.+) .*")
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = EQ ambiguous.foo "bar"
%10 = SELECT %8 [predicate=%9]
%11 = RANGE_AGGREGATION %10 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%12 = VECTOR_AGGREGATION %11 [operation=sum, group_by=(ambiguous.foo)]
%13 = LOGQL_COMPAT %12
RETURN %13
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("creates projection instruction with regexp parse operation for log query", func(t *testing.T) {
		q := &query{
			statement: `{app="test"} | regexp "(?P<level>\\w+):\\s+(?P<message>.*)" | level="error"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the SSA representation for log query
		// Note: LogQL query has \\w+ which LogQL parses to \w+, then printed as \w+ in output
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PARSE_REGEXP(builtin.message, "(?P<level>\w+):\s+(?P<message>.*)")
%8 = PROJECT %6 [mode=*E, expr=%7]
%9 = EQ ambiguous.level "error"
%10 = SELECT %8 [predicate=%9]
%11 = TOPK %10 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%12 = LOGQL_COMPAT %11
RETURN %12
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("preserves operation order with filters before and after projection with parse operation", func(t *testing.T) {
		// A map of parse statement => generated
		parsers := map[string]string{
			`json`:                     `PARSE_JSON(builtin.message, [], false, false)`,
			`logfmt`:                   `PARSE_LOGFMT(builtin.message, [], false, false)`,
			`regexp "(?P<level>\\w+)"`: `PARSE_REGEXP(builtin.message, "(?P<level>\w+)")`,
		}

		for parser, statement := range parsers {
			// Test that filters before logfmt parse are applied before parsing,
			// and filters after logfmt parse are applied after parsing.
			// This is important for performance - we don't want to parse lines
			// that will be filtered out.
			q := &query{
				statement: fmt.Sprintf(`{job="app"} |= "error" | label="value" | %s | level="debug"`, parser),
				start:     3600,
				end:       7200,
				direction: logproto.BACKWARD,
				limit:     1000,
			}

			plan, err := BuildPlan(context.Background(), q)
			require.NoError(t, err)
			t.Logf("\n%s\n", plan.String())

			// Expected behavior - PARSE should happen after filters that don't need parsed fields
			expected := strings.Replace(`%1 = EQ label.job "app"
%2 = MATCH_STR builtin.message "error"
%3 = EQ ambiguous.label "value"
%4 = MAKETABLE [selector=%1, predicates=[%2, %3], shard=0_of_1]
%5 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = LT builtin.timestamp 1970-01-01T02:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = SELECT %8 [predicate=%2]
%10 = SELECT %9 [predicate=%3]
%11 = {PARSE_STATEMENT}
%12 = PROJECT %10 [mode=*E, expr=%11]
%13 = EQ ambiguous.level "debug"
%14 = SELECT %12 [predicate=%13]
%15 = TOPK %14 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%16 = LOGQL_COMPAT %15
RETURN %16
`, "{PARSE_STATEMENT}", statement, 1)

			require.Equal(t, expected, plan.String(), "Operations should be in the correct order: LineFilter before Parse, LabelFilter after Parse")

		}
	})

	t.Run("preserves operation order in metric query with filters before and after projection with parse operation", func(t *testing.T) {
		parsers := map[string]string{
			`json`:                     `PARSE_JSON(builtin.message, [], false, false)`,
			`logfmt`:                   `PARSE_LOGFMT(builtin.message, [], false, false)`,
			`regexp "(?P<level>\\w+)"`: `PARSE_REGEXP(builtin.message, "(?P<level>\w+)")`,
		}

		for parser, statement := range parsers {
			// Test that filters before logfmt parse are applied before parsing in metric queries too
			q := &query{
				statement: fmt.Sprintf(`sum by (level) (count_over_time({job="app"} |= "error" | label="value" | %s | level="debug" [5m]))`, parser),
				start:     3600,
				end:       7200,
				interval:  5 * time.Minute,
			}

			plan, err := BuildPlan(context.Background(), q)
			require.NoError(t, err)
			t.Logf("\n%s\n", plan.String())

			// Expected behavior - PARSE should happen after filters that don't need parsed fields
			// For metric queries: no SORT, but time range filters are applied earlier
			expected := strings.Replace(`%1 = EQ label.job "app"
%2 = MATCH_STR builtin.message "error"
%3 = EQ ambiguous.label "value"
%4 = MAKETABLE [selector=%1, predicates=[%2, %3], shard=0_of_1]
%5 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%6 = SELECT %4 [predicate=%5]
%7 = LT builtin.timestamp 1970-01-01T02:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = SELECT %8 [predicate=%2]
%10 = SELECT %9 [predicate=%3]
%11 = {PARSE_STATEMENT}
%12 = PROJECT %10 [mode=*E, expr=%11]
%13 = EQ ambiguous.level "debug"
%14 = SELECT %12 [predicate=%13]
%15 = RANGE_AGGREGATION %14 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%16 = VECTOR_AGGREGATION %15 [operation=sum, group_by=(ambiguous.level)]
%17 = LOGQL_COMPAT %16
RETURN %17
`, "{PARSE_STATEMENT}", statement, 1)

			require.Equal(t, expected, plan.String(), "Metric query should preserve operation order: filters before parse, then parse, then filters after parse")
		}
	})
}

func TestPlannerCreatesProjection(t *testing.T) {
	t.Run("drop labels", func(t *testing.T) {
		q := &query{
			statement: `{service_name="loki"} | drop level, detected_level`,
			start:     0,
			end:       3600,
			interval:  5 * time.Minute,
			direction: logproto.BACKWARD,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		expected := `%1 = EQ label.service_name "loki"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T01:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*D, expr=ambiguous.level, expr=ambiguous.detected_level]
%8 = TOPK %7 [sort_by=builtin.timestamp, k=0, asc=false, nulls_first=false]
%9 = LOGQL_COMPAT %8
RETURN %9
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("order of drop labels is preserved", func(t *testing.T) {
		q := &query{
			statement: `{service_name="loki"} | drop level, detected_level | json | drop __error__, __error_details__`,
			start:     0,
			end:       3600,
			interval:  5 * time.Minute,
			direction: logproto.BACKWARD,
		}

		plan, err := BuildPlan(context.Background(), q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		expected := `%1 = EQ label.service_name "loki"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T01:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*D, expr=ambiguous.level, expr=ambiguous.detected_level]
%8 = PARSE_JSON(builtin.message, [], false, false)
%9 = PROJECT %7 [mode=*E, expr=%8]
%10 = PROJECT %9 [mode=*D, expr=ambiguous.__error__, expr=ambiguous.__error_details__]
%11 = TOPK %10 [sort_by=builtin.timestamp, k=0, asc=false, nulls_first=false]
%12 = LOGQL_COMPAT %11
RETURN %12
`
		require.Equal(t, expected, plan.String())
	})
}

func TestBuildDeletePredicates(t *testing.T) {
	queryParams := &query{
		start: 1000, // Unix timestamp
		end:   2000,
	}

	// helper to create delete requests
	createDeleteRequest := func(selector string, startSec, endSec int64) *deletion.Request {
		return &deletion.Request{
			Selector: selector,
			Start:    startSec * 1e9, // Convert to nanoseconds
			End:      endSec * 1e9,
		}
	}

	tests := []struct {
		name          string
		deletes       []*deletion.Request
		rangeInterval time.Duration
		wantLen       int
		expectedPlan  string // Expected SSA plan string
	}{
		{
			name:          "simple selector",
			deletes:       []*deletion.Request{createDeleteRequest(`{job="test"}`, 1200, 1800)},
			rangeInterval: 0,
			wantLen:       1,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = LT builtin.timestamp 1970-01-01T00:20:00Z
%4 = GT builtin.timestamp 1970-01-01T00:30:00Z
%5 = OR %3 %4
%6 = EQ label.job "test"
%7 = NOT(%6)
%8 = OR %5 %7
%9 = SELECT %2 [predicate=%8]
RETURN %9
`,
		},
		{
			name: "selector with multiple matchers including regex",
			deletes: []*deletion.Request{
				createDeleteRequest(`{job="test", namespace=~"loki-.*", env!="dev"}`, 1200, 1800),
			},
			wantLen: 1,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = LT builtin.timestamp 1970-01-01T00:20:00Z
%4 = GT builtin.timestamp 1970-01-01T00:30:00Z
%5 = OR %3 %4
%6 = EQ label.job "test"
%7 = MATCH_RE label.namespace "loki-.*"
%8 = AND %6 %7
%9 = NEQ label.env "dev"
%10 = AND %8 %9
%11 = NOT(%10)
%12 = OR %5 %11
%13 = SELECT %2 [predicate=%12]
RETURN %13
`,
		},
		{
			name: "delete request with line filter regex",
			deletes: []*deletion.Request{
				createDeleteRequest(`{job="test"} |~ "error.*"`, 1200, 1800),
			},
			wantLen: 1,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = LT builtin.timestamp 1970-01-01T00:20:00Z
%4 = GT builtin.timestamp 1970-01-01T00:30:00Z
%5 = OR %3 %4
%6 = EQ label.job "test"
%7 = NOT(%6)
%8 = OR %5 %7
%9 = MATCH_RE builtin.message "error.*"
%10 = NOT(%9)
%11 = OR %8 %10
%12 = SELECT %2 [predicate=%11]
RETURN %12
`,
		},
		{
			name: "simple selector with multiple filters",
			deletes: []*deletion.Request{
				createDeleteRequest(`{job="test"} |= "error" | level="error" | env="prod"`, 1200, 1800),
			},
			wantLen: 1,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = LT builtin.timestamp 1970-01-01T00:20:00Z
%4 = GT builtin.timestamp 1970-01-01T00:30:00Z
%5 = OR %3 %4
%6 = EQ label.job "test"
%7 = NOT(%6)
%8 = OR %5 %7
%9 = MATCH_STR builtin.message "error"
%10 = EQ ambiguous.level "error"
%11 = AND %9 %10
%12 = EQ ambiguous.env "prod"
%13 = AND %11 %12
%14 = NOT(%13)
%15 = OR %8 %14
%16 = SELECT %2 [predicate=%15]
RETURN %16
`,
		},
		{
			name: "multiple delete requests",
			deletes: []*deletion.Request{
				createDeleteRequest(`{job="test1"}`, 200, 800), // outside range - no predicate
				createDeleteRequest(`{job="test1"}`, 1200, 1500),
				createDeleteRequest(`{job="test2"} |= "error"`, 1500, 1800),
				createDeleteRequest(`{job="test3"} | level="debug"`, 1300, 1700),
			},
			wantLen: 3,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = LT builtin.timestamp 1970-01-01T00:20:00Z
%4 = GT builtin.timestamp 1970-01-01T00:25:00Z
%5 = OR %3 %4
%6 = EQ label.job "test1"
%7 = NOT(%6)
%8 = OR %5 %7
%9 = SELECT %2 [predicate=%8]
%10 = LT builtin.timestamp 1970-01-01T00:25:00Z
%11 = GT builtin.timestamp 1970-01-01T00:30:00Z
%12 = OR %10 %11
%13 = EQ label.job "test2"
%14 = NOT(%13)
%15 = OR %12 %14
%16 = MATCH_STR builtin.message "error"
%17 = NOT(%16)
%18 = OR %15 %17
%19 = SELECT %9 [predicate=%18]
%20 = LT builtin.timestamp 1970-01-01T00:21:40Z
%21 = GT builtin.timestamp 1970-01-01T00:28:20Z
%22 = OR %20 %21
%23 = EQ label.job "test3"
%24 = NOT(%23)
%25 = OR %22 %24
%26 = EQ ambiguous.level "debug"
%27 = NOT(%26)
%28 = OR %25 %27
%29 = SELECT %19 [predicate=%28]
RETURN %29
`,
		},
		{
			name: "multiple delete requests with time range optimizations",
			deletes: []*deletion.Request{
				createDeleteRequest(`{job="covers"}`, 500, 2500),  // Covers entire range - no time predicate
				createDeleteRequest(`{job="before"}`, 500, 1500),  // Starts before - only end check
				createDeleteRequest(`{job="after"}`, 1500, 2500),  // Ends after - only start check
				createDeleteRequest(`{job="within"}`, 1200, 1800), // Within range - both checks
			},
			wantLen: 4,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = EQ label.job "covers"
%4 = NOT(%3)
%5 = SELECT %2 [predicate=%4]
%6 = GT builtin.timestamp 1970-01-01T00:25:00Z
%7 = EQ label.job "before"
%8 = NOT(%7)
%9 = OR %6 %8
%10 = SELECT %5 [predicate=%9]
%11 = LT builtin.timestamp 1970-01-01T00:25:00Z
%12 = EQ label.job "after"
%13 = NOT(%12)
%14 = OR %11 %13
%15 = SELECT %10 [predicate=%14]
%16 = LT builtin.timestamp 1970-01-01T00:20:00Z
%17 = GT builtin.timestamp 1970-01-01T00:30:00Z
%18 = OR %16 %17
%19 = EQ label.job "within"
%20 = NOT(%19)
%21 = OR %18 %20
%22 = SELECT %15 [predicate=%21]
RETURN %22
`,
		},
		{
			name:          "time range optimizations with non-zero range interval",
			rangeInterval: 300 * time.Second, // 5 minutes - extends query start from 1000 to 700
			deletes: []*deletion.Request{
				createDeleteRequest(`{job="covers"}`, 500, 2500), // Covers entire range (700-2000) - no time predicate
				createDeleteRequest(`{job="before"}`, 500, 1500), // Starts before qStart (700) - only end check
				createDeleteRequest(`{job="after"}`, 1500, 2500), // Ends after qEnd (2000) - only start check
				createDeleteRequest(`{job="within"}`, 800, 900),  // not skipped, this is within range
			},
			wantLen: 4,
			expectedPlan: `%1 = EQ label.env "prod"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = EQ label.job "covers"
%4 = NOT(%3)
%5 = SELECT %2 [predicate=%4]
%6 = GT builtin.timestamp 1970-01-01T00:25:00Z
%7 = EQ label.job "before"
%8 = NOT(%7)
%9 = OR %6 %8
%10 = SELECT %5 [predicate=%9]
%11 = LT builtin.timestamp 1970-01-01T00:25:00Z
%12 = EQ label.job "after"
%13 = NOT(%12)
%14 = OR %11 %13
%15 = SELECT %10 [predicate=%14]
%16 = LT builtin.timestamp 1970-01-01T00:13:20Z
%17 = GT builtin.timestamp 1970-01-01T00:15:00Z
%18 = OR %16 %17
%19 = EQ label.job "within"
%20 = NOT(%19)
%21 = OR %18 %20
%22 = SELECT %15 [predicate=%21]
RETURN %22
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicates, err := buildDeletePredicates(context.Background(), tt.deletes, queryParams, tt.rangeInterval)
			require.NoError(t, err)
			require.Len(t, predicates, tt.wantLen)

			actualPlan := buildPlanFromPredicates(t, predicates, nil)
			require.Equal(t, strings.TrimSpace(tt.expectedPlan), strings.TrimSpace(actualPlan))
		})
	}
}

func TestAlignStartToStepGrid(t *testing.T) {
	tests := []struct {
		name  string
		start time.Time
		step  time.Duration
		want  time.Time
	}{
		{
			name:  "zero step returns original start (instant query)",
			start: time.Unix(50, 0),
			step:  0,
			want:  time.Unix(50, 0),
		},
		{
			name:  "already aligned start is unchanged",
			start: time.Unix(100, 0),
			step:  100 * time.Second,
			want:  time.Unix(100, 0),
		},
		{
			name:  "non-aligned start is floored to step boundary",
			start: time.Unix(50, 0),
			step:  100 * time.Second,
			want:  time.Unix(0, 0),
		},
		{
			name:  "start just past a step boundary",
			start: time.Unix(101, 0),
			step:  100 * time.Second,
			want:  time.Unix(100, 0),
		},
		{
			name:  "sub-second step alignment",
			start: time.Unix(0, 750_000_000), // 750ms
			step:  500 * time.Millisecond,
			want:  time.Unix(0, 500_000_000), // 500ms
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := alignStartToStepGrid(tc.start, tc.step)
			require.Equal(t, tc.want.UnixNano(), got.UnixNano())
		})
	}
}

// TestRangeAggregationStepAlignment verifies that the logical planner aligns
// the start timestamp of a RangeAggregation node to the step grid, matching
// Prometheus's standard behaviour for range queries.
func TestRangeAggregationStepAlignment(t *testing.T) {
	// start=50s, step=100s → aligned start = 0s (floor(50/100)*100)
	q := &query{
		statement: `count_over_time({app="test"}[100s])`,
		start:     50,
		end:       250,
		step:      100 * time.Second,
		direction: logproto.BACKWARD,
		limit:     1000,
	}
	plan, err := BuildPlan(context.Background(), q)
	require.NoError(t, err)

	planStr := plan.String()
	// The RANGE_AGGREGATION node must show the aligned start (epoch 0 = 1970-01-01T00:00:00Z),
	// not the raw start (50s = 1970-01-01T00:00:50Z).
	require.Contains(t, planStr, "start_ts=1970-01-01T00:00:00Z",
		"RangeAggregation start should be aligned to step grid; plan:\n%s", planStr)
	require.Contains(t, planStr, "step=1m40s",
		"step should be preserved in the plan; plan:\n%s", planStr)
}

// Helper to build a plan from predicates and return the SSA string
func buildPlanFromPredicates(t *testing.T, predicates []Value, selector Value) string {
	t.Helper()

	// Use default selector if not provided
	if selector == nil {
		selector = &BinOp{
			Left:  NewColumnRef("env", types.ColumnTypeLabel),
			Right: NewLiteral("prod"),
			Op:    types.BinaryOpEq,
		}
	}

	makeTable := &MakeTable{
		Selector: selector,
		Shard:    noShard,
	}

	builder := NewBuilder(makeTable)
	for _, p := range predicates {
		builder = builder.Select(p)
	}

	plan, err := builder.ToPlan()
	require.NoError(t, err, "predicates should be valid for plan construction")
	require.NotNil(t, plan)

	return plan.String()
}
