package main

import (
	"fmt"
	"os"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/docker/go-plugins-helpers/sdk"
	"github.com/weaveworks/common/logging"
)

const socketAddress = "/run/docker/plugins/loki.sock"

var logLevel logging.Level

func main() {
	levelVal := os.Getenv("LOG_LEVEL")
	if levelVal == "" {
		levelVal = "info"
	}

	if err := logLevel.Set(levelVal); err != nil {
		fmt.Fprintln(os.Stderr, "invalid log level: ", levelVal)
		os.Exit(1)
	}
	logger, err := util.NewPrometheusLogger(logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not create logger: ", err.Error())
		os.Exit(1)
	}

	h := sdk.NewHandler(`{"Implements": ["LoggingDriver"]}`)
	handlers(&h, newDriver(logger))
	if err := h.ServeUnix(socketAddress, 0); err != nil {
		panic(err)
	}

}
