package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// RouterConfig configures sending queries to a separate engine.
type RouterConfig struct {
	Enabled bool

	Start time.Time     // Start time of the v2 engine
	Lag   time.Duration // Lag after which v2 engine has data

	// Validate function to check if the query is supported by the engine.
	Validate func(params logql.Params) bool

	// Handler to execute queries against the engine.
	Handler queryrangebase.Handler
}

// engineReqResp represents a request with its result channel
type engineReqResp struct {
	lokiResult
	isV2Engine bool
}

// engineRouter handles splitting queries between V1 and V2 engines
type engineRouter struct {
	v2Start time.Time
	v2Lag   time.Duration

	forMetricQuery bool

	v1Next queryrangebase.Handler
	v2Next queryrangebase.Handler

	checkV2 func(params logql.Params) bool

	merger queryrangebase.Merger

	logger log.Logger

	// Used for tests.
	clock quartz.Clock
}

// newEngineRouterMiddleware creates a middleware that splits and routes part of the query
// to v2 engine if the query is supported by it.
func newEngineRouterMiddleware(
	v2RouterConfig RouterConfig,
	v1Chain []queryrangebase.Middleware,
	merger queryrangebase.Merger,
	metricQuery bool,
	logger log.Logger,
) queryrangebase.Middleware {
	if v2RouterConfig.Handler == nil {
		panic("v2 engine handler cannot be nil")
	}

	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &engineRouter{
			v2Start:        v2RouterConfig.Start,
			v2Lag:          v2RouterConfig.Lag,
			v1Next:         queryrangebase.MergeMiddlewares(v1Chain...).Wrap(next),
			v2Next:         v2RouterConfig.Handler,
			checkV2:        v2RouterConfig.Validate,
			merger:         merger,
			logger:         logger,
			forMetricQuery: metricQuery,
			clock:          quartz.NewReal(),
		}
	})
}

func (e *engineRouter) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	start, end := e.v2Start, e.getEnd()
	// if query is entirely before or after v2 engine range, process using next handler.
	// ignore any boundary overlap, splitting requests that fall on bounary would result in tiny requests.
	if !e.isOverlappingV2range(r, start, end) {
		return e.v1Next.Do(ctx, r)
	}

	params, err := ParamsFromRequest(r)
	if err != nil {
		return nil, err
	}

	// Unsupported queries should be entirely executed by chunks.
	if !e.checkV2(params) {
		return e.v1Next.Do(ctx, r)
	}

	inputs := e.splitOverlapping(r, start, end)

	// for log queries, order the splits to return early on hitting limits.
	var limit uint32
	if !e.forMetricQuery && len(inputs) > 1 {
		r, ok := r.(*LokiRequest)
		if !ok {
			level.Error(e.logger).Log("msg", "engine router received unexpected request type", "type", fmt.Sprintf("%T", r))
			return nil, errors.New("engine router: unexpected request type")
		}

		limit = r.Limit

		if r.Direction == logproto.BACKWARD {
			slices.SortFunc(inputs, func(a, b *engineReqResp) int {
				return b.req.GetStart().Compare(a.req.GetStart())
			})
		} else {
			slices.SortFunc(inputs, func(a, b *engineReqResp) int {
				return a.req.GetStart().Compare(b.req.GetStart())
			})
		}
	}

	responses, err := e.process(ctx, inputs, limit)
	if err != nil {
		return nil, err
	}

	// Merge responses
	return e.merger.MergeResponse(responses...)
}

// whether the time range of the request overlaps with the time range of the v2 engine
func (e engineRouter) isOverlappingV2range(r queryrangebase.Request, start, end time.Time) bool {
	if !r.GetEnd().After(start) || !r.GetStart().Before(end) {
		return false
	}
	return true
}

// the end time of the v2 engine based on current timestamp and v2 engine lag
func (e engineRouter) getEnd() time.Time {
	return e.clock.Now().UTC().Add(-e.v2Lag)
}

// splitOverlapping breaks down the request into multiple ranges based on the V2 engine time range.
// It returns a max of 3 requests:
// - one for the range before V2 engine
// - one for the range overlapping V2 engine range
// - one for the range after V2 engine
func (e *engineRouter) splitOverlapping(r queryrangebase.Request, v2Start, v2End time.Time) []*engineReqResp {
	var (
		reqs []*engineReqResp

		stepNs = r.GetStep() * int64(time.Millisecond)
		gap    = time.Duration(stepNs)
	)

	// metric query splits are separated by a gap of 1 step. This is to ensure a step is included only in a single split.
	if !e.forMetricQuery {
		gap = 0
	}

	// align the ranges by step before splitting.
	start, end := alignStartEnd(stepNs, r.GetStart(), r.GetEnd())
	v2Start, v2End = alignStartEnd(stepNs, v2Start, v2End)

	// chunk req before V2 engine range
	if start.Before(v2Start) {
		reqs = append(reqs, &engineReqResp{
			lokiResult: lokiResult{
				req: r.WithStartEnd(start, v2Start.Add(-gap)), // add gap between splits
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

		// add gap after v2 query only if there is a chunk query after it.
		addSplitGap = true
	}

	if start.After(v2Start) {
		v2Start = start
	}
	if end.Before(v2End) {
		v2End = end
	} else if addSplitGap {
		v2End = v2End.Add(-gap)
	}

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
		resp.resp, resp.err = e.v2Next.Do(ctx, r.req)
		if isUnsupportedError(resp.err) {
			// Our router validates queries beforehand, but we fall back here for safety.
			level.Warn(e.logger).Log("msg", "falling back to v1 engine", "err", resp.err)
			resp.resp, resp.err = e.v1Next.Do(ctx, r.req)
		}
	} else {
		resp.resp, resp.err = e.v1Next.Do(ctx, r.req)
	}

	select {
	case <-ctx.Done():
		return
	case r.ch <- &resp:
	}
}

// isUnsupportedError checks whether the provided error corresponds to a
// [http.StatusNotImplemented] provided via [httpgrpc].
func isUnsupportedError(err error) bool {
	if err == nil {
		return false
	}

	resp, ok := httpgrpc.HTTPResponseFromError(err)
	return ok && resp.Code == http.StatusNotImplemented
}

// process executes the inputs in parallel and collects the responses.
func (e *engineRouter) process(ctx context.Context, inputs []*engineReqResp, limit uint32) ([]queryrangebase.Response, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errors.New("engine router process cancelled"))

	// Run all requests in parallel as we only get a max of 3 splits.
	for _, r := range inputs {
		go e.handleReq(ctx, r)
	}

	var responses []queryrangebase.Response
	var count int64
	for _, x := range inputs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case data := <-x.ch:
			if data.err != nil {
				return nil, data.err
			}

			responses = append(responses, data.resp)
			if limit > 0 {
				// exit early if limit has been reached
				if r, ok := data.resp.(*LokiResponse); ok {
					count += r.Count()
					if count >= int64(limit) {
						return responses, nil
					}
				}
			}

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
