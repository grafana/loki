package indexgateway_test

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

// The client can't import tsdb, so it computes tables itself. Check it picks
// exactly the tables the index gateway's TSDB store reads.
func TestSplitByTableMatchesIndexBuckets(t *testing.T) {
	const n = 20000
	day := model.Time(config.ObjectStorageIndexRequiredPeriod / time.Millisecond)
	tableRange := config.TableRange{
		Start: n,
		End:   n + 5,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: config.ObjectStorageIndexRequiredPeriod},
			},
		},
	}

	// Times around each midnight from before to after the period.
	var times []model.Time
	for d := n - 2; d <= n+8; d++ {
		start := model.Time(d) * day
		times = append(times, start-1, start, start+1, start+day/2)
	}

	for _, from := range times {
		for _, through := range times {
			if through < from {
				continue
			}
			var want []string
			for _, b := range tsdb.IndexBuckets(from, through, config.TableRanges{tableRange}) {
				want = append(want, b.Prefix)
			}
			var got []string
			for _, p := range indexgateway.SplitByTable(from, through, tableRange) {
				got = append(got, p.Table)
				require.LessOrEqual(t, p.From, p.Through)
				require.Equal(t, int64(p.From/day), int64(p.Through/day), "part %+v spans tables", p)
			}
			require.Equal(t, want, got, "from %d through %d", from, through)
		}
	}
}
