package helpers

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/collectors/version"

	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

func Setup() (loki.Config, services.Service, string, error) {
	var c loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&c, os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	bucket := os.Getenv("BUCKET")
	dir := os.Getenv("DIR")

	if bucket == "" {
		return c.Config, nil, "", fmt.Errorf("$BUCKET must be specified")
	}

	if dir == "" {
		return c.Config, nil, "", fmt.Errorf("$DIR must be specified")
	}

	if err := util.EnsureDirectory(dir); err != nil {
		return c.Config, nil, "", fmt.Errorf("failed to ensure directory %s: %w", dir, err)
	}

	c.Config.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadOnly
	util_log.InitLogger(&c.Server, prometheus.DefaultRegisterer, false)

	c.Config.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory = filepath.Join(dir, "tsdb-active")
	c.Config.StorageConfig.TSDBShipperConfig.CacheLocation = filepath.Join(dir, "tsdb-cache")

	svc, err := moduleManager(&c.Config.Server)
	if err != nil {
		return c.Config, nil, "", err
	}

	return c.Config, svc, bucket, nil
}

func moduleManager(cfg *server.Config) (services.Service, error) {
	prometheus.MustRegister(version.NewCollector("loki"))
	// unregister default go collector
	prometheus.Unregister(collectors.NewGoCollector())
	// register collector with additional metrics
	prometheus.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll),
	))

	if cfg.HTTPListenPort == 0 {
		cfg.HTTPListenPort = 8080
	}

	serv, err := server.New(*cfg)
	if err != nil {
		return nil, err
	}

	s := loki.NewServerService(serv, func() []services.Service { return nil })

	return s, nil
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
