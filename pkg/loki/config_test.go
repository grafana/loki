package loki

import (
	"flag"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func TestCrossComponentValidation(t *testing.T) {
	for _, tc := range []struct {
		desc string
		base *Config
		err  bool
	}{
		{
			desc: "correct shards",
			base: &Config{
				Ingester: ingester.Config{
					IndexShards: 32,
				},
				SchemaConfig: config.SchemaConfig{
					Configs: []config.PeriodConfig{
						{
							From: config.DayTime{
								Time: model.Now(),
							},
							IndexType:  types.TSDBType,
							ObjectType: types.StorageTypeS3,
							Schema:     "v11",
							IndexTables: config.IndexPeriodicTableConfig{
								PeriodicTableConfig: config.PeriodicTableConfig{
									Period: 24 * time.Hour,
								},
							},
							RowShards: 16,
						},
					},
				},
			},
			err: false,
		},
		{
			desc: "correct shards",
			base: &Config{
				Ingester: ingester.Config{
					IndexShards: 32,
				},
				SchemaConfig: config.SchemaConfig{
					Configs: []config.PeriodConfig{
						{
							IndexType:  types.BoltDBShipperType,
							ObjectType: types.StorageTypeS3,
							RowShards:  16,
							Schema:     "v11",
							From: config.DayTime{
								Time: model.Now().Add(-48 * time.Hour),
							},
							IndexTables: config.IndexPeriodicTableConfig{
								PeriodicTableConfig: config.PeriodicTableConfig{
									Period: 24 * time.Hour,
								},
							},
						},
						{
							IndexType:  types.BoltDBShipperType,
							ObjectType: types.StorageTypeS3,
							RowShards:  17,
							Schema:     "v11",
							From: config.DayTime{
								Time: model.Now(),
							},
							IndexTables: config.IndexPeriodicTableConfig{
								PeriodicTableConfig: config.PeriodicTableConfig{
									Period: 24 * time.Hour,
								},
							},
						},
					},
				},
			},
			err: true,
		},
	} {
		tc.base.RegisterFlags(flag.NewFlagSet(tc.desc, 0))
		// This test predates the newer schema required for structured metadata
		tc.base.LimitsConfig.AllowStructuredMetadata = false
		// Several caches will error if not configured, disabled them for this test
		tc.base.QueryRange.CacheIndexStatsResults = false
		tc.base.QueryRange.CacheSeriesResults = false
		tc.base.QueryRange.CacheLabelResults = false
		tc.base.QueryRange.CacheVolumeResults = false
		// Several other validations are required
		tc.base.CompactorConfig.WorkingDirectory = "tmp"
		tc.base.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory = "tmp"
		tc.base.StorageConfig.TSDBShipperConfig.CacheLocation = "tmp"
		tc.base.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory = "tmp"
		tc.base.StorageConfig.BoltDBShipperConfig.CacheLocation = "tmp"
		err := tc.base.Validate()
		if tc.err {
			require.NotNil(t, err)
		} else {
			require.Nil(t, err)
		}
	}
}

func TestShouldWarnQueryIngestersWithin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		queryIngestersWithin time.Duration
		maxChunkAge          time.Duration
		maxChunkIdle         time.Duration
		wantWarn             bool
		wantMaxRetention     time.Duration
	}{
		{
			name:                 "warn when query window shorter than max retention",
			queryIngestersWithin: 3 * time.Hour,
			maxChunkAge:          6 * time.Hour,
			maxChunkIdle:         2 * time.Hour,
			wantWarn:             true,
			wantMaxRetention:     6 * time.Hour,
		},
		{
			name:                 "no warn when query window covers retention",
			queryIngestersWithin: 6 * time.Hour,
			maxChunkAge:          3 * time.Hour,
			maxChunkIdle:         2 * time.Hour,
			wantWarn:             false,
			wantMaxRetention:     3 * time.Hour,
		},
		{
			name:                 "no warn when query window disabled",
			queryIngestersWithin: 0,
			maxChunkAge:          6 * time.Hour,
			maxChunkIdle:         2 * time.Hour,
			wantWarn:             false,
			wantMaxRetention:     6 * time.Hour,
		},
		{
			name:                 "no warn when no retention configured",
			queryIngestersWithin: 3 * time.Hour,
			maxChunkAge:          0,
			maxChunkIdle:         0,
			wantWarn:             false,
			wantMaxRetention:     0,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotWarn, gotMaxRetention := shouldWarnQueryIngestersWithin(
				tc.queryIngestersWithin,
				tc.maxChunkAge,
				tc.maxChunkIdle,
			)

			require.Equal(t, tc.wantWarn, gotWarn)
			require.Equal(t, tc.wantMaxRetention, gotMaxRetention)
		})
	}
}
