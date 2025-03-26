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
}
