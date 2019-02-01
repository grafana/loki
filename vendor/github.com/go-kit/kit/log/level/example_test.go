package level_test

import (
	"errors"
	"os"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

func Example_basic() {
	// setup logger with level filter
	logger := log.NewLogfmtLogger(os.Stdout)
	logger = level.NewFilter(logger, level.AllowInfo())
	logger = log.With(logger, "caller", log.DefaultCaller)

	// use level helpers to log at different levels
	level.Error(logger).Log("err", errors.New("bad data"))
	level.Info(logger).Log("event", "data saved")
	level.Debug(logger).Log("next item", 17) // filtered

	// Output:
	// level=error caller=example_test.go:18 err="bad data"
	// level=info caller=example_test.go:19 event="data saved"
}
