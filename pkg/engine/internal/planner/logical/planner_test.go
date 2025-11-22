package logical

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	logicalPlan, err := BuildPlan(q)
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

		logicalPlan, err := BuildPlan(q)
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

		logicalPlan, err := BuildPlan(q)
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

		logicalPlan, err := BuildPlan(q)
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
			// both vector and range aggregation are required
			statement: `count_over_time({env="prod"}[1m])`,
		},
		{
			statement: `sum(count_over_time({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `max(avg_over_time({env="prod"} | unwrap size [1m]))`,
		},
		{
			statement: `sum by (level) (rate({env="prod"}[1m]))`,
			expected:  true,
		},
		{
			statement: `avg by (level) (rate({env="prod"}[1m]))`,
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
			// both vector and range aggregation are required
			statement: `sum_over_time({env="prod"} | unwrap size [1m])`,
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
			// both vector and range aggregation are required
			statement: `max_over_time({env="prod"} | unwrap size [1m])`,
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

			logicalPlan, err := BuildPlan(q)
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

func TestPlannerCreatesCastOperationForUnwrap(t *testing.T) {
	t.Run("creates projection with unary cast operation instruction for metric query with unwrap duration", func(t *testing.T) {
		// Query with duration unwrap in a sum_over_time metric query
		q := &query{
			statement: `sum by (status) (sum_over_time({app="api"} | unwrap duration(response_time) [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the correct SSA representation
		// The UNWRAP should appear after SELECT operations but before RANGE_AGGREGATION
		expected := `%1 = EQ label.app "api"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*E, expr=CAST_DURATION(ambiguous.response_time)]
%8 = RANGE_AGGREGATION %7 [operation=sum, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%9 = VECTOR_AGGREGATION %8 [operation=sum, group_by=(ambiguous.status)]
%10 = LOGQL_COMPAT %9
RETURN %10
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

		plan, err := BuildPlan(q)
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
%7 = PROJECT %6 [mode=*E, expr=PARSE_LOGFMT(builtin.message, [], false, false)]
%8 = EQ ambiguous.level "error"
%9 = SELECT %7 [predicate=%8]
%10 = RANGE_AGGREGATION %9 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%11 = VECTOR_AGGREGATION %10 [operation=sum, group_by=(ambiguous.level)]
%12 = LOGQL_COMPAT %11
RETURN %12
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

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the SSA representation for log query
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*E, expr=PARSE_LOGFMT(builtin.message, [], false, false)]
%8 = EQ ambiguous.level "error"
%9 = SELECT %7 [predicate=%8]
%10 = TOPK %9 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%11 = LOGQL_COMPAT %10
RETURN %11
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

		plan, err := BuildPlan(q)
		require.NoError(t, err)

		// Assert against the correct SSA representation
		// Since there are no filters before logfmt, parse comes right after MAKETABLE
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]
%8 = EQ ambiguous.level "error"
%9 = SELECT %7 [predicate=%8]
%10 = RANGE_AGGREGATION %9 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%11 = VECTOR_AGGREGATION %10 [operation=sum, group_by=(ambiguous.level)]
%12 = LOGQL_COMPAT %11
RETURN %12
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

		plan, err := BuildPlan(q)
		require.NoError(t, err)

		// Assert against the SSA representation for log query
		expected := `%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]
%8 = EQ ambiguous.level "error"
%9 = SELECT %7 [predicate=%8]
%10 = TOPK %9 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%11 = LOGQL_COMPAT %10
RETURN %11
`
		require.Equal(t, expected, plan.String())
	})

	t.Run("preserves operation order with filters before and after projection with parse operation", func(t *testing.T) {
		// Test that filters before logfmt parse are applied before parsing,
		// and filters after logfmt parse are applied after parsing.
		// This is important for performance - we don't want to parse lines
		// that will be filtered out.
		q := &query{
			statement: `{job="app"} |= "error" | label="value" | logfmt | level="debug"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Expected behavior - PARSE should happen after filters that don't need parsed fields
		expected := `%1 = EQ label.job "app"
%2 = MATCH_STR builtin.message "error"
%3 = EQ ambiguous.label "value"
%4 = MAKETABLE [selector=%1, predicates=[%2, %3], shard=0_of_1]
%5 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = LT builtin.timestamp 1970-01-01T02:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = SELECT %8 [predicate=%2]
%10 = SELECT %9 [predicate=%3]
%11 = PROJECT %10 [mode=*E, expr=PARSE_LOGFMT(builtin.message, [], false, false)]
%12 = EQ ambiguous.level "debug"
%13 = SELECT %11 [predicate=%12]
%14 = TOPK %13 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%15 = LOGQL_COMPAT %14
RETURN %15
`

		require.Equal(t, expected, plan.String(), "Operations should be in the correct order: LineFilter before Parse, LabelFilter after Parse")
	})

	t.Run("preserves operation order in metric query with filters before and after projection with parse operation", func(t *testing.T) {
		// Test that filters before logfmt parse are applied before parsing in metric queries too
		q := &query{
			statement: `sum by (level) (count_over_time({job="app"} |= "error" | label="value" | logfmt | level="debug" [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Expected behavior - PARSE should happen after filters that don't need parsed fields
		// For metric queries: no SORT, but time range filters are applied earlier
		expected := `%1 = EQ label.job "app"
%2 = MATCH_STR builtin.message "error"
%3 = EQ ambiguous.label "value"
%4 = MAKETABLE [selector=%1, predicates=[%2, %3], shard=0_of_1]
%5 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%6 = SELECT %4 [predicate=%5]
%7 = LT builtin.timestamp 1970-01-01T02:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = SELECT %8 [predicate=%2]
%10 = SELECT %9 [predicate=%3]
%11 = PROJECT %10 [mode=*E, expr=PARSE_LOGFMT(builtin.message, [], false, false)]
%12 = EQ ambiguous.level "debug"
%13 = SELECT %11 [predicate=%12]
%14 = RANGE_AGGREGATION %13 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%15 = VECTOR_AGGREGATION %14 [operation=sum, group_by=(ambiguous.level)]
%16 = LOGQL_COMPAT %15
RETURN %16
`

		require.Equal(t, expected, plan.String(), "Metric query should preserve operation order: filters before parse, then parse, then filters after parse")
	})
}

func TestPlannerCreatesProjection(t *testing.T) {
	t.Run("", func(t *testing.T) {
		// Query with duration unwrap in a sum_over_time metric query
		q := &query{
			statement: `{service_name="loki"} | drop level,detected_level`,
			start:     0,
			end:       3600,
			interval:  5 * time.Minute,
			direction: logproto.BACKWARD,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		t.Logf("\n%s\n", plan.String())

		// Assert against the correct SSA representation
		// The UNWRAP should appear after SELECT operations but before RANGE_AGGREGATION
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
}
