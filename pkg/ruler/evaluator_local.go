package ruler

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

const EvalModeLocal = "local"

type LocalEvaluator struct {
	engine *logql.Engine
}

func NewLocalEvaluator(engine *logql.Engine) *LocalEvaluator {
	return &LocalEvaluator{engine: engine}
}

func (l *LocalEvaluator) Eval(ctx context.Context, qs string, now time.Time) (interface{}, error) {
	params := logql.NewLiteralParams(
		qs,
		now,
		now,
		0,
		0,
		logproto.FORWARD,
		0,
		nil,
	)

	q := l.engine.Query(params)
	return q.Exec(ctx)
}
