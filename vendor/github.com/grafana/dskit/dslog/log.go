package dslog

import (
	"fmt"
	"os"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/logging"
)

// PrometheusLogger exposes Prometheus counters for each of go-kit's log levels.
type PrometheusLogger struct {
	logger                    log.Logger
	logMessages               *prometheus.CounterVec
	experimentalFeaturesInUse prometheus.Counter
}

// NewPrometheusLogger creates a new instance of PrometheusLogger which exposes
// Prometheus counters for various log levels.
func NewPrometheusLogger(l logging.Level, format logging.Format, reg prometheus.Registerer, metricsNamespace string) log.Logger {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	if format.String() == "json" {
		logger = log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
	}
	logger = level.NewFilter(logger, l.Gokit)

	logMessages := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "log_messages_total",
		Help: "Total number of log messages.",
	}, []string{"level"})
	// Initialise counters for all supported levels:
	supportedLevels := []level.Value{
		level.DebugValue(),
		level.InfoValue(),
		level.WarnValue(),
		level.ErrorValue(),
	}
	for _, level := range supportedLevels {
		logMessages.WithLabelValues(level.String())
	}

	logger = &PrometheusLogger{
		logger:      logger,
		logMessages: logMessages,
		experimentalFeaturesInUse: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Name:      "experimental_features_in_use_total",
				Help:      "The number of experimental features in use.",
			},
		),
	}

	// return a Logger without caller information, shouldn't use directly
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	return logger
}

// Log increments the appropriate Prometheus counter depending on the log level.
func (pl *PrometheusLogger) Log(kv ...interface{}) error {
	pl.logger.Log(kv...)
	l := "unknown"
	for i := 1; i < len(kv); i += 2 {
		if v, ok := kv[i].(level.Value); ok {
			l = v.String()
			break
		}
	}
	pl.logMessages.WithLabelValues(l).Inc()
	return nil
}

// CheckFatal prints an error and exits with error code 1 if err is non-nil.
func CheckFatal(location string, err error, logger log.Logger) {
	if err != nil {
		logger := level.Error(logger)
		if location != "" {
			logger = log.With(logger, "msg", "error "+location)
		}
		// %+v gets the stack trace from errors using github.com/pkg/errors
		logger.Log("err", fmt.Sprintf("%+v", err))
		os.Exit(1)
	}
}

// WarnExperimentalUse logs a warning and increments the experimental features metric.
func WarnExperimentalUse(feature string, logger log.Logger) {
	level.Warn(logger).Log("msg", "experimental feature in use", "feature", feature)
	if pl, ok := logger.(*PrometheusLogger); ok {
		pl.experimentalFeaturesInUse.Inc()
	}
}
