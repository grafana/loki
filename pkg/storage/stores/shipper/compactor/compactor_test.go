package compactor

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	loki_net "github.com/grafana/loki/pkg/util/net"
)

func setupTestCompactor(t *testing.T, tempDir string, clientMetrics storage.ClientMetrics) *Compactor {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.WorkingDirectory = filepath.Join(tempDir, workingDirName)
	cfg.SharedStoreType = "filesystem"
	cfg.RetentionEnabled = false

	if loopbackIFace, err := loki_net.LoopbackInterfaceName(); err == nil {
		cfg.CompactorRing.InstanceInterfaceNames = append(cfg.CompactorRing.InstanceInterfaceNames, loopbackIFace)
	}

	require.NoError(t, cfg.Validate())

	c, err := NewCompactor(cfg, storage.Config{FSConfig: local.FSConfig{Directory: tempDir}}, loki_storage.SchemaConfig{}, nil, clientMetrics, nil)
	require.NoError(t, err)

	return c
}

func TestCompactor_RunCompaction(t *testing.T) {
	tempDir := t.TempDir()

	tablesPath := filepath.Join(tempDir, "index")
	tablesCopyPath := filepath.Join(tempDir, "index-copy")

	tables := map[string]map[string]testutil.DBConfig{
		"table1": {
			"db1": {
				DBRecords: testutil.DBRecords{
					Start:      0,
					NumRecords: 10,
				},
			},
			"db2": {
				DBRecords: testutil.DBRecords{
					Start:      10,
					NumRecords: 10,
				},
			},
			"db3": {
				DBRecords: testutil.DBRecords{
					Start:      20,
					NumRecords: 10,
				},
			},
			"db4": {
				DBRecords: testutil.DBRecords{
					Start:      30,
					NumRecords: 10,
				},
			},
		},
		"table2": {
			"db1": {
				DBRecords: testutil.DBRecords{
					Start:      40,
					NumRecords: 10,
				},
			},
			"db2": {
				DBRecords: testutil.DBRecords{
					Start:      50,
					NumRecords: 10,
				},
			},
			"db3": {
				DBRecords: testutil.DBRecords{
					Start:      60,
					NumRecords: 10,
				},
			},
			"db4": {
				DBRecords: testutil.DBRecords{
					Start:      70,
					NumRecords: 10,
				},
			},
		},
	}

	for name, dbs := range tables {
		testutil.SetupDBsAtPath(t, filepath.Join(tablesPath, name), dbs, nil)

		// setup exact same copy of dbs for comparison.
		testutil.SetupDBsAtPath(t, filepath.Join(tablesCopyPath, name), dbs, nil)
	}

	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	compactor := setupTestCompactor(t, tempDir, cm)
	err := compactor.RunCompaction(context.Background(), false)
	require.NoError(t, err)

	for name := range tables {
		// verify that we have only 1 file left in storage after compaction.
		files, err := ioutil.ReadDir(filepath.Join(tablesPath, name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		// verify we have all the kvs in compacted db which were there in source dbs.
		compareCompactedTable(t, filepath.Join(tablesPath, name), filepath.Join(tablesCopyPath, name))
	}
}
