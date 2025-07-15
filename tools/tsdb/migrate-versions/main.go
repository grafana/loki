package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	shipperstorage "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	gzipExtension  = ".gz"
	tempFileSuffix = ".temp"
)

var (
	desiredVer               = tsdbindex.FormatV3
	tableNumMin, tableNumMax int64
	newTablePrefix           string
)

func exit(code int) {
	util_log.Flush()
	os.Exit(code)
}

// Ussage: TSDB_VERSION=3 TABLE_NUM_MIN=19464 TABLE_NUM_MAX=19465 NEW_TABLE_PREFIX=tsdb_v3_ go run tools/tsdb/migrate-versions/main.go --config.file /tmp/loki-config.yaml
func main() {
	lokiCfg := setup()
	clientMetrics := storage.NewClientMetrics()

	if got := os.Getenv("TSDB_VERSION"); got != "" {
		n, err := strconv.Atoi(got)
		if err != nil {
			log.Fatalf("invalid TSDB_VERSION: %v", err)
		}
		desiredVer = n
	}

	if got := os.Getenv("TABLE_NUM_MIN"); got != "" {
		n, err := strconv.Atoi(got)
		if err != nil {
			log.Fatalf("invalid TABLE_NUM_MIN: %v", err)
		}
		tableNumMin = int64(n)
	}

	if got := os.Getenv("TABLE_NUM_MAX"); got != "" {
		n, err := strconv.Atoi(got)
		if err != nil {
			log.Fatalf("invalid TABLE_NUM_MAX: %v", err)
		}
		tableNumMax = int64(n)
	}

	if got := os.Getenv("NEW_TABLE_PREFIX"); got != "" {
		newTablePrefix = got
	}

	for i, cfg := range lokiCfg.SchemaConfig.Configs {
		if cfg.IndexType != types.TSDBType {
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
	objClient, err := storage.NewObjectClient(pCfg.ObjectType, "tsdb-migrate", storageCfg, clientMetrics)
	if err != nil {
		return err
	}

	indexStorageClient := shipperstorage.NewIndexStorageClient(objClient, pCfg.IndexTables.PathPrefix)

	tableNames, err := indexStorageClient.ListTables(context.Background())
	if err != nil {
		return err
	}

	for _, tableName := range tableNames {
		if !strings.HasPrefix(tableName, pCfg.IndexTables.Prefix) {
			continue
		}
		tableNum, err := config.ExtractTableNumberFromName(tableName)
		if err != nil {
			return err
		}
		if tableNumMin != 0 && tableNum < tableNumMin {
			continue
		}

		if tableNumMax != 0 && tableNum > tableNumMax {
			continue
		}

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
		level.Info(util_log.Logger).Log("msg", "successfully migrated", "table_name", tableName)
	}

	return nil
}

func migrateTable(tableName string, indexStorageClient shipperstorage.Client) error {
	tempDir := os.TempDir()

	uncompactedFiles, tenants, err := indexStorageClient.ListFiles(context.Background(), tableName, true)
	if err != nil {
		return err
	}
	if len(uncompactedFiles) != 0 {
		level.Warn(util_log.Logger).Log("msg", "skipping migration of table which has un-compacted files", "table_name", tableName)
		return nil
	}

	newTableName := tableName
	if newTablePrefix != "" {
		tableNum, err := config.ExtractTableNumberFromName(tableName)
		if err != nil {
			return err
		}

		newTableName = fmt.Sprintf("%s%d", newTablePrefix, tableNum)
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

		decompress := shipperstorage.IsCompressedFile(indexFiles[0].Name)
		if decompress {
			dst = strings.Trim(dst, gzipExtension)
		}
		if err := shipperstorage.DownloadFileFromStorage(
			dst,
			decompress,
			true,
			shipperstorage.LoggerWithFilename(util_log.Logger, indexFiles[0].Name),
			func() (io.ReadCloser, error) {
				return indexStorageClient.GetUserFile(context.Background(), tableName, tenant, indexFiles[0].Name)
			},
		); err != nil {
			return err
		}

		idx, err := tsdb.RebuildWithVersion(context.Background(), dst, desiredVer)
		if err != nil {
			if errors.Is(err, tsdb.ErrAlreadyOnDesiredVersion) {
				continue
			}
			return errors.Wrapf(err, "failed to build index with new version")
		}

		if err := os.Remove(dst); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove downloaded index file", "path", dst, "err", err)
		}

		if err := uploadFile(idx, indexStorageClient, newTableName, tenant); err != nil {
			return errors.Wrapf(err, "failed to upload index file for tenant %s", tenant)
		}

		idxPath := idx.Path()
		if err := idx.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close index file", "err", err)
		}

		if err := os.Remove(idxPath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove local copy of migrated index file", "path", idxPath, "err", err)
		}

		// remove existing index file when writing migrated index in the same table since compactor would anyways be compacted it to a single file
		if newTablePrefix == "" {
			if err := indexStorageClient.DeleteUserFile(context.Background(), tableName, tenant, indexFiles[0].Name); err != nil {
				return errors.Wrapf(err, "failed to remove older version index file %s for tenant %s", indexFiles[0].Name, tenant)
			}
		}
	}

	return nil
}

func uploadFile(idx shipperindex.Index, indexStorageClient shipperstorage.Client, tableName, tenant string) error {
	fileName := idx.Name()
	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("uploading index %s", fileName))

	idxPath := idx.Path()

	filePath := fmt.Sprintf("%s%s", idxPath, tempFileSuffix)
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close temp file", "path", filePath, "err", err)
		}

		if err := os.Remove(filePath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove temp file", "path", filePath, "err", err)
		}
	}()

	gzipPool := compression.GetWriterPool(compression.GZIP)
	compressedWriter := gzipPool.GetWriter(f)
	defer gzipPool.PutWriter(compressedWriter)

	idxReader, err := idx.Reader()
	if err != nil {
		return err
	}

	_, err = idxReader.Seek(0, 0)
	if err != nil {
		return err
	}

	_, err = io.Copy(compressedWriter, idxReader)
	if err != nil {
		return err
	}

	err = compressedWriter.Close()
	if err != nil {
		return err
	}

	// flush the file to disk and seek the file to the beginning.
	if err := f.Sync(); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	return indexStorageClient.PutUserFile(context.Background(), tableName, tenant, fmt.Sprintf("%s.gz", idx.Name()), f)
}

func setup() loki.Config {
	var c loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&c, os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	serverCfg := &c.Server
	serverCfg.Log = util_log.InitLogger(serverCfg, prometheus.DefaultRegisterer, false)

	if err := c.Validate(); err != nil {
		level.Error(util_log.Logger).Log("msg", "validating config", "err", err.Error())
		exit(1)
	}

	return c.Config
}
