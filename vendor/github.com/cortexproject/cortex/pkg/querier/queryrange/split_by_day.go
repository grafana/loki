package queryrange

import (
	"context"
	"time"

	"github.com/weaveworks/common/user"
)

const millisecondPerDay = int64(24 * time.Hour / time.Millisecond)

// SplitByDayMiddleware creates a new Middleware that splits requests by day.
func SplitByDayMiddleware(limits Limits) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return splitByDay{
			next:   next,
			limits: limits,
		}
	})
}

type splitByDay struct {
	next   Handler
	limits Limits
}

func (s splitByDay) Do(ctx context.Context, r *Request) (*APIResponse, error) {
	// First we're going to build new requests, one for each day, taking care
	// to line up the boundaries with step.
	reqs := splitQuery(r)

	reqResps, err := doRequests(ctx, s.next, reqs, s.limits)
	if err != nil {
		return nil, err
	}

	resps := make([]*APIResponse, 0, len(reqResps))
	for _, reqResp := range reqResps {
		resps = append(resps, reqResp.resp)
	}

	return mergeAPIResponses(resps)
}

func splitQuery(r *Request) []*Request {
	var reqs []*Request
	for start := r.Start; start < r.End; start = nextDayBoundary(start, r.Step) + r.Step {
		end := nextDayBoundary(start, r.Step)
		if end+r.Step >= r.End {
			end = r.End
		}

		reqs = append(reqs, &Request{
			Path:  r.Path,
			Start: start,
			End:   end,
			Step:  r.Step,
			Query: r.Query,
		})
	}
	return reqs
}

// Round up to the step before the next day boundary.
func nextDayBoundary(t, step int64) int64 {
	startOfNextDay := ((t / millisecondPerDay) + 1) * millisecondPerDay
	// ensure that target is a multiple of steps away from the start time
	target := startOfNextDay - ((startOfNextDay - t) % step)
	if target == startOfNextDay {
		target -= step
	}
	return target
}

type requestResponse struct {
	req  *Request
	resp *APIResponse
}

func doRequests(ctx context.Context, downstream Handler, reqs []*Request, limits Limits) ([]requestResponse, error) {
	userid, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// If one of the requests fail, we want to be able to cancel the rest of them.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Feed all requests to a bounded intermediate channel to limit parallelism.
	intermediate := make(chan *Request)
	go func() {
		for _, req := range reqs {
			intermediate <- req
		}
		close(intermediate)
	}()

	respChan, errChan := make(chan requestResponse), make(chan error)
	parallelism := limits.MaxQueryParallelism(userid)
	if parallelism > len(reqs) {
		parallelism = len(reqs)
	}
	for i := 0; i < parallelism; i++ {
		go func() {
			for req := range intermediate {
				resp, err := downstream.Do(ctx, req)
				if err != nil {
					errChan <- err
				} else {
					respChan <- requestResponse{req, resp}
				}
			}
		}()
	}

	resps := make([]requestResponse, 0, len(reqs))
	var firstErr error
	for range reqs {
		select {
		case resp := <-respChan:
			resps = append(resps, resp)
		case err := <-errChan:
			if firstErr == nil {
				cancel()
				firstErr = err
			}
		}
	}

	return resps, firstErr
}
