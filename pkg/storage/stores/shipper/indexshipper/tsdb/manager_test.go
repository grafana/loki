package tsdb

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func TestIndexBuckets(t *testing.T) {
	var (
		day0    = model.Time(0)
		day1    = day0.Add(24 * time.Hour)
		day2    = day1.Add(24 * time.Hour)
		periods = []config.PeriodConfig{
			{
				From:      config.NewDayTime(day0),
				Schema:    "v12",
				IndexType: "tsdb",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index/",
						Period: 24 * time.Hour,
					},
				},
			},
			{
				From:      config.NewDayTime(day2),
				Schema:    "v13",
				IndexType: "tsdb",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index2/",
						Period: 24 * time.Hour,
					},
				},
			},
		}

		tableRanges = config.GetIndexStoreTableRanges(types.TSDBType, periods)
	)
	tests := []struct {
		name         string
		from         model.Time
		through      model.Time
		expectedInfo []IndexInfo
	}{
		{
			name:    "single table range",
			from:    day0,
			through: day2,
			expectedInfo: []IndexInfo{
				{BucketStart: day0, TsdbFormat: index.FormatV2, Prefix: "index/0"},
				{BucketStart: day1, TsdbFormat: index.FormatV2, Prefix: "index/1"},
				{BucketStart: day2, TsdbFormat: index.FormatV3, Prefix: "index2/2"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := IndexBuckets(tc.from, tc.through, tableRanges)
			require.Equal(t, tc.expectedInfo, res)
		})
	}
}
