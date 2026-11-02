package queryrange

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/logproto"
	"github.com/grafana/loki/v4/pkg/logql/model/stats"
	"github.com/grafana/loki/v4/pkg/querier/queryrange/queryrangebase"
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

			discardedWork, ctx := stats.NewDiscardedWorkContext(parent)
			result, err := h.Process(ctx, 2, 1, []*lokiResult{first, second}, 0)

			// On failure, only the second split returned statistics: expect 200 bytes
			// in discarded-work stats. On line-limit success, expect 300 bytes but only the
			// first split's entry in the response.
			if tc.queryErr != nil {
				require.ErrorIs(t, err, tc.queryErr)
				require.Nil(t, result)
				require.Equal(t, int64(200), discardedWork.Result().Summary.TotalBytesProcessed)
			} else {
				require.NoError(t, err)
				response := result.(*LokiResponse)
				require.Equal(t, int64(300), response.Statistics.Summary.TotalBytesProcessed)
				require.Equal(t, int64(1), response.Count(), "discarded split must not change returned entries")
				require.Equal(t, int64(1), response.Statistics.Summary.TotalEntriesReturned)

				require.Equal(t, int64(2), response.Statistics.Summary.Splits)
				require.Zero(t, discardedWork.Result().Summary.TotalBytesProcessed, "response statistics must not also enter the discarded-work context")
			}
		})
	}
}

func TestDiscardedResponseUsageTrackerFinalizesAllErrorUsage(t *testing.T) {
	for _, withUncollectedResponse := range []bool{false, true} {
		t.Run(fmt.Sprintf("uncollected=%t", withUncollectedResponse), func(t *testing.T) {
			discardedWork, ctx := stats.NewDiscardedWorkContext(context.Background())
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

			tracker.finalizeFailedResponseUsage(ctx, []queryrangebase.Response{response})
			require.Equal(t, want, discardedWork.Result().Summary.TotalBytesProcessed)
			require.Equal(t, int64(100), response.Statistics.Summary.TotalBytesProcessed)
		})
	}
}

func TestDiscardedResponseUsageTrackerFinalizesSuccessfulUsage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		discardedBytes []int64
		wantBytes      int64
		wantSplits     int64
	}{
		{name: "no discarded responses", wantBytes: 100, wantSplits: 2},
		{name: "discarded response with zero bytes", discardedBytes: []int64{0}, wantBytes: 100, wantSplits: 3},
		{name: "discarded response with scanned bytes", discardedBytes: []int64{200}, wantBytes: 300, wantSplits: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &discardedResponseUsageTracker{}
			for _, bytesScanned := range tc.discardedBytes {
				tracker.recordResponse(&lokiResult{}, logResponseWithScanUsage(bytesScanned))
			}

			// The input is already merged. Preserve its statistics and entries,
			// even when the caller shares this response with another consumer.
			response := logResponseWithScanUsage(100)
			response.Statistics.Summary.Splits = 2
			originalStats := response.Statistics

			result := tracker.finalizeSuccessfulResponseUsage(response).(*LokiResponse)
			require.Equal(t, tc.wantBytes, result.Statistics.Summary.TotalBytesProcessed)
			require.Equal(t, tc.wantSplits, result.Statistics.Summary.Splits)
			require.Equal(t, int64(1), result.Statistics.Summary.TotalEntriesReturned)
			require.Equal(t, response.Data, result.Data)
			require.Equal(t, originalStats, response.Statistics)
		})
	}
}
