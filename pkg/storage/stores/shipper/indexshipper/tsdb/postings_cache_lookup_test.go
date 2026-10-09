package tsdb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type gatedPostingsCache struct {
	mu sync.Mutex
	postingsTestCache
	release  chan struct{}
	requests atomic.Int64
}

func (c *gatedPostingsCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	c.requests.Add(1)
	select {
	case <-ctx.Done():
		return nil, nil, nil, ctx.Err()
	case <-c.release:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.postingsTestCache.Fetch(ctx, keys)
	}
}

type postingsLookupResult struct {
	postings index.Postings
	populate bool
}

func TestPostingsLookupDeduplication(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hit      bool
		refs     []storage.SeriesRef
		corrupt  bool
		fetchErr error
	}{
		{name: "hit", hit: true, refs: []storage.SeriesRef{1, 3, 7}},
		{name: "empty hit", hit: true},
		{name: "miss"},
		{name: "corrupt", corrupt: true},
		{name: "backend error", fetchErr: errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				backend := &gatedPostingsCache{
					postingsTestCache: postingsTestCache{fetchErr: tc.fetchErr, entries: map[string][]byte{}},
					release:           make(chan struct{}),
				}
				if tc.hit {
					encoded, err := encodePostings("key", tc.refs)
					require.NoError(t, err)
					backend.entries[cache.HashKey("key")] = encoded
				} else if tc.corrupt {
					backend.entries[cache.HashKey("key")] = []byte("corrupt")
				}
				c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
				defer c.Stop()
				const callers = 20
				results := make(chan postingsLookupResult, callers)
				for range callers {
					go func() {
						p, populate := c.fetchPostings(context.Background(), "key")
						results <- postingsLookupResult{p, populate}
					}()
				}
				synctest.Wait() // All callers are blocked on the fetch or its shared result.
				require.Equal(t, int64(1), backend.requests.Load())
				close(backend.release)
				synctest.Wait()
				require.Len(t, results, callers)
				for range callers {
					result := <-results
					require.Equal(t, !tc.hit && tc.fetchErr == nil, result.populate)
					if tc.hit {
						require.NotNil(t, result.postings)
						// Fully consuming one iterator must not advance any other caller's iterator.
						refs, err := index.ExpandPostings(result.postings)
						require.NoError(t, err)
						require.Equal(t, tc.refs, refs)
					} else {
						require.Nil(t, result.postings)
					}
				}
				if tc.corrupt {
					require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.decodeFailures), "decode is shared too")
				}
				c.fetchPostings(context.Background(), "key")
				require.Equal(t, int64(2), backend.requests.Load(), "a later request must fetch again")
			})
		})
	}
}

func TestPostingsLookupDifferentKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &gatedPostingsCache{release: make(chan struct{})}
		c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
		defer c.Stop()
		for _, key := range []string{"tenant-a/file", "tenant-b/file"} {
			go c.fetchPostings(context.Background(), key)
		}
		synctest.Wait()
		require.Equal(t, int64(2), backend.requests.Load(), "different keys must fetch independently")
		close(backend.release)
		synctest.Wait()
	})
}

func TestPostingsLookupCanceledQueryDoesNotCompute(t *testing.T) {
	backend := &postingsTestCache{}
	c := newPostingsCache(backend, "test", prometheus.NewRegistry(), log.NewNopLogger())
	defer c.Stop()
	reader := &postingsReaderSpy{}
	idx := NewTSDBIndex(reader)
	idx.setPostingsCache(c, "file")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := idx.forPostings(ctx, nil, 0, 10, nil, func(index.Postings) error {
		t.Error("canceled query must not invoke the callback")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, reader.calls)
	require.Zero(t, backend.fetches)
}
