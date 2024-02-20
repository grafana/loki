package bloomshipper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			metasCache := cache.NewMockCache()
			cfg := bloomStoreConfig{workingDir: t.TempDir(), numWorkers: 1}

			oc, err := local.NewFSObjectClient(local.FSConfig{Directory: dir})
			require.NoError(t, err)

			c, err := NewBloomClient(cfg, oc, logger)
			require.NoError(t, err)

			fetcher, err := NewFetcher(cfg, c, metasCache, nil, logger)
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

func TestFetcher_LoadBlocksFromFS(t *testing.T) {
	base := t.TempDir()
	cfg := bloomStoreConfig{workingDir: base, numWorkers: 1}
	resolver := NewPrefixedResolver(base, defaultKeyResolver{})

	refs := []BlockRef{
		// no directory for block
		{Ref: Ref{TenantID: "tenant", TableName: "12345", Bounds: v1.NewBounds(0x0000, 0x0fff)}},
		// invalid directory for block
		{Ref: Ref{TenantID: "tenant", TableName: "12345", Bounds: v1.NewBounds(0x1000, 0x1fff)}},
		// valid directory for block
		{Ref: Ref{TenantID: "tenant", TableName: "12345", Bounds: v1.NewBounds(0x2000, 0x2fff)}},
	}
	dirs := []string{
		resolver.Block(refs[0]).LocalPath(),
		resolver.Block(refs[1]).LocalPath(),
		resolver.Block(refs[2]).LocalPath(),
	}

	createBlockDir(t, dirs[1])
	_ = os.Remove(filepath.Join(dirs[1], "bloom")) // remove file to make it invalid

	createBlockDir(t, dirs[2])

	oc, err := local.NewFSObjectClient(local.FSConfig{Directory: base})
	require.NoError(t, err)
	c, err := NewBloomClient(cfg, oc, log.NewNopLogger())
	require.NoError(t, err)

	fetcher, err := NewFetcher(cfg, c, nil, nil, log.NewNopLogger())
	require.NoError(t, err)

	found, missing, err := fetcher.loadBlocksFromFS(context.Background(), refs)
	require.NoError(t, err)

	require.Len(t, found, 1)
	require.Len(t, missing, 2)

	require.Equal(t, refs[2], found[0].BlockRef)
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
	fetcher, _ := NewFetcher(bloomStoreConfig{}, nil, nil, nil, log.NewNopLogger())

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

func metasFromCache(data map[string][]byte) []Meta {
	metas := make([]Meta, 0, len(data))
	for _, v := range data {
		meta := Meta{
			MetaRef: MetaRef{},
		}
		_ = json.Unmarshal(v, &meta)
		metas = append(metas, meta)
	}
	return metas
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
