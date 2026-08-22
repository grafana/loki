package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_net "github.com/grafana/loki/v3/pkg/util/net"
	"github.com/grafana/loki/v3/pkg/validation"
)

const indexTablePrefix = "table_"
const localhost = "localhost"

func dayFromTime(t model.Time) config.DayTime {
	parsed, err := time.Parse("2006-01-02", t.Time().In(time.UTC).Format("2006-01-02"))
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(parsed.Unix()),
	}
}

var (
	start = model.Now().Add(-30 * 24 * time.Hour)
)

func setupTestCompactor(t *testing.T, objectClients map[config.DayTime]client.ObjectClient, periodConfigs []config.PeriodConfig, tempDir string, cfgOpts ...func(*Config)) *Compactor {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.WorkingDirectory = filepath.Join(tempDir, workingDirName)
	cfg.RetentionEnabled = true
	cfg.DeleteRequestStore = periodConfigs[len(periodConfigs)-1].ObjectType
	cfg.CompactorRing.InstanceAddr = localhost

	if loopbackIFace, err := loki_net.LoopbackInterfaceName(); err == nil {
		cfg.CompactorRing.InstanceInterfaceNames = append(cfg.CompactorRing.InstanceInterfaceNames, loopbackIFace)
	}

	for _, opt := range cfgOpts {
		opt(&cfg)
	}

	require.NoError(t, cfg.Validate())

	defaultLimits := validation.Limits{}
	flagext.DefaultValues(&defaultLimits)
	require.NoError(t, defaultLimits.RetentionPeriod.Set("30d"))

	overrides, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	c, err := NewCompactor(cfg, objectClients, objectClients[periodConfigs[len(periodConfigs)-1].From], config.SchemaConfig{
		Configs: periodConfigs,
	}, overrides, 0, prometheus.NewPedanticRegistry(), constants.Loki)
	require.NoError(t, err)

	c.RegisterIndexCompactor("dummy", testIndexCompactor{})

	return c
}

func TestCompactor_RunCompactionMultipleStores(t *testing.T) {
	tempDir := t.TempDir()

	commonDBsConfig := IndexesConfig{NumUnCompactedFiles: 5}
	perUserDBsConfig := PerUserIndexesConfig{}

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	periodOneStart := tableNumEnd - 10
	periodTwoStart := tableNumEnd - 5

	periodConfigs := []config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Time(0)},
			IndexType:  "dummy",
			ObjectType: "fs_01",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
		{
			From:       config.DayTime{Time: model.Time(periodTwoStart * daySeconds * 1000)},
			IndexType:  "dummy",
			ObjectType: "fs_02",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	periodOnePath := filepath.Join(tempDir, "p1")
	periodTwoPath := filepath.Join(tempDir, "p2")

	tablesPath := filepath.Join(periodOnePath, "index")
	for i := periodOneStart; i < periodTwoStart; i++ {
		SetupTable(t, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
	}

	tablesPath = filepath.Join(periodTwoPath, "index")
	for i := periodTwoStart; i < tableNumEnd; i++ {
		SetupTable(t, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
	}

	var (
		objectClients = map[config.DayTime]client.ObjectClient{}
		err           error
	)
	objectClients[periodConfigs[0].From], err = local.NewFSObjectClient(local.FSConfig{Directory: periodOnePath})
	require.NoError(t, err)

	objectClients[periodConfigs[1].From], err = local.NewFSObjectClient(local.FSConfig{Directory: periodTwoPath})
	require.NoError(t, err)

	compactor := setupTestCompactor(t, objectClients, periodConfigs, tempDir)
	err = compactor.tablesManager.runCompaction(context.Background(), false)
	require.NoError(t, err)

	for i := periodOneStart; i < periodTwoStart; i++ {
		name := fmt.Sprintf("%s%d", indexTablePrefix, i)
		// verify that we have only 1 file left in storage after compaction.
		files, err := os.ReadDir(filepath.Join(periodOnePath, "index", name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(periodOnePath, "index", name))
	}

	for i := periodTwoStart; i < tableNumEnd; i++ {
		name := fmt.Sprintf("%s%d", indexTablePrefix, i)
		// verify that we have only 1 file left in storage after compaction.
		files, err := os.ReadDir(filepath.Join(periodTwoPath, "index", name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(periodTwoPath, "index", name))
	}
}

// TestCompactor_SweepsOwnMarkersWithoutLeadership verifies that a compactor keeps
// processing the chunk deletion markers it wrote to its local disk even while it does
// not own the compaction. Leadership can move to another instance within the
// retention_delete_delay window, and markers on local disk are not visible to the other
// instances, so the chunks would otherwise never be deleted from the object store.
func TestCompactor_SweepsOwnMarkersWithoutLeadership(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "store")

	periodConfig := config.PeriodConfig{
		From:       config.DayTime{Time: model.Time(0)},
		IndexType:  "dummy",
		ObjectType: "filesystem",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: config.ObjectStorageIndexRequiredPeriod,
			}},
		RowShards: 16,
	}
	schemaConfig := config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: storePath})
	require.NoError(t, err)

	compactor := setupTestCompactor(t, map[config.DayTime]client.ObjectClient{periodConfig.From: objectClient},
		[]config.PeriodConfig{periodConfig}, tempDir, func(cfg *Config) {
			// markers become eligible for processing right away.
			cfg.RetentionDeleteDelay = 0
		})
	require.True(t, compactor.cfg.markersOnLocalDisk())

	// store a chunk and mark it for deletion the same way the compaction would.
	chunkRef := logproto.ChunkRef{
		UserID:      "user1",
		Fingerprint: 1,
		From:        start,
		Through:     start.Add(time.Hour),
		Checksum:    1,
	}
	chunkID := schemaConfig.ExternalKey(chunkRef)
	chunkKey := client.FSEncoder(schemaConfig, chunk.Chunk{ChunkRef: chunkRef})
	require.NoError(t, objectClient.PutObject(context.Background(), chunkKey, strings.NewReader("chunk")))

	markersClient, err := local.NewFSObjectClient(local.FSConfig{Directory: filepath.Join(
		compactor.cfg.WorkingDirectory, "retention", fmt.Sprintf("%s_%s", periodConfig.ObjectType, periodConfig.From.String()), MarkersFolder,
	)})
	require.NoError(t, err)

	markerWriter, err := retention.NewMarkerWriter(markersClient)
	require.NoError(t, err)
	require.NoError(t, markerWriter.Put([]byte(chunkID)))
	require.NoError(t, markerWriter.Close())

	// The ring is not running, so this instance never gets elected to run the compaction.
	// Even if it did, the compaction only starts after one compaction interval, so the
	// marker below can only be processed by a sweeper that does not depend on leadership.
	ctx, cancel := context.WithCancel(context.Background())
	loopStopped := make(chan struct{})
	var loopErr error
	go func() {
		defer close(loopStopped)
		loopErr = compactor.loop(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-loopStopped
		require.NoError(t, loopErr)
	})

	require.Eventually(t, func() bool {
		exists, err := objectClient.ObjectExists(context.Background(), chunkKey)
		return err == nil && !exists
	}, 30*time.Second, 100*time.Millisecond, "chunk marked for deletion was not swept")
}

func Test_schemaPeriodForTable(t *testing.T) {
	indexFromTime := func(t time.Time) string {
		return fmt.Sprintf("%d", t.Unix()/int64(24*time.Hour/time.Second))
	}
	tsdbIndexTablePrefix := fmt.Sprintf("%stsdb_", indexTablePrefix)
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{
		{
			From:       dayFromTime(start),
			IndexType:  "boltdb",
			ObjectType: "filesystem",
			Schema:     "v9",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: time.Hour * 24,
				}},
		},
		{
			From:       dayFromTime(start.Add(25 * time.Hour)),
			IndexType:  "boltdb",
			ObjectType: "filesystem",
			Schema:     "v12",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: time.Hour * 24,
				}},
		},
		{
			From:       dayFromTime(start.Add(73 * time.Hour)),
			IndexType:  "tsdb",
			ObjectType: "filesystem",
			Schema:     "v12",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: tsdbIndexTablePrefix,
					Period: time.Hour * 24,
				}},
		},
		{
			From:       dayFromTime(start.Add(100 * time.Hour)),
			IndexType:  "tsdb",
			ObjectType: "filesystem",
			Schema:     "v12",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: time.Hour * 24,
				}},
		},
	}}
	tests := []struct {
		name          string
		config        config.SchemaConfig
		tableName     string
		expected      config.PeriodConfig
		expectedFound bool
	}{
		{"out of scope", schemaCfg, indexTablePrefix + indexFromTime(start.Time().Add(-24*time.Hour)), config.PeriodConfig{}, false},
		{"first table", schemaCfg, indexTablePrefix + indexFromTime(dayFromTime(start).Time.Time()), schemaCfg.Configs[0], true},
		{"4 hour after first table", schemaCfg, indexTablePrefix + indexFromTime(dayFromTime(start).Time.Time().Add(4*time.Hour)), schemaCfg.Configs[0], true},
		{"second schema", schemaCfg, indexTablePrefix + indexFromTime(dayFromTime(start.Add(28*time.Hour)).Time.Time()), schemaCfg.Configs[1], true},
		{"third schema", schemaCfg, tsdbIndexTablePrefix + indexFromTime(dayFromTime(start.Add(75*time.Hour)).Time.Time()), schemaCfg.Configs[2], true},
		{"unexpected table prefix", schemaCfg, indexTablePrefix + indexFromTime(dayFromTime(start.Add(75*time.Hour)).Time.Time()), config.PeriodConfig{}, false},
		{"unexpected table number", schemaCfg, tsdbIndexTablePrefix + indexFromTime(time.Now()), config.PeriodConfig{}, false},
		{"now", schemaCfg, indexTablePrefix + indexFromTime(time.Now()), schemaCfg.Configs[3], true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, actualFound := SchemaPeriodForTable(tt.config, tt.tableName)
			require.Equal(t, tt.expectedFound, actualFound)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func Test_tableSort(t *testing.T) {
	intervals := []string{
		"index_19191",
		"index_19195",
		"index_19192",
	}

	SortTablesByRange(intervals)
	require.Equal(t, []string{"index_19195", "index_19192", "index_19191"}, intervals)
}
