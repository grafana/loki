package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	shipperstorage "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	indexPrefix = "tsdb_prefix_"
	userID      = "user1"
)

func TestMigrateTables(t *testing.T) {
	tempDir := t.TempDir()

	now := model.Now()
	pcfg := config.PeriodConfig{
		From:       config.DayTime{Time: now.Add(-10 * 24 * time.Hour)},
		IndexType:  "tsdb",
		ObjectType: "filesystem",
		Schema:     "v12",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: indexPrefix,
				Period: 24 * time.Hour,
			}},
	}

	storageCfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: tempDir,
		},
	}
	clientMetrics := storage.NewClientMetrics()

	objClient, err := storage.NewObjectClient(pcfg.ObjectType, "test", storageCfg, clientMetrics)
	require.NoError(t, err)
	indexStorageClient := shipperstorage.NewIndexStorageClient(objClient, pcfg.IndexTables.PathPrefix)

	currTableName := pcfg.IndexTables.TableFor(now)
	currTableNum, err := config.ExtractTableNumberFromName(currTableName)
	require.NoError(t, err)

	// setup some tables
	for i := currTableNum - 5; i <= currTableNum; i++ {
		b := tsdb.NewBuilder(index.FormatV2)
		b.AddSeries(labels.Labels{
			{
				Name:  "table_name",
				Value: currTableName,
			},
		}, 1, []index.ChunkMeta{
			{
				Checksum: 1,
				MinTime:  0,
				MaxTime:  1,
				KB:       1,
				Entries:  1,
			},
		})

		id, err := b.Build(context.Background(), tempDir, func(from, through model.Time, checksum uint32) tsdb.Identifier {
			id := tsdb.SingleTenantTSDBIdentifier{
				TS:       time.Now(),
				From:     from,
				Through:  through,
				Checksum: checksum,
			}
			return tsdb.NewPrefixedIdentifier(id, tempDir, "")
		})
		require.NoError(t, err)

		tableName := fmt.Sprintf("%s%d", indexPrefix, i)
		idx, err := tsdb.NewShippableTSDBFile(id)
		require.NoError(t, err)

		require.NoError(t, uploadFile(idx, indexStorageClient, tableName, userID))
		idxPath := idx.Path()
		require.NoError(t, idx.Close())
		require.NoError(t, os.Remove(idxPath))
	}

	for _, migrateToVer := range []int{index.FormatV3, index.FormatV2} {
		t.Run(fmt.Sprintf("migrate_to_ver_%d", migrateToVer), func(t *testing.T) {
			desiredVer = migrateToVer
			require.NoError(t, migrateTables(pcfg, storageCfg, clientMetrics, config.TableRange{
				Start:        0,
				End:          currTableNum,
				PeriodConfig: &pcfg,
			}))

			tables, err := indexStorageClient.ListTables(context.Background())
			require.NoError(t, err)
			require.Len(t, tables, 6)

			for _, table := range tables {
				uncompactedFiles, tenants, err := indexStorageClient.ListFiles(context.Background(), table, true)
				require.NoError(t, err)
				require.Len(t, uncompactedFiles, 0)
				require.Len(t, tenants, 1)
				require.Equal(t, userID, tenants[0])

				indexFiles, err := indexStorageClient.ListUserFiles(context.Background(), table, userID, true)
				require.NoError(t, err)
				require.Len(t, indexFiles, 1)

				dst := filepath.Join(t.TempDir(), strings.Trim(indexFiles[0].Name, gzipExtension))
				err = shipperstorage.DownloadFileFromStorage(
					dst,
					true,
					true,
					shipperstorage.LoggerWithFilename(util_log.Logger, indexFiles[0].Name),
					func() (io.ReadCloser, error) {
						return indexStorageClient.GetUserFile(context.Background(), table, userID, indexFiles[0].Name)
					},
				)
				require.NoError(t, err)

				// try running migration again, it should throw error saying already on desired verion
				_, err = tsdb.RebuildWithVersion(context.Background(), dst, desiredVer)
				require.ErrorIs(t, err, tsdb.ErrAlreadyOnDesiredVersion)
			}
		})
	}
	clientMetrics.Unregister()
}
