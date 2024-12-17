package main

import (
	"flag"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logqlanalyzer"
	"github.com/grafana/loki/v3/pkg/sizing"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func main() {
	cfg := getConfig()
	util_log.InitLogger(&server.Config{
		LogLevel: cfg.LogLevel,
	}, prometheus.DefaultRegisterer, false)
	s, err := createServer(cfg, util_log.Logger)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error while creating the server", "err", err)
	}

	err = s.Run()
	defer s.Shutdown()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error while running the server", "err", err)
	}
}

func getConfig() server.Config {
	cfg := server.Config{}
	cfg.RegisterFlags(flag.CommandLine)
	flag.Parse()
	return cfg
}

func createServer(cfg server.Config, logger log.Logger) (*server.Server, error) {
	s, err := server.New(cfg)
	if err != nil {
		return nil, err
	}
	s.HTTP.Use(mux.CORSMethodMiddleware(s.HTTP))
	s.HTTP.Use(logqlanalyzer.CorsMiddleware())
	s.HTTP.Handle("/api/logql-analyze", &logqlanalyzer.LogQLAnalyzeHandler{}).Methods(http.MethodPost, http.MethodOptions)

	sizingHandler := sizing.NewHandler(log.With(logger, "component", "sizing"))

	s.HTTP.Handle("/api/sizing/helm", http.HandlerFunc(sizingHandler.GenerateHelmValues)).Methods(http.MethodGet, http.MethodOptions)
	s.HTTP.Handle("/api/sizing/nodes", http.HandlerFunc(sizingHandler.Nodes)).Methods(http.MethodGet, http.MethodOptions)
	s.HTTP.Handle("/api/sizing/cluster", http.HandlerFunc(sizingHandler.Cluster)).Methods(http.MethodGet, http.MethodOptions)

	s.HTTP.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ready", http.StatusOK)
	}).Methods(http.MethodGet)
	return s, err
}
