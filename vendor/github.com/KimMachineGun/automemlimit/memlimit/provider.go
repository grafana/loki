package memlimit

import (
	"fmt"
)

// Provider is a function that returns the memory limit.
type Provider func() (uint64, error)

// Limit is a helper Provider function that returns the given limit.
func Limit(limit uint64) func() (uint64, error) {
	return func() (uint64, error) {
		return limit, nil
	}
}

// ApplyRationA is a helper Provider function that applies the given ratio to the given provider.
func ApplyRatio(provider Provider, ratio float64) Provider {
	if ratio == 1 {
		return provider
	}
	return func() (uint64, error) {
		if ratio <= 0 || ratio > 1 {
			return 0, fmt.Errorf("invalid ratio: %f, ratio should be in the range (0.0,1.0]", ratio)
		}
		limit, err := provider()
		if err != nil {
			return 0, err
		}
		return uint64(float64(limit) * ratio), nil
	}
}

// ApplyFallback is a helper Provider function that sets the fallback provider.
func ApplyFallback(provider Provider, fallback Provider) Provider {
	return func() (uint64, error) {
		limit, err := provider()
		if err != nil {
			return fallback()
		}
		return limit, nil
	}
}
