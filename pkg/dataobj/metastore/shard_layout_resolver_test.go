package metastore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestShardLayoutResolver(t *testing.T) {
	layout32 := streams.MustShardLayoutFromBuckets(32)
	layout8 := streams.MustShardLayoutFromBuckets(8)

	t.Run("keeps the layout of each tenant of an index object apart", func(t *testing.T) {
		resolver := newShardLayoutResolver(func(_ context.Context, tenantID, _ string) (streams.ShardLayout, error) {
			if tenantID == "tenant-a" {
				return layout32, nil
			}
			return layout8, nil
		}, log.NewNopLogger())

		layoutA, err := resolver.resolveShardLayout(t.Context(), "tenant-a", "index-a")
		require.NoError(t, err)
		layoutB, err := resolver.resolveShardLayout(t.Context(), "tenant-b", "index-a")
		require.NoError(t, err)

		require.Equal(t, layout32, layoutA)
		require.Equal(t, layout8, layoutB)
	})

	t.Run("does not remember an error", func(t *testing.T) {
		var opens atomic.Int64
		resolver := newShardLayoutResolver(func(context.Context, string, string) (streams.ShardLayout, error) {
			if opens.Inc() == 1 {
				return streams.ShardLayout{}, errors.New("storage unavailable")
			}
			return layout32, nil
		}, log.NewNopLogger())

		_, err := resolver.resolveShardLayout(t.Context(), "tenant", "index-a")
		require.EqualError(t, err, "storage unavailable")

		layout, err := resolver.resolveShardLayout(t.Context(), "tenant", "index-a")
		require.NoError(t, err)
		require.Equal(t, layout32, layout)
	})

	t.Run("shares one read between concurrent calls for the same index object", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				opens   atomic.Int64
				release = make(chan struct{})
			)
			resolver := newShardLayoutResolver(func(context.Context, string, string) (streams.ShardLayout, error) {
				opens.Inc()
				<-release
				return layout32, nil
			}, log.NewNopLogger())

			var (
				wg      sync.WaitGroup
				layouts = make([]streams.ShardLayout, 8)
				errs    = make([]error, 8)
			)
			for i := range layouts {
				wg.Add(1)
				go func() {
					defer wg.Done()
					layouts[i], errs[i] = resolver.resolveShardLayout(t.Context(), "tenant", "index-a")
				}()
			}
			synctest.Wait()
			close(release)
			wg.Wait()

			require.Equal(t, int64(1), opens.Load())
			for i := range layouts {
				require.NoError(t, errs[i])
				require.Equal(t, layout32, layouts[i])
			}
		})
	})

	t.Run("keeps the concurrent reads of two tenants apart", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			resolver := newShardLayoutResolver(func(_ context.Context, tenantID, _ string) (streams.ShardLayout, error) {
				<-release
				if tenantID == "tenant-a" {
					return layout32, nil
				}
				return layout8, nil
			}, log.NewNopLogger())

			var (
				wg         sync.WaitGroup
				layoutA    streams.ShardLayout
				layoutB    streams.ShardLayout
				errA, errB error
			)
			wg.Add(2)
			go func() {
				defer wg.Done()
				layoutA, errA = resolver.resolveShardLayout(t.Context(), "tenant-a", "index-a")
			}()
			go func() {
				defer wg.Done()
				layoutB, errB = resolver.resolveShardLayout(t.Context(), "tenant-b", "index-a")
			}()
			synctest.Wait()
			close(release)
			wg.Wait()

			require.NoError(t, errA)
			require.NoError(t, errB)
			require.Equal(t, layout32, layoutA)
			require.Equal(t, layout8, layoutB)
		})
	})

	t.Run("lets a call return on its own cancel without stopping the read for the others", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			resolver := newShardLayoutResolver(func(ctx context.Context, _, _ string) (streams.ShardLayout, error) {
				select {
				case <-release:
					return layout32, nil
				case <-ctx.Done():
					return streams.ShardLayout{}, ctx.Err()
				}
			}, log.NewNopLogger())

			canceled, cancel := context.WithCancel(t.Context())
			firstDone := make(chan error, 1)
			go func() {
				_, err := resolver.resolveShardLayout(canceled, "tenant", "index-a")
				firstDone <- err
			}()
			synctest.Wait()

			var (
				secondLayout streams.ShardLayout
				secondErr    error
				secondDone   = make(chan struct{})
			)
			go func() {
				defer close(secondDone)
				secondLayout, secondErr = resolver.resolveShardLayout(t.Context(), "tenant", "index-a")
			}()
			synctest.Wait()

			cancel()
			synctest.Wait()
			require.ErrorIs(t, <-firstDone, context.Canceled)

			close(release)
			<-secondDone
			require.NoError(t, secondErr)
			require.Equal(t, layout32, secondLayout)
		})
	})

	t.Run("fails a read that reaches its time limit with an error that does not wrap a deadline error", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			resolver := newShardLayoutResolver(func(ctx context.Context, _, _ string) (streams.ShardLayout, error) {
				<-ctx.Done()
				return streams.ShardLayout{}, ctx.Err()
			}, log.NewNopLogger())

			_, err := resolver.resolveShardLayout(t.Context(), "tenant", "index-a")

			require.EqualError(t, err, `reading the shard layout of index object "index-a" from object storage took longer than 30s`)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
		})
	})

	t.Run("starts no read for a call that is already canceled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var opens atomic.Int64
			resolver := newShardLayoutResolver(func(context.Context, string, string) (streams.ShardLayout, error) {
				opens.Inc()
				return layout32, nil
			}, log.NewNopLogger())

			canceled, cancel := context.WithCancel(t.Context())
			cancel()

			_, err := resolver.resolveShardLayout(canceled, "tenant", "index-a")
			synctest.Wait()

			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, opens.Load())
		})
	})

	t.Run("reports the layout of each index object in its own slot", func(t *testing.T) {
		resolver := newShardLayoutResolver(func(_ context.Context, _, indexPath string) (streams.ShardLayout, error) {
			if indexPath == "index-b" {
				return streams.ShardLayout{}, errors.New("corrupt index object")
			}
			return layout32, nil
		}, log.NewNopLogger())

		resolutions := resolver.resolveShardLayouts(t.Context(), "tenant", []IndexEntry{{Path: "index-a"}, {Path: "index-b"}, {Path: "index-c"}})

		require.Len(t, resolutions, 3)
		require.NoError(t, resolutions[0].err)
		require.Equal(t, layout32, resolutions[0].layout)
		require.EqualError(t, resolutions[1].err, "corrupt index object")
		require.NoError(t, resolutions[2].err)
	})

	t.Run("forgets the least recently used layout when it is full", func(t *testing.T) {
		var opens atomic.Int64
		resolver := newShardLayoutResolver(func(context.Context, string, string) (streams.ShardLayout, error) {
			opens.Inc()
			return layout32, nil
		}, log.NewNopLogger())

		for i := range shardLayoutResolverSize + 1 {
			_, err := resolver.resolveShardLayout(t.Context(), "tenant", fmt.Sprintf("index-%d", i))
			require.NoError(t, err)
		}
		require.Equal(t, int64(shardLayoutResolverSize+1), opens.Load())

		_, err := resolver.resolveShardLayout(t.Context(), "tenant", fmt.Sprintf("index-%d", shardLayoutResolverSize))
		require.NoError(t, err)
		require.Equal(t, int64(shardLayoutResolverSize+1), opens.Load())

		_, err = resolver.resolveShardLayout(t.Context(), "tenant", "index-0")
		require.NoError(t, err)
		require.Equal(t, int64(shardLayoutResolverSize+2), opens.Load())
	})
}
