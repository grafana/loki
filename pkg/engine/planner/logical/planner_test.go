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

func TestConvertAST(t *testing.T) {
	q := &query{
		statement: `{cluster="prod", namespace=~"loki-.*"} | foo="bar" |= "metric.go" |= "org_id=1"`,
		start:     1000,
		end:       2000,
		direction: logproto.FORWARD,
		limit:     1000,
	}
	logicalPlan, ok, err := ConvertToLogicalPlan(q)
	require.NoError(t, err)
	require.True(t, ok)
	t.Logf("\n%s\n", logicalPlan.String())

	expected := `%1 = EQ label.cluster, "prod"
%2 = MATCH_RE label.namespace, "loki-.*"
%3 = AND %1, %2
%4 = MAKE_TABLE [selector=%3]
%5 = MATCH_STR ambiguous.foo, "bar"
%6 = SELECT %4 [predicate=%5]
%7 = MATCH_STR builtin.log, "metric.go"
%8 = MATCH_STR builtin.log, "org_id=1"
%9 = AND %7, %8
%10 = SELECT %6 [predicate=%9]
%11 = MATCH_STR builtin.log, "metric.go"
%12 = SELECT %10 [predicate=%11]
%13 = GTE builtin.timestamp, 1000
%14 = LT builtin.timestamp, 2000
%15 = AND %13, %14
%16 = SELECT %12 [predicate=%15]
%17 = SORT %16 [column=builtin.timestamp, asc=true, nulls_first=false]
%18 = limit %17 [skip=0, fetch=1000]
RETURN %18
`

	require.Equal(t, expected, logicalPlan.String())
}
