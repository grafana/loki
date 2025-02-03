package ruler

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util"
)

const EvalModeLocal = "local"

type LocalEvaluator struct {
	engine *logql.Engine
	logger log.Logger

	// we don't want/need to log all the additional context, such as
	// caller=spanlogger.go:116 component=ruler evaluation_mode=remote method=ruler.remoteEvaluation.Query
	// in insights logs, so create a new logger
	insightsLogger log.Logger
}

func NewLocalEvaluator(engine *logql.Engine, logger log.Logger) (*LocalEvaluator, error) {
	if engine == nil {
		return nil, fmt.Errorf("given engine is nil")
	}

	return &LocalEvaluator{engine: engine, logger: logger, insightsLogger: log.NewLogfmtLogger(os.Stderr)}, nil
}

func (l *LocalEvaluator) Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error) {
	params, err := logql.NewLiteralParams(
		qs,
		now,
		now,
		0,
		0,
		logproto.FORWARD,
		0,
		nil,
		nil,
	)
	if err != nil {
		return nil, err
	}

	q := l.engine.Query(params)
	res, err := q.Exec(ctx)
	if err != nil {
		return nil, err
	}

	// Retrieve rule details from context
	ruleName, ruleType := GetRuleDetailsFromContext(ctx)

	level.Info(l.insightsLogger).Log("msg", "request timings", "insight", "true", "source", "loki_ruler", "rule_name", ruleName, "rule_type", ruleType, "total", res.Statistics.Summary.ExecTime, "total_bytes", res.Statistics.Summary.TotalBytesProcessed, "query_hash", util.HashedQuery(qs))
	return &res, nil
}
