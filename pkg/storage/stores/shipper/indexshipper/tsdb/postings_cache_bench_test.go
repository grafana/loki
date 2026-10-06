package tsdb

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// benchmarkPostingsCache serializes only cache access, not cache-miss computation.
// Concurrent misses can therefore duplicate computation just as in the real cache.
type benchmarkPostingsCache struct {
	postingsTestCache
	mu sync.Mutex
}

func (c *benchmarkPostingsCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.postingsTestCache.Fetch(ctx, keys)
}

func (c *benchmarkPostingsCache) Store(ctx context.Context, keys []string, bufs [][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.postingsTestCache.Store(ctx, keys, bufs)
}

func BenchmarkPostingsCacheShardReuse(b *testing.B) {
	for _, size := range []int{1024, 65536} {
		b.Run(fmt.Sprintf("matches=%d", size), func(b *testing.B) {
			series := make([]LoadableSeries, size)
			for n := range series {
				series[n] = LoadableSeries{Labels: labels.FromStrings("app", "api", "id", fmt.Sprint(n)), Chunks: index.ChunkMetas{{MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}}}
			}
			file := BuildIndex(b, b.TempDir(), series)
			b.Cleanup(func() { require.NoError(b, file.Close()) })
			reader := file.Index.(*TSDBIndex).reader
			matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "api")}
			for _, shards := range []uint32{2, 16} {
				for _, shared := range []bool{false, true} {
					for _, mode := range []string{"cold-one", "warm-one", "cold-all", "warm-all", "concurrent-cold-all"} {
						b.Run(fmt.Sprintf("shards=%d/shared=%t/%s", shards, shared, mode), func(b *testing.B) {
							backend := &benchmarkPostingsCache{}
							c := newPostingsCache(backend, "bench", prometheus.NewRegistry(), log.NewNopLogger())
							query := func(shard uint32) error {
								filter := index.NewShard(shard, shards)
								key := fmt.Sprintf("shard-%d", shard)
								var computeFilter index.FingerprintFilter = filter
								if shared {
									key = "all"
									computeFilter = nil
								}
								p, err := c.cachedPostings(context.Background(), key, func() (index.Postings, error) { return PostingsForMatchers(reader, computeFilter, matchers...) })
								if err != nil {
									return err
								}
								if shared {
									p = reader.ShardPostings(p, filter)
								}
								for p.Next() {
								}
								return p.Err()
							}
							for shard := uint32(0); shard < shards; shard++ {
								require.NoError(b, query(shard))
							}
							var encodedBytes int
							for _, entry := range backend.entries {
								encodedBytes += len(entry)
							}
							b.ReportAllocs()
							b.ResetTimer()
							for n := 0; n < b.N; n++ {
								if mode == "cold-one" || mode == "cold-all" || mode == "concurrent-cold-all" {
									backend.entries = nil
								}
								switch mode {
								case "cold-one", "warm-one":
									require.NoError(b, query(uint32(n)%shards))
								case "concurrent-cold-all":
									var wg sync.WaitGroup
									errs := make(chan error, shards)
									for shard := uint32(0); shard < shards; shard++ {
										wg.Add(1)
										go func(s uint32) { defer wg.Done(); errs <- query(s) }(shard)
									}
									wg.Wait()
									close(errs)
									for err := range errs {
										require.NoError(b, err)
									}
								default:
									for shard := uint32(0); shard < shards; shard++ {
										require.NoError(b, query(shard))
									}
								}
							}
							b.StopTimer()
							b.ReportMetric(float64(encodedBytes), "cache-bytes")
						})
					}
				}
			}
		})
	}
}
