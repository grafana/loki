package queryrange

import (
	"context"
	"sync"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// discardedResponseStats stores statistics from completed responses before
// workers deliver them. The collector removes each response after taking
// responsibility for recording its statistics. At finalization, the remaining
// statistics account for completed responses that were discarded.
// Key identifies an interval response or a shard within one fan-out operation.
type discardedResponseStats[Key comparable] struct {
	mu               sync.Mutex
	uncollectedStats map[Key]stats.Result
	finalized        bool
}

func (tracker *discardedResponseStats[Key]) recordStatistics(key Key, responseStats stats.Result) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.finalized {
		return
	}
	if tracker.uncollectedStats == nil {
		tracker.uncollectedStats = make(map[Key]stats.Result)
	}
	tracker.uncollectedStats[key] = responseStats
}

func (tracker *discardedResponseStats[Key]) markResponseCollected(key Key) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.uncollectedStats, key)
}

// closeAndAggregate stops registration and sums statistics from responses
// that were not collected, without waiting for requests still running.
// The boolean is true if any response was included, even if it scanned zero bytes.
func (tracker *discardedResponseStats[Key]) closeAndAggregate() (stats.Result, bool) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.finalized = true
	var uncollectedStats stats.Result
	hasUncollectedStats := len(tracker.uncollectedStats) > 0
	for _, responseStats := range tracker.uncollectedStats {
		// Discarded entries were scanned but were not returned to the client.
		responseStats.Summary.TotalEntriesReturned = 0
		uncollectedStats.Merge(responseStats)
	}
	tracker.uncollectedStats = nil
	return uncollectedStats, hasUncollectedStats
}

// discardedResponseUsageTracker accounts for completed interval responses that
// are discarded on early return or failure.
type discardedResponseUsageTracker struct {
	discardedResponseStats[*lokiResult]
}

func (tracker *discardedResponseUsageTracker) recordResponse(split *lokiResult, response queryrangebase.Response) {
	responseStats, hasStats := statisticsFromResponse(response)
	if !hasStats {
		return
	}
	// Count this interval once, as MergeSplit does in normal response merging.
	responseStats.Summary.Splits = 1
	tracker.recordStatistics(split, responseStats)
}

// finalizeResponseUsage preserves statistics from collected and uncollected
// responses. On error, callers must still pass their collected responses: this
// function records their usage in the partial-stats context before returning nil.
func (tracker *discardedResponseUsageTracker) finalizeResponseUsage(ctx context.Context, responses []queryrangebase.Response, err error) []queryrangebase.Response {
	uncollectedStats, hasUncollectedStats := tracker.closeAndAggregate()
	if err != nil {
		recordDiscardedResponseUsage(ctx, responses)
		if hasUncollectedStats {
			stats.JoinPartial(ctx, uncollectedStats)
		}
		return nil
	}
	if !hasUncollectedStats {
		return responses
	}
	// Log queries can succeed at the line limit without collecting every
	// interval response. Add usage from completed, uncollected intervals to
	// the collected log response's statistics, without adding their entries.
	// Process has already merged the collected log responses on this path.
	// Successful metric queries collect every response and need no adjustment.
	if len(responses) > 0 {
		if response, ok := responses[0].(*LokiResponse); ok {
			// Copy before changing statistics; the original response may be shared.
			responseWithUsage := *response
			responseWithUsage.Statistics.Merge(uncollectedStats)
			responses[0] = &responseWithUsage
		}
	}
	return responses
}

// recordDiscardedResponseUsage preserves usage before successful responses are
// discarded because their parent request failed.
func recordDiscardedResponseUsage(ctx context.Context, responses []queryrangebase.Response) {
	for _, response := range responses {
		if responseStats, hasStats := statisticsFromResponse(response); hasStats {
			stats.JoinPartial(ctx, responseStats)
		}
	}
}
