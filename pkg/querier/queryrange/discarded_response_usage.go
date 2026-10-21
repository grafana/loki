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

// finalize closes the tracker and returns the sum of statistics from responses
// that were not collected, without waiting for requests still running. It clears
// the stored statistics and ignores subsequent registrations, unlike a snapshot.
// The boolean is true if any response was included, even if it scanned zero bytes.
func (tracker *discardedResponseStats[Key]) finalize() (stats.Result, bool) {
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

// finalizeSuccessfulResponseUsage closes the tracker and adds discarded log
// statistics to the already-merged response. Return the result without merging
// it again, which would reset its split count.
func (tracker *discardedResponseUsageTracker) finalizeSuccessfulResponseUsage(response queryrangebase.Response) queryrangebase.Response {
	uncollectedStats, hasUncollectedStats := tracker.finalize()
	if !hasUncollectedStats {
		return response
	}
	// Only log queries that reach their line limit can succeed with responses
	// still in the tracker. Add their statistics without adding their entries.
	if logResponse, ok := response.(*LokiResponse); ok {
		// Copy before changing statistics; the original response may be shared.
		responseWithUsage := *logResponse
		responseWithUsage.Statistics.Merge(uncollectedStats)
		return &responseWithUsage
	}
	return response
}

// finalizeFailedResponseUsage closes the tracker and records statistics from
// all collected responses and any completed responses still in the tracker.
// Call it before discarding responses on a collection or merge failure.
func (tracker *discardedResponseUsageTracker) finalizeFailedResponseUsage(ctx context.Context, responses []queryrangebase.Response) {
	uncollectedStats, hasUncollectedStats := tracker.finalize()
	recordDiscardedResponseUsage(ctx, responses)
	if hasUncollectedStats {
		stats.JoinDiscardedWork(ctx, uncollectedStats)
	}
}

// recordDiscardedResponseUsage preserves usage before successful responses are
// discarded because their parent request failed.
func recordDiscardedResponseUsage(ctx context.Context, responses []queryrangebase.Response) {
	for _, response := range responses {
		if responseStats, hasStats := statisticsFromResponse(response); hasStats {
			stats.JoinDiscardedWork(ctx, responseStats)
		}
	}
}
