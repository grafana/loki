package bloomcompactor

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/compactor"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

const (
	indexTablePrefix = "table_"
	workingDirName   = "working-dir"
)

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func TestCompactor_StartStopService(t *testing.T) {
	shardingStrategy := NewNoopStrategy()
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	var limits validation.Limits
	limits.RegisterFlags(flag.NewFlagSet("limits", flag.PanicOnError))
	overrides, _ := validation.NewOverrides(limits, nil)

	periodConfigUnsupported := config.PeriodConfig{
		From:       parseDayTime("2023-09-01"),
		IndexType:  config.BoltDBShipperType,
		ObjectType: config.StorageTypeFileSystem,
		Schema:     "v13",
		RowShards:  16,
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: config.ObjectStorageIndexRequiredPeriod,
			},
		},
	}

	periodConfigSupported := config.PeriodConfig{
		From:       parseDayTime("2023-10-01"),
		IndexType:  config.TSDBType,
		ObjectType: config.StorageTypeFileSystem,
		Schema:     "v13",
		RowShards:  16,
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: config.ObjectStorageIndexRequiredPeriod,
			},
		},
	}

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			periodConfigUnsupported,
			periodConfigSupported,
		},
	}

	fsDir := t.TempDir()
	tsdbDir := t.TempDir()

	storageCfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: fsDir,
		},
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: filepath.Join(tsdbDir, "index"),
			ResyncInterval:       1 * time.Minute,
			Mode:                 indexshipper.ModeReadWrite,
			CacheLocation:        filepath.Join(tsdbDir, "cache"),
		},
	}

	t.Run("ignore unsupported index types in schema config", func(t *testing.T) {
		kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), logger, reg)
		t.Cleanup(func() {
			closer.Close()
		})

		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.Enabled = true
		cfg.WorkingDirectory = filepath.Join(t.TempDir(), workingDirName)
		cfg.Ring = lokiring.RingConfig{
			KVStore: kv.Config{
				Mock: kvStore,
			},
		}

		c, err := New(cfg, storageCfg, schemaCfg, overrides, logger, shardingStrategy, cm, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), c)
		require.NoError(t, err)

		require.Equal(t, 1, len(c.storeClients))

		// supported index type TSDB is present
		sc, ok := c.storeClients[periodConfigSupported.From]
		require.True(t, ok)
		require.NotNil(t, sc)

		// unsupported index type BoltDB is not present
		_, ok = c.storeClients[periodConfigUnsupported.From]
		require.False(t, ok)

		err = services.StopAndAwaitTerminated(context.Background(), c)
		require.NoError(t, err)
	})
}

func TestCompactor_RunCompaction(t *testing.T) {
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	tempDir := t.TempDir()
	indexDir := filepath.Join(tempDir, "index")

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: model.Time(0)},
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v12",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: indexTablePrefix,
						Period: config.ObjectStorageIndexRequiredPeriod,
					}},
			},
		},
	}

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	tableNumStart := tableNumEnd - 5
	for i := tableNumStart; i <= tableNumEnd; i++ {
		compactor.SetupTable(
			t,
			filepath.Join(indexDir, fmt.Sprintf("%s%d", indexTablePrefix, i)),
			compactor.IndexesConfig{
				NumUnCompactedFiles: 5,
				NumCompactedFiles:   5,
			},
			compactor.PerUserIndexesConfig{
				NumUsers: 5,
				IndexesConfig: compactor.IndexesConfig{
					NumUnCompactedFiles: 5,
					NumCompactedFiles:   5,
				},
			},
		)
	}

	kvStore, cleanUp := consul.NewInMemoryClient(ring.GetCodec(), logger, nil)
	t.Cleanup(func() { assert.NoError(t, cleanUp.Close()) })

	var cfg Config
	flagext.DefaultValues(&cfg)
	cfg.WorkingDirectory = filepath.Join(tempDir, workingDirName)
	cfg.Ring.KVStore.Mock = kvStore
	cfg.Ring.ListenPort = 0
	cfg.Ring.InstanceAddr = "bloomcompactor"
	cfg.Ring.InstanceID = "bloomcompactor"

	storageConfig := storage.Config{
		FSConfig: local.FSConfig{Directory: tempDir},
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: indexDir,
			ResyncInterval:       1 * time.Minute,
			Mode:                 indexshipper.ModeReadWrite,
			CacheLocation:        filepath.Join(tempDir, "cache"),
		},
	}

	var limits validation.Limits
	limits.RegisterFlags(flag.NewFlagSet("limits", flag.PanicOnError))
	overrides, _ := validation.NewOverrides(limits, nil)

	ringManager, err := lokiring.NewRingManager("bloom-compactor", lokiring.ServerMode, cfg.Ring, 1, 1, logger, reg)
	require.NoError(t, err)

	err = ringManager.StartAsync(context.Background())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return ringManager.State() == services.Running
	}, 1*time.Minute, 100*time.Millisecond)
	defer func() {
		ringManager.StopAsync()
		require.Eventually(t, func() bool {
			return ringManager.State() == services.Terminated
		}, 1*time.Minute, 100*time.Millisecond)
	}()

	shuffleSharding := NewShuffleShardingStrategy(ringManager.Ring, ringManager.RingLifecycler, overrides)

	c, err := New(cfg, storageConfig, schemaCfg, overrides, logger, shuffleSharding, cm, nil)
	require.NoError(t, err)

	err = c.runCompaction(context.Background())
	require.NoError(t, err)

	// TODO: Once compaction is implemented, verify compaction here.
}
