package main

import (
	"flag"
	"os"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/loghttp"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/tools/querytee"
)

type Config struct {
	ServerMetricsPort int
	LogLevel          logging.Level
	ProxyConfig       querytee.ProxyConfig
}

func main() {
	// Parse CLI flags.
	cfg := Config{}
	flag.IntVar(&cfg.ServerMetricsPort, "server.metrics-port", 9900, "The port where metrics are exposed.")
	cfg.LogLevel.RegisterFlags(flag.CommandLine)
	cfg.ProxyConfig.RegisterFlags(flag.CommandLine)
	flag.Parse()

	util_log.InitLogger(&server.Config{
		LogLevel: cfg.LogLevel,
	}, prometheus.DefaultRegisterer, true, false)

	// Run the instrumentation server.
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())

	i := querytee.NewInstrumentationServer(cfg.ServerMetricsPort, registry)
	if err := i.Start(); err != nil {
		level.Error(util_log.Logger).Log("msg", "Unable to start instrumentation server", "err", err.Error())
		os.Exit(1)
	}

	// Run the proxy.
	proxy, err := querytee.NewProxy(cfg.ProxyConfig, util_log.Logger, lokiReadRoutes(cfg), lokiWriteRoutes(), registry)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "Unable to initialize the proxy", "err", err.Error())
		os.Exit(1)
	}

	if err := proxy.Start(); err != nil {
		level.Error(util_log.Logger).Log("msg", "Unable to start the proxy", "err", err.Error())
		os.Exit(1)
	}

	proxy.Await()
}

func lokiReadRoutes(cfg Config) []querytee.Route {
	samplesComparator := querytee.NewSamplesComparator(querytee.SampleComparisonOptions{
		Tolerance:         cfg.ProxyConfig.ValueComparisonTolerance,
		UseRelativeError:  cfg.ProxyConfig.UseRelativeError,
		SkipRecentSamples: cfg.ProxyConfig.SkipRecentSamples,
	})
	samplesComparator.RegisterSamplesType(loghttp.ResultTypeStream, compareStreams)

	return []querytee.Route{
		{Path: "/loki/api/v1/query_range", RouteName: "api_v1_query_range", Methods: []string{"GET"}, ResponseComparator: samplesComparator},
		{Path: "/loki/api/v1/query", RouteName: "api_v1_query", Methods: []string{"GET"}, ResponseComparator: samplesComparator},
		{Path: "/loki/api/v1/label", RouteName: "api_v1_label", Methods: []string{"GET"}, ResponseComparator: nil},
		{Path: "/loki/api/v1/labels", RouteName: "api_v1_labels", Methods: []string{"GET"}, ResponseComparator: nil},
		{Path: "/loki/api/v1/label/{name}/values", RouteName: "api_v1_label_name_values", Methods: []string{"GET"}, ResponseComparator: nil},
		{Path: "/loki/api/v1/series", RouteName: "api_v1_series", Methods: []string{"GET"}, ResponseComparator: nil},
		{Path: "/api/prom/query", RouteName: "api_prom_query", Methods: []string{"GET"}, ResponseComparator: samplesComparator},
		{Path: "/api/prom/label", RouteName: "api_prom_label", Methods: []string{"GET"}, ResponseComparator: nil},
		{Path: "/api/prom/label/{name}/values", RouteName: "api_prom_label_name_values", Methods: []string{"GET"}, ResponseComparator: nil},
		{Path: "/api/prom/series", RouteName: "api_prom_series", Methods: []string{"GET"}, ResponseComparator: nil},
	}
}

func lokiWriteRoutes() []querytee.Route {
	return []querytee.Route{
		{Path: "/loki/api/v1/push", RouteName: "api_v1_push", Methods: []string{"POST"}, ResponseComparator: nil},
		{Path: "/api/prom/push", RouteName: "api_prom_push", Methods: []string{"POST"}, ResponseComparator: nil},
	}
}
