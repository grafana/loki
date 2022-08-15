package stats

import (
	"sync"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
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

func TestStatsBloom_Chunks(t *testing.T) {
	sb := BloomPool.Get()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(x int) {
			sb.AddChunk(model.Fingerprint(x%2), index.ChunkMeta{
				Checksum: uint32(x) % 4,
				KB:       1,
				Entries:  1,
			})
			wg.Done()
		}(i)
	}
	wg.Wait()

	require.Equal(t, 4, int(sb.stats.Chunks))
	require.Equal(t, 4<<10, int(sb.stats.Bytes))
	require.Equal(t, 4, int(sb.stats.Entries))
}
