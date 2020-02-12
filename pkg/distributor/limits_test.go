package distributor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// utility for testing limits
type constLimits int

func (c constLimits) MaxLineSize(userID string) int {
	return int(c)
}

func (c constLimits) EnforceMetricName(userID string) bool { return int(c) > 0 }

func (c constLimits) MaxLabelNamesPerSeries(userID string) int { return int(c) }

func (c constLimits) MaxLabelNameLength(userID string) int { return int(c) }

func (c constLimits) MaxLabelValueLength(userID string) int { return int(c) }

func (c constLimits) CreationGracePeriod(userID string) time.Duration { return time.Duration(c) }

func (c constLimits) RejectOldSamples(userID string) bool { return c > 0 }

func (c constLimits) RejectOldSamplesMaxAge(userID string) time.Duration { return time.Duration(c) }

func TestLimits(t *testing.T) {
	require.Equal(t, 0, constLimits(0).MaxLineSize("a"))
	require.Equal(t, 2, constLimits(2).MaxLineSize("a"))
	require.Equal(t, 1,
		PriorityLimits([]Limits{
			constLimits(0),
			constLimits(1),
		}).MaxLineSize("a"),
	)
}
