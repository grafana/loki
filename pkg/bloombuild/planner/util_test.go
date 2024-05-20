package planner

import (
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestSplitFingerprintKeyspaceByFactor(t *testing.T) {
	for _, tt := range []struct {
		name   string
		factor int
	}{
		{
			name:   "Factor is 0",
			factor: 0,
		},
		{
			name:   "Factor is 1",
			factor: 1,
		},
		{
			name:   "Factor is 256",
			factor: 256,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitFingerprintKeyspaceByFactor(tt.factor)

			if tt.factor == 0 {
				require.Empty(t, got)
				return
			}

			// Check overall min and max values of the ranges.
			require.Equal(t, model.Fingerprint(math.MaxUint64), got[len(got)-1].Max)
			require.Equal(t, model.Fingerprint(0), got[0].Min)

			// For each range, check that the max value of the previous range is one less than the min value of the current range.
			for i := 1; i < len(got); i++ {
				require.Equal(t, got[i-1].Max+1, got[i].Min)
			}
		})
	}
}
