package queryrange

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

func Test_PreservesCachingOptions(t *testing.T) {
	splitter := newDefaultSplitter(fakeLimits{}, nil)
	req := &LokiRequest{
		StartTs: time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC).Add(-3 * time.Hour),
		EndTs:   time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC),
		CachingOptions: resultscache.CachingOptions{
			Disabled: true,
		},
	}
	splitReqs := splitter.split(time.Now().UTC(), []string{"1"}, req, time.Hour)
	require.Len(t, splitReqs, 3)
	for _, splitReq := range splitReqs {
		require.Equal(t, splitReq.GetCachingOptions(), req.GetCachingOptions())
	}
}
