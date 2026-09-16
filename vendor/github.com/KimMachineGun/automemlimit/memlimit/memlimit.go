package memlimit

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"time"
)

const (
	envGOMEMLIMIT   = "GOMEMLIMIT"
	envAUTOMEMLIMIT = "AUTOMEMLIMIT"
	// Deprecated: use memlimit.WithLogger instead
	envAUTOMEMLIMIT_DEBUG = "AUTOMEMLIMIT_DEBUG"

	defaultAUTOMEMLIMIT = 0.9
)

// ErrNoLimit is returned when the memory limit is not set.
var ErrNoLimit = errors.New("memory is not limited")

type config struct {
	logger   *slog.Logger
	ratio    float64
	provider Provider
	refresh  time.Duration
}

// Option is a function that configures the behavior of SetGoMemLimitWithOptions.
type Option func(cfg *config)

// WithRatio configures the ratio of the memory limit to set as GOMEMLIMIT.
//
// Default: 0.9
func WithRatio(ratio float64) Option {
	return func(cfg *config) {
		cfg.ratio = ratio
	}
}

// WithProvider configures the provider.
//
// Default: FromCgroup
func WithProvider(provider Provider) Option {
	return func(cfg *config) {
		cfg.provider = provider
	}
}

// WithLogger configures the logger.
// It automatically attaches the "package" attribute to the logs.
//
// Default: slog.New(noopLogger{})
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *config) {
		cfg.logger = memlimitLogger(logger)
	}
}

// WithRefreshInterval configures the refresh interval for automemlimit.
// If a refresh interval is greater than 0, automemlimit periodically fetches
// the memory limit from the provider and reapplies it if it has changed.
// If the provider returns an error, it logs the error and continues.
// ErrNoLimit is treated as math.MaxInt64.
//
// Default: 0 (no refresh)
func WithRefreshInterval(refresh time.Duration) Option {
	return func(cfg *config) {
		cfg.refresh = refresh
	}
}

// WithEnv configures whether to use environment variables.
//
// Default: false
//
// Deprecated: currently this does nothing.
func WithEnv() Option {
	return func(cfg *config) {}
}

func memlimitLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(noopLogger{})
	}
	return logger.With(slog.String("package", "github.com/KimMachineGun/automemlimit/memlimit"))
}

// SetGoMemLimitWithOpts sets GOMEMLIMIT with options and environment variables.
//
// You can configure how much memory of the cgroup's memory limit to set as GOMEMLIMIT
// through AUTOMEMLIMIT environment variable in the half-open range (0.0,1.0].
//
// If AUTOMEMLIMIT is not set, it defaults to 0.9. (10% is the headroom for memory sources the Go runtime is unaware of.)
// If GOMEMLIMIT is already set or AUTOMEMLIMIT=off, this function does nothing.
//
// If AUTOMEMLIMIT_EXPERIMENT is set, it enables experimental features.
// Please see the documentation of Experiments for more details.
//
// Options:
//   - WithRatio
//   - WithProvider
//   - WithLogger
func SetGoMemLimitWithOpts(opts ...Option) (_ int64, _err error) {
	// init config
	cfg := &config{
		logger:   slog.New(noopLogger{}),
		ratio:    defaultAUTOMEMLIMIT,
		provider: FromCgroup,
	}
	// TODO: remove this
	if debug, ok := os.LookupEnv(envAUTOMEMLIMIT_DEBUG); ok {
		defaultLogger := memlimitLogger(slog.Default())
		defaultLogger.Warn("AUTOMEMLIMIT_DEBUG is deprecated, use memlimit.WithLogger instead")
		if debug == "true" {
			cfg.logger = defaultLogger
		}
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// log error if any on return
	defer func() {
		if _err != nil {
			cfg.logger.Error("failed to set GOMEMLIMIT", slog.Any("error", _err))
		}
	}()

	// parse experiments
	exps, err := parseExperiments()
	if err != nil {
		return 0, fmt.Errorf("failed to parse experiments: %w", err)
	}
	if exps.System {
		cfg.logger.Info("system experiment is enabled: using system memory limit as a fallback")
		cfg.provider = ApplyFallback(cfg.provider, FromSystem)
	}

	// rollback to previous memory limit on panic
	snapshot := debug.SetMemoryLimit(-1)
	defer rollbackOnPanic(cfg.logger, snapshot, &_err)

	// check if GOMEMLIMIT is already set
	if val, ok := os.LookupEnv(envGOMEMLIMIT); ok {
		cfg.logger.Info("GOMEMLIMIT is already set, skipping", slog.String(envGOMEMLIMIT, val))
		return 0, nil
	}

	// parse AUTOMEMLIMIT
	ratio := cfg.ratio
	if val, ok := os.LookupEnv(envAUTOMEMLIMIT); ok {
		if val == "off" {
			cfg.logger.Info("AUTOMEMLIMIT is set to off, skipping")
			return 0, nil
		}
		ratio, err = strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse AUTOMEMLIMIT: %s", val)
		}
	}

	// apply ratio to the provider
	provider := capProvider(ApplyRatio(cfg.provider, ratio))

	// set the memory limit and start refresh
	limit, err := updateGoMemLimit(uint64(snapshot), provider, cfg.logger)
	refresh(provider, cfg.logger, cfg.refresh)
	if err != nil {
		if errors.Is(err, ErrNoLimit) {
			cfg.logger.Info("memory is not limited, skipping")
			// TODO: consider returning the snapshot
			return 0, nil
		}
		return 0, fmt.Errorf("failed to set GOMEMLIMIT: %w", err)
	}

	return int64(limit), nil
}

// updateGoMemLimit updates the Go's memory limit, if it has changed.
func updateGoMemLimit(currLimit uint64, provider Provider, logger *slog.Logger) (uint64, error) {
	newLimit, err := provider()
	if err != nil {
		return 0, err
	}

	if newLimit == currLimit {
		logger.Debug("GOMEMLIMIT is not changed, skipping", slog.Uint64(envGOMEMLIMIT, newLimit))
		return newLimit, nil
	}

	debug.SetMemoryLimit(int64(newLimit))
	logger.Info("GOMEMLIMIT is updated", slog.Uint64(envGOMEMLIMIT, newLimit), slog.Uint64("previous", currLimit))

	return newLimit, nil
}

// refresh spawns a goroutine that runs every refresh duration and updates the GOMEMLIMIT if it has changed.
// See more details in the documentation of WithRefreshInterval.
func refresh(provider Provider, logger *slog.Logger, refresh time.Duration) {
	if refresh == 0 {
		return
	}

	provider = noErrNoLimitProvider(provider)

	t := time.NewTicker(refresh)
	go func() {
		for range t.C {
			err := func() (_err error) {
				snapshot := debug.SetMemoryLimit(-1)
				defer rollbackOnPanic(logger, snapshot, &_err)

				_, err := updateGoMemLimit(uint64(snapshot), provider, logger)
				if err != nil {
					return err
				}

				return nil
			}()
			if err != nil {
				logger.Error("failed to refresh GOMEMLIMIT", slog.Any("error", err))
			}
		}
	}()
}

// rollbackOnPanic rollbacks to the snapshot on panic.
// Since it uses recover, it should be called in a deferred function.
func rollbackOnPanic(logger *slog.Logger, snapshot int64, err *error) {
	panicErr := recover()
	if panicErr != nil {
		if *err != nil {
			logger.Error("failed to set GOMEMLIMIT", slog.Any("error", *err))
		}
		*err = fmt.Errorf("panic during setting the Go's memory limit, rolling back to previous limit %d: %v",
			snapshot, panicErr,
		)
		debug.SetMemoryLimit(snapshot)
	}
}

// SetGoMemLimitWithEnv sets GOMEMLIMIT with the value from the environment variables.
// Since WithEnv is deprecated, this function is equivalent to SetGoMemLimitWithOpts().
// Deprecated: use SetGoMemLimitWithOpts instead.
func SetGoMemLimitWithEnv() {
	_, _ = SetGoMemLimitWithOpts()
}

// SetGoMemLimit sets GOMEMLIMIT with the value from the cgroup's memory limit and given ratio.
func SetGoMemLimit(ratio float64) (int64, error) {
	return SetGoMemLimitWithOpts(WithRatio(ratio))
}

// SetGoMemLimitWithProvider sets GOMEMLIMIT with the value from the given provider and ratio.
func SetGoMemLimitWithProvider(provider Provider, ratio float64) (int64, error) {
	return SetGoMemLimitWithOpts(WithProvider(provider), WithRatio(ratio))
}

func noErrNoLimitProvider(provider Provider) Provider {
	return func() (uint64, error) {
		limit, err := provider()
		if errors.Is(err, ErrNoLimit) {
			return math.MaxInt64, nil
		}
		return limit, err
	}
}

func capProvider(provider Provider) Provider {
	return func() (uint64, error) {
		limit, err := provider()
		if err != nil {
			return 0, err
		} else if limit > math.MaxInt64 {
			return math.MaxInt64, nil
		}
		return limit, nil
	}
}
