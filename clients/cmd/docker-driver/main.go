package main

import (
	"fmt"
	"net/http"
	"os"

	_ "net/http/pprof"

	"github.com/docker/go-plugins-helpers/sdk"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"

	_ "github.com/grafana/loki/pkg/util/build"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const socketAddress = "/run/docker/plugins/loki.sock"

var logLevel logging.Level

func main() {
	levelVal := os.Getenv("LOG_LEVEL")
	if levelVal == "" {
		levelVal = "info"
	}

	if err := logLevel.Set(levelVal); err != nil {
		fmt.Fprintln(os.Stdout, "invalid log level: ", levelVal)
		os.Exit(1)
	}
	logger := newLogger(logLevel)
	level.Info(util_log.Logger).Log("msg", "Starting docker-plugin", "version", version.Info())

	h := sdk.NewHandler(`{"Implements": ["LoggingDriver"]}`)

	handlers(&h, newDriver(logger))

	pprofPort := os.Getenv("PPROF_PORT")
	if pprofPort != "" {
		go func() {
			err := http.ListenAndServe(fmt.Sprintf(":%s", pprofPort), nil)
			logger.Log("msg", "http server stopped", "err", err)
		}()
	}

	if err := h.ServeUnix(socketAddress, 0); err != nil {
		panic(err)
	}
}

func newLogger(lvl logging.Level) log.Logger {
	// plugin logs must be stdout to appear.
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout))
	logger = level.NewFilter(logger, lvl.Gokit)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.Caller(3))
	return logger
}
