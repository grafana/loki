package ruler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
)

const EvalModeLocal = "local"

type LocalEvaluator struct {
	engine *logql.Engine
	logger log.Logger
}

func NewLocalEvaluator(engine *logql.Engine, logger log.Logger) (*LocalEvaluator, error) {
	if engine == nil {
		return nil, fmt.Errorf("given engine is nil")
	}

	return &LocalEvaluator{engine: engine, logger: logger}, nil
}

func (l *LocalEvaluator) Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error) {
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
	res, err := q.Exec(ctx)
	if err != nil {
		return nil, err
	}

	return &res, nil
}
