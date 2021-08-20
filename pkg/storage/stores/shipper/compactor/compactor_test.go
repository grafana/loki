package compactor

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/stretchr/testify/require"

	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

func setupTestCompactor(t *testing.T, tempDir string) *Compactor {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.WorkingDirectory = filepath.Join(tempDir, workingDirName)
	cfg.SharedStoreType = "filesystem"
	cfg.RetentionEnabled = false

	c, err := NewCompactor(cfg, storage.Config{FSConfig: local.FSConfig{Directory: tempDir}}, loki_storage.SchemaConfig{}, nil, nil)
	require.NoError(t, err)

	return c
}

func TestIsDefaults(t *testing.T) {
	for i, tc := range []struct {
		in  *Config
		out bool
	}{
		{&Config{
			WorkingDirectory: "/tmp",
		}, false},
		{&Config{}, false},
		{&Config{
			SharedStoreKeyPrefix:      "index/",
			CompactionInterval:        10 * time.Minute,
			RetentionDeleteDelay:      2 * time.Hour,
			RetentionDeleteWorkCount:  150,
			DeleteRequestCancelPeriod: 24 * time.Hour,
			MaxCompactionParallelism:  1,
		}, true},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.out, tc.in.IsDefaults())
		})
	}
}

func TestCompactor_RunCompaction(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "compactor-run-compaction")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	tablesPath := filepath.Join(tempDir, "index")
	tablesCopyPath := filepath.Join(tempDir, "index-copy")

	tables := map[string]map[string]testutil.DBRecords{
		"table1": {
			"db1": {
				Start:      0,
				NumRecords: 10,
			},
			"db2": {
				Start:      10,
				NumRecords: 10,
			},
			"db3": {
				Start:      20,
				NumRecords: 10,
			},
			"db4": {
				Start:      30,
				NumRecords: 10,
			},
		},
		"table2": {
			"db1": {
				Start:      40,
				NumRecords: 10,
			},
			"db2": {
				Start:      50,
				NumRecords: 10,
			},
			"db3": {
				Start:      60,
				NumRecords: 10,
			},
			"db4": {
				Start:      70,
				NumRecords: 10,
			},
		},
	}

	for name, dbs := range tables {
		testutil.SetupDBTablesAtPath(t, name, tablesPath, dbs, false)

		// setup exact same copy of dbs for comparison.
		testutil.SetupDBTablesAtPath(t, name, tablesCopyPath, dbs, false)
	}

	compactor := setupTestCompactor(t, tempDir)
	err = compactor.RunCompaction(context.Background())
	require.NoError(t, err)

	for name := range tables {
		// verify that we have only 1 file left in storage after compaction.
		files, err := ioutil.ReadDir(filepath.Join(tablesPath, name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		// verify we have all the kvs in compacted db which were there in source dbs.
		compareCompactedDB(t, filepath.Join(tablesPath, name, files[0].Name()), filepath.Join(tablesCopyPath, name))
	}
}
