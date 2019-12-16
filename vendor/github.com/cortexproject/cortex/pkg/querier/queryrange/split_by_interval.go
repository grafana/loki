package queryrange

import (
	"context"
	"time"
)

// SplitByIntervalMiddleware creates a new Middleware that splits requests by a given interval.
func SplitByIntervalMiddleware(interval time.Duration, limits Limits, merger Merger) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return splitByInterval{
			next:     next,
			limits:   limits,
			merger:   merger,
			interval: interval,
		}
	})
}

type splitByInterval struct {
	next     Handler
	limits   Limits
	merger   Merger
	interval time.Duration
}

func (s splitByInterval) Do(ctx context.Context, r Request) (Response, error) {
	// First we're going to build new requests, one for each day, taking care
	// to line up the boundaries with step.
	reqs := splitQuery(r, s.interval)

	reqResps, err := DoRequests(ctx, s.next, reqs, s.limits)
	if err != nil {
		return nil, err
	}

	resps := make([]Response, 0, len(reqResps))
	for _, reqResp := range reqResps {
		resps = append(resps, reqResp.Response)
	}

	response, err := s.merger.MergeResponse(resps...)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func splitQuery(r Request, interval time.Duration) []Request {
	var reqs []Request
	for start := r.GetStart(); start < r.GetEnd(); start = nextIntervalBoundary(start, r.GetStep(), interval) + r.GetStep() {
		end := nextIntervalBoundary(start, r.GetStep(), interval)
		if end+r.GetStep() >= r.GetEnd() {
			end = r.GetEnd()
		}

		reqs = append(reqs, r.WithStartEnd(start, end))
	}
	return reqs
}

// Round up to the step before the next interval boundary.
func nextIntervalBoundary(t, step int64, interval time.Duration) int64 {
	msPerInterval := int64(interval / time.Millisecond)
	startOfNextInterval := ((t / msPerInterval) + 1) * msPerInterval
	// ensure that target is a multiple of steps away from the start time
	target := startOfNextInterval - ((startOfNextInterval - t) % step)
	if target == startOfNextInterval {
		target -= step
	}
	return target
}
