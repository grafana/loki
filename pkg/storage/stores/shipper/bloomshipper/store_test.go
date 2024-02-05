package bloomshipper

import (
	"archive/tar"
	"io"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	storageconfig "github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/stretchr/testify/require"
)

func createStore(t *testing.T) (*BloomStore, string) {
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

func TestBloomStore_ResolveMetas(t *testing.T) {}

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
