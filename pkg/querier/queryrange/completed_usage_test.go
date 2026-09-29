package queryrange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func completedUsageResponse(bytes int64) *LokiResponse {
	s := stats.Result{Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: bytes}}}}
	s.ComputeSummary(time.Second, 0, 1)
	return &LokiResponse{Status: "success", Limit: 1, Direction: logproto.FORWARD, Statistics: s,
		Data: LokiData{ResultType: "streams", Result: []logproto.Stream{{Labels: `{app="test"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(bytes, 0), Line: "line"}}}}}}
}

func testProcessAccountsCompletedUnconsumedSplits(t *testing.T, engineRouting bool) {
	t.Helper()
	for _, outcome := range []string{"line limit", "failure", "cancellation"} {
		t.Run(map[bool]string{false: "interval/", true: "engine/"}[engineRouting]+outcome, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := &lokiResult{req: &LokiRequest{}, ch: make(chan *packedResp)}
			// A buffer lets the test observe that the second split has finished while
			// Process is still waiting for the first. Production channels remain unbuffered.
			second := &lokiResult{req: &LokiRequest{}, ch: make(chan *packedResp, 1)}
			sentinel := errors.New("first split failed")
			h := &splitByInterval{next: queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				if r == second.req {
					return completedUsageResponse(200), nil
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
				if outcome == "failure" {
					return nil, sentinel
				}
				if outcome == "cancellation" {
					cancel()
					return nil, context.Canceled
				}
				return completedUsageResponse(100), nil
			})}
			partial, ctx := stats.NewPartialContext(parent)
			var responses []queryrangebase.Response
			var err error
			if engineRouting {
				router := &engineRouter{v1Next: h.next, v2Next: h.next}
				responses, err = router.process(ctx, []*engineReqResp{{lokiResult: *first}, {lokiResult: *second, isV2Engine: true}}, 1)
			} else {
				responses, err = h.Process(ctx, 2, 1, []*lokiResult{first, second}, 0)
			}
			if outcome != "line limit" {
				expectedError := sentinel
				if outcome == "cancellation" {
					expectedError = context.Canceled
				}
				require.ErrorIs(t, err, expectedError)
				require.Equal(t, int64(200), partial.Result().Summary.TotalBytesProcessed)
			} else {
				require.NoError(t, err)
				require.Len(t, responses, 1)
				response := responses[0].(*LokiResponse)
				require.Equal(t, int64(300), response.Statistics.Summary.TotalBytesProcessed)
				require.Equal(t, int64(1), response.Count(), "discarded split must not change returned entries")
				require.Equal(t, int64(1), response.Statistics.Summary.TotalEntriesReturned)
				expectedSplits := int64(1)
				if engineRouting {
					expectedSplits = 0
				}
				require.Equal(t, expectedSplits, response.Statistics.Summary.Splits)
				require.Zero(t, partial.Result().Summary.TotalBytesProcessed, "successful usage must not also be partial usage")
			}
		})
	}
}

func TestCompletedSplitUsageDoesNotDoubleCountOrAcceptLateResults(t *testing.T) {
	u := &completedSplitUsage{}
	consumed, discarded := &lokiResult{}, &lokiResult{}
	u.record(consumed, completedUsageResponse(100))
	u.record(discarded, completedUsageResponse(200))
	u.consumed(consumed)
	result, ok := u.finish()
	require.True(t, ok)
	require.Equal(t, int64(200), result.Summary.TotalBytesProcessed)
	require.Zero(t, result.Summary.TotalEntriesReturned)
	u.record(&lokiResult{}, completedUsageResponse(300))
	_, ok = u.finish()
	require.False(t, ok, "a finalized response cannot be amended with late execution")
}
