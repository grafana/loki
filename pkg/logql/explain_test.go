package logql

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestExplain(t *testing.T) {

	query := `topk(5, avg_over_time({app="loki"} |= "caller=metrics.go" | logfmt | unwrap bytes [5s]))`

	// TODO(karsten): Ideally the querier and downstreamer are not required
	// to create the step evaluators.
	querier := NewMockQuerier(4, nil)
	opts := EngineOpts{}
	regular := NewEngine(opts, querier, NoLimits, log.NewNopLogger())

	ctx := user.InjectOrgID(context.Background(), "fake")

	defaultEv := NewDefaultEvaluator(querier, 30*time.Second)
	downEv := &DownstreamEvaluator{Downstreamer: MockDownstreamer{regular}, defaultEvaluator: defaultEv}

	mapper := NewShardMapper(ConstantShards(4), nilShardMetrics)
	_, _, expr, err := mapper.Parse(query)
	require.NoError(t, err)

	params := LiteralParams{
		qs:    query,
		start: time.Unix(60, 0),
		end:   time.Unix(60, 0),
		limit: 1000,
	}

	ev, err := downEv.NewStepEvaluator(ctx, downEv, expr.(syntax.SampleExpr), params)
	require.NoError(t, err)

	tree := NewTree()
	ev.Explain(tree)

	expected :=
		`[topk,  by ()] VectorAgg
 └── Concat
      ├── VectorStep
      ├── ...
      └── VectorStep
`
	require.Equal(t, expected, tree.String())
}
