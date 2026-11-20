package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestIndexStatsHintsMiddleware(t *testing.T) {
	now := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	hints := func(ranges ...logproto.HintTimeRange) *logproto.HintTimeRanges {
		return &logproto.HintTimeRanges{Ranges: ranges}
	}
	recentHint := logproto.HintTimeRange{Start: ago(5 * time.Hour), End: ago(4 * time.Hour)}
	ingesters := ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour}

	for _, tc := range []struct {
		desc          string
		iqo           util.IngesterQueryOptions
		from, through time.Time
		hints         *logproto.HintTimeRanges
		wantCalled    bool
		wantHints     *logproto.HintTimeRanges
	}{
		{
			desc:       "no hints pass through",
			iqo:        ingesters,
			from:       ago(6 * time.Hour),
			through:    now,
			wantCalled: true,
		},
		{
			desc:       "the ingester window is added to the hints",
			iqo:        ingesters,
			from:       ago(6 * time.Hour),
			through:    now,
			hints:      hints(recentHint),
			wantCalled: true,
			wantHints:  hints(recentHint, logproto.HintTimeRange{Start: ago(3 * time.Hour), End: now}),
		},
		{
			desc:       "an empty set over the ingester window still reads the ingesters",
			iqo:        ingesters,
			from:       ago(6 * time.Hour),
			through:    ago(time.Hour),
			hints:      hints(),
			wantCalled: true,
			wantHints:  hints(logproto.HintTimeRange{Start: ago(3 * time.Hour), End: ago(time.Hour)}),
		},
		{
			desc:    "an empty set before the ingester window is zero without a call",
			iqo:     ingesters,
			from:    ago(10 * time.Hour),
			through: ago(6 * time.Hour),
			hints:   hints(),
		},
		{
			desc:    "hints outside the request are zero without a call",
			iqo:     ingesters,
			from:    ago(10 * time.Hour),
			through: ago(6 * time.Hour),
			hints:   hints(recentHint),
		},
		{
			desc:       "hints are clipped to the request",
			iqo:        ingesters,
			from:       ago(10 * time.Hour),
			through:    ago(4*time.Hour + 30*time.Minute),
			hints:      hints(recentHint),
			wantCalled: true,
			wantHints:  hints(logproto.HintTimeRange{Start: ago(5 * time.Hour), End: ago(4*time.Hour + 30*time.Minute)}),
		},
		{
			desc:       "store-only queriers add no window",
			iqo:        ingesterQueryOpts{queryStoreOnly: true, queryIngestersWithin: 3 * time.Hour},
			from:       ago(6 * time.Hour),
			through:    now,
			hints:      hints(recentHint),
			wantCalled: true,
			wantHints:  hints(recentHint),
		},
		{
			desc:       "queriers that read every range from the ingesters drop the hints",
			iqo:        ingesterQueryOpts{},
			from:       ago(6 * time.Hour),
			through:    now,
			hints:      hints(recentHint),
			wantCalled: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := &logproto.IndexStatsRequest{
				From:       model.TimeFromUnixNano(tc.from.UnixNano()),
				Through:    model.TimeFromUnixNano(tc.through.UnixNano()),
				Matchers:   `{foo="bar"}`,
				HintRanges: tc.hints,
			}
			var got *logproto.IndexStatsRequest
			next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				got = r.(*logproto.IndexStatsRequest)
				return &IndexStatsResponse{Response: &logproto.IndexStatsResponse{Chunks: 1}}, nil
			})

			resp, err := indexStatsHintsMiddleware(tc.iqo, func() time.Time { return now }).Wrap(next).Do(context.Background(), req)
			require.NoError(t, err)
			if !tc.wantCalled {
				require.Nil(t, got)
				require.Equal(t, emptyIndexStatsResponse(), resp)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.wantHints, got.HintRanges)
			require.Equal(t, tc.hints, req.HintRanges, "the request is not modified")
		})
	}
}
