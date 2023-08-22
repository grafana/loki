package helpers

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
	"github.com/prometheus/client_golang/prometheus"
)

func Setup() (loki.Config, string, error) {
	var c loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&c, os.Args[1:], flag.CommandLine); err != nil {
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

func DefaultConfigs() (config.ChunkStoreConfig, *validation.Overrides, storage.ClientMetrics) {
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

func ExitErr(during string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "encountered error during %s: %v\n", during, err)
		os.Exit(1)
	}

}
