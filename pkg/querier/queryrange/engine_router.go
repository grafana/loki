package queryrange

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/pkg/errors"
)

// engineReqResp represents a request with its result channel
type engineReqResp struct {
	lokiResult
	isV2Engine bool
}

// engineRouter handles splitting queries between V1 and V2 engines
type engineRouter struct {
	v2EngineCfg engine.Config

	next    queryrangebase.Handler
	merger  queryrangebase.Merger
	v1Chain []queryrangebase.Middleware

	logger log.Logger
}

// NewEngineRouterMiddleware creates a middleware that splits and routes part of the query to v2 engine
// if the query is supported by it.
func NewEngineRouterMiddleware(
	v2EngineCfg engine.Config,
	v1Chain []queryrangebase.Middleware,
	merger queryrangebase.Merger,
	logger log.Logger,
) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &engineRouter{
			v2EngineCfg: v2EngineCfg,
			v1Chain:     v1Chain,
			merger:      merger,
			next:        next,
			logger:      logger,
		}
	})
}

// TODO:
// - apply limits for log queries, consider query direction.
// - handle very small splits
// - splits smaller than step?
func (e *engineRouter) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	v2Start, v2End := e.v2EngineCfg.ValidQueryRange()

	// if query is entirely before or after v2 engine range, process using next handler.
	// ignore any boundary overlap, splitting requests that fall on bounary would result in tiny requests.
	if !r.GetEnd().After(v2Start) || !r.GetStart().Before(v2End) {
		return queryrangebase.MergeMiddlewares(e.v1Chain...).Wrap(e.next).Do(ctx, r)
	}

	params, err := ParamsFromRequest(r)
	if err != nil {
		return nil, err
	}

	// Unsupported queries should be entirely executed by chunks.
	if !engine.IsQuerySupported(params) {
		return queryrangebase.MergeMiddlewares(e.v1Chain...).Wrap(e.next).Do(ctx, r)
	}

	inputs := e.splitOverlapping(r, v2Start, v2End)
	responses, err := e.process(ctx, inputs)
	if err != nil {
		return nil, err
	}

	// Merge responses
	return e.merger.MergeResponse(responses...)
}

// splitOverlapping breaks down the request into multiple ranges based on the V2 engine time range.
// It returns a max of 3 requests:
// - one for the range before V2 engine
// - one for the range overlapping V2 engine range
// - one for the range after V2 engine
func (e *engineRouter) splitOverlapping(r queryrangebase.Request, v2Start, v2End time.Time) []*engineReqResp {
	stepNs := r.GetStep() * int64(time.Millisecond)

	// align the ranges by step before splitting.
	start, end := alignStartEnd(stepNs, r.GetStart(), r.GetEnd())
	v2Start, v2End = alignStartEnd(stepNs, v2Start, v2End)

	// End time is exclusive for metric and log queries.
	// So the splits are allowed to overlap on the boundary.
	var reqs []*engineReqResp

	// chunk req before V2 engine range
	if start.Before(v2Start) {
		reqs = append(reqs, &engineReqResp{
			lokiResult: lokiResult{
				req: r.WithStartEnd(start, v2Start.Add(-time.Duration(stepNs))),
				ch:  make(chan *packedResp),
			},
			isV2Engine: false,
		})
	}

	addSplitGap := false
	// chunk req after V2 engine range
	if end.After(v2End) {
		reqs = append(reqs, &engineReqResp{
			lokiResult: lokiResult{
				req: r.WithStartEnd(v2End, end),
				ch:  make(chan *packedResp),
			},
			isV2Engine: false,
		})

		// add split gap between v2 query and the v1 query after it.
		addSplitGap = true
	}

	if start.After(v2Start) {
		v2Start = start
	}
	if end.Before(v2End) {
		v2End = end
	} else if addSplitGap {
		v2End = v2End.Add(-time.Duration(stepNs))
	}

	// TODO: req order is important for log queries with a limit.
	return append(reqs, &engineReqResp{
		lokiResult: lokiResult{
			req: r.WithStartEnd(v2Start, v2End),
			ch:  make(chan *packedResp),
		},
		isV2Engine: true,
	})
}

func (e *engineRouter) handleReq(ctx context.Context, r *engineReqResp) {
	var resp packedResp
	if r.isV2Engine {
		// TODO: Add handler for v2 engine.
		panic("V2 engine handler not implemented")
	}

	resp.resp, resp.err = queryrangebase.MergeMiddlewares(e.v1Chain...).Wrap(e.next).Do(ctx, r.req)
	select {
	case <-ctx.Done():
		return
	case r.ch <- &resp:
	}
}

// process executes the inputs in parallel and collects the responses.
func (e *engineRouter) process(ctx context.Context, inputs []*engineReqResp) ([]queryrangebase.Response, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errors.New("engine router process cancelled"))

	// Run all requests in parallel as we only get a max of 3 splits.
	for _, r := range inputs {
		go e.handleReq(ctx, r)
	}

	var responses []queryrangebase.Response
	for _, x := range inputs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case data := <-x.ch:
			if data.err != nil {
				return nil, data.err
			}
			responses = append(responses, data.resp)
		}
	}

	return responses, nil
}

// alignStartEnd aligns start and end times to step boundaries.
func alignStartEnd(stepNs int64, start, end time.Time) (time.Time, time.Time) {
	startNs := start.UnixNano()
	endNs := end.UnixNano()

	startNs -= startNs % stepNs // round down
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod // round up
	}

	return time.Unix(0, startNs), time.Unix(0, endNs)
}
