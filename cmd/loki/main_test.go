package main

import (
	"errors"
	"runtime/debug"
	"testing"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

// The Go runtime reads GOMEMLIMIT once at startup, so these tests set a known
// limit with debug.SetMemoryLimit and assert on what setGoMemLimit leaves
// behind. The memory limit of the cgroup the test runs in is not readable
// portably, so a stub provider stands in for it.
func TestSetGoMemLimit(t *testing.T) {
	const (
		current     = 123 << 20
		cgroupLimit = 1 << 30
	)

	fixedLimit := func(limit uint64) memlimit.Provider {
		return func() (uint64, error) { return limit, nil }
	}

	for _, tc := range []struct {
		name     string
		env      map[string]string
		provider memlimit.Provider
		expected int64
	}{
		{
			name:     "derives the limit from the cgroup limit",
			provider: fixedLimit(cgroupLimit),
			expected: 966367641, // the default ratio of 0.9, truncated
		},
		{
			name:     "AUTOMEMLIMIT overrides the ratio",
			env:      map[string]string{"AUTOMEMLIMIT": "0.5"},
			provider: fixedLimit(cgroupLimit),
			expected: cgroupLimit / 2,
		},
		{
			name:     "an explicit GOMEMLIMIT is kept",
			env:      map[string]string{"GOMEMLIMIT": "256MiB"},
			provider: fixedLimit(cgroupLimit),
			expected: current,
		},
		{
			name:     "AUTOMEMLIMIT=off keeps the current limit",
			env:      map[string]string{"AUTOMEMLIMIT": "off"},
			provider: fixedLimit(cgroupLimit),
			expected: current,
		},
		{
			name:     "an unparsable AUTOMEMLIMIT keeps the current limit",
			env:      map[string]string{"AUTOMEMLIMIT": "not-a-ratio"},
			provider: fixedLimit(cgroupLimit),
			expected: current,
		},
		{
			name:     "an unreadable cgroup limit keeps the current limit",
			provider: func() (uint64, error) { return 0, errors.New("no cgroup here") },
			expected: current,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			previous := debug.SetMemoryLimit(current)
			t.Cleanup(func() { debug.SetMemoryLimit(previous) })

			setGoMemLimit(log.NewNopLogger(), memlimit.WithProvider(tc.provider))

			require.Equal(t, tc.expected, debug.SetMemoryLimit(-1))
		})
	}
}
