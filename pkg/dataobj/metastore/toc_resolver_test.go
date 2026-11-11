package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/flagext"
)

var errTOCTest = errors.New("object store failure")

var tocTestWindow = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

func TestNewTOCResolver(t *testing.T) {
	path := TableOfContentsPath("tenant-a", tocTestWindow)
	data := newTOCBytes(t, "tenant-a", indexpointers.IndexPointer{Path: "index/a", StartTs: tocTestWindow, EndTs: tocTestWindow.Add(time.Hour)})

	t.Run("refreshes the ToCs in memory once per refresh interval while the service runs and stops refreshing when it stops", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			bucket := newTOCTestBucket()
			bucket.upload(t, path, data)

			cfg := testResolverConfig()
			cfg.ActiveWindow = time.Hour
			r := NewTOCResolver(bucket, cfg, nil, "", log.NewNopLogger(), prometheus.NewPedanticRegistry())
			r.jitter = func() time.Duration { return 0 }

			_, err := r.ReadTOC(t.Context(), path)
			require.NoError(t, err)

			require.NoError(t, services.StartAndAwaitRunning(t.Context(), r))
			time.Sleep(3*time.Minute + time.Second)
			synctest.Wait()
			require.Equal(t, 1+3, bucket.reads(path))

			require.NoError(t, services.StopAndAwaitTerminated(t.Context(), r))
			time.Sleep(3 * time.Minute)
			synctest.Wait()
			require.Equal(t, 1+3, bucket.reads(path), "a stopped resolver does not refresh")
		})
	})

	t.Run("fails to start when the refresh interval is not positive", func(t *testing.T) {
		cfg := testResolverConfig()
		cfg.RefreshInterval = 0
		r := NewTOCResolver(newTOCTestBucket(), cfg, nil, "", log.NewNopLogger(), prometheus.NewPedanticRegistry())

		require.EqualError(t, services.StartAndAwaitRunning(t.Context(), r), "the refresh interval of the ToC resolver must be positive")
	})

	t.Run("runs without refreshing when the resolver is disabled", func(t *testing.T) {
		bucket := newTOCTestBucket()
		bucket.upload(t, path, data)

		cfg := testResolverConfig()
		cfg.Enabled = false
		cfg.RefreshInterval = 0
		r := NewTOCResolver(bucket, cfg, nil, "", log.NewNopLogger(), prometheus.NewPedanticRegistry())

		_, err := r.ReadTOC(t.Context(), path)
		require.NoError(t, err)

		require.NoError(t, services.StartAndAwaitRunning(t.Context(), r))
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, 1, bucket.reads(path))
		require.NoError(t, services.StopAndAwaitTerminated(t.Context(), r))
	})
}

func TestTOCResolver_ReadTOC(t *testing.T) {
	var (
		pathA = TableOfContentsPath("tenant-a", tocTestWindow)
		dataA = newTOCBytes(t, "tenant-a", indexpointers.IndexPointer{Path: "index/a", StartTs: tocTestWindow, EndTs: tocTestWindow.Add(time.Hour)})
	)

	newResolver := func(t *testing.T) (*TOCResolver, *tocTestBucket, *testClock) {
		t.Helper()
		bucket := newTOCTestBucket()
		bucket.upload(t, pathA, dataA)
		clock := newTestClock()
		return newTestResolver(bucket, testResolverConfig(), nil, "", clock), bucket, clock
	}

	newLimitedResolver := func(t *testing.T, limit int, count int) (*TOCResolver, *tocTestBucket, []string) {
		t.Helper()
		cfg := testResolverConfig()
		cfg.MaxMemoryCacheBytes = flagext.ByteSize(limit)
		bucket := newTOCTestBucket()
		limitedPaths := make([]string, count)
		for i := range limitedPaths {
			limitedPaths[i] = TableOfContentsPath(fmt.Sprintf("tenant-%02d", i), tocTestWindow)
			bucket.upload(t, limitedPaths[i], dataA)
		}
		return newTestResolver(bucket, cfg, nil, "", newTestClock()), bucket, limitedPaths
	}
	oneSize := tocSize(TableOfContentsPath("tenant-00", tocTestWindow), &tocValue{data: dataA})

	t.Run("reads the object store for a miss and serves the next call from memory", func(t *testing.T) {
		r, bucket, _ := newResolver(t)

		got, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, dataA, got)
		require.Equal(t, 1, bucket.reads(pathA))

		got, err = r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, dataA, got)
		require.Equal(t, 1, bucket.reads(pathA))
	})

	t.Run("reads the object store on every call and keeps nothing when the resolver is disabled", func(t *testing.T) {
		bucket := newTOCTestBucket()
		bucket.upload(t, pathA, dataA)
		cfg := testResolverConfig()
		cfg.Enabled = false
		r := newTestResolver(bucket, cfg, nil, "", newTestClock())

		for range 3 {
			got, err := r.ReadTOC(t.Context(), pathA)
			require.NoError(t, err)
			require.Equal(t, dataA, got)
		}

		require.Equal(t, 3, bucket.reads(pathA))
		require.Nil(t, entryOf(r, pathA))
	})

	t.Run("returns a new ToC without storing it when it would take the size over the limit", func(t *testing.T) {
		r, bucket, limitedPaths := newLimitedResolver(t, 2*oneSize+oneSize/2, 3)

		for range 2 {
			for _, path := range limitedPaths {
				got, err := r.ReadTOC(t.Context(), path)
				require.NoError(t, err)
				require.Equal(t, dataA, got)
			}
		}

		require.NotNil(t, entryOf(r, limitedPaths[0]))
		require.NotNil(t, entryOf(r, limitedPaths[1]))
		require.Nil(t, entryOf(r, limitedPaths[2]), "the ToC that does not fit is not stored")
		require.Equal(t, 2*oneSize, r.tocsBytes)
		require.Equal(t, 2, bucket.reads(limitedPaths[2]), "a ToC that is not stored is read from object storage every time")
		require.Equal(t, 2.0, testutil.ToFloat64(r.metrics.evictions.WithLabelValues(evictionRejected)))
	})

	t.Run("makes one object store read for concurrent misses of one path", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, bucket, _ := newResolver(t)

			release := make(chan struct{})
			bucket.setBefore(holdUntil(release))

			const callers = 64
			results := make([][]byte, callers)
			errs := make([]error, callers)
			var wg sync.WaitGroup
			for i := range callers {
				wg.Go(func() {
					results[i], errs[i] = r.ReadTOC(t.Context(), pathA)
				})
			}

			synctest.Wait()
			close(release)
			wg.Wait()

			require.Equal(t, 1, bucket.reads(pathA))
			for i := range callers {
				require.NoError(t, errs[i])
				require.Equal(t, dataA, results[i])
			}
		})
	})

	t.Run("lets the shared read finish for a waiting caller when the caller that started it gives up", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, bucket, _ := newResolver(t)

			release := make(chan struct{})
			bucket.setBefore(func(ctx context.Context, _ string) error {
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})

			starterCtx, cancel := context.WithCancel(t.Context())
			starterErr := make(chan error, 1)
			go func() {
				_, err := r.ReadTOC(starterCtx, pathA)
				starterErr <- err
			}()
			synctest.Wait()

			type result struct {
				data []byte
				err  error
			}
			waiter := make(chan result, 1)
			go func() {
				data, err := r.ReadTOC(t.Context(), pathA)
				waiter <- result{data, err}
			}()
			synctest.Wait()

			cancel()
			require.ErrorIs(t, <-starterErr, context.Canceled)

			close(release)
			synctest.Wait()

			got := <-waiter
			require.NoError(t, got.err)
			require.Equal(t, dataA, got.data)
			require.Equal(t, 1, bucket.reads(pathA))
		})
	})

	t.Run("serves from memory after a clock that stepped back left a value stamped ahead", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		ahead := &tocValue{data: dataA, readAt: clock.Now().Add(time.Minute)}
		r.writeToLocalCache(pathA, ahead, clock.Now())

		for range 2 {
			got, err := r.ReadTOC(t.Context(), pathA)
			require.NoError(t, err)
			require.Equal(t, dataA, got)
		}
		require.Equal(t, 1, bucket.reads(pathA), "the first call reads again, and the second is served from memory")
	})

	t.Run("gives up on a hung read after the read timeout and reads again on the next call", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, bucket, _ := newResolver(t)
			bucket.setBefore(hang)

			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()

			start := time.Now()
			_, err := r.ReadTOC(ctx, pathA)
			require.EqualError(t, err, "reading the ToC "+pathA+" from object storage took longer than 10s")
			require.NotErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, tocReadTimeout, time.Since(start))

			bucket.setBefore(nil)
			got, err := r.ReadTOC(t.Context(), pathA)
			require.NoError(t, err)
			require.Equal(t, dataA, got)
		})
	})

	t.Run("reads the object store again for a ToC older than twice the refresh interval", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)

		clock.Advance(2 * time.Minute)
		_, err = r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, 1, bucket.reads(pathA), "a ToC exactly twice the refresh interval old is still served from memory")

		clock.Advance(time.Second)
		_, err = r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, 2, bucket.reads(pathA))
	})

	t.Run("serves the next call from memory after it reads a ToC that was stale", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)

		clock.Advance(2*time.Minute + time.Second)
		for range 2 {
			_, err = r.ReadTOC(t.Context(), pathA)
			require.NoError(t, err)
		}
		require.Equal(t, 2, bucket.reads(pathA))
	})

	t.Run("keeps serving the old bytes after a failed refresh until the ToC is stale, then returns the read error", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)

		bucket.setErr(errTOCTest)
		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		got, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, dataA, got)

		clock.Advance(time.Minute + time.Second)
		_, err = r.ReadTOC(t.Context(), pathA)
		require.ErrorIs(t, err, errTOCTest)
	})

	t.Run("returns no bytes and no error for a missing ToC and remembers it", func(t *testing.T) {
		r, bucket, _ := newResolver(t)
		missing := TableOfContentsPath("tenant-missing", tocTestWindow)

		for range 2 {
			got, err := r.ReadTOC(t.Context(), missing)
			require.NoError(t, err)
			require.Empty(t, got)
		}
		require.Equal(t, 1, bucket.reads(missing))
	})

	t.Run("returns an error for a ToC object that has no bytes and does not remember it", func(t *testing.T) {
		r, bucket, _ := newResolver(t)
		empty := TableOfContentsPath("tenant-empty", tocTestWindow)
		bucket.upload(t, empty, nil)

		_, err := r.ReadTOC(t.Context(), empty)
		require.EqualError(t, err, "metastore object "+empty+" is empty")

		bucket.upload(t, empty, dataA)
		got, err := r.ReadTOC(t.Context(), empty)
		require.NoError(t, err)
		require.Equal(t, dataA, got)
	})

	t.Run("returns a bucket error and does not remember it", func(t *testing.T) {
		r, bucket, _ := newResolver(t)

		bucket.setErr(errTOCTest)
		_, err := r.ReadTOC(t.Context(), pathA)
		require.ErrorIs(t, err, errTOCTest)

		bucket.setErr(nil)
		got, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, dataA, got)
		require.Equal(t, 2, bucket.reads(pathA))
	})

	t.Run("counts a miss, a hit and a stale lookup for a first read, a repeated read and a read of a stale ToC", func(t *testing.T) {
		r, _, clock := newResolver(t)
		lookups := func(result string) float64 {
			return testutil.ToFloat64(r.metrics.lookups.WithLabelValues(result))
		}

		_, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		_, err = r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		clock.Advance(3 * time.Minute)
		_, err = r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)

		require.Equal(t, 1.0, lookups(lookupMiss))
		require.Equal(t, 1.0, lookups(lookupHit))
		require.Equal(t, 1.0, lookups(lookupStale))
		require.Equal(t, 2.0, testutil.ToFloat64(r.metrics.loads.WithLabelValues(sourceBucket, triggerMiss)))
	})

	t.Run("does not read or write the shared cache when a ToC is not in memory", func(t *testing.T) {
		bucket := newTOCTestBucket()
		bucket.upload(t, pathA, dataA)
		mock := cache.NewMockCache()
		r := newTestResolver(bucket, testResolverConfig(), newTestMetadataCache(mock, 0), "", newTestClock())

		got, err := r.ReadTOC(t.Context(), pathA)
		require.NoError(t, err)
		require.Equal(t, dataA, got)
		require.Zero(t, mock.KeysRequested())
		require.Zero(t, mock.NumKeyUpdates())
	})

	t.Run("keeps the byte count exact and under the limit when requests and refreshes run at the same time", func(t *testing.T) {
		r, _, limitedPaths := newLimitedResolver(t, 5*oneSize, 30)
		clock := newTestClock()
		r.now = clock.Now

		var g errgroup.Group
		for worker := range 8 {
			g.Go(func() error {
				for i := range 200 {
					got, err := r.ReadTOC(t.Context(), limitedPaths[(worker+i)%len(limitedPaths)])
					if err != nil {
						return err
					}
					if !bytes.Equal(dataA, got) {
						return errors.New("a request returned other bytes")
					}
				}
				return nil
			})
		}
		g.Go(func() error {
			for range 50 {
				clock.Advance(10 * time.Second)
				r.refreshTOCs(t.Context())
			}
			return nil
		})

		require.NoError(t, g.Wait())

		r.tocsMx.RLock()
		defer r.tocsMx.RUnlock()
		var total int
		for path, e := range r.tocs {
			total += tocSize(path, e.value)
		}
		require.Equal(t, total, r.tocsBytes, "the byte count matches the ToCs in memory")
		require.LessOrEqual(t, r.tocsBytes, r.cfg.MaxMemoryCacheBytes.Val())
	})
}

func TestTOCResolver_readFromBucket(t *testing.T) {
	const path = "tocs/toc.toc"

	t.Run("stamps the value with the time before the read when the clock advances during the read", func(t *testing.T) {
		bucket := newTOCTestBucket()
		bucket.upload(t, path, []byte("toc"))
		clock := newTestClock()
		r := newTestResolver(bucket, testResolverConfig(), nil, "", clock)
		bucket.setBefore(func(context.Context, string) error {
			clock.Advance(5 * time.Second)
			return nil
		})
		start := clock.Now()

		got, err := r.readFromBucket(t.Context(), path)
		require.NoError(t, err)

		require.True(t, start.Equal(got.readAt))
	})
}

func TestTOCResolver_writeToLocalCache(t *testing.T) {
	path := TableOfContentsPath("tenant-a", tocTestWindow)

	t.Run("keeps the value that was read later when an older read finishes after it", func(t *testing.T) {
		clock := newTestClock()
		r := newTestResolver(newTOCTestBucket(), testResolverConfig(), nil, "", clock)

		r.writeToLocalCache(path, &tocValue{data: []byte("newer"), readAt: clock.Now()}, clock.Now())
		r.writeToLocalCache(path, &tocValue{data: []byte("older"), readAt: clock.Now().Add(-time.Second)}, clock.Now())

		require.Equal(t, []byte("newer"), entryOf(r, path).value.data)
	})

	t.Run("keeps the value that a refresh stored while a slow read ran", func(t *testing.T) {
		clock := newTestClock()
		r := newTestResolver(newTOCTestBucket(), testResolverConfig(), nil, "", clock)
		requestedAt := clock.Now()

		r.writeToLocalCache(path, &tocValue{data: []byte("refreshed"), readAt: requestedAt.Add(6 * time.Second)}, requestedAt)
		clock.Advance(8 * time.Second)
		r.writeToLocalCache(path, &tocValue{data: []byte("slow"), readAt: requestedAt}, requestedAt)

		require.Equal(t, []byte("refreshed"), entryOf(r, path).value.data)
	})
}

func TestTOCResolver_replaceLocked(t *testing.T) {
	const path = "tocs/path"

	var (
		now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
		r   = func() *TOCResolver {
			return newTestResolver(newTOCTestBucket(), testResolverConfig(), nil, "", newTestClock())
		}
		held = func(r *TOCResolver, readAt time.Time) *tocEntry {
			e := &tocEntry{value: &tocValue{data: []byte("held"), readAt: readAt}}
			r.tocs[path] = e
			r.tocsBytes = tocSize(path, e.value)
			return e
		}
		replace = func(r *TOCResolver, e *tocEntry, v *tocValue) {
			r.tocsMx.Lock()
			defer r.tocsMx.Unlock()
			r.replaceLocked(path, e, v, now)
		}
	)

	t.Run("keeps a held value that is stamped a little ahead of the clock", func(t *testing.T) {
		resolver := r()
		e := held(resolver, now.Add(tocMaxClockSkew))

		replace(resolver, e, &tocValue{data: []byte("old"), readAt: now})

		require.Equal(t, []byte("held"), e.value.data)
	})

	t.Run("replaces the value when the new size is exactly the limit", func(t *testing.T) {
		resolver := r()
		resolver.cfg.MaxMemoryCacheBytes = 100
		e := held(resolver, now)
		other := &tocEntry{value: &tocValue{data: bytes.Repeat([]byte("o"), 50), readAt: now}}
		resolver.tocs["tocs/other"] = other
		resolver.tocsBytes += tocSize("tocs/other", other.value)
		v := &tocValue{data: bytes.Repeat([]byte("n"), 31), readAt: now.Add(time.Second)}

		replace(resolver, e, v)

		require.Same(t, v, e.value)
		require.Equal(t, 100, resolver.tocsBytes)
	})
}

func TestTOCResolver_iterate(t *testing.T) {
	var (
		path = TableOfContentsPath("tenant-a", tocTestWindow)
		data = newTOCBytes(t, "tenant-a", indexpointers.IndexPointer{Path: "index/a", StartTs: tocTestWindow, EndTs: tocTestWindow.Add(time.Hour)})
	)

	newResolver := func(t *testing.T, jitter time.Duration) (*TOCResolver, *tocTestBucket) {
		t.Helper()
		bucket := newTOCTestBucket()
		bucket.upload(t, path, data)
		r := newTestResolver(bucket, testResolverConfig(), nil, "", newTestClock())
		r.jitter = func() time.Duration { return jitter }
		_, err := r.ReadTOC(t.Context(), path)
		require.NoError(t, err)
		return r, bucket
	}

	t.Run("runs a refresh pass only after the jitter", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const jitter = 5 * time.Second
			r, bucket := newResolver(t, jitter)

			done := make(chan error, 1)
			go func() { done <- r.iterate(t.Context()) }()

			synctest.Wait()
			require.Equal(t, 1, bucket.reads(path), "no pass runs while the jitter lasts")

			time.Sleep(jitter)
			synctest.Wait()
			require.Equal(t, 2, bucket.reads(path))
			require.NoError(t, <-done)
		})
	})

	t.Run("returns no error and runs no pass when the context ends during the jitter", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, bucket := newResolver(t, time.Minute)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- r.iterate(ctx) }()

			synctest.Wait()
			cancel()
			require.NoError(t, <-done)
			require.Equal(t, 1, bucket.reads(path))
		})
	})

	t.Run("draws default jitters that differ and stay below a tenth of the refresh interval", func(t *testing.T) {
		r := newTestResolver(newTOCTestBucket(), testResolverConfig(), nil, "", newTestClock())

		distinct := map[time.Duration]struct{}{}
		for range 1000 {
			jitter := r.jitter()
			require.Less(t, jitter, testResolverConfig().RefreshInterval/10)
			distinct[jitter] = struct{}{}
		}
		require.Greater(t, len(distinct), 100)
	})
}

func TestTOCResolver_refreshTOCs(t *testing.T) {
	var (
		window = tocTestWindow
		paths  = []string{
			TableOfContentsPath("tenant-a", window),
			TableOfContentsPath("tenant-b", window),
			TableOfContentsPath("tenant-c", window),
		}
		data = newTOCBytes(t, "tenant-a", indexpointers.IndexPointer{Path: "index/a", StartTs: window, EndTs: window.Add(time.Hour)})
	)

	newResolver := func(t *testing.T) (*TOCResolver, *tocTestBucket, *testClock) {
		t.Helper()
		bucket := newTOCTestBucket()
		for _, path := range paths {
			bucket.upload(t, path, data)
		}
		clock := newTestClock()
		return newTestResolver(bucket, testResolverConfig(), nil, "", clock), bucket, clock
	}

	type cachedEnv struct {
		bucket *tocTestBucket
		mock   cache.MockCache
		clock  *testClock
	}

	newCachedEnv := func(t *testing.T) cachedEnv {
		t.Helper()
		bucket := newTOCTestBucket()
		bucket.upload(t, paths[0], data)
		return cachedEnv{bucket: bucket, mock: cache.NewMockCache(), clock: newTestClock()}
	}

	newCachedResolver := func(e cachedEnv) *TOCResolver {
		return newTestResolver(e.bucket, testResolverConfig(), newTestMetadataCache(e.mock, 0), "", e.clock)
	}

	t.Run("reads again the paths requested within the active window and drops the others", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		clock.Advance(2 * time.Minute)
		_, err = r.ReadTOC(t.Context(), paths[1])
		require.NoError(t, err)
		clock.Advance(2*time.Minute + 30*time.Second)
		_, err = r.ReadTOC(t.Context(), paths[2])
		require.NoError(t, err)

		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		require.Nil(t, entryOf(r, paths[0]), "requested 5 m 30 s before the pass")
		require.NotNil(t, entryOf(r, paths[1]), "requested 3 m 30 s before the pass")
		require.NotNil(t, entryOf(r, paths[2]), "requested 1 m before the pass")
		require.Equal(t, 1, bucket.reads(paths[0]))
		require.Equal(t, 2, bucket.reads(paths[1]))
		require.Equal(t, 2, bucket.reads(paths[2]))
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.evictions.WithLabelValues(evictionInactive)))
	})

	t.Run("keeps a ToC that queries hit within the active window", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		for range 6 {
			clock.Advance(time.Minute)
			_, err = r.ReadTOC(t.Context(), paths[0])
			require.NoError(t, err)
			r.refreshTOCs(t.Context())
		}

		require.NotNil(t, entryOf(r, paths[0]))
		require.Equal(t, 7, bucket.reads(paths[0]), "one read for the miss and one for each of the six passes")
	})

	t.Run("replaces the bytes of a ToC with the bytes of the object that changed", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		updated := newTOCBytes(t, "tenant-a",
			indexpointers.IndexPointer{Path: "index/a", StartTs: window, EndTs: window.Add(time.Hour)},
			indexpointers.IndexPointer{Path: "index/a2", StartTs: window.Add(time.Hour), EndTs: window.Add(2 * time.Hour)},
		)
		bucket.upload(t, paths[0], updated)

		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		got, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		require.Equal(t, updated, got)
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.refreshes.WithLabelValues(refreshOK)))
	})

	t.Run("keeps the old bytes and counts the failure when a read fails", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		bucket.setErr(errTOCTest)
		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		got, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		require.Equal(t, data, got)
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.refreshes.WithLabelValues(refreshError)))
	})

	t.Run("keeps a missing ToC as empty bytes and counts it as not found", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		missing := TableOfContentsPath("tenant-missing", window)

		_, err := r.ReadTOC(t.Context(), missing)
		require.NoError(t, err)

		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		got, err := r.ReadTOC(t.Context(), missing)
		require.NoError(t, err)
		require.Empty(t, got)
		require.Equal(t, 2, bucket.reads(missing))
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.refreshes.WithLabelValues(refreshNotFound)))
	})

	t.Run("serves the bytes of a ToC that appears after it was missing", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		later := TableOfContentsPath("tenant-later", window)

		got, err := r.ReadTOC(t.Context(), later)
		require.NoError(t, err)
		require.Empty(t, got)

		bucket.upload(t, later, data)
		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		got, err = r.ReadTOC(t.Context(), later)
		require.NoError(t, err)
		require.Equal(t, data, got)
	})

	t.Run("drops the least recently requested ToCs until a tenth of the limit is free", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		for _, path := range paths {
			_, err := r.ReadTOC(t.Context(), path)
			require.NoError(t, err)
			clock.Advance(time.Second)
		}
		oneSize := tocSize(paths[0], entryOf(r, paths[0]).value)
		r.cfg.MaxMemoryCacheBytes = flagext.ByteSize(3*oneSize + 3)

		r.refreshTOCs(t.Context())

		require.Nil(t, entryOf(r, paths[0]), "the least recently requested ToC is dropped")
		require.NotNil(t, entryOf(r, paths[1]))
		require.NotNil(t, entryOf(r, paths[2]))
		require.Equal(t, 2*oneSize, r.tocsBytes)
		require.Equal(t, 1, bucket.reads(paths[0]), "a dropped ToC is not refreshed")
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.evictions.WithLabelValues(evictionSize)))
	})

	t.Run("drops a ToC whose new value is too large to keep and leaves the others", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		_, err := r.ReadTOC(t.Context(), paths[1])
		require.NoError(t, err)
		clock.Advance(time.Second)
		_, err = r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		oneSize := tocSize(paths[0], entryOf(r, paths[0]).value)
		r.cfg.MaxMemoryCacheBytes = flagext.ByteSize(3 * oneSize)
		bucket.upload(t, paths[0], bytes.Repeat([]byte("x"), 4*oneSize))
		clock.Advance(time.Minute)

		r.refreshTOCs(t.Context())

		require.Nil(t, entryOf(r, paths[0]))
		require.NotNil(t, entryOf(r, paths[1]), "the older ToC stays")
		require.Equal(t, oneSize, r.tocsBytes)
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.evictions.WithLabelValues(evictionOversized)))
	})

	t.Run("keeps the byte count exact when ToCs are replaced and dropped", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		for _, path := range paths[:2] {
			_, err := r.ReadTOC(t.Context(), path)
			require.NoError(t, err)
		}
		bigger := bytes.Repeat([]byte("x"), 3*len(data))
		bucket.upload(t, paths[0], bigger)
		clock.Advance(time.Minute)

		r.refreshTOCs(t.Context())

		require.Equal(t, tocSize(paths[0], &tocValue{data: bigger})+tocSize(paths[1], &tocValue{data: data}), r.tocsBytes)

		clock.Advance(10 * time.Minute)
		r.refreshTOCs(t.Context())

		require.Zero(t, r.tocsBytes)
	})

	t.Run("reads at most 8 ToCs at the same time, starting with the ones read longest ago", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const total = 20

			bucket := newTOCTestBucket()
			clock := newTestClock()
			r := newTestResolver(bucket, testResolverConfig(), nil, "", clock)

			ordered := make([]string, total)
			for i := range total {
				ordered[i] = TableOfContentsPath(fmt.Sprintf("tenant-%02d", total-1-i), window)
				bucket.upload(t, ordered[i], data)
				_, err := r.ReadTOC(t.Context(), ordered[i])
				require.NoError(t, err)
				clock.Advance(time.Second)
			}

			var (
				inflight, peak atomic.Int64
				mtx            sync.Mutex
				started        []string
			)
			release := make(chan struct{})
			bucket.setBefore(func(_ context.Context, name string) error {
				n := inflight.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				mtx.Lock()
				started = append(started, name)
				mtx.Unlock()
				<-release
				inflight.Add(-1)
				return nil
			})

			done := make(chan struct{})
			go func() {
				r.refreshTOCs(t.Context())
				close(done)
			}()

			synctest.Wait()
			require.Equal(t, int64(8), inflight.Load())
			mtx.Lock()
			require.ElementsMatch(t, ordered[:8], started, "the first 8 reads are the 8 ToCs read longest ago")
			mtx.Unlock()

			close(release)
			<-done

			require.Equal(t, int64(8), peak.Load())
			require.Equal(t, 2*total, bucket.totalReads())
		})
	})

	t.Run("gives up on a hung read after the read timeout and counts a refresh error", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r, bucket, clock := newResolver(t)

			_, err := r.ReadTOC(t.Context(), paths[0])
			require.NoError(t, err)

			bucket.setBefore(hang)
			clock.Advance(time.Minute)

			start := time.Now()
			r.refreshTOCs(t.Context())

			require.Equal(t, tocReadTimeout, time.Since(start))
			require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.refreshes.WithLabelValues(refreshError)))
		})
	})

	t.Run("marks a ToC as requested when a query reads it again after it went stale", func(t *testing.T) {
		r, _, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		clock.Advance(3 * time.Minute)
		_, err = r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		clock.Advance(2*time.Minute + 30*time.Second)
		r.refreshTOCs(t.Context())

		require.NotNil(t, entryOf(r, paths[0]), "the second query requested the ToC 2 m 30 s before the pass")
	})

	t.Run("does not bring back a ToC that left memory while its read ran", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		bucket.setBefore(func(_ context.Context, name string) error {
			r.tocsMx.Lock()
			defer r.tocsMx.Unlock()
			r.removeLocked(name, evictionInactive)
			return nil
		})
		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		require.Nil(t, entryOf(r, paths[0]))
	})

	t.Run("drops a ToC that every pass kept fresh once no query requested it within the active window", func(t *testing.T) {
		r, bucket, clock := newResolver(t)

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		for range 5 {
			clock.Advance(time.Minute)
			r.refreshTOCs(t.Context())
		}
		require.NotNil(t, entryOf(r, paths[0]), "the last query came exactly 5 m before the last pass")
		require.Equal(t, 6, bucket.reads(paths[0]))

		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())
		require.Nil(t, entryOf(r, paths[0]))
		require.Equal(t, 6, bucket.reads(paths[0]), "a dropped ToC is not read again")
	})

	t.Run("observes the duration of each refresh under its result", func(t *testing.T) {
		r, bucket, clock := newResolver(t)
		missing := TableOfContentsPath("tenant-missing", window)
		sampleCount := func(result string) uint64 {
			var m dto.Metric
			require.NoError(t, r.metrics.refreshDuration.WithLabelValues(result).(prometheus.Metric).Write(&m))
			return m.GetHistogram().GetSampleCount()
		}

		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		_, err = r.ReadTOC(t.Context(), missing)
		require.NoError(t, err)

		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())
		bucket.setErr(errTOCTest)
		clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())

		require.Equal(t, uint64(1), sampleCount(refreshOK))
		require.Equal(t, uint64(1), sampleCount(refreshNotFound))
		require.Equal(t, uint64(2), sampleCount(refreshError))
	})

	t.Run("counts its loads by source when it reads through a shared cache", func(t *testing.T) {
		e := newCachedEnv(t)
		r := newCachedResolver(e)
		_, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		e.clock.Advance(time.Minute)
		r.refreshTOCs(t.Context())
		e.clock.Advance(time.Second)
		r.refreshTOCs(t.Context())

		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.loads.WithLabelValues(sourceBucket, triggerRefresh)))
		require.Equal(t, 1.0, testutil.ToFloat64(r.metrics.loads.WithLabelValues(sourceCache, triggerRefresh)))
	})

	t.Run("keeps the held bytes when the cached value was read earlier", func(t *testing.T) {
		e := newCachedEnv(t)
		r := newCachedResolver(e)

		held, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		older := newTOCBytes(t, "tenant-a", indexpointers.IndexPointer{Path: "index/older", StartTs: window, EndTs: window.Add(time.Hour)})
		seedCache(t, e.mock, paths[0], (&tocValue{data: older, readAt: e.clock.Now().Add(-10 * time.Second)}).marshal())

		r.refreshTOCs(t.Context())

		got, err := r.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		require.Equal(t, held, got)
		require.Equal(t, 1, e.bucket.reads(paths[0]))
	})

	t.Run("takes the bytes that another resolver stored in the cache without an object storage read", func(t *testing.T) {
		e := newCachedEnv(t)
		first, second := newCachedResolver(e), newCachedResolver(e)

		for _, r := range []*TOCResolver{first, second} {
			_, err := r.ReadTOC(t.Context(), paths[0])
			require.NoError(t, err)
		}

		updated := newTOCBytes(t, "tenant-a",
			indexpointers.IndexPointer{Path: "index/a", StartTs: window, EndTs: window.Add(time.Hour)},
			indexpointers.IndexPointer{Path: "index/a2", StartTs: window.Add(time.Hour), EndTs: window.Add(2 * time.Hour)},
		)
		e.bucket.upload(t, paths[0], updated)
		e.clock.Advance(time.Minute)
		reads := e.bucket.reads(paths[0])

		first.refreshTOCs(t.Context())
		require.Equal(t, reads+1, e.bucket.reads(paths[0]), "the first refresh reads object storage")

		second.refreshTOCs(t.Context())
		require.Equal(t, reads+1, e.bucket.reads(paths[0]), "the second refresh reads the cache")

		got, err := second.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)
		require.Equal(t, updated, got)
	})

	t.Run("reads object storage when the cached value is older than half the refresh interval", func(t *testing.T) {
		e := newCachedEnv(t)
		first, second := newCachedResolver(e), newCachedResolver(e)
		for _, r := range []*TOCResolver{first, second} {
			_, err := r.ReadTOC(t.Context(), paths[0])
			require.NoError(t, err)
		}

		e.clock.Advance(time.Minute)
		first.refreshTOCs(t.Context())
		reads := e.bucket.reads(paths[0])
		e.clock.Advance(31 * time.Second)
		second.refreshTOCs(t.Context())

		require.Equal(t, reads+1, e.bucket.reads(paths[0]))
	})

	t.Run("serves a value taken from the cache only until twice the refresh interval after the other resolver read it", func(t *testing.T) {
		e := newCachedEnv(t)
		first, second := newCachedResolver(e), newCachedResolver(e)
		for _, r := range []*TOCResolver{first, second} {
			_, err := r.ReadTOC(t.Context(), paths[0])
			require.NoError(t, err)
		}

		e.clock.Advance(time.Minute)
		first.refreshTOCs(t.Context())
		e.clock.Advance(29 * time.Second)
		second.refreshTOCs(t.Context())
		reads := e.bucket.reads(paths[0])

		e.clock.Advance(time.Minute + 32*time.Second)
		_, err := second.ReadTOC(t.Context(), paths[0])
		require.NoError(t, err)

		require.Equal(t, reads+1, e.bucket.reads(paths[0]), "the other resolver read the bytes 2m1s ago")
	})

	t.Run("abandons the read behind a shared cache after the read timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			e := newCachedEnv(t)
			r := newCachedResolver(e)
			_, err := r.ReadTOC(t.Context(), paths[0])
			require.NoError(t, err)

			var abandonedAfter atomic.Int64
			start := time.Now()
			e.bucket.setBefore(func(ctx context.Context, _ string) error {
				<-ctx.Done()
				abandonedAfter.Store(int64(time.Since(start)))
				return ctx.Err()
			})
			e.clock.Advance(time.Minute)

			r.refreshTOCs(t.Context())
			synctest.Wait()

			require.Equal(t, int64(tocReadTimeout), abandonedAfter.Load())
		})
	})
}

func TestTOCResolver_readForRefresh(t *testing.T) {
	var (
		path = TableOfContentsPath("tenant-a", tocTestWindow)
		data = newTOCBytes(t, "tenant-a", indexpointers.IndexPointer{Path: "index/a", StartTs: tocTestWindow, EndTs: tocTestWindow.Add(time.Hour)})
	)

	type env struct {
		bucket *tocTestBucket
		mock   cache.MockCache
		clock  *testClock
	}

	newEnv := func(t *testing.T) env {
		t.Helper()
		bucket := newTOCTestBucket()
		bucket.upload(t, path, data)
		return env{bucket: bucket, mock: cache.NewMockCache(), clock: newTestClock()}
	}

	newResolver := func(e env, maxItemBytes int64, cacheKeyPrefix string) *TOCResolver {
		return newTestResolver(e.bucket, testResolverConfig(), newTestMetadataCache(e.mock, maxItemBytes), cacheKeyPrefix, e.clock)
	}

	storedValue := func(t *testing.T, e env) *tocValue {
		t.Helper()
		require.Len(t, e.mock.GetInternal(), 1)
		for _, buf := range e.mock.GetInternal() {
			v := &tocValue{}
			require.NoError(t, v.unmarshal(buf))
			return v
		}
		return nil
	}

	t.Run("reads the object store when the cached value is stamped far ahead of the clock", func(t *testing.T) {
		e := newEnv(t)
		r := newResolver(e, 0, "")
		seedCache(t, e.mock, path, (&tocValue{data: []byte("future"), readAt: e.clock.Now().Add(time.Hour)}).marshal())

		v, source, err := r.readForRefresh(t.Context(), path)
		require.NoError(t, err)

		require.Equal(t, sourceBucket, source)
		require.Equal(t, data, v.data)
		require.True(t, e.clock.Now().Equal(storedValue(t, e).readAt), "the new value replaces the future one")
	})

	t.Run("stores a missing ToC as an empty value and serves it from the cache next", func(t *testing.T) {
		e := newEnv(t)
		r := newResolver(e, 0, "")
		missing := TableOfContentsPath("tenant-missing", tocTestWindow)

		v, source, err := r.readForRefresh(t.Context(), missing)
		require.NoError(t, err)
		require.Equal(t, sourceBucket, source)
		require.Empty(t, v.data)

		v, source, err = r.readForRefresh(t.Context(), missing)
		require.NoError(t, err)
		require.Equal(t, sourceCache, source)
		require.Empty(t, v.data)
		require.Equal(t, 1, e.bucket.reads(missing))
	})

	t.Run("reads the object store and overwrites a cached value with an unknown version", func(t *testing.T) {
		e := newEnv(t)
		r := newResolver(e, 0, "")
		unknownVersion := (&tocValue{data: data, readAt: e.clock.Now()}).marshal()
		unknownVersion[0] = tocValueVersion + 1
		seedCache(t, e.mock, path, unknownVersion)

		v, source, err := r.readForRefresh(t.Context(), path)
		require.NoError(t, err)
		require.Equal(t, sourceBucket, source)
		require.Equal(t, data, v.data)
		require.Equal(t, data, storedValue(t, e).data)
	})

	t.Run("reads the object store when the cache fails", func(t *testing.T) {
		e := newEnv(t)
		e.mock.SetErr(errTOCTest, errTOCTest)
		r := newResolver(e, 0, "")

		v, source, err := r.readForRefresh(t.Context(), path)
		require.NoError(t, err)
		require.Equal(t, sourceBucket, source)
		require.Equal(t, data, v.data)
	})

	t.Run("returns a value above the item limit and does not store it", func(t *testing.T) {
		e := newEnv(t)
		r := newResolver(e, int64(len(data)), "")

		v, _, err := r.readForRefresh(t.Context(), path)
		require.NoError(t, err)
		require.Equal(t, data, v.data)
		require.Empty(t, e.mock.GetInternal())
	})

	t.Run("returns an error when the object store fails", func(t *testing.T) {
		e := newEnv(t)
		e.bucket.setErr(errTOCTest)
		r := newResolver(e, 0, "")

		_, _, err := r.readForRefresh(t.Context(), path)
		require.ErrorIs(t, err, errTOCTest)
		require.Empty(t, e.mock.GetInternal())
	})
}

func TestNewTOCResolverMetrics(t *testing.T) {
	t.Run("reports every label value of the counters before the first event", func(t *testing.T) {
		reg := prometheus.NewPedanticRegistry()
		newTOCResolverMetrics(reg)

		require.Equal(t, 3, testutil.CollectAndCount(reg, "loki_metastore_toc_lookups_total"))
		require.Equal(t, 4, testutil.CollectAndCount(reg, "loki_metastore_toc_loads_total"))
		require.Equal(t, 3, testutil.CollectAndCount(reg, "loki_metastore_toc_refreshes_total"))
		require.Equal(t, 4, testutil.CollectAndCount(reg, "loki_metastore_toc_evictions_total"))
	})
}

func TestTOCValue_marshal(t *testing.T) {
	t.Run("writes the version, the read time and the bytes in this layout", func(t *testing.T) {
		v := &tocValue{data: []byte("abc"), readAt: time.Unix(0, 0x0102030405060708)}

		require.Equal(t, []byte{1, 1, 2, 3, 4, 5, 6, 7, 8, 'a', 'b', 'c'}, v.marshal())
	})
}

func TestTOCValue_unmarshal(t *testing.T) {
	readAt := time.Date(2026, 10, 16, 12, 0, 0, 123, time.UTC)

	t.Run("returns an error and keeps the value when the input is shorter than the header", func(t *testing.T) {
		got := tocValue{data: []byte("old"), readAt: readAt}

		require.EqualError(t, got.unmarshal([]byte{tocValueVersion, 0, 0}), "the ToC value has 3 bytes, fewer than its 9 byte header")
		require.Equal(t, tocValue{data: []byte("old"), readAt: readAt}, got)
	})

	t.Run("returns an error and keeps the value when the version is unknown", func(t *testing.T) {
		buf := (&tocValue{data: []byte("toc"), readAt: readAt}).marshal()
		buf[0] = tocValueVersion + 1
		got := tocValue{data: []byte("old"), readAt: readAt}

		require.EqualError(t, got.unmarshal(buf), "the ToC value has version 2, want version 1")
		require.Equal(t, tocValue{data: []byte("old"), readAt: readAt}, got)
	})
}

func TestTOCValue_isFresh(t *testing.T) {
	var (
		now    = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
		maxAge = time.Minute
	)

	t.Run("is fresh when the read time is ahead of now by exactly the clock skew", func(t *testing.T) {
		v := &tocValue{readAt: now.Add(tocMaxClockSkew)}

		require.True(t, v.isFresh(now, maxAge))
	})

	t.Run("is not fresh when the read time is ahead of now by more than the clock skew", func(t *testing.T) {
		v := &tocValue{readAt: now.Add(tocMaxClockSkew + time.Nanosecond)}

		require.False(t, v.isFresh(now, maxAge))
	})
}

func TestObjectMetastore_listObjects(t *testing.T) {
	var (
		ctx  = user.InjectOrgID(t.Context(), tenantID)
		path = TableOfContentsPath(tenantID, tocTestWindow)
	)

	newMetastore := func(t *testing.T, toc []byte) *ObjectMetastore {
		t.Helper()
		bucket := newTOCTestBucket()
		if toc != nil {
			bucket.upload(t, IndexStoragePrefix+"/"+path, toc)
		}
		return NewObjectMetastore(bucket, Config{}, nil, prometheus.NewRegistry())
	}

	t.Run("returns no entries and no error when the ToC does not exist", func(t *testing.T) {
		entries, err := newMetastore(t, nil).listObjects(ctx, path, timestampScalar(tocTestWindow), timestampScalar(tocTestWindow.Add(12*time.Hour)))
		require.NoError(t, err)
		require.Empty(t, entries)
	})

	t.Run("returns an error when the object is not a data object", func(t *testing.T) {
		_, err := newMetastore(t, []byte("not a ToC")).listObjects(ctx, path, timestampScalar(tocTestWindow), timestampScalar(tocTestWindow.Add(12*time.Hour)))
		require.EqualError(t, err, `getting object from reader: reading metadata: reading header: scanning header: unexpected magic: got="not " want="DOBJ"`)
	})

	t.Run("returns a new entry only after the next refresh when the writer appended it", func(t *testing.T) {
		var (
			bucket = newTOCTestBucket()
			writer = newTableOfContentsWriter(t, NewIndexBucket(bucket))
			clock  = newTestClock()
			m      = NewObjectMetastore(bucket, Config{TOCResolver: testResolverConfig()}, nil, prometheus.NewRegistry())
			start  = timestampScalar(tocTestWindow)
			end    = timestampScalar(tocTestWindow.Add(12 * time.Hour))
		)
		m.tocResolver.now = clock.Now

		require.NoError(t, writer.WriteEntry(ctx, tenantID, TableOfContentsEntry{Path: "index/a", StartTime: tocTestWindow, EndTime: tocTestWindow.Add(time.Hour)}))
		entries, err := m.listObjects(ctx, path, start, end)
		require.NoError(t, err)
		require.Len(t, entries, 1)

		require.NoError(t, writer.WriteEntry(ctx, tenantID, TableOfContentsEntry{Path: "index/b", StartTime: tocTestWindow.Add(time.Hour), EndTime: tocTestWindow.Add(2 * time.Hour)}))
		require.Len(t, readToC(ctx, t, NewIndexBucket(bucket), path), 2, "the writer appends to the ToC in the object store, not to the old bytes in memory")

		entries, err = m.listObjects(ctx, path, start, end)
		require.NoError(t, err)
		require.Len(t, entries, 1, "the resolver still holds the old bytes")

		clock.Advance(time.Minute)
		m.tocResolver.refreshTOCs(ctx)

		entries, err = m.listObjects(ctx, path, start, end)
		require.NoError(t, err)
		require.Len(t, entries, 2)
	})
}

func TestObjectMetastore_GetIndexes(t *testing.T) {
	var (
		ctx         = user.InjectOrgID(t.Context(), tenantID)
		clock       = newTestClock()
		cfg         = Config{TOCResolver: testResolverConfig()}
		wideRequest = GetIndexesRequest{Start: now.Add(-24 * time.Hour), End: now.Add(24 * time.Hour)}
		builder     = newTestDataBuilder(t)
	)
	for _, stream := range testStreams {
		builder.addStreamAndFlush(tenantID, stream)
	}

	t.Run("returns the same non-empty indexes as a metastore without a resolver for a wide range", func(t *testing.T) {
		without, with := newMetastoresOnBucket(builder.bucket, cfg, clock)

		want, err := without.GetIndexes(ctx, wideRequest)
		require.NoError(t, err)
		got, err := with.GetIndexes(ctx, wideRequest)
		require.NoError(t, err)

		require.NotEmpty(t, want.Indexes)
		require.Equal(t, want, got)
	})

	t.Run("returns the same indexes as a metastore without a resolver for concurrent callers", func(t *testing.T) {
		without, with := newMetastoresOnBucket(builder.bucket, cfg, clock)
		want, err := without.GetIndexes(ctx, wideRequest)
		require.NoError(t, err)

		var g errgroup.Group
		for range 16 {
			g.Go(func() error {
				got, err := with.GetIndexes(ctx, wideRequest)
				if err != nil {
					return err
				}
				if !slices.Equal(got.Indexes, want.Indexes) {
					return fmt.Errorf("got indexes %v, want %v", got.Indexes, want.Indexes)
				}
				return nil
			})
		}
		require.NoError(t, g.Wait())
	})

	t.Run("reads each ToC from the object store once when the same range repeats with the resolver enabled", func(t *testing.T) {
		bucket := &getRecordingBucket{Bucket: builder.bucket}
		_, with := newMetastoresOnBucket(bucket, cfg, clock)

		for range 5 {
			_, err := with.GetIndexes(ctx, wideRequest)
			require.NoError(t, err)
		}

		reads := bucket.tocReads()
		require.NotEmpty(t, reads)
		seen := map[string]int{}
		for _, name := range reads {
			seen[name]++
		}
		for name, n := range seen {
			require.Equal(t, 1, n, "ToC %s read more than once", name)
		}
	})

	t.Run("returns the indexes of a ToC whose entries hold a run ID, posting keys and shard layout", func(t *testing.T) {
		keys, err := postings.NewKeyRange(
			postings.Key{Kind: postings.KindBloom, ColumnName: "trace_id"},
			postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api"},
		)
		require.NoError(t, err)
		buckets := indexpointers.NewShardBuckets(streams.MustShardLayoutFromBuckets(8))
		start, end := tocTestWindow, tocTestWindow.Add(time.Hour)

		bucket := newTOCTestBucket()
		bucket.upload(t, IndexStoragePrefix+"/"+TableOfContentsPath(tenantID, tocTestWindow), newTOCBytes(t, tenantID,
			indexpointers.IndexPointer{Path: "index/a", StartTs: start, EndTs: end, RunID: 1, PostingKeys: keys, ShardBuckets: buckets},
			indexpointers.IndexPointer{Path: "index/b", StartTs: start, EndTs: end},
		))

		resp, err := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry()).GetIndexes(ctx, GetIndexesRequest{Start: start, End: end})
		require.NoError(t, err)
		require.Len(t, resp.Indexes, 2)
		for i, path := range []string{"index/a", "index/b"} {
			require.Equal(t, path, resp.Indexes[i].Path)
			require.True(t, resp.Indexes[i].Start.Equal(start))
			require.True(t, resp.Indexes[i].End.Equal(end))
		}
		require.Equal(t, buckets, resp.Indexes[0].ShardBuckets)
		require.Zero(t, resp.Indexes[1].ShardBuckets)
	})

	t.Run("returns no shard layout for an entry that records none when an earlier read batch held one", func(t *testing.T) {
		buckets := indexpointers.NewShardBuckets(streams.MustShardLayoutFromBuckets(8))
		start, end := tocTestWindow, tocTestWindow.Add(time.Hour)

		// Each read batch holds 1024 pointers, so the pointers with a layout fill the first batch and
		// the pointers without one start the second.
		pointers := make([]indexpointers.IndexPointer, 1500)
		want := make(map[string]indexpointers.ShardBuckets, len(pointers))
		for i := range pointers {
			pointers[i] = indexpointers.IndexPointer{Path: fmt.Sprintf("index/%04d", i), StartTs: start, EndTs: end}
			if i < 1024 {
				pointers[i].ShardBuckets = buckets
			}
			want[pointers[i].Path] = pointers[i].ShardBuckets
		}
		bucket := newTOCTestBucket()
		bucket.upload(t, IndexStoragePrefix+"/"+TableOfContentsPath(tenantID, tocTestWindow), newTOCBytes(t, tenantID, pointers...))

		resp, err := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry()).GetIndexes(ctx, GetIndexesRequest{Start: start, End: end})
		require.NoError(t, err)
		got := make(map[string]indexpointers.ShardBuckets, len(resp.Indexes))
		for _, index := range resp.Indexes {
			got[index.Path] = index.ShardBuckets
		}
		require.Equal(t, want, got)
	})
}

func TestObjectMetastore_DataObjects(t *testing.T) {
	var (
		ctx     = user.InjectOrgID(t.Context(), tenantID)
		builder = newTestDataBuilder(t)
		paths   []string
	)
	for _, stream := range testStreams {
		paths = append(paths, builder.addStreamAndFlush(tenantID, stream))
	}

	t.Run("returns the same data objects as a metastore without a resolver", func(t *testing.T) {
		without, with := newMetastoresOnBucket(builder.bucket, Config{TOCResolver: testResolverConfig()}, newTestClock())

		want, err := without.DataObjects(ctx, now.Add(-24*time.Hour), now.Add(24*time.Hour))
		require.NoError(t, err)
		got, err := with.DataObjects(ctx, now.Add(-24*time.Hour), now.Add(24*time.Hour))
		require.NoError(t, err)

		require.Equal(t, want, got)
		require.ElementsMatch(t, paths, got)
	})
}

// testClock is a clock that only moves when a test advances it.
type testClock struct {
	mtx sync.Mutex
	t   time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.t = c.t.Add(d)
}

// tocTestBucket counts the reads of each object, and can fail or hold them.
type tocTestBucket struct {
	objstore.Bucket

	mtx  sync.Mutex
	gets map[string]int
	err  error

	// before runs at the start of every Get. The Get returns its error, if any, instead of the object.
	before func(ctx context.Context, name string) error
}

func newTOCTestBucket() *tocTestBucket {
	return &tocTestBucket{Bucket: objstore.NewInMemBucket(), gets: map[string]int{}}
}

func (b *tocTestBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	b.mtx.Lock()
	b.gets[name]++
	err, before := b.err, b.before
	b.mtx.Unlock()

	if before != nil {
		if beforeErr := before(ctx, name); beforeErr != nil {
			return nil, beforeErr
		}
	}
	if err != nil {
		return nil, err
	}
	return b.Bucket.Get(ctx, name)
}

func (b *tocTestBucket) setErr(err error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.err = err
}

func (b *tocTestBucket) setBefore(f func(ctx context.Context, name string) error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.before = f
}

func (b *tocTestBucket) reads(name string) int {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	return b.gets[name]
}

func (b *tocTestBucket) totalReads() int {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	var n int
	for _, c := range b.gets {
		n += c
	}
	return n
}

func (b *tocTestBucket) upload(t *testing.T, name string, data []byte) {
	t.Helper()
	require.NoError(t, b.Upload(t.Context(), name, bytes.NewReader(data)))
}

func holdUntil(release <-chan struct{}) func(context.Context, string) error {
	return func(context.Context, string) error {
		<-release
		return nil
	}
}

// hang is a hook that blocks every Get until its context ends, and then fails it with the context error.
func hang(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func newTOCBytes(t *testing.T, tenant string, pointers ...indexpointers.IndexPointer) []byte {
	t.Helper()

	builder, err := indexobj.NewBuilder(tenant, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	for _, p := range pointers {
		require.NoError(t, builder.AppendIndexPointer(p))
	}

	rc, err := flushToc(t.Context(), builder)
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func timestampScalar(t time.Time) *scalar.Timestamp {
	return scalar.NewTimestampScalar(arrow.Timestamp(t.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
}

// testResolverConfig returns an enabled config with the default intervals and a 1 MiB size limit.
func testResolverConfig() TOCResolverConfig {
	return TOCResolverConfig{
		Enabled:             true,
		RefreshInterval:     time.Minute,
		ActiveWindow:        5 * time.Minute,
		MaxMemoryCacheBytes: 1 << 20,
	}
}

// newTestResolver returns a resolver on bucket that reads the time from clock. It does not run.
func newTestResolver(bucket objstore.BucketReader, cfg TOCResolverConfig, c *metadata.Cache, cacheKeyPrefix string, clock *testClock) *TOCResolver {
	r := NewTOCResolver(bucket, cfg, c, cacheKeyPrefix, log.NewNopLogger(), prometheus.NewPedanticRegistry())
	r.now = clock.Now
	return r
}

func newTestMetadataCache(backend cache.Cache, maxItemBytes int64) *metadata.Cache {
	return metadata.NewCache(backend, maxItemBytes, metadata.KindTOC, metadata.NewCacheMetrics(nil), nil)
}

func entryOf(r *TOCResolver, path string) *tocEntry {
	r.tocsMx.RLock()
	defer r.tocsMx.RUnlock()

	return r.tocs[path]
}

// seedCache stores value under path, the key that a resolver with an empty cache key prefix uses.
func seedCache(t *testing.T, mock cache.MockCache, path string, value []byte) {
	t.Helper()

	_, err := newTestMetadataCache(mock, 0).GetOrLoad(t.Context(), path, nil, func(context.Context) ([]byte, error) {
		return value, nil
	})
	require.NoError(t, err)
}

// newMetastoresOnBucket returns two metastores on bucket. The first has the resolver disabled, so it reads every ToC from
// the bucket. The second uses cfg and reads the time from clock.
func newMetastoresOnBucket(bucket objstore.Bucket, cfg Config, clock *testClock) (without, with *ObjectMetastore) {
	disabled := cfg
	disabled.TOCResolver.Enabled = false
	without = NewObjectMetastore(bucket, disabled, nil, prometheus.NewRegistry())
	with = NewObjectMetastore(bucket, cfg, nil, prometheus.NewPedanticRegistry())
	with.tocResolver.now = clock.Now
	return without, with
}
