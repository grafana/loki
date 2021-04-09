package compactor

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsDefaults(t *testing.T) {
	for i, tc := range []struct {
		in  *Config
		out bool
	}{
		{&Config{
			WorkingDirectory: "/tmp",
		}, false},
		{&Config{}, false},
		{&Config{
			SharedStoreKeyPrefix: "index/",
			CompactionInterval:   2 * time.Hour,
		}, true},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.out, tc.in.IsDefaults())
		})
	}
}
