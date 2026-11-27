package querier

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/validation"
)

// statsRecordingStore records the hint ranges that reach Stats.
type statsRecordingStore struct {
	*storeMock
	calls int
	hints *logproto.HintTimeRanges
}

func (s *statsRecordingStore) Stats(_ context.Context, _ string, _, _ model.Time, hints *logproto.HintTimeRanges, _ ...*labels.Matcher) (*stats.Stats, error) {
	s.calls++
	s.hints = hints
	return &stats.Stats{Chunks: 1}, nil
}

func TestHandlerIndexStatsPassesHintRanges(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	through := time.Now().Truncate(time.Millisecond)
	from := through.Add(-time.Hour)
	ctx := user.InjectOrgID(context.Background(), "fake")

	for _, tc := range []struct {
		desc  string
		hints *logproto.HintTimeRanges
	}{
		{desc: "nil", hints: nil},
		{desc: "empty", hints: &logproto.HintTimeRanges{}},
		{desc: "with ranges", hints: &logproto.HintTimeRanges{Ranges: []logproto.HintTimeRange{
			{Start: from.Add(10 * time.Minute), End: from.Add(20 * time.Minute)},
		}}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			store := &statsRecordingStore{storeMock: newStoreMock()}
			q := &SingleTenantQuerier{chunkStore: store, limits: limits}
			handler := NewQuerierHandler(NewQuerierAPI(mockQuerierConfig(), q, limits, log.NewNopLogger()))

			resp, err := handler.Do(ctx, &logproto.IndexStatsRequest{
				From:       model.TimeFromUnixNano(from.UnixNano()),
				Through:    model.TimeFromUnixNano(through.UnixNano()),
				Matchers:   `{foo="bar"}`,
				HintRanges: tc.hints,
			})
			require.NoError(t, err)
			require.Equal(t, uint64(1), resp.(*queryrange.IndexStatsResponse).Response.Chunks)
			require.Equal(t, 1, store.calls)
			require.Equal(t, tc.hints, store.hints)
		})
	}
}
