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
	start     = model.Now().Add(-30 * 24 * time.Hour)
)

func setupTestCompactor(t *testing.T, tempDir string) *Compactor {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.WorkingDirectory = filepath.Join(tempDir, workingDirName)
	cfg.SharedStoreType = "filesystem"
	cfg.RetentionEnabled = false

	if loopbackIFace, err := loki_net.LoopbackInterfaceName(); err == nil {
		cfg.CompactorRing.InstanceInterfaceNames = append(cfg.CompactorRing.InstanceInterfaceNames, loopbackIFace)
	}

	require.NoError(t, cfg.Validate())

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	indexType := "dummy"

	c, err := NewCompactor(cfg, objectClient, config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:      config.DayTime{Time: model.Time(0)},
				IndexType: indexType,
				IndexTables: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: config.ObjectStorageIndexRequiredPeriod,
				},
			},
		},
	}, nil, nil)
	require.NoError(t, err)

	c.RegisterIndexCompactor(indexType, testIndexCompactor{})

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

	for i := tableNumStart; i <= tableNumEnd; i++ {
		SetupTable(t, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
	}

	compactor := setupTestCompactor(t, tempDir)
	err := compactor.RunCompaction(context.Background(), false)
	require.NoError(t, err)

	for i := tableNumStart; i <= tableNumEnd; i++ {
		name := fmt.Sprintf("%s%d", indexTablePrefix, i)
		// verify that we have only 1 file left in storage after compaction.
		files, err := os.ReadDir(filepath.Join(tablesPath, name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)))
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
