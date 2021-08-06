package loki

import (
	"flag"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
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
				SchemaConfig: storage.SchemaConfig{
					SchemaConfig: chunk.SchemaConfig{
						Configs: []chunk.PeriodConfig{
							{
								// zero should not error
								RowShards: 0,
								Schema:    "v6",
								From: chunk.DayTime{
									Time: model.Now().Add(-48 * time.Hour),
								},
							},
							{
								RowShards: 16,
								Schema:    "v11",
								From: chunk.DayTime{
									Time: model.Now(),
								},
							},
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
				SchemaConfig: storage.SchemaConfig{
					SchemaConfig: chunk.SchemaConfig{
						Configs: []chunk.PeriodConfig{
							{
								RowShards: 16,
								Schema:    "v11",
								From: chunk.DayTime{
									Time: model.Now().Add(-48 * time.Hour),
								},
							},
							{
								RowShards: 17,
								Schema:    "v11",
								From: chunk.DayTime{
									Time: model.Now(),
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
		err := tc.base.Validate()
		if tc.err {
			require.NotNil(t, err)
		} else {
			require.Nil(t, err)
		}
	}
}
