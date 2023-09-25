package queryrangebase

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/logql/syntax"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type timeSnap struct {
	log    log.Logger
	next   Handler
	limits Limits
}

func NewTimeSnapMiddleware(log log.Logger, limits Limits) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return timeSnap{
			log:    log,
			next:   next,
			limits: limits,
		}
	})
}

func (s timeSnap) Do(ctx context.Context, r Request) (Response, error) {
	user, err := tenant.TenantID(ctx)
	if err != nil {
		level.Error(util_log.WithContext(ctx, s.log)).Log("msg", "failed to extract tenant from request", "err", err)
		return s.next.Do(ctx, r)
	}
	// If snapping is disabled, bypass the middleware
	if !s.limits.SnapQueryTimestamps(ctx, user) {
		return s.next.Do(ctx, r)
	}

	start := time.Unix(0, r.GetStart())
	end := time.Unix(0, r.GetEnd())
	var snappedStart, snappedEnd time.Time

	duration := end.Sub(start)
	// if duration is not 0, then we have a range query
	if duration == 0 {
		// if duration is 0, then we have an instant query and we have extract the range from the query to use for snapping
		duration, err = extractRange(r.GetQuery())
		if err != nil {
			level.Warn(util_log.WithContext(ctx, s.log)).Log("msg", "error extracting range from instant query, bypassing TimeSnap middleware", "query", r.GetQuery(), "err", err)
			return s.next.Do(ctx, r)
		}
	}
	snappedStart, snappedEnd = snap(duration, start, end)
	level.Debug(util_log.WithContext(ctx, s.log)).Log("msg", "time snap middleware", "start", start, "end", end, "snapped_start", snappedStart, "snapped_end", snappedEnd, "query_duration", duration)
	return s.next.Do(ctx, r.WithStartEnd(snappedStart.UnixNano(), snappedEnd.UnixNano()))
}

func snap(duration time.Duration, start, end time.Time) (time.Time, time.Time) {
	// Grafana time picker values
	// Last 5 minutes
	// Last 15 minutes
	// Last 30 minutes
	// Last 1 hour
	// Last 3 hours
	// Last 6 hours
	// Last 12 hours
	// Last 24 hours
	// Last 2 days
	// Last 7 days
	// Last 30 days
	if duration >= time.Hour {
		start = start.Truncate(time.Minute)
		end = end.Truncate(time.Minute)
	} else if duration >= time.Minute {
		start = start.Truncate(time.Second)
		end = end.Truncate(time.Second)
	}
	return start, end
}

func extractRange(query string) (time.Duration, error) {
	expr, err := syntax.ParseSampleExpr(query)
	if err != nil {
		return 0, errors.Wrap(err, "failed to parse query")
	}
	var rng time.Duration
	expr.Walk(func(e interface{}) {
		switch e := e.(type) {
		case *syntax.RangeAggregationExpr:
			if e.Left != nil {
				rng = e.Left.Interval
			}
		}
	})
	if rng == 0 {
		return 0, errors.Wrap(err, "failed to extract [range] from instant query")
	}
	return rng, nil
}
