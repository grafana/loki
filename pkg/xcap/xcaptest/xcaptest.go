// Package xcaptest provides helpers for tests that check the statistics that code records in xcap
// regions.
package xcaptest

import (
	"context"
	"testing"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// regionName is the name of the region that NewRegion starts.
const regionName = "test"

// NewRegion starts a capture and a region, and returns a context that holds the region. Code that
// records statistics with that context records them in the region.
//
// It also returns a function that returns the sum of the values that the code recorded for an
// integer statistic in the region. The function returns zero for a statistic that the code did not
// record.
func NewRegion(t testing.TB) (ctx context.Context, recorded func(stat xcap.Statistic) int64) {
	t.Helper()

	ctx, capture := xcap.NewCapture(t.Context(), nil)
	ctx, _ = xcap.StartRegion(ctx, regionName)
	return ctx, func(stat xcap.Statistic) int64 {
		return xcap.ValueFromRegion[int64](capture, regionName, stat)
	}
}
