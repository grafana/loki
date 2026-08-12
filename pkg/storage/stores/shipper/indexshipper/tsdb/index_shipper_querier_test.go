package tsdb

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// Test_IndexShipperQuerier_BindsChunkFilterer checks that the filterer is bound
// where indices enter the read path, not where they are built. The shipper hands out
// indices from two places -- ones it downloaded and ones built locally and added by
// the tsdb manager -- and the locally built ones are constructed without a filterer,
// so binding has to happen here or LBAC filtered series would be served.
func Test_IndexShipperQuerier_BindsChunkFilterer(t *testing.T) {
	tableRange := config.TableRange{
		Start: 0,
		End:   math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}
	indexStart := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	matchAll := labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+")

	querySeries := func(t *testing.T, chunkFilter chunk.RequestChunkFilterer) []Series {
		// Built with no filterer of its own, exactly as the tsdb manager builds the
		// indices it hands to the shipper.
		idx := BuildIndex(t, t.TempDir(), nil, []LoadableSeries{
			{
				Labels: mustParseLabels(`{foo="bar"}`),
				Chunks: buildChunkMetas(int64(indexStart), int64(indexStart+99)),
			},
		})

		shipper := mockIndexShipperIndexIterator{tables: map[string][]*TSDBFile{
			tableRange.PeriodConfig.IndexTables.TableFor(indexStart): {idx},
		}}
		querier := newIndexShipperQuerier(shipper, tableRange, chunkFilter)

		got, err := querier.Series(context.Background(), "fake", indexStart, indexStart+100, nil, nil, matchAll)
		require.Nil(t, err)
		return got
	}

	t.Run("without a filterer the series is returned", func(t *testing.T) {
		require.Len(t, querySeries(t, nil), 1)
	})

	t.Run("a filterAll filterer filters the series out", func(t *testing.T) {
		require.Len(t, querySeries(t, &filterAll{}), 0)
	})
}
