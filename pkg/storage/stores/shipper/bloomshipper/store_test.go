package bloomshipper

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	storageconfig "github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func newMockBloomStore(t *testing.T) (*BloomStore, string) {
	workDir := t.TempDir()

	periodicConfigs := []storageconfig.PeriodConfig{
		{
			ObjectType: storageconfig.StorageTypeInMemory,
			From:       parseDayTime("2024-01-01"),
			IndexTables: storageconfig.IndexPeriodicTableConfig{
				PeriodicTableConfig: storageconfig.PeriodicTableConfig{
					Period: 24 * time.Hour,
					// TODO(chaudum): Integrate {,Parse}MetaKey into schema config
					// Prefix: "schema_a_table_",
				}},
		},
		{
			ObjectType: storageconfig.StorageTypeInMemory,
			From:       parseDayTime("2024-02-01"),
			IndexTables: storageconfig.IndexPeriodicTableConfig{
				PeriodicTableConfig: storageconfig.PeriodicTableConfig{
					Period: 24 * time.Hour,
					// TODO(chaudum): Integrate {,Parse}MetaKey into schema config
					// Prefix: "schema_b_table_",
				}},
		},
	}

	storageConfig := storage.Config{
		BloomShipperConfig: config.Config{
			WorkingDirectory: workDir,
			BlocksDownloadingQueue: config.DownloadingQueueConfig{
				WorkersCount: 1,
			},
		},
	}

	metrics := storage.NewClientMetrics()
	t.Cleanup(metrics.Unregister)
	logger := log.NewLogfmtLogger(os.Stderr)
	store, err := NewBloomStore(periodicConfigs, storageConfig, metrics, cache.NewNoopCache(), nil, logger)
	require.NoError(t, err)

	return store, workDir
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
		Blocks:     []BlockRef{},
		Tombstones: []BlockRef{},
	}
	err := store.storeDo(start, func(s *bloomStoreEntry) error {
		raw, _ := json.Marshal(meta)
		meta.MetaRef.Ref.TableName = tablesForRange(s.cfg, NewInterval(start, start.Add(12*time.Hour)))[0]
		return s.objectClient.PutObject(context.Background(), s.Meta(meta.MetaRef).Addr(), bytes.NewReader(raw))
	})
	return meta, err
}

func TestBloomStore_ResolveMetas(t *testing.T) {
	store, _ := newMockBloomStore(t)

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

func TestBloomStore_FetchMetas(t *testing.T) {}

func TestBloomStore_FetchBlocks(t *testing.T) {}

func TestBloomStore_Fetcher(t *testing.T) {}

func TarGz(t *testing.T, dst io.Writer, file string) {
	src, err := os.Open(file)
	require.NoError(t, err)
	defer src.Close()

	gzipper := chunkenc.GetWriterPool(chunkenc.EncGZIP).GetWriter(dst)
	defer gzipper.Close()

	tarballer := tar.NewWriter(gzipper)
	defer tarballer.Close()

	for _, f := range []*os.File{src} {
		info, err := f.Stat()
		require.NoError(t, err)

		header, err := tar.FileInfoHeader(info, f.Name())
		require.NoError(t, err)

		err = tarballer.WriteHeader(header)
		require.NoError(t, err)

		_, err = io.Copy(tarballer, f)
		require.NoError(t, err)
	}
}
