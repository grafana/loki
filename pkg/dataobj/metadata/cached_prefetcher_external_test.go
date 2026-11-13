// These tests are in package metadata_test because they use objtest, which imports pkg/dataobj, which
// imports metadata. A test in package metadata that imports objtest is an import cycle.

package metadata_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/metrics"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// headBytes is the smallest first read of a cold open.
const headBytes = 16 * 1024

func TestCachedPrefetcher(t *testing.T) {
	obj := newTestObject(t)

	prefetchSets := map[string]metadata.PrefetchSet{
		"stream ID and timestamp": {},
		"message":                 {Message: true},
		"one metadata column":     {MetadataNames: []string{"trace_id"}},
		"every metadata column":   {Message: true, AllMetadata: true},
	}

	t.Run("a warm lookup makes one cache lookup and no object storage read", func(t *testing.T) {
		for name, prefetchSet := range prefetchSets {
			t.Run(name, func(t *testing.T) {
				backend := newFakeCache()
				p, reg := newPrefetcherWithRegistry(t, backend, nil)
				req := obj.request(prefetchSet)
				_, err := p.Prefetch(t.Context(), req)
				require.NoError(t, err)

				backend.resetCounts()
				src := req.Source.(*testSource)
				src.reset()
				res, err := p.Prefetch(t.Context(), req)

				require.NoError(t, err)
				obj.requireMatches(t, res)
				require.Equal(t, 1, backend.lookups())
				require.Zero(t, backend.sets())
				require.Empty(t, src.calls())
				counts := gatherPrefetcherCounts(t, reg)
				require.Equal(t, 1.0, counts.cacheMisses)
				require.Equal(t, 1.0, counts.cacheHits)
			})
		}
	})

	t.Run("two objects that share one cache keep their own metadata, cold and warm", func(t *testing.T) {
		other := newTestObjectWithStreams(t, 60)
		require.NotEqual(t, obj.path, other.path)
		objs := []*testObject{obj, other}
		p := newPrefetcher(t, newFakeCache(), nil)

		prefetchBoth := func(t *testing.T) []*testSource {
			t.Helper()

			srcs := make([]*testSource, len(objs))
			for i, o := range objs {
				req := o.request(prefetchSets["message"])
				res, err := p.Prefetch(t.Context(), req)
				require.NoError(t, err)
				o.requireMatches(t, res)
				srcs[i] = req.Source.(*testSource)
			}
			return srcs
		}

		for _, src := range prefetchBoth(t) {
			require.NotEmpty(t, src.calls(), "a cold prefetch reads the object")
		}
		for _, src := range prefetchBoth(t) {
			require.Empty(t, src.calls(), "a warm prefetch does not read the object")
		}
	})

	t.Run("a column the object does not have is a hit", func(t *testing.T) {
		backend := newFakeCache()
		p := newPrefetcher(t, backend, nil)
		req := obj.request(metadata.PrefetchSet{MetadataNames: []string{"not_a_column"}})
		_, err := p.Prefetch(t.Context(), req)
		require.NoError(t, err)

		backend.resetCounts()
		src := req.Source.(*testSource)
		src.reset()
		res, err := p.Prefetch(t.Context(), req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		require.Zero(t, backend.sets())
		require.Empty(t, src.calls())
	})

	t.Run("the region ends where the metadata regions end, so it holds no section data", func(t *testing.T) {
		all, err := newPrefetcher(t, newFakeCache(), nil).Prefetch(t.Context(), obj.request(prefetchSets["every metadata column"]))
		require.NoError(t, err)
		narrow, err := newPrefetcher(t, newFakeCache(), nil).Prefetch(t.Context(), obj.request(prefetchSets["message"]))
		require.NoError(t, err)

		obj.requireMatches(t, all)
		require.Len(t, all.PrefetchedRanges, 1)
		require.EqualValues(t, obj.metadataRegionEnd, len(all.PrefetchedRanges[0].Data))
		last := narrow.PrefetchedRanges[len(narrow.PrefetchedRanges)-1]
		require.LessOrEqual(t, last.Offset+int64(len(last.Data)), obj.metadataRegionEnd)
	})

	t.Run("an object is prefetchable when its metadata region equals the max region size, and not when it is larger", func(t *testing.T) {
		every := obj.request(prefetchSets["every metadata column"])

		fits, err := newPrefetcherWithMaxRegionBytes(t, newFakeCache(), int(obj.metadataRegionEnd)).Prefetch(t.Context(), every)
		require.NoError(t, err)
		obj.requireMatches(t, fits)

		_, err = newPrefetcherWithMaxRegionBytes(t, newFakeCache(), int(obj.metadataRegionEnd)-1).Prefetch(t.Context(), every)
		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
	})

	t.Run("an object above the region limit is skipped with one head read and no cache write", func(t *testing.T) {
		backend := newFakeCache()
		p := newPrefetcherWithMaxRegionBytes(t, backend, int(obj.metadataRegionEnd)-1)
		req := obj.request(prefetchSets["every metadata column"])

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Equal(t, []readCall{{offset: 0, length: headBytes}}, req.Source.(*testSource).calls())
		require.Zero(t, backend.sets())
	})

	t.Run("an object above the region limit is skipped without a read when its file metadata is cached", func(t *testing.T) {
		backend := newFakeCache()
		req := obj.request(prefetchSets["message"])
		_, err := newPrefetcher(t, backend, nil).Prefetch(t.Context(), req)
		require.NoError(t, err)
		backend.drop("core")
		backend.drop("region")
		backend.resetCounts()
		src := req.Source.(*testSource)
		src.reset()
		p := newPrefetcherWithMaxRegionBytes(t, backend, int(obj.metadataRegionEnd)-1)

		_, err = p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Empty(t, src.calls())
		require.Zero(t, backend.sets())
	})

	t.Run("an object whose file metadata does not fit in the head is skipped with one head read and no cache write", func(t *testing.T) {
		backend := newFakeCache()
		p, reg := newPrefetcherWithRegistry(t, backend, nil)
		req := obj.request(prefetchSets["message"])
		head := append([]byte("DOBJ"), binary.LittleEndian.AppendUint32(nil, 4*headBytes)...)
		src := req.Source.(*testSource)
		src.data = append(head, make([]byte, 2*headBytes)...)

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Equal(t, []readCall{{offset: 0, length: headBytes}}, src.calls())
		require.Zero(t, backend.sets())
		require.Equal(t, map[string]float64{"head_too_small": 1}, gatherPrefetcherCounts(t, reg).failures)
	})

	t.Run("a prefetch set that names more than 64 columns reads the whole region with one lookup", func(t *testing.T) {
		names := make([]string, 65)
		for i := range names {
			names[i] = fmt.Sprintf("column_%d", i)
		}
		backend := newFakeCache()
		p := newPrefetcher(t, backend, nil)
		req := obj.request(metadata.PrefetchSet{MetadataNames: names})
		_, err := p.Prefetch(t.Context(), req)
		require.NoError(t, err)

		backend.resetCounts()
		res, err := p.Prefetch(t.Context(), req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		require.Len(t, res.PrefetchedRanges, 1)
		require.Equal(t, 1, backend.lookups())
	})

	for name, tc := range map[string]struct {
		drop        string
		prefetchSet metadata.PrefetchSet
	}{
		"the core group":   {drop: "core", prefetchSet: prefetchSets["message"]},
		"the message":      {drop: "msg", prefetchSet: prefetchSets["message"]},
		"a metadata group": {drop: "col:", prefetchSet: prefetchSets["one metadata column"]},
	} {
		t.Run("an evicted group is read back with one exact read of the region: "+name, func(t *testing.T) {
			backend := newFakeCache()
			p, reg := newPrefetcherWithRegistry(t, backend, nil)
			req := obj.request(tc.prefetchSet)
			_, err := p.Prefetch(t.Context(), req)
			require.NoError(t, err)

			backend.drop(tc.drop)
			backend.drop("region")
			backend.resetCounts()
			src := req.Source.(*testSource)
			src.reset()
			res, err := p.Prefetch(t.Context(), req)

			require.NoError(t, err)
			obj.requireMatches(t, res)
			calls := src.calls()
			require.Len(t, calls, 1)
			require.Zero(t, calls[0].offset)
			require.Positive(t, calls[0].length)
			require.Equal(t, 2, backend.sets(), "the load writes the group that it asked for and missed, and the region")
			require.NotEmpty(t, backend.entry("region"), "the region was written back, so the next load can read it from the cache")
			require.Equal(t, 1.0, gatherPrefetcherCounts(t, reg).cachePartialMisses)

			backend.resetCounts()
			src.reset()
			_, err = p.Prefetch(t.Context(), req)
			require.NoError(t, err)
			require.Zero(t, backend.sets(), "the group was written back")
			require.Empty(t, src.calls())
			require.Equal(t, 1.0, gatherPrefetcherCounts(t, reg).cacheHits)
		})
	}

	t.Run("a cold open reads the head, then the tail, and writes every group", func(t *testing.T) {
		backend := newFakeCache()
		p, reg := newPrefetcherWithRegistry(t, backend, nil)
		req := obj.request(prefetchSets["message"])

		res, err := p.Prefetch(t.Context(), req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		calls := req.Source.(*testSource).calls()
		require.Len(t, calls, 2)
		require.Equal(t, readCall{offset: 0, length: headBytes}, calls[0])
		require.Equal(t, int64(headBytes), calls[1].offset)
		for _, part := range []string{"fm", "core", "msg", "region", "col:"} {
			require.NotEmpty(t, backend.entry(part), part)
		}

		// The request looks up 3 keys: the file metadata, the core and the message group.
		var returned float64
		for _, r := range res.PrefetchedRanges {
			returned += float64(len(r.Data))
		}
		require.Equal(t, prefetcherCounts{
			requests: 1, cacheMisses: 1,
			durations: 1, lookupKeys: 1, lookupKeysSum: 3, responseBytes: 1, responseBytesSum: returned,
		}, gatherPrefetcherCounts(t, reg))
		require.Equal(t, regionLoaderMetrics{requests: 1, storageSuccesses: 1, storageDurations: 1, stores: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("a load that takes longer than the load timeout fails with the deadline", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			p, reg := newPrefetcherWithRegistry(t, newFakeCache(), func(cfg *metadata.PrefetcherConfig) { cfg.LoadTimeout = time.Second })
			req := obj.request(prefetchSets["message"])
			req.Source.(*testSource).block = true

			_, err := p.Prefetch(t.Context(), req)

			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.NotErrorIs(t, err, metadata.ErrNotPrefetchable)
			require.Equal(t, map[string]float64{"timeout": 1}, gatherPrefetcherCounts(t, reg).failures)
		})
	})

	t.Run("a failing close of the object storage reader fails the load", func(t *testing.T) {
		closeErr := errors.New("close failed")
		p := newPrefetcher(t, newFakeCache(), nil)
		req := obj.request(prefetchSets["message"])
		req.Source.(*testSource).closeErr = closeErr

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, closeErr)
	})

	t.Run("a cold open takes the region from the cache when object storage is slow", func(t *testing.T) {
		backend := newFakeCache()
		p, reg := newPrefetcherWithRegistry(t, backend, nil)
		_, err := p.Prefetch(t.Context(), obj.request(prefetchSets["message"]))
		require.NoError(t, err)

		backend.drop("fm")
		req := obj.request(prefetchSets["message"])
		req.Source.(*testSource).block = true
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		res, err := p.Prefetch(ctx, req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		require.NotEmpty(t, backend.entry("fm"), "the open wrote the file metadata group back")
		require.Equal(t, 1.0, gatherPrefetcherCounts(t, reg).cachePartialMisses, "the other groups were in the cache")
		require.Equal(t, 1.0, gatherRegionLoaderMetrics(t, reg).cacheSuccesses)
	})

	t.Run("a group that fails validation counts as a miss and is replaced", func(t *testing.T) {
		backend := newFakeCache()
		p := newPrefetcher(t, backend, nil)
		req := obj.request(prefetchSets["message"])
		_, err := p.Prefetch(t.Context(), req)
		require.NoError(t, err)

		backend.corrupt("core")
		res, err := p.Prefetch(t.Context(), req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		backend.resetCounts()
		_, err = p.Prefetch(t.Context(), req)
		require.NoError(t, err)
		require.Zero(t, backend.sets(), "the first call replaced the corrupt group")
	})

	t.Run("a group stored under the key of another group counts as a miss and is replaced", func(t *testing.T) {
		backend := newFakeCache()
		p := newPrefetcher(t, backend, nil)
		req := obj.request(prefetchSets["message"])
		_, err := p.Prefetch(t.Context(), req)
		require.NoError(t, err)

		backend.copyEntry("core", "msg")
		backend.resetCounts()
		res, err := p.Prefetch(t.Context(), req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		backend.resetCounts()
		_, err = p.Prefetch(t.Context(), req)
		require.NoError(t, err)
		require.Zero(t, backend.sets())
	})

	t.Run("a region that fails validation is replaced by the object storage read", func(t *testing.T) {
		backend := newFakeCache()
		p := newPrefetcher(t, backend, nil)
		_, err := p.Prefetch(t.Context(), obj.request(prefetchSets["message"]))
		require.NoError(t, err)

		backend.corrupt("region")
		backend.drop("core")
		res, err := p.Prefetch(t.Context(), obj.request(prefetchSets["message"]))
		require.NoError(t, err)
		obj.requireMatches(t, res)

		every := obj.request(prefetchSets["every metadata column"])
		res, err = p.Prefetch(t.Context(), every)
		require.NoError(t, err)
		obj.requireMatches(t, res)

		backend.resetCounts()
		src := every.Source.(*testSource)
		src.reset()
		_, err = p.Prefetch(t.Context(), every)
		require.NoError(t, err)
		require.Zero(t, backend.sets(), "the region was written back")
		require.Empty(t, src.calls())
	})

	t.Run("concurrent cold prefetches of one object share one load", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			backend := newFakeCache()
			p := newPrefetcher(t, backend, nil)
			src := &testSource{bucket: obj.bucket, path: obj.path, gate: make(chan struct{})}

			var wg sync.WaitGroup
			errs := make([]error, 4)
			for i := range errs {
				wg.Go(func() {
					_, errs[i] = p.Prefetch(t.Context(), metadata.PrefetchRequest{Path: obj.path, PrefetchSet: prefetchSets["message"], Source: src})
				})
			}
			synctest.Wait()
			close(src.gate)
			wg.Wait()

			for _, err := range errs {
				require.NoError(t, err)
			}
			require.Len(t, src.calls(), 2, "one head read and one tail read for every caller")
		})
	})

	t.Run("concurrent cold prefetches of two objects do not share a load", func(t *testing.T) {
		other := newTestObjectWithStreams(t, 60)
		require.NotEqual(t, obj.path, other.path)

		synctest.Test(t, func(t *testing.T) {
			backend := newFakeCache()
			p := newPrefetcher(t, backend, nil)
			gate := make(chan struct{})
			objs := []*testObject{obj, other}
			srcs := []*testSource{
				{bucket: obj.bucket, path: obj.path, gate: gate},
				{bucket: other.bucket, path: other.path, gate: gate},
			}

			var wg sync.WaitGroup
			res := make([]metadata.PrefetchResponse, len(objs))
			errs := make([]error, len(objs))
			for i := range objs {
				wg.Go(func() {
					res[i], errs[i] = p.Prefetch(t.Context(), metadata.PrefetchRequest{Path: objs[i].path, PrefetchSet: prefetchSets["message"], Source: srcs[i]})
				})
			}
			synctest.Wait()
			close(gate)
			wg.Wait()

			for i, o := range objs {
				require.NoError(t, errs[i])
				o.requireMatches(t, res[i])
				require.Len(t, srcs[i].calls(), 2, "one head read and one tail read for each object")
			}
		})
	})

	t.Run("a caller that cancels leaves, and the caller that shares its load still gets the metadata", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			backend := newFakeCache()
			p, reg := newPrefetcherWithRegistry(t, backend, nil)
			src := &testSource{bucket: obj.bucket, path: obj.path, gate: make(chan struct{})}
			req := metadata.PrefetchRequest{Path: obj.path, PrefetchSet: prefetchSets["message"], Source: src}

			var (
				wg                      sync.WaitGroup
				leaderErr, followerErr  error
				leaderCtx, cancelLeader = context.WithCancel(t.Context())
			)
			wg.Go(func() { _, leaderErr = p.Prefetch(leaderCtx, req) })
			synctest.Wait()
			wg.Go(func() { _, followerErr = p.Prefetch(t.Context(), req) })
			synctest.Wait()

			cancelLeader()
			synctest.Wait()
			close(src.gate)
			wg.Wait()

			require.ErrorIs(t, leaderErr, context.Canceled)
			require.NoError(t, followerErr)
			require.NotEmpty(t, backend.entry("core"), "the load ran to the end and wrote its groups")
			require.Equal(t, map[string]float64{"canceled": 1}, gatherPrefetcherCounts(t, reg).failures)
		})
	})

	t.Run("returns ErrNotPrefetchable when the metadata is above the limit, and writes nothing", func(t *testing.T) {
		backend := newFakeCache()
		p, reg := newPrefetcherWithRegistry(t, backend, func(cfg *metadata.PrefetcherConfig) { cfg.MaxRegionBytes = 1024 })

		_, err := p.Prefetch(t.Context(), obj.request(prefetchSets["message"]))

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Zero(t, backend.sets())
		require.Equal(t, map[string]float64{"too_large": 1}, gatherPrefetcherCounts(t, reg).failures)
		require.Equal(t, regionLoaderMetrics{requests: 1, failures: map[string]float64{"too_large": 1}}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("returns ErrNotPrefetchable for bytes that are not a data object, and writes nothing", func(t *testing.T) {
		backend := newFakeCache()
		p, reg := newPrefetcherWithRegistry(t, backend, nil)
		req := obj.request(prefetchSets["message"])
		req.Source = &testSource{data: bytes.Repeat([]byte("x"), 64*1024)}

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Zero(t, backend.sets())
		require.Equal(t, map[string]float64{"corrupted": 1}, gatherPrefetcherCounts(t, reg).failures)
	})

	t.Run("returns ErrNotPrefetchable when the cache fails to read, so the open reads directly", func(t *testing.T) {
		backend := newFakeCache()
		backend.failReads = true
		p, reg := newPrefetcherWithRegistry(t, backend, nil)
		req := obj.request(prefetchSets["message"])

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Empty(t, req.Source.(*testSource).calls(), "the open must not read the whole region")
		require.Equal(t, map[string]float64{"cache_unavailable": 1}, gatherPrefetcherCounts(t, reg).failures)
	})

	t.Run("returns ErrNotPrefetchable and counts a cache timeout when the cache lookup times out", func(t *testing.T) {
		backend := newFakeCache()
		backend.timeoutReads = true
		p, reg := newPrefetcherWithRegistry(t, backend, nil)
		req := obj.request(prefetchSets["message"])

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Empty(t, req.Source.(*testSource).calls(), "the open must not read the whole region")
		require.Equal(t, map[string]float64{"cache_timeout": 1}, gatherPrefetcherCounts(t, reg).failures)
	})

	t.Run("counts a storage timeout when the object storage client gives up waiting", func(t *testing.T) {
		p, reg := newPrefetcherWithRegistry(t, newFakeCache(), nil)
		req := obj.request(prefetchSets["message"])
		req.Source.(*testSource).err = socketTimeoutError()

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		require.Equal(t, map[string]float64{"storage_timeout": 1}, gatherPrefetcherCounts(t, reg).failures)
	})

	t.Run("serves the prefetch from the groups the cache returned when the cache also returned an error", func(t *testing.T) {
		warm := newFakeCache()
		_, err := newPrefetcher(t, warm, nil).Prefetch(t.Context(), obj.request(prefetchSets["message"]))
		require.NoError(t, err)
		warm.resetCounts()
		p, reg := newPrefetcherWithRegistry(t, partialResultsCache{warm}, nil)
		req := obj.request(prefetchSets["message"])

		res, err := p.Prefetch(t.Context(), req)

		require.NoError(t, err)
		obj.requireMatches(t, res)
		require.Empty(t, req.Source.(*testSource).calls())
		require.Zero(t, warm.sets())
		require.Equal(t, 1.0, gatherPrefetcherCounts(t, reg).cacheHits)
	})

	t.Run("a cache that fails to write does not fail the prefetch", func(t *testing.T) {
		backend := newFakeCache()
		backend.failWrites = true
		p, reg := newPrefetcherWithRegistry(t, backend, nil)

		res, err := p.Prefetch(t.Context(), obj.request(prefetchSets["message"]))

		require.NoError(t, err)
		obj.requireMatches(t, res)
		require.Equal(t, 1.0, gatherRegionLoaderMetrics(t, reg).storeFailures, "the write of the region failed")
	})

	t.Run("fails when object storage fails and the cache has nothing", func(t *testing.T) {
		p, reg := newPrefetcherWithRegistry(t, newFakeCache(), nil)
		req := obj.request(prefetchSets["message"])
		wantErr := errors.New("storage is down")
		req.Source.(*testSource).err = wantErr

		_, err := p.Prefetch(t.Context(), req)

		require.ErrorIs(t, err, wantErr)
		require.Equal(t, map[string]float64{"storage_error": 1}, gatherPrefetcherCounts(t, reg).failures)
	})

	t.Run("reads a column that the prefetch set does not name from object storage and the other columns from memory", func(t *testing.T) {
		types := []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp}
		p := newPrefetcher(t, newFakeCache(), nil)
		prefetchSet := metadata.PrefetchSet{MetadataNames: []string{"trace_id"}}

		// warm opens the object twice, so the second open is warm, and returns the rows of the second open
		// and the reads of it that start inside the metadata block.
		warm := func(names []string) (rows []string, reads int) {
			obj.readRows(t, obj.bucket, p, prefetchSet, types, names)

			recording := &recordingBucket{Bucket: obj.bucket}
			rows = obj.readRows(t, recording, p, prefetchSet, types, names)
			return rows, recording.readsBelow(obj.metadataRegionEnd)
		}

		_, namedReads := warm([]string{"trace_id"})
		_, unnamedReads := warm([]string{"level"})
		rows, mixedReads := warm([]string{"level", "trace_id"})

		require.Equal(t, obj.readRows(t, obj.bucket, nil, metadata.PrefetchSet{}, types, []string{"level", "trace_id"}), rows)
		require.Zero(t, namedReads)
		require.Positive(t, unnamedReads)
		require.Equal(t, unnamedReads, mixedReads, "only the column that the prefetch set does not name reads from object storage")
	})
}

type testObject struct {
	bucket objstore.Bucket
	path   string
	raw    []byte

	// metadataRegionEnd is where the metadata regions of all sections end.
	metadataRegionEnd int64
}

// newTestObject builds an object with many small logs sections, so its metadata is larger than the
// first read of a cold open.
func newTestObject(t *testing.T) *testObject {
	t.Helper()
	return newTestObjectWithStreams(t, 40)
}

// newTestObjectWithStreams builds a test object with the given number of streams. Objects with a
// different number of streams have different paths and different metadata.
func newTestObjectWithStreams(t *testing.T, streams int) *testObject {
	t.Helper()

	builder := objtest.NewBuilder(t, objtest.WithTargetSectionSize(flagext.Bytes(8*1024)))
	ctx := user.InjectOrgID(t.Context(), objtest.Tenant)
	for i := range streams {
		entries := make([]push.Entry, 0, 200)
		for j := range 200 {
			entries = append(entries, push.Entry{
				Timestamp: time.Unix(int64(j+1), 0).UTC(),
				Line:      fmt.Sprintf("line %d %d with some more text to fill sections", i, j),
				StructuredMetadata: []push.LabelAdapter{
					{Name: "trace_id", Value: fmt.Sprintf("t%d-%d", i, j)},
					{Name: "level", Value: "info"},
					{Name: fmt.Sprintf("only_in_%d", i%3), Value: "x"},
				},
			})
		}
		builder.Append(ctx, logproto.Stream{Labels: fmt.Sprintf(`{app="a", idx="%d"}`, i), Entries: entries})
	}
	builder.Close()

	resp, err := builder.Metastore().Sections(ctx, metastore.SectionsRequest{
		Start:    time.Unix(0, 0).UTC(),
		End:      time.Unix(1000, 0).UTC(),
		Matchers: syntax.MustParseLogSelector(`{app="a"}`, true).Matchers(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Sections)

	obj := &testObject{bucket: builder.Location().Bucket, path: resp.Sections[0].ObjectPath}
	rc, err := obj.bucket.Get(ctx, obj.path)
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()
	obj.raw, err = io.ReadAll(rc)
	require.NoError(t, err)

	obj.metadataRegionEnd = objtest.MetadataRegionEnd(t, obj.bucket, obj.path)
	require.Greater(t, obj.metadataRegionEnd, int64(headBytes), "the metadata must not fit in the first read")
	return obj
}

func (o *testObject) request(prefetchSet metadata.PrefetchSet) metadata.PrefetchRequest {
	return metadata.PrefetchRequest{Path: o.path, PrefetchSet: prefetchSet, Source: &testSource{bucket: o.bucket, path: o.path}}
}

// requireMatches checks that the ranges of res are sorted, do not overlap, start at offset 0, and equal
// the bytes of the object.
func (o *testObject) requireMatches(t *testing.T, res metadata.PrefetchResponse) {
	t.Helper()

	require.NotEmpty(t, res.PrefetchedRanges)
	require.Zero(t, res.PrefetchedRanges[0].Offset)
	end := int64(0)
	for _, r := range res.PrefetchedRanges {
		require.GreaterOrEqual(t, r.Offset, end, "ranges must be sorted and must not overlap")
		end = r.Offset + int64(len(r.Data))
		require.Equal(t, o.raw[r.Offset:end], r.Data, "range at %d", r.Offset)
	}
}

// readRows opens the object, optionally through p, and returns the rows of every logs section with the
// given columns.
func (o *testObject) readRows(t *testing.T, bucket objstore.Bucket, p metadata.Prefetcher, prefetchSet metadata.PrefetchSet, types []logs.ColumnType, names []string) []string {
	t.Helper()

	var opts []dataobj.OpenOption
	if p != nil {
		opts = append(opts, dataobj.WithMetadataPrefetcher(p, prefetchSet))
	}
	opened, err := dataobj.FromBucket(t.Context(), bucket, o.path, 0, opts...)
	require.NoError(t, err)

	var rows []string
	for _, sec := range opened.Sections() {
		if !logs.CheckSection(sec) {
			continue
		}
		section, err := logs.Open(t.Context(), sec)
		require.NoError(t, err)

		reader := logs.NewRowReader(section)
		require.NoError(t, reader.SetProjectedColumns(types, names))
		require.NoError(t, reader.Open(t.Context()))

		buf := make([]logs.Record, 128)
		for {
			n, err := reader.Read(t.Context(), buf)
			for _, r := range buf[:n] {
				rows = append(rows, fmt.Sprintf("%d %d %s %s", r.StreamID, r.Timestamp.UnixNano(), r.Metadata, r.Line))
			}
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}
		require.NoError(t, reader.Close())
	}
	return rows
}

func newPrefetcher(t *testing.T, backend cache.Cache, mutate func(*metadata.PrefetcherConfig)) *metadata.CachedPrefetcher {
	t.Helper()
	p, _ := newPrefetcherWithRegistry(t, backend, mutate)
	return p
}

// newPrefetcherWithRegistry returns a prefetcher that has its own registry, so a test reads only its own
// metrics.
func newPrefetcherWithRegistry(t *testing.T, backend cache.Cache, mutate func(*metadata.PrefetcherConfig)) (*metadata.CachedPrefetcher, *prometheus.Registry) {
	t.Helper()

	reg := prometheus.NewPedanticRegistry()
	p, err := metadata.NewCachedPrefetcher(backend, 0, testConfig(mutate), reg, nil)
	require.NoError(t, err)
	return p, reg
}

func newPrefetcherWithMaxRegionBytes(t *testing.T, backend cache.Cache, maxRegionBytes int) *metadata.CachedPrefetcher {
	t.Helper()
	return newPrefetcher(t, backend, func(cfg *metadata.PrefetcherConfig) { cfg.MaxRegionBytes = maxRegionBytes })
}

func testConfig(mutate func(*metadata.PrefetcherConfig)) metadata.PrefetcherConfig {
	cfg := metadata.PrefetcherConfig{
		Enabled:             true,
		Compression:         "zstd",
		MinCompressionRatio: 1.5,
		HeadBytes:           headBytes,
		MaxRegionBytes:      16 << 20,
		LoadTimeout:         30 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return cfg
}

// prefetcherCounts holds the values of the metrics of a CachedPrefetcher. The histograms have the
// number of their samples, and for two of them also the sum of their samples.
type prefetcherCounts struct {
	requests                                   float64
	cacheHits, cachePartialMisses, cacheMisses float64

	// failures holds the failures by reason. It has no reason without a failure.
	failures map[string]float64

	durations, lookupKeys, responseBytes uint64
	lookupKeysSum, responseBytesSum      float64
}

func gatherPrefetcherCounts(t *testing.T, reg *prometheus.Registry) prefetcherCounts {
	t.Helper()

	const prefix = "loki_dataobj_metadata_prefetcher_"
	mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
	require.NoError(t, err)

	var failures map[string]float64
	for _, reason := range []string{"canceled", "timeout", "cache_timeout", "cache_unavailable", "too_large", "head_too_small", "unsupported", "corrupted", "storage_timeout", "storage_error"} {
		n := counterWithLabels(t, mfm, prefix+"failures_total", "reason", reason)
		if n == 0 {
			continue
		}
		if failures == nil {
			failures = map[string]float64{}
		}
		failures[reason] = n
	}
	keys, err := metrics.FindHistogramWithNameAndLabels(mfm, prefix+"cache_lookup_keys")
	require.NoError(t, err)
	size, err := metrics.FindHistogramWithNameAndLabels(mfm, prefix+"response_bytes")
	require.NoError(t, err)
	return prefetcherCounts{
		requests:           mfm.SumCounters(prefix + "requests_total"),
		cacheHits:          counterWithLabels(t, mfm, prefix+"successes_total", "outcome", "cache_hit"),
		cachePartialMisses: counterWithLabels(t, mfm, prefix+"successes_total", "outcome", "cache_partial_miss"),
		cacheMisses:        counterWithLabels(t, mfm, prefix+"successes_total", "outcome", "cache_miss"),
		failures:           failures,
		durations:          mfm.SumHistograms(prefix + "duration_seconds").Count(),
		lookupKeys:         keys.GetSampleCount(),
		lookupKeysSum:      keys.GetSampleSum(),
		responseBytes:      size.GetSampleCount(),
		responseBytesSum:   size.GetSampleSum(),
	}
}

// counterWithLabels returns the value of the one counter of the family name that has the labels.
// The dskit metrics package has no such helper, so it builds on FindMetricsInFamilyMatchingLabels.
func counterWithLabels(t *testing.T, mfm metrics.MetricFamilyMap, name string, labelNamesAndValues ...string) float64 {
	t.Helper()

	matching := metrics.FindMetricsInFamilyMatchingLabels(mfm[name], labelNamesAndValues...)
	require.Len(t, matching, 1, "series of %s with labels %v", name, labelNamesAndValues)
	return matching[0].GetCounter().GetValue()
}

type fakeCache struct {
	mu     sync.Mutex
	values map[string][]byte

	// failReads and failWrites make the reads and the writes return an error. timeoutReads makes the
	// reads return a socket timeout.
	failReads, failWrites, timeoutReads bool

	lookupCalls, setItems int
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string][]byte{}} }

func (s *fakeCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// A lookup asks for several keys. The read of the whole region asks for one.
	if len(keys) > 1 {
		s.lookupCalls++
	}
	if s.failReads {
		return nil, nil, nil, errors.New("cache is down")
	}
	if s.timeoutReads {
		return nil, nil, nil, socketTimeoutError()
	}
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			found, bufs = append(found, key), append(bufs, value)
		} else {
			missing = append(missing, key)
		}
	}
	return found, bufs, missing, nil
}

func (s *fakeCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setItems += len(keys)
	if s.failWrites {
		return errors.New("cache is down")
	}
	for i, key := range keys {
		s.values[key] = bufs[i]
	}
	return nil
}

func (s *fakeCache) Stop() {}

func (s *fakeCache) GetCacheType() stats.CacheType { return "fake" }

func (s *fakeCache) lookups() int { s.mu.Lock(); defer s.mu.Unlock(); return s.lookupCalls }
func (s *fakeCache) sets() int    { s.mu.Lock(); defer s.mu.Unlock(); return s.setItems }

func (s *fakeCache) resetCounts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookupCalls, s.setItems = 0, 0
}

// entry returns the name of an entry whose cache item holds an unhashed key with part after the "#",
// or "" when the cache has none.
func (s *fakeCache) entry(part string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, value := range s.values {
		if strings.Contains(itemKey(value), "#"+part) {
			return name
		}
	}
	return ""
}

// drop deletes every entry whose cache item holds an unhashed key with part after the "#".
func (s *fakeCache) drop(part string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, value := range s.values {
		if strings.Contains(itemKey(value), "#"+part) {
			delete(s.values, name)
		}
	}
}

// corrupt changes the last byte of an entry whose cache item holds an unhashed key with part after
// the "#".
func (s *fakeCache) corrupt(part string) {
	name := s.entry(part)
	s.mu.Lock()
	defer s.mu.Unlock()
	value := bytes.Clone(s.values[name])
	value[len(value)-1] ^= 0xff
	s.values[name] = value
}

// copyEntry stores the value of the entry that entry(from) finds under the name of the entry that
// entry(to) finds.
func (s *fakeCache) copyEntry(from, to string) {
	fromName, toName := s.entry(from), s.entry(to)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[toName] = bytes.Clone(s.values[fromName])
}

// partialResultsCache returns the items it found together with an error, like a cache client that
// loses a part of a multi-key lookup.
type partialResultsCache struct{ *fakeCache }

func (c partialResultsCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	found, bufs, missing, err := c.fakeCache.Fetch(ctx, keys)
	if err != nil {
		return nil, nil, nil, err
	}
	return found, bufs, missing, errors.New("part of the lookup failed")
}

// itemKey returns the unhashed cache key that a cache item holds, or "" when the item is malformed. A
// cache item starts with a version byte, a codec byte, the raw size as a uvarint and a 4 byte checksum.
// The key follows with its length as a uvarint.
func itemKey(item []byte) string {
	if len(item) < 2 {
		return ""
	}
	_, n := binary.Uvarint(item[2:])
	if n <= 0 || len(item) < 2+n+4 {
		return ""
	}
	rest := item[2+n+4:]
	length, n := binary.Uvarint(rest)
	if n <= 0 || length > uint64(len(rest)-n) {
		return ""
	}
	return string(rest[n : n+int(length)])
}

type readCall struct{ offset, length int64 }

// testSource reads an object from a bucket and records the reads.
type testSource struct {
	bucket objstore.Bucket
	path   string

	// data, when set, replaces the object.
	data []byte

	// block makes a read wait until its context ends. gate makes a read wait until it is closed.
	block bool
	gate  chan struct{}

	// err makes every read fail. closeErr makes closing the reader of a read fail.
	err      error
	closeErr error

	mu    sync.Mutex
	reads []readCall
}

func (s *testSource) ReadRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	s.mu.Lock()
	s.reads = append(s.reads, readCall{offset, length})
	s.mu.Unlock()

	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.data != nil {
		end := min(offset+length, int64(len(s.data)))
		return s.withCloseErr(io.NopCloser(bytes.NewReader(s.data[min(offset, end):end]))), nil
	}
	rc, err := s.bucket.GetRange(ctx, s.path, offset, length)
	if err != nil {
		return nil, err
	}
	return s.withCloseErr(rc), nil
}

// withCloseErr returns rc, or a reader whose Close also returns s.closeErr.
func (s *testSource) withCloseErr(rc io.ReadCloser) io.ReadCloser {
	if s.closeErr == nil {
		return rc
	}
	return failingCloser{ReadCloser: rc, err: s.closeErr}
}

type failingCloser struct {
	io.ReadCloser
	err error
}

func (c failingCloser) Close() error {
	return errors.Join(c.ReadCloser.Close(), c.err)
}

func (s *testSource) calls() []readCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]readCall(nil), s.reads...)
}

func (s *testSource) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads = nil
}

type recordingBucket struct {
	objstore.Bucket

	mu    sync.Mutex
	reads []readCall
}

func (b *recordingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.mu.Lock()
	b.reads = append(b.reads, readCall{off, length})
	b.mu.Unlock()
	return b.Bucket.GetRange(ctx, name, off, length)
}

// readsBelow counts the reads that start before end.
func (b *recordingBucket) readsBelow(end int64) (n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.reads {
		if r.offset < end {
			n++
		}
	}
	return n
}

// socketTimeoutError returns the error that a client gets when a socket read passes its deadline.
func socketTimeoutError() error {
	return &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
}
