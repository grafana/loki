package memlimit

import (
	"fmt"
	"math"
)

// Provider returns a memory limit in bytes.
// It should return [ErrNoLimit] when the source reports no limit.
type Provider func() (uint64, error)

// Limit returns a [Provider] that always returns limit.
func Limit(limit uint64) Provider {
	return func() (uint64, error) {
		return limit, nil
	}
}

// ApplyRatio wraps provider and applies ratio to its result.
// The ratio must be in (0.0, 1.0].
func ApplyRatio(provider Provider, ratio float64) Provider {
	if ratio == 1 {
		return provider
	}
	return func() (uint64, error) {
		if math.IsNaN(ratio) || ratio <= 0 || ratio > 1 {
			return 0, fmt.Errorf("invalid ratio: %f, ratio should be in the range (0.0,1.0]", ratio)
		}
		limit, err := provider()
		if err != nil {
			return 0, err
		}
		return uint64(float64(limit) * ratio), nil
	}
}

// ApplyFallback returns a [Provider] that calls fallback when provider returns an error,
// including [ErrNoLimit].
func ApplyFallback(provider Provider, fallback Provider) Provider {
	return func() (uint64, error) {
		limit, err := provider()
		if err != nil {
			return fallback()
		}
		return limit, nil
	}
}
