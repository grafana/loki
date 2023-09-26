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
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	loki_net "github.com/grafana/loki/pkg/util/net"
)

const indexTablePrefix = "table_"

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

func setupTestCompactor(t *testing.T, objectClients map[string]client.ObjectClient, periodConfigs []config.PeriodConfig, tempDir string) *Compactor {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.WorkingDirectory = filepath.Join(tempDir, workingDirName)
	cfg.RetentionEnabled = false

	if loopbackIFace, err := loki_net.LoopbackInterfaceName(); err == nil {
		cfg.CompactorRing.InstanceInterfaceNames = append(cfg.CompactorRing.InstanceInterfaceNames, loopbackIFace)
	}

	require.NoError(t, cfg.Validate())

	c, err := NewCompactor(cfg, objectClients, config.SchemaConfig{
		Configs: periodConfigs,
	}, nil, nil)
	require.NoError(t, err)

	c.RegisterIndexCompactor("dummy", testIndexCompactor{})

	return c
}

func TestCompactor_RunCompaction(t *testing.T) {
	tempDir := t.TempDir()

	tablesPath := filepath.Join(tempDir, "index")
	commonDBsConfig := IndexesConfig{NumUnCompactedFiles: 5}
	perUserDBsConfig := PerUserIndexesConfig{}

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	tableNumStart := tableNumEnd - 5

	periodConfigs := []config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Time(0)},
			IndexType:  "dummy",
			ObjectType: "fs_01",
			IndexTables: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: config.ObjectStorageIndexRequiredPeriod,
			},
		},
	}

	for i := tableNumStart; i <= tableNumEnd; i++ {
		SetupTable(t, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
	}

	var (
		objectClients = map[string]client.ObjectClient{}
		err           error
	)
	objectClients["fs_01"], err = local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	compactor := setupTestCompactor(t, objectClients, periodConfigs, tempDir)
	err = compactor.RunCompaction(context.Background(), false)
	require.NoError(t, err)

	for i := tableNumStart; i <= tableNumEnd; i++ {
		name := fmt.Sprintf("%s%d", indexTablePrefix, i)
		// verify that we have only 1 file left in storage after compaction.
		files, err := os.ReadDir(filepath.Join(tablesPath, name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(tablesPath, name))
	}
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
			IndexTables: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: config.ObjectStorageIndexRequiredPeriod,
			},
		},
		{
			From:       config.DayTime{Time: model.Time(periodTwoStart * daySeconds * 1000)},
			IndexType:  "dummy",
			ObjectType: "fs_02",
			IndexTables: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: config.ObjectStorageIndexRequiredPeriod,
			},
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
		objectClients = map[string]client.ObjectClient{}
		err           error
	)
	objectClients["fs_01"], err = local.NewFSObjectClient(local.FSConfig{Directory: periodOnePath})
	require.NoError(t, err)

	objectClients["fs_02"], err = local.NewFSObjectClient(local.FSConfig{Directory: periodTwoPath})
	require.NoError(t, err)

	compactor := setupTestCompactor(t, objectClients, periodConfigs, tempDir)
	err = compactor.RunCompaction(context.Background(), false)
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
			IndexTables: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: time.Hour * 24,
			},
			RowShards: 16,
		},
		{
			From:       dayFromTime(start.Add(25 * time.Hour)),
			IndexType:  "boltdb",
			ObjectType: "filesystem",
			Schema:     "v12",
			IndexTables: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: time.Hour * 24,
			},
			RowShards: 16,
		},
		{
			From:       dayFromTime(start.Add(73 * time.Hour)),
			IndexType:  "tsdb",
			ObjectType: "filesystem",
			Schema:     "v12",
			IndexTables: config.PeriodicTableConfig{
				Prefix: tsdbIndexTablePrefix,
				Period: time.Hour * 24,
			},
			RowShards: 16,
		},
		{
			From:       dayFromTime(start.Add(100 * time.Hour)),
			IndexType:  "tsdb",
			ObjectType: "filesystem",
			Schema:     "v12",
			IndexTables: config.PeriodicTableConfig{
				Prefix: indexTablePrefix,
				Period: time.Hour * 24,
			},
			RowShards: 16,
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
			actual, actualFound := schemaPeriodForTable(tt.config, tt.tableName)
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

	sortTablesByRange(intervals)
	require.Equal(t, []string{"index_19195", "index_19192", "index_19191"}, intervals)
}
