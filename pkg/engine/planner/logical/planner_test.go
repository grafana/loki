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
	logicalPlan, err := BuildPlan(q)
	require.NoError(t, err)
	t.Logf("\n%s\n", logicalPlan.String())

	expected := `%1 = EQ label.cluster "prod"
%2 = MATCH_RE label.namespace "loki-.*"
%3 = AND %1 %2
%4 = MAKETABLE [selector=%3]
%5 = SORT %4 [column=builtin.timestamp, asc=true, nulls_first=false]
%6 = GTE builtin.timestamp 1000
%7 = SELECT %5 [predicate=%6]
%8 = LT builtin.timestamp 2000
%9 = SELECT %7 [predicate=%8]
%10 = MATCH_STR ambiguous.foo "bar"
%11 = MATCH_STR ambiguous.bar "baz"
%12 = OR %10 %11
%13 = SELECT %9 [predicate=%12]
%14 = MATCH_STR builtin.line "metric.go"
%15 = MATCH_STR builtin.line "foo"
%16 = AND %14 %15
%17 = NOT_MATCH_RE builtin.line "(a|b|c)"
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
			statement: `sum(rate({env="prod"}[1m]))`,
		},
	} {
		t.Run(tt.statement, func(t *testing.T) {
			q := &query{
				statement: tt.statement,
				start:     1000,
				end:       2000,
				direction: logproto.FORWARD,
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
