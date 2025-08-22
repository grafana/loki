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

func TestConvertStringHintsFunction(t *testing.T) {
	t.Run("converts int64 hint", func(t *testing.T) {
		stringHints := map[string]string{
			"bytes": TypeHintInt64,
		}

		result := convertStringHintsToNumericType(stringHints)

		require.NotNil(t, result)
		require.Contains(t, result, "bytes")
		require.Equal(t, NumericInt64, result["bytes"])
	})

	t.Run("converts float64 hint", func(t *testing.T) {
		stringHints := map[string]string{
			"response_time": TypeHintFloat64,
		}

		result := convertStringHintsToNumericType(stringHints)

		require.NotNil(t, result)
		require.Contains(t, result, "response_time")
		require.Equal(t, NumericFloat64, result["response_time"])
	})

	t.Run("converts duration hint to float64", func(t *testing.T) {
		stringHints := map[string]string{
			"duration_field": TypeHintDuration,
		}

		result := convertStringHintsToNumericType(stringHints)

		require.NotNil(t, result)
		require.Contains(t, result, "duration_field")
		require.Equal(t, NumericFloat64, result["duration_field"])
	})

	t.Run("handles multiple hints", func(t *testing.T) {
		stringHints := map[string]string{
			"bytes":         TypeHintInt64,
			"response_time": TypeHintFloat64,
			"duration":      TypeHintDuration,
		}

		result := convertStringHintsToNumericType(stringHints)

		require.NotNil(t, result)
		require.Len(t, result, 3)
		require.Equal(t, NumericInt64, result["bytes"])
		require.Equal(t, NumericFloat64, result["response_time"])
		require.Equal(t, NumericFloat64, result["duration"])
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		result := convertStringHintsToNumericType(nil)

		require.Nil(t, result)
	})

	t.Run("returns empty map for empty input", func(t *testing.T) {
		stringHints := map[string]string{}

		result := convertStringHintsToNumericType(stringHints)

		require.NotNil(t, result)
		require.Empty(t, result)
	})
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
		// The key collector should have identified that "level" needs to be extracted
		require.Contains(t, parseInst.RequestedKeys, "level")
	})
}

func TestCountOverTimeWithParsedFilter(t *testing.T) {
	t.Run("count_over_time should count only matching parsed values", func(t *testing.T) {
		// Test that count_over_time counts only logs where parsed field matches filter
		// Query: sum by (app) (count_over_time({app="test"} | logfmt | level="error" [5m]))
		// This should:
		// 1. Parse logfmt to extract "level" field
		// 2. Filter to only logs where level="error"
		// 3. Count those filtered logs over 5m windows
		q := &query{
			statement: `sum by (app) (count_over_time({app="test"} | logfmt | level="error" [5m]))`,
			start:     time.Now().Unix(),
			end:       time.Now().Unix(),
			direction: logproto.FORWARD,
			limit:     1000,
		}

		// Build the logical plan
		plan, err := BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Find the Parse instruction
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		// Assert Parse instruction extracts "level" field for filtering
		require.NotNil(t, parseInst, "Parse instruction should exist")
		require.Contains(t, parseInst.RequestedKeys, "level",
			"Parse should extract 'level' field for filtering")

		// Find Select instructions that should filter on parsed "level" field
		var foundLevelFilter bool
		for _, inst := range plan.Instructions {
			if sel, ok := inst.(*Select); ok {
				// Check if this Select filters on level="error"
				if binOp, ok := sel.Predicate.(*BinOp); ok {
					if colRef, ok := binOp.Left.(*ColumnRef); ok {
						if colRef.Name() == "ambiguous.level" {
							foundLevelFilter = true
							// Verify it's checking for "error"
							if lit, ok := binOp.Right.(*Literal); ok {
								require.Equal(t, "error", lit.Value(),
									"Filter should check level='error'")
							}
						}
					}
				}
			}
		}
		require.True(t, foundLevelFilter,
			"Should have a Select instruction filtering on parsed 'level' field")
	})

	t.Run("multiple parsed field filters should all be applied", func(t *testing.T) {
		// Test with multiple parsed field filters
		// Query: sum by (app) (count_over_time({app="test"} | logfmt | level="error" | method="GET" [5m]))
		// Note: Using string comparison since numeric filters are not implemented
		q := &query{
			statement: `sum by (app) (count_over_time({app="test"} | logfmt | level="error" | method="GET" [5m]))`,
			start:     time.Now().Unix(),
			end:       time.Now().Unix(),
			direction: logproto.FORWARD,
			limit:     1000,
		}

		// Build the logical plan
		plan, err := BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Find the Parse instruction
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		// Assert Parse instruction extracts both "level" and "method" fields
		require.NotNil(t, parseInst, "Parse instruction should exist")
		require.Contains(t, parseInst.RequestedKeys, "level",
			"Parse should extract 'level' field")
		require.Contains(t, parseInst.RequestedKeys, "method",
			"Parse should extract 'method' field")

		// Count the number of Select instructions that filter on parsed fields
		var levelFilterFound, methodFilterFound bool
		for _, inst := range plan.Instructions {
			if sel, ok := inst.(*Select); ok {
				if binOp, ok := sel.Predicate.(*BinOp); ok {
					if colRef, ok := binOp.Left.(*ColumnRef); ok {
						name := colRef.Name()
						switch name {
						case "ambiguous.level":
							levelFilterFound = true
						case "ambiguous.method":
							methodFilterFound = true
						}
					}
				}
			}
		}

		require.True(t, levelFilterFound, "Should have filter for 'level' field")
		require.True(t, methodFilterFound, "Should have filter for 'method' field")
	})

	t.Run("parsed filter with no aggregation grouping", func(t *testing.T) {
		// Test without groupBy - just sum of all counts
		// Query: sum(count_over_time({app="test"} | logfmt | level="error" [5m]))
		// Note: This should fail because current implementation requires groupBy
		q := &query{
			statement: `sum(count_over_time({app="test"} | logfmt | level="error" [5m]))`,
			start:     time.Now().Unix(),
			end:       time.Now().Unix(),
			direction: logproto.FORWARD,
			limit:     1000,
		}

		// This should fail as the current implementation requires groupBy
		_, err := BuildPlan(q)
		require.Error(t, err, "Should fail without groupBy labels")
		require.Contains(t, err.Error(), "unimplemented",
			"Should indicate this is not yet implemented")
	})
}

func TestVectorAggregationGroupsByParsedLabel(t *testing.T) {
	t.Run("vector aggregation should group by parsed string field", func(t *testing.T) {
		// Test that vector aggregation can group by a field extracted from logfmt parsing
		// Query: sum by (region) (count_over_time({app="test"} | logfmt | region!="" [5m]))
		// The "region" field comes from logfmt parsing, not from stream labels
		q := &query{
			statement: `sum by (region) (count_over_time({app="test"} | logfmt | region!="" [5m]))`,
			start:     time.Now().Unix(),
			end:       time.Now().Unix(),
			direction: logproto.FORWARD,
			limit:     1000,
		}

		// Build the logical plan
		plan, err := BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Find the Parse instruction
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		require.NotNil(t, parseInst, "Parse instruction should exist")
		require.Contains(t, parseInst.RequestedKeys, "region",
			"Parse should extract 'region' field for grouping")

		// Find the VectorAggregation instruction
		var vecAggInst *VectorAggregation
		for _, inst := range plan.Instructions {
			if va, ok := inst.(*VectorAggregation); ok {
				vecAggInst = va
				break
			}
		}

		// Assert VectorAggregation exists and groups by "region"
		require.NotNil(t, vecAggInst, "VectorAggregation instruction should exist")

		// Check that region is in the GroupBy columns
		foundRegion := false
		var actualNames []string
		for _, col := range vecAggInst.GroupBy {
			actualNames = append(actualNames, col.Name())
			if col.Name() == "ambiguous.region" {
				foundRegion = true
				break
			}
		}
		require.True(t, foundRegion,
			"VectorAggregation should group by 'ambiguous.region' column. Actual columns: %v", actualNames)
	})

	t.Run("vector aggregation should handle multiple parsed fields in groupBy", func(t *testing.T) {
		// Test grouping by multiple fields where some come from logfmt parsing
		// Query: sum by (app, region, env) (count_over_time({app="test"} | logfmt [5m]))
		// "app" is a stream label, "region" and "env" come from logfmt parsing
		q := &query{
			statement: `sum by (app, region, env) (count_over_time({app="test"} | logfmt [5m]))`,
			start:     time.Now().Unix(),
			end:       time.Now().Unix(),
			direction: logproto.FORWARD,
			limit:     1000,
		}

		// Build the logical plan
		plan, err := BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Find the Parse instruction
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		// Assert Parse instruction extracts the parsed fields (region, env)
		// but not the stream label (app)
		require.NotNil(t, parseInst, "Parse instruction should exist")
		require.Contains(t, parseInst.RequestedKeys, "region",
			"Parse should extract 'region' field")
		require.Contains(t, parseInst.RequestedKeys, "env",
			"Parse should extract 'env' field")
		// Note: "app" should NOT be in RequestedKeys as it's a stream label

		// Find the VectorAggregation instruction
		var vecAggInst *VectorAggregation
		for _, inst := range plan.Instructions {
			if va, ok := inst.(*VectorAggregation); ok {
				vecAggInst = va
				break
			}
		}

		// Assert VectorAggregation groups by all three fields
		require.NotNil(t, vecAggInst, "VectorAggregation instruction should exist")
		require.Len(t, vecAggInst.GroupBy, 3, "Should group by 3 columns")

		// Check that all fields are present
		groupByNames := make(map[string]bool)
		var actualNames []string
		for _, col := range vecAggInst.GroupBy {
			name := col.Name()
			groupByNames[name] = true
			actualNames = append(actualNames, name)
		}
		require.True(t, groupByNames["ambiguous.app"], "Should group by 'app'. Actual columns: %v", actualNames)
		require.True(t, groupByNames["ambiguous.region"], "Should group by 'region'. Actual columns: %v", actualNames)
		require.True(t, groupByNames["ambiguous.env"], "Should group by 'env'. Actual columns: %v", actualNames)
	})
}
