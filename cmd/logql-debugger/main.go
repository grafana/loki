package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/server"

	util_log "github.com/grafana/loki/pkg/util/log"
)

func main() {
	cfg := getConfig()
	util_log.InitLogger(&server.Config{
		LogLevel: cfg.LogLevel,
	}, prometheus.DefaultRegisterer)
	s, err := createServer(cfg)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error while creating the server", "err", err)
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("Starting the server on port %v", s.Addr))
	err = s.ListenAndServe()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error while running the server", "err", err)
	}
}

func getConfig() config {
	cfg := config{}
	flag.IntVar(&cfg.HTTPListenPort, "server.http-listen-port", 3009, "HTTP server listen port.")
	cfg.LogLevel.RegisterFlags(flag.CommandLine)
	flag.Parse()
	return cfg
}

func createServer(cfg config) (*http.Server, error) {
	router := mux.NewRouter()
	router.Use(mux.CORSMethodMiddleware(router))
	router.Use(corsMiddlware())
	router.Handle("/api/logql-debug", &debugServer{}).Methods(http.MethodPost, http.MethodOptions)
	return &http.Server{
		Handler: router,
		Addr:    fmt.Sprintf(":%d", cfg.HTTPListenPort),
	}, nil
}

type config struct {
	HTTPListenPort int
	LogLevel       logging.Level
}
