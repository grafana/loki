package logical

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	panic("unimplemented")
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
%4 = MAKETABLE [selector=%3, predicates=[%12, %18], shard=0_of_1]
%5 = SORT %4 [column=builtin.timestamp, asc=false, nulls_first=false]
%6 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%7 = SELECT %5 [predicate=%6]
%8 = LT builtin.timestamp 1970-01-01T02:00:00Z
%9 = SELECT %7 [predicate=%8]
%10 = EQ ambiguous.foo "bar"
%11 = EQ ambiguous.bar "baz"
%12 = OR %10 %11
%13 = SELECT %9 [predicate=%12]
%14 = MATCH_STR builtin.message "metric.go"
%15 = MATCH_STR builtin.message "foo"
%16 = AND %14 %15
%17 = NOT_MATCH_RE builtin.message "(a|b|c)"
%18 = AND %16 %17
%19 = SELECT %13 [predicate=%18]
%20 = LIMIT %19 [skip=0, fetch=1000]
RETURN %20
`

	require.Equal(t, expected, logicalPlan.String())

	var sb strings.Builder
	PrintTree(&sb, logicalPlan.Value())

	t.Logf("\n%s\n", sb.String())
}

func TestConvertAST_MetricQuery_Success(t *testing.T) {
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
%4 = MAKETABLE [selector=%3, predicates=[%9], shard=0_of_1]
%5 = GTE builtin.timestamp 1970-01-01T00:55:00Z
%6 = SELECT %4 [predicate=%5]
%7 = LT builtin.timestamp 1970-01-01T02:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = MATCH_STR builtin.message "metric.go"
%10 = SELECT %8 [predicate=%9]
%11 = RANGE_AGGREGATION %10 [operation=count, start_ts=1970-01-01T01:00:00Z, end_ts=1970-01-01T02:00:00Z, step=0s, range=5m0s]
%12 = VECTOR_AGGREGATION %11 [operation=sum, group_by=(ambiguous.level)]
RETURN %12
`

	require.Equal(t, expected, logicalPlan.String())

	var sb strings.Builder
	PrintTree(&sb, logicalPlan.Value())

	t.Logf("\n%s\n", sb.String())
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
			// both vector and range aggregation are required
			statement: `count_over_time({env="prod"}[1m])`,
		},
		{
			// group by labels are required
			statement: `sum(count_over_time({env="prod"}[1m]))`,
		},
		{
			// rate is not supported
			statement: `sum by (level) (rate({env="prod"}[1m]))`,
		},
		{
			// max is not supported
			statement: `max by (level) (count_over_time({env="prod"}[1m]))`,
		},
		{
			statement: `sum by (level) (count_over_time({env="prod"}[1m] offset 5m))`,
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
			} else {
				require.Nil(t, logicalPlan)
				require.ErrorContains(t, err, "failed to convert AST into logical plan")
			}
		})
	}
}

func TestPlannerCreatesParseFromLogfmt(t *testing.T) {
	t.Run("Planner creates Parse instruction from LogfmtParserExpr in metric query", func(t *testing.T) {
		// Query with logfmt parser followed by label filter in an instant metric query
		q := &query{
			statement: `sum by (level) (count_over_time({app="test"} | logfmt | level="error" [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Find the Parse instruction in the plan
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		// Assert Parse instruction was created
		require.NotNil(t, parseInst, "Parse instruction should be created for logfmt")
		require.Equal(t, ParserLogfmt, parseInst.Kind)
	})

	t.Run("creates Parse instruction for log query", func(t *testing.T) {
		q := &query{
			statement: `{app="test"} | logfmt | level="error"`,
			start:     3600,
			end:       7200,
			direction: logproto.BACKWARD,
			limit:     1000,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)

		// Find Parse instruction
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		require.NotNil(t, parseInst, "should create Parse instruction")
		require.Equal(t, ParserLogfmt, parseInst.Kind)
	})
}

func TestLogicalPlannerIdentifiesStreamLabels(t *testing.T) {
	tests := []struct {
		name             string
		query            string
		wantStreamLabels map[string]bool             // Labels from stream selector
		checkColumns     map[string]types.ColumnType // Columns to check and their expected types
	}{
		{
			name:  "simple log query with stream selector",
			query: `{app="test", env="prod"}`,
			wantStreamLabels: map[string]bool{
				"app": true,
				"env": true,
			},
			checkColumns: map[string]types.ColumnType{
				"app": types.ColumnTypeLabel, // Stream selector label
				"env": types.ColumnTypeLabel, // Stream selector label
			},
		},
		{
			name:  "metric query with stream labels and groupby",
			query: `sum by(app, region, status) (count_over_time({app="test", region="us"} | logfmt [5m]))`,
			wantStreamLabels: map[string]bool{
				"app":    true,
				"region": true,
			},
			checkColumns: map[string]types.ColumnType{
				"app":    types.ColumnTypeLabel,     // Stream selector label (used in groupby)
				"region": types.ColumnTypeLabel,     // Stream selector label (used in groupby)
				"status": types.ColumnTypeAmbiguous, // groupby label (not in stream selector)
			},
		},
		{
			name:  "metric query with stream labels and filter",
			query: `sum by(app, region) (count_over_time({app="test", region="us"} | logfmt | status="200" [5m]))`,
			wantStreamLabels: map[string]bool{
				"app":    true,
				"region": true,
			},
			checkColumns: map[string]types.ColumnType{
				"app":    types.ColumnTypeLabel,     // Stream selector label (used in groupby)
				"region": types.ColumnTypeLabel,     // Stream selector label (used in groupby)
				"status": types.ColumnTypeAmbiguous, // groupby label (not in stream selector)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create params for the query - use instant query
			now := time.Now()
			params, err := logql.NewLiteralParams(
				tt.query,
				now, // instant query at now
				now,
				0, // no step for instant query
				0,
				logproto.BACKWARD, // backward for log queries
				1000,
				nil,
				nil,
			)
			require.NoError(t, err)

			// Build the logical plan
			logicalPlan, err := BuildPlan(params)
			require.NoError(t, err)

			// Get the root value from the plan - need to extract from instructions
			require.NotNil(t, logicalPlan)
			require.GreaterOrEqual(t, len(logicalPlan.Instructions), 1)

			// Walk ALL instructions to find column references
			foundColumns := make(map[string]types.ColumnType)
			for _, inst := range logicalPlan.Instructions {
				switch node := inst.(type) {
				case *BinOp:
					// Check left side if it's a column reference
					if colRef, ok := node.Left.(*ColumnRef); ok {
						foundColumns[colRef.Ref.Column] = colRef.Ref.Type
					}
				case *VectorAggregation:
					// Check GroupBy columns
					for _, col := range node.GroupBy {
						foundColumns[col.Ref.Column] = col.Ref.Type
					}
				case *RangeAggregation:
					// Check PartitionBy columns
					for _, col := range node.PartitionBy {
						foundColumns[col.Ref.Column] = col.Ref.Type
					}
				}
			}

			// Verify the expected columns have the right types
			for colName, expectedType := range tt.checkColumns {
				actualType, found := foundColumns[colName]
				require.True(t, found, "Column %s not found in plan", colName)
				require.Equal(t, expectedType, actualType, "Column %s has wrong type", colName)
			}
		})
	}
}
