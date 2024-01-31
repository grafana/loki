package bloomshipper

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
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
					MinFingerprint: uint64(keyspace.Min),
					MaxFingerprint: uint64(keyspace.Max),
					StartTimestamp: ts,
					EndTimestamp:   ts,
				},
			},
			Tombstones: []BlockRef{},
			Blocks:     []BlockRef{},
		}
		metas[i].FilePath = externalMetaKey(metas[i].MetaRef)
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

			oc, err := local.NewFSObjectClient(local.FSConfig{Directory: dir})
			require.NoError(t, err)

			c, err := NewBloomClient(oc, logger)
			require.NoError(t, err)

			fetcher, err := NewFetcher(c, metasCache, nil, logger)
			require.NoError(t, err)

			// prepare metas cache
			keys := make([]string, 0, len(test.start))
			metas := make([][]byte, 0, len(test.start))
			for _, meta := range test.start {
				b, err := json.Marshal(meta)
				require.NoError(t, err)
				metas = append(metas, b)
				t.Log(string(b))

				k := externalMetaKey(meta.MetaRef)
				keys = append(keys, k)
			}
			require.NoError(t, metasCache.Store(ctx, keys, metas))

			// prepare store
			for _, meta := range test.store {
				meta.FilePath = path.Join(dir, meta.FilePath)
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

func metasFromCache(data map[string][]byte) []Meta {
	metas := make([]Meta, 0, len(data))
	for k, v := range data {
		meta := Meta{
			MetaRef: MetaRef{
				FilePath: k,
			},
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
		_, contains := actual[meta.MetaRef.FilePath]
		require.True(t, contains)
	}
}
