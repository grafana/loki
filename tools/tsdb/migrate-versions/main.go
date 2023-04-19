package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	shipper_storage "github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	gzipExtension = ".gz"
)

var (
	desiredVer = flag.Int("ver", index.LiveFormat, "desired version to migrate")
)

func exit(code int) {
	util_log.Flush()
	os.Exit(code)
}

func main() {
	lokiCfg := setup()
	clientMetrics := storage.NewClientMetrics()

	for i, cfg := range lokiCfg.SchemaConfig.Configs {
		if cfg.IndexType != config.TSDBType {
			continue
		}

		periodEndTime := config.DayTime{Time: math.MaxInt64}
		if i < len(lokiCfg.SchemaConfig.Configs)-1 {
			periodEndTime = config.DayTime{Time: lokiCfg.SchemaConfig.Configs[i+1].From.Time.Add(-time.Millisecond)}
		}

		tableRange := cfg.GetIndexTableNumberRange(periodEndTime)
		if err := migrateTables(cfg, lokiCfg.StorageConfig, clientMetrics, tableRange); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to migrate tsdb version", "schema_start", cfg.From, "err", err)
		}
	}
}

func migrateTables(pCfg config.PeriodConfig, storageCfg storage.Config, clientMetrics storage.ClientMetrics, tableRange config.TableRange) error {
	objClient, err := storage.NewObjectClient(pCfg.ObjectType, storageCfg, clientMetrics)
	if err != nil {
		return err
	}

	indexStorageClient := shipper_storage.NewIndexStorageClient(objClient, storageCfg.TSDBShipperConfig.SharedStoreKeyPrefix)

	tableNames, err := indexStorageClient.ListTables(context.Background())
	if err != nil {
		return err
	}

	for _, tableName := range tableNames {
		tableInRange, err := tableRange.TableInRange(tableName)
		if err != nil {
			return err
		}
		if !tableInRange {
			continue
		}

		if err := migrateTable(tableName, indexStorageClient); err != nil {
			return err
		}
	}

	return nil
}

func migrateTable(tableName string, indexStorageClient shipper_storage.Client) error {
	tempDir := os.TempDir()

	uncompactedFiles, tenants, err := indexStorageClient.ListFiles(context.Background(), tableName, true)
	if err != nil {
		return err
	}
	if len(uncompactedFiles) != 0 {
		level.Warn(util_log.Logger).Log("msg", "skipping migration of table which has un-compacted files", "table_name", tableName)
		return nil
	}

	for _, tenant := range tenants {
		indexFiles, err := indexStorageClient.ListUserFiles(context.Background(), tableName, tenant, true)
		if err != nil {
			return err
		}

		if len(indexFiles) != 1 {
			level.Warn(util_log.Logger).Log("msg", "skipping migration of tenant index which has un-compacted files", "table_name", tableName, "tenant", tenant, "file_count", len(indexFiles))
			continue
		}

		tenantDir := filepath.Join(tempDir, tenant)
		if err := util.EnsureDirectory(tenantDir); err != nil {
			return err
		}

		dst := filepath.Join(tenantDir, indexFiles[0].Name)

		decompress := shipper_storage.IsCompressedFile(indexFiles[0].Name)
		if decompress {
			dst = strings.Trim(dst, gzipExtension)
		}
		if err := shipper_storage.DownloadFileFromStorage(
			dst,
			decompress,
			true,
			shipper_storage.LoggerWithFilename(util_log.Logger, indexFiles[0].Name),
			func() (io.ReadCloser, error) {
				return indexStorageClient.GetUserFile(context.Background(), tableName, tenant, indexFiles[0].Name)
			},
		); err != nil {
			return err
		}

		idx, err := tsdb.RebuildWithVersion(context.Background(), dst, *desiredVer)
		if err != nil {
			if errors.Is(err, tsdb.ErrAlreadyOnDesiredVersion) {
				continue
			}
			return errors.Wrapf(err, "failed to build index with new version")
		}

		idxReader, err := idx.Reader()
		if err != nil {
			return errors.Wrapf(err, "failed to create reader for uploading index")
		}
		if err := indexStorageClient.PutUserFile(context.Background(), tableName, tenant, idx.Name(), idxReader); err != nil {
			return errors.Wrapf(err, "failed to upload index file for tenant %s", tenant)
		}

		if err := indexStorageClient.DeleteUserFile(context.Background(), tableName, tenant, indexFiles[0].Name); err != nil {
			return errors.Wrapf(err, "failed to remove older version index file %s for tenant %s", indexFiles[0].Name, tenant)
		}
	}

	return nil
}

func setup() loki.Config {
	var c loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&c, os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	util_log.InitLogger(&c.Server, prometheus.DefaultRegisterer, c.UseBufferedLogger, c.UseSyncLogger)

	if err := c.Validate(); err != nil {
		level.Error(util_log.Logger).Log("msg", "validating config", "err", err.Error())
		exit(1)
	}

	return c.Config
}
