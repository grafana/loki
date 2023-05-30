// SPDX-License-Identifier: AGPL-3.0-only

package worker

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"go.uber.org/atomic"
)

// newExecutionContext returns a new execution context (execCtx) that wraps the input workerCtx and
// it used to run the querier's worker loop and execute queries.
// The purpose of the execution context is to gracefully shutdown queriers, waiting
// until inflight queries are terminated before the querier process exits.
//
// The caller must call execCancel() once done.
//
// How it's used:
//
// - The querier worker's loop run in a dedicated context, called the "execution context".
//
// - The execution context is canceled when the worker context gets cancelled (ie. querier is shutting down)
// and there's no inflight query execution. In case there's an inflight query, the execution context is canceled
// once the inflight query terminates and the response has been sent.
func newExecutionContext(workerCtx context.Context, logger log.Logger) (execCtx context.Context, execCancel context.CancelFunc, inflightQuery *atomic.Bool) {
	execCtx, execCancel = context.WithCancel(context.Background())
	inflightQuery = atomic.NewBool(false)

	go func() {
		// Wait until it's safe to cancel the execution context, which is when one of the following conditions happen:
		// - The worker context has been canceled and there's no inflight query
		// - The execution context itself has been explicitly canceled
		select {
		case <-workerCtx.Done():
			level.Debug(logger).Log("msg", "querier worker context has been canceled, waiting until there's no inflight query")
			//
			// TODO：a potential race condition
			//
			// summarizing Marco's answer.
			//   Question 1：
			//     isn't there a potential race condition between testing the flag and setting it in the querier loop?
			//     It could be false here but then the next query is received.
			//   Answer 1：
			//     When the querier shutdowns it's expected to cancel the context and so the call to request,
			//         err := c.Recv() (done in schedulerProcessor.querierLoop())
			//     to return error because of the canceled context (I mean the querier context, not the query execution context).
			//
			//   Question 2：
			//     Is there a race？
			//   Answer 2：
			//     Yes, there's a race between the call to c.Recv() and the sequent call to inflightQuery.Store(true),
			//     but the time window is very short and we ignored it in Mimir (all in all we want to gracefully handle the 99.9% of cases).
			//
			//  Question Q3：
			//    I was wondering if we can end up in a state were the query is inflight but we shut down.I guess it times out.
			//  Answer 3：
			//    I think that race condition still exists (I found it very hard to guarantee to never happen) but in practice should be very unlikely.
			for inflightQuery.Load() {
				select {
				case <-execCtx.Done():
					// In the meanwhile, the execution context has been explicitly canceled, so we should just terminate.
					return
				case <-time.After(100 * time.Millisecond):
					// Going to check it again.
				}
			}

			level.Debug(logger).Log("msg", "querier worker context has been canceled and there's no inflight query, canceling the execution context too")
			execCancel()
		case <-execCtx.Done():
			// Nothing to do. The execution context has been explicitly canceled.
		}
	}()

	return
}
