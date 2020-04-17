package storage

import (
	"io/ioutil"
	"os"
	"reflect"
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
			Schema:    "v3",
		},
		{
			From:      chunk.DayTime{Time: model.Time(1)},
			IndexType: "inmemory",
			Schema:    "v9",
		},
	}

	limits, err := validation.NewOverrides(defaults, nil)
	require.NoError(t, err)

	store, err := NewStore(cfg, storeConfig, schemaConfig, limits, nil)
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

type customBoltDBTableClient struct {
	chunk.TableClient
}

func newBoltDBCustomTableClient(directory string) (chunk.TableClient, error) {
	tableClient, err := local.NewTableClient(directory)
	if err != nil {
		return nil, err
	}

	return &customBoltDBTableClient{tableClient}, nil
}

func TestCustomIndexClient(t *testing.T) {
	cfg := Config{}
	schemaCfg := chunk.SchemaConfig{}

	dirname, err := ioutil.TempDir(os.TempDir(), "boltdb")
	if err != nil {
		return
	}
	cfg.BoltDBConfig.Directory = dirname

	for _, tc := range []struct {
		indexClientName         string
		indexClientFactories    indexStoreFactories
		errorExpected           bool
		expectedIndexClientType reflect.Type
		expectedTableClientType reflect.Type
	}{
		{
			indexClientName:         "boltdb",
			expectedIndexClientType: reflect.TypeOf(&local.BoltIndexClient{}),
			expectedTableClientType: reflect.TypeOf(&local.TableClient{}),
		},
		{
			indexClientName: "boltdb",
			indexClientFactories: indexStoreFactories{
				indexClientFactoryFunc: func() (client chunk.IndexClient, e error) {
					return newBoltDBCustomIndexClient(cfg.BoltDBConfig)
				},
			},
			expectedIndexClientType: reflect.TypeOf(&customBoltDBIndexClient{}),
			expectedTableClientType: reflect.TypeOf(&local.TableClient{}),
		},
		{
			indexClientName: "boltdb",
			indexClientFactories: indexStoreFactories{
				tableClientFactoryFunc: func() (client chunk.TableClient, e error) {
					return newBoltDBCustomTableClient(cfg.BoltDBConfig.Directory)
				},
			},
			expectedIndexClientType: reflect.TypeOf(&local.BoltIndexClient{}),
			expectedTableClientType: reflect.TypeOf(&customBoltDBTableClient{}),
		},
		{
			indexClientName: "boltdb",
			indexClientFactories: indexStoreFactories{
				indexClientFactoryFunc: func() (client chunk.IndexClient, e error) {
					return newBoltDBCustomIndexClient(cfg.BoltDBConfig)
				},
				tableClientFactoryFunc: func() (client chunk.TableClient, e error) {
					return newBoltDBCustomTableClient(cfg.BoltDBConfig.Directory)
				},
			},
			expectedIndexClientType: reflect.TypeOf(&customBoltDBIndexClient{}),
			expectedTableClientType: reflect.TypeOf(&customBoltDBTableClient{}),
		},
		{
			indexClientName: "boltdb1",
			errorExpected:   true,
		},
	} {
		if tc.indexClientFactories.indexClientFactoryFunc != nil || tc.indexClientFactories.tableClientFactoryFunc != nil {
			RegisterIndexStore(tc.indexClientName, tc.indexClientFactories.indexClientFactoryFunc, tc.indexClientFactories.tableClientFactoryFunc)
		}

		indexClient, err := NewIndexClient(tc.indexClientName, cfg, schemaCfg)
		if tc.errorExpected {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.expectedIndexClientType, reflect.TypeOf(indexClient))
		}

		tableClient, err := NewTableClient(tc.indexClientName, cfg)
		if tc.errorExpected {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.expectedTableClientType, reflect.TypeOf(tableClient))
		}
		unregisterAllCustomIndexStores()
	}
}

// useful for cleaning up state after tests
func unregisterAllCustomIndexStores() {
	customIndexStores = map[string]indexStoreFactories{}
}
