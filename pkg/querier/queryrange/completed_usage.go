package queryrange

import (
	"context"
	"sync"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// completedSplitUsage retains statistics independently of ordered response
// delivery. A completed split can otherwise block on its response channel until
// another split fills the line limit or fails, at which point its stats are lost.
// It deliberately does not wait for requests that are still running at finish.
type completedSplitUsage struct {
	mu      sync.Mutex
	pending map[*lokiResult]stats.Result
	closed  bool
}

func (u *completedSplitUsage) record(split *lokiResult, response queryrangebase.Response) {
	s, ok := statisticsFromResponse(response)
	if !ok {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return
	}
	if u.pending == nil {
		u.pending = make(map[*lokiResult]stats.Result)
	}
	u.pending[split] = s
}

func (u *completedSplitUsage) consumed(split *lokiResult) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.pending, split)
}

func (u *completedSplitUsage) finish() (stats.Result, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.closed = true
	var result stats.Result
	found := len(u.pending) > 0
	for _, s := range u.pending {
		// These entries were scanned but were not included in the client response.
		// Splits describes the partitioning of the returned response.
		s.Summary.Splits = 0
		s.Summary.TotalEntriesReturned = 0
		result.Merge(s)
	}
	u.pending = nil
	return result, found
}

// finishResponses preserves usage without adding discarded entries to a response.
func (u *completedSplitUsage) finishResponses(ctx context.Context, responses []queryrangebase.Response, err error) []queryrangebase.Response {
	pending, ok := u.finish()
	if !ok {
		return responses
	}
	if err != nil {
		stats.JoinPartial(ctx, pending)
		return responses
	}
	if len(responses) > 0 {
		if response, ok := responses[0].(*LokiResponse); ok {
			copy := *response
			copy.Statistics.Merge(pending)
			responses[0] = &copy
		}
	}
	return responses
}
