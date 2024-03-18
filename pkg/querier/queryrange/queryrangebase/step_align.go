package queryrangebase

import (
	"context"
	"time"

	"github.com/go-kit/log/level"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// StepAlignMiddleware aligns the start and end of request to the step to
// improved the cacheability of the query results.
var StepAlignMiddleware = MiddlewareFunc(func(next Handler) Handler {
	return stepAlign{
		next: next,
	}
})

type stepAlign struct {
	next Handler
}

func (s stepAlign) Do(ctx context.Context, r Request) (Response, error) {
	level.Debug(util_log.Logger).Log("msg", "Inside step align")
	start := (r.GetStart().UnixMilli() / r.GetStep()) * r.GetStep()
	end := (r.GetEnd().UnixMilli() / r.GetStep()) * r.GetStep()
	return s.next.Do(ctx, r.WithStartEnd(time.UnixMilli(start), time.UnixMilli(end)))
}
