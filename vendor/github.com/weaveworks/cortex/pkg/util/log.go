package util

import (
	"flag"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/promlog"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"
)

// Logger is a shared go-kit logger.
// TODO: Change all components to take a non-global logger via their constructors.
var Logger = log.NewNopLogger()

// InitLogger initializes the global logger according to the allowed log level.
func InitLogger(level promlog.AllowedLevel) {
	Logger = MustNewPrometheusLogger(promlog.New(level))
}

// LogLevel supports registering a flag for the desired log level.
type LogLevel struct {
	promlog.AllowedLevel
}

// RegisterFlags adds the log level flag to the provided flagset.
func (l *LogLevel) RegisterFlags(f *flag.FlagSet) {
	l.Set("info")
	f.Var(
		&l.AllowedLevel,
		"log.level",
		"Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]",
	)
}

// WithContext returns a Logger that has information about the current user in
// its details.
//
// e.g.
//   log := util.WithContext(ctx)
//   log.Errorf("Could not chunk chunks: %v", err)
func WithContext(ctx context.Context, l log.Logger) log.Logger {
	// Weaveworks uses "orgs" and "orgID" to represent Cortex users,
	// even though the code-base generally uses `userID` to refer to the same thing.
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return l
	}
	return WithUserID(userID, l)
}

// WithUserID returns a Logger that has information about the current user in
// its details.
func WithUserID(userID string, l log.Logger) log.Logger {
	// See note in WithContext.
	return log.With(l, "org_id", userID)
}

// PrometheusLogger exposes Prometheus counters for each of go-kit's log levels.
type PrometheusLogger struct {
	counterVec *prometheus.CounterVec
	logger     log.Logger
}

var supportedLevels = []level.Value{level.DebugValue(), level.InfoValue(), level.WarnValue(), level.ErrorValue()}

// NewPrometheusLogger creates a new instance of PrometheusLogger which exposes Prometheus counters for various log levels.
// Contrarily to MustNewPrometheusLogger, it returns an error to the caller in case of issue.
// Use NewPrometheusLogger if you want more control. Use MustNewPrometheusLogger if you want a less verbose logger creation.
func NewPrometheusLogger(l log.Logger) (*PrometheusLogger, error) {
	counterVec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "log_messages",
		Help: "Total number of log messages.",
	}, []string{"level"})
	// Initialise counters for all supported levels:
	for _, level := range supportedLevels {
		counterVec.WithLabelValues(level.String())
	}
	err := prometheus.Register(counterVec)
	// If another library already registered the same metric, use it
	if err != nil {
		ar, ok := err.(prometheus.AlreadyRegisteredError)
		if !ok {
			return nil, err
		}
		counterVec, ok = ar.ExistingCollector.(*prometheus.CounterVec)
		if !ok {
			return nil, err
		}
	}
	return &PrometheusLogger{
		counterVec: counterVec,
		logger:     l,
	}, nil
}

// MustNewPrometheusLogger creates a new instance of PrometheusLogger which exposes Prometheus counters for various log levels.
// Contrarily to NewPrometheusLogger, it does not return any error to the caller, but panics instead.
// Use MustNewPrometheusLogger if you want a less verbose logger creation. Use NewPrometheusLogger if you want more control.
func MustNewPrometheusLogger(l log.Logger) *PrometheusLogger {
	logger, err := NewPrometheusLogger(l)
	if err != nil {
		panic(err)
	}
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
	pl.counterVec.WithLabelValues(l).Inc()
	return nil
}
