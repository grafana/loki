package bloomshipper

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func makeMetas(t *testing.T, schemaCfg config.SchemaConfig, ts model.Time, keyspaces []v1.FingerprintBounds) []Meta {
	t.Helper()

	metas := make([]Meta, len(keyspaces))
	for i, keyspace := range keyspaces {
		metas[i] = Meta{
			MetaRef: MetaRef{
				Ref: Ref{
					TenantID:       "fake",
					TableName:      fmt.Sprintf("%s%d", schemaCfg.Configs[0].IndexTables.Prefix, 0),
					Bounds:         keyspace,
					StartTimestamp: ts,
					EndTimestamp:   ts,
				},
			},
			Blocks: []BlockRef{},
		}
	}
	return metas
}

func TestMetasFetcher(t *testing.T) {
	dir := t.TempDir()
	logger := log.NewNopLogger()
	now := model.Now()

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: 0},
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "table_",
						Period: 24 * time.Hour,
					},
				},
				ChunkTables: config.PeriodicTableConfig{},
				RowShards:   16,
			},
		},
	}

	tests := []struct {
		name  string
		store []Meta // initial store state
		start []Meta // initial cache state
		end   []Meta // final cache state
		fetch []Meta // metas to fetch
		err   error  // error that is returned when calling cache.Fetch()
	}{
		{
			name:  "all metas found in cache",
			store: []Meta{},
			start: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			end:   makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			fetch: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
		},
		{
			name:  "no metas found in cache",
			store: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			start: []Meta{},
			end:   makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			fetch: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
		},
		{
			name:  "some metas found in cache",
			store: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}, {Min: 0x10000, Max: 0x1ffff}}),
			start: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			end:   makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}, {Min: 0x10000, Max: 0x1ffff}}),
			fetch: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}, {Min: 0x10000, Max: 0x1ffff}}),
		},
		{
			name:  "error fetching metas yields empty result",
			err:   errors.New("failed to fetch"),
			store: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}, {Min: 0x10000, Max: 0x1ffff}}),
			start: makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			end:   makeMetas(t, schemaCfg, now, []v1.FingerprintBounds{{Min: 0x0000, Max: 0xffff}}),
			fetch: []Meta{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			metasCache := cache.NewMockCache()
			metasCache.SetErr(nil, test.err)

			cfg := bloomStoreConfig{workingDirs: []string{t.TempDir()}, numWorkers: 1}

			oc, err := local.NewFSObjectClient(local.FSConfig{Directory: dir})
			require.NoError(t, err)

			c, err := NewBloomClient(cfg, oc, logger)
			require.NoError(t, err)

			fetcher, err := NewFetcher(cfg, c, metasCache, nil, nil, logger, v1.NewMetrics(nil))
			require.NoError(t, err)

			// prepare metas cache
			keys := make([]string, 0, len(test.start))
			metas := make([][]byte, 0, len(test.start))
			for _, meta := range test.start {
				b, err := json.Marshal(meta)
				require.NoError(t, err)
				metas = append(metas, b)

				k := meta.String()
				keys = append(keys, k)
			}
			require.NoError(t, metasCache.Store(ctx, keys, metas))

			// prepare store
			for _, meta := range test.store {
				err := c.PutMeta(ctx, meta)
				require.NoError(t, err)
			}

			actual, err := fetcher.FetchMetas(ctx, metaRefs(test.fetch))
			require.NoError(t, err)
			require.ElementsMatch(t, test.fetch, actual)

			requireCachedMetas(t, test.end, metasCache.GetInternal())
		})
	}
}

func TestFetchOptions(t *testing.T) {
	options := &options{
		ignoreNotFound: false,
		fetchAsync:     false,
	}

	options.apply(WithFetchAsync(true), WithIgnoreNotFound(true))

	require.True(t, options.fetchAsync)
	require.True(t, options.ignoreNotFound)
}

func TestFetcher_DownloadQueue(t *testing.T) {
	t.Run("invalid arguments", func(t *testing.T) {
		for _, tc := range []struct {
			size, workers int
			err           string
		}{
			{
				size: 0, workers: 1, err: "queue size needs to be greater than 0",
			},
			{
				size: 1, workers: 0, err: "queue requires at least 1 worker",
			},
		} {
			t.Run(tc.err, func(t *testing.T) {
				_, err := newDownloadQueue[bool, bool](
					tc.size,
					tc.workers,
					func(_ context.Context, _ downloadRequest[bool, bool]) {},
					log.NewNopLogger(),
				)
				require.ErrorContains(t, err, tc.err)
			})
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx := context.Background()

		q, err := newDownloadQueue[bool, bool](
			100,
			1,
			func(_ context.Context, _ downloadRequest[bool, bool]) {},
			log.NewNopLogger(),
		)
		require.NoError(t, err)
		t.Cleanup(q.close)

		ctx, cancel := context.WithCancel(ctx)
		cancel()

		resultsCh := make(chan downloadResponse[bool], 1)
		errorsCh := make(chan error, 1)

		r := downloadRequest[bool, bool]{
			ctx:     ctx,
			item:    false,
			key:     "test",
			idx:     0,
			results: resultsCh,
			errors:  errorsCh,
		}
		q.enqueue(r)

		select {
		case err := <-errorsCh:
			require.Error(t, err)
		case res := <-resultsCh:
			require.False(t, true, "got %+v should have received an error instead", res)
		}

	})

	t.Run("process function is called with context and request as arguments", func(t *testing.T) {
		ctx := context.Background()

		q, err := newDownloadQueue[bool, bool](
			100,
			1,
			func(_ context.Context, r downloadRequest[bool, bool]) {
				r.results <- downloadResponse[bool]{
					key:  r.key,
					idx:  r.idx,
					item: true,
				}
			},
			log.NewNopLogger(),
		)
		require.NoError(t, err)
		t.Cleanup(q.close)

		resultsCh := make(chan downloadResponse[bool], 1)
		errorsCh := make(chan error, 1)

		r := downloadRequest[bool, bool]{
			ctx:     ctx,
			item:    false,
			key:     "test",
			idx:     0,
			results: resultsCh,
			errors:  errorsCh,
		}
		q.enqueue(r)

		select {
		case err := <-errorsCh:
			require.False(t, true, "got %+v should have received a response instead", err)
		case res := <-resultsCh:
			require.True(t, res.item)
			require.Equal(t, r.key, res.key)
			require.Equal(t, r.idx, res.idx)
		}

	})

	t.Run("download multiple items and return in order", func(t *testing.T) {
		ctx := context.Background()

		q, err := newDownloadQueue[bool, bool](
			100,
			1,
			func(_ context.Context, r downloadRequest[bool, bool]) {
				r.results <- downloadResponse[bool]{
					key:  r.key,
					idx:  r.idx,
					item: true,
				}
			},
			log.NewNopLogger(),
		)
		require.NoError(t, err)

		count := 10
		resultsCh := make(chan downloadResponse[bool], count)
		errorsCh := make(chan error, count)

		reqs := buildDownloadRequest(ctx, count, resultsCh, errorsCh)
		for _, r := range reqs {
			q.enqueue(r)
		}

		for i := 0; i < count; i++ {
			select {
			case err := <-errorsCh:
				require.False(t, true, "got %+v should have received a response instead", err)
			case res := <-resultsCh:
				require.True(t, res.item)
				require.Equal(t, reqs[i].key, res.key)
				require.Equal(t, reqs[i].idx, res.idx)
			}
		}
	})
}

func buildDownloadRequest(ctx context.Context, count int, resCh chan downloadResponse[bool], errCh chan error) []downloadRequest[bool, bool] {
	requests := make([]downloadRequest[bool, bool], count)
	for i := 0; i < count; i++ {
		requests[i] = downloadRequest[bool, bool]{
			ctx:     ctx,
			item:    false,
			key:     "test",
			idx:     i,
			results: resCh,
			errors:  errCh,
		}
	}
	return requests
}

func TestFetcher_LoadBlocksFromFS(t *testing.T) {
	base := t.TempDir()
	cfg := bloomStoreConfig{workingDirs: []string{base}, numWorkers: 1}
	resolver := NewPrefixedResolver(base, defaultKeyResolver{})

	refs := []BlockRef{
		// no directory for block
		{Ref: Ref{TenantID: "tenant", TableName: "12345", Bounds: v1.NewBounds(0x0000, 0x0fff)}, Codec: compression.None},
		// invalid directory for block
		{Ref: Ref{TenantID: "tenant", TableName: "12345", Bounds: v1.NewBounds(0x1000, 0x1fff)}, Codec: compression.Snappy},
		// valid directory for block
		{Ref: Ref{TenantID: "tenant", TableName: "12345", Bounds: v1.NewBounds(0x2000, 0x2fff)}, Codec: compression.GZIP},
	}
	dirs := []string{
		localFilePathWithoutExtension(refs[0], resolver),
		localFilePathWithoutExtension(refs[1], resolver),
		localFilePathWithoutExtension(refs[2], resolver),
	}

	createBlockDir(t, dirs[1])
	_ = os.Remove(filepath.Join(dirs[1], "bloom")) // remove file to make it invalid

	createBlockDir(t, dirs[2])

	oc, err := local.NewFSObjectClient(local.FSConfig{Directory: base})
	require.NoError(t, err)
	c, err := NewBloomClient(cfg, oc, log.NewNopLogger())
	require.NoError(t, err)

	fetcher, err := NewFetcher(cfg, c, nil, nil, nil, log.NewNopLogger(), v1.NewMetrics(nil))
	require.NoError(t, err)

	found, missing, err := fetcher.loadBlocksFromFS(context.Background(), refs)
	require.NoError(t, err)

	require.Len(t, found, 1)
	require.Len(t, missing, 2)

	require.Equal(t, refs[2].Ref, found[0].Ref)
	require.ElementsMatch(t, refs[0:2], missing)
}

func createBlockDir(t *testing.T, path string) {
	_ = os.MkdirAll(path, 0755)

	fp, err := os.Create(filepath.Join(path, v1.BloomFileName))
	require.NoError(t, err)
	_ = fp.Close()

	fp, err = os.Create(filepath.Join(path, v1.SeriesFileName))
	require.NoError(t, err)
	_ = fp.Close()
}

func TestFetcher_IsBlockDir(t *testing.T) {
	cfg := bloomStoreConfig{
		numWorkers:  1,
		workingDirs: []string{t.TempDir()},
	}

	fetcher, err := NewFetcher(cfg, nil, nil, nil, nil, log.NewNopLogger(), v1.NewMetrics(nil))
	require.NoError(t, err)

	t.Run("path does not exist", func(t *testing.T) {
		base := t.TempDir()
		exists, _ := fetcher.isBlockDir(filepath.Join(base, "doesnotexist"))
		require.False(t, exists)
	})

	t.Run("path is not a directory", func(t *testing.T) {
		base := t.TempDir()
		fp, err := os.Create(filepath.Join(base, "block"))
		require.NoError(t, err)
		_ = fp.Close()
		exists, _ := fetcher.isBlockDir(filepath.Join(base, "block"))
		require.False(t, exists)
	})

	t.Run("bloom file does not exist", func(t *testing.T) {
		base := t.TempDir()
		dir := filepath.Join(base, "block")
		_ = os.MkdirAll(dir, 0755)
		fp, err := os.Create(filepath.Join(dir, v1.SeriesFileName))
		require.NoError(t, err)
		_ = fp.Close()
		exists, _ := fetcher.isBlockDir(dir)
		require.False(t, exists)
	})

	t.Run("series file does not exist", func(t *testing.T) {
		base := t.TempDir()
		dir := filepath.Join(base, "block")
		_ = os.MkdirAll(dir, 0755)
		fp, err := os.Create(filepath.Join(dir, v1.BloomFileName))
		require.NoError(t, err)
		_ = fp.Close()
		exists, _ := fetcher.isBlockDir(dir)
		require.False(t, exists)
	})

	t.Run("valid directory", func(t *testing.T) {
		base := t.TempDir()
		dir := filepath.Join(base, "block")
		_ = os.MkdirAll(dir, 0755)
		fp, err := os.Create(filepath.Join(dir, v1.BloomFileName))
		require.NoError(t, err)
		_ = fp.Close()
		fp, err = os.Create(filepath.Join(dir, v1.SeriesFileName))
		require.NoError(t, err)
		_ = fp.Close()
		exists, _ := fetcher.isBlockDir(dir)
		require.True(t, exists)
	})
}

func Benchmark_Fetcher_processMetasCacheResponse(b *testing.B) {
	b.Log(b.N)

	cfg := bloomStoreConfig{
		numWorkers:  1,
		workingDirs: []string{b.TempDir()},
	}

	c, err := NewBloomClient(cfg, nil, log.NewNopLogger())
	require.NoError(b, err)
	f, err := NewFetcher(cfg, c, nil, nil, nil, log.NewNopLogger(), v1.NewMetrics(nil))
	require.NoError(b, err)

	step := math.MaxUint64 / uint64(b.N)

	refs := make([]MetaRef, 0, b.N)
	keys := make([]string, 0, b.N)
	bufs := make([][]byte, 0, b.N)

	for i := 0; i < b.N; i++ {
		minFp := model.Fingerprint(uint64(i) * step)
		maxFp := model.Fingerprint(uint64(i)*step + step)

		ref := Ref{
			TenantID:       "fake",
			TableName:      "tsdb_index_20000",
			Bounds:         v1.NewBounds(minFp, maxFp),
			StartTimestamp: model.Earliest,
			EndTimestamp:   model.Latest,
		}
		metaRef := MetaRef{Ref: ref}
		refs = append(refs, metaRef)

		keys = append(keys, f.client.Meta(metaRef).Addr())

		meta := Meta{
			MetaRef: metaRef,
			Blocks:  []BlockRef{{Ref: ref}},
		}

		buf, _ := json.Marshal(meta)
		bufs = append(bufs, buf)
	}

	b.ReportAllocs()
	b.ResetTimer()

	_, _, _ = f.processMetasCacheResponse(context.TODO(), refs, keys, bufs)
}

func metaRefs(metas []Meta) []MetaRef {
	refs := make([]MetaRef, 0, len(metas))
	for _, meta := range metas {
		refs = append(refs, meta.MetaRef)
	}
	return refs
}

func requireEqualMetas(t *testing.T, expected []Meta, actual []MetaRef) {
	require.Equal(t, len(expected), len(actual))
	require.ElementsMatch(t, metaRefs(expected), actual)
}

func requireCachedMetas(t *testing.T, expected []Meta, actual map[string][]byte) {
	require.Equal(t, len(expected), len(actual))
	for _, meta := range expected {
		_, contains := actual[meta.String()]
		require.True(t, contains)
	}
}
