// Package memlimit configures GOMEMLIMIT from a memory limit provider.
package memlimit

import (
	"context"
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

	defaultAUTOMEMLIMIT = 0.9
)

// ErrNoLimit indicates that a [Provider] reports no memory limit.
// [Set] handles it as success by setting GOMEMLIMIT to [math.MaxInt64].
var ErrNoLimit = errors.New("memory is not limited")

type config struct {
	logger     *slog.Logger
	ratio      float64
	minLimit   int64
	provider   Provider
	refresh    time.Duration
	refreshCtx context.Context
}

// Option configures the behavior of [Set].
type Option func(cfg *config)

// WithRatio configures the fraction of the memory limit used for GOMEMLIMIT.
// The ratio must be in (0.0, 1.0].
//
// Default: 0.9
func WithRatio(ratio float64) Option {
	return func(cfg *config) {
		cfg.ratio = ratio
	}
}

// WithMin configures the minimum GOMEMLIMIT after applying the ratio.
// A non-positive value disables the minimum.
//
// Default: 0 (no minimum)
func WithMin(minLimit int64) Option {
	return func(cfg *config) {
		cfg.minLimit = minLimit
	}
}

// WithProvider configures the provider used by [Set].
//
// Default: [FromCgroup]
func WithProvider(provider Provider) Option {
	return func(cfg *config) {
		cfg.provider = provider
	}
}

// WithLogger configures the logger.
// It adds the "package" attribute to every log record.
//
// Default: logging disabled
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *config) {
		cfg.logger = memlimitLogger(logger)
	}
}

// WithRefreshInterval configures [Set] to periodically refresh GOMEMLIMIT.
//
// Set starts a refresh goroutine after the initial provider call
// when refresh is positive and ctx is non-nil.
// The goroutine starts even if the initial call returns an error.
// Canceling ctx stops the refresh loop but does not interrupt an active provider call.
// Provider errors other than [ErrNoLimit] are reported to the configured logger
// and do not stop later refreshes.
//
// Default: 0 (no refresh)
func WithRefreshInterval(ctx context.Context, refresh time.Duration) Option {
	return func(cfg *config) {
		cfg.refresh = refresh
		cfg.refreshCtx = ctx
	}
}

// Set sets GOMEMLIMIT using the configured provider.
//
// By default, Set uses 90% of the limit reported by [FromCgroup].
// AUTOMEMLIMIT overrides the configured ratio with a value in (0.0, 1.0].
// Setting it to "off" disables Set.
//
// If the GOMEMLIMIT environment variable is present or AUTOMEMLIMIT is set to "off",
// Set returns the current GOMEMLIMIT without calling the provider or starting a refresh goroutine.
//
// Set returns the resulting GOMEMLIMIT on success.
// On error, it returns the previous GOMEMLIMIT and the error.
// It handles [ErrNoLimit] as success by setting GOMEMLIMIT to [math.MaxInt64].
func Set(opts ...Option) (_limit int64, _err error) {
	cfg := &config{
		logger:   slog.New(discardHandler{}),
		ratio:    defaultAUTOMEMLIMIT,
		provider: FromCgroup,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	defer func() {
		if _err != nil {
			cfg.logger.Error("failed to set GOMEMLIMIT", slog.Any("error", _err))
		}
	}()

	snapshot := debug.SetMemoryLimit(-1)

	if val, ok := os.LookupEnv(envGOMEMLIMIT); ok {
		cfg.logger.Info("GOMEMLIMIT is already set, skipping", slog.String(envGOMEMLIMIT, val))
		return snapshot, nil
	}

	ratio := cfg.ratio
	if val, ok := os.LookupEnv(envAUTOMEMLIMIT); ok {
		if val == "off" {
			cfg.logger.Info("AUTOMEMLIMIT is off, skipping")
			return snapshot, nil
		}

		r, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return snapshot, fmt.Errorf("cannot parse AUTOMEMLIMIT: %s", val)
		}
		ratio = r
	}
	if math.IsNaN(ratio) || ratio <= 0 || ratio > 1 {
		return snapshot, fmt.Errorf(
			"failed to set GOMEMLIMIT: invalid ratio: %f, ratio should be in the range (0.0,1.0]",
			ratio,
		)
	}

	provider := boundedProvider(ApplyRatio(cfg.provider, ratio), cfg.minLimit)
	limit, err := updateGoMemLimit(provider, cfg.logger)
	if cfg.refresh > 0 && cfg.refreshCtx != nil {
		go refresh(cfg.refreshCtx, provider, cfg.logger, cfg.refresh)
	}
	if err != nil {
		return snapshot, fmt.Errorf("failed to set GOMEMLIMIT: %w", err)
	}

	return int64(limit), nil
}

func updateGoMemLimit(provider Provider, logger *slog.Logger) (uint64, error) {
	newLimit, err := provider()
	if err != nil {
		if errors.Is(err, ErrNoLimit) {
			return updateGoMemLimit(Limit(math.MaxInt64), logger)
		}
		return 0, err
	}

	previous := debug.SetMemoryLimit(int64(newLimit))
	if newLimit == uint64(previous) {
		logger.Debug("GOMEMLIMIT is unchanged", slog.Uint64(envGOMEMLIMIT, newLimit))
		return newLimit, nil
	}

	logger.Info("GOMEMLIMIT is updated", slog.Uint64(envGOMEMLIMIT, newLimit), slog.Uint64("previous", uint64(previous)))

	return newLimit, nil
}

func refresh(ctx context.Context, provider Provider, logger *slog.Logger, refresh time.Duration) {
	if refresh == 0 {
		return
	}

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := updateGoMemLimit(provider, logger)
			if err != nil {
				logger.Error("failed to refresh GOMEMLIMIT", slog.Any("error", err))
			}
		}
	}
}

func boundedProvider(provider Provider, minLimit int64) Provider {
	return func() (uint64, error) {
		limit, err := provider()
		if err != nil {
			return 0, err
		} else if limit > math.MaxInt64 {
			return math.MaxInt64, nil
		} else if minLimit > 0 && limit < uint64(minLimit) {
			return uint64(minLimit), nil
		}
		return limit, nil
	}
}
