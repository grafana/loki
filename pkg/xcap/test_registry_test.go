package xcap

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

// withTestStatisticRegistry installs an isolated registry for testing.
func withTestStatisticRegistry(t testing.TB) {
	t.Helper()

	statisticsMu.Lock()
	previousStatistics := statisticsByID
	statisticsByID = [statid.Count]Statistic{}
	statisticsMu.Unlock()

	t.Cleanup(func() {
		statisticsMu.Lock()
		statisticsByID = previousStatistics
		statisticsMu.Unlock()
	})
}
