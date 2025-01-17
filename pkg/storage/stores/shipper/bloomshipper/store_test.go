package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

func newMockBloomStore(t *testing.T) (*BloomStore, string, error) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "bloomshipper")
	storeDir := filepath.Join(dir, "fs-storage")
	return newMockBloomStoreWithWorkDir(t, workDir, storeDir)
}

func newMockBloomStoreWithWorkDir(t *testing.T, workDir, storeDir string) (*BloomStore, string, error) {
	periodicConfigs := []storageconfig.PeriodConfig{
		{
			ObjectType: types.StorageTypeFileSystem,
			From:       parseDayTime("2024-01-01"),
			IndexTables: storageconfig.IndexPeriodicTableConfig{
				PeriodicTableConfig: storageconfig.PeriodicTableConfig{
					Period: 24 * time.Hour,
					Prefix: "schema_a_table_",
				}},
		},
		{
			ObjectType: types.StorageTypeFileSystem,
			From:       parseDayTime("2024-02-01"),
			IndexTables: storageconfig.IndexPeriodicTableConfig{
				PeriodicTableConfig: storageconfig.PeriodicTableConfig{
					Period: 24 * time.Hour,
					Prefix: "schema_b_table_",
				}},
		},
	}

	storageConfig := storage.Config{
		FSConfig: local.FSConfig{
			Directory: storeDir,
		},
		BloomShipperConfig: config.Config{
			WorkingDirectory:    []string{workDir},
			DownloadParallelism: 1,
			BlocksCache: config.BlocksCacheConfig{
				SoftLimit:     1 << 20,
				HardLimit:     2 << 20,
				TTL:           time.Hour,
				PurgeInterval: time.Hour,
			},
		},
	}

	reg := prometheus.NewPedanticRegistry()
	metrics := storage.NewClientMetrics()
	t.Cleanup(metrics.Unregister)
	logger := log.NewLogfmtLogger(os.Stderr)

	metasCache := cache.NewMockCache()
	blocksCache := NewFsBlocksCache(storageConfig.BloomShipperConfig.BlocksCache, prometheus.NewPedanticRegistry(), logger)
	store, err := NewBloomStore(periodicConfigs, storageConfig, metrics, metasCache, blocksCache, &mempool.SimpleHeapAllocator{}, reg, logger)
	if err == nil {
		t.Cleanup(store.Stop)
	}

	return store, workDir, err
}

func createMetaInStorage(store *BloomStore, tenant string, start model.Time, minFp, maxFp model.Fingerprint) (Meta, error) {
	meta := Meta{
		MetaRef: MetaRef{
			Ref: Ref{
				TenantID: tenant,
				Bounds:   v1.NewBounds(minFp, maxFp),
				// Unused
				// StartTimestamp: start,
				// EndTimestamp:   start.Add(12 * time.Hour),
			},
		},
		Blocks: []BlockRef{},
	}
	err := store.storeDo(start, func(s *bloomStoreEntry) error {
		raw, _ := json.Marshal(meta)
		meta.MetaRef.Ref.TableName = tablesForRange(s.cfg, NewInterval(start, start.Add(12*time.Hour)))[0]
		return s.objectClient.PutObject(context.Background(), s.Meta(meta.MetaRef).Addr(), bytes.NewReader(raw))
	})
	return meta, err
}

func createBlockInStorage(t *testing.T, store *BloomStore, tenant string, start model.Time, minFp, maxFp model.Fingerprint) (Block, error) {
	tmpDir := t.TempDir()
	fp, _ := os.CreateTemp(t.TempDir(), "*.tar")

	blockWriter := v1.NewDirectoryBlockWriter(tmpDir)
	err := blockWriter.Init()
	require.NoError(t, err)

	enc := compression.GZIP
	err = v1.TarCompress(enc, fp, v1.NewDirectoryBlockReader(tmpDir))
	require.NoError(t, err)

	_, _ = fp.Seek(0, 0)

	block := Block{
		BlockRef: BlockRef{
			Ref: Ref{
				TenantID:       tenant,
				Bounds:         v1.NewBounds(minFp, maxFp),
				StartTimestamp: start,
				EndTimestamp:   start.Add(12 * time.Hour),
			},
			Codec: enc,
		},
		Data: fp,
	}
	err = store.storeDo(start, func(s *bloomStoreEntry) error {
		block.BlockRef.Ref.TableName = tablesForRange(s.cfg, NewInterval(start, start.Add(12*time.Hour)))[0]
		return s.objectClient.PutObject(context.Background(), s.Block(block.BlockRef).Addr(), block.Data)
	})
	return block, err
}

func TestBloomStore_ResolveMetas(t *testing.T) {
	store, _, err := newMockBloomStore(t)
	require.NoError(t, err)

	// schema 1
	// outside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-01-19 00:00"), 0x00010000, 0x0001ffff)
	// outside of interval, inside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-01-19 00:00"), 0x00000000, 0x0000ffff)
	// inside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-01-20 00:00"), 0x00010000, 0x0001ffff)
	// inside of interval, inside of bounds
	m1, _ := createMetaInStorage(store, "tenant", parseTime("2024-01-20 00:00"), 0x00000000, 0x0000ffff)

	// schema 2
	// inside of interval, inside of bounds
	m2, _ := createMetaInStorage(store, "tenant", parseTime("2024-02-05 00:00"), 0x00000000, 0x0000ffff)
	// inside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-02-05 00:00"), 0x00010000, 0x0001ffff)
	// outside of interval, inside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-02-11 00:00"), 0x00000000, 0x0000ffff)
	// outside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-02-11 00:00"), 0x00010000, 0x0001ffff)

	t.Run("tenant matches", func(t *testing.T) {
		ctx := context.Background()
		params := MetaSearchParams{
			"tenant",
			NewInterval(parseTime("2024-01-20 00:00"), parseTime("2024-02-10 00:00")),
			v1.NewBounds(0x00000000, 0x0000ffff),
		}

		refs, fetchers, err := store.ResolveMetas(ctx, params)
		require.NoError(t, err)
		require.Len(t, refs, 2)
		require.Len(t, fetchers, 2)

		require.Equal(t, [][]MetaRef{{m1.MetaRef}, {m2.MetaRef}}, refs)
	})

	t.Run("tenant does not match", func(t *testing.T) {
		ctx := context.Background()
		params := MetaSearchParams{
			"other",
			NewInterval(parseTime("2024-01-20 00:00"), parseTime("2024-02-10 00:00")),
			v1.NewBounds(0x00000000, 0x0000ffff),
		}

		refs, fetchers, err := store.ResolveMetas(ctx, params)
		require.NoError(t, err)
		require.Len(t, refs, 0)
		require.Len(t, fetchers, 0)
		require.Equal(t, [][]MetaRef{}, refs)
	})
}

func TestBloomStore_FetchMetas(t *testing.T) {
	store, _, err := newMockBloomStore(t)
	require.NoError(t, err)

	// schema 1
	// outside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-01-19 00:00"), 0x00010000, 0x0001ffff)
	// outside of interval, inside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-01-19 00:00"), 0x00000000, 0x0000ffff)
	// inside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-01-20 00:00"), 0x00010000, 0x0001ffff)
	// inside of interval, inside of bounds
	m1, _ := createMetaInStorage(store, "tenant", parseTime("2024-01-20 00:00"), 0x00000000, 0x0000ffff)

	// schema 2
	// inside of interval, inside of bounds
	m2, _ := createMetaInStorage(store, "tenant", parseTime("2024-02-05 00:00"), 0x00000000, 0x0000ffff)
	// inside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-02-05 00:00"), 0x00010000, 0x0001ffff)
	// outside of interval, inside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-02-11 00:00"), 0x00000000, 0x0000ffff)
	// outside of interval, outside of bounds
	_, _ = createMetaInStorage(store, "tenant", parseTime("2024-02-11 00:00"), 0x00010000, 0x0001ffff)

	t.Run("tenant matches", func(t *testing.T) {
		ctx := context.Background()
		params := MetaSearchParams{
			"tenant",
			NewInterval(parseTime("2024-01-20 00:00"), parseTime("2024-02-10 00:00")),
			v1.NewBounds(0x00000000, 0x0000ffff),
		}

		metas, err := store.FetchMetas(ctx, params)
		require.NoError(t, err)
		require.Len(t, metas, 2)

		require.Equal(t, []Meta{m1, m2}, metas)
	})

	t.Run("tenant does not match", func(t *testing.T) {
		ctx := context.Background()
		params := MetaSearchParams{
			"other",
			NewInterval(parseTime("2024-01-20 00:00"), parseTime("2024-02-10 00:00")),
			v1.NewBounds(0x00000000, 0x0000ffff),
		}

		metas, err := store.FetchMetas(ctx, params)
		require.NoError(t, err)
		require.Len(t, metas, 0)
		require.Equal(t, []Meta{}, metas)
	})
}

func TestBloomStore_FetchBlocks(t *testing.T) {
	store, _, err := newMockBloomStore(t)
	require.NoError(t, err)

	// schema 1
	b1, _ := createBlockInStorage(t, store, "tenant", parseTime("2024-01-20 00:00"), 0x00000000, 0x0000ffff)
	b2, _ := createBlockInStorage(t, store, "tenant", parseTime("2024-01-20 00:00"), 0x00010000, 0x0001ffff)
	// schema 2
	b3, _ := createBlockInStorage(t, store, "tenant", parseTime("2024-02-05 00:00"), 0x00000000, 0x0000ffff)
	b4, _ := createBlockInStorage(t, store, "tenant", parseTime("2024-02-05 00:00"), 0x00000000, 0x0001ffff)

	ctx := context.Background()

	// first call fetches two blocks from cache
	bqs, err := store.FetchBlocks(ctx, []BlockRef{b1.BlockRef, b3.BlockRef})
	require.NoError(t, err)
	require.Len(t, bqs, 2)

	require.Equal(t, []BlockRef{b1.BlockRef, b3.BlockRef}, []BlockRef{bqs[0].BlockRef, bqs[1].BlockRef})

	// second call fetches two blocks from cache and two from storage
	bqs, err = store.FetchBlocks(ctx, []BlockRef{b1.BlockRef, b2.BlockRef, b3.BlockRef, b4.BlockRef})
	require.NoError(t, err)
	require.Len(t, bqs, 4)

	require.Equal(t,
		[]BlockRef{b1.BlockRef, b2.BlockRef, b3.BlockRef, b4.BlockRef},
		[]BlockRef{bqs[0].BlockRef, bqs[1].BlockRef, bqs[2].BlockRef, bqs[3].BlockRef},
	)
}

func TestBloomStore_TenantFilesForInterval(t *testing.T) {
	ctx := context.Background()
	var keyResolver defaultKeyResolver

	store, _, err := newMockBloomStore(t)
	require.NoError(t, err)

	// schema 1
	// day 1 - 1 tenant
	s1d1t1m1, _ := createMetaInStorage(store, "1", parseTime("2024-01-19 00:00"), 0x00010000, 0x0001ffff)
	s1d1t1m2, _ := createMetaInStorage(store, "1", parseTime("2024-01-19 00:00"), 0x00000000, 0x0000ffff)
	// day 2 - 2 tenants
	s1d2t1m1, _ := createMetaInStorage(store, "1", parseTime("2024-01-20 00:00"), 0x00010000, 0x0001ffff)
	s1d2t1m2, _ := createMetaInStorage(store, "1", parseTime("2024-01-20 00:00"), 0x00000000, 0x0000ffff)
	s1d2t2m1, _ := createMetaInStorage(store, "2", parseTime("2024-01-20 00:00"), 0x00010000, 0x0001ffff)
	s1d2t2m2, _ := createMetaInStorage(store, "2", parseTime("2024-01-20 00:00"), 0x00000000, 0x0000ffff)

	// schema 2
	// day 1 - 2 tenants
	s2d1t1m1, _ := createMetaInStorage(store, "1", parseTime("2024-02-07 00:00"), 0x00010000, 0x0001ffff)
	s2d1t1m2, _ := createMetaInStorage(store, "1", parseTime("2024-02-07 00:00"), 0x00000000, 0x0000ffff)
	s2d1t2m1, _ := createMetaInStorage(store, "2", parseTime("2024-02-07 00:00"), 0x00010000, 0x0001ffff)
	s2d1t2m2, _ := createMetaInStorage(store, "2", parseTime("2024-02-07 00:00"), 0x00000000, 0x0000ffff)
	// day 2 - 1 tenant
	s2d2t2m1, _ := createMetaInStorage(store, "2", parseTime("2024-02-10 00:00"), 0x00010000, 0x0001ffff)
	s2d2t2m2, _ := createMetaInStorage(store, "2", parseTime("2024-02-10 00:00"), 0x00000000, 0x0000ffff)

	t.Run("no filter", func(t *testing.T) {
		tenantFiles, err := store.TenantFilesForInterval(
			ctx,
			NewInterval(parseTime("2024-01-18 00:00"), parseTime("2024-02-12 00:00")),
			nil,
		)
		require.NoError(t, err)

		var tenants []string
		for tenant := range tenantFiles {
			tenants = append(tenants, tenant)
		}
		require.ElementsMatch(t, []string{"1", "2"}, tenants)

		tenant1Keys := keysFromStorageObjects(tenantFiles["1"])
		expectedTenant1Keys := []string{
			// schema 1 - day 1
			keyResolver.Meta(s1d1t1m1.MetaRef).Addr(),
			keyResolver.Meta(s1d1t1m2.MetaRef).Addr(),
			// schema 1 - day 2
			keyResolver.Meta(s1d2t1m1.MetaRef).Addr(),
			keyResolver.Meta(s1d2t1m2.MetaRef).Addr(),
			// schema 2 - day 1
			keyResolver.Meta(s2d1t1m1.MetaRef).Addr(),
			keyResolver.Meta(s2d1t1m2.MetaRef).Addr(),
		}
		require.ElementsMatch(t, expectedTenant1Keys, tenant1Keys)

		tenant2Keys := keysFromStorageObjects(tenantFiles["2"])
		expectedTenant2Keys := []string{
			// schema 1 - day 2
			keyResolver.Meta(s1d2t2m1.MetaRef).Addr(),
			keyResolver.Meta(s1d2t2m2.MetaRef).Addr(),
			// schema 2 - day 1
			keyResolver.Meta(s2d1t2m1.MetaRef).Addr(),
			keyResolver.Meta(s2d1t2m2.MetaRef).Addr(),
			// schema 2 - day 2
			keyResolver.Meta(s2d2t2m1.MetaRef).Addr(),
			keyResolver.Meta(s2d2t2m2.MetaRef).Addr(),
		}
		require.ElementsMatch(t, expectedTenant2Keys, tenant2Keys)
	})

	t.Run("filter tenant 1", func(t *testing.T) {
		tenantFiles, err := store.TenantFilesForInterval(
			ctx,
			NewInterval(parseTime("2024-01-18 00:00"), parseTime("2024-02-12 00:00")),
			func(tenant string, _ client.StorageObject) bool {
				return tenant == "1"
			},
		)
		require.NoError(t, err)

		var tenants []string
		for tenant := range tenantFiles {
			tenants = append(tenants, tenant)
		}
		require.ElementsMatch(t, []string{"1", "2"}, tenants)

		tenant1Keys := keysFromStorageObjects(tenantFiles["1"])
		expectedTenant1Keys := []string{
			// schema 1 - day 1
			keyResolver.Meta(s1d1t1m1.MetaRef).Addr(),
			keyResolver.Meta(s1d1t1m2.MetaRef).Addr(),
			// schema 1 - day 2
			keyResolver.Meta(s1d2t1m1.MetaRef).Addr(),
			keyResolver.Meta(s1d2t1m2.MetaRef).Addr(),
			// schema 2 - day 1
			keyResolver.Meta(s2d1t1m1.MetaRef).Addr(),
			keyResolver.Meta(s2d1t1m2.MetaRef).Addr(),
		}
		require.ElementsMatch(t, expectedTenant1Keys, tenant1Keys)

		tenant2Keys := keysFromStorageObjects(tenantFiles["2"])
		require.Empty(t, tenant2Keys)
	})
}

func keysFromStorageObjects(objects []client.StorageObject) (keys []string) {
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	return keys
}

func TestBloomShipper_WorkingDir(t *testing.T) {
	t.Run("insufficient permissions on directory yields error", func(t *testing.T) {
		base := t.TempDir()
		wd := filepath.Join(base, "notpermitted")
		err := os.MkdirAll(wd, 0500)
		require.NoError(t, err)
		fi, _ := os.Stat(wd)
		t.Log("working directory", wd, fi.Mode())

		_, _, err = newMockBloomStoreWithWorkDir(t, wd, base)
		require.ErrorContains(t, err, "insufficient permissions")
	})

	t.Run("not existing directory will be created", func(t *testing.T) {
		base := t.TempDir()
		// if the base directory does not exist, it will be created
		wd := filepath.Join(base, "doesnotexist")
		t.Log("working directory", wd)

		store, _, err := newMockBloomStoreWithWorkDir(t, wd, base)
		require.NoError(t, err)
		b, err := createBlockInStorage(t, store, "tenant", parseTime("2024-01-20 00:00"), 0x00000000, 0x0000ffff)
		require.NoError(t, err)

		ctx := context.Background()
		_, err = store.FetchBlocks(ctx, []BlockRef{b.BlockRef})
		require.NoError(t, err)
	})
}

func TestTablesForRange(t *testing.T) {
	conf := storageconfig.PeriodConfig{
		From: parseDayTime("2024-01-01"),
		IndexTables: storageconfig.IndexPeriodicTableConfig{
			PeriodicTableConfig: storageconfig.PeriodicTableConfig{
				Period: 24 * time.Hour,
			},
		},
	}
	for _, tc := range []struct {
		desc     string
		interval Interval
		exp      []string
	}{
		{
			desc: "few days",
			interval: Interval{
				Start: parseTime("2024-01-01 00:00"),
				End:   parseTime("2024-01-03 00:00"),
			},
			exp: []string{"19723", "19724"},
		},
		{
			desc: "few days with offset",
			interval: Interval{
				Start: parseTime("2024-01-01 00:00"),
				End:   parseTime("2024-01-03 00:01"),
			},
			exp: []string{"19723", "19724", "19725"},
		},
		{
			desc:     "one day",
			interval: NewInterval(parseDayTime("2024-01-01").Bounds()),
			exp:      []string{"19723"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tablesForRange(conf, tc.interval))
		})
	}
}

func TestBloomStore_SortBlocks(t *testing.T) {
	now := parseTime("2024-02-01 00:00")

	refs := make([]BlockRef, 10)
	bqs := make([]*CloseableBlockQuerier, 10)

	for i := 0; i < 10; i++ {
		refs[i] = BlockRef{
			Ref: Ref{
				TenantID: "fake",
				Bounds: v1.NewBounds(
					model.Fingerprint(i*1000),
					model.Fingerprint((i+1)*1000-1),
				),
				StartTimestamp: now,
				EndTimestamp:   now.Add(12 * time.Hour),
			},
		}
		if i%2 == 0 {
			bqs[i] = &CloseableBlockQuerier{
				BlockRef: refs[i],
			}
		}
	}

	// shuffle the slice of block queriers
	rand.Shuffle(len(bqs), func(i, j int) { bqs[i], bqs[j] = bqs[j], bqs[i] })

	// sort the block queriers based on the refs
	sortBlocks(bqs, refs)

	// assert order of block queriers
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			require.Equal(t, refs[i], bqs[i].BlockRef)
		} else {
			require.Nil(t, nil, bqs[i])
		}
	}
}
