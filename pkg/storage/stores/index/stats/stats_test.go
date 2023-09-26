package stats

import (
	"sync"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestStatsBloom_Stream(t *testing.T) {
	sb := BloomPool.Get()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(x int) {
			sb.AddStream(model.Fingerprint(x % 2))
			wg.Done()
		}(i)
	}
	wg.Wait()

	require.Equal(t, uint64(2), sb.stats.Streams)
}
