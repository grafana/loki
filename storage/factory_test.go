package storage

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

func TestFactoryStop(t *testing.T) {
	var (
		cfg          Config
		storeConfig  chunk.StoreConfig
		schemaConfig chunk.SchemaConfig
		defaults     validation.Limits
	)
	flagext.DefaultValues(&cfg, &storeConfig, &schemaConfig, &defaults)
	schemaConfig.Configs = []chunk.PeriodConfig{
		{
			From:      chunk.DayTime{Time: model.Time(0)},
			IndexType: "inmemory",
		},
		{
			From:      chunk.DayTime{Time: model.Time(1)},
			IndexType: "inmemory",
		},
	}

	limits, err := validation.NewOverrides(defaults)
	require.NoError(t, err)

	store, err := NewStore(cfg, storeConfig, schemaConfig, limits)
	require.NoError(t, err)

	store.Stop()
}
