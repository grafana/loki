package loki

import (
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func TestCrossComponentValidation(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		setup func(*Config)
		err   string
	}{
		{
			desc: "correct shards",
		},
		{
			desc: "storage lag too short for the ToC resolver with data objects disabled",
			setup: func(c *Config) {
				c.DataObj.StorageLag = time.Minute
			},
		},
		{
			desc: "storage lag too short for the ToC resolver with data objects enabled",
			setup: func(c *Config) {
				c.DataObj.Enabled = true
				c.DataObj.StorageLag = time.Minute
			},
			err: "CONFIG ERROR: invalid dataobj metastore config: twice the refresh interval of the ToC resolver (2m0s) must not exceed a tenth of the storage lag (1m0s)",
		},
	} {
		cfg := &Config{
			Ingester: ingester.Config{
				IndexShards: 32,
			},
			SchemaConfig: config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From: config.DayTime{
							Time: model.Now(),
						},
						IndexType:  types.IndexTypeTSDB,
						ObjectType: types.StorageTypeS3,
						Schema:     "v11",
						IndexTables: config.IndexPeriodicTableConfig{
							PeriodicTableConfig: config.PeriodicTableConfig{
								Period: 24 * time.Hour,
							},
						},
					},
				},
			},
		}
		flagext.DefaultValues(cfg)
		// This test predates the newer schema required for structured metadata
		cfg.LimitsConfig.AllowStructuredMetadata = false
		// Several caches will error if not configured, disabled them for this test
		cfg.QueryRange.CacheIndexStatsResults = false
		cfg.QueryRange.CacheSeriesResults = false
		cfg.QueryRange.CacheLabelResults = false
		cfg.QueryRange.CacheVolumeResults = false
		// Several other validations are required
		cfg.CompactorConfig.WorkingDirectory = "tmp"
		cfg.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory = "tmp"
		cfg.StorageConfig.TSDBShipperConfig.CacheLocation = "tmp"
		if tc.setup != nil {
			tc.setup(cfg)
		}

		err := cfg.Validate()
		if tc.err != "" {
			require.EqualError(t, err, tc.err, tc.desc)
		} else {
			require.NoError(t, err, tc.desc)
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
