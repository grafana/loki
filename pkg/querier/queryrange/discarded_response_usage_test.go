package queryrange

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func logResponseWithScanUsage(bytesScanned int64) *LokiResponse {
	s := stats.Result{Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: bytesScanned}}}}
	s.ComputeSummary(time.Second, 0, 1)
	return &LokiResponse{Status: "success", Limit: 1, Direction: logproto.FORWARD, Statistics: s,
		Data: LokiData{ResultType: "streams", Result: []logproto.Stream{{Labels: `{app="test"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(bytesScanned, 0), Line: "line"}}}}}}
}

func TestProcessAccountsCompletedUnconsumedSplits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cancelQuery bool
		queryErr    error
	}{
		{name: "line limit"},
		{name: "failure", queryErr: errors.New("first split failed")},
		{name: "cancellation", cancelQuery: true, queryErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Make the collector wait for the first split while the second completes.
			first := &lokiResult{req: &LokiRequest{}, ch: make(chan *packedResp)}
			// A buffer lets the test observe that the second split has finished while
			// Process is still waiting for the first. Production channels remain unbuffered.
			second := &lokiResult{req: &LokiRequest{}, ch: make(chan *packedResp, 1)}

			// Release or fail the first split only after the second has registered usage.
			h := &splitByInterval{next: queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				if r == second.req {
					return logResponseWithScanUsage(200), nil
				}

				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				timeout := time.NewTimer(5 * time.Second)
				defer timeout.Stop()
				for len(second.ch) == 0 {
					select {
					case <-ticker.C:
					case <-timeout.C:
						return nil, errors.New("second split did not finish")
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}

				if tc.cancelQuery {
					cancel()
				}
				if tc.queryErr != nil {
					return nil, tc.queryErr
				}
				return logResponseWithScanUsage(100), nil
			})}

			partial, ctx := stats.NewPartialContext(parent)
			responses, err := h.Process(ctx, 2, 1, []*lokiResult{first, second}, 0)

			// On failure, only the second split returned statistics: expect 200 bytes
			// in partial stats. On line-limit success, expect 300 bytes but only the
			// first split's entry in the response.
			if tc.queryErr != nil {
				require.ErrorIs(t, err, tc.queryErr)
				require.Equal(t, int64(200), partial.Result().Summary.TotalBytesProcessed)
			} else {
				require.NoError(t, err)
				require.Len(t, responses, 1)
				response := responses[0].(*LokiResponse)
				require.Equal(t, int64(300), response.Statistics.Summary.TotalBytesProcessed)
				require.Equal(t, int64(1), response.Count(), "discarded split must not change returned entries")
				require.Equal(t, int64(1), response.Statistics.Summary.TotalEntriesReturned)

				require.Equal(t, int64(2), response.Statistics.Summary.Splits)
				require.Zero(t, partial.Result().Summary.TotalBytesProcessed, "successful usage must not also be partial usage")
			}
		})
	}
}

func TestDiscardedResponseUsageTrackerFinalizesAllErrorUsage(t *testing.T) {
	for _, withUncollectedResponse := range []bool{false, true} {
		for _, queryErr := range []error{errors.New("query failed"), context.Canceled} {
			t.Run(fmt.Sprintf("uncollected=%t/error=%s", withUncollectedResponse, queryErr), func(t *testing.T) {
				partial, ctx := stats.NewPartialContext(context.Background())
				tracker := &discardedResponseUsageTracker{}
				response := logResponseWithScanUsage(100)
				collected := &lokiResult{}
				tracker.recordResponse(collected, response)
				tracker.markResponseCollected(collected)
				want := int64(100)
				if withUncollectedResponse {
					tracker.recordResponse(&lokiResult{}, logResponseWithScanUsage(200))
					want += 200
				}
				result := tracker.finalizeResponseUsage(ctx, []queryrangebase.Response{response}, queryErr)
				require.Nil(t, result, "error returns must not expose successful sibling responses")
				require.Equal(t, want, partial.Result().Summary.TotalBytesProcessed)
				require.Equal(t, int64(100), response.Statistics.Summary.TotalBytesProcessed)
			})
		}
	}
}
