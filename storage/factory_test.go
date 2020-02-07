package storage

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
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

	limits, err := validation.NewOverrides(defaults, nil)
	require.NoError(t, err)

	store, err := NewStore(cfg, storeConfig, schemaConfig, limits)
	require.NoError(t, err)

	store.Stop()
}

type customBoltDBIndexClient struct {
	*local.BoltIndexClient
}

func newBoltDBCustomIndexClient(cfg local.BoltDBConfig) (chunk.IndexClient, error) {
	boltdbClient, err := local.NewBoltDBIndexClient(cfg)
	if err != nil {
		return nil, err
	}

	return &customBoltDBIndexClient{boltdbClient}, nil
}

type anotherIndexClient struct {
	*local.BoltIndexClient
}

func newAnotherIndexClient(cfg local.BoltDBConfig) (chunk.IndexClient, error) {
	boltdbClient, err := local.NewBoltDBIndexClient(cfg)
	if err != nil {
		return nil, err
	}

	return &anotherIndexClient{boltdbClient}, nil
}

func TestCustomIndexClient(t *testing.T) {
	cfg := Config{}
	schemaCfg := chunk.SchemaConfig{}

	dirname, err := ioutil.TempDir(os.TempDir(), "boltdb")
	if err != nil {
		return
	}
	cfg.BoltDBConfig.Directory = dirname

	// register custom index clients, overwriting boltdb client and a new one with different name
	RegisterIndexClient("boltdb", func() (client chunk.IndexClient, e error) {
		return newBoltDBCustomIndexClient(cfg.BoltDBConfig)
	})

	RegisterIndexClient("another-index-client", func() (client chunk.IndexClient, e error) {
		return newAnotherIndexClient(cfg.BoltDBConfig)
	})

	// try creating a new index client for boltdb
	indexClient, err := NewIndexClient("boltdb", cfg, schemaCfg)
	require.NoError(t, err)

	// check whether we got custom boltdb index client type registered above
	_, ok := indexClient.(*customBoltDBIndexClient)
	require.Equal(t, true, ok)

	// check whether non-existent index client returns an error
	_, err = NewIndexClient("boltdb1", cfg, schemaCfg)
	require.Error(t, err)

	// try creating a new index client for another index client
	indexClient, err = NewIndexClient("another-index-client", cfg, schemaCfg)
	require.NoError(t, err)

	// check whether we got another index client type registered above
	_, ok = indexClient.(*anotherIndexClient)
	require.Equal(t, true, ok)

	unregisterAllCustomIndexClients()

	// try creating a new index client for boltdb
	indexClient, err = NewIndexClient("boltdb", cfg, schemaCfg)
	require.NoError(t, err)

	// check whether we got original boltdb index client
	_, ok = indexClient.(*local.BoltIndexClient)
	require.Equal(t, true, ok)
}
