package logical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

type query struct {
	statement  string
	start, end int64
	direction  logproto.Direction
	limit      uint32
}

// Direction implements logql.Params.
func (q *query) Direction() logproto.Direction {
	return q.direction
}

// End implements logql.Params.
func (q *query) End() time.Time {
	return time.Unix(0, q.end)
}

// Start implements logql.Params.
func (q *query) Start() time.Time {
	return time.Unix(0, q.start)
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
	panic("unimplemented")
}

// Step implements logql.Params.
func (q *query) Step() time.Duration {
	panic("unimplemented")
}

var _ logql.Params = (*query)(nil)

func TestConvertAST_Success(t *testing.T) {
	q := &query{
		statement: `{cluster="prod", namespace=~"loki-.*"} | foo="bar" or bar="baz" |= "metric.go" |= "foo" or "bar" !~ "(a|b|c)" `,
		start:     1000,
		end:       2000,
		direction: logproto.FORWARD,
		limit:     1000,
	}
	logicalPlan, err := ConvertToLogicalPlan(q)
	require.NoError(t, err)
	t.Logf("\n%s\n", logicalPlan.String())

	expected := `%1 = EQ label.cluster, "prod"
%2 = MATCH_RE label.namespace, "loki-.*"
%3 = AND %1, %2
%4 = MAKE_TABLE [selector=%3]
%5 = MATCH_STR ambiguous.foo, "bar"
%6 = MATCH_STR ambiguous.bar, "baz"
%7 = OR %5, %6
%8 = SELECT %4 [predicate=%7]
%9 = MATCH_STR builtin.log, "metric.go"
%10 = MATCH_STR builtin.log, "foo"
%11 = AND %9, %10
%12 = NOT_MATCH_RE builtin.log, "(a|b|c)"
%13 = AND %11, %12
%14 = SELECT %8 [predicate=%13]
%15 = GTE builtin.timestamp, 1000
%16 = LT builtin.timestamp, 2000
%17 = AND %15, %16
%18 = SELECT %14 [predicate=%17]
%19 = SORT %18 [column=builtin.timestamp, asc=true, nulls_first=false]
%20 = limit %19 [skip=0, fetch=1000]
RETURN %20
`

	require.Equal(t, expected, logicalPlan.String())
}

func TestConvertAST_UnsupportedFeature(t *testing.T) {
	q := &query{
		statement: `{cluster="prod", namespace=~"loki-.*"} |= "metric.go" | retry > 2`,
		start:     1000,
		end:       2000,
		direction: logproto.FORWARD,
		limit:     1000,
	}
	logicalPlan, err := ConvertToLogicalPlan(q)
	require.Nil(t, logicalPlan)
	require.ErrorContains(t, err, "failed to convert AST into logical plan: not implemented: *log.NumericLabelFilter")
}
