package queryrange

import (
	"context"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/tenant"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/weaveworks/common/httpgrpc"
	"net/http"
)

type splitByRange struct {
	logger log.Logger
	next   queryrangebase.Handler
	limits Limits
}

// SplitByRangeMiddleware creates a new Middleware that splits log requests by the range interval.
func SplitByRangeMiddleware(logger log.Logger, limits Limits) (queryrangebase.Middleware, error) {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &splitByRange{
			logger: log.With(logger, "middleware", "QueryShard.splitByRange"),
			next:   next,
			limits: limits,
		}
	}), nil
}

func (s *splitByRange) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	logger := util_log.WithContext(ctx, s.logger)

	userid, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	// TODO: new configuration value?
	interval := s.limits.QuerySplitDuration(userid)
	// if no interval configured, continue to the next middleware
	if interval == 0 {
		return s.next.Do(ctx, r)
	}

	mapper, err := logql.NewRangeVectorMapper(interval)
	if err != nil {
		return nil, err
	}

	noop, parsed, err := mapper.Parse(r.GetQuery())
	if err != nil {
		level.Warn(logger).Log("msg", "failed mapping AST", "err", err.Error(), "query", r.GetQuery())
		return nil, err
	}
	level.Debug(logger).Log("no-op", noop, "mapped", parsed.String())

	if noop {
		// the query cannot be sharded, so continue
		return s.next.Do(ctx, r)
	}

	// TODO: forward the resultant subqueries to the following middleware

	return nil, nil
}
