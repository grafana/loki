package storage

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

func TestFactoryStop(t *testing.T) {
	var (
		cfg          Config
		storeConfig  chunk.StoreConfig
		schemaConfig chunk.SchemaConfig
		defaults     validation.Limits
	)
	util.DefaultValues(&cfg, &storeConfig, &schemaConfig, &defaults)
	schemaConfig.Configs = []chunk.PeriodConfig{
		{
			From:  model.Time(0),
			Store: "inmemory",
		},
		{
			From:  model.Time(1),
			Store: "inmemory",
		},
	}
	cfg.memcacheClient.Host = "localhost" // Fake address that should at least resolve.

	limits, err := validation.NewOverrides(defaults)
	require.NoError(t, err)

	store, err := NewStore(cfg, storeConfig, schemaConfig, limits)
	require.NoError(t, err)

	store.Stop()
}
