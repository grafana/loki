package main

import (
	"math"
	"os"
	"runtime/debug"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loki"
)

func TestSetAutoMemLimit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		enabled     bool
		gomemlimit  *string
		wantChanged bool
	}{
		{name: "disabled", enabled: false},
		{name: "enabled", enabled: true, wantChanged: true},
		{name: "explicit GOMEMLIMIT wins", enabled: true, gomemlimit: new("1GiB")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The memory limit is process-wide: start from "no limit" and restore it.
			prev := debug.SetMemoryLimit(math.MaxInt64)
			t.Cleanup(func() { debug.SetMemoryLimit(prev) })

			t.Setenv("AUTOMEMLIMIT", "")
			require.NoError(t, os.Unsetenv("AUTOMEMLIMIT"))
			t.Setenv("GOMEMLIMIT", "")
			require.NoError(t, os.Unsetenv("GOMEMLIMIT"))
			if tc.gomemlimit != nil {
				t.Setenv("GOMEMLIMIT", *tc.gomemlimit)
			}

			setAutoMemLimit(t.Context(), loki.Config{AutoMemLimitEnabled: tc.enabled, AutoMemLimitRatio: 0.9}, log.NewNopLogger())

			got := debug.SetMemoryLimit(-1)
			if tc.wantChanged {
				require.Greater(t, got, int64(0))
				require.Less(t, got, int64(math.MaxInt64))
			} else {
				require.Equal(t, int64(math.MaxInt64), got)
			}
		})
	}
}
