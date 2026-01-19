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
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/validation"
)

// RetentionLimits provides access to tenant retention settings.
type RetentionLimits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
}

// RouterConfig configures sending queries to a separate engine.
type RouterConfig struct {
	Enabled bool

	Start time.Time     // Start time of the v2 engine
	Lag   time.Duration // Lag after which v2 engine has data

	RetentionLimits RetentionLimits

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

	retentionLimits RetentionLimits

	// Used for tests.
	clock quartz.Clock
}

// NewEngineRouterMiddleware creates a middleware that splits and routes part of the query
// to v2 engine if the query is supported by it.
func NewEngineRouterMiddleware(
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
			v2Start:         v2RouterConfig.Start,
			v2Lag:           v2RouterConfig.Lag,
			v1Next:          queryrangebase.MergeMiddlewares(v1Chain...).Wrap(next),
			v2Next:          v2RouterConfig.Handler,
			checkV2:         v2RouterConfig.Validate,
			merger:          merger,
			logger:          logger,
			forMetricQuery:  metricQuery,
			retentionLimits: v2RouterConfig.RetentionLimits,
			clock:           quartz.NewReal(),
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

	v2Start, v2End, useV2, err := e.adjustV2RangeForRetention(ctx, start, end)
	if err != nil {
		return nil, err
	}

	// do not use v2 engine as it fails to guarantee retention enforcement for this tenant.
	if !useV2 {
		return e.v1Next.Do(ctx, r)
	}

	inputs := e.splitOverlapping(r, v2Start, v2End)

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

// splitOverlapping creates a set of requests to be made. Returned requests are
// split up to the boundary of the v2 engine's time range (v2Start, v2End).
//
// splitOverlapping can create 1 to 3 splits depending on the overlap between
// the requested time range and the v2 engine's time range:
//
//   - A split for v2 is created for timestamps that overlap with v2Start and
//     v2End.
//   - If necessary, a pre-v2 split is created for timestamps before v2Start.
//   - If necessary, a post-v2 split is created for timestamps after v2End.
//
// If engineRouter is used for metric queries, the split for v2 will be aligned
// to the requested step: the start time will be shifted up to the next step
// boundary, and the end time will be rounded down to the previous step
// boundary.
func (e *engineRouter) splitOverlapping(r queryrangebase.Request, v2Start, v2End time.Time) []*engineReqResp {
	if !e.isOverlappingV2range(r, v2Start, v2End) {
		// This line should be unreachable, since [engineRouter.Do] already
		// makes sure there is at least some overlap. However, we keep this line
		// for safety so that logsSplit doesn't produce incorrect splits.
		return []*engineReqResp{{
			lokiResult: lokiResult{
				req: r,
				ch:  make(chan *packedResp),
			},
			isV2Engine: false,
		}}
	}

	// step determines alignment for splits.
	//
	// step is only used for metrics queries, which produce a set of samples
	// from r.GetStart() to r.GetEnd() incremented by step. For logs queries,
	// step is left as the zero value.
	var step time.Duration

	if e.forMetricQuery {
		// Get the step (in milliseconds) from the request.
		step = time.Duration(r.GetStep() * int64(time.Millisecond))
	}

	var (
		// Align v2Start and v2End to the requested step. This is a no-op for logs
		// queries, as the step is 0.
		alignedV2Start, alignedV2End = alignV2Range(step, v2Start, v2End)

		// v2SplitStart is clamped to the earliest timestamp which is still
		// serviceable by v2.
		v2SplitStart = maxTime(alignedV2Start, r.GetStart())

		// v2SplitEnd is clamped to the latest timestamp which is still
		// serviceable by v2.
		v2SplitEnd = minTime(alignedV2End, r.GetEnd())
	)

	var splits []*engineReqResp

	// We need a pre-v2 split if the request starts before v2.
	if r.GetStart().Before(v2SplitStart) {
		// For metric queries, two splits must not share an instant (determined
		// by step). To avoid this, we shift back the end by one step.
		//
		// To avoid this resulting in end < start, we clamp it to the starting
		// time.
		preV2SplitEnd := maxTime(r.GetStart(), v2SplitStart.Add(-step))

		splits = append(splits, &engineReqResp{
			lokiResult: lokiResult{
				req: r.WithStartEnd(r.GetStart(), preV2SplitEnd),
				ch:  make(chan *packedResp),
			},
			isV2Engine: false,
		})
	}

	// Create the v2 split.
	{
		splits = append(splits, &engineReqResp{
			lokiResult: lokiResult{
				req: r.WithStartEnd(v2SplitStart, v2SplitEnd),
				ch:  make(chan *packedResp),
			},
			isV2Engine: true,
		})
	}

	// We need a post-v2 split if v2 ends before the request ends.
	if v2SplitEnd.Before(r.GetEnd()) {
		// For metric queries, two splits must not share an instant (determined
		// by step). To avoid this, we shift the start forward by one step.
		//
		// To avoid this resulting in end < start, we clamp it to the ending
		// time.
		postV2SplitStart := minTime(v2SplitEnd.Add(step), r.GetEnd())

		// The start must be one step *after* v2SplitEnd.
		splits = append(splits, &engineReqResp{
			lokiResult: lokiResult{
				req: r.WithStartEnd(postV2SplitStart, r.GetEnd()),
				ch:  make(chan *packedResp),
			},
			isV2Engine: false,
		})
	}

	return splits
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
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

// alignV2Range aligns v2Start and v2End to the given step.
//
// - If v2Start is not aligned to step, it is rounded up to the next step boundary.
// - If v2End is not aligned to step, it is rounded down to the previous step boundary.
//
// If step is 0, no alignment is done.
func alignV2Range(step time.Duration, v2Start, v2End time.Time) (v2AlignedStart, v2AlignedEnd time.Time) {
	if step == 0 {
		// No alignment to perform.
		return v2Start, v2End
	}

	if mod := v2Start.UnixNano() % step.Nanoseconds(); mod != 0 {
		// Round the start time up.
		v2AlignedStart = v2Start.Add(step - time.Duration(mod))
	} else {
		// Start time is already aligned.
		v2AlignedStart = v2Start
	}

	if mod := v2End.UnixNano() % step.Nanoseconds(); mod != 0 {
		// Round the end time down to the previous step boundary.
		v2AlignedEnd = v2End.Add(-time.Duration(mod))
	} else {
		// End time is already aligned.
		v2AlignedEnd = v2End
	}

	return v2AlignedStart, v2AlignedEnd
}

// v2 engine does not support retention enforcement. The following table outlines how to handle retention period:
//
// Only global retention_period configured (no stream retention):
//   - retentionBoundary = now - retentionPeriod (data older than this is expired)
//
// ┌─────────────────────────────────────┬──────────────────────────────────────────────────────────────┐
// │ Retention Boundary Position         │ Handling Strategy                                            │
// ├─────────────────────────────────────┼──────────────────────────────────────────────────────────────┤
// │ retentionBoundary <= v2Start        │ Use split logic. All data in v2 range is within retention.   │
// ├─────────────────────────────────────┼──────────────────────────────────────────────────────────────┤
// │ v2Start < retentionBoundary < v2End │ Use split logic. Snap v2 engine start to retention boundary  │
// │                                     │ so the data out of retention is handled by v1 engine.        │
// ├─────────────────────────────────────┼──────────────────────────────────────────────────────────────┤
// │ retentionBoundary >= v2End          │ Route entirely to v1 as data in v2 range is out of retention │
// └─────────────────────────────────────┴──────────────────────────────────────────────────────────────┘
//
// Both global retention_period and per-stream retention configured:
//
//   - globalBoundary = now - globalRetentionPeriod
//   - streamBoundaryWorst = latest possible per-stream cutoff time
//     (i.e. the most restrictive stream retention boundary)
//
// Conservative routing rule when *any* stream retention is configured:
//
//   - If globalBoundary > v2Start OR streamBoundaryWorst > v2Start:
//     route the entire query to v1 (avoid returning expired data or dropping valid data).
//
//   - Else (globalBoundary <= v2Start AND streamBoundaryWorst <= v2Start):
//     split as usual; v2 can execute its split as if there is no retention configured,
//     since the entire v2 range is guaranteed in-retention for all streams.
func (e *engineRouter) adjustV2RangeForRetention(ctx context.Context, v2Start, v2End time.Time) (time.Time, time.Time, bool, error) {
	if e.retentionLimits == nil {
		return v2Start, v2End, true, nil
	}

	// TODO: support for multi-tenant queries.
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return v2Start, v2End, false, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	var (
		smallestStreamRetention time.Duration
		hasStreamRetention      bool

		today           = e.clock.Now().UTC().Truncate(24 * time.Hour) // calculate retention boundary at day granularity.
		globalRetention = e.retentionLimits.RetentionPeriod(tenantID)
	)

	if sr := e.retentionLimits.StreamRetention(tenantID); len(sr) > 0 {
		hasStreamRetention = true
		for _, streamRetention := range sr {
			if smallestStreamRetention == 0 || time.Duration(streamRetention.Period) < smallestStreamRetention {
				smallestStreamRetention = time.Duration(streamRetention.Period)
			}
		}
	}

	globalBoundary := today.Add(-globalRetention)
	if hasStreamRetention {
		streamBoundary := today.Add(-smallestStreamRetention)

		// if either global or the most restrictive stream retention boundary is after v2 start,
		// conservatively route entirely to v1.
		if globalBoundary.After(v2Start) || streamBoundary.After(v2Start) {
			// TODO: consider more aggressive approach:
			//
			//	If streamBoundaryWorst falls inside the v2 range, we could still use v2 by extending the v1 portion:
			//	  v1 executes [queryStart, streamBoundaryWorst) to enforce retention,
			//	  v2 executes [max(v2Start, streamBoundaryWorst), v2End].
			return v2Start, v2End, false, nil
		}

		// if the most restrictive stream retention boundary & global boundary are before v2 start,
		// split as usual; the whole v2 split is expected to be in-retention.
		return v2Start, v2End, true, nil
	}

	if globalRetention <= 0 {
		return v2Start, v2End, true, nil
	}

	// retention bounday is not before v2 end, route entirely to v1.
	if !globalBoundary.Before(v2End) {
		return v2Start, v2End, false, nil
	}

	// retention boundary is inside v2 range, snap v2 start to retention boundary.
	// data outside of retention will be handled by v1 engine.
	if globalBoundary.After(v2Start) {
		v2Start = globalBoundary
	}

	// split as usual, the whole v2 split is expected to be in-retention.
	return v2Start, v2End, true, nil
}
