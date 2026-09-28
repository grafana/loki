package queryrange

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

func Test_PreservesCachingOptions(t *testing.T) {
	splitter := newDefaultSplitter(fakeLimits{}, nil)
	end := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
	req := &LokiRequest{
		StartTs: end.Add(-3 * time.Hour),
		EndTs:   end,
		CachingOptions: resultscache.CachingOptions{
			Disabled: true,
		},
		HintRanges: []logproto.HintTimeRange{
			{Start: end.Add(-150 * time.Minute), End: end.Add(-90 * time.Minute)},
			{Start: end.Add(-30 * time.Minute), End: end.Add(30 * time.Minute)},
		},
	}
	splitReqs := splitter.split(time.Now().UTC(), []string{"1"}, req, time.Hour)
	require.Len(t, splitReqs, 3)
	for _, splitReq := range splitReqs {
		require.Equal(t, splitReq.GetCachingOptions(), req.GetCachingOptions())
	}
	require.Equal(t, []logproto.HintTimeRange{{
		Start: end.Add(-150 * time.Minute),
		End:   end.Add(-2 * time.Hour),
	}}, splitReqs[0].(*LokiRequest).HintRanges)
	require.Equal(t, []logproto.HintTimeRange{{
		Start: end.Add(-2 * time.Hour),
		End:   end.Add(-90 * time.Minute),
	}}, splitReqs[1].(*LokiRequest).HintRanges)
	require.Equal(t, []logproto.HintTimeRange{{
		Start: end.Add(-30 * time.Minute),
		End:   end,
	}}, splitReqs[2].(*LokiRequest).HintRanges)
}
