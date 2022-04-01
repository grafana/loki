package storage

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/pkg/storage/config"
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
