package storage

import (
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
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
	store, err := NewStore(cfg, storeConfig, schemaConfig, limits, cm, nil, log.NewNopLogger())
	require.NoError(t, err)

	store.Stop()
}

func TestNamedStores(t *testing.T) {
	tempDir := t.TempDir()

	// config for BoltDB Shipper
	boltdbShipperConfig := shipper.Config{}
	flagext.DefaultValues(&boltdbShipperConfig)
	boltdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "index")
	boltdbShipperConfig.SharedStoreType = "filesystem.named-store"
	boltdbShipperConfig.CacheLocation = path.Join(tempDir, "boltdb-shipper-cache")
	boltdbShipperConfig.Mode = indexshipper.ModeReadWrite

	cfg := Config{
		NamedStores: NamedStores{
			Filesystem: map[string]local.FSConfig{
				"named-store": {Directory: path.Join(tempDir, "chunks")},
			},
		},
		BoltDBShipperConfig: boltdbShipperConfig,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(parseDate("2019-01-01"))},
				IndexType:  "boltdb-shipper",
				ObjectType: "filesystem.named-store",
				Schema:     "v9",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
			},
		},
	}

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	t.Run("refer to named store", func(t *testing.T) {
		store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
		require.NoError(t, err)

		store.Stop()
	})

	t.Run("refer to unrecognized store", func(t *testing.T) {
		schemaConfig := schemaConfig
		schemaConfig.Configs[0].ObjectType = "filesystem.not-found"
		_, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
		require.Error(t, err)
		require.Contains(t, err.Error(), "Unrecognized named filesystem storage config filesystem.not-found")
	})
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

	store, err := NewStore(cfg, storeConfig, schemaCfg, limits, cm, nil, log.NewNopLogger())
	require.NoError(t, err)

	store.Stop()
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
			IndexTables: config.PeriodicTableConfig{
				Prefix: "cortex_chunks",
				Period: 7 * 24 * time.Hour,
			},
		}},
	}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}
