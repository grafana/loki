package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	indexshipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

// go build ./tools/tsdb/index-analyzer && BUCKET=19453 DIR=/tmp/loki-index-analysis ./index-analyzer --config.file=/tmp/loki-config.yaml
func main() {
	conf, bucket, err := setup()
	exitErr("setting up", err)

	_, overrides, clientMetrics := defaultConfigs()

	flag.Parse()

	objectClient, err := storage.NewObjectClient(conf.StorageConfig.TSDBShipperConfig.SharedStoreType, conf.StorageConfig, clientMetrics)
	exitErr("creating object client", err)

	tableRanges := getIndexStoreTableRanges(config.TSDBType, conf.SchemaConfig.Configs)

	openFn := func(p string) (indexshipper_index.Index, error) {
		return tsdb.OpenShippableTSDB(p, tsdb.IndexOpts{UsePostingsCache: false})
	}

	shipper, err := indexshipper.NewIndexShipper(
		conf.StorageConfig.TSDBShipperConfig.Config,
		objectClient,
		overrides,
		nil,
		openFn,
		tableRanges[len(tableRanges)-1],
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", prometheus.DefaultRegisterer),
		util_log.Logger,
	)
	exitErr("creating index shipper", err)

	tenants, tableName, err := resolveTenants(objectClient, bucket, tableRanges)
	exitErr("resolving tenants", err)

	err = analyze(shipper, tableName, tenants)
	exitErr("analyzing", err)

}

func exitErr(during string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "encountered error during %s: %v\n", during, err)
		os.Exit(1)
	}

}

func setup() (loki.Config, string, error) {
	var c loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&c, os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "bonk:%T,  %+v", err, err)
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	bucket := os.Getenv("BUCKET")
	dir := os.Getenv("DIR")

	if bucket == "" {
		return c.Config, "", fmt.Errorf("$BUCKET must be specified")
	}

	if dir == "" {
		return c.Config, "", fmt.Errorf("$DIR must be specified")
	}

	if err := util.EnsureDirectory(dir); err != nil {
		return c.Config, "", fmt.Errorf("failed to ensure directory %s: %w", dir, err)
	}

	c.Config.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadOnly
	util_log.InitLogger(&c.Server, prometheus.DefaultRegisterer, c.UseBufferedLogger, c.UseSyncLogger)

	c.Config.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory = filepath.Join(dir, "tsdb-active")
	c.Config.StorageConfig.TSDBShipperConfig.CacheLocation = filepath.Join(dir, "tsdb-cache")
	return c.Config, bucket, nil
}

func defaultConfigs() (config.ChunkStoreConfig, *validation.Overrides, storage.ClientMetrics) {
	var (
		chunkStoreConfig config.ChunkStoreConfig
		limits           validation.Limits
		clientMetrics    storage.ClientMetrics
	)
	chunkStoreConfig.RegisterFlags(flag.NewFlagSet("chunk-store", flag.PanicOnError))
	limits.RegisterFlags(flag.NewFlagSet("limits", flag.PanicOnError))
	overrides, _ := validation.NewOverrides(limits, nil)
	return chunkStoreConfig, overrides, clientMetrics
}
