package bloomcompactor

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	ww "github.com/grafana/dskit/server"
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
	util_log "github.com/grafana/loki/pkg/util/log"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

const (
	indexTablePrefix = "table_"
	workingDirName   = "working-dir"
)

func TestCompactor_RunCompaction(t *testing.T) {
	servercfg := &ww.Config{}
	require.Nil(t, servercfg.LogLevel.Set("debug"))
	util_log.InitLogger(servercfg, nil, true, false)

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

	kvStore, cleanUp := consul.NewInMemoryClient(ring.GetCodec(), util_log.Logger, nil)
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

	clientMetrics := storage.NewClientMetrics()
	t.Cleanup(clientMetrics.Unregister)

	ringManager, err := lokiring.NewRingManager("bloom-compactor", lokiring.ServerMode, cfg.Ring, 1, 1, util_log.Logger, prometheus.DefaultRegisterer)
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

	c, err := New(cfg, storageConfig, schemaCfg, overrides, util_log.Logger, shuffleSharding, clientMetrics, nil)
	require.NoError(t, err)

	err = c.runCompaction(context.Background())
	require.NoError(t, err)

	// TODO: Once compaction is implemented, verify compaction here.
}
