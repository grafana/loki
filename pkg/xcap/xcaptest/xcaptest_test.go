package xcaptest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestNewRegion(t *testing.T) {
	requests := xcap.NewStatisticInt64("xcaptest.requests", xcap.AggregationTypeSum)
	bytes := xcap.NewStatisticInt64("xcaptest.bytes", xcap.AggregationTypeSum)

	t.Run("returns the sum of the values that the code recorded in the region", func(t *testing.T) {
		ctx, recorded := NewRegion(t)

		region := xcap.RegionFromContext(ctx)
		region.Record(requests.Observe(1))
		region.Record(requests.Observe(2))

		require.Equal(t, int64(3), recorded(requests))
	})

	t.Run("returns zero for a statistic that the code did not record", func(t *testing.T) {
		ctx, recorded := NewRegion(t)

		xcap.RegionFromContext(ctx).Record(requests.Observe(1))

		require.Zero(t, recorded(bytes))
	})

	t.Run("keeps the values of two regions apart", func(t *testing.T) {
		ctx, recorded := NewRegion(t)
		_, otherRecorded := NewRegion(t)

		xcap.RegionFromContext(ctx).Record(requests.Observe(1))

		require.Equal(t, int64(1), recorded(requests))
		require.Zero(t, otherRecorded(requests))
	})
}
