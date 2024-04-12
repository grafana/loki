package storage

import (
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/boltdb"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestFactoryStop(t *testing.T) {
	var (
		cfg          Config
		storeConfig  config.ChunkStoreConfig
		schemaConfig config.SchemaConfig
		defaults     validation.Limits
	)
	flagext.DefaultValues(&cfg, &storeConfig, &schemaConfig, &defaults)
	schemaConfig.Configs = []config.PeriodConfig{
		{
			From:      config.DayTime{Time: model.Time(0)},
			IndexType: "inmemory",
			Schema:    "v11",
			RowShards: 16,
		},
		{
			From:      config.DayTime{Time: model.Time(1)},
			IndexType: "inmemory",
			Schema:    "v9",
		},
	}

	limits, err := validation.NewOverrides(defaults, nil)
	require.NoError(t, err)
	store, err := NewStore(cfg, storeConfig, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)

	store.Stop()
}

func TestCassandraInMultipleSchemas(t *testing.T) {
	addresses := os.Getenv("CASSANDRA_TEST_ADDRESSES")
	if addresses == "" {
		return
	}

	// cassandra config
	var cassandraCfg cassandra.Config
	flagext.DefaultValues(&cassandraCfg)
	cassandraCfg.Addresses = addresses
	cassandraCfg.Keyspace = "test"
	cassandraCfg.Consistency = "QUORUM"
	cassandraCfg.ReplicationFactor = 1

	// build schema with cassandra in multiple periodic configs
	schemaCfg := DefaultSchemaConfig("cassandra", "v1", model.Now().Add(-7*24*time.Hour))
	newSchemaCfg := schemaCfg.Configs[0]
	newSchemaCfg.Schema = "v2"
	newSchemaCfg.From = config.DayTime{Time: model.Now()}

	schemaCfg.Configs = append(schemaCfg.Configs, newSchemaCfg)

	var (
		cfg         Config
		storeConfig config.ChunkStoreConfig
		defaults    validation.Limits
	)
	flagext.DefaultValues(&cfg, &storeConfig, &defaults)
	cfg.CassandraStorageConfig = cassandraCfg

	limits, err := validation.NewOverrides(defaults, nil)
	require.NoError(t, err)

	store, err := NewStore(cfg, storeConfig, schemaCfg, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)

	store.Stop()
}

func TestNamedStores(t *testing.T) {
	tempDir := t.TempDir()

	// config for BoltDB Shipper
	boltdbShipperConfig := boltdb.IndexCfg{}
	flagext.DefaultValues(&boltdbShipperConfig)
	boltdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "index")
	boltdbShipperConfig.CacheLocation = path.Join(tempDir, "boltdb-shipper-cache")
	boltdbShipperConfig.Mode = indexshipper.ModeReadWrite

	cfg := Config{
		NamedStores: NamedStores{
			Filesystem: map[string]NamedFSConfig{
				"named-store": {Directory: path.Join(tempDir, "named-store")},
			},
		},
		FSConfig: local.FSConfig{
			Directory: path.Join(tempDir, "default"),
		},
		BoltDBShipperConfig: boltdbShipperConfig,
	}
	require.NoError(t, cfg.NamedStores.Validate())

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(parseDate("2019-01-01"))},
				IndexType:  "boltdb-shipper",
				ObjectType: "named-store",
				Schema:     "v9",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 168,
					}},
			},
		},
	}

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	t.Run("period config referring to configured named store", func(t *testing.T) {
		err := os.Remove(cfg.NamedStores.Filesystem["named-store"].Directory)
		if err != nil {
			require.True(t, os.IsNotExist(err))
		}

		err = os.Remove(cfg.FSConfig.Directory)
		if err != nil {
			require.True(t, os.IsNotExist(err))
		}

		store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger, constants.Loki)
		require.NoError(t, err)
		defer store.Stop()

		// FSObjectClient creates the configured dir on init, ensure that correct cfg is picked by checking for it.
		_, err = os.Stat(cfg.NamedStores.Filesystem["named-store"].Directory)
		require.NoError(t, err)

		// dir specified in StorageConfig/FSConfig should not be created as we are not referring to it.
		_, err = os.Stat(cfg.FSConfig.Directory)
		require.True(t, os.IsNotExist(err))

	})

	t.Run("period config referring to unrecognized store", func(t *testing.T) {
		schemaConfig := schemaConfig
		schemaConfig.Configs[0].ObjectType = "not-found"
		_, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger, constants.Loki)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unrecognized chunk client type not-found, choose one of:")
	})
}

func TestNamedStores_populateStoreType(t *testing.T) {
	t.Run("found duplicates", func(t *testing.T) {
		ns := NamedStores{
			AWS: map[string]NamedAWSStorageConfig{
				"store-1": {},
				"store-2": {},
			},
			GCS: map[string]NamedGCSConfig{
				"store-1": {},
			},
		}

		err := ns.populateStoreType()
		require.ErrorContains(t, err, `named store "store-1" is already defined under`)

	})

	t.Run("illegal store name", func(t *testing.T) {
		ns := NamedStores{
			GCS: map[string]NamedGCSConfig{
				"aws": {},
			},
		}

		err := ns.populateStoreType()
		require.ErrorContains(t, err, `named store "aws" should not match with the name of a predefined storage type`)

	})

	t.Run("lookup populated entries", func(t *testing.T) {
		ns := NamedStores{
			AWS: map[string]NamedAWSStorageConfig{
				"store-1": {},
				"store-2": {},
			},
			GCS: map[string]NamedGCSConfig{
				"store-3": {},
			},
		}

		err := ns.populateStoreType()
		require.NoError(t, err)

		storeType, ok := ns.storeType["store-1"]
		assert.True(t, ok)
		assert.Equal(t, types.StorageTypeAWS, storeType)

		storeType, ok = ns.storeType["store-2"]
		assert.True(t, ok)
		assert.Equal(t, types.StorageTypeAWS, storeType)

		storeType, ok = ns.storeType["store-3"]
		assert.True(t, ok)
		assert.Equal(t, types.StorageTypeGCS, storeType)

		_, ok = ns.storeType["store-4"]
		assert.False(t, ok)
	})
}

func TestNewObjectClient_prefixing(t *testing.T) {
	t.Run("no prefix", func(t *testing.T) {
		var cfg Config
		flagext.DefaultValues(&cfg)

		objectClient, err := NewObjectClient("inmemory", cfg, cm)
		require.NoError(t, err)

		_, ok := objectClient.(client.PrefixedObjectClient)
		assert.False(t, ok)
	})

	t.Run("prefix with trailing /", func(t *testing.T) {
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.ObjectPrefix = "my/prefix/"

		objectClient, err := NewObjectClient("inmemory", cfg, cm)
		require.NoError(t, err)

		prefixed, ok := objectClient.(client.PrefixedObjectClient)
		assert.True(t, ok)
		assert.Equal(t, "my/prefix/", prefixed.GetPrefix())
	})

	t.Run("prefix without trailing /", func(t *testing.T) {
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.ObjectPrefix = "my/prefix"

		objectClient, err := NewObjectClient("inmemory", cfg, cm)
		require.NoError(t, err)

		prefixed, ok := objectClient.(client.PrefixedObjectClient)
		assert.True(t, ok)
		assert.Equal(t, "my/prefix/", prefixed.GetPrefix())
	})

	t.Run("prefix with starting and trailing /", func(t *testing.T) {
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.ObjectPrefix = "/my/prefix/"

		objectClient, err := NewObjectClient("inmemory", cfg, cm)
		require.NoError(t, err)

		prefixed, ok := objectClient.(client.PrefixedObjectClient)
		assert.True(t, ok)
		assert.Equal(t, "my/prefix/", prefixed.GetPrefix())
	})
}

// DefaultSchemaConfig creates a simple schema config for testing
func DefaultSchemaConfig(store, schema string, from model.Time) config.SchemaConfig {
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{{
			IndexType: store,
			Schema:    schema,
			From:      config.DayTime{Time: from},
			ChunkTables: config.PeriodicTableConfig{
				Prefix: "cortex",
				Period: 7 * 24 * time.Hour,
			},
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: "cortex_chunks",
					Period: 7 * 24 * time.Hour,
				}},
		}},
	}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}
